package service

import (
	"context"
	"fmt"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// PathItemView 学习路径节点视图（存储只引用 room_slug；title/topic_slug 为
// 响应富化字段，供前端展示与跳转，不落路径表——teaching-integration skill）。
type PathItemView struct {
	Order     int    `json:"order"`
	RoomSlug  string `json:"room_slug"`
	Title     string `json:"title"`
	TopicSlug string `json:"topic_slug"`
	Completed bool   `json:"completed"`
}

// PathView 学习路径（含当前用户进度）。
type PathView struct {
	ID    uint           `json:"id"`
	Title string         `json:"title"`
	Items []PathItemView `json:"items"`
}

// PathService 学习路径：有序 Room 引用视图（teaching-integration：禁止引用 Challenge / 内联内容）。
type PathService struct {
	paths    repo.PathRepo
	rooms    repo.RoomRepo
	progress repo.ProgressRepo
}

func NewPathService(paths repo.PathRepo, rooms repo.RoomRepo, progress repo.ProgressRepo) *PathService {
	return &PathService{paths: paths, rooms: rooms, progress: progress}
}

func (s *PathService) List(ctx context.Context, userID uint) ([]PathView, error) {
	paths, err := s.paths.List(ctx)
	if err != nil {
		return nil, err
	}
	completed, err := s.completedSet(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]PathView, 0, len(paths))
	for _, p := range paths {
		view, err := s.buildView(ctx, p, completed)
		if err != nil {
			return nil, err
		}
		out = append(out, *view)
	}
	return out, nil
}

func (s *PathService) Get(ctx context.Context, userID, id uint) (*PathView, error) {
	path, err := s.paths.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	completed, err := s.completedSet(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.buildView(ctx, *path, completed)
}

// Create 管理端创建路径：校验 room_slug 存在、order 在 path 内唯一。
func (s *PathService) Create(ctx context.Context, title string, items []PathItemView) (*PathView, error) {
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrValidation)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: items required", ErrValidation)
	}
	seenOrder := map[int]bool{}
	seenSlug := map[string]bool{}
	modelItems := make([]model.PathItem, 0, len(items))
	for _, it := range items {
		if it.RoomSlug == "" {
			return nil, fmt.Errorf("%w: room_slug is required", ErrValidation)
		}
		if seenOrder[it.Order] {
			return nil, fmt.Errorf("%w: duplicate order %d", ErrValidation, it.Order)
		}
		seenOrder[it.Order] = true
		if seenSlug[it.RoomSlug] {
			return nil, fmt.Errorf("%w: duplicate room_slug %q", ErrValidation, it.RoomSlug)
		}
		seenSlug[it.RoomSlug] = true
		ok, err := s.rooms.ExistsBySlug(ctx, it.RoomSlug)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: room %q not found", ErrValidation, it.RoomSlug)
		}
		modelItems = append(modelItems, model.PathItem{Order: it.Order, RoomSlug: it.RoomSlug})
	}
	path := &model.LearningPath{Title: title}
	if err := s.paths.Create(ctx, path, modelItems); err != nil {
		return nil, err
	}
	return s.buildView(ctx, *path, map[string]bool{})
}

func (s *PathService) Delete(ctx context.Context, id uint) error {
	return s.paths.Delete(ctx, id)
}

func (s *PathService) completedSet(ctx context.Context, userID uint) (map[string]bool, error) {
	slugs, err := s.progress.ListCompletedSlugs(ctx, userID)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		set[slug] = true
	}
	return set, nil
}

func (s *PathService) buildView(ctx context.Context, path model.LearningPath, completed map[string]bool) (*PathView, error) {
	items, err := s.paths.ListItems(ctx, path.ID)
	if err != nil {
		return nil, err
	}
	slugs := make([]string, len(items))
	for i, it := range items {
		slugs[i] = it.RoomSlug
	}
	rooms, err := s.rooms.FindBySlugs(ctx, slugs...)
	if err != nil {
		return nil, err
	}
	bySlug := make(map[string]model.Room, len(rooms))
	for _, r := range rooms {
		bySlug[r.Slug] = r
	}
	view := &PathView{ID: path.ID, Title: path.Title, Items: []PathItemView{}}
	for _, it := range items {
		v := PathItemView{Order: it.Order, RoomSlug: it.RoomSlug, Completed: completed[it.RoomSlug]}
		if r, ok := bySlug[it.RoomSlug]; ok {
			v.Title, v.TopicSlug = r.Title, r.TopicSlug
		}
		view.Items = append(view.Items, v)
	}
	return view, nil
}
