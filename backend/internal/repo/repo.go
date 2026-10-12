// Package repo 是数据访问层。每个子域一个接口，SQLite 是唯一实现细节，
// 向 service 只暴露领域类型（model），禁止泄漏 gorm 类型（project-conventions skill）。
package repo

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

type UserRepo interface {
	Create(ctx context.Context, u *model.User) error
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	FindByID(ctx context.Context, id uint) (*model.User, error)
	UpdateGlobalRole(ctx context.Context, id uint, role model.GlobalRole) error
	UpsertCourseRole(ctx context.Context, r *model.CourseRole) error
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
	CountSolves(ctx context.Context, challengeID uint) (int64, error)
}

type SubmissionRepo interface {
	Create(ctx context.Context, s *model.Submission) error
	FindByID(ctx context.Context, id uint) (*model.Submission, error)
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
	Stats(ctx context.Context, userID uint) (completed int, inProgress int, err error)
}