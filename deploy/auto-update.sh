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
#   - 部署状态 .deployed 是跨轮唯一需要保存的事实：原子写入并保留
#     .deployed.prev；标记丢失时依次从上一份标记、运行中镜像的
#     org.opencontainers.image.revision 标签恢复。基线未知只影响“增量判断”，
#     绝不因此强制重建靶场/攻击盒镜像（镜像存在就复用）。
#   - 可选镜像（AWD 攻击盒）默认不参与发布，需 ACADEMY_AWD_ENABLED=1 才构建，
#     避免一个可选组件把整个发布卡死。
#   - 失败退避：连续失败按 5/10/20/40/60 分钟退避，并写入 update-status.json；
#     配置 ACADEMY_ALERT_WEBHOOK（飞书 text 消息格式）后可推送告警。
#
# 安装（部署机）：
#   install -m 755 deploy/auto-update.sh ~/academy_workspace/scripts/auto-update.sh
# systemd user unit 的 ExecStart 指向该路径即可。脚本会按 SOFTWARE_VERSION
# 把仓库里的更高版本同步到安装位置（只升不降），避免长期运行旧副本。
#
# 可覆盖环境变量：
#   ACADEMY_WS              workspace 根，默认 $HOME/academy_workspace
#   ACADEMY_MAIN_DIR        main worktree，默认 $ACADEMY_WS/main
#   ACADEMY_DOCKER_MIRROR   基础镜像来源，默认 docker.1ms.run；设为空跳过自动拉取
#   ACADEMY_AWD_ENABLED     设为 1 才构建/检查 AWD 攻击盒镜像（默认 0）
#   ACADEMY_ALERT_WEBHOOK   失败告警 webhook（飞书 text 消息格式），默认关闭
# 端口、镜像 tag、GOPROXY 等部署参数由 main/.env 提供。
set -uo pipefail

# 安装副本的版本标记：仅用于“只升不降”的自动同步比较。
SOFTWARE_VERSION=20261126.1

WS="${ACADEMY_WS:-$HOME/academy_workspace}"
MAIN="${ACADEMY_MAIN_DIR:-$WS/main}"
LOG="$WS/auto-update.log"
STATE="$WS/update-state"
STATUS="$WS/update-status.json"
LOCK=/tmp/academy-auto-update.lock
VULIN_TAG=yak-academy/vulinbox:latest
BACKOFF_STEPS=(5 10 20 40 60)
DEPLOYED=none
LOCAL=""
FAIL_REASON_HINT=""

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

state_get() {
  local k="$1" v=""
  [ -f "$STATE" ] || return 0
  v=$(grep -E "^${k}=" "$STATE" 2>/dev/null | tail -1 | cut -d= -f2-)
  printf '%s' "$v"
}

state_set() {
  local k="$1" v="$2" tmp="$STATE.tmp.$$"
  if [ -f "$STATE" ]; then
    grep -vE "^${k}=" "$STATE" >"$tmp" 2>/dev/null || : >"$tmp"
  else
    : >"$tmp"
  fi
  printf '%s=%s\n' "$k" "$v" >>"$tmp"
  mv -f "$tmp" "$STATE"
}

