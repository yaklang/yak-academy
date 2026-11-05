package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
)

// Event / 派生表的实现。注意语义约束（event-sourcing-rules skill）：
// EventRepo 只提供 Append/ListAfter；ScoreRepo/ProgressRepo 的写方法仅限派生器
// 调用——唯一例外是 ProgressRepo.DeleteByRoomSlugs（内容包删除清理，见其注释）。

type eventRepo struct{ db *gorm.DB }

func NewEventRepo(db *gorm.DB) EventRepo { return &eventRepo{db} }

func (r *eventRepo) Append(ctx context.Context, ev *model.Event) error {
	return r.db.WithContext(ctx).Create(ev).Error
}

func (r *eventRepo) ListAfter(ctx context.Context, afterID uint, limit int) ([]model.Event, error) {
	var list []model.Event
	err := r.db.WithContext(ctx).Where("id > ?", afterID).Order("id").Limit(limit).Find(&list).Error
	return list, err
}

// HasHintUnlocked 用户是否已解锁某提示。payload 是 JSON 文本列：SQLite 的
// json_extract 是方言函数（PostgreSQL 无），改为 LIKE 子串下推预筛
// （json.Marshal 序列化无空格，"hint_id":N 可作子串）+ Go 侧精确解析
// （子串会撞前缀：1 匹配 12）。语义与原 json_extract 等价。
func (r *eventRepo) HasHintUnlocked(ctx context.Context, userID, hintID uint) (bool, error) {
	var payloads []string
	err := r.db.WithContext(ctx).Model(&model.Event{}).
		Where("user_id = ? AND type = ? AND payload LIKE ?",
			userID, model.EventHintUnlocked, fmt.Sprintf(`%%"hint_id":%d%%`, hintID)).
		Pluck("payload", &payloads).Error
	if err != nil {
		return false, err
	}
	for _, p := range payloads {
		var v struct {
			HintID uint `json:"hint_id"`
		}
		if json.Unmarshal([]byte(p), &v) == nil && v.HintID == hintID {
			return true, nil
		}
	}
	return false, nil
}

type scoreRepo struct{ db *gorm.DB }

func NewScoreRepo(db *gorm.DB) ScoreRepo { return &scoreRepo{db} }

