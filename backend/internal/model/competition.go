package model

import "time"

// CompetitionStatus 由当前时刻与起止时间派生（不落库）：
// scheduled=未开始 / running=进行中 / ended=已结束。
type CompetitionStatus string

const (
	CompetitionScheduled CompetitionStatus = "scheduled"
	CompetitionRunning   CompetitionStatus = "running"
	CompetitionEnded     CompetitionStatus = "ended"
)

// CompetitionKind 竞赛两档（创建时选定，此后不可更改）：
// public = 公开赛（竞赛广场展示、自助报名、已报名者免邀请码入场）；
// invitational = 邀请赛（默认；定向导入/一键生成参赛者，凭邀请码入场）。
type CompetitionKind string

const (
	CompetitionPublic       CompetitionKind = "public"
	CompetitionInvitational CompetitionKind = "invitational"
)

// Valid 是否合法档位。
func (k CompetitionKind) Valid() bool {
	return k == CompetitionPublic || k == CompetitionInvitational
}

// CompetitionFormat 竞赛形式（创建时选定，此后不可更改）：
// ctf = 解题赛（现有 Challenge/Submission 形态）；
// awd = 攻防赛（独立 Awd* 域）；exam = 在线考试（独立 Exam* 域）。
type CompetitionFormat string

const (
	CompetitionFormatCTF  CompetitionFormat = "ctf"
	CompetitionFormatAWD  CompetitionFormat = "awd"
	CompetitionFormatExam CompetitionFormat = "exam"
)

// Valid 是否合法竞赛形式。
func (f CompetitionFormat) Valid() bool {
	return f == CompetitionFormatCTF || f == CompetitionFormatAWD || f == CompetitionFormatExam
}

// CompetitionScoringMode CTF 计分模式（非 CTF 形式忽略）：
// dynamic = 现有按通过人数线性衰减；fixed = 按题目分值固定计分（固定分赛制）。
type CompetitionScoringMode string

const (
	CompetitionScoringDynamic CompetitionScoringMode = "dynamic"
	CompetitionScoringFixed   CompetitionScoringMode = "fixed"
)

// Valid 是否合法计分模式。
func (m CompetitionScoringMode) Valid() bool {
	return m == CompetitionScoringDynamic || m == CompetitionScoringFixed
}

// Competition 竞赛实体：管理员创建并配置起止时间。
// 状态全程由时间派生；结束前已入场的邀请赛参赛者被锁定在竞赛入口
// （公开赛报名入场不锁定，可自由进出，见 Participant）。
type Competition struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"uniqueIndex;size:128" json:"name"`
	Description string `gorm:"size:2000" json:"description"`
	// Kind 公开/邀请两档（见 CompetitionKind）；旧库行为 AutoMigrate 默认
	// 补 invitational（改动前创建的竞赛全部走定向导入语义）。
	Kind CompetitionKind `gorm:"size:16;index;default:invitational" json:"kind"`
	// Format 竞赛形式（见 CompetitionFormat）；旧库行默认补 ctf。
	Format CompetitionFormat `gorm:"size:16;index;default:ctf" json:"format"`
	// ScoringMode 仅 CTF 形式生效；旧库行默认补 dynamic。
	ScoringMode CompetitionScoringMode `gorm:"size:16;index;default:dynamic" json:"scoring_mode"`
	// RegistrationStart/RegistrationEnd 可选报名窗口（公开赛自助报名用；
	// 两者必须同时配置或同时为空；为空 = 沿用旧语义，报名开放到竞赛结束）。
	RegistrationStart *time.Time `json:"registration_start"`
	RegistrationEnd   *time.Time `json:"registration_end"`
	// BonusCap 单人正向附加分上限（0 = 不限制）；仅 CTF 生效。用于公告中
	// “AI 附加 +10 封顶”一类赛务规则，管理端追加附加分时强制校验。
	BonusCap  int       `gorm:"not null;default:0" json:"bonus_cap"`
	StartTime time.Time `gorm:"index" json:"start_time"`
	EndTime   time.Time `gorm:"index" json:"end_time"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RegistrationOpen 当前时刻是否在可报名窗口内（未配置窗口 = 不限制，
