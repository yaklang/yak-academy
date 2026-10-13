package model

import "time"

// ChallengeType 题目判定类型（多态 type 列，架构文档 §3.3）。
type ChallengeType string

const (
	ChallengeStatic ChallengeType = "static" // 静态 flag
	ChallengeRegex  ChallengeType = "regex"  // 正则 flag
)

// Challenge 独立题目资源（管理员经 challenge.json 上传）。
// tutorial_slug 可选关联教程 Room；不关联的纯题目也合法。
type Challenge struct {
	ID       uint          `gorm:"primaryKey" json:"id"`
	Slug     string        `gorm:"uniqueIndex;size:128" json:"slug"`
	Title    string        `gorm:"size:256" json:"title"`
	Category string        `gorm:"index;size:128" json:"category"` // 分类 slug，训练场分组键
	Difficulty Difficulty `gorm:"size:16" json:"difficulty"`
	// Description 题目描述 Markdown（背景/任务目标）。
	Description string `gorm:"type:text" json:"description_md"`
	Type      ChallengeType `gorm:"size:16" json:"type"`
	Score     int           `json:"score"` // 初始分值，动态计分基于 solve_count 派生
	// 容器环境定义（challenge.json env 段，可选）。Image 非空表示该题有容器环境。
	Image string `gorm:"size:256" json:"image"`
	Port  int    `json:"port"`
	// TutorialSlug 可选关联教程 Room slug（教题关联：题目可指回配套教程）。
	TutorialSlug string `gorm:"index;size:128" json:"tutorial_slug"`
	CreatedAt time.Time      `json:"created_at"`
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
	ID          uint   `gorm:"primaryKey" json:"id"`
	ChallengeID uint   `gorm:"index" json:"challenge_id"`
	Title       string `gorm:"size:128" json:"title"`
	Content     string `json:"content"`
	Cost        int    `json:"cost"` // 扣减积分
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