# 供监控/人工查看的最近一次运行结果（JSON，单行）。
status_write() {
  local result="$1" reason="$2" failures="$3" next="${4:-0}" tmp="$STATUS.tmp.$$"
  reason=${reason//[^A-Za-z0-9_.: -]/}
  printf '{"at":"%s","result":"%s","reason":"%s","failures":%s,"next_attempt":%s,"deployed":"%s","local":"%s"}\n' \
    "$(date -Iseconds)" "$result" "$reason" "$failures" "$next" "$DEPLOYED" "$LOCAL" >"$tmp" \
    && mv -f "$tmp" "$STATUS"
}

alert() {
  local msg="$1"
  log "ALERT: $msg"
  [ -n "${ACADEMY_ALERT_WEBHOOK:-}" ] || return 0
  curl -sS -m 10 -H 'Content-Type: application/json' \
    -d "{\"msg_type\":\"text\",\"content\":{\"text\":\"[yak-academy] ${msg//\"/}\"}}" \
    "$ACADEMY_ALERT_WEBHOOK" >/dev/null 2>&1 || log "WARN: 告警发送失败"
}

# 记录失败并按连续失败次数退避；code 保留原有退出语义（0=可重试，1=需关注）。
fail_and_exit() {
  local reason="$1" code="${2:-0}" n idx mins next
  n=$(state_get failures)
  case "$n" in ''|*[!0-9]*) n=0;; esac
  n=$((n + 1))
  idx=$((n - 1))
  [ "$idx" -ge "${#BACKOFF_STEPS[@]}" ] && idx=$(( ${#BACKOFF_STEPS[@]} - 1 ))
  mins="${BACKOFF_STEPS[$idx]}"
  next=$(( $(date +%s) + mins * 60 ))
  state_set failures "$n"
  state_set last_reason "$reason"
  state_set next_attempt "$next"
  status_write failed "$reason" "$n" "$next"
  log "BACKOFF: 连续失败 ${n} 次，${mins} 分钟后重试（reason=${reason}）"
  [ "$n" -ge 2 ] && alert "自动更新连续失败 ${n} 次：${reason}（日志 $LOG）"
  exit "$code"
}

mark_success() {
  state_set failures 0
  state_set last_reason ""
  state_set next_attempt ""
  status_write ok "" 0 0
}

marker_read() {
  [ -f "$1" ] || return 0
  tr -d '[:space:]' <"$1" 2>/dev/null
}

# 原子写入 .deployed，并保留一份 .deployed.prev 供标记丢失时恢复。
marker_write() {
  local c="$1" tmp="$MAIN/.deployed.tmp.$$"
  printf '%s\n' "$c" >"$tmp" && mv -f "$tmp" "$MAIN/.deployed" || return 1
  cp -f "$MAIN/.deployed" "$MAIN/.deployed.prev" 2>/dev/null || true
  return 0
}

# 从运行中的前端/后端容器读取构建时写入的 commit 标签。
deployed_from_images() {
  local name v
  for name in main-frontend-1 main-backend-1; do
    v=$(docker inspect "$name" \
      --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' 2>/dev/null || true)
    case "$v" in ''|'<no value>'|unknown) continue;; esac
    printf '%s' "$v"
    return 0
  done
  return 1
}

# 把仓库里的更高版本脚本同步到安装位置（只升不降，避免回退手工热修的版本）。
sync_installed_script() {
  local src="$MAIN/deploy/auto-update.sh" dst="$WS/scripts/auto-update.sh" rv iv newest
  [ -f "$src" ] && [ -f "$dst" ] || return 0
  cmp -s "$src" "$dst" && return 0
  rv=$(sed -n 's/^SOFTWARE_VERSION=//p' "$src" 2>/dev/null | tail -1)
  iv=$(sed -n 's/^SOFTWARE_VERSION=//p' "$dst" 2>/dev/null | tail -1)
  # 仓库副本暂无版本标记（本改动尚未合入）：保留安装版本，绝不降级。
  case "$rv" in ''|*[!0-9.]*) return 0;; esac
  case "$iv" in ''|*[!0-9.]*) iv=0;; esac
  newest=$(printf '%s\n%s\n' "$rv" "$iv" | sort -V | tail -1)
  [ "$newest" = "$rv" ] && [ "$rv" != "$iv" ] || return 0
  if bash -n "$src" 2>/dev/null && install -m 755 "$src" "$dst.new.$$" && mv -f "$dst.new.$$" "$dst"; then
    log "已从仓库同步更新脚本到 ${rv}（下一轮生效）"
  else
    log "WARN: 同步更新脚本失败，保留当前版本"
  fi
}

# 构建/确保 vulinbox 基础镜像。force=1 强制重建；否则脚本自身在已存在时 skip。
ensure_vulinbox() {
  local force="${1:-0}" rc out="$WS/.vulinbox-build.log"
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
    ACADEMY_FORCE_VULINBOX=1 bash "$build_script" >"$out" 2>&1; rc=$?
  else
    log "vulinbox: 确保 $VULIN_TAG 存在"
    bash "$build_script" >"$out" 2>&1; rc=$?
  fi
  cat "$out" >>"$LOG" 2>/dev/null || true
  if [ "$rc" -ne 0 ]; then
    if grep -qE 'sha256sum: WARNING|vulinbox: FAILED|computed checksum did NOT match' "$out" 2>/dev/null; then
      FAIL_REASON_HINT=vulinbox-sha-mismatch
      log "FAIL: vulinbox 构建失败：下载产物 sha256 与锁定值不符（上游 latest 很可能已更新）。需要人工更新"
      log "      backend/internal/service/builtin/vulinbox/image/Dockerfile 的 VULINBOX_SHA256（并验证题集）后重试。"
    else
      FAIL_REASON_HINT=vulinbox-build-failed
      log "FAIL: vulinbox 构建失败"
    fi
    return 1
  fi
  log "vulinbox: OK"
  return 0
}

# 构建/确保 AWD SSH 攻击盒镜像；缺失或 Dockerfile/entrypoint 变更时重建。
ensure_awd_attackbox() {
  local force="${1:-0}"
  local build_script="$MAIN/deploy/build-awd-attackbox.sh"
  if [ -f "$build_script" ] && [ ! -x "$build_script" ]; then
    chmod +x "$build_script"
  fi
  if [ ! -f "$build_script" ]; then
    log "WARN: 缺少 $build_script，跳过攻击盒构建"
    return 0
  fi
  if [ "$force" = 1 ]; then
    log "awd-attackbox: 强制构建 $ATTACK_TAG"
    ACADEMY_FORCE_ATTACKBOX=1 ACADEMY_AWD_ATTACK_IMAGE="$ATTACK_TAG" \
      bash "$build_script" >>"$LOG" 2>&1 \
      || { log "FAIL: 攻击盒镜像构建失败"; return 1; }
  else
    log "awd-attackbox: 确保 $ATTACK_TAG 存在"
    ACADEMY_AWD_ATTACK_IMAGE="$ATTACK_TAG" bash "$build_script" >>"$LOG" 2>&1 \
      || { log "FAIL: 攻击盒镜像构建失败"; return 1; }
  fi
  log "awd-attackbox: OK"
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
  marker_write "$DEPLOYED" || true
  return 0
}

exec 9>"$LOCK"; flock -n 9 || exit 0   # 上一轮构建未结束时跳过

# 退避：上一轮失败后等到 next_attempt 再试，避免每 5 分钟重复同样的失败。
NOW=$(date +%s)
NEXT=$(state_get next_attempt)
case "$NEXT" in ''|*[!0-9]*) NEXT=0;; esac
if [ "$NEXT" -gt "$NOW" ]; then
  log "BACKOFF: 等待中（reason=$(state_get last_reason)，剩余 $((NEXT - NOW))s）"
  exit 0
