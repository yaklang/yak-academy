# yak-academy 关键数据流

本文档描述核心时序。参与方缩写：FE=前端、API=api 层、SVC=service 层、EB=eventbus、DB=SQLite。

## 1. 教程上传流（Content Pipeline）

```
FE(admin)          API                content SVC           repo            DB
   │  POST /content/packages (zip)    │                      │               │
   ├─────────────────────────────────→│                      │               │
   │                                  │ 1.Upload: 校验大小/MIME                  │
   │                                  │ 2.Parse: 解压→manifest→topics→rooms      │
   │                                  │   ├─ 解析失败 → 整体拒绝(422) ────────→│
   │                                  │ 3.Validate: front matter/难度/slug       │
   │                                  │ 4.Persist: Package+Version(draft) ───→ │
   │   201 {package_id, version, status:"draft"}             │               │
   │←─────────────────────────────────┤                      │               │
   │                                  │                                                      │
   │  POST /content/packages/{id}/publish (admin)           │               │
   ├─────────────────────────────────→│ draft→published ────────────────────→ │
   │   200 {status:"published"}         │  （学生端立即可见：GET /topics）        │
```

要点：
- 教程包只含教程（文字 + 视频）；题目是独立资源，经 challenge.json 上传（见流 2）。
- 整体拒绝：任何一 Room 解析或校验失败，整个包不入库（无部分发布）。
- 同名包再上传 = 新 ContentVersion；发布后旧版本可回滚（archived）。

## 2. 题目上传流（Challenge JSON Upsert）

```
FE(admin)          API                content/challenge SVC   repo            DB
   │  POST /admin/challenges (JSON: 单对象或数组)             │               │
   ├─────────────────────────────────→│ 解析 JSON → 逐题校验(slug/枚举/flag    │
   │                                  │  regex/env 成对)                      │
   │                                  │  ├─ 单题失败 → 整体拒绝(422) ────────→│
   │                                  │ └─ UpsertChallenge(按 slug) ────────→│
   │   200 {imported, slugs}           │  （upsert 即生效，无 draft/publish）   │
   │←─────────────────────────────────┤                      │               │
```

要点：
- slug 重复 = 更新（upsert），上传即对学生可见；版本化机制只属于教程包。
- 删除（DELETE /admin/challenges/{slug}）：存在通关记录 → 409 拒绝（submission 事件 append-only，删除已解题目会造成悬挂积分）；无通关记录 → 先销毁该题全部运行中容器，再级联删 flags/hints/lab_instances 行。
- 题目包 zip 且 `image: .`：上传时 docker build 打 tag，失败则 422 不入库。
- `flag_mode=instance`：须有 env；flag 为含 `{token}` 的模板。

## 2b. 实例生命周期与续期

```
POST /challenges/{slug}/instance → creating → driver.Create(-e FLAG, -e CHECKPOINT_TOKEN, -e CHECKPOINT_URL) → running + expire_at
POST /challenges/{slug}/instance/extend → 仅 running：expire_at = now+1h（不写 Event）
DELETE → destroy；reaper 按 expire_at 回收
```

要点：
- `checkpoint_token` 每实例随机生成，存 `LabInstance.checkpoint_token`（API JSON 隐藏）；实例销毁 token 即失效。
- `CHECKPOINT_URL = $ACADEMY_PUBLIC_URL/api/v1/checkpoints/<token>`（容器内经 host.docker.internal 回达宿主）。

## 2c. 检测点自动判定流（容器回调，参考 PortSwigger）

```
题目容器内 checker   API                judge SVC            EB              DB
   │  POST /checkpoints/{token}（无 JWT，token 即能力凭证）   │               │
   ├────────────────────────→│ FindByCheckpointToken           │               │
   │                         │  ├─ 未命中/实例非活跃 → 404      │               │
   │                         │  └─ 命中活跃实例                 │               │
   │                         ├─ 写 submission(pending, 哨兵答案) →             │
   │                         ├─ Append(submission.created, source=checkpoint)→│
   │   202 {status:"pending"} │                                 │               │
   │←────────────────────────┤                                 │               │
   │                         │ worker：哨兵答案短路判对（跳过 flag 比对），       │
   │                         │ 复用防重复通关/动态计分/room.completed 管线        │
```

