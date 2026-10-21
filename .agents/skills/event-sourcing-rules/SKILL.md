---
name: event-sourcing-rules
description: yak-academy 事件溯源数据流约束。涉及积分、进度、排行榜、做题提交、判题流程、事件表设计时必须加载，防止直接改写派生数据破坏审计与重放能力。
---

# 事件溯源规则（强制）

## 硬性原则

1. **Event 表 append-only**：只允许 INSERT，**禁止**任何 UPDATE / DELETE（包括"修正数据"场景——修正本身就是一条新事件）。
2. **派生表只读于事件**：Progress（进度）、ScoreRecord（积分）、排行榜全部是事件的物化视图，**禁止**任何业务代码直接改写派生表。刷新 = 重放或增量消费事件。**唯一例外**：`ProgressRepo.DeleteByRoomSlugs`（内容包删除时清理被删教程的进度行，见 content-package-spec「删除」）——必须与 deriver 的 Room 存在性守卫（`SetRoomLookup`，room.completed 对已删 Room 跳过）成对存在，否则启动重放会复活已删进度；新增类似例外必须先修订本 skill 并说明重放一致性依据。
3. **判题必须异步**：提交接口只做"入队 + 返回 pending"，判定由 worker 完成。**禁止**在提交的 HTTP 请求路径里同步判定。

## 事件类型（第一版枚举，新增需更新本 skill）

| type | 触发 |
|---|---|
| `submission.created` | 用户提交答案（flag 明文 / yak 源码） |
| `submission.judged` | worker 完成判定（payload 含 correct 与得分；编程题另含 `verdict`: ac/wa/tle/re/internal_error） |
| `hint.unlocked` | 用户扣分解锁提示 |
| `room.completed` | Room 通关 |
| `lab.instance.started / stopped` | 题目环境生命周期 |

每个事件必带：`user_id / type / payload(JSON) / created_at`。

> 容器生命周期另有 `instance_logs` 表（append-only 审计视图，含失败原因 detail），
> 与 Event 表是**双写而非主从**：Event 服务全局事件溯源，instance_logs 是 env 域
> 管理端展示视图。改容器状态时两张表都要写，禁止只写一张。

## 判题数据流（唯一合法路径）

```
POST 提交 → 写 submission + submission.created 事件 → 返回 pending
worker 消费 → 判定（flag 题：比对；编程题：CodeEngine 逐组运行 testdata）→ 写 submission.judged 事件
派生器消费事件 → 更新 ScoreRecord/Progress → 广播排行榜变更
```

- **flag 题**（type=flag）：比对目标见下表；恒时比较，禁止 == 比较。
- **编程题**（type=code）：判定结论写 `Submission.verdict`（ac/wa/tle/re/internal_error），`status` 仍为 pending/correct/incorrect——积分/排行派生管线对两类题完全一致（verdict 只影响前端展示与 AC 判定语义）。逐组 lazy 判定（首个失败即停），输出比对 token 级空白不敏感。引擎由 `ACADEMY_YAK_BIN` 门控，未配置 = internal_error（不是 error，是合法 verdict，仍走事件流）。
- **编程题提交守卫**：源码 ≤128KiB；同一 (user, challenge) 已有 pending 判定 → 422（防并发重复入队）。

### Flag 比对来源（flag_mode）

| flag_mode | 比对目标 |
|---|---|
| `shared`（默认） | Challenge 级 Flag 表 |
| `instance` | 当前用户该题 **running** 实例的 `LabInstance.flag_value`（销毁后须重新启动） |

- 实例 Flag 在 `EnsureInstance` 生成并注入容器环境变量 `FLAG`，**不经 Event 表**（属实例运营态）。
- TTL 续期只更新 `LabInstance.expire_at`，**不写 Event**（非积分/进度派生）。

## 动态计分

`value = f(solve_count)`，内置 linear / logarithmic 衰减，下限 minimum。计分结果只通过 `submission.judged` 事件进入派生表。

## 正反例

✅ 正确：排行榜接口查派生表；刷新任务从 Event 重放聚合。
❌ 错误：通关时直接 `UPDATE score_records SET value = value + 100`。
❌ 错误：判题逻辑写在 handler 里同步返回 correct/incorrect。
