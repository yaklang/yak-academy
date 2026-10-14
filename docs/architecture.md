# yak-academy 架构设计文档

> 版本：v0.2（Phase 2 最小闭环 + Phase 3 容器化大部分完成）
> 本文档是项目的完整设计源。约束的执行摘要位于 `.agents/skills/`，两者冲突时以本文档为准。

## 1. 背景与对标分析

yak-academy 是一个安全实训平台，提供安全教程（文字+视频）、独立实战题目、学习路径、进度与积分排行。设计对标调研结论：

| 参考对象 | 借鉴点 | 不借鉴点 |
|---|---|---|
| PortSwigger Web Security Academy | 训练场按分类分组展示题目；题目详情页承载描述/提示/环境/提交；路径只引用不复制；服务端自动判定（远期）；徽章/认证分层激励 | 商业变现部分 |
| 实训常用判题约定 | challenges/flags/hints/submissions 数据模型；恒时比较；动态计分衰减函数 | 比赛制 teams 模式、第三方插件体系（第一版） |
| TryHackMe | 教题关联：教程与题目互指而非互嵌；Path→Room→Lesson；XP 游戏化 | 靶机常驻 IaaS 成本模型；教题强制同库耦合 |
| CTFtime | 排行榜按赛季切片；题目页聚合题解（远期） | 纯 UGC 聚合模式（无判定） |

**核心架构决策**（均在 skill 中有对应纠偏规则）：

1. **教程与题目两类独立资源**：教程包只含教程（文字+视频），题目用固定格式 challenge.json 单独上传；题目可选 `tutorial_slug` 关联配套教程（双向入口），不关联的纯题目也合法（`.agents/skills/teaching-integration`）。
2. **内容只来源于管理员上传**：内容包 zip + 五步上传管线，禁止内置内容（`.agents/skills/content-package-spec`）。
3. **两档角色**：普通人(`student`) / 管理员(`admin`)，声明式中间件鉴权（`.agents/skills/rbac-model`）。
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
│   ├─ auth        登录 / JWT / 两档角色 RBAC                    │
│   ├─ content     教程包上传→解析→校验→入库→发布（五步管线）    │
│   ├─ challenge   Challenge/Flag/Hint 业务（题目独立资源）      │
│   ├─ judge       异步判题 worker（恒时比较、动态计分）          │
│   ├─ env         Lab 环境生命周期（docker 驱动 / noop 兜底）   │
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
- 两档角色：`student`（普通人，仅学习端）/ `admin`（管理员，管理端全部能力）；普通人前端不显示管理入口。
- 路由注册时声明所需角色，中间件统一执行；历史 `teacher` 启动迁移降为 `student`。

### 3.2 content — 教程包管线

教程包只含教程（文字 + 视频），题目定义不经此管线。五步管线 `Upload → Parse → Validate → Persist → Publish`，整体拒绝（禁止部分入库）：

1. Upload：接收 zip，校验大小与 MIME；
2. Parse：解析 manifest.yaml / topic.md / lesson.md（front matter 可选 `video` 字段）；
3. Validate：front matter 必填、难度枚举、slug 冲突；
4. Persist：写 ContentPackage/ContentVersion，状态 draft；
5. Publish：管理员显式发布；新版本 = 新 ContentVersion，旧版本可回滚。

状态机：`draft → published → archived`（详见 `.agents/skills/content-package-spec`）。

### 3.3 challenge — 题目域（独立资源）

- 题目以自有 `slug` 为主键入口，管理员经实训常用题目包 zip（`challenge.yml` + 容器文件）或兼容 challenge.json 上传：**upsert 即生效**（slug 重复 = 更新），不走 draft/publish 管线（版本化仍属教程包机制）。
- `category` 为自由分类 slug（训练场分组键）；`description_md` 承载背景与任务目标。
- Challenge 多态 `type` 列（v1：static / regex flag）。
- Flag 比较使用恒时比较（逐字符 XOR），防时序侧信道。
- `tutorial_slug` 可选关联教程 Room：题目详情给"配套教程"入口，Room 详情反查给"前往题目"入口；纯题目（不关联）合法。
- Hint 扣分解锁：详情页折叠；`POST .../hints/{id}/unlock` 写 `hint.unlocked` 事件，派生器扣分；未解锁正文不返回。

### 3.4 judge — 异步判题

