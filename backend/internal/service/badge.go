package service

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// Badge 徽章视图（GET /badges 条目）。徽章是积分/进度/路径派生数据的
// 计算型读模型：不落库、不产生事件（event-sourcing-rules skill——派生数据
// 禁止反向物化），每次请求实时计算 earned。
type Badge struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Earned      bool   `json:"earned"`
}

// badgeRule 徽章目录：code 唯一，条件为对统计快照的纯函数。
type badgeRule struct {
	code, title, description string
	cond                     func(s badgeStats) bool
}

// badgeStats 徽章判定的统计快照（一次查询聚合，全部来自派生读模型）。
type badgeStats struct {
	Score          int
	SolveCount     int
	CompletedRooms int
	PathFinished   bool
}

// badgeCatalog 徽章目录（含"认证"类：学有所成 = 走完一条完整学习路径）。
var badgeCatalog = []badgeRule{
	{"first_solve", "初试锋芒", "通过第一道题目", func(s badgeStats) bool { return s.SolveCount >= 1 }},
	{"ten_solves", "身经百战", "累计通过 10 道题目", func(s badgeStats) bool { return s.SolveCount >= 10 }},
	{"first_room", "开卷有益", "完成第一个教程 Room", func(s badgeStats) bool { return s.CompletedRooms >= 1 }},
	{"ten_rooms", "博览群书", "累计完成 10 个教程 Room", func(s badgeStats) bool { return s.CompletedRooms >= 10 }},
	{"xp_100", "小有所成", "积分达到 100", func(s badgeStats) bool { return s.Score >= 100 }},
	{"xp_500", "登堂入室", "积分达到 500", func(s badgeStats) bool { return s.Score >= 500 }},
	{"xp_1000", "登峰造极", "积分达到 1000", func(s badgeStats) bool { return s.Score >= 1000 }},
	{"path_finisher", "学有所成", "完整走完一条学习路径", func(s badgeStats) bool { return s.PathFinished }},
}

// BadgeService 徽章查询（只读组合服务：score/progress/path 三个派生读模型）。
type BadgeService struct {
	scores   repo.ScoreRepo
	progress repo.ProgressRepo
	paths    repo.PathRepo
}

func NewBadgeService(scores repo.ScoreRepo, progress repo.ProgressRepo, paths repo.PathRepo) *BadgeService {
	return &BadgeService{scores: scores, progress: progress, paths: paths}
}

// List 当前用户徽章全集（目录所有徽章 + earned 状态）。
func (s *BadgeService) List(ctx context.Context, userID uint) ([]Badge, error) {
	stats, err := s.stats(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Badge, 0, len(badgeCatalog))
	for _, rule := range badgeCatalog {
		out = append(out, Badge{
			Code: rule.code, Title: rule.title, Description: rule.description,
			Earned: rule.cond(stats),
		})
	}
	return out, nil
}

// stats 聚合徽章判定快照：积分/通关数来自 ScoreRecord，Room 完成数来自
// Progress，path_finisher 需比对"路径全部 Room 均在完成集合"。
func (s *BadgeService) stats(ctx context.Context, userID uint) (badgeStats, error) {
	var stats badgeStats
	if rec, err := s.scores.Get(ctx, userID); err == nil && rec != nil {
		stats.Score = rec.Score
		stats.SolveCount = rec.SolveCount
	}
	completed, inProgress, err := s.progress.Stats(ctx, userID)
	if err != nil {
		return stats, err
	}
	stats.CompletedRooms = completed
	_ = inProgress // 徽章暂不用进行中数量，保留快照完整性

	completedSlugs, err := s.progress.ListCompletedSlugs(ctx, userID)
	if err != nil {
		return stats, err
	}
	done := make(map[string]struct{}, len(completedSlugs))
	for _, slug := range completedSlugs {
		done[slug] = struct{}{}
	}
	paths, err := s.paths.List(ctx)
	if err != nil {
		return stats, err
	}
	for _, p := range paths {
		items, err := s.paths.ListItems(ctx, p.ID)
		if err != nil {
			return stats, err
		}
		if len(items) == 0 {
			continue
		}
		all := true
		for _, it := range items {
			if _, ok := done[it.RoomSlug]; !ok {
				all = false
				break
			}
		}
		if all {
			stats.PathFinished = true
			break
		}
	}
	return stats, nil
}