// 由 Register 另行校验竞赛未结束）。
func (c *Competition) RegistrationOpen(now time.Time) bool {
	if c.RegistrationStart != nil && now.Before(*c.RegistrationStart) {
		return false
	}
	if c.RegistrationEnd != nil && now.After(*c.RegistrationEnd) {
		return false
	}
	return true
}

// Status 当前时刻的派生状态。
func (c *Competition) Status(now time.Time) CompetitionStatus {
	switch {
	case now.Before(c.StartTime):
		return CompetitionScheduled
	case now.Before(c.EndTime):
		return CompetitionRunning
	default:
		return CompetitionEnded
	}
}

// Team 竞赛内战队：名称在竞赛内唯一；成员经 Participant 关联。
type Team struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CompetitionID uint      `gorm:"uniqueIndex:idx_team_comp_name"`
	Name          string    `gorm:"uniqueIndex:idx_team_comp_name;size:128" json:"name"`
	CreatedAt     time.Time `json:"created_at"`
}

// Participant 参赛人员：管理员批量导入时生成，携带唯一邀请码，
// 站内邀请邮件投递给本人；凭码入场（EnteredAt 置位）后，
// 邀请赛结束前该用户被锁定在竞赛入口（登出重进不可退出）；
// 公开赛报名入场不锁定，可随时返回平台。
type Participant struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	CompetitionID uint   `gorm:"uniqueIndex:idx_part_comp_user;index"`
	UserID        uint   `gorm:"uniqueIndex:idx_part_comp_user;index"`
	TeamID        uint   `gorm:"index"` // 0 = 个人参赛
	InviteCode    string `gorm:"uniqueIndex;size:32" json:"-"`
	// EnteredAt 入场时刻（nil = 尚未入场，仍可从邮箱邀请进入）。
	EnteredAt *time.Time `json:"-"`
	// LastIP 最近一次竞赛提交来源 IP（IP 异常检测基线；空 = 尚无提交）。
	LastIP    string    `gorm:"size:64" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// CompetitionChallenge 竞赛题目绑定：Challenge 仍是独立资源（teaching-integration
// skill），本表只表达"该题属于这场竞赛"的多对多关系。
type CompetitionChallenge struct {
	ID            uint `gorm:"primaryKey"`
	CompetitionID uint `gorm:"uniqueIndex:idx_comp_chal_bind;"`
	ChallengeID   uint `gorm:"uniqueIndex:idx_comp_chal_bind;"`
	CreatedAt     time.Time
}

// WarningKind 反作弊警告类型。
const (
	WarningCrossTeamFlag = "cross_team_flag" // 提交了其他队伍的实例 flag
	WarningIPAnomaly     = "ip_anomaly"      // 提交来源 IP 异常变化
)

// CheatWarning 反作弊警告：只在管理端展示（说明原因、人员、队伍）。
// 检测发生在判题/提交路径，本表只增不改。
type CheatWarning struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CompetitionID uint      `gorm:"index" json:"-"`
	UserID        uint      `gorm:"index" json:"user_id"`
	TeamID        uint      `json:"team_id"`
	Kind          string    `gorm:"size:32;index" json:"kind"`
	Reason        string    `gorm:"size:512" json:"reason"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
}

