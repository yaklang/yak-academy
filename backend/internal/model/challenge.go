package model

import "time"

// ChallengeType 题目判定类型（多态 type 列，架构文档 §3.3）。
type ChallengeType string

const (
	ChallengeStatic ChallengeType = "static" // 静态 flag
	ChallengeRegex  ChallengeType = "regex"  // 正则 flag
	// ChallengeCode ACM 编程题：提交 yak 源码，judge worker 用引擎对每组测试数据
	// 运行比对（对齐 ICPC 模型），不比对 flag。判定配置在 ACMJudge/JudgeTestdata 表。
	ChallengeCode ChallengeType = "code"
)

// FlagMode flag 作用域：shared=题目级共享；instance=每容器实例唯一。
type FlagMode string

const (
	FlagModeShared   FlagMode = "shared"
	FlagModeInstance FlagMode = "instance"
)

// ChallengeBank 题库 tab 分类（题库页 教程题目/CTF 题目/ACM 题目）。
type ChallengeBank string

const (
	// BankTutorial 教程题目：内部题库默认归置（含官方内置训练题集）。
	BankTutorial ChallengeBank = "tutorial"
	// BankCTF CTF 题目。
	BankCTF ChallengeBank = "ctf"
	// BankACM ACM 题目。
	BankACM ChallengeBank = "acm"
)

// Challenge 独立题目资源（管理员经 challenge.json / challenge.yml 上传）。
// tutorial_slug 可选关联教程 Room；不关联的纯题目也合法。
type Challenge struct {
	ID       uint          `gorm:"primaryKey" json:"id"`
	Slug     string        `gorm:"uniqueIndex;size:128" json:"slug"`
	Title    string        `gorm:"size:256" json:"title"`
	Category string        `gorm:"index;size:128" json:"category"` // 分类 slug，题库页分组键
	// Bank 题库 tab（tutorial/ctf/acm）；历史行迁移时由列默认值补 tutorial。
	Bank      ChallengeBank `gorm:"index;size:16;default:tutorial" json:"bank"`
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
	// IsVisible 可见性开关（管理端控制；false 时学生端训练场/详情/实例/提交全部 404）。
	IsVisible bool `gorm:"default:true" json:"is_visible"`
	// IsBuiltin 来自官方内置训练题集（导入时标记；普通上传/编辑不改写此来源标记）。
	// 带默认值（不带 not null，与 is_visible 一致）：无默认值时 ADD COLUMN 给旧行
	// 落 NULL，而「非内置」过滤（is_builtin = 0）按 SQL 三值逻辑匹配不到 NULL
	// （db.Migrate 另有回填兜底旧库）。not null 会让 AutoMigrate 的整表重建在
	// 回填前就撞上旧行 NULL，迁移直接失败，故不加。
	IsBuiltin bool `gorm:"default:false" json:"is_builtin"`
	// ReleaseAt 定时发布时间（nil = 不限，立即语义；未来时间学生端隐藏，到期自动可见）。
	ReleaseAt *time.Time `json:"release_at"`
	CreatedAt time.Time      `json:"created_at"`
}

// Published 当前时刻对学生端是否可见（is_visible 且已到发布时间）。
func (c *Challenge) Published(now time.Time) bool {
	return c.IsVisible && (c.ReleaseAt == nil || !now.Before(*c.ReleaseAt))
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
// ACM 编程题（type=code）不写 Flag 行——判定走 ACMJudge + JudgeTestdata。
type Flag struct {
	ID          uint          `gorm:"primaryKey"`
	ChallengeID uint          `gorm:"index"`
	Type        ChallengeType `gorm:"size:16"`
	Value       string        `gorm:"size:512"` // static: 明文；regex: 正则
}

// ACMJudge ACM 编程题判定配置（type=code 专用；与 Flag 行互斥）。
// 学生提交 yak 源码，worker 用引擎对每组 JudgeTestdata 运行比对（ICPC 模型）。
type ACMJudge struct {
	ID          uint   `gorm:"primaryKey" json:"-"`
	ChallengeID uint   `gorm:"uniqueIndex" json:"-"`
	Engine      string `gorm:"size:16" json:"engine"`        // 判题引擎：yak
	TimeLimitS  int    `json:"time_limit_s"`                 // 单组数据时限（秒）
	MemoryLimitMB int  `json:"memory_limit_mb"`              // 单组数据内存上限（MB）
}

// JudgeTestdata ACM 编程题测试数据组（.in/.answer 成对；sample=true 题面样例）。
// 期望输出列名 AnswerFile 避开 .ans 后缀（构建工具误处理，见 openapi 契约说明）。
type JudgeTestdata struct {
	ID          uint   `gorm:"primaryKey" json:"-"`
	ChallengeID uint   `gorm:"index:idx_judge_testdata" json:"-"`
	Name        string `gorm:"index:idx_judge_testdata;size:64" json:"name"` // 组名（包内文件去扩展名）
	Input       string `gorm:"type:text" json:"input"`                      // 输入数据全文
	Answer      string `gorm:"type:text" json:"output"`                     // 期望输出全文
	Sample      bool   `gorm:"default:false" json:"sample"`                 // 样例组（学生可见）
	// Score 组分值（部分得分制）：0 = 未配置，按组数均分题面分值；>0 时
	// 各组按比例分摊题面分值（总题分不随配置变化）。ACM 语义：通过组即得该组分。
	Score int `gorm:"default:0" json:"score"`
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

// SubmissionVerdict ACM 编程题判定细分（flag 判定题为空串，JSON 输出 null）：
// ac/wa/tle/re/internal_error（openapi 契约 Submission.verdict）。
type SubmissionVerdict string

const (
	VerdictNone           SubmissionVerdict = ""                // flag 判定题
	VerdictAC             SubmissionVerdict = "ac"              // 全部测试数据通过
	VerdictWA             SubmissionVerdict = "wa"             // 输出不匹配
	VerdictTLE            SubmissionVerdict = "tle"            // 超时
	VerdictRE             SubmissionVerdict = "re"             // 运行错误/内存超限
	VerdictInternalError  SubmissionVerdict = "internal_error" // 平台侧失败（引擎不可用等）
)

// Submission 一次答案提交。判定由 worker 异步完成，HTTP 路径禁止同步判定。
type Submission struct {
	ID          uint             `gorm:"primaryKey" json:"id"`
	UserID      uint             `gorm:"index" json:"user_id"`
	ChallengeID uint             `gorm:"index" json:"challenge_id"`
	// CompetitionID 竞赛提交的作用域（0 = 训练场）：防重复计分/动态计分/
	// 竞赛排行榜派生均按此分组，竞赛首通不影响训练场首通判定。
	CompetitionID uint `gorm:"index;default:0" json:"competition_id"`
	// SourceIP 提交来源 IP（竞赛反作弊 IP 异常检测用；训练场提交不记录）。
	SourceIP string             `gorm:"size:64" json:"-"`
	Answer   string             `json:"-"`
	Status   SubmissionStatus   `gorm:"size:16;index" json:"status"`
	// Verdict ACM 判定细分（ac/wa/tle/re/internal_error；flag 判定题为空串 → JSON null）。
	Verdict SubmissionVerdict `gorm:"size:16" json:"verdict"`
	Score   int               `json:"score"` // 判定通过后写入（submission.judged 事件派生）
	// Groups ACM 逐组判定明细（JSON，CodeGroupResult[]；部分得分制学生端反馈，
	// 与 submission.judged 事件 payload.groups 同源）。flag 判定题为空串 → JSON null。
	Groups    string     `gorm:"type:text" json:"groups"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
