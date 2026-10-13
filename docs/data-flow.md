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
- 删除（DELETE /admin/challenges/{slug}）级联删 flags/hints。
- 题目包 zip 且 `image: .`：上传时 docker build 打 tag，失败则 422 不入库。
- `flag_mode=instance`：须有 env；flag 为含 `{token}` 的模板。

## 2b. 实例生命周期与续期

```
POST /challenges/{slug}/instance → creating → driver.Create(-e FLAG) → running + expire_at
POST /challenges/{slug}/instance/extend → 仅 running：expire_at = now+1h（不写 Event）
DELETE → destroy；reaper 按 expire_at 回收
```

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