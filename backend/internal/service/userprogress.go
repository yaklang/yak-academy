package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// UserProgressService 路径与进度。Progress 为事件派生数据，
// 本服务只读派生表，不做任何直接改写（event-sourcing-rules skill）。
type UserProgressService struct {
	scores   repo.ScoreRepo
	progress repo.ProgressRepo
	users    repo.UserRepo
}

func NewUserProgressService(s repo.ScoreRepo, p repo.ProgressRepo, u repo.UserRepo) *UserProgressService {
	return &UserProgressService{scores: s, progress: p, users: u}
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

// LeaderboardEntry 排行榜条目（含用户名组装，契约 LeaderboardEntry schema）。
type LeaderboardEntry struct {
	Rank       int    `json:"rank"`
	Username   string `json:"username"`
	Score      int    `json:"score"`
	SolveCount int    `json:"solved_count"`
}

// Leaderboard 排行榜（派生数据视图）。rank 按 score DESC, solve_count DESC 排序赋值；
// 同分并列的跨页歧义 Phase 2 接受（Phase 4 引入持久化 rank）。
func (s *UserProgressService) Leaderboard(ctx context.Context, offset, limit int) ([]LeaderboardEntry, int64, error) {
	records, total, err := s.scores.Leaderboard(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	if len(records) == 0 {
		return []LeaderboardEntry{}, total, nil
	}
	ids := make([]uint, len(records))
	for i, r := range records {
		ids[i] = r.UserID
	}
	users, err := s.users.FindByIDs(ctx, ids...)
	if err != nil {
		return nil, 0, err
	}
	names := make(map[uint]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Username
	}
	entries := make([]LeaderboardEntry, len(records))
	for i, r := range records {
		entries[i] = LeaderboardEntry{
			Rank: offset + i + 1, Username: names[r.UserID],
			Score: r.Score, SolveCount: r.SolveCount,
		}
	}
	return entries, total, nil
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

// judgedPayload submission.judged 事件的 payload 结构。
type judgedPayload struct {
	Correct      bool `json:"correct"`
	Score        int  `json:"score"`
	SubmissionID uint `json:"submission_id"`
	ChallengeID  uint `json:"challenge_id"`
}

// completedPayload room.completed 事件的 payload 结构。
type completedPayload struct {
	RoomSlug string `json:"room_slug"`
}

// hintUnlockedPayload hint.unlocked 事件的 payload 结构。
type hintUnlockedPayload struct {
	HintID uint `json:"hint_id"`
	Cost   int  `json:"cost"`
}

// Handle 实现事件消费。仅处理计分/进度相关事件。
// 派生操作幂等性依赖：UpdateScore 只在 score>0 时调用（重复 correct 事件 score=0 天然跳过）；
// MarkCompleted 幂等（FirstOrCreate）；hint 扣分由解锁接口保证事件只写一次。
func (d *Deriver) Handle(ctx context.Context, ev model.Event) {
	switch ev.Type {
	case model.EventSubmissionJudged:
		var p judgedPayload
		if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
			log.Printf("deriver: bad submission.judged payload: %v", err)
			return
		}
		if !p.Correct || p.Score <= 0 {
			return // 零分（重复通关）不计入派生表
		}
		if err := d.scores.UpdateScore(ctx, ev.UserID, p.Score, 1); err != nil {
			log.Printf("deriver: update score: %v", err)
		}
	case model.EventHintUnlocked:
		var p hintUnlockedPayload
		if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
			log.Printf("deriver: bad hint.unlocked payload: %v", err)
			return
		}
		if p.Cost <= 0 {
			return // 免费提示不改积分
		}
		if err := d.scores.UpdateScore(ctx, ev.UserID, -p.Cost, 0); err != nil {
			log.Printf("deriver: deduct hint cost: %v", err)
		}
	case model.EventRoomCompleted:
		var p completedPayload
		if err := json.Unmarshal([]byte(ev.Payload), &p); err != nil {
			log.Printf("deriver: bad room.completed payload: %v", err)
			return
		}
		if err := d.progress.MarkCompleted(ctx, ev.UserID, p.RoomSlug); err != nil {
			log.Printf("deriver: mark completed: %v", err)
		}
	default:
	}
}

// Replay 启动补偿：从头重放事件表，修复进程内 bus 崩溃窗口丢失的派生。
// 已知限制：进程内 bus 无持久消费位点，单个事件可能重复消费（幂等保护）或
// 在崩溃窗口内丢失派生（Replay 补偿）。Phase 4 换持久总线后移除。
func (d *Deriver) Replay(ctx context.Context, events repo.EventRepo) error {
	var afterID uint
	for {
		batch, err := events.ListAfter(ctx, afterID, 500)
		if err != nil {
			return fmt.Errorf("replay: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for _, ev := range batch {
			d.Handle(ctx, ev)
		}
		afterID = batch[len(batch)-1].ID
	}
}

// MarkRoomViewed Room 详情浏览时记录 in-progress（属主/业务规则在 service 层，
// rbac-model skill）。仅当该 Room 尚无进度行时创建 completed=false 行。
func (s *UserProgressService) MarkRoomViewed(ctx context.Context, userID uint, roomSlug string) error {
	return s.progress.MarkInProgress(ctx, userID, roomSlug)
}