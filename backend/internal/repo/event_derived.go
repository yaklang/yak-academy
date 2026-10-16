package repo

import (
	"context"

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

func (r *eventRepo) HasHintUnlocked(ctx context.Context, userID, hintID uint) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Event{}).
		Where("user_id = ? AND type = ? AND json_extract(payload, '$.hint_id') = ?",
			userID, model.EventHintUnlocked, hintID).
		Count(&n).Error
	return n > 0, err
}

type scoreRepo struct{ db *gorm.DB }

func NewScoreRepo(db *gorm.DB) ScoreRepo { return &scoreRepo{db} }

func (r *scoreRepo) UpdateScore(ctx context.Context, userID uint, delta, solveDelta int) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO score_records (user_id, score, solve_count, updated_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(user_id) DO UPDATE SET
			score = score + excluded.score,
			solve_count = solve_count + excluded.solve_count,
			updated_at = excluded.updated_at
	`, userID, delta, solveDelta).Error
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
	err := r.db.WithContext(ctx).Order("score DESC, solve_count DESC").
		Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
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
//（event-sourcing-rules skill「内容包删除例外」；deriver 对不存在 Room 的
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
		Completed   int64
		InProgress  int64
	}
	err = r.db.WithContext(ctx).Model(&model.Progress{}).
		Select(`SUM(CASE WHEN completed THEN 1 ELSE 0 END) AS completed,
			SUM(CASE WHEN completed THEN 0 ELSE 1 END) AS in_progress`).
		Where("user_id = ?", userID).Scan(&row).Error
	return int(row.Completed), int(row.InProgress), err
}