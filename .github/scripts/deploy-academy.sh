#!/usr/bin/env bash
# Shared CI deployment entry point. No credentials or host-specific secrets belong here.
set -euo pipefail
umask 077

SOURCE_SHA="${1:?Usage: deploy-academy.sh <40-character source SHA>}"
[[ "$SOURCE_SHA" =~ ^[0-9a-f]{40}$ ]] || { echo 'Invalid source SHA' >&2; exit 1; }
[[ "$(id -u)" == 0 ]] || { echo 'Deployment requires root on the target server' >&2; exit 1; }
DOMAIN=academy.yaklang.com
DEPLOY_ROOT=/opt/yak-academy
CONFIG_DIR=/etc/yak-academy
ENV_FILE=$CONFIG_DIR/academy.env
DATA_DIR=/var/lib/yak-academy
CERT_DIR=/etc/nginx/ssl/$DOMAIN
BUNDLE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RELEASE_DIR=$DEPLOY_ROOT/releases/$SOURCE_SHA

install -d -m 700 "$DEPLOY_ROOT" "$DEPLOY_ROOT/releases" "$CONFIG_DIR" "$DATA_DIR"
exec 9>"$DEPLOY_ROOT/deploy.lock"
flock -w 600 9
[[ "$(cat "$BUNDLE_DIR/source-sha")" == "$SOURCE_SHA" ]] || { echo 'Bundle source SHA mismatch' >&2; exit 1; }
(cd "$BUNDLE_DIR" && sha256sum -c checksums.txt)

if ! command -v docker >/dev/null || ! docker compose version >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l
  apt-get update -qq
  apt-get install -y --no-install-recommends docker.io docker-compose-v2
fi
systemctl enable --now docker
command -v nginx >/dev/null || { echo 'An existing nginx installation is required' >&2; exit 1; }

# First boot generates strong credentials; upgrades preserve the environment and data.
if [[ ! -f "$ENV_FILE" ]]; then
  jwt_secret="$(openssl rand -hex 32)"
  admin_password="$(openssl rand -hex 16)"
  cat > "$ENV_FILE" <<EOF
ACADEMY_JWT_SECRET=$jwt_secret
ACADEMY_ADMIN_USERNAME=admin
ACADEMY_ADMIN_PASSWORD=$admin_password
ACADEMY_STRICT=1
ACADEMY_DATA_DIR=$DATA_DIR
ACADEMY_WEB_PORT=127.0.0.1:18081
ACADEMY_BACKEND_PORT=127.0.0.1:18080
ACADEMY_PUBLIC_URL=https://$DOMAIN
ACADEMY_INSTANCE_PUBLIC_HOST=$DOMAIN
ACADEMY_INSTANCE_MEMORY=256m
ACADEMY_INSTANCE_CPUS=0.25
ACADEMY_BACKUP_INTERVAL=6h
ACADEMY_BACKUP_KEEP=7
ACADEMY_LOG_JSON=1
EOF
  printf 'URL=https://%s\nUSERNAME=admin\nPASSWORD=%s\n' "$DOMAIN" "$admin_password" > "$CONFIG_DIR/bootstrap-admin.txt"
  chmod 600 "$ENV_FILE" "$CONFIG_DIR/bootstrap-admin.txt"
  unset jwt_secret admin_password
fi

# Keep a consistent SQLite snapshot before an upgrade; uploads stay in the persistent volume.
if [[ -f "$DATA_DIR/academy.db" ]]; then
  install -d -m 700 "$DEPLOY_ROOT/pre-deploy-backups"
  python3 - "$DATA_DIR/academy.db" "$DEPLOY_ROOT/pre-deploy-backups/$(date -u +%Y%m%dT%H%M%SZ)-$SOURCE_SHA.db" <<'PY'
import sqlite3
import sys
with sqlite3.connect('file:' + sys.argv[1] + '?mode=ro', uri=True) as source:
    with sqlite3.connect(sys.argv[2]) as target:
        source.backup(target)
PY
fi

