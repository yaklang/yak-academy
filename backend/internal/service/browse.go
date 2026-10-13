package service

import (
	"context"
	"errors"

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
}

func NewTopicService(t repo.TopicRepo, r repo.RoomRepo, c repo.ChallengeRepo, sub repo.SubmissionRepo, ev repo.EventRepo) *TopicService {
	return &TopicService{topics: t, rooms: r, challenges: c, submissions: sub, events: ev}
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
func (s *TopicService) FindChallengeBySlug(ctx context.Context, slug string) (*model.Challenge, error) {
	return s.challenges.FindBySlug(ctx, slug)
}

// TrainingItem 训练场条目：独立题目资源的列表视角（含当前用户状态与可选教程关联）。
type TrainingItem struct {
	ID            uint             `json:"id"`
	Slug          string           `json:"slug"`
	Title         string           `json:"title"`
	Category      string           `json:"category"`
	Difficulty    model.Difficulty `json:"difficulty"`
	Score         int              `json:"score"`
	Solved        bool             `json:"solved"`
	HasEnv        bool             `json:"has_env"`
	TutorialSlug  string           `json:"tutorial_slug"`
	TutorialTitle string           `json:"tutorial_title"`
	// TutorialTopicSlug 关联教程所属 topic（前端 Room 详情路由需要 topic 段）。
	TutorialTopicSlug string `json:"tutorial_topic_slug"`
}

// ChallengeDetail 题目详情（训练场点入页）：描述 markdown + hints（未解锁正文为空）。
type ChallengeDetail struct {
	TrainingItem
	DescriptionMD string     `json:"description_md"`
	Hints         []HintView `json:"hints"`
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

// toTrainingItem model.Challenge → TrainingItem（不含 solved 与教程标题）。
func toTrainingItem(c model.Challenge) TrainingItem {
	return TrainingItem{
		ID: c.ID, Slug: c.Slug, Title: c.Title, Category: c.Category,
		Difficulty: c.Difficulty, Score: c.Score, HasEnv: c.Image != "",
		TutorialSlug: c.TutorialSlug,
	}
}

// markSolved 逐题算当前用户是否已通关。
func (s *TopicService) markSolved(ctx context.Context, userID uint, items []TrainingItem) {
	for i := range items {
		if solved, err := s.submissions.HasCorrect(ctx, userID, items[i].ID); err == nil {
			items[i].Solved = solved
		}
	}
}

// ListTraining 训练场列表：全量题目分页（按 category 分组由前端渲染）。
func (s *TopicService) ListTraining(ctx context.Context, userID uint, offset, limit int) ([]TrainingItem, int64, error) {
	chals, total, err := s.challenges.List(ctx, offset, limit)
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
func (s *TopicService) AdminListChallenges(ctx context.Context, offset, limit int) ([]AdminChallengeItem, int64, error) {
	chals, total, err := s.challenges.List(ctx, offset, limit)
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
func (s *TopicService) GetChallengeDetail(ctx context.Context, userID uint, slug string) (*ChallengeDetail, error) {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, err
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
	if solved, err := s.submissions.HasCorrect(ctx, userID, chal.ID); err == nil {
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

// UpdateTopic 更新主题元数据（PUT /topics/{slug}，最低 teacher——课程级角色挂载点）。
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