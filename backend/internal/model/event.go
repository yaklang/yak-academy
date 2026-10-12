package model

import "time"

// EventType 事件类型枚举（第一版，新增需更新 event-sourcing-rules skill）。
type EventType string

const (
	EventSubmissionCreated EventType = "submission.created"
	EventSubmissionJudged  EventType = "submission.judged"
	EventHintUnlocked      EventType = "hint.unlocked"
	EventRoomCompleted     EventType = "room.completed"
	EventLabStarted        EventType = "lab.instance.started"
	EventLabStopped        EventType = "lab.instance.stopped"
)

// Event append-only 事件表：只允许 INSERT，禁止任何 UPDATE/DELETE
// （event-sourcing-rules skill：修正以新事件表达）。
type Event struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"index" json:"user_id"`
	Type      EventType `gorm:"size:32;index" json:"type"`
	Payload   string    `gorm:"type:text" json:"payload"` // JSON
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// ScoreRecord 积分（派生表）：只能由派生器从 Event 增量更新，
// 业务代码禁止直接改写（event-sourcing-rules skill）。
type ScoreRecord struct {
	UserID      uint  `gorm:"primaryKey"`
	Score       int   `json:"score"`
	SolveCount  int   `json:"solve_count"`
	UpdatedAt   time.Time
}

// Progress 学习进度（派生表）：Room/路径完成率，来源同上。
type Progress struct {
	UserID    uint   `gorm:"primaryKey"`
	RoomSlug  string `gorm:"uniqueIndex:idx_user_room;size:128"`
	Completed bool
	UpdatedAt time.Time
}
