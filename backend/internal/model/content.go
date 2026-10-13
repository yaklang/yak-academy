package model

import "time"

// PackageStatus 内容包状态机：draft → published → archived
// （唯一合法迁移，见 content-package-spec skill）。
type PackageStatus string

const (
	PackageDraft     PackageStatus = "draft"
	PackagePublished PackageStatus = "published"
	PackageArchived  PackageStatus = "archived"
)

// ContentPackage 一次上传对应一个包；同包再上传产生新 ContentVersion。
type ContentPackage struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Name          string    `gorm:"uniqueIndex;size:128" json:"name"`
	LatestVersion string    `json:"latest_version"`
	Status        PackageStatus `gorm:"size:16;index" json:"status"`
	Error         string     `json:"error,omitempty"` // 管线失败原因
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ContentVersion 包的历史版本，支持发布后回滚（旧版本 archived）。
type ContentVersion struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	PackageID uint      `gorm:"index" json:"-"`
	Version   string    `gorm:"size:32" json:"version"` // 语义化版本
	Status    PackageStatus `gorm:"size:16;index" json:"status"`
	Checksum  string    `gorm:"size:64" json:"checksum"` // zip sha256
	// Payload 解析产物的 JSON 序列化（Upload 暂存，Publish 时从此写入内容表）。
	Payload   string    `gorm:"type:text" json:"-"`
	CreatedAt time.Time `json:"created_at"`
}
