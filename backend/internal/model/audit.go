package model

import "time"

// AdminAuditLog 管理操作审计（append-only，禁止 UPDATE/DELETE）：管理端全部
// 写操作（内容包上传/发布/回滚/归档/删除、题目与附件、竞赛生命周期、用户
// 管理、引擎与镜像操作、备份）逐条记录操作者与目标。与 Event 表分工：
// Event 是学生行为的事件溯源事实源（积分/进度派生参与重放），本表是管理域
// 审计视图、不参与重放（对照 instance_logs 的双轨先例）。
type AdminAuditLog struct {
	ID uint `gorm:"primaryKey" json:"id"`
	// AdminID/AdminName 操作管理员快照（改名后历史行保持操作时身份）。
	AdminID   uint   `gorm:"index" json:"admin_id"`
	AdminName string `gorm:"size:64" json:"admin_name"`
	// Action 动作名（点分域：content.publish / challenge.delete / user.roles…）。
	Action string `gorm:"size:64;index" json:"action"`
	// TargetType/TargetID 操作目标（package/challenge/competition/user/…；
	// ID 为字符串：数字主键与 slug 文件名统一形态）。
	TargetType string `gorm:"size:32" json:"target_type"`
	TargetID   string `gorm:"size:128" json:"target_id"`
	// Detail 附加信息 JSON 文本（计数、目标值、选项；空 = 无附加）。
	Detail string `gorm:"type:text" json:"detail"`
	// IP 来源地址（去端口 host 部分）。
	IP        string    `gorm:"size:64" json:"ip"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