// UpdateScore 原子累加积分/通关数（upsert）。ON CONFLICT 语法 SQLite ≥3.24 与
// PostgreSQL 均原生支持；已有行列必须表名限定（PG 中裸列名与 excluded 同名
// 列歧义报 42702，SQLite 同样接受限定写法）；时间戳走绑定参数
// （datetime('now') 是 SQLite 方言）。仅派生器调用（event-sourcing-rules skill）。
func (r *scoreRepo) UpdateScore(ctx context.Context, userID uint, delta, solveDelta int) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO score_records (user_id, score, solve_count, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			score = score_records.score + excluded.score,
			solve_count = score_records.solve_count + excluded.solve_count,
			updated_at = excluded.updated_at
	`, userID, delta, solveDelta, time.Now()).Error
}

func (r *scoreRepo) Get(ctx context.Context, userID uint) (*model.ScoreRecord, error) {
	var s model.ScoreRecord
	if err := r.db.WithContext(ctx).First(&s, "user_id = ?", userID).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *scoreRepo) Leaderboard(ctx context.Context, offset, limit int) ([]model.ScoreRecord, int64, error) {
	var (
		list  []model.ScoreRecord
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.ScoreRecord{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	// 同分并列同名次；updated_at/user_id 只做稳定排序，保证分页不重不漏。
	err := r.db.WithContext(ctx).Order("score DESC, solve_count DESC, updated_at ASC, user_id ASC").
		Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// Stats 全站积分聚合 + 最近变动时间：排行榜 KPI 与我的排名共用。
func (r *scoreRepo) Stats(ctx context.Context) (ScoreStats, error) {
	var row struct {
		Participants int64
		TotalScore   int64
		TotalSolves  int64
		MaxScore     int
		MinScore     int
	}
	err := r.db.WithContext(ctx).Model(&model.ScoreRecord{}).
		Select(`COUNT(*) AS participants,
			COALESCE(SUM(score), 0) AS total_score,
			COALESCE(SUM(solve_count), 0) AS total_solves,
			COALESCE(MAX(score), 0) AS max_score,
			COALESCE(MIN(score), 0) AS min_score`).
		Scan(&row).Error
	if err != nil {
		return ScoreStats{}, err
	}
	stats := ScoreStats{
		Participants: row.Participants,
		TotalScore:   row.TotalScore,
		TotalSolves:  row.TotalSolves,
		MaxScore:     row.MaxScore,
		MinScore:     row.MinScore,
	}
	if row.Participants == 0 {
		return stats, nil
	}
	// MAX(updated_at) 在 SQLite 驱动中返回字符串，直接扫 time.Time 会失败；
	// 单独取最新一行交给 GORM 解析时间，SQLite/PostgreSQL 行为一致。
	var last model.ScoreRecord
	err = r.db.WithContext(ctx).Model(&model.ScoreRecord{}).
		Select("updated_at").Order("updated_at DESC").Limit(1).Take(&last).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return stats, nil
	}
	if err != nil {
		return ScoreStats{}, err
	}
	stats.LastActiveAt = &last.UpdatedAt
	return stats, nil
}

// ScoreBuckets 按 score/step 整数桶分组；负分归入 bucket=-1。
// 整数除法在 SQLite/PostgreSQL 均为截断语义，负分单独分流避免与 0 混桶。
func (r *scoreRepo) ScoreBuckets(ctx context.Context, step int) ([]ScoreBucket, error) {
	if step < 1 {
		step = 1
	}
	var rows []ScoreBucket
	err := r.db.WithContext(ctx).Model(&model.ScoreRecord{}).
		Select("CASE WHEN score < 0 THEN -1 ELSE score / ? END AS bucket, COUNT(*) AS count", step).
		Group("bucket").Order("bucket").Scan(&rows).Error
	return rows, err
}

func (r *scoreRepo) CountAhead(ctx context.Context, score, solveCount int) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.ScoreRecord{}).
		Where("score > ? OR (score = ? AND solve_count > ?)", score, score, solveCount).
		Count(&n).Error
	return n, err
}

type progressRepo struct{ db *gorm.DB }

func NewProgressRepo(db *gorm.DB) ProgressRepo { return &progressRepo{db} }

func (r *progressRepo) MarkCompleted(ctx context.Context, userID uint, roomSlug string) error {
	var p model.Progress
	return r.db.WithContext(ctx).Where(model.Progress{UserID: userID, RoomSlug: roomSlug}).
		Assign(map[string]any{"completed": true}).FirstOrCreate(&p).Error
}

func (r *progressRepo) MarkInProgress(ctx context.Context, userID uint, roomSlug string) error {
	var p model.Progress
	return r.db.WithContext(ctx).Where(model.Progress{UserID: userID, RoomSlug: roomSlug}).
		Assign(map[string]any{"completed": false}).FirstOrCreate(&p).Error
}

// DeleteByRoomSlugs 派生表唯一例外写路径：内容包删除时清理被删 Room 的进度行
// （event-sourcing-rules skill「内容包删除例外」；deriver 对不存在 Room 的
// room.completed 事件跳过，重放不复活已删进度）。
func (r *progressRepo) DeleteByRoomSlugs(ctx context.Context, roomSlugs []string) (int64, error) {
	if len(roomSlugs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Where("room_slug IN ?", roomSlugs).Delete(&model.Progress{})
	return res.RowsAffected, res.Error
}

func (r *progressRepo) Stats(ctx context.Context, userID uint) (completed, inProgress int, err error) {
	var row struct {
		Completed  int64
		InProgress int64
	}
	err = r.db.WithContext(ctx).Model(&model.Progress{}).
		Select(`SUM(CASE WHEN completed THEN 1 ELSE 0 END) AS completed,
			SUM(CASE WHEN completed THEN 0 ELSE 1 END) AS in_progress`).
		Where("user_id = ?", userID).Scan(&row).Error
	return int(row.Completed), int(row.InProgress), err
}
