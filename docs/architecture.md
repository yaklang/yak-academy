# yak-academy 架构设计文档

> 版本：v0.4（Phase 2/3 完成 + 竞赛系统 + 自助注册）
> 本文档是项目的完整设计源。约束的执行摘要位于 `.agents/skills/`，两者冲突时以本文档为准。

## 1. 背景与对标分析

yak-academy 是一个安全实训平台，提供安全教程（文字+视频）、独立实战题目、进度与积分排行。设计对标调研结论：

| 参考对象 | 借鉴点 | 不借鉴点 |
|---|---|---|
| PortSwigger Web Security Academy | 训练场按分类分组展示题目；题目详情页承载描述/提示/环境/提交；服务端自动判定（远期）；徽章/认证分层激励 | 商业变现部分 |
| 实训常用判题约定 | challenges/flags/hints/submissions 数据模型；恒时比较；动态计分衰减函数 | 比赛制 teams 模式、第三方插件体系（第一版） |
| TryHackMe | 教题关联：教程与题目互指而非互嵌；XP 游戏化 | 靶机常驻 IaaS 成本模型；教题强制同库耦合 |
| CTFtime | 排行榜按赛季切片；题目页聚合题解（远期） | 纯 UGC 聚合模式（无判定） |

**核心架构决策**（均在 skill 中有对应纠偏规则）：

1. **教程与题目两类独立资源**：教程包只含教程（文字+视频），题目用固定格式 challenge.json 单独上传；题目可选 `tutorial_slug` 关联配套教程（双向入口），不关联的纯题目也合法（`.agents/skills/teaching-integration`）。
2. **内容只来源于管理员上传**：内容包 zip + 五步上传管线，禁止内置内容（`.agents/skills/content-package-spec`）。**唯一例外**：官方内置训练集（配套教程 + 题目，基于 yaklang vulinbox 靶场引擎）随后端二进制 go:embed 分发，但仅在管理员于管理端显式「一键导入」（`POST /admin/challenges/builtin`）时写入内容库——题目经同一上传管线 upsert，教程直接 upsert Topic/Room（重复导入即更新）——内容生效仍是管理员动作，vulinbox 二进制为 AGPL-3.0 的上游独立程序，平台不引用其源码（镜像内附 NOTICE，见 `backend/internal/service/builtin/vulinbox/image/`）。
3. **两档角色**：普通人(`student`) / 管理员(`admin`)，声明式中间件鉴权（`.agents/skills/rbac-model`）。
4. **事件溯源**：积分/进度/排行榜从 append-only 事件派生，判题异步（`.agents/skills/event-sourcing-rules`）。
5. **契约先行**：openapi.yaml 是接口唯一契约源（`.agents/skills/api-contract`）。
6. **分层单向依赖**：api → service → repo → model（`.agents/skills/project-conventions`）。

## 2. 总体架构

