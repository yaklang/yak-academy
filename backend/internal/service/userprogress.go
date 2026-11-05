package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

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
		"xp":                xp,
		"completed_rooms":   completed,
		"in_progress_rooms": inProgress,
	}, nil
}

// LeaderboardEntry 排行榜条目（含用户名组装，契约 LeaderboardEntry schema）。
type LeaderboardEntry struct {
	Rank         int     `json:"rank"`
	Username     string  `json:"username"`
	Score        int     `json:"score"`
	SolveCount   int     `json:"solved_count"`
	LastActiveAt *string `json:"last_active_at"`
}

// LeaderboardStats 全站排行榜聚合（事件派生表只读统计）。
type LeaderboardStats struct {
	Participants int64   `json:"participants"`
	TotalScore   int64   `json:"total_score"`
	TotalSolves  int64   `json:"total_solves"`
	AverageScore int     `json:"average_score"`
	TopScore     int     `json:"top_score"`
	UpdatedAt    *string `json:"updated_at"`
}

// ScoreDistributionBand 积分分布区间：标签与分档由 service 生成，
// repo 只返回 score/step 桶计数（展示文案不进数据层）。
type ScoreDistributionBand struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// LeaderboardView 排行榜整页响应：分页名次 + 全场前三 + 当前用户 +
// 聚合统计 + 积分分布。所有数据来自事件派生表，只读。
type LeaderboardView struct {
	Total        int64                   `json:"total"`
	Page         int                     `json:"page"`
	PageSize     int                     `json:"page_size"`
	Items        []LeaderboardEntry      `json:"items"`
	Top          []LeaderboardEntry      `json:"top"`
	Me           *LeaderboardEntry       `json:"me"`
	Stats        LeaderboardStats        `json:"stats"`
	Distribution []ScoreDistributionBand `json:"distribution"`
}

// Leaderboard 排行榜（派生数据视图）。rank = 1 + 严格靠前记录数：
// 同分同解决数并列同名次，且跨页一致（count-based，替代 offset+i+1 的分页错位）。
// top 固定取全场前三（任何分页都返回）；me 不受分页限制（未上榜为 nil）。
func (s *UserProgressService) Leaderboard(ctx context.Context, userID uint, page, pageSize int) (*LeaderboardView, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	records, total, err := s.scores.Leaderboard(ctx, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}
	items, err := s.leaderboardEntries(ctx, records)
	if err != nil {
		return nil, err
	}
	topRecords, _, err := s.scores.Leaderboard(ctx, 0, 3)
	if err != nil {
		return nil, err
	}
	top, err := s.leaderboardEntries(ctx, topRecords)
	if err != nil {
		return nil, err
	}
	stats, err := s.scores.Stats(ctx)
	if err != nil {
		return nil, err
	}
	buckets, err := s.scores.ScoreBuckets(ctx, distributionStep(stats.MaxScore))
	if err != nil {
		return nil, err
	}
	me, err := s.leaderboardMe(ctx, userID)
	if err != nil {
		return nil, err
	}
	avg := 0
	if stats.Participants > 0 {
		avg = int(stats.TotalScore / stats.Participants)
	}
	view := &LeaderboardView{
		Total:        total,
		Page:         page,
		PageSize:     pageSize,
		Items:        items,
		Top:          top,
		Me:           me,
		Stats:        LeaderboardStats{Participants: stats.Participants, TotalScore: stats.TotalScore, TotalSolves: stats.TotalSolves, AverageScore: avg, TopScore: stats.MaxScore, UpdatedAt: formatOptionalTime(stats.LastActiveAt)},
		Distribution: buildDistribution(stats.MaxScore, buckets),
	}
	if view.Items == nil {
		view.Items = []LeaderboardEntry{}
	}
	if view.Top == nil {
		view.Top = []LeaderboardEntry{}
	}
	if view.Distribution == nil {
		view.Distribution = []ScoreDistributionBand{}
	}
	return view, nil
}

