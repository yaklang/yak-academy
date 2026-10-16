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

// Leaderboard 排行榜（派生数据视图）。rank = 1 + 严格靠前记录数：
// 同分同解决数并列同名次，且跨页一致（count-based，替代 offset+i+1 的分页错位）。
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
		ahead, err := s.scores.CountAhead(ctx, r.Score, r.SolveCount)
		if err != nil {
			return nil, 0, err
		}
		entries[i] = LeaderboardEntry{
			Rank: int(ahead) + 1, Username: names[r.UserID],
			Score: r.Score, SolveCount: r.SolveCount,
		}
	}
	return entries, total, nil
}

// Deriver 事件派生器：消费事件 → 增量更新 ScoreRecord/Progress/CompetitionScore。
// 派生表唯一的合法写入者（架构文档 §3.7）。
type Deriver struct {
	scores       repo.ScoreRepo
	progress     repo.ProgressRepo
	competitions repo.CompetitionRepo
	// teams 队伍名查询（CompetitionScore 派生行冗余队伍快照用）。
	teams func(ctx context.Context, competitionID uint) (map[uint]string, error)
	// roomExists 教程 Room 存在性查询（内容包删除守卫：被删 Room 的
	// room.completed 事件跳过，重放不复活已清进度）。nil = 不设防（单测）。
	roomExists func(ctx context.Context, roomSlug string) bool
}

func NewDeriver(s repo.ScoreRepo, p repo.ProgressRepo) *Deriver {
	return &Deriver{scores: s, progress: p}
}

// SetCompetitionStores 竞赛派生依赖（AddScore 首通行 + 队伍快照）。
// 由 main 组装时注入，避免 service 横向依赖（deriver 不 import competition service）。
func (d *Deriver) SetCompetitionStores(competitions repo.CompetitionRepo, teams func(ctx context.Context, competitionID uint) (map[uint]string, error)) {
	d.competitions = competitions
	d.teams = teams
}

// SetRoomLookup 注入 Room 存在性查询（内容包删除守卫，见 ContentService.DeletePackage）。
func (d *Deriver) SetRoomLookup(roomExists func(ctx context.Context, roomSlug string) bool) {
	d.roomExists = roomExists
}

// judgedPayload submission.judged 事件的 payload 结构。
type judgedPayload struct {
	Correct       bool   `json:"correct"`
	Score         int    `json:"score"`
	SubmissionID  uint   `json:"submission_id"`
	ChallengeID   uint   `json:"challenge_id"`
	CompetitionID uint   `json:"competition_id"`
	SourceIP      string `json:"source_ip"`
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
		// 竞赛提交：同事件另派生竞赛积分（CompetitionScore，event-sourcing-rules：
		// 同一事实源 submission.judged 派生两份视图，训练场 XP 与竞赛榜互不串扰）。
		if p.CompetitionID > 0 && d.competitions != nil {
			d.deriveCompetitionScore(ctx, ev, p)
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
		// 内容包删除守卫：Room 已不存在（含历代版本移除的 slug）时跳过，
		// 防启动重放为已清理的进度行"复活"（event-sourcing-rules「内容包删除例外」）。
		if d.roomExists != nil && !d.roomExists(ctx, p.RoomSlug) {
			return
		}
		if err := d.progress.MarkCompleted(ctx, ev.UserID, p.RoomSlug); err != nil {
			log.Printf("deriver: mark completed: %v", err)
		}
	default:
	}
}

// deriveCompetitionScore 竞赛首通 → CompetitionScore 派生行（AddScore 幂等：
// 已有行 0 rows，事件重复消费安全）。队伍快照（TeamID）取自参赛记录所属队伍。
// 派生失败只记日志：Event 表仍是事实源，重放可修复。
func (d *Deriver) deriveCompetitionScore(ctx context.Context, ev model.Event, p judgedPayload) {
	rec := &model.CompetitionScore{
		CompetitionID: p.CompetitionID,
		UserID:        ev.UserID,
		ChallengeID:   p.ChallengeID,
		Score:         p.Score,
		SolvedAt:      ev.CreatedAt,
	}
	// 队伍快照：参赛记录是运营态而非派生表，直接读。
	if part, err := d.competitions.FindParticipant(ctx, p.CompetitionID, ev.UserID); err != nil {
		log.Printf("deriver: competition participant: %v", err)
	} else if part != nil {
		rec.TeamID = part.TeamID
	}
	if _, err := d.competitions.AddScore(ctx, rec); err != nil {
		log.Printf("deriver: competition score: %v", err)
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