```
┌────────────────────────────────────────────────────────────┐
│ 表现层  frontend/  Vue3 + TS + Vite                          │
│   学生端：Topic/Room 教程浏览、题库（题目分组列表+详情）、       │
│           进度、排行榜、竞赛（入场/做题/双榜/WP）              │
│   管理端：资源管理（教程/题目/容器/竞赛管理）                  │
│           （用户与角色不设管理页：两档模型下角色是账号固有     │
│            属性，建号入口在竞赛「导入参赛者」）                │
├────────────────────────────────────────────────────────────┤
│ 服务层  backend/internal/api  REST + JWT + RBAC 中间件        │
│   ├─ auth        登录 / JWT / 两档角色 RBAC / 自助注册        │
│   ├─ content     教程包上传→解析→校验→入库→发布（五步管线）    │
│   ├─ challenge   Challenge/Flag/Hint 业务（题目独立资源）      │
│   ├─ judge       异步判题 worker（恒时比较、动态计分、反作弊）  │
│   ├─ env         Lab 环境生命周期（docker 驱动 / noop 兜底）   │
│   ├─ userprogress 进度跟踪 + 事件派生器（含竞赛积分）          │
│   ├─ competition 竞赛域（赛程/参赛者/入场锁定/双榜/WP）        │
│   └─ eventbus    进程内 append-only 事件总线 + 派生器           │
├────────────────────────────────────────────────────────────┤
│ 数据层  SQLite + gorm（repo 层接口抽象，可平滑迁 PostgreSQL）    │
│   业务库：用户/内容/题目/事件/派生表/竞赛                       │
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
- **自助注册（手机验证码）**：登录页提供注册入口，流程为 `POST /auth/sms/code`（发码，公开）→ `POST /auth/register`（凭 code 建号，公开，角色固定 student，禁止自选角色）。验证码 6 位、5 分钟有效、核销一次性；同手机号 60 秒限频、10 分钟内最多 3 次；`SmsSender` 为接口抽象，默认实现为开发投递器（验证码写服务日志，供本地联调与 E2E 断言），生产以环境变量切换真实通道（第一版不接第三方）。手机号作为注册凭据与找回依据，不参与登录（登录仍为用户名 + 密码）；重复手机号注册拒绝。

### 3.2 content — 教程包管线

教程包只含教程（文字 + 视频），题目定义不经此管线。五步管线 `Upload → Parse → Validate → Persist → Publish`，整体拒绝（禁止部分入库）：

1. Upload：接收 zip，校验大小与 MIME；
2. Parse：解析 manifest.yaml / topic.md / lesson.md（front matter 可选 `video` 字段）；
3. Validate：front matter 必填、难度枚举、slug 冲突；
4. Persist：写 ContentPackage/ContentVersion，状态 draft；
5. Publish：管理员显式发布；新版本 = 新 ContentVersion，旧版本可回滚。

状态机：`draft → published → archived`（详见 `.agents/skills/content-package-spec`）。

**删除**（`DELETE /content/packages/{id}`，管理员显式清理，不可恢复）：删除包记录与全部版本，并按**历代版本 Payload 的 slug 并集**清扫内容表（applyPayload 只 upsert 不删行，v2 移除的教程也须清掉）；同时清空指向被删 Room 的 `challenges.tutorial_slug`（题目本体保留）并删除被删 Room 的 `progresses` 行。事件表 append-only 不动；配套守卫：派生器处理 `room.completed` 时 Room 已不存在则跳过（`SetRoomLookup` 注入查询），启动重放不会复活已删进度。内置训练集不走内容包记录，无删除入口（重新一键导入即恢复）。

### 3.3 challenge — 题目域（独立资源）

- 题目以自有 `slug` 为主键入口，管理员经实训常用题目包 zip（`challenge.yml` + 容器文件）或兼容 challenge.json 上传：**upsert 即生效**（slug 重复 = 更新），不走 draft/publish 管线（版本化仍属教程包机制）。
- `category` 为自由分类 slug（题库分组键）；`description_md` 承载背景与任务目标。
- `bank`（tutorial/ctf/acm）：题库页三 tab（教程题目/CTF 题目/ACM 题目）的分组键。**教程题目 tab 专属官方内置题集**——上传路径仅接受 ctf/acm（显式 tutorial 一律 422），缺省时新建默认 ctf、重传保留原值；内置导入统一注入 tutorial（历史误入 tutorial 的上传题由迁移改判 ctf）。`is_builtin` 为导入来源标记——仅内置导入置 true（`MarkBuiltin`），普通上传/编辑 upsert 不触碰（学生端 `GET /challenges` 按 bank/builtin 过滤）。
- Challenge 多态 `type` 列（v1：static / regex flag）。
- Flag 比较使用恒时比较（逐字符 XOR），防时序侧信道。
- `tutorial_slug` 可选关联教程 Room：题目详情给"配套教程"入口，Room 详情反查给"前往题目"入口；纯题目（不关联）合法。
- Hint 扣分解锁：详情页折叠；`POST .../hints/{id}/unlock` 写 `hint.unlocked` 事件，派生器扣分；未解锁正文不返回。
- **内置训练集（vulinbox，配套教程 + 题目）**：教程与题目定义、基础镜像 Dockerfile 随后端二进制分发（`service/builtin/vulinbox/`，教程在 `tutorials/` 子目录），管理端一键导入（`ImportBuiltinChallenges`）：Docker 可用时先构建共享镜像 `yak-academy/vulinbox:latest`（构建失败整体 422、零写入，与题目包语义一致），再导入配套教程（`importBuiltinTutorials`：复用 `parseFiles` 解析与静态校验，绕过包状态机直接 upsert Topic/Room，幂等），最后复用 `UploadChallenges` 逐题 upsert——**教程先、题目后**，题目落库时 `tutorial_slug` 指向的 Room 已存在，双向入口即刻生效；noop 驱动下跳过构建仅导入定义。**一教程对一题**：`challenges.json` 每题带 `tutorial_slug`，11 篇教程（`yakit-` 前缀 slug 命名空间，避免与上传教程包碰撞）分属 4 个专题，覆盖 Web Fuzzer/MITM/Codec 等核心 Yakit 功能。全题集共用单容器（vulinbox 单进程多靶机、按路径路由），`flag_mode=shared`（vulinbox 不消费 FLAG 环境变量）；SSRF 两题为 regex 判定（flag{uuid}），其余为基于靶场确定性种子数据的人工设计 static 判定。

### 3.4 judge — 异步判题

- 提交接口只入队并返回 pending；worker 消费执行判定，写 `submission.judged` 事件。
- 动态计分：`value = f(solve_count)`，linear / logarithmic 衰减，下限 minimum。
- 服务端自动判定（检测点回调，参考 PortSwigger）：实例启动时生成 `checkpoint_token`，随 `CHECKPOINT_TOKEN`/`CHECKPOINT_URL` 注入容器；题目内 checker 达成目标后 POST `CHECKPOINT_URL`（公开接口，token 即能力凭证）。服务端以哨兵答案入队，worker 走与手动提交相同的管线判过计分（防重复通关、动态计分、room.completed 复用）；提交接口拒绝哨兵值，用户无法伪造环境判定。

### 3.5 env — Lab 环境

- `InstanceDriver`：Create（可注入 `FLAG` 等环境变量）/ Destroy；`ImageBuilder`：题目包 `image: .` 时 `docker build`。
- 单用户同时仅一个活跃容器；TTL 默认 1h，`POST .../instance/extend` 续期不重建。
- `flag_mode=instance`：启动时生成唯一 flag 写入 `LabInstance.flag_value` 并注入容器；判题只接受 running 实例上的值。
- 每实例生成 `checkpoint_token`（`LabInstance.checkpoint_token` 列，API JSON 隐藏）：注入 `CHECKPOINT_TOKEN`/`CHECKPOINT_URL`（`ACADEMY_PUBLIC_URL` 前缀，默认 `host.docker.internal`）；实例销毁 token 即失效。
- Docker 驱动实现；未配置 daemon 时 noop（启动/构建返回明确错误）。

### 3.6 userprogress — 进度

- Progress 为事件派生物：Room 完成率、XP。

### 3.7 eventbus — 事件总线

- 进程内 append-only：`Append(event)` 落库 + 内存广播。
- 派生器（deriver）订阅事件 → 增量更新 ScoreRecord / Progress / CompetitionScore / 排行榜缓存。
- Phase 2 可替换为 Redis Stream / MQ，接口不变。

### 3.8 competition — 竞赛域

- **赛程状态机**：`scheduled → running → ended`（派生自 start_time/end_time，非落库列）；运行中的竞赛只允许延后 end_time 与改描述（收紧起止时间会破坏已入场的锁定语义）。
- **题目绑定**：三种入参统一入口——题目 JSON（对象/数组，复用 content 管线导入后绑定）、`{"slugs": [...]}` 绑定既有题目、zip 题目包；单题失败整体拒绝，已绑定幂等跳过；running 竞赛禁止解绑（榜单已产生，撤题破坏现场）。
- **参赛者与队伍**：批量导入按用户名引用，不存在自动创建账号（随机初始密码仅本次响应返回）；每人生成唯一邀请码并投递站内邀请邮件（投递失败仅标记，不回滚，管理端可重发）；已参赛幂等跳过并沿用原邀请码；队伍归属以首次导入为准。
- **入场与锁定**：凭邀请码入场仅 running 竞赛有效；入场后不可退出，锁定状态 = `(已入场 且 running)`，比赛结束自动解锁。中间件层面强制：已锁定的普通用户访问平台任何非竞赛接口 → 403，前端路由守卫做同语义 UX 重定向（服务端是兜底权威）；管理员不受锁定约束。
- **竞赛做题**：题目/容器/提交都在 `/competitions/{id}/...` 命名空间下复用训练场能力（容器按竞赛隔离管理），判定走与平台完全相同的 judge 管线（异步、恒时比较、动态计分）。
- **反作弊**：judge 在竞赛提交上执行跨队检测——提交串命中「本竞赛其他队伍成员」的运行中实例 flag（cross_team_flag）等类型写 CheatWarning，只产生管理端警告，不打断提交与判定。
- **双榜**：队伍榜/个人榜均从 CompetitionScore 派生表只读聚合（派生行由事件派生器在竞赛首通时写入，AddScore 幂等）；竞赛服务不触碰派生表写入（event-sourcing-rules）。
- **WP（Writeup）**：仅竞赛 ended 后开放；按题目分条提交/覆盖（文字 markdown + 可选脚本 zip ≤20MB），一人一题一份（队伍内各自提交）；管理端可浏览正文与下载 zip 附件。

## 4. 数据模型（ER 概览）

```
User ─┬─ Role(global: student|admin)
      ├─ Submission ─→ Challenge
      ├─ Progress(派生) ─→ Room
      ├─ SmsCode(phone, code, expires_at, used)   # 注册验证码（一次性核销）
      └─ Participant ─→ Competition ─→ Team       # 参赛/组队

