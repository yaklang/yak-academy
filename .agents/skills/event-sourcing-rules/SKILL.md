---
name: event-sourcing-rules
description: yak-academy 事件溯源数据流约束。涉及积分、进度、排行榜、做题提交、判题流程、事件表设计时必须加载，防止直接改写派生数据破坏审计与重放能力。
---

# 事件溯源规则（强制）

## 硬性原则

1. **Event 表 append-only**：只允许 INSERT，**禁止**任何 UPDATE / DELETE（包括"修正数据"场景——修正本身就是一条新事件）。
2. **派生表只读于事件**：Progress（进度）、ScoreRecord（积分）、排行榜、`ExamStanding`、`AwdStanding` 全部是事实的物化视图，**禁止**任何业务代码直接改写派生表。刷新 = 重放或增量消费事件。`ExamStanding` 由 `ExamDeriver` 消费 `exam.attempt.graded` 重建；`AwdStanding` 由 `AwdScoreDeriver` 重放 append-only `AwdScoreEvent` 重建。**唯一例外**：`ProgressRepo.DeleteByRoomSlugs`（内容包删除时清理被删教程的进度行，见 content-package-spec「删除」）——必须与 deriver 的 Room 存在性守卫（`SetRoomLookup`，room.completed 对已删 Room 跳过）成对存在，否则启动重放会复活已删进度；新增类似例外必须先修订本 skill 并说明重放一致性依据。
3. **判题必须异步**：提交接口只做"入队 + 返回 pending"，判定由 worker 完成。**禁止**在提交的 HTTP 请求路径里同步判定。
4. **AWD 得分账本独立于 Event**：`AwdScoreEvent` 是 AWD 域的 append-only 账本（`idempotency_key` 唯一），不是 `Event` 表；回合结算只允许写入账本，再由 `AwdScoreDeriver` 重建排名。`awd.attack.*` / `awd.report.*` 是运营审计事件，不参与排名重放。禁止在结算路径直接 UPDATE `AwdStanding` 或手工补写账本跳过 `idempotency_key`。

## 事件类型（第一版枚举，新增需更新本 skill）

| type | 触发 |
|---|---|
| `submission.created` | 用户提交答案（flag 明文 / yak 源码） |
| `submission.judged` | worker 完成判定（payload 含 correct 与得分；编程题另含 `verdict`: ac/wa/tle/re/internal_error；考试编程题另含 `exam_attempt_id`，全局派生器跳过 XP/竞赛积分） |
| `hint.unlocked` | 用户扣分解锁提示 |
| `room.completed` | Room 通关 |
| `lab.instance.started / stopped` | 题目环境生命周期 |
| `awd.attack.submitted` | AWD 攻击提交入队（运营审计；payload 含 competition/attack/round/attacker/victim） |
| `awd.attack.judged` | AWD worker 完成攻击判定（accepted/rejected + reason） |
| `awd.report.submitted` | AWD 赛后代战报提交（Markdown/zip 更新） |
| `awd.report.reviewed` | AWD 战报 rubric 评审完成（score/comment） |
| `exam.attempt.started` | 考试开始作答（服务端计时，payload 含 competition/paper/attempt/expires_at） |
| `exam.attempt.submitted` | 考试交卷（手动或到期自动；payload 含 attempt/auto_submitted） |
| `exam.attempt.graded` | 考试评分完成（客观题即时 + 编程题 judge 回写；ExamStanding 由此重放） |
| `competition.certificate.issued` | 赛后证书批量签发（payload 含规则/级别/数量；证书快照只增不改） |

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
- **编程题**（type=code）：判定结论写 `Submission.verdict`（全过 = ac，否则首个失败组的 wa/tle/re/internal_error），`status` 仍为 pending/correct/incorrect——积分/排行派生管线对两类题完全一致（verdict 只影响前端展示与 AC 判定语义）。**逐组全跑**（每组结论独立，全跑后取首个失败 verdict），输出比对 token 级空白不敏感。**部分得分制**：数据组配分（全 0 = 未配置，全有全无）时部分通过 = status incorrect 但 score>0，逐组明细写 `Submission.groups` 并随 `submission.judged` 事件 payload `groups` 附带；**派生器不累计部分分**（`correct=false` 事件行直接跳过），通关与总分结算仍以全过首通为准——防重复刷部分分虚增 XP/竞赛榜。引擎由 `ACADEMY_YAK_BIN` 门控，未配置 = internal_error（不是 error，是合法 verdict，仍走事件流）。
- **编程题提交守卫**：源码 ≤128KiB；同一 (user, challenge) 已有 pending 判定 → 422（防并发重复入队）。
- **自测运行（动态执行框）不进事件流**：`/challenges/{slug}/judge/runs` 是运营态（学生本地验证，非判定）——不写 Submission、不写 Event、不计分，票据仅进程内存（重启即清）。这是设计意图而非遗漏：自测无限次、无审计/重放诉求，混入事件流会污染积分重放。异步票据模型（202 + 轮询）与判题一致，引擎执行在请求路径外。

### Flag 比对来源（flag_mode）

| flag_mode | 比对目标 |
|---|---|
| `shared`（默认） | Challenge 级 Flag 表 |
| `instance` | 当前用户该题 **running** 实例的 `LabInstance.flag_value`（销毁后须重新启动） |

- 实例 Flag 在 `EnsureInstance` 生成并注入容器环境变量 `FLAG`，**不经 Event 表**（属实例运营态）。
- TTL 续期只更新 `LabInstance.expire_at`，**不写 Event**（非积分/进度派生）。

## AWD 得分账本（独立事实流）

```
回合结算（管理员/引擎） → 聚合 AwdCheck/AwdAttack
  → 写 AwdScoreEvent[]（source=sla|attack|defense|checkdown|report|manual，
     idempotency_key 唯一，重复结算影响行数 0）
  → AwdScoreDeriver 全量重放该竞赛账本 → ReplaceStandings(AwdStanding)
```

- 结算可重复调用，结果必须幂等；新增得分来源 = 新增 `source` + 唯一幂等键，禁止直接改账本行。
- 攻击提交/判定写 `awd.attack.submitted/judged` 事件（审计）；回合检查明细写 `AwdCheck`（运营事实表），两者都不驱动 `AwdStanding`。
- 最终排名由 `AwdStanding` + `AwdReport.Score` + accepted attacks 计算，是读模型，不落派生表。

## 考试成绩派生（事件驱动）

```
开始作答 → exam.attempt.started
交卷/到期 → exam.attempt.submitted
评分完成 → exam.attempt.graded（客观题即时；编程题由 submission.judged(exam_attempt_id) 回写后写）
ExamDeriver 消费 graded → 重建该竞赛 ExamStanding（全量替换）
```

- 全局 deriver 对带 `exam_attempt_id` 的 `submission.judged` 跳过 XP/CFT 竞赛积分；考试分数只由 ExamService 写 attempt/answer，再经事件派生。
- 到期自动交卷与手动交卷共用同一 Finalize 路径（条件更新，0 行 = 已被处理），禁止重复评分/重复发事件。

## 动态计分

`value = f(solve_count)`，内置 linear / logarithmic 衰减，下限 minimum。计分结果只通过 `submission.judged` 事件进入派生表。

## 正反例

✅ 正确：排行榜接口查派生表；刷新任务从 Event 重放聚合。
❌ 错误：通关时直接 `UPDATE score_records SET value = value + 100`。
❌ 错误：判题逻辑写在 handler 里同步返回 correct/incorrect。
