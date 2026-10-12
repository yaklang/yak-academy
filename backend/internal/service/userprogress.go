package service

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// UserProgressService 路径与进度。Progress 为事件派生数据，
// 本服务只读派生表，不做任何直接改写（event-sourcing-rules skill）。
type UserProgressService struct {
	scores   repo.ScoreRepo
	progress repo.ProgressRepo
}

func NewUserProgressService(s repo.ScoreRepo, p repo.ProgressRepo) *UserProgressService {
	return &UserProgressService{scores: s, progress: p}
}

// MyProgress 个人进度视图。
func (s *UserProgressService) MyProgress(ctx context.Context, userID uint) (map[string]any, error) {
	completed, inProgress, err := s.progress.Stats(ctx, userID)
	if err != nil {
		return nil, err
	}
	xp := 0
	if rec, err := s.scores.Get(ctx, userID); err == nil {
		xp = rec.Score
	}
	return map[string]any{
		"xp":                 xp,
		"completed_rooms":    completed,
		"in_progress_rooms":  inProgress,
	}, nil
}

// Leaderboard 排行榜（派生数据视图）。
func (s *UserProgressService) Leaderboard(ctx context.Context, offset, limit int) ([]model.ScoreRecord, int64, error) {
	return s.scores.Leaderboard(ctx, offset, limit)
}

// Deriver 事件派生器：消费事件 → 增量更新 ScoreRecord/Progress。
// 派生表唯一的合法写入者（架构文档 §3.7）。
type Deriver struct {
	scores   repo.ScoreRepo
	progress repo.ProgressRepo
}

func NewDeriver(s repo.ScoreRepo, p repo.ProgressRepo) *Deriver {
	return &Deriver{scores: s, progress: p}
}

// Handle 实现事件消费。仅处理计分/进度相关事件。
func (d *Deriver) Handle(ctx context.Context, ev model.Event) {
	switch ev.Type {
	case model.EventSubmissionJudged:
		// TODO(Phase2): 解析 payload 中的 correct/score，d.scores.UpdateScore(...)。
	case model.EventRoomCompleted:
		// TODO(Phase2): d.progress.MarkCompleted(...)。
	default:
	}
}