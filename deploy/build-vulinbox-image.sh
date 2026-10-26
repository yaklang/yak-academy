#!/usr/bin/env bash
# 构建 yak-academy 内置训练集基础镜像（yak-academy/vulinbox:latest）。
#
# 远端 Docker Desktop 直连 docker.io 常超时（daemon 不吃 shell 代理），因此：
#   1) 经 registry mirror 预拉并 retag 基础镜像（golang / alpine）
#   2) --network=host + HTTP(S)_PROXY，使 apk/wget 能走本机 7897 代理
#      （build 容器内 127.0.0.1 否则指向容器自身，代理不可达）
#
# 环境变量（均可选）：
#   ACADEMY_VULINBOX_TAG     目标 tag，默认 yak-academy/vulinbox:latest
#   ACADEMY_DOCKER_MIRROR    基础镜像源，默认 docker.m.daocloud.io
#   ACADEMY_HTTP_PROXY       构建期代理，默认 http://127.0.0.1:7897
#   ACADEMY_FORCE_VULINBOX=1 强制重建（已有镜像也重建）
#   ACADEMY_BUILD_NETWORK    docker build --network，默认 host；设 bridge 可关闭
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
IMAGE_CTX="$REPO_ROOT/backend/internal/service/builtin/vulinbox/image"
TAG="${ACADEMY_VULINBOX_TAG:-yak-academy/vulinbox:latest}"
MIRROR="${ACADEMY_DOCKER_MIRROR:-docker.m.daocloud.io}"
PROXY="${ACADEMY_HTTP_PROXY:-${http_proxy:-${HTTP_PROXY:-http://127.0.0.1:7897}}}"
FORCE="${ACADEMY_FORCE_VULINBOX:-0}"
NET="${ACADEMY_BUILD_NETWORK:-host}"

log(){ echo "[build-vulinbox] $*"; }

[ -f "$IMAGE_CTX/Dockerfile" ] || { log "FATAL: missing $IMAGE_CTX/Dockerfile"; exit 1; }

if [ "$FORCE" != 1 ] && docker image inspect "$TAG" >/dev/null 2>&1; then
  log "skip: $TAG already present (set ACADEMY_FORCE_VULINBOX=1 to rebuild)"
  exit 0
fi

# Docker Desktop：client 侧 HTTP_PROXY 对 pull 无效；走 mirror 再 retag。
ensure_base() {
  local name="$1" # e.g. golang:1.26-alpine / alpine:3.21
  if docker image inspect "$name" >/dev/null 2>&1; then
    log "base ok: $name"
    return 0
  fi
  local remote="$MIRROR/library/$name"
  log "pull base: $remote"
  docker pull "$remote"
  docker tag "$remote" "$name"
  log "retag: $remote -> $name"
}

ensure_base golang:1.26-alpine
ensure_base alpine:3.21

log "build $TAG (proxy=$PROXY network=$NET)"
# build-arg 覆盖 RUN 层（apk / wget OSS）；FROM 已由本地同名基础镜像满足。
docker build \
  --network="$NET" \
  --build-arg "HTTP_PROXY=$PROXY" \
  --build-arg "HTTPS_PROXY=$PROXY" \
  --build-arg "http_proxy=$PROXY" \
  --build-arg "https_proxy=$PROXY" \
  --build-arg "NO_PROXY=localhost,127.0.0.1" \
  --build-arg "no_proxy=localhost,127.0.0.1" \
  -t "$TAG" \
  "$IMAGE_CTX"

log "OK: $TAG"
docker image inspect "$TAG" --format "{{.Id}} {{.Size}}"
