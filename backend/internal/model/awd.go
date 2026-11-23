package model

import "time"

// AWD（Attack With Defense）独立域：只复用 Competition/Participant/Team/User，
// 不引用 Challenge/CompetitionScore/CTF judge（计划 2「独立原则」）。

// AwdCompetitorType 竞域单位：team = 以战队为参赛单位；user = 个人赛。
type AwdCompetitorType string

const (
	AwdCompetitorTeam AwdCompetitorType = "team"
	AwdCompetitorUser AwdCompetitorType = "user"
)

// Valid 是否合法竞域单位。
func (t AwdCompetitorType) Valid() bool {
	return t == AwdCompetitorTeam || t == AwdCompetitorUser
}

// AwdGameStatus 比赛生命周期状态（落库；与 Competition 的时间派生状态解耦，
// 由管理员显式 start/pause/resume/end）。
type AwdGameStatus string

const (
	AwdGamePending AwdGameStatus = "pending"
	AwdGameRunning AwdGameStatus = "running"
	AwdGamePaused  AwdGameStatus = "paused"
	AwdGameEnded   AwdGameStatus = "ended"
)

// Valid 是否合法比赛状态。
func (s AwdGameStatus) Valid() bool {
	return s == AwdGamePending || s == AwdGameRunning || s == AwdGamePaused || s == AwdGameEnded
}

// AwdRoundStatus 回合状态。
type AwdRoundStatus string

const (
	AwdRoundPending AwdRoundStatus = "pending"
	AwdRoundRunning AwdRoundStatus = "running"
	AwdRoundSettled AwdRoundStatus = "settled"
)

