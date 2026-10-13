package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// TopicService 学生端内容浏览：主题列表 / 主题详情（含 Room 列表）/ Room 详情。
// 教题一体：Room 详情附带内嵌 Lab（teaching-integration skill）。
type TopicService struct {
	topics      repo.TopicRepo
	rooms       repo.RoomRepo
	challenges  repo.ChallengeRepo
	submissions repo.SubmissionRepo
}

func NewTopicService(t repo.TopicRepo, r repo.RoomRepo, c repo.ChallengeRepo, sub repo.SubmissionRepo) *TopicService {
	return &TopicService{topics: t, rooms: r, challenges: c, submissions: sub}
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

// RoomDetail Room 详情视图：教程正文 + 内嵌 Lab（可无）。
type RoomDetail struct {
	Room       *model.Room
	Challenge  *model.Challenge // 可为 nil（纯教程 Room）
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
	if chal, err := s.challenges.FindByRoomSlug(ctx, roomSlug); err == nil {
		detail.Challenge = chal
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return detail, nil
}

// FindChallengeByRoomSlug 提交答案时解析 room → 内嵌题目（教题一体：Lab 必属 Room）。
func (s *TopicService) FindChallengeByRoomSlug(ctx context.Context, roomSlug string) (*model.Challenge, error) {
	return s.challenges.FindByRoomSlug(ctx, roomSlug)
}

// TrainingItem 训练场条目：跨 Topic 的全量题目视角（含所属 Room 元数据与当前用户状态）。
type TrainingItem struct {
	ID         uint             `json:"id"`
	RoomSlug   string           `json:"room_slug"`
	RoomTitle  string           `json:"room_title"`
	TopicSlug  string           `json:"topic_slug"`
	Title      string           `json:"title"`
	Difficulty model.Difficulty `json:"difficulty"`
	Score      int              `json:"score"`
	Solved     bool             `json:"solved"`
	HasEnv     bool             `json:"has_env"`
}

// ListTraining 训练场列表：全量题目分页，批量拼 Room 元数据，逐题算当前用户 solved。
func (s *TopicService) ListTraining(ctx context.Context, userID uint, offset, limit int) ([]TrainingItem, int64, error) {
	chals, total, err := s.challenges.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	slugs := make([]string, 0, len(chals))
	for _, c := range chals {
		slugs = append(slugs, c.RoomSlug)
	}
	rooms, err := s.rooms.FindBySlugs(ctx, slugs...)
	if err != nil {
		return nil, 0, err
	}
	roomBySlug := make(map[string]model.Room, len(rooms))
	for _, r := range rooms {
		roomBySlug[r.Slug] = r
	}
	items := make([]TrainingItem, 0, len(chals))
	for _, c := range chals {
		item := TrainingItem{
			ID: c.ID, RoomSlug: c.RoomSlug, Title: c.Title,
			Score: c.Score, HasEnv: c.Image != "",
		}
		if room, ok := roomBySlug[c.RoomSlug]; ok {
			item.RoomTitle = room.Title
			item.TopicSlug = room.TopicSlug
			item.Difficulty = room.Difficulty
		}
		if solved, err := s.submissions.HasCorrect(ctx, userID, c.ID); err == nil {
			item.Solved = solved
		}
		items = append(items, item)
	}
	return items, total, nil
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