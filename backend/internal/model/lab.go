package model

import "time"

// InstanceStatus 题目环境实例状态（env 模块，第一版仅接口 + stub）。
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
	Status      InstanceStatus `gorm:"size:16;index" json:"status"`
	Endpoint    string         `json:"endpoint"`            // 实例访问地址
	ExpireAt    time.Time      `gorm:"index" json:"expire_at"` // TTL 回收时间
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}