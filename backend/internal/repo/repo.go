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
	// FindByPhone 注册手机号查重（自助注册）。调用前应保证 phone 非空。
	FindByPhone(ctx context.Context, phone string) (*model.User, error)
	List(ctx context.Context, offset, limit int) ([]model.User, int64, error)
	UpdateGlobalRole(ctx context.Context, id uint, role model.GlobalRole) error
	// UpdatePassword 改密 + 同时推进 token 失效阈值（全量吊销已签发 token）。
	UpdatePassword(ctx context.Context, id uint, passwordHash string, invalidBefore time.Time) error
	// UpdateBio 更新账号简介（账号信息面板）。
	UpdateBio(ctx context.Context, id uint, bio string) error
	// UpdateAvatar 更新头像文件名（空串 = 未上传）。
	UpdateAvatar(ctx context.Context, id uint, avatarFile string) error
}

// SmsCodeRepo 注册验证码（自助注册）：发码入库、限频计数、核销。
type SmsCodeRepo interface {
	Create(ctx context.Context, c *model.SmsCode) error
	// LatestByPhone 该手机号最近一条验证码（含已用/已过期，供核销判定）；
	// gorm.ErrRecordNotFound = 从未发过。
	LatestByPhone(ctx context.Context, phone string) (*model.SmsCode, error)
	// CountSince 该手机号 windowStart 之后已发送条数（限频窗口计数）。
	CountSince(ctx context.Context, phone string, windowStart time.Time) (int64, error)
	// MarkUsed 核销（一次性；返回 0 = 已被并发核销或不存在）。
	MarkUsed(ctx context.Context, id uint) (int64, error)
}

// MailRepo 站内邮箱（只收不发）：用户只读 + 标记已读；投递走 Deliver。
type MailRepo interface {
	Deliver(ctx context.Context, m *model.Mail) error
	List(ctx context.Context, userID uint, offset, limit int) ([]model.Mail, int64, error)
	UnreadCount(ctx context.Context, userID uint) (int64, error)
	// MarkRead 标记已读（幂等；返回 0 = 无此邮件或不属当前用户）。
	MarkRead(ctx context.Context, userID, mailID uint) (int64, error)
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
	// FindBuiltinBySlugs 查一批 slug 中的内置 Topic 行（一键导入回填标记用；
	// 返回按 slug 索引，非内置/不存在的 slug 不出现）。
	FindBuiltinBySlugs(ctx context.Context, slugs ...string) (map[string]model.Topic, error)
	// MarkBuiltin 批量改写来源标记（仅内置导入调用；Upsert 不触碰此列）。
	MarkBuiltin(ctx context.Context, slugs []string, builtin bool) error
	// DeleteBySlugs 删除内容包时按 slug 批量清理，返回受影响行数。
	DeleteBySlugs(ctx context.Context, slugs ...string) (int64, error)
	// FindBuiltinNotInSlugs 查不在当前内置目录里的内置 Topic 行（导入 prune：
	// 目录外残留即清理对象；slugs 为空 = 全部内置行）。
	FindBuiltinNotInSlugs(ctx context.Context, slugs ...string) ([]model.Topic, error)
}

