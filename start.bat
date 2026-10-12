@echo off
rem yak-academy 一键启动（Windows 双击运行），依赖 Git Bash（含 bash）
where bash >nul 2>nul
if errorlevel 1 (
  echo [yak-academy] 未找到 bash，请先安装 Git Bash 后重试
  pause
  exit /b 1
)
bash "%~dp0start.sh"
pause