// Writeup 比赛结束后的 WP（单位 = 题目 × 队伍/个人，可反复修改）：
// 队伍模式全队共享一份（TeamID>0，任一队员可提交/修改，UserID 记最近修改人）；
// 个人参赛者 TeamID=0、按本人一份。Content 为 Markdown 正文，ZipFile/ZipName
// 为附带脚本 zip（落盘路径 + 原文件名，空串 = 未上传）。
type Writeup struct {
	ID            uint `gorm:"primaryKey"`
	CompetitionID uint `gorm:"uniqueIndex:idx_wp_scope;"`
	ChallengeID   uint `gorm:"uniqueIndex:idx_wp_scope;"`
	TeamID        uint `gorm:"uniqueIndex:idx_wp_scope;"`
	UserID        uint
	Content       string `gorm:"type:text" json:"content"`
	ZipFile       string `gorm:"size:255" json:"zip_file"`
	ZipName       string `gorm:"size:255" json:"zip_name"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CompetitionScore 竞赛积分（派生表）：只能由事件派生器从 submission.judged
// 事件增量更新（event-sourcing-rules skill），业务代码禁止直接改写。
// 一行 = 该用户在该竞赛中某题的首通得分；战队/个人排行榜由此聚合。
type CompetitionScore struct {
	ID            uint `gorm:"primaryKey"`
	CompetitionID uint `gorm:"uniqueIndex:idx_compscore;index"`
	UserID        uint `gorm:"uniqueIndex:idx_compscore"`
	TeamID        uint `gorm:"index"` // 冗余队伍快照（排行榜聚合免 join）
	// 唯一约束必须包含 ChallengeID：同一用户在竞赛内可通关多题，
	// 只是同一题不重复计分。
	ChallengeID uint `gorm:"uniqueIndex:idx_compscore"`
	Score       int
	SolvedAt    time.Time
}

// CtfAdjustment CTF 管理员附加分（append-only 源事实，不修改 CompetitionScore）：
// 排行榜 = 首通分合计 + 附加分合计；Points 可为负（纠正），正向合计受
// Competition.BonusCap 限制（0 = 不限）。Reason 必填，创建操作写 AdminAuditLog。
type CtfAdjustment struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CompetitionID uint      `gorm:"index" json:"competition_id"`
	UserID        uint      `gorm:"index" json:"user_id"`
	TeamID        uint      `gorm:"index" json:"team_id"` // 冗余队伍快照（0 = 个人）
	Points        int       `json:"points"`
	Reason        string    `gorm:"size:512" json:"reason"`
	CreatedBy     uint      `gorm:"index" json:"created_by"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
}

// QualificationMode 晋级筛选方式。
type QualificationMode string

const (
	QualificationMinScore   QualificationMode = "min_score"   // 最低分（含）
	QualificationTopN       QualificationMode = "top_n"       // 前 N 名（并列同名次一并晋级）
	QualificationTopPercent QualificationMode = "top_percent" // 前百分之 N（并列同名次一并晋级）
)

// Valid 是否合法晋级方式。
func (m QualificationMode) Valid() bool {
	return m == QualificationMinScore || m == QualificationTopN || m == QualificationTopPercent
}

// QualificationRule 晋级规则（预览不落库；应用时落一条规则 + 中选名单）。
// Source 是榜单来源竞赛，Target 是邀请赛目标竞赛（应用时把中选者导入为参赛者）。
type QualificationRule struct {
	ID                  uint              `gorm:"primaryKey" json:"id"`
	SourceCompetitionID uint              `gorm:"index" json:"source_competition_id"`
	TargetCompetitionID uint              `gorm:"index" json:"target_competition_id"`
	Mode                QualificationMode `gorm:"size:16" json:"mode"`
	Threshold           int               `json:"threshold"`
	CreatedBy           uint              `gorm:"index" json:"created_by"`
	CreatedAt           time.Time         `json:"created_at"`
	AppliedAt           *time.Time        `json:"applied_at"`
}

// QualificationEntry 晋级中选名单快照（规则应用时的名次与分数）。
type QualificationEntry struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	RuleID              uint      `gorm:"index" json:"rule_id"`
	SourceCompetitionID uint      `gorm:"index" json:"source_competition_id"`
	TargetCompetitionID uint      `gorm:"index" json:"target_competition_id"`
	UserID              uint      `gorm:"index" json:"user_id"`
	Rank                int       `json:"rank"`
	Score               int       `json:"score"`
	CreatedAt           time.Time `json:"created_at"`
}
