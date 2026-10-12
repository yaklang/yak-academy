package model

import "time"

// LearningPath 学习路径：Room 的有序视图，PathItem 只引用 room_slug，
// 禁止复制 Room 内容字段（teaching-integration skill：路径引用而非复制）。
type LearningPath struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Title     string    `gorm:"size:256" json:"title"`
	CreatedAt time.Time `json:"created_at"`
}

// PathItem 路径项：order 在 path 内唯一，调序只改 order 不动 Room。
type PathItem struct {
	ID       uint   `gorm:"primaryKey"`
	PathID   uint   `gorm:"uniqueIndex:idx_path_order"`
	Order    int    `gorm:"uniqueIndex:idx_path_order"`
	RoomSlug string `gorm:"size:128;index"`
}
