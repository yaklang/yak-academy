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
	// Phone 注册手机号（自助注册凭据，唯一；竞赛导入建号可为空）。不参与登录。
	// SQLite 唯一索引允许多个 NULL，但空串不空——竞赛批量建号的空 Phone 会撞唯一索引，
	// 故唯一性排除空串（手机号已注册的查重由 service FindByPhone 兜住）。
	Phone string `gorm:"index:idx_users_phone,unique,where:phone <> ''" json:"phone"`
	// Bio 描述简介（账号信息面板，≤500 字）。
	Bio string `gorm:"size:500" json:"bio"`
	// AvatarFile 头像落盘文件名（avatars/<userID>/<file>，空 = 未上传）。
	AvatarFile string `gorm:"size:200" json:"avatar_file"`
	// TokenInvalidBefore 改密/管理员重置后置为当时时刻：签发时间早于该值的
	// token 一律拒绝（全量失效，无需逐个撤销）。零值 = 不失效。
	TokenInvalidBefore time.Time `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
}

// Mail 站内邮件（只收不发）：用户收件箱，来源为系统投递（如竞赛邀请）。
// 用户侧只读 + 标记已读；投递入口在 service 层（Deliver），不经 HTTP。
type Mail struct {
	ID     uint   `gorm:"primaryKey"`
	UserID uint   `gorm:"index"`
	Title  string `gorm:"size:200"`
	BodyMd string `gorm:"size:4000"`
	// Kind 邮件类型：invite = 竞赛邀请（附邀请码）。
	Kind string `gorm:"size:32"`
	// CompetitionID / InviteCode 仅 kind=invite 时有意义，其余为零值。
	CompetitionID uint
	InviteCode    string `gorm:"size:64"`
	// Read 已读标记（用户侧唯一可写状态）。
	Read      bool `gorm:"index"`
	CreatedAt time.Time
}

// TokenRevocation 登出撤销记录：按 JWT 的 jti 吊销单个 token。
// 撤销是鉴权状态而非派生数据，不进事件表（event-sourcing-rules skill：
// 事件表只承载积分/进度等可重放事实源）。
type TokenRevocation struct {
	ID        uint      `gorm:"primaryKey"`
	JTI       string    `gorm:"uniqueIndex;size:64"`
	UserID    uint      `gorm:"index"`
	ExpiresAt time.Time `gorm:"index"` // token 自然过期后可清理
	CreatedAt time.Time
}

// SmsCode 注册验证码（自助注册，手机验证码核销）。鉴权辅助状态而非派生数据，
// 不进事件表（同 TokenRevocation）。核销一次性：Used 置位后不可重复注册。
type SmsCode struct {
	ID        uint      `gorm:"primaryKey"`
	Phone     string    `gorm:"size:20;index"`
	Code      string    `gorm:"size:6"`
	ExpiresAt time.Time `gorm:"index"` // 5 分钟有效
	Used      bool      // 核销一次性标记
	CreatedAt time.Time
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
