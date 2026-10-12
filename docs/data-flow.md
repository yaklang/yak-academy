# yak-academy 关键数据流

本文档描述三条核心时序。参与方缩写：FE=前端、API=api 层、SVC=service 层、EB=eventbus、DB=SQLite。

## 1. 内容上传流（Content Pipeline）

```
FE(admin)          API                content SVC           repo            DB
   │  POST /content/packages (zip)    │                      │               │
   ├─────────────────────────────────→│                      │               │
   │                                  │ 1.Upload: 校验大小/MIME                  │
   │                                  │ 2.Parse: 解压→manifest→topics→rooms      │
   │                                  │   ├─ 解析失败 → 整体拒绝(422) ────────→│
   │                                  │ 3.Validate: front matter/难度/flag/slug │
   │                                  │ 4.Persist: Package+Version(draft) ───→ │
   │   201 {package_id, version, status:"draft"}             │               │
   │←─────────────────────────────────┤                      │               │
   │                                  │                                                      │
   │  POST /content/packages/{id}/publish (admin)           │               │
   ├─────────────────────────────────→│ draft→published ────────────────────→ │
   │   200 {status:"published"}         │  （学生端立即可见：GET /topics）        │
```

要点：
- 整体拒绝：任何一 Room/lab 解析或校验失败，整个包不入库（无部分发布）。
- 同名包再上传 = 新 ContentVersion；发布后旧版本可回滚（archived）。

## 2. 做题判定流（Event-Sourced Judging）

```
FE(student)        API                judge SVC             EB              DB
   │  POST /submissions {room_id, answer}                   │               │
   ├─────────────────────────────────→│ 写 submission(pending)               │
   │                                  ├─ Append(submission.created) ──────→│
   │   202 {submission_id, status:"pending"}                 │               │
   │←─────────────────────────────────┤                      │               │
   │                                  │ worker(异步消费)                       │
   │                                  │  ├─ flag 恒时比较(逐字符XOR)          │
   │                                  │  ├─ 动态计分 value=f(solve_count)      │
   │                                  │  ├─ submission → correct/incorrect    │
   │                                  │  └─ Append(submission.judged) ──────→│
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

## 3. RBAC 鉴权流（双层角色）

```
FE                API middleware                    handler            SVC
 │  GET /admin/users (Bearer JWT)                                      │
 ├────────────────→│ 1.解析 JWT → user_id/全局角色                        │
 │                 │ 2.路由元数据 RequireRole("admin")                    │
 │                 │   ├─ 全局 admin → 短路通过                            │
 │                 │   ├─ 全局不足 → 查课程级角色(CourseRole)               │
 │                 │   └─ 均不足 → 403 FORBIDDEN（禁止404掩盖）            │
 │                 ├───────────────────────────────────────────────────→ │
 │                 │                    3.属主规则(如"自己班级")在 SVC 内检查 │
 │   200 [users]   │                                                    │
 │←────────────────┤                                                    │
```

要点：
- 角色声明只在路由注册处（`RequireRole(...)`），handler 内禁止散落权限判断。
- 判定顺序：全局角色 → 课程级角色；admin 短路。
- 401（未登录/token 失效）与 403（角色不足）严格区分，统一错误信封 `{"error":{"code","message"}}`。

## 4. Hint 解锁流（附加）

```
POST /rooms/{id}/hints/{hintId}/unlock → 校验积分够扣 → 写 hint.unlocked 事件
→ 派生器扣减 ScoreRecord → 返回 hint 内容
```