package service

import (
	"context"
	"fmt"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// AwdRecoveryResult 启动恢复摘要（main 只记录日志；测试断言字段）。
type AwdRecoveryResult struct {
	RunningGames  int    `json:"running_games"`
	ResumedRounds []uint `json:"resumed_rounds"`
	JudgedAttacks int    `json:"judged_attacks"`
}

// Recover 进程启动恢复：AWD 回合与攻击队列都落在数据库，重启后补齐崩溃窗口。
//   - running 比赛中存在 pending 回合（已落库、检查未完成）：幂等补 flag、
//     只补跑缺失的 PUT/GET/CHECK，然后置 running 并刷新回合计时；
//   - 已是 running 的回合保留现场，不重复检查（避免重复检查污染 SLA 分母）；
//   - 顺手判定全部滞留 pending 攻击（worker 之后仍会周期扫描）。
func (s *AwdCompetitionService) Recover(ctx context.Context) (*AwdRecoveryResult, error) {
	games, err := s.awd.ListGamesByStatus(ctx, model.AwdGameRunning)
	if err != nil {
		return nil, err
	}
	result := &AwdRecoveryResult{RunningGames: len(games), ResumedRounds: []uint{}}
	for i := range games {
		game := games[i]
		err := s.withRoundLock(ctx, game.CompetitionID, func(ctx context.Context) error {
			round, err := s.awd.FindActiveRound(ctx, game.ID)
			if err != nil {
				if isNotFound(err) {
					return nil
				}
				return err
			}
			if round.Status != model.AwdRoundPending {
				return nil
			}
			if _, err := s.resumeRoundLocked(ctx, &game, round); err != nil {
				return err
			}
			result.ResumedRounds = append(result.ResumedRounds, round.ID)
			return nil
		})
		if err != nil {
			return result, fmt.Errorf("recover AWD game %d: %w", game.ID, err)
		}
	}
	judged, err := s.JudgePendingAttacks(ctx)
	result.JudgedAttacks = judged
	if err != nil {
		return result, err
	}
	return result, nil
}