// AwdGame AWD 比赛配置与生命周期：一场 format=awd 的 Competition 对应一条。
type AwdGame struct {
	ID             uint              `gorm:"primaryKey" json:"id"`
	CompetitionID  uint              `gorm:"uniqueIndex" json:"competition_id"`
	Status         AwdGameStatus     `gorm:"size:16;index;default:pending" json:"status"`
	CompetitorType AwdCompetitorType `gorm:"size:16;default:team" json:"competitor_type"`
	// RoundSeconds 单回合秒数；由 round engine 生成 AwdRound。
	RoundSeconds int `gorm:"not null;default:300" json:"round_seconds"`
	// CurrentRound 已生成的最大回合号（0 = 尚未开赛）。
	CurrentRound int `gorm:"not null;default:0" json:"current_round"`
	// ScoringConfig 计分参数 JSON（SLA/攻击/防守/checkdown 权重，公式见 docs）。
	ScoringConfig string     `gorm:"type:text" json:"scoring_config"`
	StartedAt     *time.Time `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// AwdService AWD 靶机服务定义：一个服务 = 一份镜像 + 端口 + checker 配置。
// 服务包上传（awd-service.json + Dockerfile）构建镜像后 upsert 本表。
type AwdService struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	CompetitionID uint   `gorm:"uniqueIndex:idx_awd_service_comp_slug;index" json:"competition_id"`
	Slug          string `gorm:"uniqueIndex:idx_awd_service_comp_slug;size:64" json:"slug"`
	Name          string `gorm:"size:128" json:"name"`
	Image         string `gorm:"size:255" json:"image"`
	// PortsJSON 服务端口列表 JSON：[{"name":"web","port":80,"protocol":"tcp"}]。
	PortsJSON string `gorm:"type:text" json:"ports_json"`
	// CheckerConfig checker 配置 JSON（type=http/tcp/command + 参数）。
	CheckerConfig string `gorm:"type:text" json:"checker_config"`
	BaseScore     int    `gorm:"not null;default:100" json:"base_score"`
	// SlaWeight SLA 权重（百分比；默认 100 = SLA 全额）。
	SlaWeight int `gorm:"not null;default:100" json:"sla_weight"`
	// AttackPoints 每回合攻击分池（按本轮不同攻击者均分）。
	AttackPoints int `gorm:"not null;default:100" json:"attack_points"`
	// DefenseLoss 每个受害服务每回合扣分（每服务每回合只扣一次）。
	DefenseLoss int `gorm:"not null;default:50" json:"defense_loss"`
	// CheckdownPenalty PUT/CHECK 失败直接扣分。
	CheckdownPenalty int `gorm:"not null;default:50" json:"checkdown_penalty"`
	// FlagLifetimeRounds flag 有效回合数（1 = 仅当回合）。
	FlagLifetimeRounds int       `gorm:"not null;default:1" json:"flag_lifetime_rounds"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// AwdTeamService 某竞域单位的一套靶机实例（容器 + 内部地址 + 端口）。
type AwdTeamService struct {
	ID             uint              `gorm:"primaryKey" json:"id"`
	GameID         uint              `gorm:"uniqueIndex:idx_awd_ts;index" json:"game_id"`
	ServiceID      uint              `gorm:"uniqueIndex:idx_awd_ts;index" json:"service_id"`
	CompetitorType AwdCompetitorType `gorm:"uniqueIndex:idx_awd_ts;size:16" json:"competitor_type"`
	CompetitorID   uint              `gorm:"uniqueIndex:idx_awd_ts;index" json:"competitor_id"`
	ContainerID    string            `gorm:"size:128" json:"container_id"`
	// InternalHost 竞赛内部网络中的容器名/别名（选手经攻击盒访问）。
	InternalHost string `gorm:"size:128" json:"internal_host"`
	PortsJSON    string `gorm:"type:text" json:"ports_json"`
	// Status pending/running/stopped/failed。
	Status      string     `gorm:"size:16;index" json:"status"`
	LastCheckAt *time.Time `json:"last_check_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// AwdRound 回合：由 round engine 单 goroutine 生成（含 flag 播种）。
type AwdRound struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	GameID    uint           `gorm:"uniqueIndex:idx_awd_round;index" json:"game_id"`
	Number    int            `gorm:"uniqueIndex:idx_awd_round" json:"number"`
	Status    AwdRoundStatus `gorm:"size:16;index" json:"status"`
	StartsAt  time.Time      `json:"starts_at"`
	EndsAt    time.Time      `json:"ends_at"`
	Seed      string         `gorm:"size:64" json:"-"`
	CreatedAt time.Time      `json:"created_at"`
}

// AwdFlag 每竞域单位每服务的回合 flag。Value 仅 checker 读取（不返回前端）；
// ValueHash 用于攻击提交比对；ValidUntilRound 支持 flag 生命周期 >1 回合。
type AwdFlag struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	RoundID         uint      `gorm:"uniqueIndex:idx_awd_flag_round_ts;index" json:"round_id"`
	TeamServiceID   uint      `gorm:"uniqueIndex:idx_awd_flag_round_ts;index" json:"team_service_id"`
	Value           string    `gorm:"size:255" json:"-"`
	ValueHash       string    `gorm:"size:64;index" json:"-"`
	ValidUntilRound int       `json:"valid_until_round"`
	PlacedAt        time.Time `json:"placed_at"`
}

// AwdCheck checker 执行明细（运营事实表；与事件双写）。
type AwdCheck struct {
	ID            uint `gorm:"primaryKey" json:"id"`
	RoundID       uint `gorm:"index" json:"round_id"`
	TeamServiceID uint `gorm:"index" json:"team_service_id"`
	// Action check/put/get。
	Action string `gorm:"size:16;index" json:"action"`
	// Status ok/fail/error。
	Status    string    `gorm:"size:16;index" json:"status"`
	LatencyMS int       `json:"latency_ms"`
	Output    string    `gorm:"type:text" json:"output"`
	CreatedAt time.Time `json:"created_at"`
}

// AwdAttackStatus 攻击提交判定状态。
type AwdAttackStatus string

const (
	AwdAttackPending  AwdAttackStatus = "pending"
	AwdAttackAccepted AwdAttackStatus = "accepted"
	AwdAttackRejected AwdAttackStatus = "rejected"
)

// AwdAttack 攻击提交：先入 pending，worker 校验后 accepted/rejected。
// 同一 flag 只允许首个 accepted；重复/自攻/过期/旗标不属于他人均拒绝。
type AwdAttack struct {
	ID                  uint              `gorm:"primaryKey" json:"id"`
	GameID              uint              `gorm:"index" json:"game_id"`
	RoundID             uint              `gorm:"index" json:"round_id"`
	AttackerType        AwdCompetitorType `gorm:"size:16;index" json:"attacker_type"`
	AttackerID          uint              `gorm:"index" json:"attacker_id"`
	VictimTeamServiceID uint              `gorm:"index" json:"victim_team_service_id"`
	FlagID              uint              `gorm:"index" json:"flag_id"`
	Status              AwdAttackStatus   `gorm:"size:16;index" json:"status"`
	Reason              string            `gorm:"size:255" json:"reason"`
	SubmittedAt         time.Time         `json:"submitted_at"`
	JudgedAt            *time.Time        `json:"judged_at"`
}

// AwdScoreEvent 得分账本（append-only）：Standing 只能由本表派生。
// IdempotencyKey 唯一，保证回合结算/攻击判定重放不重复计分。
type AwdScoreEvent struct {
	ID             uint              `gorm:"primaryKey" json:"id"`
	CompetitionID  uint              `gorm:"index" json:"competition_id"`
	GameID         uint              `gorm:"index" json:"game_id"`
	RoundID        uint              `gorm:"index" json:"round_id"`
	CompetitorType AwdCompetitorType `gorm:"size:16;index" json:"competitor_type"`
	CompetitorID   uint              `gorm:"index" json:"competitor_id"`
	// Source sla/attack/defense/checkdown/report/manual。
	Source         string    `gorm:"size:16;index" json:"source"`
	Delta          int       `json:"delta"`
	IdempotencyKey string    `gorm:"uniqueIndex;size:128" json:"idempotency_key"`
	Reason         string    `gorm:"size:255" json:"reason"`
	CreatedAt      time.Time `gorm:"index" json:"created_at"`
}

// AwdStanding 派生排名（只由 AwdScoreEvent 聚合；禁止业务直接改写）。
type AwdStanding struct {
	ID             uint              `gorm:"primaryKey" json:"id"`
	CompetitionID  uint              `gorm:"uniqueIndex:idx_awd_standing;index" json:"competition_id"`
	CompetitorType AwdCompetitorType `gorm:"uniqueIndex:idx_awd_standing;size:16" json:"competitor_type"`
	CompetitorID   uint              `gorm:"uniqueIndex:idx_awd_standing;index" json:"competitor_id"`
	Score          int               `json:"score"`
	AttackScore    int               `json:"attack_score"`
	DefenseLoss    int               `json:"defense_loss"`
	SlaScore       int               `json:"sla_score"`
	LastEventAt    *time.Time        `json:"last_event_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// AwdIncident 运营事件（PUT 失败、check 连续失败、flag 泄漏等）。
type AwdIncident struct {
	ID             uint              `gorm:"primaryKey" json:"id"`
	CompetitionID  uint              `gorm:"index" json:"competition_id"`
	CompetitorType AwdCompetitorType `gorm:"size:16;index" json:"competitor_type"`
	CompetitorID   uint              `gorm:"index" json:"competitor_id"`
	ServiceID      uint              `gorm:"index" json:"service_id"`
	Kind           string            `gorm:"size:32;index" json:"kind"`
	Detail         string            `gorm:"type:text" json:"detail"`
	CreatedAt      time.Time         `gorm:"index" json:"created_at"`
}

// AwdAttackBox 每个竞域单位的 SSH 攻击盒（选手经它访问内部竞赛网络）。
// 密码加密存储，不返回前端（service 负责解密展示/轮换）。
type AwdAttackBox struct {
	ID             uint              `gorm:"primaryKey" json:"id"`
	CompetitionID  uint              `gorm:"uniqueIndex:idx_awd_attackbox;index" json:"competition_id"`
	CompetitorType AwdCompetitorType `gorm:"uniqueIndex:idx_awd_attackbox;size:16" json:"competitor_type"`
	CompetitorID   uint              `gorm:"uniqueIndex:idx_awd_attackbox" json:"competitor_id"`
	ContainerID    string            `gorm:"size:128" json:"container_id"`
	SSHHost        string            `gorm:"size:128" json:"ssh_host"`
	SSHPort        int               `json:"ssh_port"`
	SSHUser        string            `gorm:"size:64" json:"ssh_user"`
	SSHPasswordEnc string            `gorm:"size:255" json:"-"`
	Status         string            `gorm:"size:16;index" json:"status"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// AwdReport 赛后战报（Markdown + 可选 zip）；一个竞域单位一份。
type AwdReport struct {
	ID             uint              `gorm:"primaryKey" json:"id"`
	CompetitionID  uint              `gorm:"uniqueIndex:idx_awd_report;index" json:"competition_id"`
	CompetitorType AwdCompetitorType `gorm:"uniqueIndex:idx_awd_report;size:16" json:"competitor_type"`
	CompetitorID   uint              `gorm:"uniqueIndex:idx_awd_report;index" json:"competitor_id"`
	ContentMD      string            `gorm:"type:text" json:"content_md"`
	ZipFile        string            `gorm:"size:255" json:"zip_file"`
	ZipName        string            `gorm:"size:255" json:"zip_name"`
	// Status submitted/reviewed。
	Status    string    `gorm:"size:16;index" json:"status"`
	Score     int       `json:"score"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AwdReportReview 战报 rubric 评审（保留每次评审，最终分取最后一条）。
type AwdReportReview struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ReportID   uint      `gorm:"index" json:"report_id"`
	ReviewerID uint      `gorm:"index" json:"reviewer_id"`
	RubricJSON string    `gorm:"type:text" json:"rubric_json"`
	Score      int       `json:"score"`
	Comment    string    `gorm:"type:text" json:"comment"`
	CreatedAt  time.Time `json:"created_at"`
}
