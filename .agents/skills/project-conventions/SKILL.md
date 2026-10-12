---
name: project-conventions
description: yak-academy 项目分层与目录约定。任何代码改动前必须阅读本 skill，防止破坏分层架构。适用于：新增/修改 Go 后端代码、调整目录结构、新增模块、依赖引入。
---

# 项目分层约定（强制）

## 分层依赖方向（唯一允许的依赖箭头）

```
api → service → repo → model
```

- `api`：HTTP handler + 中间件。只做：参数解析 → 调 service → 返回响应。**禁止**包含业务逻辑、**禁止**直接 import repo 或 model 之外触碰数据库。
- `service`：业务逻辑。**禁止**直接使用 gorm/sql 对象，一切数据访问走 repo 接口。
- `repo`：数据访问层。每个 repo 面向 service 暴露接口（Go interface），SQLite 是其唯一实现细节，**禁止**向上泄漏 gorm 类型。
- `model`：gorm 实体定义。只有字段和标签，**禁止**包含业务方法。

## 禁止事项

- 禁止 service 之间横向 import（白名单除外：`judge` 依赖 `event`、`content` 依赖 `challenge`）。
- 禁止 handler 里散落权限判断（见 rbac-model skill：统一中间件）。
- 禁止绕过 repo 直接开 DB 连接。
- 禁止引入 Casbin / Redis / Kafka 等中间件（第一版自实现 + 进程内广播，见 docs/architecture.md）。

## 目录职责表

| 目录 | 职责 |
|---|---|
| `backend/internal/api` | handler、中间件、路由注册 |
| `backend/internal/service` | 业务逻辑（auth/content/challenge/judge/env/userprogress） |
| `backend/internal/repo` | 数据访问接口及 SQLite 实现 |
| `backend/internal/model` | gorm 实体 |
| `backend/internal/eventbus` | 进程内事件总线 |
| `backend/internal/config` | 配置加载 |
| `frontend/src/api` | 按契约手写的请求封装（唯一允许发 HTTP 的地方） |

## 正反例

✅ 正确：`api/room.go` 的 handler 调用 `service.Room.Get(ctx, id)`，service 调用 `repo.Room.Find(ctx, id)`。
❌ 错误：handler 里 `db.Where("id = ?", id).First(&room)`。
❌ 错误：`service/content` import `service/judge`（不在白名单）。
