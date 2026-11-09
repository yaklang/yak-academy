#!/usr/bin/env bash
# yak-academy 部署机自动更新脚本（systemd user timer 每 5 分钟触发一次）。
#
# 职责：把部署机上的 main worktree 快进到 origin/main；代码变化时在部署机
# 本地构建 backend/frontend 镜像、确保 vulinbox 基础镜像、重启实例。
#
# 设计约束（docs/architecture.md §9、remote-deploy skill）：
#   - 后端镜像不再要求开发机用 Docker 构建后再 docker save | docker load；
#     部署机直接 docker compose build。受限网络通过 .env 的 ACADEMY_GOPROXY
#     指定可达的 Go 模块代理（如 https://goproxy.cn,direct），默认 Go 官方代理。
#   - 构建前确保 Dockerfile FROM 的基础镜像存在；缺失时从
#     ACADEMY_DOCKER_MIRROR（默认 docker.1ms.run）拉取并 retag，避免镜像清理
#     或宿主机重置后构建失败。
#   - 所有需要的镜像构建成功后才 docker compose up -d，不同版本不混跑。
#   - 健康检查失败时按构建前备份的镜像 ID 回滚并重启旧版本。
#
# 安装（部署机）：
#   install -m 755 deploy/auto-update.sh ~/academy_workspace/scripts/auto-update.sh
# systemd user unit 的 ExecStart 指向该路径即可。
#
# 可覆盖环境变量：
#   ACADEMY_WS              workspace 根，默认 $HOME/academy_workspace
#   ACADEMY_MAIN_DIR        main worktree，默认 $ACADEMY_WS/main
#   ACADEMY_DOCKER_MIRROR   基础镜像来源，默认 docker.1ms.run；设为空跳过自动拉取
# 端口、镜像 tag、GOPROXY 等部署参数由 main/.env 提供。
set -uo pipefail

WS="${ACADEMY_WS:-$HOME/academy_workspace}"
MAIN="${ACADEMY_MAIN_DIR:-$WS/main}"
LOG="$WS/auto-update.log"
LOCK=/tmp/academy-auto-update.lock
VULIN_TAG=yak-academy/vulinbox:latest

log(){ echo "[$(date '+%F %T')] $*" >>"$LOG"; }

# 旧版脚本只在部署成功后轮转，WAIT 期间日志会无限增长；改为每轮运行前轮转。
rotate_log() {
  local size
  size=$(stat -c%s "$LOG" 2>/dev/null || echo 0)
  if [ "$size" -gt 204800 ]; then
    tail -c 102400 "$LOG" >"$LOG.tmp" && mv "$LOG.tmp" "$LOG"
  fi
}
rotate_log

# 构建/确保 vulinbox 基础镜像。force=1 强制重建；否则脚本自身在已存在时 skip。
ensure_vulinbox() {
  local force="${1:-0}"
  local build_script="$MAIN/deploy/build-vulinbox-image.sh"
  if [ -f "$build_script" ] && [ ! -x "$build_script" ]; then
    chmod +x "$build_script"
  fi
  if [ ! -f "$build_script" ]; then
    log "WARN: 缺少 $build_script，跳过 vulinbox 构建"
    return 0
  fi
  if [ "$force" = 1 ]; then
    log "vulinbox: 强制构建 $VULIN_TAG"
    ACADEMY_FORCE_VULINBOX=1 bash "$build_script" >>"$LOG" 2>&1 \
      || { log "FAIL: vulinbox 构建失败"; return 1; }
  else
    log "vulinbox: 确保 $VULIN_TAG 存在"
    bash "$build_script" >>"$LOG" 2>&1 \
      || { log "FAIL: vulinbox 构建失败"; return 1; }
  fi
  log "vulinbox: OK"
  return 0
}