fi

cd "$MAIN" || { log "FATAL: worktree $MAIN 不存在"; exit 1; }
[ -f .env ] || { log "FATAL: $MAIN/.env 缺失（未做过首次部署）"; exit 1; }

env_get() { grep -E "^$1=" .env | tail -1 | cut -d= -f2-; }
TAG=$(env_get ACADEMY_IMAGE_TAG); TAG=${TAG:-latest}
MIRROR="${ACADEMY_DOCKER_MIRROR:-$(env_get ACADEMY_DOCKER_MIRROR)}"
MIRROR="${MIRROR:-docker.1ms.run}"
BACK_IMAGE="yak-academy/backend:$TAG"
FRONT_IMAGE="yak-academy/frontend:$TAG"
ATTACK_TAG=$(env_get ACADEMY_AWD_ATTACK_IMAGE); ATTACK_TAG=${ATTACK_TAG:-yak-awd-attackbox:latest}
AWD_ENABLED="${ACADEMY_AWD_ENABLED:-$(env_get ACADEMY_AWD_ENABLED)}"
AWD_ENABLED="${AWD_ENABLED:-0}"

# 守卫（对应 legion git_switch_main）：main 树只允许本脚本推进；
# 出现本地改动说明有人在树上开发，跳过等待人工处理。
if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  log "SKIP: main 工作树有本地改动，跳过（人工清理后自动恢复）"; exit 0
fi

