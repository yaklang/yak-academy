package model

import "time"

// InstanceStatus 题目环境实例状态（env 模块：docker 驱动 / noop 兜底）。
type InstanceStatus string

const (
	InstanceCreating  InstanceStatus = "creating"
	InstanceRunning   InstanceStatus = "running"
	InstanceStopped   InstanceStatus = "stopped"
	InstanceDestroyed InstanceStatus = "destroyed"
)

// LabInstance 题目环境实例：user × challenge 维度，生命周期由 EnvService + 驱动管理。
type LabInstance struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	UserID      uint           `gorm:"index" json:"user_id"`
	ChallengeID uint           `gorm:"index" json:"challenge_id"`
	// CompetitionID 所属竞赛（0 = 训练场实例）。竞赛容器走独立管理面板。
	CompetitionID uint           `gorm:"index;default:0" json:"competition_id"`
	Status        InstanceStatus `gorm:"size:16;index" json:"status"`
	Endpoint    string         `json:"endpoint"`               // 实例访问地址
	ExpireAt    time.Time      `gorm:"index" json:"expire_at"` // TTL 回收时间
	// ExtendCount 已续期次数（治理上限 2 次：初始 1h + 2×1h = 最长 3h 占用）。
	ExtendCount int `gorm:"default:0" json:"extend_count"`
	// FlagValue 实例唯一 flag（flag_mode=instance 时写入；禁止出现在 API JSON）。
	FlagValue string `gorm:"size:512" json:"-"`
	// CheckpointToken 检测点回调令牌：实例创建时随机生成，注入容器
	// CHECKPOINT_TOKEN/CHECKPOINT_URL；题目内 checker 持它 POST /checkpoints/{token}
	// 触发自动判定（token 即能力凭证，无需 JWT；禁止出现在 API JSON）。
	CheckpointToken string    `gorm:"size:64;index" json:"-"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// InstanceAction 实例生命周期动作（日志表）。
type InstanceAction string

const (
	InstanceActionStarted InstanceAction = "started"
	InstanceActionStopped InstanceAction = "stopped"
)

// InstanceLog 容器实例生命周期日志（append-only）：每次 start/stop 落一行，
// 管理端容器日志表格展示。与 Event 表的事件双写不冲突——Event 服务全局
// 事件溯源，本表是 env 域的操作审计视图（含富化字段，避免管理端反查 join）。
type InstanceLog struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	InstanceID  uint           `gorm:"index" json:"instance_id"`
	UserID      uint           `gorm:"index" json:"user_id"`
	ChallengeID uint           `gorm:"index" json:"challenge_id"`
	Action      InstanceAction `gorm:"size:16;index" json:"action"`
	// Source 触发来源：owner（用户本人）/ admin（管理端强制）/ reaper（TTL 回收）。
	Source    string         `gorm:"size:16" json:"source"`
	Status    InstanceStatus `gorm:"size:16" json:"status"`  // 动作发生后的实例状态
	Detail    string         `gorm:"size:512" json:"detail"` // 失败原因等补充信息（可为空）
	CreatedAt time.Time      `gorm:"index" json:"created_at"`
}
