package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// TopicService 学生端内容浏览：主题列表 / 主题详情（含 Room 列表）/ Room 详情 / 训练场。
// 教程与题目是两类资源：题目经 ChallengeService 独立上传，tutorial_slug 可选指回 Room。
type TopicService struct {
	topics      repo.TopicRepo
	rooms       repo.RoomRepo
	challenges  repo.ChallengeRepo
	submissions repo.SubmissionRepo
	events      repo.EventRepo
	// now 时钟注入（可见性/定时发布的当前时刻判定）。
	now func() time.Time
}

func NewTopicService(t repo.TopicRepo, r repo.RoomRepo, c repo.ChallengeRepo, sub repo.SubmissionRepo, ev repo.EventRepo) *TopicService {
	return &TopicService{topics: t, rooms: r, challenges: c, submissions: sub, events: ev, now: time.Now}
}

type TopicSummary struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	RoomCount int    `json:"room_count"`
}

func (s *TopicService) ListTopics(ctx context.Context, offset, limit int) ([]TopicSummary, int64, error) {
	topics, total, err := s.topics.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	out := make([]TopicSummary, 0, len(topics))
	for _, t := range topics {
		rooms, err := s.rooms.ListByTopic(ctx, t.Slug)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, TopicSummary{Slug: t.Slug, Title: t.Title, Summary: t.Summary, RoomCount: len(rooms)})
	}
	return out, total, nil
}

type RoomSummary struct {
	Slug       string           `json:"slug"`
	Title      string           `json:"title"`
	Difficulty model.Difficulty `json:"difficulty"`
}

type TopicDetail struct {
	TopicSummary
	Rooms []RoomSummary `json:"rooms"`
}

func (s *TopicService) GetTopic(ctx context.Context, topicSlug string) (*TopicDetail, error) {
	topic, err := s.topics.FindBySlug(ctx, topicSlug)
	if err != nil {
		return nil, err
	}
	rooms, err := s.rooms.ListByTopic(ctx, topicSlug)
	if err != nil {
		return nil, err
	}
	detail := &TopicDetail{Rooms: []RoomSummary{}}
	detail.TopicSummary = TopicSummary{Slug: topic.Slug, Title: topic.Title, Summary: topic.Summary, RoomCount: len(rooms)}
	for _, r := range rooms {
		detail.Rooms = append(detail.Rooms, RoomSummary{Slug: r.Slug, Title: r.Title, Difficulty: r.Difficulty})
	}
	return detail, nil
}

// RoomDetail Room 详情视图：文字/视频教程正文 + 可选关联题目 slug。
// 题目是独立资源，Room 不再内嵌 Lab；有关联题目时仅返回 slug 供前端跳转。
type RoomDetail struct {
	Room       *model.Room
	Challenge  *model.Challenge // 关联题目（tutorial_slug 指向本 Room），可为 nil
}