要点：
- 哨兵答案未导出：`POST /submissions` 拒绝该值，用户无法伪造环境判定。
- 重复回调安全：走 HasCorrect 幂等护栏，重复判对 0 分。
- 前端在环境运行中轮询题目详情 `solved` 字段感知自动判定结果。

## 2d. 结算模型（分值一次结算）

- **题目可重复完成/重复提交 flag**：已通关后仍可提交，判对返回 `correct` 且 `score=0`（不重复计分、不重发 `room.completed`）；前端不禁用提交框，仅提示「已结算过」。
- **分值以题目为单位、只结算一次**：首次判对按动态分值入账（ScoreRecord 派生）。
- **提示扣费按账号永久**：`hint.unlocked` 事件扣分一次，解锁状态永久（不随重复完成退回、不重复扣费）。

## 3. 做题判定流（Event-Sourced Judging）

```
FE(student)        API                judge SVC             EB              DB
   │  POST /submissions {challenge_slug, answer}             │               │
   ├─────────────────────────────────→│ 写 submission(pending)               │
   │                                  ├─ Append(submission.created) ──────→│
   │   202 {submission_id, status:"pending"}                 │               │
   │←─────────────────────────────────┤                      │               │
   │                                  │ worker(异步消费)                       │
   │                                  │  ├─ flag 恒时比较(逐字符XOR)          │
   │                                  │  ├─ 动态计分 value=f(solve_count)      │
   │                                  │  ├─ submission → correct/incorrect    │
   │                                  │  └─ Append(submission.judged) ──────→│
   │                                  │  └─ 题目关联教程(tutorial_slug)时       │
   │                                  │     Append(room.completed) ─────────→│
   │                                  │                      │ 派生器消费:   │
   │                                  │                      │  ├─ ScoreRecord(积分)
   │                                  │                      │  ├─ Progress(进度)
   │                                  │                      │  └─ 排行榜缓存
   │  GET /submissions/{id} (轮询/SSE) │                      │               │
   ├─────────────────────────────────→│ 读 submission → 200 {status, score}  │
```

要点：
- 提交请求内**禁止**同步判定；恒时比较防时序侧信道。
- 积分/进度/排行榜**只能**由派生器从事件更新，业务代码禁止直写。
- Event 表只 INSERT；"修正"以新事件表达。
- 纯题目（无 tutorial_slug）不产生 Room 侧进度事件。

## 4. RBAC 鉴权流（两档角色）

```
FE                API middleware                    handler            SVC
 │  GET /admin/users (Bearer JWT)                                      │
 ├────────────────→│ 1.解析 JWT → user_id / 全局角色                      │
 │                 │ 2.路由元数据 RequireRole("admin")                    │
 │                 │   ├─ admin → 通过                                    │
 │                 │   └─ student（普通人）→ 403 FORBIDDEN                 │
 │                 ├───────────────────────────────────────────────────→ │
 │   200 [users]   │                                                    │
 │←────────────────┤                                                    │
```

要点：
- 仅 `student`（普通人）与 `admin`（管理员）；普通人前端不渲染管理入口。
- 角色声明只在路由注册处，handler 内禁止散落权限判断。
- 401（未登录/token 失效）与 403（角色不足）严格区分，统一错误信封 `{"error":{"code","message"}}`。

## 5. Hint 解锁流

```
POST /challenges/{slug}/hints/{hintId}/unlock
  → 校验 hint 属于该题、积分够扣（已解锁则幂等返回）
  → 写 hint.unlocked 事件（append-only）
  → 派生器扣减 ScoreRecord（cost>0）
  → 返回 hint 正文 + 当前积分
题目详情 GET /challenges/{slug}：未解锁 hint 的 content 置空，仅返回 id/title/cost/unlocked。
```
## 6. 自助注册流（手机验证码）