type RoomRepo interface {
	Upsert(ctx context.Context, r *model.Room) error
	FindBySlug(ctx context.Context, slug string) (*model.Room, error)
	// FindBySlugs 批量查 Room 元数据（训练场列表拼房间信息）。
	FindBySlugs(ctx context.Context, slugs ...string) ([]model.Room, error)
	ListByTopic(ctx context.Context, topicSlug string) ([]model.Room, error)
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
	// FindBuiltinBySlugs 查一批 slug 中的内置 Room 行（导入回填/管理端目录
	// imported 探测用；返回按 slug 索引，非内置/不存在的 slug 不出现）。
	FindBuiltinBySlugs(ctx context.Context, slugs ...string) (map[string]model.Room, error)
	// MarkBuiltin 批量改写来源标记（仅内置导入调用；Upsert 不触碰此列）。
	MarkBuiltin(ctx context.Context, slugs []string, builtin bool) error
	// DeleteBySlugs 删除内容包时按 slug 批量清理，返回受影响行数。
	DeleteBySlugs(ctx context.Context, slugs ...string) (int64, error)
	// FindBuiltinNotInSlugs 查不在当前内置目录里的内置 Room 行（导入 prune）。
	FindBuiltinNotInSlugs(ctx context.Context, slugs ...string) ([]model.Room, error)
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
	// List 全量题目分页（管理端表格用；filter 零值 = 不过滤）。
	List(ctx context.Context, filter ChallengeListFilter, offset, limit int) ([]model.Challenge, int64, error)
	// ListVisible 学生端题库分页：仅 is_visible 且已到发布时间的题目（filter 零值 = 不过滤）。
	ListVisible(ctx context.Context, now time.Time, filter ChallengeListFilter, offset, limit int) ([]model.Challenge, int64, error)
	// UpdateVisibility 管理端更新可见性开关/定时发布时间；返回受影响行数（0 = slug 不存在）。
	UpdateVisibility(ctx context.Context, slug string, isVisible *bool, releaseAt *time.Time) (int64, error)
	// MarkBuiltin 批量改写题目来源标记（内置题集导入置 true；普通上传不经此路径）。
	// 返回受影响行数。
	MarkBuiltin(ctx context.Context, slugs []string, builtin bool) (int64, error)
	// ClearTutorialSlugs 教程 Room 被删除时批量清空指向它的 tutorial_slug（防死链），
	// 返回受影响行数。
	ClearTutorialSlugs(ctx context.Context, roomSlugs []string) (int64, error)
	FindFlags(ctx context.Context, challengeID uint) ([]model.Flag, error)
	FindHints(ctx context.Context, challengeID uint) ([]model.Hint, error)
	// FindHint 按 hint 主键查（解锁接口用）。
	FindHint(ctx context.Context, hintID uint) (*model.Hint, error)
	CountSolves(ctx context.Context, challengeID uint) (int64, error)
	// Delete 删除题目（级联删 flags/hints/lab_instances 行，事务内）。
	// 调用方负责先销毁运行中容器（service.EnvService）并拦截存在通关记录的删除。
	Delete(ctx context.Context, slug string) error
	// FindBuiltinNotInSlugs 查不在当前内置目录里的内置题目行（导入 prune）。
	FindBuiltinNotInSlugs(ctx context.Context, slugs ...string) ([]model.Challenge, error)
}

// ChallengeListFilter 题库列表过滤：Bank 空 = 全部题库 tab；IsBuiltin nil = 不限来源。
type ChallengeListFilter struct {
	Bank      string
	IsBuiltin *bool
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
	// HasCorrect 判定"该用户该题（指定作用域）是否已通过"（防重复计分）。
	// competitionID=0 表示训练场作用域；竞赛内首通与训练场首通相互独立。
	HasCorrect(ctx context.Context, userID, challengeID, competitionID uint) (bool, error)
	// CountCorrectSolves 该题在指定作用域的通过人数（动态计分 solve_count）。
	CountCorrectSolves(ctx context.Context, challengeID, competitionID uint) (int64, error)
	// ListByCompetitionUser 参赛者本人在该竞赛的提交（竞赛首页我的提交）。
	ListByCompetitionUser(ctx context.Context, competitionID, userID uint, limit int) ([]model.Submission, error)
	// DeleteByCompetition 删除该竞赛全部提交行（竞赛删除级联，事务由调用方管理）。
	DeleteByCompetition(ctx context.Context, competitionID uint) error
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
	// Delete 删除内容包（事务内级联删全部版本行）。内容表（topics/rooms）
	// 与派生表的级联清理由调用方（ContentService.DeletePackage）按 Payload 编排。
	Delete(ctx context.Context, id uint) error
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
	// DeleteByRoomSlugs 删除内容包时清理被删 Room 的进度行（派生表唯一例外写路径，
	// 见 event-sourcing-rules skill「内容包删除例外」；deriver 对不存在 Room 的
	// room.completed 事件跳过，重放不复活）。返回受影响行数。
	DeleteByRoomSlugs(ctx context.Context, roomSlugs []string) (int64, error)
	Stats(ctx context.Context, userID uint) (completed int, inProgress int, err error)
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
	// Extend TTL 续期：expire_at 前移并自增 extend_count（单语句原子）。
	Extend(ctx context.Context, id uint, expireAt time.Time) error
	UpdateStatus(ctx context.Context, id uint, status model.InstanceStatus) error
	// ListExpired ExpireAt 已过期且仍 running 的实例（reaper 用）。
	ListExpired(ctx context.Context, now time.Time) ([]model.LabInstance, error)
	// List 全量实例分页（管理端容器面板用）。
	List(ctx context.Context, offset, limit int) ([]model.LabInstance, int64, error)
	// ListActive 全部用户的活跃（creating/running）实例（管理端概览用）。
	ListActive(ctx context.Context) ([]model.LabInstance, error)
	// ListActiveByChallenge 某题目全部用户的活跃实例（删除题目前销毁容器用）。
	ListActiveByChallenge(ctx context.Context, challengeID uint) ([]model.LabInstance, error)
	// ListByCompetition 竞赛容器分页（competition_id = 指定竞赛；管理端独立面板）。
	ListByCompetition(ctx context.Context, competitionID uint, offset, limit int) ([]model.LabInstance, int64, error)
	// FindByCheckpointToken 检测点回调解析：token 命中且实例仍活跃（creating/running），
	// 无则返回 nil（回调 404 语义）。
	FindByCheckpointToken(ctx context.Context, token string) (*model.LabInstance, error)
}

