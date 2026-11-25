# yak-academy 关键数据流

本文档描述核心时序。参与方缩写：FE=前端、API=api 层、SVC=service 层、EB=eventbus、DB=SQLite（开发）/ PostgreSQL（生产，AWD 回合锁为 PG advisory lock）。

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
- `CHECKPOINT_URL = $ACADEMY_PUBLIC_URL/api/v1/checkpoints/<token>`（容器内经 host.docker.internal 回达宿主；Linux 由 `DockerDriver.Create` 的 `--add-host host-gateway` 注入解析，默认值开箱即用）。

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
## 6. 微信注册/登录流（唯一自助入口）

手机号/邮箱自助注册已下线；新账号只经微信 OAuth 创建。服务端只使用微信官方
网页授权接口，不引入第三方 OAuth 库。

```
FE(未登录)              API                    wechat SVC          repo/DB
   │  GET /auth/wechat/status                                        │
   ├──────────────────────────────────→│ enabled/mock/mode（未配置 → 前端  │
   │  200 {enabled,mock,mock_admin,mode}│ 禁用按钮并提示联系管理员）          │
   │←──────────────────────────────────┤                                   │
   │  GET /auth/wechat/start?redirect=/profile                         │
   ├──────────────────────────────────→│ 随机 nonce + HMAC-SHA256 签名       │
   │                                  │ state 会话（nonce/redirect/exp）    │
   │  Set-Cookie: academy_wechat_state=…；HttpOnly; SameSite=Lax; 10min │
   │  200 {authorize_url}               │ official_account: oauth2/authorize │
   │←──────────────────────────────────┤ open_platform:  qrconnect           │
   │  浏览器整页跳转微信授权页（携带 appid/redirect_uri/state）           │
   │  微信回调 → 302 前端 /login?code=…&state=…                         │
   │  POST /auth/wechat/callback {code, state}                          │
   ├──────────────────────────────────→│ 无 state Cookie → 401              │
   │                                  │ 签名/过期/nonce 恒时比对失败 → 401  │
   │                                  │ 立即清除 state Cookie（一次性）      │
   │                                  │ Exchange: access_token → userinfo  │
   │                                  │  ├─ openid 命中 → 复用账号          │
   │                                  │  ├─ unionid 命中 → 复用并补 openid  │
   │                                  │  └─ 未命中 → 建 student 账号        │
   │                                  │     （昵称生成用户名，冲突加后缀；   │
   │                                  │      PasswordHash 为空）───────────→│ User
   │  200 {token, expires_at, redirect}（JWT 只在响应体，绝不进 URL）    │
   │←──────────────────────────────────┤                                   │
   │  前端保存 token → /me → 跳转 redirect（仅站内路径，默认 /learn）      │
```

要点：
- **两种官方模式**：`official_account`（公众号网页授权 `snsapi_userinfo`，可用免费测试号联调）与 `open_platform`（开放平台网站应用扫码 `snsapi_login`）；`ACADEMY_WECHAT_MODE` 选择，空配置 = 未启用。
- **Mock 仅本地/E2E**：`ACADEMY_WECHAT_MOCK=1` 直接回跳登录页并建号，不访问微信；`ACADEMY_STRICT=1` 时该开关必须关闭。`ACADEMY_WECHAT_REDIRECT_URL` 必须与微信后台配置的授权回调域一致，指向**前端登录页**。
- **Mock 管理员入口（显式提权，默认关闭）**：`ACADEMY_WECHAT_MOCK_ADMIN_USERNAME` 绑定一个既有 admin 账号后，登录页多出「以管理员身份模拟登录」；`start?as=admin` 在签名 state 中记录该意图，回调直接登录绑定账号（再次校验其确为 admin），不创建学员账号。未配置、非 Mock、账号不存在/被降级时一律拒绝；生产严格模式禁止 Mock。
- **state 防 CSRF**：随机 nonce + HMAC-SHA256 签名会话存 HttpOnly + SameSite=Lax Cookie，10 分钟有效；回调时恒时比对并立即清除（重放/伪造一律 401）。JWT 只经 JSON 响应体返回，避免 token 泄漏进日志/Referer。
- **账号归并**：openid 优先（唯一索引，空值不占用），unionid 兜底（同主体多应用互通）；用户名由微信昵称生成，重名追加稳定后缀，昵称不足 3 字符时补后缀而非丢弃。
- **无密码账号**：`PasswordHash` 为空，`GET /me` 返回 `password_set=false`；密码登录 401、本人改密 422，资料页隐藏改密入口并显示「微信登录」标签。管理员重置密码后即可密码登录。
- 历史邮箱/手机号账号继续用户名 + 密码登录；邮箱/手机号字段仅为历史数据保留，不参与登录。

### 6.1 找回密码流（仅历史账号）

