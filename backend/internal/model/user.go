// Package model 定义全部 gorm 实体。本包只有字段与标签，禁止业务方法（project-conventions skill）。
package model

import "time"

// GlobalRole 全局角色（rbac-model skill：仅 student / admin 两档）。
// student = 普通人（学习端）；admin = 管理员（管理端全部能力）。
// 历史值 teacher 在迁移中降为 student，禁止再写入。
type GlobalRole string

const (
	GlobalRoleStudent GlobalRole = "student"
	GlobalRoleAdmin   GlobalRole = "admin"

	// GlobalRoleTeacher 已废弃，仅兼容读旧数据；Normalize() 会降为 student。
	GlobalRoleTeacher GlobalRole = "teacher"
)

// Normalize 将角色收敛为合法两档（teacher/未知 → student）。
func (r GlobalRole) Normalize() GlobalRole {
	if r == GlobalRoleAdmin {
		return GlobalRoleAdmin
	}
	return GlobalRoleStudent
}

// Rank 返回角色等级：admin=2，普通人=1（用于"角色足够"判断）。
func (r GlobalRole) Rank() int {
	if r.Normalize() == GlobalRoleAdmin {
		return 2
	}
	return 1
}

// User 平台用户。
type User struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Username     string     `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string     `json:"-"`
	GlobalRole   GlobalRole `gorm:"size:16;index" json:"global_role"`
	CreatedAt    time.Time  `json:"created_at"`
}

// CourseRole 已废弃的课程级角色表（历史双层 RBAC 遗留）。
// 两档角色模型下不再参与鉴权；表保留以免破坏既有库，新代码禁止依赖。
type CourseRole struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"uniqueIndex:idx_user_topic"`
	TopicSlug string    `gorm:"uniqueIndex:idx_user_topic;size:128"`
	Role      string    `gorm:"size:16"`
	CreatedAt time.Time `json:"created_at"`
}
