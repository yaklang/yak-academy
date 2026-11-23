#!/usr/bin/env bash
# 构建 yak-academy AWD SSH 攻击盒镜像（默认 yak-awd-attackbox:latest）。
#
# 攻击盒是每次 AWD 比赛选手 SSH 进入的跳板：只接竞赛内部网络 + 随机 SSH
# 端口，镜像内含 bash/curl/jq/nc/python3/tcpdump 等常用攻防工具。
# 控制面按 USER_NAME/USER_PASSWORD 环境变量在容器启动时建号（见 entrypoint）。
#
# 环境变量（均可选）：
#   ACADEMY_AWD_ATTACK_IMAGE  目标 tag，默认 yak-awd-attackbox:latest
#   ACADEMY_DOCKER_MIRROR     基础镜像源，默认 docker.m.daocloud.io
#   ACADEMY_HTTP_PROXY        构建期代理（apk 下载），默认沿用环境 http_proxy
#   ACADEMY_FORCE_ATTACKBOX=1 强制重建（已有镜像也重建）
#   ACADEMY_BUILD_NETWORK     docker build --network，默认 host；设 bridge 可关闭
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
DOCKERFILE="$REPO_ROOT/deploy/awd-attackbox.Dockerfile"
TAG="${ACADEMY_AWD_ATTACK_IMAGE:-yak-awd-attackbox:latest}"
MIRROR="${ACADEMY_DOCKER_MIRROR:-docker.m.daocloud.io}"
PROXY="${ACADEMY_HTTP_PROXY:-${http_proxy:-${HTTP_PROXY:-}}}"
FORCE="${ACADEMY_FORCE_ATTACKBOX:-0}"
NET="${ACADEMY_BUILD_NETWORK:-host}"

log(){ echo "[build-awd-attackbox] $*"; }

[ -f "$DOCKERFILE" ] || { log "FATAL: missing $DOCKERFILE"; exit 1; }

if [ "$FORCE" != 1 ] && docker image inspect "$TAG" >/dev/null 2>&1; then
  log "skip: $TAG already present (set ACADEMY_FORCE_ATTACKBOX=1 to rebuild)"
  exit 0
fi

# 受限网络下先经 mirror 预拉基础镜像并 retag（FROM 才能命中本地镜像）。
ensure_base() {
  local name="$1"
  if docker image inspect "$name" >/dev/null 2>&1; then
    log "base ok: $name"
    return 0
  fi
  [ -n "$MIRROR" ] || { log "FATAL: base image $name missing and ACADEMY_DOCKER_MIRROR empty"; exit 1; }
  local remote="$MIRROR/library/$name"
  log "pull base: $remote"
  docker pull "$remote"
  docker tag "$remote" "$name"
  log "retag: $remote -> $name"
}

ensure_base alpine:3.21

BUILD_ARGS=(--network="$NET")
if [ -n "$PROXY" ]; then
  BUILD_ARGS+=(
    --build-arg "HTTP_PROXY=$PROXY"
    --build-arg "HTTPS_PROXY=$PROXY"
    --build-arg "http_proxy=$PROXY"
    --build-arg "https_proxy=$PROXY"
    --build-arg "NO_PROXY=localhost,127.0.0.1"
    --build-arg "no_proxy=localhost,127.0.0.1"
  )
fi

log "build $TAG (proxy=${PROXY:-none} network=$NET)"
docker build "${BUILD_ARGS[@]}" -f "$DOCKERFILE" -t "$TAG" "$REPO_ROOT"
log "OK: $TAG"
docker image inspect "$TAG" --format "{{.Id}} {{.Size}}"
