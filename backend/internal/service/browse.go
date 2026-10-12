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
	topics     repo.TopicRepo
	rooms      repo.RoomRepo
	challenges repo.ChallengeRepo
}

func NewTopicService(t repo.TopicRepo, r repo.RoomRepo, c repo.ChallengeRepo) *TopicService {
	return &TopicService{topics: t, rooms: r, challenges: c}
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