func (s *TopicService) GetRoomDetail(ctx context.Context, topicSlug, roomSlug string) (*RoomDetail, error) {
	room, err := s.rooms.FindBySlug(ctx, roomSlug)
	if err != nil {
		return nil, err
	}
	if room.TopicSlug != topicSlug {
		return nil, gorm.ErrRecordNotFound
	}
	detail := &RoomDetail{Room: room}
	if chal, err := s.challenges.FindByTutorialSlug(ctx, roomSlug); err == nil {
		detail.Challenge = chal
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return detail, nil
}

// FindChallengeBySlug 提交答案/实例操作时按题目 slug 解析（题目独立资源的主键入口）。
// 学生入口统一做发布校验：隐藏/未到发布时间的题目按不存在处理（404）。
func (s *TopicService) FindChallengeBySlug(ctx context.Context, slug string) (*model.Challenge, error) {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !chal.Published(s.now()) {
		return nil, ErrNotFound
	}
	return chal, nil
}

// TrainingItem 题库条目：独立题目资源的列表视角（含当前用户状态与可选教程关联）。
type TrainingItem struct {
	ID            uint             `json:"id"`
	Slug          string           `json:"slug"`
	Title         string           `json:"title"`
	Category      string           `json:"category"`
	Bank          string           `json:"bank"`
	IsBuiltin     bool             `json:"is_builtin"`
	Difficulty    model.Difficulty `json:"difficulty"`
	Score         int              `json:"score"`
	Solved        bool             `json:"solved"`
	HasEnv        bool             `json:"has_env"`
	// IsVisible/ReleaseAt 可见性状态（管理端列表展示与编辑回显；学生端行恒为可见）。
	IsVisible     bool             `json:"is_visible"`
	ReleaseAt     *time.Time       `json:"release_at"`
	Links         []model.ChallengeLink `json:"links"`
	TutorialSlug  string           `json:"tutorial_slug"`
	TutorialTitle string           `json:"tutorial_title"`
	// TutorialTopicSlug 关联教程所属 topic（前端 Room 详情路由需要 topic 段）。
	TutorialTopicSlug string         `json:"tutorial_topic_slug"`
}

// ChallengeDetail 题目详情（训练场点入页）：描述 markdown + hints（未解锁正文为空）。
type ChallengeDetail struct {
	TrainingItem
	DescriptionMD string          `json:"description_md"`
	Hints         []HintView      `json:"hints"`
	// Attachments 附件列表（api 层从 ChallengeService 拼装，学生凭 JWT 下载）。
	Attachments []AttachmentView `json:"attachments"`
	// Judge ACM 编程题判定配置（flag 判定题为 nil；数据内容不下发，
	// 样例经 ListJudgeSamples 提供）。
	Judge *JudgeConfigView `json:"judge"`
}

// JudgeConfigView ACM 判定配置的学生端视图（不含测试数据内容）。
type JudgeConfigView struct {
	Engine        string `json:"engine"`
	TimeLimitS    int    `json:"time_limit_s"`
	MemoryLimitMB int    `json:"memory_limit_mb"`
	TestdataCount int    `json:"testdata_count"`
	// AllowedLibs 题目 yak 库白名单（nil = 未配置，允许全部非黑名单库）。
	// 已扣除全局黑名单；编辑器补全过滤与题面库标签以此为准。
	AllowedLibs []string `json:"allowed_libs"`
	// BlockedLibs 平台全局黑名单（exec/file 等高危库）——题面提示与补全排除用。
	BlockedLibs []string `json:"blocked_libs"`
	// EngineAvailable 判定引擎是否已装配（api 层从 JudgeService 拼装）。
	// false 时前端禁用提交/自测入口，代替点击后才收到 internal_error/422。
	EngineAvailable bool `json:"engine_available"`
}

// enrichTutorial 批量拼关联教程 Room 的标题（tutorial_slug → room 元数据）。
func (s *TopicService) enrichTutorial(ctx context.Context, items []TrainingItem) error {
	slugs := make([]string, 0, len(items))
	for _, it := range items {
		if it.TutorialSlug != "" {
			slugs = append(slugs, it.TutorialSlug)
		}
	}
	rooms, err := s.rooms.FindBySlugs(ctx, slugs...)
	if err != nil {
		return err
	}
	titleBySlug := make(map[string]string, len(rooms))
	topicBySlug := make(map[string]string, len(rooms))
	for _, r := range rooms {
		titleBySlug[r.Slug] = r.Title
		topicBySlug[r.Slug] = r.TopicSlug
	}
	for i := range items {
		items[i].TutorialTitle = titleBySlug[items[i].TutorialSlug]
		items[i].TutorialTopicSlug = topicBySlug[items[i].TutorialSlug]
	}
	return nil
}

// bankOrDefault 读侧兜底：历史空 bank 按教程题目展示（迁移后不应再出现空值）。
func bankOrDefault(b model.ChallengeBank) model.ChallengeBank {
	if b == "" {
		return model.BankTutorial
	}
	return b
}

// toTrainingItem model.Challenge → TrainingItem（不含 solved 与教程标题）。
func toTrainingItem(c model.Challenge) TrainingItem {
	return TrainingItem{
		ID: c.ID, Slug: c.Slug, Title: c.Title, Category: c.Category,
		Bank: string(bankOrDefault(c.Bank)), IsBuiltin: c.IsBuiltin,
		Difficulty: c.Difficulty, Score: c.Score, HasEnv: c.Image != "",
		IsVisible: c.IsVisible, ReleaseAt: c.ReleaseAt,
		Links: parseLinks(c.Links), TutorialSlug: c.TutorialSlug,
	}
}

// parseLinks Challenge.Links JSON 字符串列 → 切片（空列/坏数据返回 nil）。
func parseLinks(s string) []model.ChallengeLink {
	if s == "" {
		return nil
	}
	var links []model.ChallengeLink
	if err := json.Unmarshal([]byte(s), &links); err != nil {
		return nil
	}
	return links
}

// markSolved 逐题算当前用户是否已通关。
func (s *TopicService) markSolved(ctx context.Context, userID uint, items []TrainingItem) {
	for i := range items {
		if solved, err := s.submissions.HasCorrect(ctx, userID, items[i].ID, 0); err == nil {
			items[i].Solved = solved
		}
	}
}

// ListTraining 题库列表：仅对学生可见（is_visible 且已到发布时间）的题目分页。
// bank 空 = 全部题库 tab；builtin nil = 不限来源。
func (s *TopicService) ListTraining(ctx context.Context, userID uint, bank string, builtin *bool, offset, limit int) ([]TrainingItem, int64, error) {
	filter := repo.ChallengeListFilter{Bank: bank, IsBuiltin: builtin}
	chals, total, err := s.challenges.ListVisible(ctx, s.now(), filter, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	items := make([]TrainingItem, 0, len(chals))
	for _, c := range chals {
		items = append(items, toTrainingItem(c))
	}
	s.markSolved(ctx, userID, items)
	if err := s.enrichTutorial(ctx, items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// AdminChallengeItem 题目管理表格条目：TrainingItem + 描述（管理端可看全量，
// solved 语义属于学生进度，置零值）。
type AdminChallengeItem struct {
	TrainingItem
	DescriptionMD string `json:"description_md"`
}

// AdminListChallenges 管理端：题目全量列表（分页，含 has_env 与教程关联富化）。
// filter 承载表格搜索/筛选（题库 tab / 关键字 / 分类 / 难度 / 来源，零值 = 不过滤）。
func (s *TopicService) AdminListChallenges(ctx context.Context, filter repo.ChallengeListFilter, offset, limit int) ([]AdminChallengeItem, int64, error) {
	chals, total, err := s.challenges.List(ctx, filter, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	items := make([]AdminChallengeItem, 0, len(chals))
	for _, c := range chals {
		items = append(items, AdminChallengeItem{
			TrainingItem: toTrainingItem(c), DescriptionMD: c.Description,
		})
	}
	flat := make([]TrainingItem, len(items))
	for i, it := range items {
		flat[i] = it.TrainingItem
	}
	if err := s.enrichTutorial(ctx, flat); err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].TrainingItem = flat[i]
	}
	return items, total, nil
}

// GetChallengeDetail 题目详情：描述 + hints（按解锁态脱敏）+ solved + 关联教程。
// 隐藏/未到发布时间的题目按不存在处理（404）。
func (s *TopicService) GetChallengeDetail(ctx context.Context, userID uint, slug string) (*ChallengeDetail, error) {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if !chal.Published(s.now()) {
		return nil, ErrNotFound
	}
	hints, err := s.challenges.FindHints(ctx, chal.ID)
	if err != nil {
		return nil, err
	}
	unlocked := map[uint]bool{}
	for _, h := range hints {
		ok, err := s.events.HasHintUnlocked(ctx, userID, h.ID)
		if err != nil {
			return nil, err
		}
		unlocked[h.ID] = ok
	}
	detail := &ChallengeDetail{
		TrainingItem:  toTrainingItem(*chal),
		Hints:         toHintViews(hints, unlocked),
		DescriptionMD: chal.Description,
	}
	// ACM 编程题：判定配置随详情下发（数据内容不下发）。
	if chal.Type == model.ChallengeCode {
		if conf, err := s.challenges.FindACM(ctx, chal.ID); err == nil {
			n := 0
			if data, err := s.challenges.FindTestdata(ctx, chal.ID); err == nil {
				n = len(data)
			}
			detail.Judge = &JudgeConfigView{
				Engine: conf.Engine, TimeLimitS: conf.TimeLimitS,
				MemoryLimitMB: conf.MemoryLimitMB, TestdataCount: n,
				AllowedLibs: effectiveAllowedLibs(conf.AllowedLibs),
				BlockedLibs: sortedBlockedList(),
			}
		}
	}
	if solved, err := s.submissions.HasCorrect(ctx, userID, chal.ID, 0); err == nil {
		detail.Solved = solved
	}
	// enrichTutorial 按值传切片，这里原地拼回（单题场景）。
	items := []TrainingItem{detail.TrainingItem}
	if err := s.enrichTutorial(ctx, items); err != nil {
		return nil, err
	}
	detail.TrainingItem = items[0]
	return detail, nil
}

// JudgeSampleView ACM 样例数据组（学生可见；隐藏组不下发）。
type JudgeSampleView struct {
	Name   string `json:"name"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

// ListJudgeSamples ACM 编程题样例数据（GET /challenges/{slug}/judge/samples）。
// 仅返回 sample=true 的组；flag 判定题或不可见题 → ErrNotFound。
// 时限一并下发（提交页展示；隐藏在详情 judge 段也有，这里自包含方便复用）。
func (s *TopicService) ListJudgeSamples(ctx context.Context, slug string) ([]JudgeSampleView, int, error) {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, 0, err
	}
	if !chal.Published(s.now()) || chal.Type != model.ChallengeCode {
		return nil, 0, ErrNotFound
	}
	conf, err := s.challenges.FindACM(ctx, chal.ID)
	if err != nil {
		return nil, 0, err
	}
	data, err := s.challenges.FindTestdata(ctx, chal.ID)
	if err != nil {
		return nil, 0, err
	}
	out := make([]JudgeSampleView, 0, len(data))
	for _, d := range data {
		if d.Sample {
			out = append(out, JudgeSampleView{Name: d.Name, Input: d.Input, Output: d.Answer})
		}
	}
	return out, conf.TimeLimitS, nil
}

// UpdateTopic 更新主题元数据（PUT /topics/{slug}，仅管理员）。
func (s *TopicService) UpdateTopic(ctx context.Context, topicSlug, title, summary string) (*model.Topic, error) {
	topic, err := s.topics.FindBySlug(ctx, topicSlug)
	if err != nil {
		return nil, err
	}
	if title != "" {
		topic.Title = title
	}
	if summary != "" {
		topic.Summary = summary
	}
	if err := s.topics.Upsert(ctx, topic); err != nil {
		return nil, err
	}
	return s.topics.FindBySlug(ctx, topicSlug)
}