# 从两个 Dockerfile 的 FROM 行提取基础镜像，缺失时从镜像源拉取并 retag。
ensure_base_images() {
  local dockerfile image remote
  for dockerfile in "$MAIN/deploy/backend.Dockerfile" "$MAIN/deploy/frontend.Dockerfile"; do
    [ -f "$dockerfile" ] || continue
    while read -r image; do
      [ -n "$image" ] || continue
      if docker image inspect "$image" >/dev/null 2>&1; then
        continue
      fi
      [ -n "$MIRROR" ] || { log "FAIL: 基础镜像 $image 缺失且未配置 ACADEMY_DOCKER_MIRROR"; return 1; }
      case "$image" in
        */*) remote="$MIRROR/$image" ;;
        *)   remote="$MIRROR/library/$image" ;;
      esac
      # 上一轮可能已经从镜像源拉过、只是没 retag；直接复用，避免重复下载。
      if docker image inspect "$remote" >/dev/null 2>&1; then
        docker tag "$remote" "$image" >>"$LOG" 2>&1 \
          || { log "FAIL: retag $remote -> $image"; return 1; }
        log "base image 就绪: $image（复用 $remote）"
        continue
      fi
      log "base image 缺失: $image，从 $remote 拉取"
      if ! timeout 600 docker pull "$remote" >>"$LOG" 2>&1; then
        log "FAIL: 拉取 $remote 失败"
        return 1
      fi
      if ! docker tag "$remote" "$image" >>"$LOG" 2>&1; then
        log "FAIL: retag $remote -> $image"
        return 1
      fi
      log "base image 就绪: $image"
    done < <(awk '/^[[:space:]]*FROM[[:space:]]/ {for (i = 2; i <= NF; i++) if ($i !~ /^--/) {print $i; break}}' "$dockerfile" | sort -u)
  done
  return 0
}

# 恢复到构建前的镜像与源码。仅在 DEPLOYED != none 时有效。
rollback() {
  [ "$DEPLOYED" != none ] || return 1
  log "回滚到 deployed=${DEPLOYED:0:12}"
  [ -n "${BACK_OLD:-}" ] && docker tag "$BACK_OLD" "$BACK_IMAGE" >>"$LOG" 2>&1 || true
  [ -n "${FRONT_OLD:-}" ] && docker tag "$FRONT_OLD" "$FRONT_IMAGE" >>"$LOG" 2>&1 || true
  git reset --hard -q "$DEPLOYED" || true
  docker compose up -d >>"$LOG" 2>&1 || return 1
  return 0
}

exec 9>"$LOCK"; flock -n 9 || exit 0   # 上一轮构建未结束时跳过

cd "$MAIN" || { log "FATAL: worktree $MAIN 不存在"; exit 1; }
[ -f .env ] || { log "FATAL: $MAIN/.env 缺失（未做过首次部署）"; exit 1; }

env_get() { grep -E "^$1=" .env | tail -1 | cut -d= -f2-; }
TAG=$(env_get ACADEMY_IMAGE_TAG); TAG=${TAG:-latest}
MIRROR="${ACADEMY_DOCKER_MIRROR:-$(env_get ACADEMY_DOCKER_MIRROR)}"
MIRROR="${MIRROR:-docker.1ms.run}"
BACK_IMAGE="yak-academy/backend:$TAG"
FRONT_IMAGE="yak-academy/frontend:$TAG"

# 守卫（对应 legion git_switch_main）：main 树只允许本脚本推进；
# 出现本地改动说明有人在树上开发，跳过等待人工处理。
if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  log "SKIP: main 工作树有本地改动，跳过（人工清理后自动恢复）"; exit 0
fi

git fetch -q origin main || { log "WARN: git fetch 失败，下轮重试"; exit 0; }
REMOTE=$(git rev-parse FETCH_HEAD)
LOCAL=$(git rev-parse HEAD)
DEPLOYED=$(cat .deployed 2>/dev/null || echo none)

if [ "$REMOTE" != "$LOCAL" ]; then
  # 仅快进到远端；本地超前（未推送）时绝不 reset，避免吞掉本地提交。
  if git merge-base --is-ancestor "$LOCAL" "$REMOTE"; then
    log "main 更新: ${LOCAL:0:12} -> ${REMOTE:0:12}"
    git reset --hard -q "$REMOTE"
    LOCAL="$REMOTE"
  elif git merge-base --is-ancestor "$REMOTE" "$LOCAL"; then
    log "SKIP: 本地超前远端（${REMOTE:0:12}..${LOCAL:0:12}），等待 push 后再部署"
    exit 0
  else
    log "SKIP: main 与远端分叉（local=${LOCAL:0:12} remote=${REMOTE:0:12}），人工处理"
    exit 0
  fi
fi

# 已部署最新时：仍检查靶场镜像是否被 prune 掉，缺失则补建后退出。
if [ "$DEPLOYED" = "$LOCAL" ]; then
  if ! docker image inspect "$VULIN_TAG" >/dev/null 2>&1; then
    log "vulinbox: 已部署但镜像缺失，补建"
    ensure_vulinbox 1 || exit 0
  fi
  exit 0
fi

log "部署 main@${LOCAL:0:12}（deployed=${DEPLOYED:0:12}）"

# 变更面探测（baseline = 已部署提交）。
BASE="$DEPLOYED"
BACK_CHG=""; FRONT_CHG=""; VULIN_CHG=""
if [ "$BASE" != none ]; then
  # vulinbox image/ 变更不算 backend 服务镜像，单独走 vulinbox 构建。
  BACK_CHG=$(git diff --name-only "$BASE" "$LOCAL" -- backend/ ':!backend/internal/service/builtin/vulinbox/image/' deploy/backend.Dockerfile docker-compose.yml)
  FRONT_CHG=$(git diff --name-only "$BASE" "$LOCAL" -- frontend/ deploy/frontend.Dockerfile deploy/nginx.conf docker-compose.yml)
  VULIN_CHG=$(git diff --name-only "$BASE" "$LOCAL" -- backend/internal/service/builtin/vulinbox/image/ deploy/build-vulinbox-image.sh)
else
  BACK_CHG="first-deploy"
  FRONT_CHG="first-deploy"
  VULIN_CHG="first-deploy"
fi

BUILD=()
[ -n "$BACK_CHG" ] && BUILD+=(backend)
[ -n "$FRONT_CHG" ] && BUILD+=(frontend)

if [ ${#BUILD[@]} -gt 0 ]; then
  ensure_base_images || { log "FAIL: 基础镜像准备失败，保留现网，下轮重试"; exit 0; }
  BACK_OLD=$(docker image inspect -f '{{.Id}}' "$BACK_IMAGE" 2>/dev/null || true)
  FRONT_OLD=$(docker image inspect -f '{{.Id}}' "$FRONT_IMAGE" 2>/dev/null || true)
  log "构建: ${BUILD[*]}（main@${LOCAL:0:12}）"
  if ! docker compose build "${BUILD[@]}" >>"$LOG" 2>&1; then
    log "FAIL: ${BUILD[*]} 构建失败，保留现网，下轮重试"
    exit 0
  fi
fi

if [ -n "$VULIN_CHG" ]; then
  ensure_vulinbox 1 || { log "FAIL: vulinbox 构建失败，保留现网，下轮重试"; exit 0; }
else
  ensure_vulinbox 0 || { log "FAIL: vulinbox 构建失败，保留现网，下轮重试"; exit 0; }
fi

if ! docker compose up -d >>"$LOG" 2>&1; then
  log "FAIL: docker compose up 失败，尝试回滚"
  rollback || true
  exit 1
fi

WEB=$(env_get ACADEMY_WEB_PORT)
API=$(env_get ACADEMY_BACKEND_PORT)
if [ -z "$WEB" ] || [ -z "$API" ]; then
  log "FATAL: .env 缺少 ACADEMY_WEB_PORT / ACADEMY_BACKEND_PORT"
  rollback || true
  exit 1
fi

ok=0
for _ in $(seq 1 12); do
  sleep 5
  w=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:${WEB}/" || true)
  a=$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:${API}/health" || true)
  if [ "$w" = 200 ] && [ "$a" = 200 ]; then ok=1; break; fi
done

if [ "$ok" != 1 ]; then
  log "CRITICAL: main@${LOCAL:0:12} 健康检查失败（web=$w api=$a），尝试回滚"
  rollback || true
  exit 1
fi

echo "$LOCAL" > .deployed
rm -f .backend-image-commit   # 旧脚本的手工 marker，已废弃
log "OK: 已部署 main@${LOCAL:0:12}（web=$WEB api=$API）"