// leaderboardEntries 把派生行组装为契约条目（用户名 + count-based 名次）。
func (s *UserProgressService) leaderboardEntries(ctx context.Context, records []model.ScoreRecord) ([]LeaderboardEntry, error) {
	entries := make([]LeaderboardEntry, 0, len(records))
	if len(records) == 0 {
		return entries, nil
	}
	ids := make([]uint, len(records))
	for i, r := range records {
		ids[i] = r.UserID
	}
	users, err := s.users.FindByIDs(ctx, ids...)
	if err != nil {
		return nil, err
	}
	names := make(map[uint]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Username
	}
	for _, r := range records {
		ahead, err := s.scores.CountAhead(ctx, r.Score, r.SolveCount)
		if err != nil {
			return nil, err
		}
		name := names[r.UserID]
		if name == "" {
			name = fmt.Sprintf("用户 #%d", r.UserID)
		}
		entries = append(entries, LeaderboardEntry{
			Rank: int(ahead) + 1, Username: name,
			Score: r.Score, SolveCount: r.SolveCount, LastActiveAt: formatOptionalTime(&r.UpdatedAt),
		})
	}
	return entries, nil
}

// leaderboardMe 当前用户排名（未上榜返回 nil, nil；排行榜页「我的位置」用）。
func (s *UserProgressService) leaderboardMe(ctx context.Context, userID uint) (*LeaderboardEntry, error) {
	rec, err := s.scores.Get(ctx, userID)
	if err != nil {
		if errorsIsNotFound(err) {
			return nil, nil // 未产生积分记录 = 未上榜
		}
		return nil, err
	}
	users, err := s.users.FindByIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	name := ""
	if len(users) > 0 {
		name = users[0].Username
	}
	if name == "" {
		name = fmt.Sprintf("用户 #%d", userID)
	}
	ahead, err := s.scores.CountAhead(ctx, rec.Score, rec.SolveCount)
	if err != nil {
		return nil, err
	}
	return &LeaderboardEntry{
		Rank: int(ahead) + 1, Username: name,
		Score: rec.Score, SolveCount: rec.SolveCount, LastActiveAt: formatOptionalTime(&rec.UpdatedAt),
	}, nil
}

func formatOptionalTime(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	v := t.UTC().Format(time.RFC3339)
	return &v
}

// distributionStep 根据榜首积分选取 5 档的档宽（1/2/2.5/5/10 × 10^n）。
func distributionStep(maxScore int) int {
	raw := (maxScore + 4) / 5 // ceil(max/5)
	if raw < 1 {
		return 1
	}
	pow := 1
	for pow*10 <= raw {
		pow *= 10
	}
	candidates := []int{pow, 2 * pow, 5 * pow, 10 * pow}
	if pow >= 10 {
		candidates = []int{pow, 2 * pow, pow * 5 / 2, 5 * pow, 10 * pow}
	}
	for _, c := range candidates {
		if c >= raw {
			return c
		}
	}
	return 10 * pow
}

// buildDistribution 将 repo 的整数桶组装为展示区间；负分单独一档。
// 非负分固定 5 档，总数与 participants 对得上（最后一档吸收所有 >=4*step 的桶）。
func buildDistribution(maxScore int, buckets []repo.ScoreBucket) []ScoreDistributionBand {
	counts := make(map[int]int64, len(buckets))
	for _, b := range buckets {
		counts[b.Bucket] += b.Count
	}
	out := make([]ScoreDistributionBand, 0, 6)
	if n := counts[-1]; n > 0 {
		out = append(out, ScoreDistributionBand{Label: "负分", Count: n})
	}
	if maxScore <= 0 {
		if n := counts[0]; n > 0 {
			out = append(out, ScoreDistributionBand{Label: "0", Count: n})
		}
		return out
	}
	step := distributionStep(maxScore)
	for i := 0; i < 5; i++ {
		count := counts[i]
		label := fmt.Sprintf("%d–%d", i*step, (i+1)*step-1)
		if i == 4 {
			label = fmt.Sprintf("%d+", 4*step)
			for b, n := range counts {
				if b >= 4 {
					count += n
				}
			}
		}
		out = append(out, ScoreDistributionBand{Label: label, Count: count})
	}
	return out
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