docker load --input "$BUNDLE_DIR/academy-images.tar.gz"
install -d -m 700 "$RELEASE_DIR/.github/deploy"
install -m 600 "$BUNDLE_DIR/docker-compose.yml" "$RELEASE_DIR/docker-compose.yml"
install -m 600 "$BUNDLE_DIR/.github/deploy/compose.production.yml" "$RELEASE_DIR/.github/deploy/compose.production.yml"
install -m 644 "$BUNDLE_DIR/.github/deploy/frontend.nginx.conf" "$RELEASE_DIR/.github/deploy/frontend.nginx.conf"
printf '%s\n' "$SOURCE_SHA" > "$RELEASE_DIR/source-sha"

compose_at() {
  local release_dir=$1
  shift
  ACADEMY_IMAGE_TAG="$(cat "$release_dir/source-sha")" \
    docker compose --project-name yak-academy --env-file "$ENV_FILE" \
      -f "$release_dir/docker-compose.yml" -f "$release_dir/.github/deploy/compose.production.yml" "$@"
}
compose_at "$RELEASE_DIR" config --quiet
previous_release="$(readlink -f "$DEPLOY_ROOT/current" 2>/dev/null || true)"
if ! compose_at "$RELEASE_DIR" up -d --no-build --pull never --wait --wait-timeout 180; then
  if [[ "$previous_release" == "$DEPLOY_ROOT/releases/"* && -f "$previous_release/source-sha" ]]; then
    echo 'New containers failed health checks; restarting the previous release' >&2
    compose_at "$previous_release" up -d --no-build --pull never --wait --wait-timeout 180
  fi
  exit 1
fi
curl --fail --silent --show-error http://127.0.0.1:18080/health \
  | python3 -c 'import json,sys; assert json.load(sys.stdin)["status"] == "ok"'
curl --fail --silent --show-error http://127.0.0.1:18081/ > /dev/null

# Add only the academy virtual host. Existing AIBalance/RAG virtual hosts stay in place.
install -d -m 755 /var/www/letsencrypt "$CERT_DIR"
if [[ -d /etc/nginx/sites-enabled ]]; then
  NGINX_CONF=/etc/nginx/sites-enabled/$DOMAIN.conf
else
  NGINX_CONF=/etc/nginx/conf.d/$DOMAIN.conf
fi
write_proxy_locations() {
  cat <<'NGINX'
    client_max_body_size 256m;
    location = /health {
        proxy_pass http://127.0.0.1:18080/health;
        proxy_set_header Host $host;
    }
    location / {
        proxy_pass http://127.0.0.1:18081;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 300s;
    }
NGINX
}
write_nginx() {
  {
    cat <<NGINX
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN;
    location /.well-known/acme-challenge/ { root /var/www/letsencrypt; }
NGINX
    if [[ -s "$CERT_DIR/fullchain.pem" && -s "$CERT_DIR/key.pem" ]]; then
      echo '    location / { return 301 https://$host$request_uri; }'
      cat <<NGINX
}
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name $DOMAIN;
    ssl_certificate $CERT_DIR/fullchain.pem;
    ssl_certificate_key $CERT_DIR/key.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
NGINX
    fi
    write_proxy_locations
    echo '}'
  } > "$NGINX_CONF"
  chmod 644 "$NGINX_CONF"
  nginx -t
  systemctl reload nginx
}
write_nginx

if [[ ! -s "$CERT_DIR/fullchain.pem" || ! -s "$CERT_DIR/key.pem" ]]; then
  ACME=/root/.acme.sh/acme.sh
  [[ -x "$ACME" ]] || { echo 'Install acme.sh or provide the academy TLS certificate' >&2; exit 1; }
  "$ACME" --issue --server letsencrypt -d "$DOMAIN" --webroot /var/www/letsencrypt --keylength ec-256
  "$ACME" --install-cert -d "$DOMAIN" --ecc \
    --key-file "$CERT_DIR/key.pem" --fullchain-file "$CERT_DIR/fullchain.pem" \
    --reloadcmd 'nginx -t && systemctl reload nginx'
  write_nginx
fi

curl --fail --silent --show-error --resolve "$DOMAIN:443:127.0.0.1" "https://$DOMAIN/health" \
  | python3 -c 'import json,sys; assert json.load(sys.stdin)["status"] == "ok"'
ln -sfn "$RELEASE_DIR" "$DEPLOY_ROOT/current"
printf 'Deployed %s at https://%s\nBootstrap credentials: %s/bootstrap-admin.txt\n' "$SOURCE_SHA" "$DOMAIN" "$CONFIG_DIR"