// InstanceLogRepo 容器实例生命周期日志（append-only：只 Create 与分页查询）。
type InstanceLogRepo interface {
	Create(ctx context.Context, l *model.InstanceLog) error
	List(ctx context.Context, offset, limit int) ([]model.InstanceLog, int64, error)
}

// CompetitionRepo 竞赛域聚合仓库（competition/team/participant/binding/
// warning/writeup/派生积分七表；SQLite 唯一实现）。
type CompetitionRepo interface {
	Create(ctx context.Context, c *model.Competition) error
	FindByID(ctx context.Context, id uint) (*model.Competition, error)
	List(ctx context.Context, offset, limit int) ([]model.Competition, int64, error)
	// UpdateSelective 局部更新（map 形式字段，零值可更新）。
	UpdateSelective(ctx context.Context, id uint, fields map[string]any) error
	// DeleteCascade 删除竞赛及其全部关联行（参赛者/队伍/题目绑定/反作弊警告/
	// WP/竞赛积分，一个事务内完成）。仅 scheduled/ended 可调（service 层校验）。
	DeleteCascade(ctx context.Context, id uint) error

	CreateTeam(ctx context.Context, t *model.Team) error
	// FindTeamByName 竞赛内战队名查重（nil = 不存在）。
	FindTeamByName(ctx context.Context, competitionID uint, name string) (*model.Team, error)
	ListTeams(ctx context.Context, competitionID uint) ([]model.Team, error)

	CreateParticipant(ctx context.Context, p *model.Participant) error
	// FindParticipant 该竞赛该用户的参赛记录（nil = 未参赛）。
	FindParticipant(ctx context.Context, competitionID, userID uint) (*model.Participant, error)
	// FindParticipantByCode 邀请码解析（nil = 无效邀请码）。
	FindParticipantByCode(ctx context.Context, code string) (*model.Participant, error)
	ListParticipants(ctx context.Context, competitionID uint, offset, limit int) ([]model.Participant, int64, error)
	CountEntered(ctx context.Context, competitionID uint) (int64, error)
	// MarkEntered 入场（幂等：已入场返回 0）。
	MarkEntered(ctx context.Context, id uint) (int64, error)
	// UpdateParticipantIP 记录最近提交来源 IP（IP 异常检测基线）。
	UpdateParticipantIP(ctx context.Context, competitionID, userID uint, ip string) error

	// BindChallenges 幂等绑定题目（已绑定跳过），返回新增数。
	BindChallenges(ctx context.Context, competitionID uint, challengeIDs []uint) (int64, error)
	UnbindChallenges(ctx context.Context, competitionID uint, challengeIDs []uint) (int64, error)
	ListBoundChallengeIDs(ctx context.Context, competitionID uint) ([]uint, error)

	CreateWarning(ctx context.Context, w *model.CheatWarning) error
	ListWarnings(ctx context.Context, competitionID uint) ([]model.CheatWarning, error)

	UpsertWriteup(ctx context.Context, w *model.Writeup) error
	ListWriteups(ctx context.Context, competitionID uint) ([]model.Writeup, error)
	// FindWriteup 单份 WP（nil = 不存在）。
	FindWriteup(ctx context.Context, id uint) (*model.Writeup, error)

	// AddScore 竞赛派生积分首通插入（重复返回 0 行）。仅事件派生器调用。
	AddScore(ctx context.Context, rec *model.CompetitionScore) (int64, error)
	// ListScores 竞赛全部派生积分行（排行榜聚合）。
	ListScores(ctx context.Context, competitionID uint) ([]model.CompetitionScore, error)

	// ActiveCompetitions 当前 running 的竞赛（入场锁定判定用）。
	ActiveCompetitions(ctx context.Context, now time.Time) ([]model.Competition, error)
}

