# yak-academy 架构设计文档

> 版本：v0.1（第一版：架构设计 + 前后端骨架）
> 本文档是项目的完整设计源。约束的执行摘要位于 `.agents/skills/`，两者冲突时以本文档为准。

## 1. 背景与对标分析

yak-academy 是一个安全实训平台，提供安全教程（文字+视频）、独立实战题目、学习路径、进度与积分排行。设计对标调研结论：

| 参考对象 | 借鉴点 | 不借鉴点 |
|---|---|---|
| PortSwigger Web Security Academy | 训练场按分类分组展示题目；题目详情页承载描述/提示/环境/提交；路径只引用不复制；服务端自动判定（远期）；徽章/认证分层激励 | 商业变现部分 |
| CTFd | challenges/flags/hints/submissions 数据模型；恒时比较；动态计分衰减函数 | Flask 插件体系、比赛制 teams 模式（第一版） |
| TryHackMe | 教题关联：教程与题目互指而非互嵌；Path→Room→Lesson；XP 游戏化 | 靶机常驻 IaaS 成本模型；教题强制同库耦合 |
| CTFtime | 排行榜按赛季切片；题目页聚合题解（远期） | 纯 UGC 聚合模式（无判定） |

**核心架构决策**（均在 skill 中有对应纠偏规则）：

1. **教程与题目两类独立资源**：教程包只含教程（文字+视频），题目用固定格式 challenge.json 单独上传；题目可选 `tutorial_slug` 关联配套教程（双向入口），不关联的纯题目也合法（`.agents/skills/teaching-integration`）。
2. **内容只来源于管理员上传**：内容包 zip + 五步上传管线，禁止内置内容（`.agents/skills/content-package-spec`）。
3. **双层 RBAC**：全局角色 + 课程级角色，声明式中间件鉴权（`.agents/skills/rbac-model`）。
4. **事件溯源**：积分/进度/排行榜从 append-only 事件派生，判题异步（`.agents/skills/event-sourcing-rules`）。
5. **契约先行**：openapi.yaml 是接口唯一契约源（`.agents/skills/api-contract`）。
6. **分层单向依赖**：api → service → repo → model（`.agents/skills/project-conventions`）。

## 2. 总体架构

```
┌────────────────────────────────────────────────────────────┐
│ 表现层  frontend/  Vue3 + TS + Vite                          │
│   学生端：Topic/Room 教程浏览、训练场（题目分组列表+详情）、     │
│           路径、进度、排行榜                                    │
│   管理端：资源管理（教程管理/题目管理）、用户与角色管理、       │
│           容器监控                                             │
├────────────────────────────────────────────────────────────┤
│ 服务层  backend/internal/api  REST + JWT + RBAC 中间件        │
│   ├─ auth        登录 / JWT / 双层 RBAC                       │
│   ├─ content     教程包上传→解析→校验→入库→发布（五步管线）    │
│   ├─ challenge   Challenge/Flag/Hint 业务（题目独立资源）      │
│   ├─ judge       异步判题 worker（恒时比较、动态计分）          │
│   ├─ env         Lab 环境生命周期（InstanceDriver 接口，v1 stub）│
│   ├─ userprogress 学习路径、进度跟踪                            │
│   └─ eventbus    进程内 append-only 事件总线 + 派生器           │
├────────────────────────────────────────────────────────────┤
│ 数据层  SQLite + gorm（repo 层接口抽象，可平滑迁 PostgreSQL）    │
│   业务库：用户/内容/题目/事件/派生表                             │
├────────────────────────────────────────────────────────────┤
│ 基础设施（远期）  题目环境容器化：模板镜像+按需实例+空闲回收       │
└────────────────────────────────────────────────────────────┘
```

依赖方向（唯一合法）：`api → service → repo → model`。service 横向依赖白名单：`judge→event`、`content→challenge`。

## 3. 模块设计

### 3.1 auth — 认证与权限

