package service

import (
	"context"
	"fmt"
	"math"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 跨阶段晋级：来源竞赛可以是 CTF / AWD / 考试。各赛制域提供自己的排名口径，
// 统一展开到「个人」候选（AWD 队伍赛按队伍名次展开到全部成员），
// CompetitionService 只做阈值筛选与目标邀请赛导入。
type QualificationSourceCandidate struct {
	UserID      uint
	Username    string
	AccountCode string
	TeamName    *string
	Rank        int
	Score       int
}

type QualificationRanking struct {
	// Total 排名单位数（CTF/考试=个人数；AWD=竞域单位数），用于百分比名额。
	Total      int
	Candidates []QualificationSourceCandidate
}

type QualificationRankingSource interface {
	QualificationRanking(ctx context.Context, competitionID uint) (*QualificationRanking, error)
}

// SetQualificationSources 注入 AWD / 考试排名来源；CTF 由本服务自身提供。
func (s *CompetitionService) SetQualificationSources(awd, exam QualificationRankingSource) {
	s.qualificationSources = map[model.CompetitionFormat]QualificationRankingSource{
		model.CompetitionFormatAWD:  awd,
		model.CompetitionFormatExam: exam,
	}
}

func (s *CompetitionService) qualificationSource(format model.CompetitionFormat) (QualificationRankingSource, error) {
	if format == model.CompetitionFormatCTF {
		return s, nil
	}
	source := s.qualificationSources[format]
	if source == nil {
		return nil, fmt.Errorf("%w: qualification source is not configured for %s", ErrValidation, format)
	}
	return source, nil
}

// QualificationRanking CTF 个人榜（含零分参赛者；与排行榜/证书同一口径）。
func (s *CompetitionService) QualificationRanking(ctx context.Context, competitionID uint) (*QualificationRanking, error) {
	board, err := s.Leaderboard(ctx, competitionID, "individual")
	if err != nil {
		return nil, err
	}
	items, _ := board["items"].([]TeamLeaderboardEntry)
	ranking := &QualificationRanking{Total: len(items), Candidates: make([]QualificationSourceCandidate, 0, len(items))}
	for _, item := range items {
		if item.UserID == 0 {
			continue
		}
		username := ""
		if len(item.Members) > 0 {
			username = item.Members[0]
		}
		ranking.Candidates = append(ranking.Candidates, QualificationSourceCandidate{
			UserID: item.UserID, Username: username, AccountCode: item.AccountCode,
			TeamName: item.TeamName, Rank: item.Rank, Score: item.Score,
		})
	}
	return ranking, nil
}

// QualificationRanking 考试已交卷者（成绩单排名；未交卷不参与）。
func (s *ExamService) QualificationRanking(ctx context.Context, competitionID uint) (*QualificationRanking, error) {
	view, err := s.AdminResults(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]uint, 0, len(view.Results))
	for _, row := range view.Results {
		if row.Status.Terminal() && row.Rank > 0 {
			userIDs = append(userIDs, row.UserID)
		}
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, err
	}
	userByID := make(map[uint]*model.User, len(users))
	for i := range users {
		userByID[users[i].ID] = &users[i]
	}
	ranking := &QualificationRanking{Candidates: make([]QualificationSourceCandidate, 0, len(userIDs))}
	for _, row := range view.Results {
		if !row.Status.Terminal() || row.Rank <= 0 {
			continue
		}
		ranking.Total++
		candidate := QualificationSourceCandidate{
			UserID: row.UserID, Username: row.Username, Rank: row.Rank, Score: row.TotalScore,
		}
		if user := userByID[row.UserID]; user != nil {
			candidate.AccountCode = user.AccountCode
			if candidate.Username == "" {
				candidate.Username = user.Username
			}
		}
		ranking.Candidates = append(ranking.Candidates, candidate)
	}
	return ranking, nil
}

// QualificationRanking AWD 最终排名（赛后）：个人赛按人，队伍赛把队伍名次
// 展开到全部成员，保证目标邀请赛仍按个人账号导入。
func (s *AwdCompetitionService) QualificationRanking(ctx context.Context, competitionID uint) (*QualificationRanking, error) {
	if s.users == nil {
		return nil, fmt.Errorf("%w: user repository is not configured", ErrValidation)
	}
	view, err := s.FinalRanking(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if !view.Finalized {
		return nil, fmt.Errorf("%w: AWD game has not ended", ErrValidation)
	}
	participants, _, err := s.comps.ListParticipants(ctx, competitionID, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	teams, err := s.comps.ListTeams(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	teamNames := make(map[uint]string, len(teams))
	for _, team := range teams {
		teamNames[team.ID] = team.Name
	}
	userIDs := make([]uint, 0, len(participants))
	for _, participant := range participants {
		userIDs = append(userIDs, participant.UserID)
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, err
	}
	userByID := make(map[uint]*model.User, len(users))
	for i := range users {
		userByID[users[i].ID] = &users[i]
	}
	ranking := &QualificationRanking{Total: len(view.Items), Candidates: make([]QualificationSourceCandidate, 0)}
	for _, item := range view.Items {
		if item.Rank <= 0 {
			continue
		}
		if item.CompetitorType == model.AwdCompetitorUser {
			candidate := QualificationSourceCandidate{
				UserID: item.CompetitorID, Username: item.CompetitorName,
				Rank: item.Rank, Score: int(math.Round(item.FinalScore)),
			}
			if user := userByID[item.CompetitorID]; user != nil {
				candidate.Username = user.Username
				candidate.AccountCode = user.AccountCode
			}
			ranking.Candidates = append(ranking.Candidates, candidate)
			continue
		}
		teamName := teamNames[item.CompetitorID]
		if teamName == "" {
			teamName = item.CompetitorName
		}
		for _, participant := range participants {
			if participant.TeamID != item.CompetitorID {
				continue
			}
			candidate := QualificationSourceCandidate{
				UserID: participant.UserID, TeamName: &teamName,
				Rank: item.Rank, Score: int(math.Round(item.FinalScore)),
			}
			if user := userByID[participant.UserID]; user != nil {
				candidate.Username = user.Username
				candidate.AccountCode = user.AccountCode
			}
			ranking.Candidates = append(ranking.Candidates, candidate)
		}
	}
	return ranking, nil
}