- 提交接口只入队并返回 pending；worker 消费执行判定，写 `submission.judged` 事件。
- 动态计分：`value = f(solve_count)`，linear / logarithmic 衰减，下限 minimum。
- 远期：服务端自动判定（环境内检测点回调，参考 PortSwigger）。

### 3.5 env — Lab 环境

- `InstanceDriver`：Create（可注入 `FLAG` 等环境变量）/ Destroy；`ImageBuilder`：题目包 `image: .` 时 `docker build`。
- 单用户同时仅一个活跃容器；TTL 默认 1h，`POST .../instance/extend` 续期不重建。
- `flag_mode=instance`：启动时生成唯一 flag 写入 `LabInstance.flag_value` 并注入容器；判题只接受 running 实例上的值。
- Docker 驱动实现；未配置 daemon 时 noop（启动/构建返回明确错误）。

### 3.6 userprogress — 路径与进度

- LearningPath = 有序 Room ID 引用（PathItem），不复制内容。
- Progress 为事件派生物：Room 完成率、路径完成率、XP。

### 3.7 eventbus — 事件总线

- 进程内 append-only：`Append(event)` 落库 + 内存广播。
- 派生器（deriver）订阅事件 → 增量更新 ScoreRecord / Progress / 排行榜缓存。
- Phase 2 可替换为 Redis Stream / MQ，接口不变。

## 4. 数据模型（ER 概览）

```
User ─┬─ Role(global: student|admin)
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

1. **教程上传流**：admin 上传教程 zip → content 管线五步 → draft → admin 发布 → 对学生可见；
2. **题目上传流**：admin 上传 challenge.json → 校验 → upsert 即生效（无 draft/publish）；
3. **做题判定流**：student 在题目详情页提交 → 入队 pending → worker 恒时比较 → `submission.judged` 事件 → 派生器更新积分/进度/排行榜（题目关联教程时同步发 Room 通关事件）；
4. **鉴权流**：请求 → JWT 解析 → 角色中间件（student/admin）→ handler。

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
| `POST /content/packages` | admin | 上传教程包（进入管线） |
| `GET /content/packages/{id}` | admin | 包状态与版本 |
| `POST /content/packages/{id}/publish` | admin | 发布 |
| `POST /admin/challenges` | admin | 上传题目 JSON（upsert 即生效） |
| `GET /admin/challenges` | admin | 题目管理表格 |
| `DELETE /admin/challenges/{slug}` | admin | 删除题目（级联 flags/hints） |
| `POST /admin/users` / `PATCH /admin/users/{id}/roles` | admin | 用户与角色（student/admin） |

## 7. 技术选型

| 项 | 选型 | 理由 |
|---|---|---|
| 后端 | Go 1.22+ / net/http（标准库路由） | 零框架依赖起步，分层清晰 |
| ORM | gorm + SQLite | 与 yaklang 生态一致；repo 接口抽象便于迁移 |
| 认证 | golang-jwt/jwt/v5（HS256） | 成熟、无状态 |
| 前端 | Vue3 + TS + Vite + Pinia + Vue Router | 管理后台开发效率 |
| API 契约 | OpenAPI 3.0（手写 yaml） | 契约先行，前后端并行 |

## 8. 演进路线

- **Phase 1** ✅ 已完成：架构文档 + skill 体系 + 可编译骨架。
- **Phase 2** ✅ 已完成：最小闭环——真实登录、内容包上传与发布（版本化/回滚/归档）、Room 浏览、提交与异步判题（恒时比较 + 动态计分 + 持久 pending 队列）、事件派生积分/进度/排行榜。
- **Phase 3** 🔶 进行中：题目环境容器化已落地（docker CLI 驱动、单容器约束、TTL reaper、实例 flag、管理端容器总览）；**未做**：服务端自动判定（环境内检测点回调）、徽章与认证。
- **Phase 4** ⬜ 未开始：多人规模——SQLite → PostgreSQL、事件总线外置（Redis Stream/MQ）、排行榜按赛季切片。

## 9. 当前边界（明确不做）

- 服务端自动判定与徽章认证（Phase 3 剩余项，见上）。
- 事件总线为进程内实现：无持久消费位点，崩溃窗口靠启动 Replay 补偿（Phase 4 外置后移除）。
- 排行榜未按赛季切片；单机部署假设（docker 驱动仅绑 127.0.0.1）。
- 不引入 Casbin / Redis / K8s；数据层仍为 SQLite。
- JWT secret 与初始管理员密码存在开发默认值，生产必须以环境变量覆盖（启动时告警提醒）。
