package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// newBadgeTestService 构造徽章判定所需的三类派生数据 repo + 一条两节点路径。
func newBadgeTestService(t *testing.T) (*BadgeService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:badgetest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	rooms := repo.NewRoomRepo(gdb)
	for _, slug := range []string{"room-a", "room-b"} {
		if err := rooms.Upsert(context.Background(), &model.Room{
			Slug: slug, TopicSlug: "topic-x", Title: slug, Difficulty: model.DifficultyApprentice,
		}); err != nil {
			t.Fatal(err)
		}
	}
	pathSvc := NewPathService(repo.NewPathRepo(gdb), rooms, repo.NewProgressRepo(gdb))
	if _, err := pathSvc.Create(context.Background(), "徽章路径", []PathItemView{
		{Order: 1, RoomSlug: "room-a"}, {Order: 2, RoomSlug: "room-b"},
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewBadgeService(
		repo.NewScoreRepo(gdb), repo.NewProgressRepo(gdb), repo.NewPathRepo(gdb),
	)
	return svc, gdb
}

// earned 按 code 取判定结果。
func earnedByCode(badges []Badge) map[string]bool {
	m := make(map[string]bool, len(badges))
	for _, b := range badges {
		m[b.Code] = b.Earned
	}
	return m
}

func TestBadgeEmptyUserEarnsNothing(t *testing.T) {
	svc, _ := newBadgeTestService(t)
	badges, err := svc.List(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(badges) != len(badgeCatalog) {
		t.Fatalf("badges = %d, want full catalog %d", len(badges), len(badgeCatalog))
	}
	for _, b := range badges {
		if b.Earned {
			t.Fatalf("empty user earned %s", b.Code)
		}
		if b.Code == "" || b.Title == "" || b.Description == "" {
			t.Fatalf("badge catalog entry incomplete: %+v", b)
		}
	}
}

func TestBadgeCatalogCodesUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(badgeCatalog))
	for _, rule := range badgeCatalog {
		if _, dup := seen[rule.code]; dup {
			t.Fatalf("duplicate badge code %q", rule.code)
		}
		seen[rule.code] = struct{}{}
	}
}

func TestBadgeEarnedFromScoreAndSolves(t *testing.T) {
	svc, gdb := newBadgeTestService(t)
	ctx := context.Background()
	// 积分 120、通关 1 题、完成 1 个 Room。
	if err := repo.NewScoreRepo(gdb).UpdateScore(ctx, 7, 120, 1); err != nil {
		t.Fatal(err)
	}
	if err := repo.NewProgressRepo(gdb).MarkCompleted(ctx, 7, "room-a"); err != nil {
		t.Fatal(err)
	}
	badges, err := svc.List(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	got := earnedByCode(badges)
	for _, want := range []string{"first_solve", "first_room", "xp_100"} {
		if !got[want] {
			t.Fatalf("badge %s should be earned, got %+v", want, got)
		}
	}
	for _, not := range []string{"ten_solves", "ten_rooms", "xp_500", "xp_1000", "path_finisher"} {
		if got[not] {
			t.Fatalf("badge %s should not be earned, got %+v", not, got)
		}
	}
}

func TestBadgePathFinisherRequiresAllRooms(t *testing.T) {
	svc, gdb := newBadgeTestService(t)
	ctx := context.Background()
	progress := repo.NewProgressRepo(gdb)
	// 只完成第一个 Room：不算走完路径。
	if err := progress.MarkCompleted(ctx, 9, "room-a"); err != nil {
		t.Fatal(err)
	}
	badges, err := svc.List(ctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if earnedByCode(badges)["path_finisher"] {
		t.Fatal("path_finisher must require all rooms completed")
	}
	// 全部完成：学有所成。
	if err := progress.MarkCompleted(ctx, 9, "room-b"); err != nil {
		t.Fatal(err)
	}
	badges, err = svc.List(ctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if !earnedByCode(badges)["path_finisher"] {
		t.Fatal("path_finisher should be earned after completing the whole path")
	}
}

func TestBadgeMilestoneTiers(t *testing.T) {
	svc, gdb := newBadgeTestService(t)
	ctx := context.Background()
	// 积分 500、通关 10 题：xp_500 与 ten_solves 达成，xp_1000 未达。
	if err := repo.NewScoreRepo(gdb).UpdateScore(ctx, 5, 500, 10); err != nil {
		t.Fatal(err)
	}
	badges, err := svc.List(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	got := earnedByCode(badges)
	if !got["xp_100"] || !got["xp_500"] || got["xp_1000"] {
		t.Fatalf("xp milestones = %+v", got)
	}
	if !got["ten_solves"] {
		t.Fatal("ten_solves should be earned")
	}
}