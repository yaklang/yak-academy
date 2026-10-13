#!/usr/bin/env bash
# yak-academy 一键启动（开发模式）
# 启动后端 :8080 与前端 :5173（/api 自动代理到后端），Ctrl+C 一并停止。
# 用法：bash start.sh   （Windows 可直接双击 start.bat）
set -euo pipefail
cd "$(dirname "$0")"

BACKEND_URL="http://127.0.0.1:8080"
FRONTEND_URL="http://127.0.0.1:5173"

log() { printf '\033[36m[yak-academy]\033[0m %s\n' "$*"; }
die() { printf '\033[31m[yak-academy]\033[0m %s\n' "$*" >&2; exit 1; }

# 依赖检查
command -v go   >/dev/null 2>&1 || die "未找到 go，请先安装 Go 1.22+"
command -v node >/dev/null 2>&1 || die "未找到 node，请先安装 Node 18+"
command -v npm  >/dev/null 2>&1 || die "未找到 npm"

# 端口占用检查（被占用先运行 bash stop.sh）
port_busy() { netstat -ano 2>/dev/null | grep "LISTENING" | grep -q ":$1 "; }
port_busy 8080 && die "端口 8080 已被占用（后端）。运行 bash stop.sh 后重试"
port_busy 5173 && die "端口 5173 已被占用（前端）。运行 bash stop.sh 后重试"

BACKEND_PID=""
FRONTEND_PID=""
cleanup() {
  log "正在停止全部服务..."
  [ -n "$FRONTEND_PID" ] && kill "$FRONTEND_PID" 2>/dev/null || true
  [ -n "$BACKEND_PID" ]  && kill "$BACKEND_PID"  2>/dev/null || true
}
trap cleanup EXIT INT TERM

# 环境驱动：docker daemon 可用则用 docker 驱动（题目可启动容器），否则 noop（启动容器报 501）。
# 显式设置 ACADEMY_ENV_DRIVER 时以环境变量为准。
ENV_DRIVER="${ACADEMY_ENV_DRIVER:-}"
if [ -z "$ENV_DRIVER" ]; then
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    ENV_DRIVER="docker"
    log "检测到 docker daemon：题目环境驱动 = docker"
  else
    ENV_DRIVER="noop"
    log "docker daemon 不可用：题目环境驱动 = noop（启动 Docker Desktop 后重启即可启用容器）"
  fi
fi

# 后端：先编译成二进制再运行（比 go run 进程树更好管理）
log "编译后端..."
(cd backend && go build -o academy-server-bin ./cmd/academy-server)
BIN="academy-server-bin"
[ -f "backend/academy-server-bin.exe" ] && BIN="academy-server-bin.exe"

log "启动后端 $BACKEND_URL"
export ACADEMY_ENV_DRIVER="$ENV_DRIVER"
(cd backend && "./$BIN") &
BACKEND_PID=$!

# 前端依赖（首次运行自动安装）
if [ ! -d frontend/node_modules ]; then
  log "首次运行：安装前端依赖..."
  (cd frontend && npm install)
fi

log "启动前端 $FRONTEND_URL"
(cd frontend && npm run dev) &
FRONTEND_PID=$!

# 等待后端就绪（curl 收到 401 即视为已监听）
for _ in $(seq 1 30); do
  if curl -s -o /dev/null "$BACKEND_URL/api/v1/topics"; then
    break
  fi
  sleep 1
done

log "就绪！浏览器打开 $FRONTEND_URL（Ctrl+C 停止全部服务）"
wait