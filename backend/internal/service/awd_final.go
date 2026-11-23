package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// AWD 最终排名：AWD 回合得分（按榜首归一化到 100）× 权重 + 战报评分 × 权重。
// 权重来自 awd_games.scoring_config 的 final_awd_weight / final_report_weight
// （默认 70/30，按两者之和归一），必须在开赛前配置并公布。
// 并列依次比较：战报分、最后有效攻击时间、竞域单位 ID。

// AwdFinalWeights 最终排名权重（未归一的原始配置）。
type AwdFinalWeights struct {
	AwdWeight    float64 `json:"awd_weight"`
	ReportWeight float64 `json:"report_weight"`
}

// AwdFinalStandingView 最终排名条目：final_score 保留两位小数。
type AwdFinalStandingView struct {
	Rank           int                     `json:"rank"`
	CompetitorType model.AwdCompetitorType `json:"competitor_type"`
	CompetitorID   uint                    `json:"competitor_id"`
	CompetitorName string                  `json:"competitor_name,omitempty"`
	AwdScore       int                     `json:"awd_score"`
	ReportScore    int                     `json:"report_score"`
	FinalScore     float64                 `json:"final_score"`
	LastAttackAt   *time.Time              `json:"last_attack_at,omitempty"`
	LastEventAt    *time.Time              `json:"last_event_at,omitempty"`
}

// AwdFinalRankingView 最终排名视图；finalized=false 表示比赛尚未结束（管理端预览）。
type AwdFinalRankingView struct {
	CompetitionID uint                   `json:"competition_id"`
	Status        model.AwdGameStatus    `json:"status"`
	Finalized     bool                   `json:"finalized"`
	MaxAwdScore   int                    `json:"max_awd_score"`
	Weights       AwdFinalWeights        `json:"weights"`
	Items         []AwdFinalStandingView `json:"items"`
}

// FinalRanking 管理端最终排名（比赛未结束时返回预览，finalized=false）。
func (s *AwdCompetitionService) FinalRanking(ctx context.Context, competitionID uint) (*AwdFinalRankingView, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	standings, err := s.Standings(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	reports, err := s.awd.ListReports(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	attacks, err := s.awd.ListAcceptedAttacksByGame(ctx, game.ID)
	if err != nil {
		return nil, err
	}
	return buildAwdFinalRanking(game, standings, reports, attacks), nil
}

// ParticipantFinalRanking 参赛者最终排名：仅比赛结束后开放。
func (s *AwdCompetitionService) ParticipantFinalRanking(ctx context.Context, competitionID, userID uint) (*AwdFinalRankingView, error) {
	_, game, _, err := s.requireAwdParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGameEnded {
		return nil, fmt.Errorf("%w: final ranking is available after the game ends", ErrValidation)
	}
	return s.FinalRanking(ctx, competitionID)
}

func buildAwdFinalRanking(
	game *model.AwdGame, standings []AwdStandingView,
	reports []model.AwdReport, attacks []model.AwdAttack,
) *AwdFinalRankingView {
	weights := parseAwdScoringWeights(game.ScoringConfig)
	awdWeight, reportWeight := weights.finalWeights()
	totalWeight := awdWeight + reportWeight

	maxAwd := 0
	for _, row := range standings {
		if row.Score > maxAwd {
			maxAwd = row.Score
		}
	}
	reportScores := make(map[string]int, len(reports))
	for _, report := range reports {
		reportScores[awdCompetitorKey(report.CompetitorType, report.CompetitorID)] = report.Score
	}
	lastAttacks := make(map[string]time.Time, len(attacks))
	for _, attack := range attacks {
		at := attack.SubmittedAt
		if attack.JudgedAt != nil {
			at = *attack.JudgedAt
		}
		key := awdCompetitorKey(attack.AttackerType, attack.AttackerID)
		if at.After(lastAttacks[key]) {
			lastAttacks[key] = at
		}
	}

	items := make([]AwdFinalStandingView, 0, len(standings))
	for _, row := range standings {
		key := awdCompetitorKey(row.CompetitorType, row.CompetitorID)
		awdNormalized := 0.0
		if maxAwd > 0 {
			awdNormalized = float64(row.Score) / float64(maxAwd) * 100
		}
		reportScore := reportScores[key]
		final := 0.0
		if totalWeight > 0 {
			final = (awdWeight*awdNormalized + reportWeight*float64(reportScore)) / totalWeight
		}
		final = math.Round(final*100) / 100
		var lastAttack *time.Time
		if at, ok := lastAttacks[key]; ok {
			value := at
			lastAttack = &value
		}
		items = append(items, AwdFinalStandingView{
			CompetitorType: row.CompetitorType, CompetitorID: row.CompetitorID,
			CompetitorName: row.CompetitorName, AwdScore: row.Score,
			ReportScore: reportScore, FinalScore: final,
			LastAttackAt: lastAttack, LastEventAt: row.LastEventAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].FinalScore != items[j].FinalScore {
			return items[i].FinalScore > items[j].FinalScore
		}
		if items[i].ReportScore != items[j].ReportScore {
			return items[i].ReportScore > items[j].ReportScore
		}
		var ti, tj time.Time
		if items[i].LastAttackAt != nil {
			ti = *items[i].LastAttackAt
		}
		if items[j].LastAttackAt != nil {
			tj = *items[j].LastAttackAt
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return items[i].CompetitorID < items[j].CompetitorID
	})
	for i := range items {
		items[i].Rank = i + 1
	}
	return &AwdFinalRankingView{
		CompetitionID: game.CompetitionID, Status: game.Status,
		Finalized:   game.Status == model.AwdGameEnded,
		MaxAwdScore: maxAwd,
		Weights:     AwdFinalWeights{AwdWeight: awdWeight, ReportWeight: reportWeight},
		Items:       items,
	}
}
