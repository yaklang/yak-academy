---
name: event-sourcing-rules
description: yak-academy 事件溯源数据流约束。涉及积分、进度、排行榜、做题提交、判题流程、事件表设计时必须加载，防止直接改写派生数据破坏审计与重放能力。
---

# 事件溯源规则（强制）

## 硬性原则

1. **Event 表 append-only**：只允许 INSERT，**禁止**任何 UPDATE / DELETE（包括"修正数据"场景——修正本身就是一条新事件）。
2. **派生表只读于事件**：Progress（进度）、ScoreRecord（积分）、排行榜全部是事件的物化视图，**禁止**任何业务代码直接改写派生表。刷新 = 重放或增量消费事件。
3. **判题必须异步**：提交接口只做"入队 + 返回 pending"，判定由 worker 完成。**禁止**在提交的 HTTP 请求路径里同步判定。

## 事件类型（第一版枚举，新增需更新本 skill）

| type | 触发 |
|---|---|
| `submission.created` | 用户提交答案 |
| `submission.judged` | worker 完成判定（payload 含 correct 与得分） |
| `hint.unlocked` | 用户扣分解锁提示 |
| `room.completed` | Room 通关 |
| `lab.instance.started / stopped` | 题目环境生命周期 |

每个事件必带：`user_id / type / payload(JSON) / created_at`。

## 判题数据流（唯一合法路径）

```
POST 提交 → 写 submission + submission.created 事件 → 返回 pending
worker 消费 → flag 比对（恒时比较，禁止 == 比较）→ 写 submission.judged 事件
派生器消费事件 → 更新 ScoreRecord/Progress → 广播排行榜变更
```

## 动态计分

`value = f(solve_count)`，内置 linear / logarithmic 衰减，下限 minimum。计分结果只通过 `submission.judged` 事件进入派生表。

## 正反例

✅ 正确：排行榜接口查派生表；刷新任务从 Event 重放聚合。
❌ 错误：通关时直接 `UPDATE score_records SET value = value + 100`。
❌ 错误：判题逻辑写在 handler 里同步返回 correct/incorrect。
