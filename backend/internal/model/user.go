// Package model 定义全部 gorm 实体。本包只有字段与标签，禁止业务方法（project-conventions skill）。
package model

import "time"

// GlobalRole 全局角色（rbac-model skill：student/teacher/admin 三级，禁止随意新增）。
type GlobalRole string

const (
	GlobalRoleStudent GlobalRole = "student"
	GlobalRoleTeacher GlobalRole = "teacher"
	GlobalRoleAdmin   GlobalRole = "admin"
)

// Rank 返回角色等级，用于"角色足够"判断（admin 短路通过）。
func (r GlobalRole) Rank() int {
	switch r {
	case GlobalRoleAdmin:
		return 3
	case GlobalRoleTeacher:
		return 2
	default:
		return 1
	}
}

// User 平台用户。
type User struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Username     string     `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string     `json:"-"`
	GlobalRole   GlobalRole `gorm:"size:16;index" json:"global_role"`
	CreatedAt    time.Time  `json:"created_at"`
}

// CourseRole 课程级角色：同一人在不同 Topic 域可拥有不同角色（双层 RBAC，rbac-model skill）。
type CourseRole struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"uniqueIndex:idx_user_topic"`
	TopicSlug string    `gorm:"uniqueIndex:idx_user_topic;size:128"`
	Role      string    `gorm:"size:16"` // teacher/student
	CreatedAt time.Time `json:"created_at"`
}
