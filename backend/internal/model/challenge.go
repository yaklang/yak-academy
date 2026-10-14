package model

import "time"

// ChallengeType 题目判定类型（多态 type 列，架构文档 §3.3）。
type ChallengeType string

const (
	ChallengeStatic ChallengeType = "static" // 静态 flag
	ChallengeRegex  ChallengeType = "regex"  // 正则 flag
)

// FlagMode flag 作用域：shared=题目级共享；instance=每容器实例唯一。
type FlagMode string

const (
	FlagModeShared   FlagMode = "shared"
	FlagModeInstance FlagMode = "instance"
)

// Challenge 独立题目资源（管理员经 challenge.json / challenge.yml 上传）。
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
	// FlagMode shared|instance；instance 时 Flag.Value 为含 {token} 的模板。
	FlagMode FlagMode `gorm:"size:16;default:shared" json:"flag_mode"`
	// 容器环境定义（challenge.json env 段，可选）。Image 非空表示该题有容器环境。
	Image string `gorm:"size:256" json:"image"`
	Port  int    `json:"port"`
	// Links 题目外链（JSON 数组 [{name,url}]，配套工具/参考资料）。
	Links string `gorm:"type:text" json:"-"`
	// TutorialSlug 可选关联教程 Room slug（教题关联：题目可指回配套教程）。
	TutorialSlug string `gorm:"index;size:128" json:"tutorial_slug"`
	CreatedAt time.Time      `json:"created_at"`
}

// ChallengeLink 题目外链条目（Challenge.Links JSON 数组元素）。
type ChallengeLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ChallengeAttachment 题目附件元数据。文件本体落盘附件目录
// ACADEMY_ATTACHMENTS_DIR/<slug>/<filename>，学生端凭 JWT 下载。
// JSON 按契约输出 filename/size/created_at（id/challenge_id 是库内主键，不外泄）。
type ChallengeAttachment struct {
	ID          uint      `gorm:"primaryKey" json:"-"`
	ChallengeID uint      `gorm:"uniqueIndex:idx_chal_attach;" json:"-"`
	Filename    string    `gorm:"uniqueIndex:idx_chal_attach;size:255" json:"filename"` // 规范化文件名（下载 URL 段）
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"created_at"`
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