git fetch -q origin main || { log "WARN: git fetch 失败，按退避重试"; fail_and_exit git-fetch-failed 0; }
REMOTE=$(git rev-parse FETCH_HEAD)
LOCAL=$(git rev-parse HEAD)

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

sync_installed_script

# 解析已部署提交：.deployed -> .deployed.prev -> 运行中镜像的 revision 标签。
DEPLOYED=$(marker_read "$MAIN/.deployed")
DEPLOYED_SRC=deployed
if [ -z "$DEPLOYED" ]; then
  DEPLOYED=$(marker_read "$MAIN/.deployed.prev")
  DEPLOYED_SRC=prev
fi
if [ -z "$DEPLOYED" ]; then
  DEPLOYED=$(deployed_from_images)
  DEPLOYED_SRC=image-label
fi
if [ -n "$DEPLOYED" ] && ! git cat-file -e "${DEPLOYED}^{commit}" 2>/dev/null; then
  log "WARN: 部署标记 ${DEPLOYED:0:12} 不在当前仓库中，忽略"
  DEPLOYED=""
fi
if [ -z "$DEPLOYED" ]; then
  DEPLOYED=none
  log "WARN: 部署标记缺失（.deployed/.deployed.prev/镜像标签均不可用）：按未知状态部署"
  log "      不强制重建靶场/攻击盒镜像，回滚不可用；下次成功部署后自愈。"
else
  if [ "$DEPLOYED_SRC" != deployed ]; then
    log "WARN: .deployed 缺失，已从 ${DEPLOYED_SRC} 恢复 deployed=${DEPLOYED:0:12}"
    marker_write "$DEPLOYED" || log "WARN: 恢复 .deployed 文件失败"
  fi
fi

# 已部署最新时：仍检查靶场/攻击盒镜像是否被 prune 掉，缺失则补建后退出。
if [ "$DEPLOYED" = "$LOCAL" ]; then
  if ! docker image inspect "$VULIN_TAG" >/dev/null 2>&1; then
    log "vulinbox: 已部署但镜像缺失，补建"
    ensure_vulinbox 1 || fail_and_exit "${FAIL_REASON_HINT:-vulinbox-build-failed}" 0
  fi
  if [ "$AWD_ENABLED" = 1 ] && ! docker image inspect "$ATTACK_TAG" >/dev/null 2>&1; then
    log "awd-attackbox: 已部署但镜像缺失，补建"
    ensure_awd_attackbox 1 || fail_and_exit attackbox-build-failed 0
  fi
  mark_success
  exit 0
fi

log "部署 main@${LOCAL:0:12}（deployed=${DEPLOYED:0:12}）"