ContentPackage ─→ ContentVersion(draft|published|archived)   # 教程包
Topic ─→ Room(lesson.md + 可选 video)
Challenge(slug 主键, category, difficulty, description_md,     # 独立题目资源
          tutorial_slug → Room 可选关联)
       ─┬─ Flag(type: static|regex)
         ├─ Hint(title/content/cost)
         └─ Submission(user, answer, status: pending|correct|incorrect)
Event(id, user_id, type, payload, created_at)   # append-only
ScoreRecord(派生) / LabInstance(user×challenge, status, expire_at)

Competition(name, start_time, end_time → status 派生)            # 竞赛域
  ─┬─ CompetitionChallenge(绑定题目+分值快照)
   ├─ Team(name) ← Participant(user, invite_code, entered_at, last_ip)
   ├─ CheatWarning(user, team, type, reason)                     # 反作弊警告
   ├─ Writeup(user×challenge, content_md + zip 附件)             # ended 后提交
   └─ CompetitionScore(派生: user/team × challenge 首通分值)      # 双榜数据源
```

字段级定义见 `backend/internal/model/`（gorm 实体即数据字典）。

## 5. 关键数据流

三条主时序（带泳景图见 `docs/data-flow.md`）：

1. **教程上传流**：admin 上传教程 zip → content 管线五步 → draft → admin 发布 → 对学生可见；
2. **题目上传流**：admin 上传 challenge.json → 校验 → upsert 即生效（无 draft/publish）；
3. **做题判定流**：student 在题目详情页提交 → 入队 pending → worker 恒时比较 → `submission.judged` 事件 → 派生器更新积分/进度/排行榜（题目关联教程时同步发 Room 通关事件；竞赛提交另派生 CompetitionScore 并做跨队反作弊检测）；
4. **鉴权流**：请求 → JWT 解析 → 角色中间件（student/admin）→ handler；
5. **注册流**：登录页发码（限频/窗口校验 → SmsCode 落库 → 开发投递器写日志）→ 凭码建号（核销一次性 → 建号角色固定 student → 签发登录 token）；
6. **竞赛流**：admin 建赛/绑题/导参赛者（自动建号+邀请码+站内邀请）→ 参赛者凭码入场（锁定至比赛结束）→ 竞赛内做题走同一 judge 管线 → 双榜从 CompetitionScore 聚合 → ended 解锁后按题提交 WP（泳道图见 `docs/data-flow.md`）。

## 6. API 契约总览

完整契约见 `docs/openapi.yaml`。第一版关键接口：

| 方法与路径 | 角色 | 说明 |
|---|---|---|
| `POST /auth/login` | 公开 | 登录换 JWT |
| `POST /auth/sms/code` | 公开 | 注册发验证码（限频返回 retry_after） |
| `POST /auth/register` | 公开 | 凭验证码建号（角色固定 student）并签发 token |
| `GET /me` | student | 当前用户与角色 |
| `GET /topics` / `GET /topics/{slug}` | student | 主题列表/详情（含 Room 列表） |
| `GET /topics/{slug}/rooms/{roomSlug}` | student | Room 教程详情（文字+视频，含关联题目 slug） |
| `GET /challenges` | student | 题库题目列表（bank/builtin 过滤 + category 分组展示） |
| `GET /challenges/{slug}` | student | 题目详情（描述/提示/教程关联） |
| `POST /challenges/{slug}/instance` | student | 启动题目容器环境 |
| `DELETE /challenges/{slug}/instance` | student | 销毁环境 |
| `POST /submissions` | student | 提交答案（challenge_slug 键，返回 pending） |
| `GET /submissions/{id}` | student | 查询判定结果 |
| `GET /progress` | student | 个人进度 |
| `GET /leaderboard` | student | 排行榜（派生数据） |
| `GET /badges` | student | 徽章目录与获得状态（计算型读模型） |
| `POST /checkpoints/{token}` | 公开（token 能力鉴权） | 环境检测点自动判定回调（容器内 checker 调用） |
| `POST /content/packages` | admin | 上传教程包（进入管线） |
| `GET /content/packages/{id}` | admin | 包状态与版本 |
| `POST /content/packages/{id}/publish` | admin | 发布 |
| `DELETE /content/packages/{id}` | admin | 删除内容包（历代版本写入的教程一并清扫，不可恢复；事件历史保留） |
| `POST /admin/challenges` | admin | 上传题目 JSON（upsert 即生效） |
| `POST /admin/challenges/builtin` | admin | 一键导入内置训练集（配套教程 + 题目；重复导入 = 更新） |
| `GET /admin/challenges` | admin | 题目管理表格 |
| `DELETE /admin/challenges/{slug}` | admin | 删除题目（级联 flags/hints） |
| `POST /admin/users` / `PATCH /admin/users/{id}/roles` | admin | 用户建号/改角色（管理页已下线；API 保留供竞赛批量建号与账号运维） |
| `POST /admin/competitions` 等 13 个 `/admin/competitions/*` | admin | 竞赛管理：CRUD/题目绑定/参赛者批量导入（自动建号+邀请码）/容器总览/反作弊警告/WP 浏览与 zip 下载/双榜 |
| `GET /competitions/me` | student | 本人参赛与锁定状态（路由守卫轮询） |
| `POST /competitions/{id}/enter` | student | 凭邀请码入场（running 限定，入场后锁定至结束） |
| `/competitions/{id}/view`、`.../challenges/{slug}/instance`、`.../submissions`、`.../leaderboard`、`.../writeups*` | 参赛者 | 竞赛内做题闭环：题目列表/容器/提交/双榜/WP 提交与覆盖（ended 后） |

## 7. 技术选型

| 项 | 选型 | 理由 |
|---|---|---|
| 后端 | Go 1.22+ / net/http（标准库路由） | 零框架依赖起步，分层清晰 |
| ORM | gorm + SQLite | 与 yaklang 生态一致；repo 接口抽象便于迁移 |
| 认证 | golang-jwt/jwt/v5（HS256） | 成熟、无状态 |
| 前端 | Vue3 + TS + Vite + Pinia + Vue Router | 管理后台开发效率 |
| API 契约 | OpenAPI 3.0（手写 yaml） | 契约先行，前后端并行 |

## 8. 测试策略（分层金字塔）

| 层 | 位置 | 手段 | 运行 |
|---|---|---|---|
| 纯函数单测 | service 内 | 表驱动 + 时钟注入 | `go test` |
| 服务/仓库单测 | service / repo 同包 | 真实内存 SQLite + 真实 repo，禁止 mock repo | `go test -race ./...` |
| 契约守卫 | api 包 | contract_test 双向比对 openapi.yaml ↔ 注册路由（集合、角色、公开性），漂移即红 | 同上 |
| Docker 门控集成 | service | 真实 daemon（`ACADEMY_TEST_DOCKER=1` 门控，无 Docker 自动 Skip） | CI backend-docker job |
| 并发竞态 | service / judge | 20 goroutine 并发提交，恰好一次结算 | `go test -race` |
| 前端单测 | frontend | vitest 纯逻辑 | `npm test` |
| E2E 旅程 | frontend/e2e | Playwright：双 webServer 自管理（后端 :18080 预编译单二进制 + vite :15173）、整跑全新 DB、管理 API 播种；13 条用户旅程（认证/教程/题目全链路/可见性/账号生命周期），`E2E_DOCKER=1` 追加容器旅程 | `npm run e2e` |

E2E 基建约定（README「环境搭建与镜像加速」详述）：浏览器默认用系统 Chrome/Edge
channel（零下载），串行 worker（共享 DB 与账号），改全局状态的用例自恢复。
E2E 已抓出两个生产 bug（release_at 跨时区字典序泄漏未发布题、改密后 1 秒内
重登被拒），是回归防线的重要组成部分。

## 9. 演进路线

- **Phase 1** ✅ 已完成：架构文档 + skill 体系 + 可编译骨架。
- **Phase 2** ✅ 已完成：最小闭环——真实登录、内容包上传与发布（版本化/回滚/归档）、Room 浏览、提交与异步判题（恒时比较 + 动态计分 + 持久 pending 队列）、事件派生积分/进度/排行榜。
- **Phase 3** ✅ 已完成：题目环境容器化（docker CLI 驱动、单容器约束、TTL reaper、实例 flag、管理端容器总览）、服务端自动判定（检测点回调 + 哨兵答案复用判题管线）、徽章（计算型读模型，进度/积分/路径的实时投影，零新表零新事件）。
- **Phase 3.5** ✅ 已完成：竞赛系统（赛程状态机、题目绑定、参赛者批量导入自动建号、邀请码入场与锁定、竞赛内做题复用 judge 管线、跨队反作弊、CompetitionScore 派生双榜、ended 后 WP 提交）与自助注册（手机验证码，开发投递器通道）。
- **Phase 3.6** ✅ 已完成：内置训练集（基于 yaklang vulinbox 靶场引擎的 11 题精选集，管理端一键导入/更新，共享基础镜像 + shared/regex 判定）；扩展为教程 + 题目：4 专题 11 篇 Yakit 教程一教程对一题（`tutorial_slug` 1:1 注入，教程先于题目落库、直接 upsert 无 draft/publish）。
- **Phase 4** ⬜ 未开始：多人规模——SQLite → PostgreSQL、事件总线外置（Redis Stream/MQ）、排行榜按赛季切片。

## 10. 当前边界（明确不做）

- 事件总线为进程内实现：无持久消费位点，崩溃窗口靠启动 Replay 补偿（Phase 4 外置后移除）。
- 排行榜未按赛季切片；单机部署假设（docker 驱动仅绑 127.0.0.1）。
- 徽章为计算型读模型：不落库、不产生事件；如未来需要"颁发时间/稀有度"再演进为事件派生表。
- 短信通道仅有开发投递器（验证码写服务日志）；生产接真实通道前注册不可对外开放。竞赛 WP 的 zip 附件落盘本地目录（`ACADEMY_WRITEUPS_DIR`），未做对象存储。
- 不引入 Casbin / Redis / K8s；数据层仍为 SQLite。
- JWT secret 与初始管理员密码存在开发默认值，生产必须以环境变量覆盖（启动时告警提醒）。
- 内置训练的 vulinbox 基础镜像构建依赖官方 CDN（`oss-qn.yaklang.com`，离线部署需手工预构建镜像）且仅 amd64；二进制 sha256 锁定在 Dockerfile，上游更新会使导入 422（防靶机行为漂移，更新题集时重新锁定）。
