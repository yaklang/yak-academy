# AWD 计分与回合结算

AWD 的得分事实全部写入 `awd_score_events`（append-only 账本，`idempotency_key` 唯一），
`awd_standings` 只能由 `AwdScoreDeriver` 从账本重建（先删后插），业务代码不得直接改写排名。

## 回合结算

管理员调用 `POST /api/v1/admin/competitions/{id}/awd/rounds/{rid}/settle` 结算一个回合：

| 来源 | 公式 | 账本幂等键 |
|---|---|---|
| SLA | `round(base_score × sla_weight/100 × 成功检查/总检查 × sla_weight_cfg)` | `sla:<roundID>:<teamServiceID>` |
| 攻击 | `round(attack_points / 该受害服务本轮不同攻击者数 × attack_weight_cfg)`，每个攻击者每服务每回合只计一次 | `attack:<roundID>:<teamServiceID>:<type>:<id>` |
| 防守 | `-round(defense_loss × defense_weight_cfg)`，每个受害服务每回合只扣一次 | `defense:<roundID>:<teamServiceID>` |
| Checkdown | `-round(checkdown_penalty × checkdown_weight_cfg)`，CHECK 失败每服务每回合只扣一次 | `checkdown:<roundID>:<teamServiceID>` |

- 检查动作：`check / put / get`；`status=ok` 记为成功，其余记为失败，SLA 按竞域单位服务分组统计。
- 权重来自 `awd_games.scoring_config`（JSON 对象），缺省均为 `1`；值 `0` 表示关闭该来源，负值按 `0` 处理。
- 总分 = 攻击分 + SLA 分 − 防守扣分 − checkdown 扣分，**下限 0**。
- 排名排序：`score` DESC → `attack_score` DESC → `sla_score` DESC → `competitor_id` ASC；同分并列名次。
- 未产生账本的参赛者（含尚未结算）以 0 分进入排名，保证参赛者始终可见。
- 重复结算幂等：账本冲突静默跳过（返回 `events=0`），排名重放结果一致。

## 最终排名（AWD 分 + 战报评分）

比赛结束后公布最终排名（管理端未结束时为预览，`finalized=false`）：

```
final_score = (final_awd_weight × awd_score / max_awd_score × 100
             + final_report_weight × report_score)
            / (final_awd_weight + final_report_weight)
```

- `max_awd_score` 为榜首 AWD 分；为 0 时 AWD 项按 0 计（纯粹按战报排名）。
- `final_awd_weight` / `final_report_weight` 来自 `awd_games.scoring_config`，默认 `70` / `30`，
  取值 `0..1000` 且不能同时为 0；两者按和归一，最终分仍在 `0..100`。
- `report_score` 取战报最后一条 rubric 评审分（未提交/未评审为 0）。
- 并列依次比较：`final_score` DESC → `report_score` DESC → 最后有效攻击判定时间 DESC → `competitor_id` ASC。
- 最终排名不落派生表，由 `awd_standings` + `awd_reports` + 有效攻击实时计算，避免与账本双写漂移。
- 权重必须在开赛前写入 `scoring_config` 并向参赛者公布；比赛开始后配置锁定（仅 pending 可改）。
