package model

import "time"

// ChallengeType 题目判定类型（多态 type 列，架构文档 §3.3）。
type ChallengeType string

const (
	ChallengeStatic ChallengeType = "static" // 静态 flag
	ChallengeRegex  ChallengeType = "regex"  // 正则 flag
)

// Challenge 内嵌于 Room 的题目（teaching-integration skill：不存在无 Room 的 Challenge）。
type Challenge struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	RoomSlug       string         `gorm:"uniqueIndex;size:128" json:"room_slug"`
	Title          string         `gorm:"size:256" json:"title"`
	Type           ChallengeType  `gorm:"size:16" json:"type"`
	Score          int            `json:"score"`  // 初始分值，动态计分基于 solve_count 派生
	CreatedAt      time.Time      `json:"created_at"`
}

// Flag 题目的判定答案。比较必须使用恒时比较（event-sourcing-rules skill：禁止 == ）。
type Flag struct {
	ID          uint          `gorm:"primaryKey"`
	ChallengeID uint          `gorm:"index"`
	Type        ChallengeType `gorm:"size:16"`
	Value       string        `gorm:"size:512"` // static: 明文；regex: 正则
}

// Hint 提示，扣分解锁（解锁写 hint.unlocked 事件）。
type Hint struct {
	ID          uint   `gorm:"primaryKey"`
	ChallengeID uint   `gorm:"index"`
	Title       string `gorm:"size:128"`
	Content     string
	Cost        int // 扣减积分
}

// SubmissionStatus 提交状态：pending → correct/incorrect（异步判定）。
type SubmissionStatus string

const (
	SubmissionPending    SubmissionStatus = "pending"
	SubmissionCorrect    SubmissionStatus = "correct"
	SubmissionIncorrect  SubmissionStatus = "incorrect"
)

// Submission 一次答案提交。判定由 worker 异步完成，HTTP 路径禁止同步判定。
type Submission struct {
	ID          uint             `gorm:"primaryKey" json:"id"`
	UserID      uint             `gorm:"index" json:"user_id"`
	ChallengeID uint             `gorm:"index" json:"challenge_id"`
	Answer      string           `json:"-"`
	Status      SubmissionStatus `gorm:"size:16;index" json:"status"`
	Score       int              `json:"score"` // 判定通过后写入（submission.judged 事件派生）
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}