```
FE(未登录)         API                reset SVC             repo            DB
   │  登录页「找回账号」→ POST /auth/password-reset/code {email|phone}       │
   ├─────────────────────────────────→│ 目标必须已注册（422 未注册，         │
   │                                  │ 先于限频告知；微信/竞赛批量号没有    │
   │                                  │ 绑定值 → 引导管理端重置）            │
   │                                  │ 限频(60s/10min≤3，按 purpose 独立)  │
   │                                  │ genCode ───────────────────────────→│ SmsCode(purpose=password_reset)
   │   200 {ok}（限频 → {ok, retry_after}；本地演示回显附 code）              │
   │←─────────────────────────────────┤                      │               │
   │  POST /auth/password-reset {email|phone, code, new_password}            │
   ├─────────────────────────────────→│ 账号存在性(FindByEmail/Phone)→ 核销   │
   │                                  │ (purpose=password_reset 的一码，     │
   │                                  │  恒时比较 + MarkUsed 条件更新)       │
   │                                  │ bcrypt 改密 + TokenInvalidBefore   │
   │                                  │ 推进(签发秒+1) ───────────────────→│ User
   │   200 {username, account_code}（忘记用户名的历史账号凭邮箱一并找回）     │
   │←─────────────────────────────────┤                      │               │
   │  （不自动登录：回登录页用新密码登录；原 token 已全量失效）               │

管理端路径（微信注册/竞赛批量建号等无邮箱手机号的账号）：admin 用户管理行内「重置密码」
  POST /admin/users/{id}/reset-password {password} → 不校验旧密码，同语义
  改密 + token 全量失效（预填平台初始密码 123456 可改），写管理审计。
```

要点：
- 验证码**用途隔离**（`SmsCode.purpose` = password_reset）：注册用途已随自助注册下线，历史 register 码不可用于重置。
- 邮箱与手机号验证码分别按目标落库，互不通用（填 email 必须用邮箱码）。
- 改密失效语义与本人改密（`POST /auth/change-password`）、管理员重置完全一致：`TokenInvalidBefore` 推进，已签发 token 全量吊销。

## 7. 竞赛生命周期与入场锁定流

```
FE(admin)          API                competition SVC        repo            DB
   │  POST /admin/competitions {name, kind, start, end}      │               │
   ├─────────────────────────────────→│ 校验(起止/名称/kind 二选一，创建后   │
   │                                  │ 不可改) → 落库 ─────────────────────→│ Competition
   │  绑题 POST .../challenges {slugs|JSON}                   │               │
   ├─────────────────────────────────→│ JSON 走 content 管线导入后绑定；      │
   │                                  │ running 禁止解绑（保榜单现场）         │
   │  导入参赛者 POST .../participants {teams|individuals}    │               │
   ├─────────────────────────────────→│ 仅邀请赛；条目=账号编码或用户名，       │
   │                                  │ 只引用既有账号（不自动建号）；          │
   │                                  │ 未命中 → missing[] 回报（不阻断）      │
   │                                  │ 每人生成唯一邀请码 ─────────────────→│ Participant
   │                                  │ 站内邀请邮件（失败仅标记可重发）─────→│ Mail
   │   200 {participants[], missing[]} │                      │               │
   │←─────────────────────────────────┤                      │               │
   │  一键生成 POST .../generate-users {prefix, count,        │               │
   │                                team_size, password}     │               │
   ├─────────────────────────────────→│ 仅邀请赛；批量建号(默认密码 123456，  │
   │                                  │ 编码统一分配)并导入为参赛者；          │
   │                                  │ team_size>0 自动编入「第 n 队」──────→│ User/Team/Participant
   │   200 {accounts[](初始密码仅本次), teams[], participants[]}               │
   │←─────────────────────────────────┤                      │               │

FE(学生)           API                competition SVC
   │  竞赛广场 GET /competitions（仅公开赛，含本人报名状态）    │               │
   │  报名 POST /competitions/{id}/register（幂等；邀请赛 422）│               │
   ├─────────────────────────────────→│ 个人参赛（不投递邮件，报名即可入场）──→│ Participant
   │←─────────────────────────────────┤                      │               │

FE(参赛者)         API middleware                competition SVC
   │  邮件邀请 → /competitions/enter/{id}?code=                │               │
   │  POST /competitions/{id}/enter {code}     │              │
   ├─────────────────────────────────→│ 邀请码或公开赛已报名免码；             │
   │                                  │ 仅 running 有效（scheduled/ended → 422）
   │                                  │ Participant.entered_at 落库           │
   │   200 {entered:true, locked:<邀请赛>}                                │
   │←─────────────────────────────────┤                                      │
   │  邀请赛参赛者此后访问平台接口（/topics、/challenges...）                │
   ├─────────────────────────────────→│ 锁定中间件：已入场且 running 的邀请赛   │
   │   403 LOCKED（重定向竞赛首页）    │ 公开赛不锁、管理员豁免；ended 自动解锁 │
   │←─────────────────────────────────┤                                      │
```

要点：入场锁定只作用于邀请赛（公开赛报名入场可随时返回平台）；管理员进入任何竞赛都不受锁定约束；比赛结束（ended）自动解锁。

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

## 9. AWD 回合、攻击与结算流

