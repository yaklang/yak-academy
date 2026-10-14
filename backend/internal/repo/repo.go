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
	// UpdatePassword 改密 + 同时推进 token 失效阈值（全量吊销已签发 token）。
	UpdatePassword(ctx context.Context, id uint, passwordHash string, invalidBefore time.Time) error
}

// TokenRevocationRepo 登出撤销表（jti 唯一，插入即吊销；查询走索引）。
type TokenRevocationRepo interface {
	Revoke(ctx context.Context, r *model.TokenRevocation) error
	// FindByJTI jti 命中返回撤销记录，未命中返回 nil。
	FindByJTI(ctx context.Context, jti string) (*model.TokenRevocation, error)
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
	// FindBySlugs 批量查 Room 元数据（训练场列表拼房间信息）。
	FindBySlugs(ctx context.Context, slugs ...string) ([]model.Room, error)
	ListByTopic(ctx context.Context, topicSlug string) ([]model.Room, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
}

type ChallengeRepo interface {
	Upsert(ctx context.Context, c *model.Challenge, flags []model.Flag, hints []model.Hint) error
	// FindBySlug 按题目 slug 查（详情/实例/提交解析的主键入口）。
	FindBySlug(ctx context.Context, slug string) (*model.Challenge, error)
	// FindByTutorialSlug 查关联到某教程 Room 的题目（Room 详情反查，无则 gorm.ErrRecordNotFound）。
	FindByTutorialSlug(ctx context.Context, tutorialSlug string) (*model.Challenge, error)
	FindByID(ctx context.Context, id uint) (*model.Challenge, error)
	// FindByIDs 批量查题（管理端实例列表富化用）。
	FindByIDs(ctx context.Context, ids ...uint) ([]model.Challenge, error)
	// List 全量题目分页（训练场列表用）。
	List(ctx context.Context, offset, limit int) ([]model.Challenge, int64, error)
	FindFlags(ctx context.Context, challengeID uint) ([]model.Flag, error)
	FindHints(ctx context.Context, challengeID uint) ([]model.Hint, error)
	// FindHint 按 hint 主键查（解锁接口用）。
	FindHint(ctx context.Context, hintID uint) (*model.Hint, error)
	CountSolves(ctx context.Context, challengeID uint) (int64, error)
	// Delete 删除题目（级联删 flags/hints/lab_instances 行，事务内）。
	// 调用方负责先销毁运行中容器（service.EnvService）并拦截存在通关记录的删除。
	Delete(ctx context.Context, slug string) error
}

// ChallengeAttachmentRepo 题目附件元数据（文件本体由 service 层落盘/删除）。
type ChallengeAttachmentRepo interface {
	Upsert(ctx context.Context, a *model.ChallengeAttachment) error
	ListByChallenge(ctx context.Context, challengeID uint) ([]model.ChallengeAttachment, error)
	// Find 按题目 + 文件名查附件元数据，未命中返回 nil。
	Find(ctx context.Context, challengeID uint, filename string) (*model.ChallengeAttachment, error)
	Delete(ctx context.Context, id uint) error
	// DeleteByChallenge 题目删除时级联清理（事务外由调用方保证顺序）。
	DeleteByChallenge(ctx context.Context, challengeID uint) error
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
	FindVersionByID(ctx context.Context, id uint) (*model.ContentVersion, error)
	ListVersions(ctx context.Context, packageID uint) ([]model.ContentVersion, error)
	UpdateVersionStatus(ctx context.Context, id uint, status model.PackageStatus) error
	ArchiveOtherVersions(ctx context.Context, packageID, exceptID uint) error
	UpdateLatestVersion(ctx context.Context, id uint, version string) error
	List(ctx context.Context, offset, limit int) ([]model.ContentPackage, int64, error)
}

type EventRepo interface {
	// Append 是 Event 表唯一允许的写操作（append-only，event-sourcing-rules skill）。
	Append(ctx context.Context, ev *model.Event) error
	ListAfter(ctx context.Context, afterID uint, limit int) ([]model.Event, error)
	// HasHintUnlocked 是否已写过 hint.unlocked（解锁幂等判定）。
	HasHintUnlocked(ctx context.Context, userID, hintID uint) (bool, error)
}

type ScoreRepo interface {
	// UpdateScore 仅允许事件派生器调用。
	UpdateScore(ctx context.Context, userID uint, delta int, solveDelta int) error
	Get(ctx context.Context, userID uint) (*model.ScoreRecord, error)
	Leaderboard(ctx context.Context, offset, limit int) ([]model.ScoreRecord, int64, error)
	// CountAhead 统计 (score, solve_count) 严格靠前的记录数：rank = 1 + CountAhead，
	// 并列同名次且跨页一致（服务端分页下 offset+i+1 会错位）。
	CountAhead(ctx context.Context, score, solveCount int) (int64, error)
}

type ProgressRepo interface {
	// MarkCompleted 仅允许事件派生器调用。
	MarkCompleted(ctx context.Context, userID uint, roomSlug string) error
	// MarkInProgress 记录"开始学但未完成"（Room 首次浏览时创建 completed=false 行，幂等）。
	MarkInProgress(ctx context.Context, userID uint, roomSlug string) error
	Stats(ctx context.Context, userID uint) (completed int, inProgress int, err error)
	// ListCompletedSlugs 当前用户已完成的 Room slug 集合（学习路径进度用）。
	ListCompletedSlugs(ctx context.Context, userID uint) ([]string, error)
}

// PathRepo 学习路径：PathItem 只引用 room_slug（teaching-integration skill）。
type PathRepo interface {
	List(ctx context.Context) ([]model.LearningPath, error)
	FindByID(ctx context.Context, id uint) (*model.LearningPath, error)
	ListItems(ctx context.Context, pathID uint) ([]model.PathItem, error)
	Create(ctx context.Context, path *model.LearningPath, items []model.PathItem) error
	Delete(ctx context.Context, id uint) error
}

type LabInstanceRepo interface {
	Create(ctx context.Context, i *model.LabInstance) error
	FindByID(ctx context.Context, id uint) (*model.LabInstance, error)
	// FindActive 查询 user×challenge 当前活跃（creating/running）实例，无则返回 nil。
	FindActive(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error)
	// FindActiveAny 该用户任意题目的当前活跃实例（单容器约束用），无则 nil。
	FindActiveAny(ctx context.Context, userID uint) (*model.LabInstance, error)
	// UpdateRunning driver.Create 成功后回写 endpoint/expire_at/flag_value 并置 running。
	UpdateRunning(ctx context.Context, id uint, endpoint string, expireAt time.Time, flagValue string) error
	// UpdateExpireAt TTL 续期（不重建容器）。
	UpdateExpireAt(ctx context.Context, id uint, expireAt time.Time) error
	UpdateStatus(ctx context.Context, id uint, status model.InstanceStatus) error
	// ListExpired ExpireAt 已过期且仍 running 的实例（reaper 用）。
	ListExpired(ctx context.Context, now time.Time) ([]model.LabInstance, error)
	// List 全量实例分页（管理端容器面板用）。
	List(ctx context.Context, offset, limit int) ([]model.LabInstance, int64, error)
	// ListActive 全部用户的活跃（creating/running）实例（管理端概览用）。
	ListActive(ctx context.Context) ([]model.LabInstance, error)
	// ListActiveByChallenge 某题目全部用户的活跃实例（删除题目前销毁容器用）。
	ListActiveByChallenge(ctx context.Context, challengeID uint) ([]model.LabInstance, error)
	// FindByCheckpointToken 检测点回调解析：token 命中且实例仍活跃（creating/running），
	// 无则返回 nil（回调 404 语义）。
	FindByCheckpointToken(ctx context.Context, token string) (*model.LabInstance, error)
}

// InstanceLogRepo 容器实例生命周期日志（append-only：只 Create 与分页查询）。
type InstanceLogRepo interface {
	Create(ctx context.Context, l *model.InstanceLog) error
	List(ctx context.Context, offset, limit int) ([]model.InstanceLog, int64, error)
}
