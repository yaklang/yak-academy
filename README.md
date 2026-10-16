# yak-academy

安全实训平台：教程（文字 + 视频）与题目（独立 challenge.json 资源、可选关联教程）两类内容、两档 RBAC 权限、事件驱动的积分/进度/排行榜。前后端分离（Vue3 / Go），所有内容来源于管理员上传，无内置内容；题目环境容器化，支持环境内检测点回调自动判定通关。

## 快速开始

```bash
bash start.sh        # Git Bash / Linux / macOS；Windows 可双击 start.bat
```

- 浏览器访问 http://127.0.0.1:5173（后端 :8080，`/api` 自动代理）
- 停止：Ctrl+C 或 `bash stop.sh`
- 默认管理员账号 `admin / admin123`（users 表为空时启动自动创建，请尽快修改）

## 部署（Docker Compose，Linux 宿主）

部署约定：**以 Docker Compose 容器化部署于 Linux 宿主（amd64）为唯一受支持形态**；Docker Desktop（Win/Mac）仅用于本地体验，裸跑后端二进制（`start.sh` / `start.bat`）仅限本地开发。环境要求：Linux x86_64 + Docker Engine ≥ 20.10（`host-gateway` 支持）+ compose 插件。

```bash
cp deploy/env.example .env   # 编辑：设置 ACADEMY_JWT_SECRET / ACADEMY_ADMIN_PASSWORD
docker compose up -d --build
```

- 前端入口 `http://<宿主IP>:8081`（nginx 托管 SPA 并将 `/api` 反代到后端），后端直接暴露 `:8080`
- **docker.sock 方案**：后端容器挂载宿主 `/var/run/docker.sock` 并内置 docker CLI，题目容器创建在宿主 daemon 上（与平台是兄弟容器，非嵌套）；SQLite 与上传内容持久化在 `./data/`（升级 = 重新 `up -d --build`，备份 = 备份 `./data`）
- **检测点回调**：题目容器经 `host.docker.internal` 回达后端——`DockerDriver.Create` 以 `--add-host host-gateway` 注入解析，Linux 与 Docker Desktop 默认值均开箱即用；仅当宿主防火墙拦截容器→宿主 8080（如 firewalld 默认拦 docker0）时改 `ACADEMY_PUBLIC_URL` 为宿主 LAN IP
- **边界**：题目容器端口固定发布到宿主 `127.0.0.1`（`DockerDriver.Create`），学员必须在本机访问题目 endpoint——多用户远程访问属 Phase 4 范围

## 环境搭建与镜像加速（本地开发）

本地起环境最常见的两类卡点与对策：

**1. Playwright 浏览器下载慢/失败 —— 本套件默认不需要下载。**

`frontend/playwright.config.ts` 会自动探测本机已装的 Chrome（找不到再找 Edge），
用系统浏览器作为测试通道（`channel`），**完全跳过 Playwright 内置浏览器下载**：

```bash
cd frontend
npm run e2e          # 自动用系统 Chrome，零下载
```

显式指定通道：`E2E_CHANNEL=chrome`（系统 Chrome）/ `E2E_CHANNEL=msedge`（Edge）/
`E2E_CHANNEL=bundled`（强制用 Playwright 内置 chromium，需要下载时才用）。
CI 上同理：GitHub Actions 的 ubuntu runner 预装 Google Chrome，CI 设
`E2E_CHANNEL=chrome` 免下载。

确实需要内置 chromium（如无系统 Chrome 的裸机容器）且下载慢时的镜像加速
——**注意甄别**：`registry.npmmirror.com/-/binary/playwright` 镜像已停止同步新版本
（截至 2026-10 最高只有 chromium 1200，而本仓库 playwright 1.64 需要 1248），
镜像加速方案不可靠；`mirrors.huaweicloud.com/playwright` 返回的 200 是 HTML
回退页而非真实 zip。可靠路径只有官方 CDN
（`npx playwright install chromium` 走 `cdn.playwright.dev`，国内不稳时挂代理），
或直接装系统 Chrome/Edge 后用 channel 方案（推荐）。

**2. Docker 拉取 `nginx:alpine` 等镜像慢 —— 配置 registry mirror。**

Docker Desktop：Settings → Docker Engine，Linux 直接编辑 `/etc/docker/daemon.json`：

```json
{ "registry-mirrors": ["https://docker.m.daocloud.io"] }
```

改完 `docker info` 的 Registry Mirrors 一节应出现该地址；
验证：`docker pull nginx:alpine` 应明显提速。镜像站可用性随时间波动，
`docker.m.daocloud.io` 不可用时可换 `docker.1ms.run` 等再试。

## 测试

| 层 | 怎么跑 | 说明 |
|---|---|---|
| 后端单测 | `cd backend && go test -race ./...` | 全量（含契约守卫：`internal/api/contract_test.go` 双向比对 openapi.yaml 与注册路由） |
| 前端单测 | `cd frontend && npm test` | 纯逻辑单测（vue-tsc + vitest 在 `npm run build` 一并执行） |
| E2E | `cd frontend && npm run e2e` | Playwright 全旅程（13 用例）：自动起 Go 后端 :18080 + vite :15173、每次整跑全新 DB、经管理 API 播种 |
| E2E（容器） | `E2E_DOCKER=1 npm run e2e` | 追加 docker project（1 用例）：真实容器启动→访问→409→续期→销毁，需本机 Docker |
| Docker 集成 | `cd backend && ACADEMY_TEST_DOCKER=1 go test ./internal/service/ -run Docker` | 真实 daemon 门控集（无 Docker 时自动 Skip） |

E2E 调试：`E2E_KEEP_STATE=1` 保留运行后的 DB/附件目录（`frontend/e2e/.state/`），
`npx playwright test --project=chromium -g "关键词"` 单跑一个用例，
失败自动留 trace/截图（`npx playwright show-trace <zip>`）。

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