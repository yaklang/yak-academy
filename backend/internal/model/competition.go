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

// Competition 竞赛实体：管理员创建并配置起止时间。
// 状态全程由时间派生；结束前已入场参赛者被锁定在竞赛入口（见 Participant）。
type Competition struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:128" json:"name"`
	Description string    `gorm:"size:2000" json:"description"`
	StartTime   time.Time `gorm:"index" json:"start_time"`
	EndTime     time.Time `gorm:"index" json:"end_time"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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
// 竞赛结束前该用户被锁定在竞赛入口（登出重进不可退出）。
type Participant struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	CompetitionID uint       `gorm:"uniqueIndex:idx_part_comp_user;index"`
	UserID        uint       `gorm:"uniqueIndex:idx_part_comp_user;index"`
	TeamID        uint       `gorm:"index"` // 0 = 个人参赛
	InviteCode    string     `gorm:"uniqueIndex;size:32" json:"-"`
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

// Writeup 比赛结束后的 WP 提交（每参赛者一份，可反复修改）。
type Writeup struct {
	ID            uint   `gorm:"primaryKey"`
	CompetitionID uint   `gorm:"uniqueIndex:idx_wp_comp_user;"`
	UserID        uint   `gorm:"uniqueIndex:idx_wp_comp_user;"`
	Content       string `gorm:"type:text" json:"content"`
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
	ChallengeID   uint
	Score         int
	SolvedAt      time.Time
}
