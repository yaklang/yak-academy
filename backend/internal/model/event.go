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
	// AWD 攻击事实（运营审计）：提交即入 pending 事件，worker 判定后写 judged。
	EventAwdAttackSubmitted EventType = "awd.attack.submitted"
	EventAwdAttackJudged    EventType = "awd.attack.judged"
	// AWD 战报事实：提交/评审各写一条事件。
	EventAwdReportSubmitted EventType = "awd.report.submitted"
	EventAwdReportReviewed  EventType = "awd.report.reviewed"
	// 考试作答事实：开始/交卷/评分各写一条事件；ExamStanding 由派生器重建。
	EventExamAttemptStarted   EventType = "exam.attempt.started"
	EventExamAttemptSubmitted EventType = "exam.attempt.submitted"
	EventExamAttemptGraded    EventType = "exam.attempt.graded"
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
	UserID     uint `gorm:"primaryKey"`
	Score      int  `json:"score"`
	SolveCount int  `json:"solve_count"`
	UpdatedAt  time.Time
}

// Progress 学习进度（派生表）：Room/路径完成率，来源同上。
// 主键 = (user_id, room_slug)：同一用户对多个 Room 各有一行。
type Progress struct {
	UserID    uint   `gorm:"primaryKey;autoIncrement:false"`
	RoomSlug  string `gorm:"primaryKey;size:128"`
	Completed bool
	UpdatedAt time.Time
}
