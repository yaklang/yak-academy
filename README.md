# yak-academy

安全实训平台：教程（文字 + 视频）与题目（独立 challenge.json 资源、可选关联教程）两类内容、两档 RBAC 权限、事件驱动的积分/进度/排行榜。前后端分离（Vue3 / Go），所有内容来源于管理员上传，无内置内容；题目环境容器化，支持环境内检测点回调自动判定通关。

## 快速开始

```bash
bash start.sh        # Git Bash / Linux / macOS；Windows 可双击 start.bat
```

- 浏览器访问 http://127.0.0.1:5173（后端 :8080，`/api` 自动代理）
- 停止：Ctrl+C 或 `bash stop.sh`
- 默认管理员账号 `admin / admin123`（users 表为空时启动自动创建，请尽快修改）

## 部署（Docker Compose，单机）

```bash
cp deploy/env.example .env   # 编辑：设置 ACADEMY_JWT_SECRET / ACADEMY_ADMIN_PASSWORD
docker compose up -d --build
```

- 前端入口 `http://127.0.0.1:8081`（nginx 托管 SPA 并将 `/api` 反代到后端），后端直接暴露 `:8080`
- **docker.sock 方案**：后端容器挂载宿主 `/var/run/docker.sock` 并内置 docker CLI，题目容器创建在宿主 daemon 上（与平台是兄弟容器，非嵌套）；SQLite 与上传内容持久化在 `./data/`
- **边界**：题目容器端口固定发布到宿主 `127.0.0.1`（`DockerDriver.Create`），学员必须在本机访问题目 endpoint——多用户远程访问属 Phase 4 范围；`ACADEMY_PUBLIC_URL` 决定题目容器检测点回调地址，Docker Desktop（Win/Mac）用默认 `host.docker.internal` 即可，Linux 需改为宿主 IP 或 docker0 网桥 IP（通常 `172.17.0.1`）

## 文档

- `AGENTS.md`——AI 协作入口（skill 加载规则，任何 AI 助手先读这个）
- `docs/architecture.md`、`docs/data-flow.md`——架构设计与核心数据流
- `docs/openapi.yaml`——API 契约（唯一契约源，改接口先改它）
- `.agents/skills/`——6 个架构约束 skill

## 约定（详见 AGENTS.md 与对应 skill）

- 分层单向依赖：api → service → repo → model
- 教程与题目两类独立资源：题目可选 `tutorial_slug` 关联配套教程，纯题目合法（训练场以题目为中心）
- 教程包五步管线：Upload → Parse → Validate → Persist → Publish；题目 challenge.json 上传 upsert 即生效
- 事件溯源：积分/进度/排行榜从 append-only 事件派生，判题异步；题目可重复提交，分值只结算一次