```
FE(admin)          API                 AWD SVC                 Docker/DB
   │  POST .../awd/rounds                   │                     │
   ├───────────────────────────────────────→│ 竞赛级回合锁         │
   │                                        │ 1.CreateRound(pending) ─→ DB
   │                                        │ 2.GenerateFlags(HMAC) ──→ DB
   │                                        │ 3.PUT/GET/CHECK 每靶机 ─→ 容器
   │                                        │   检查明细/incident ────→ DB
   │                                        │ 4.Round→running + 计时 ─→ DB
   │  200 {round,flags,checks,failed}       │                     │
   │←───────────────────────────────────────┤                     │

FE(参赛者)         API                 AWD SVC             worker
   │ POST .../awd/attacks {flag}            │                  │
   ├───────────────────────────────────────→│ 预校验(存在/过期/自攻/首杀)
   │                                        │ CreateAttack(pending) + 事件
   │  202 {status:"pending"}                │                  │
   │                                        │        worker 轮询 pending
   │                                        │        条件更新 accepted/rejected
   │                                        │        + awd.attack.judged 事件

FE(admin)          API                 AWD SVC                 DB
   │ POST .../rounds/{id}/settle            │                     │
   ├───────────────────────────────────────→│ 回合锁 → 聚合 checks/attacks
   │                                        │ SLA/攻击/防守/checkdown
   │                                        │ AwdScoreEvent(idempotency_key) → DB
   │                                        │ AwdScoreDeriver 重放账本 → AwdStanding
   │  200 {events, standings}               │                     │
```

要点：
- 回合先在 `pending` 落库，检查完成后才 `running`：崩溃后启动 `Recover` 补 flag、只补缺失检查（已完成的检查不重跑，避免污染 SLA 分母）并刷新计时；运行中的回合保留现场。
- 开回合/结算/恢复共用竞赛级锁：PostgreSQL 会话级 advisory lock（跨进程），SQLite 进程内键控互斥。攻击判定不做全局锁，靠 `UPDATE ... WHERE status='pending'` 影响行数保证恰好一次。
- flag 值只用于 checker 写入与比对；检查输出中的 flag 脱敏，API/导出不下发 flag 明文。
- `AwdStanding` 只由 `AwdScoreDeriver` 从 append-only `AwdScoreEvent` 重建；结算可重复调用，`idempotency_key` 唯一保证不重复计分。
- 运行中的回合/攻击/检查是运营事实表，与 Event 双写（攻击/战报审计）；回合与检查明细不写 Event，避免把高频运营数据混入学生事件流。

## 10. 考试作答、评分与成绩派生流

```
FE(参赛者)         API                 Exam SVC                DB
   │ POST /exam/attempts                    │                     │
   ├───────────────────────────────────────→│ 单次尝试；expires=min(start+duration, end)
   │                                        │ exam.attempt.started 事件
   │  200 {paper, attempt, expires_at}      │                     │
   │ PUT .../answers/{qid} {answer}         │ 自动保存（过期懒判定） │
   │ POST .../submit                        │ 客观题即时评分        │
   │                                        │ attempt→submitted + graded 事件
   │                                        │ ExamDeriver 重建 ExamStanding（只由事件触发）
   │ POST .../questions/{qid}/code           │ 走 judge exam scope  │
   │                                        │ submission.judged(exam_attempt_id)
   │                                        │ → 回写 code 分 + graded 事件

worker（到期扫描）→ FinalizeAttempt(expired, auto) → graded 事件
```

要点：
- 时间统一按 UTC 存储和比较（SQLite 字符串比较对 `+08:00`/`+00:00` 敏感，服务层 `nowUTC` 统一）。
- 试卷/试题开赛前可编辑，开赛后锁定；作答接口只接受本人 current attempt。
- 编程题判分复用 yak 引擎，但 `exam` scope 不进入 XP/CTF 竞赛积分；只有 `ExamDeriver` 消费 graded 事件重建成绩。

## 11. 赛务：名册 / 晋级 / 证书流

```
FE(admin) → POST /admin/competitions/{id}/roster {entries}   # CSV 解析后提交
          → 引用既有账号（用户名/账号编码），写 CompetitionProfile + Participant

FE(admin) → POST .../qualification/preview {source,mode,threshold}
          → QualificationRankingSource(CTF/Exam/AWD)
             ├─ CTF: individual/team leaderboard
             ├─ Exam: terminal results（分数/用时）
             └─ AWD: FinalRanking（团队展开为成员，要求 game ended）
          → 返回候选人（并列边界一并入选）
          → POST .../qualification/apply → QualificationRule/Entry + 导入参赛者（邀请码）

FE(admin) → POST .../certificates/preview
          → CertificateRankingSource(CTF/Exam/AWD) + Profile 姓名
          → POST .../certificates → 规则+接收人快照（唯一 code）→ competition.certificate.issued
public    → GET /certificates/{code} → 仅快照（无内部用户 ID）
```

要点：
- 晋级只能应用到**未开始的邀请赛**；同一来源竞赛对同一目标只应用一次；AWD 团队证书按队伍接收，获奖名单 CSV 不含验证码。
- 证书规则/名称/接收人快照只增不改，重复签发按标题拒绝；验证码随机且唯一，验证页无需登录。
- 实名名册仅管理端可见；证书接收人姓名取签发时快照，不再随用户资料变化。