```
FE(未登录)         API                register SVC          repo            DB
   │  POST /auth/sms/code {phone}     │                      │               │
   ├─────────────────────────────────→│ 手机号格式(1[3-9]×10位)校验           │
   │                                  │ 60s 限频（最近一码 CreatedAt）        │
   │                                  │ 10min 窗口 ≤3 条                      │
   │                                  │ 手机号已注册 → 422（不发码）           │
   │                                  │ genCode(crypto/rand 拒绝采样) ─────→│ SmsCode 落库(5min TTL)
   │                                  │ DevSmsSender 投递（写服务日志）       │
   │   200 {ok:true}（限频 → 200 {ok, retry_after}，不报错）                  │
   │←─────────────────────────────────┤                      │               │
   │  POST /auth/register {phone, code, username, password}  │               │
   ├─────────────────────────────────→│ 参数校验(码6位/用户名3-64/密码≥6)     │
   │                                  │ 取该手机号最新一码：                  │
   │                                  │  ├─ 无码/已核销/过期 → 422            │
   │                                  │  ├─ 恒时比较(逐字符XOR)失败 → 422     │
   │                                  │ MarkUsed(条件更新 used=0→1) ───────→│
   │                                  │  └─ 0 行 = 并发已核销 → 422（一次性） │
   │                                  │ 用户名/手机号查重 → 422               │
   │                                  │ bcrypt 建号(角色固定 student) ──────→│
   │   200 {token, expires_at}（建号即登录）                                  │
   │←─────────────────────────────────┤                      │               │
```

要点：
- 手机号是注册凭据与找回依据，**不参与登录**（登录仍为用户名 + 密码）。
- 验证码比对与核销分离：比对失败不消耗；核销是条件更新，并发同码注册只有一个成功。
- 角色固定 `student`，接口无角色入参（禁止自选）。
- 限频对客户端表现为 200 + retry_after（与真实短信通道行为一致），前端据此倒计时。

## 7. 竞赛生命周期与入场锁定流

```
FE(admin)          API                competition SVC        repo            DB
   │  POST /admin/competitions {name, start, end}            │               │
   ├─────────────────────────────────→│ 校验(起止/名称) → 落库 ─────────────→│ Competition
   │  绑题 POST .../challenges {slugs|JSON}                   │               │
   ├─────────────────────────────────→│ JSON 走 content 管线导入后绑定；      │
   │                                  │ running 禁止解绑（保榜单现场）         │
   │  导入参赛者 POST .../participants {teams|users}          │               │
   ├─────────────────────────────────→│ 用户不存在 → 自动建号(随机密码) ────→│ User
   │                                  │ 每人生成唯一邀请码 ─────────────────→│ Participant
   │                                  │ 站内邀请邮件（失败仅标记可重发）─────→│ Mail
   │   200 {created_accounts(仅本次), participants[]}          │               │
   │←─────────────────────────────────┤                      │               │

FE(参赛者)         API middleware                competition SVC
   │  邮件邀请 → /competitions/enter/{id}?code=                │               │
   │  POST /competitions/{id}/enter {code}     │              │               │
   ├─────────────────────────────────→│ 仅 running 有效（scheduled/ended → 422）
   │                                  │ Participant.entered_at 落库           │
   │   200 {locked:true}               │                                      │
   │←─────────────────────────────────┤                                      │
   │  此后访问任何平台接口（/topics、/challenges...）                        │
   ├─────────────────────────────────→│ 锁定中间件：已入场且 running           │
   │   403 LOCKED（重定向竞赛首页）    │ 管理员不受约束；ended 自动解锁         │
   │←─────────────────────────────────┤                                      │
```

## 8. 竞赛做题与双榜派生流

```
FE(参赛者)         API                judge SVC             EB              DB
   │  POST /competitions/{id}/submissions {slug, answer}    │               │
   ├─────────────────────────────────→│ 与平台提交同一管线：入队 pending      │
   │                                  │ worker 判定(恒时比较/动态计分)         │
   │                                  │  ├─ Append(submission.judged) ─────→│
   │                                  │  └─ 竞赛提交：detectCheat            │
   │                                  │     提交串命中他队运行实例 flag       │
   │                                  │     → CheatWarning（仅管理端警告）──→│
   │                                  │                      │ 派生器消费:   │
   │                                  │                      │  └─ 竞赛首通 → CompetitionScore
   │                                  │                      │     （AddScore 幂等，冗余队伍快照）
   │  GET /competitions/{id}/leaderboard?scope=team|individual                │
   ├─────────────────────────────────→│ 从 CompetitionScore 只读聚合          │
```

要点：
- 竞赛提交复用平台 judge 管线（异步、恒时比较、动态计分、防重复通关），无第二套判定。
- CompetitionScore 为事件派生表：competition 服务只读聚合双榜，**禁止**直写（event-sourcing-rules）。
- 反作弊只告警不打断：cross_team_flag 等类型写 CheatWarning，管理端竞赛详情「反作弊」页浏览。
- WP 仅 ended 后开放：按题目分条提交/覆盖（markdown + 可选 zip ≤20MB），管理端可下载附件。
