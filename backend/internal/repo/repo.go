// Package repo 是数据访问层。每个子域一个接口，SQLite 是唯一实现细节，
// 向 service 只暴露领域类型（model），禁止泄漏 gorm 类型（project-conventions skill）。
package repo

import (
	"context"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

type UserRepo interface {
	Create(ctx context.Context, u *model.User) error
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	FindByID(ctx context.Context, id uint) (*model.User, error)
	FindByIDs(ctx context.Context, ids ...uint) ([]model.User, error)
	List(ctx context.Context, offset, limit int) ([]model.User, int64, error)
	UpdateGlobalRole(ctx context.Context, id uint, role model.GlobalRole) error
	UpsertCourseRole(ctx context.Context, r *model.CourseRole) error
	DeleteCourseRole(ctx context.Context, userID uint, topicSlug string) error
	ListCourseRoles(ctx context.Context, userID uint) ([]model.CourseRole, error)
}

type TopicRepo interface {
	Upsert(ctx context.Context, t *model.Topic) error
	List(ctx context.Context, offset, limit int) ([]model.Topic, int64, error)
	FindBySlug(ctx context.Context, slug string) (*model.Topic, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
}

type RoomRepo interface {
	Upsert(ctx context.Context, r *model.Room) error
	FindBySlug(ctx context.Context, slug string) (*model.Room, error)
	ListByTopic(ctx context.Context, topicSlug string) ([]model.Room, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
}

type ChallengeRepo interface {
	Upsert(ctx context.Context, c *model.Challenge, flags []model.Flag, hints []model.Hint) error
	FindByRoomSlug(ctx context.Context, roomSlug string) (*model.Challenge, error)
	FindByID(ctx context.Context, id uint) (*model.Challenge, error)
	FindFlags(ctx context.Context, challengeID uint) ([]model.Flag, error)
	FindHints(ctx context.Context, challengeID uint) ([]model.Hint, error)
	CountSolves(ctx context.Context, challengeID uint) (int64, error)
}

type SubmissionRepo interface {
	Create(ctx context.Context, s *model.Submission) error
	FindByID(ctx context.Context, id uint) (*model.Submission, error)
	// ListPending 扫描滞留 pending 的提交（worker 启动补偿用）。
	ListPending(ctx context.Context) ([]uint, error)
	// HasCorrect 判定"该用户该题是否已通过"（防重复计分）。
	HasCorrect(ctx context.Context, userID, challengeID uint) (bool, error)
	UpdateStatus(ctx context.Context, id uint, status model.SubmissionStatus, score int) error
}

type ContentPackageRepo interface {
	Create(ctx context.Context, p *model.ContentPackage) error
	FindByID(ctx context.Context, id uint) (*model.ContentPackage, error)
	FindByName(ctx context.Context, name string) (*model.ContentPackage, error)
	UpdateStatus(ctx context.Context, id uint, status model.PackageStatus) error
	AddVersion(ctx context.Context, v *model.ContentVersion) error
	FindLatestVersion(ctx context.Context, packageID uint) (*model.ContentVersion, error)
	UpdateLatestVersion(ctx context.Context, id uint, version string) error
	List(ctx context.Context, offset, limit int) ([]model.ContentPackage, int64, error)
}

type EventRepo interface {
	// Append 是 Event 表唯一允许的写操作（append-only，event-sourcing-rules skill）。
	Append(ctx context.Context, ev *model.Event) error
	ListAfter(ctx context.Context, afterID uint, limit int) ([]model.Event, error)
}

type ScoreRepo interface {
	// UpdateScore 仅允许事件派生器调用。
	UpdateScore(ctx context.Context, userID uint, delta int, solveDelta int) error
	Get(ctx context.Context, userID uint) (*model.ScoreRecord, error)
	Leaderboard(ctx context.Context, offset, limit int) ([]model.ScoreRecord, int64, error)
}

type ProgressRepo interface {
	// MarkCompleted 仅允许事件派生器调用。
	MarkCompleted(ctx context.Context, userID uint, roomSlug string) error
	// MarkInProgress 记录"开始学但未完成"（Room 首次浏览时创建 completed=false 行，幂等）。
	MarkInProgress(ctx context.Context, userID uint, roomSlug string) error
	Stats(ctx context.Context, userID uint) (completed int, inProgress int, err error)
}

type LabInstanceRepo interface {
	Create(ctx context.Context, i *model.LabInstance) error
	FindByID(ctx context.Context, id uint) (*model.LabInstance, error)
	// FindActive 查询 user×challenge 当前活跃（creating/running）实例，无则返回 nil。
	FindActive(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error)
	// CountActive 该用户活跃（creating/running）实例数（并发上限用）。
	CountActive(ctx context.Context, userID uint) (int64, error)
	// UpdateRunning driver.Create 成功后回写 endpoint/expire_at 并置 running。
	UpdateRunning(ctx context.Context, id uint, endpoint string, expireAt time.Time) error
	UpdateStatus(ctx context.Context, id uint, status model.InstanceStatus) error
	// ListExpired ExpireAt 已过期且仍 running 的实例（reaper 用）。
	ListExpired(ctx context.Context, now time.Time) ([]model.LabInstance, error)
}