- 登录换取 JWT（HS256），中间件解析并注入请求上下文。
- 双层角色模型：全局 `student/teacher/admin` + 课程级角色（CourseRole，scoped 到 Topic/课程域）。
- 路由注册时声明 `RequireRole(...)`，中间件统一执行；属主规则（"只看自己班级"）在 service 层。

### 3.2 content — 教程包管线

教程包只含教程（文字 + 视频），题目定义不经此管线。五步管线 `Upload → Parse → Validate → Persist → Publish`，整体拒绝（禁止部分入库）：

1. Upload：接收 zip，校验大小与 MIME；
2. Parse：解析 manifest.yaml / topic.md / lesson.md（front matter 可选 `video` 字段）；
3. Validate：front matter 必填、难度枚举、slug 冲突；
4. Persist：写 ContentPackage/ContentVersion，状态 draft；
5. Publish：管理员显式发布；新版本 = 新 ContentVersion，旧版本可回滚。

状态机：`draft → published → archived`（详见 `.agents/skills/content-package-spec`）。

### 3.3 challenge — 题目域（独立资源）

- 题目以自有 `slug` 为主键入口，管理员经 challenge.json（固定格式 JSON，单对象或数组）上传：**upsert 即生效**（slug 重复 = 更新），不走 draft/publish 管线（版本化仍属教程包机制）。
- `category` 为自由分类 slug（训练场分组键）；`description_md` 承载背景与任务目标。
- Challenge 多态 `type` 列（v1：static / regex flag）。
- Flag 比较使用恒时比较（逐字符 XOR），防时序侧信道。
- `tutorial_slug` 可选关联教程 Room：题目详情给"配套教程"入口，Room 详情反查给"前往题目"入口；纯题目（不关联）合法。
- Hint 扣分解锁（写 `hint.unlocked` 事件；v1 详情页直接展示，扣分机制待事件溯源迭代）。

### 3.4 judge — 异步判题

- 提交接口只入队并返回 pending；worker 消费执行判定，写 `submission.judged` 事件。
- 动态计分：`value = f(solve_count)`，linear / logarithmic 衰减，下限 minimum。
- 远期：服务端自动判定（环境内检测点回调，参考 PortSwigger）。

### 3.5 env — Lab 环境（v1 仅接口）

```go
type InstanceDriver interface {
    Create(ctx context.Context, chal ChallengeID, user UserID) (Instance, error)
    Destroy(ctx context.Context, id InstanceID) error
    Health(ctx context.Context, id InstanceID) (Status, error)
}
```

v1 提供 noop stub；Phase 3 实现 Docker 驱动（模板镜像 + 30 分钟空闲回收 + 预热池）。

### 3.6 userprogress — 路径与进度

- LearningPath = 有序 Room ID 引用（PathItem），不复制内容。
- Progress 为事件派生物：Room 完成率、路径完成率、XP。

### 3.7 eventbus — 事件总线

- 进程内 append-only：`Append(event)` 落库 + 内存广播。
- 派生器（deriver）订阅事件 → 增量更新 ScoreRecord / Progress / 排行榜缓存。
- Phase 2 可替换为 Redis Stream / MQ，接口不变。

## 4. 数据模型（ER 概览）

```
User ─┬─ Role(global: student|teacher|admin)
      ├─ CourseRole(user, course/topic scope)
      ├─ Submission ─→ Challenge
      └─ Progress(派生) ─→ Room

ContentPackage ─→ ContentVersion(draft|published|archived)   # 教程包
Topic ─→ Room(lesson.md + 可选 video)
Challenge(slug 主键, category, difficulty, description_md,     # 独立题目资源
          tutorial_slug → Room 可选关联)
       ─┬─ Flag(type: static|regex)
         ├─ Hint(title/content/cost)
         └─ Submission(user, answer, status: pending|correct|incorrect)
LearningPath ─→ PathItem(path_id, room_id, order)
Event(id, user_id, type, payload, created_at)   # append-only
ScoreRecord(派生) / LabInstance(user×challenge, status, expire_at)
```

字段级定义见 `backend/internal/model/`（gorm 实体即数据字典）。

