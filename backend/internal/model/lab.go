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

// LabInstance 题目环境实例：user × challenge 维度，Phase 3 由 Docker 驱动管理生命周期。
type LabInstance struct {
	ID          uint           `gorm:"primaryKey"`
	UserID      uint           `gorm:"index"`
	ChallengeID uint           `gorm:"index"`
	Status      InstanceStatus `gorm:"size:16"`
	Endpoint    string         // 实例访问地址（Phase 3）
	ExpireAt    time.Time      // 空闲回收时间（Phase 3）
	CreatedAt   time.Time
}