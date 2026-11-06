package model

import "time"

// SystemSetting 运行期可编辑的服务端配置（统一设置页）。一行一个配置域，
// Value 为域内 JSON（如 smtp）；密钥字段在 service 层加密后写入，表内不存明文。
// 与 Event/AdminAuditLog 分工：这是配置事实源，不参与事件重放；每次保存另记
// 一条管理审计。
type SystemSetting struct {
	Key       string    `gorm:"primaryKey;size:64" json:"key"`
	Value     string    `gorm:"type:text;not null" json:"-"`
	UpdatedBy uint      `gorm:"index" json:"updated_by"`
	UpdatedAt time.Time `gorm:"index" json:"updated_at"`
}