## 5. 关键数据流

三条主时序（带泳景图见 `docs/data-flow.md`）：

1. **教程上传流**：teacher 上传教程 zip → content 管线五步 → draft → admin 发布 → 对学生可见；
2. **题目上传流**：admin 上传 challenge.json → 校验 → upsert 即生效（无 draft/publish）；
3. **做题判定流**：student 在题目详情页提交 → 入队 pending → worker 恒时比较 → `submission.judged` 事件 → 派生器更新积分/进度/排行榜（题目关联教程时同步发 Room 通关事件）；
4. **鉴权流**：请求 → JWT 解析 → 角色中间件（全局→课程级）→ handler。

## 6. API 契约总览

完整契约见 `docs/openapi.yaml`。第一版关键接口：

| 方法与路径 | 角色 | 说明 |
|---|---|---|
| `POST /auth/login` | 公开 | 登录换 JWT |
| `GET /me` | student | 当前用户与角色 |
| `GET /topics` / `GET /topics/{slug}` | student | 主题列表/详情（含 Room 列表） |
| `GET /topics/{slug}/rooms/{roomSlug}` | student | Room 教程详情（文字+视频，含关联题目 slug） |
| `GET /challenges` | student | 训练场题目列表（按 category 分组展示） |
| `GET /challenges/{slug}` | student | 题目详情（描述/提示/教程关联） |
| `POST /challenges/{slug}/instance` | student | 启动题目容器环境 |
| `DELETE /challenges/{slug}/instance` | student | 销毁环境 |
| `POST /submissions` | student | 提交答案（challenge_slug 键，返回 pending） |
| `GET /submissions/{id}` | student | 查询判定结果 |
| `GET /learning-paths` / `GET /learning-paths/{id}` | student | 路径列表/详情（含进度） |
| `GET /progress` | student | 个人进度 |
| `GET /leaderboard` | student | 排行榜（派生数据） |
| `POST /content/packages` | teacher | 上传教程包（进入管线） |
| `GET /content/packages/{id}` | teacher | 包状态与版本 |
| `POST /content/packages/{id}/publish` | admin | 发布 |
| `POST /admin/challenges` | admin | 上传题目 JSON（upsert 即生效） |
| `GET /admin/challenges` | teacher | 题目管理表格 |
| `DELETE /admin/challenges/{slug}` | admin | 删除题目（级联 flags/hints） |
| `POST /admin/users` / `PATCH /admin/users/{id}/roles` | admin | 用户与角色管理 |

## 7. 技术选型

| 项 | 选型 | 理由 |
|---|---|---|
| 后端 | Go 1.22+ / net/http（标准库路由） | 零框架依赖起步，分层清晰 |
| ORM | gorm + SQLite | 与 yaklang 生态一致；repo 接口抽象便于迁移 |
| 认证 | golang-jwt/jwt/v5（HS256） | 成熟、无状态 |
| 前端 | Vue3 + TS + Vite + Pinia + Vue Router | 管理后台开发效率 |
| API 契约 | OpenAPI 3.0（手写 yaml） | 契约先行，前后端并行 |

## 8. 演进路线

- **Phase 1（当前）**：架构文档 + skill 体系 + 可编译骨架（stub 实现）；登录/上传/判题为契约+stub。
- **Phase 2**：最小闭环——登录、内容包上传与发布、Room 浏览、提交与异步判题、事件派生积分排行榜。
- **Phase 3**：题目环境容器化（Docker 驱动：模板镜像+按需实例+回收）；服务端自动判定；徽章与认证。
- **Phase 4**：多人规模——SQLite → PostgreSQL、事件总线外置（Redis Stream/MQ）、排行榜按赛季切片。

## 9. 第一版边界（明确不做）

- 不实现真实登录/判题/上传逻辑（契约 + 骨架 + stub）。
- 不引入 Casbin / Redis / K8s。
- 题目环境只定义 `InstanceDriver` 接口，不实现驱动。
- 前端页面为占位骨架。
