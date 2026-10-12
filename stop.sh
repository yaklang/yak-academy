#!/usr/bin/env bash
# 停止 yak-academy 全部服务（按端口杀掉 8080 / 5173 的监听进程）。
# 用法：bash stop.sh
# 适用场景：start.sh 异常退出后残留进程、或提示端口被占用时。
cd "$(dirname "$0")"

for port in 8080 5173; do
  pids=$(netstat -ano 2>/dev/null | grep "LISTENING" | grep ":$port " | awk '{print $NF}' | sort -u)
  if [ -z "$pids" ]; then
    echo "[yak-academy] 端口 $port 无监听进程"
    continue
  fi
  for pid in $pids; do
    echo "[yak-academy] 停止端口 $port (PID $pid)"
    taskkill //PID "$pid" //F >/dev/null 2>&1 || kill -9 "$pid" 2>/dev/null || true
  done
done
echo "[yak-academy] 完成"