# 变更面探测（baseline = 已部署提交）。
BASE="$DEPLOYED"
BACK_CHG=""; FRONT_CHG=""; VULIN_CHG=""; ATTACK_CHG=""
if [ "$BASE" != none ]; then
  # vulinbox image/ 变更不算 backend 服务镜像，单独走 vulinbox 构建。
  BACK_CHG=$(git diff --name-only "$BASE" "$LOCAL" -- backend/ ':!backend/internal/service/builtin/vulinbox/image/' deploy/backend.Dockerfile docker-compose.yml)
  FRONT_CHG=$(git diff --name-only "$BASE" "$LOCAL" -- frontend/ deploy/frontend.Dockerfile deploy/nginx.conf docker-compose.yml)
  VULIN_CHG=$(git diff --name-only "$BASE" "$LOCAL" -- backend/internal/service/builtin/vulinbox/image/ deploy/build-vulinbox-image.sh)
  ATTACK_CHG=$(git diff --name-only "$BASE" "$LOCAL" -- deploy/awd-attackbox.Dockerfile deploy/awd-attackbox/ deploy/build-awd-attackbox.sh)
else
  BACK_CHG="first-deploy"
  FRONT_CHG="first-deploy"
  # 基线未知只影响应用镜像；靶场/攻击盒镜像保持“存在即复用”，
  # 缺失时下面的 ensure_* 0 仍会补建。
  VULIN_CHG=""
  ATTACK_CHG=""
fi

BUILD=()
[ -n "$BACK_CHG" ] && BUILD+=(backend)
[ -n "$FRONT_CHG" ] && BUILD+=(frontend)

if [ ${#BUILD[@]} -gt 0 ]; then
  ensure_base_images || fail_and_exit base-image-failed 0
  BACK_OLD=$(docker image inspect -f '{{.Id}}' "$BACK_IMAGE" 2>/dev/null || true)
  FRONT_OLD=$(docker image inspect -f '{{.Id}}' "$FRONT_IMAGE" 2>/dev/null || true)
  log "构建: ${BUILD[*]}（main@${LOCAL:0:12}）"
  # ACADEMY_IMAGE_REVISION 由 docker-compose.yml 的 build.labels 写入镜像，
  # 用于 .deployed 丢失时从运行中容器恢复部署基线。
  if ! ACADEMY_IMAGE_REVISION="$LOCAL" docker compose build "${BUILD[@]}" >>"$LOG" 2>&1; then
    log "FAIL: ${BUILD[*]} 构建失败，保留现网"
    fail_and_exit app-build-failed 0
  fi
fi

if [ -n "$VULIN_CHG" ]; then
  ensure_vulinbox 1 || fail_and_exit "${FAIL_REASON_HINT:-vulinbox-build-failed}" 0
else
  ensure_vulinbox 0 || fail_and_exit "${FAIL_REASON_HINT:-vulinbox-build-failed}" 0
fi

if [ "$AWD_ENABLED" = 1 ]; then
  if [ -n "$ATTACK_CHG" ]; then
    ensure_awd_attackbox 1 || fail_and_exit attackbox-build-failed 0
  else
    ensure_awd_attackbox 0 || fail_and_exit attackbox-build-failed 0
  fi
else
  log "awd-attackbox: 未启用（ACADEMY_AWD_ENABLED!=1），跳过"
fi

if ! docker compose up -d >>"$LOG" 2>&1; then
  log "FAIL: docker compose up 失败，尝试回滚"
  rollback || true
  fail_and_exit compose-up-failed 1
fi

WEB=$(env_get ACADEMY_WEB_PORT)
API=$(env_get ACADEMY_BACKEND_PORT)
if [ -z "$WEB" ] || [ -z "$API" ]; then
  log "FATAL: .env 缺少 ACADEMY_WEB_PORT / ACADEMY_BACKEND_PORT"
  rollback || true
  fail_and_exit env-missing-ports 1
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
  fail_and_exit health-check-failed 1
fi

marker_write "$LOCAL" || log "WARN: 写入 .deployed 失败"
DEPLOYED="$LOCAL"
rm -f .backend-image-commit   # 旧脚本的手工 marker，已废弃
mark_success
log "OK: 已部署 main@${LOCAL:0:12}（web=$WEB api=$API）"
