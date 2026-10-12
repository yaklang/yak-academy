# yak-academy

安全实训平台：**教题一体**（Room = 教程 + 内嵌 Lab）、双层 RBAC 权限、事件驱动的积分/进度/排行榜。前后端分离，所有教程内容来源于**管理员上传**（内容包 zip），无内置内容。

## 快速开始

### 后端（Go 1.22+）

```bash
cd backend
go run ./cmd/academy-server
# 默认监听 :8080，SQLite 文件 academy.db（ACADEMY_ADDR / ACADEMY_DB / ACADEMY_JWT_SECRET 可覆盖）
```

### 前端（Node 18+）

```bash
cd frontend
npm install
npm run dev        # 开发服务器，/api 代理到 127.0.0.1:8080
npm run build      # 类型检查 + 产物构建
```

## 仓库结构

```
AGENTS.md            AI 协作入口（skill 加载规则）
.agents/skills/      6 个架构约束 skill（分层/内容包/教题一体/RBAC/事件溯源/API 契约）
docs/
  architecture.md   架构设计（四层架构、模块设计、数据模型、演进路线）
  data-flow.md      三条核心时序（上传流/判定流/鉴权流）
  openapi.yaml      API 契约（唯一契约源）
backend/            Go 后端（api → service → repo → model 单向分层）
frontend/           Vue3 + TS + Vite 前端（学生端 + 管理端）
```

## 核心架构决策

1. **教题一体**：Room = 教程 + 内嵌 Lab，不分离两库（对标 TryHackMe）。
2. **内容只来自管理员上传**：zip 内容包，五步管线 Upload→Parse→Validate→Persist→Publish。
3. **双层 RBAC**：全局角色（student/teacher/admin）+ 课程级角色。
4. **事件溯源**：积分/进度/排行榜从 append-only 事件派生，判题异步。
5. **契约先行**：改接口必须先改 `docs/openapi.yaml`。
6. **分层单向依赖**：api → service → repo → model。

> AI 助手请先阅读 `AGENTS.md`——每个决策在 `.agents/skills/` 有对应的纠偏规则。

## 路线图

- **Phase 1（当前）**：架构文档 + skill 体系 + 可编译骨架（stub）。
- **Phase 2**：最小闭环——登录、内容包上传与发布、Room 浏览、异步判题、事件派生排行。
- **Phase 3**：题目环境容器化（Docker 驱动）、服务端自动判定、徽章。
- **Phase 4**：PostgreSQL 迁移、外置事件总线、排行榜赛季切片。
