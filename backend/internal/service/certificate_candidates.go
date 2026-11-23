package service

import (
	"context"
	"fmt"
	"math"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 证书候选人：由各赛制域按自己的榜单口径提供，证书域只做筛选与落快照。
// 这样 CTF/AWD/Exam 互不引用，同时证书规则对三种赛制行为一致。
type CertificateCandidate struct {
	RecipientKind string // user / team
	UserID        uint
	TeamID        uint
	Username      string
	Name          string
	Rank          int
	Score         int
}

type CertificateRankingSource interface {
	CertificateCandidates(ctx context.Context, competitionID uint) ([]CertificateCandidate, error)
}

// CertificateCandidates CTF 个人榜候选（含零分参赛者；证书按人签发）。
func (s *CompetitionService) CertificateCandidates(ctx context.Context, competitionID uint) ([]CertificateCandidate, error) {
	board, err := s.Leaderboard(ctx, competitionID, "individual")
	if err != nil {
		return nil, err
	}
	items, _ := board["items"].([]TeamLeaderboardEntry)
	candidates := make([]CertificateCandidate, 0, len(items))
	for _, item := range items {
		username := ""
		if len(item.Members) > 0 {
			username = item.Members[0]
		}
		if item.UserID == 0 {
			continue
		}
		candidates = append(candidates, CertificateCandidate{
			RecipientKind: "user", UserID: item.UserID, Username: username,
			Name: username, Rank: item.Rank, Score: item.Score,
		})
	}
	return candidates, nil
}

// CertificateCandidates 考试已交卷者（成绩单排名与导出同一口径）。
func (s *ExamService) CertificateCandidates(ctx context.Context, competitionID uint) ([]CertificateCandidate, error) {
	view, err := s.AdminResults(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	candidates := make([]CertificateCandidate, 0, len(view.Results))
	for _, row := range view.Results {
		if !row.Status.Terminal() || row.Rank <= 0 {
			continue
		}
		candidates = append(candidates, CertificateCandidate{
			RecipientKind: "user", UserID: row.UserID, Username: row.Username,
			Name: row.Username, Rank: row.Rank, Score: row.TotalScore,
		})
	}
	return candidates, nil
}

// CertificateCandidates AWD 最终排名候选：个人赛按人、队伍赛按队（证书接收
// 单位为竞域单位；队伍名快照进证书，不展开到成员）。
func (s *AwdCompetitionService) CertificateCandidates(ctx context.Context, competitionID uint) ([]CertificateCandidate, error) {
	view, err := s.FinalRanking(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if !view.Finalized {
		return nil, fmt.Errorf("%w: AWD game has not ended", ErrValidation)
	}
	candidates := make([]CertificateCandidate, 0, len(view.Items))
	for _, item := range view.Items {
		if item.Rank <= 0 {
			continue
		}
		candidate := CertificateCandidate{
			Name: item.CompetitorName, Rank: item.Rank,
			Score: int(math.Round(item.FinalScore)),
		}
		switch item.CompetitorType {
		case model.AwdCompetitorTeam:
			candidate.RecipientKind = "team"
			candidate.TeamID = item.CompetitorID
		default:
			candidate.RecipientKind = "user"
			candidate.UserID = item.CompetitorID
			candidate.Username = item.CompetitorName
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}
