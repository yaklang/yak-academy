package service

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func newPathTestService(t *testing.T) (*PathService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:pathtest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	rooms := repo.NewRoomRepo(gdb)
	_ = rooms.Upsert(context.Background(), &model.Room{
		Slug: "room-a", TopicSlug: "topic-x", Title: "A", Difficulty: model.DifficultyApprentice,
	})
	_ = rooms.Upsert(context.Background(), &model.Room{
		Slug: "room-b", TopicSlug: "topic-x", Title: "B", Difficulty: model.DifficultyPractitioner,
	})
	svc := NewPathService(repo.NewPathRepo(gdb), rooms, repo.NewProgressRepo(gdb))
	return svc, gdb
}

func TestPathCreateListGetWithProgress(t *testing.T) {
	svc, gdb := newPathTestService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, "Web 入门", []PathItemView{
		{Order: 1, RoomSlug: "room-a"},
		{Order: 2, RoomSlug: "room-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Title != "Web 入门" || len(created.Items) != 2 {
		t.Fatalf("created = %+v", created)
	}
	// 节点视图富化：title/topic_slug 来自 Room（供前端跳转），非路径存储。
	if created.Items[0].Title != "A" || created.Items[0].TopicSlug != "topic-x" {
		t.Fatalf("item[0] not enriched: %+v", created.Items[0])
	}
	if created.Items[1].Title != "B" || created.Items[1].TopicSlug != "topic-x" {
		t.Fatalf("item[1] not enriched: %+v", created.Items[1])
	}
	// 用户 3 完成 room-a。
	if err := repo.NewProgressRepo(gdb).MarkCompleted(ctx, 3, "room-a"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Items[0].Completed || list[0].Items[1].Completed {
		t.Fatalf("list progress = %+v", list)
	}
	got, err := svc.Get(ctx, 3, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Items[0].Completed || got.Items[1].Completed {
		t.Fatalf("get progress = %+v", got)
	}
	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, 3, created.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("after delete err=%v", err)
	}
}

func TestPathCreateRejectsUnknownRoom(t *testing.T) {
	svc, _ := newPathTestService(t)
	_, err := svc.Create(context.Background(), "坏路径", []PathItemView{
		{Order: 1, RoomSlug: "missing-room"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err=%v, want ErrValidation", err)
	}
}

func TestPathCreateRejectsDuplicateOrder(t *testing.T) {
	svc, _ := newPathTestService(t)
	_, err := svc.Create(context.Background(), "坏路径", []PathItemView{
		{Order: 1, RoomSlug: "room-a"},
		{Order: 1, RoomSlug: "room-b"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err=%v, want ErrValidation", err)
	}
}
