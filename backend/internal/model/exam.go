package model

import "time"

// 考试（Exam）独立域：只复用 Competition/Participant/User，不引用 Awd* 或
// CTF CompetitionScore。一场 format=exam 的竞赛对应一份试卷；每位参赛者
// 单次作答，服务端计时，交卷后客观题即时评分、编程题复用 judge 的 exam scope。
// 答案/选项以 JSON 保存，避免为题型扩展反复改表；答案列永不下发选手端。

// ExamQuestionType 题型：单选/多选/判断/编程。
type ExamQuestionType string

const (
	ExamQuestionSingle    ExamQuestionType = "single"
	ExamQuestionMultiple  ExamQuestionType = "multiple"
	ExamQuestionTrueFalse ExamQuestionType = "truefalse"
	ExamQuestionCode      ExamQuestionType = "code"
)

// Valid 是否合法题型。
func (t ExamQuestionType) Valid() bool {
	switch t {
	case ExamQuestionSingle, ExamQuestionMultiple, ExamQuestionTrueFalse, ExamQuestionCode:
		return true
	}
	return false
}

// ExamAttemptStatus 作答状态：进行中 / 已交卷 / 超时自动交卷。
type ExamAttemptStatus string

const (
	ExamAttemptInProgress ExamAttemptStatus = "in_progress"
	ExamAttemptSubmitted  ExamAttemptStatus = "submitted"
	ExamAttemptExpired    ExamAttemptStatus = "expired"
)

// Valid 是否合法作答状态。
func (s ExamAttemptStatus) Valid() bool {
	return s == ExamAttemptInProgress || s == ExamAttemptSubmitted || s == ExamAttemptExpired
}

// Terminal 是否已结束（可评分/可出成绩）。
func (s ExamAttemptStatus) Terminal() bool {
	return s == ExamAttemptSubmitted || s == ExamAttemptExpired
}

// ExamPaper 试卷配置：一场考试一份（competition_id 唯一）。
// TotalScore 为题目分值之和，管理端增删改题时维护，仅作展示与校验。
type ExamPaper struct {
	ID            uint   `gorm:"primaryKey" json:"id"`
	CompetitionID uint   `gorm:"uniqueIndex" json:"competition_id"`
	Title         string `gorm:"size:200" json:"title"`
	Description   string `gorm:"size:2000" json:"description"`
	// DurationSeconds 考试时长（秒）；服务端 expires_at = min(start+duration, competition.end)。
	DurationSeconds int `gorm:"not null;default:5400" json:"duration_seconds"`
	// PassScore 通过分（0 = 不设通过线）。
	PassScore  int       `gorm:"not null;default:0" json:"pass_score"`
	TotalScore int       `gorm:"not null;default:0" json:"total_score"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ExamQuestion 试题：选项与标准答案均为 JSON（见 docs/openapi.yaml）。
// 编程题通过 ChallengeID 绑定独立 Challenge，选手代码提交走 judge exam scope。
type ExamQuestion struct {
	ID      uint             `gorm:"primaryKey" json:"id"`
	PaperID uint             `gorm:"uniqueIndex:idx_exam_question_seq;index" json:"paper_id"`
	Seq     int              `gorm:"uniqueIndex:idx_exam_question_seq" json:"seq"`
	Type    ExamQuestionType `gorm:"size:16;index" json:"type"`
	Title   string           `gorm:"size:200" json:"title"`
	// Content 题干 Markdown。
	Content string `gorm:"type:text" json:"content"`
	// OptionsJSON 选项：[{"key":"A","text":"..."}]；判断题可为空。
	OptionsJSON string `gorm:"type:text" json:"-"`
	// AnswerJSON 标准答案：single "A"；multiple ["A","C"]；truefalse true。
	// 永不下发选手端（交卷后也只返回是否正确/得分）。
	AnswerJSON string `gorm:"type:text" json:"-"`
	// Explanation 解析（可选；交卷后可见）。
	Explanation string `gorm:"type:text" json:"explanation"`
	Score       int    `gorm:"not null;default:1" json:"score"`
	// ChallengeID 编程题绑定的 Challenge（0 = 未绑定）。
	ChallengeID uint      `gorm:"index" json:"challenge_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ExamAttempt 一次作答：同一试卷同一用户唯一，服务端计时。
// ExpiresAt 在开始时写入；到期 worker 自动交卷（Status=expired）。
type ExamAttempt struct {
	ID            uint              `gorm:"primaryKey" json:"id"`
	PaperID       uint              `gorm:"uniqueIndex:idx_exam_attempt_user;index" json:"paper_id"`
	CompetitionID uint              `gorm:"index" json:"competition_id"`
	UserID        uint              `gorm:"uniqueIndex:idx_exam_attempt_user;index" json:"user_id"`
	Status        ExamAttemptStatus `gorm:"size:16;index" json:"status"`
	StartedAt     time.Time         `json:"started_at"`
	ExpiresAt     time.Time         `gorm:"index" json:"expires_at"`
	SubmittedAt   *time.Time        `json:"submitted_at"`
	// AutoSubmitted 是否由到期 worker 自动交卷。
	AutoSubmitted bool `gorm:"not null;default:false" json:"auto_submitted"`
	// LastIP 最近作答来源 IP（审计；空 = 尚无保存）。
	LastIP         string    `gorm:"size:64" json:"-"`
	ObjectiveScore int       `gorm:"not null;default:0" json:"objective_score"`
	CodeScore      int       `gorm:"not null;default:0" json:"code_score"`
	TotalScore     int       `gorm:"not null;default:0" json:"total_score"`
	Passed         bool      `gorm:"not null;default:false" json:"passed"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ExamAnswer 选手答案（一题一行）：自动保存覆盖，交卷后评分写回。
// 编程题只记录 CodeSubmissionID，分数由 judge 判题事件回写。
type ExamAnswer struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	AttemptID  uint   `gorm:"uniqueIndex:idx_exam_answer_question;index" json:"attempt_id"`
	QuestionID uint   `gorm:"uniqueIndex:idx_exam_answer_question" json:"question_id"`
	AnswerJSON string `gorm:"type:text" json:"-"`
	Graded     bool   `gorm:"not null;default:false" json:"graded"`
	Correct    bool   `gorm:"not null;default:false" json:"correct"`
	Score      int    `gorm:"not null;default:0" json:"score"`
	// CodeSubmissionID 编程题最近一次提交（0 = 未提交）。
	CodeSubmissionID uint      `gorm:"index" json:"code_submission_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ExamStanding 考试成绩派生表：只允许 ExamDeriver 从事件/作答事实重建。
type ExamStanding struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	CompetitionID  uint `gorm:"uniqueIndex:idx_exam_standing_user;index" json:"competition_id"`
	UserID         uint `gorm:"uniqueIndex:idx_exam_standing_user" json:"user_id"`
	Score          int  `json:"score"`
	ObjectiveScore int  `json:"objective_score"`
	CodeScore      int  `json:"code_score"`
	Passed         bool `json:"passed"`
	// DurationSeconds 交卷用时（服务端计时；自动交卷按 ExpiresAt 计）。
	DurationSeconds int        `json:"duration_seconds"`
	SubmittedAt     *time.Time `json:"submitted_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
