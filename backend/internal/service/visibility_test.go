package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 可见性开关 + 定时发布（基础短板 Item 3）：is_visible=false 或 release_at 在未来时，
// 学生端训练场列表/详情/提交/实例/提示全部 404；到期自动可见。

func newVisibilityTestEnv(t *testing.T) (*ChallengeService, *TopicService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:visibletest?mode=memory&cache=shared"), &gorm.Config{})
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
	chalRepo := repo.NewChallengeRepo(gdb)
	subRepo := repo.NewSubmissionRepo(gdb)
	evRepo := repo.NewEventRepo(gdb)
	chalSvc := NewChallengeService(chalRepo, subRepo, evRepo, repo.NewScoreRepo(gdb), eventbus.New())
	topicSvc := NewTopicService(repo.NewTopicRepo(gdb), repo.NewRoomRepo(gdb), chalRepo, subRepo, evRepo)
	for _, slug := range []string{"vis-a", "vis-b", "vis-c"} {
		if err := chalRepo.Upsert(context.Background(), &model.Challenge{
			Slug: slug, Title: slug, Category: "misc", Difficulty: model.DifficultyApprentice,
			Type: model.ChallengeStatic, Score: 100,
		}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{" + slug + "}"}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	return chalSvc, topicSvc, gdb
}

func boolPtr(b bool) *bool       { return &b }
func timePtr(t time.Time) *time.Time { return &t }

func TestSetVisibilityHidesFromStudents(t *testing.T) {
	chalSvc, topicSvc, _ := newVisibilityTestEnv(t)
	ctx := context.Background()

	// 初始全部可见。
	items, total, err := topicSvc.ListTraining(ctx, 1, "", nil, 0, 20)
	if err != nil || total != 3 || len(items) != 3 {
		t.Fatalf("initial list: %v %d %d", err, total, len(items))
	}

	// 隐藏 vis-a：列表过滤 + 详情 404 + 提交入口 404。
	if _, err := chalSvc.SetVisibility(ctx, "vis-a", VisibilityUpdate{IsVisible: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	items, total, err = topicSvc.ListTraining(ctx, 1, "", nil, 0, 20)
	if err != nil || total != 2 || len(items) != 2 {
		t.Fatalf("list after hide: %v %d %d", err, total, len(items))
	}
	for _, it := range items {
		if it.Slug == "vis-a" {
			t.Fatalf("hidden challenge still in student list")
		}
	}
	if _, err := topicSvc.GetChallengeDetail(ctx, 1, "vis-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("detail of hidden: %v", err)
	}
	if _, err := topicSvc.FindChallengeBySlug(ctx, "vis-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("submit entry of hidden: %v", err)
	}

	// 恢复可见。
	if _, err := chalSvc.SetVisibility(ctx, "vis-a", VisibilityUpdate{IsVisible: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := topicSvc.FindChallengeBySlug(ctx, "vis-a"); err != nil {
		t.Fatalf("re-visible: %v", err)
	}
}

func TestReleaseAtFutureHidesUntilDue(t *testing.T) {
	chalSvc, topicSvc, _ := newVisibilityTestEnv(t)
	ctx := context.Background()

	future := time.Now().Add(2 * time.Hour)
	if _, err := chalSvc.SetVisibility(ctx, "vis-b", VisibilityUpdate{ReleaseAt: &future}); err != nil {
		t.Fatal(err)
	}
	// 未到发布时间：列表不出现、入口 404。
	if _, err := topicSvc.FindChallengeBySlug(ctx, "vis-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("future release visible: %v", err)
	}
	_, total, err := topicSvc.ListTraining(ctx, 1, "", nil, 0, 20)
	if err != nil || total != 2 {
		t.Fatalf("list with future release: %v %d", err, total)
	}

	// 发布时间已过（时钟推进后）：自动可见，无需再次设置。
	past := time.Now().Add(-time.Minute)
	if _, err := chalSvc.SetVisibility(ctx, "vis-b", VisibilityUpdate{ReleaseAt: &past}); err != nil {
		t.Fatal(err)
	}
	if _, err := topicSvc.FindChallengeBySlug(ctx, "vis-b"); err != nil {
		t.Fatalf("past release not visible: %v", err)
	}
}

func TestClearReleaseAtRestoresImmediateSemantics(t *testing.T) {
	chalSvc, topicSvc, gdb := newVisibilityTestEnv(t)
	ctx := context.Background()

	future := time.Now().Add(24 * time.Hour)
	if _, err := chalSvc.SetVisibility(ctx, "vis-c", VisibilityUpdate{ReleaseAt: &future}); err != nil {
		t.Fatal(err)
	}
	if _, err := chalSvc.SetVisibility(ctx, "vis-c", VisibilityUpdate{ClearRelease: true}); err != nil {
		t.Fatal(err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "vis-c").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if chal.ReleaseAt != nil {
		t.Fatalf("release_at not cleared: %v", chal.ReleaseAt)
	}
	if _, err := topicSvc.FindChallengeBySlug(ctx, "vis-c"); err != nil {
		t.Fatalf("cleared release not visible: %v", err)
	}
}

func TestSetVisibilityUnknownSlugAndEmptyUpdate(t *testing.T) {
	chalSvc, _, _ := newVisibilityTestEnv(t)
	ctx := context.Background()
	if _, err := chalSvc.SetVisibility(ctx, "no-such", VisibilityUpdate{IsVisible: boolPtr(false)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown slug: %v", err)
	}
	if _, err := chalSvc.SetVisibility(ctx, "vis-a", VisibilityUpdate{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty update: %v", err)
	}
}

func TestHiddenChallengeBlocksHintUnlock(t *testing.T) {
	chalSvc, _, gdb := newVisibilityTestEnv(t)
	ctx := context.Background()
	// vis-a 补一条提示（Upsert 不触碰 is_visible——重传 JSON 不重置管理端开关）。
	if err := repo.NewChallengeRepo(gdb).Upsert(ctx, &model.Challenge{
		Slug: "vis-a", Title: "vis-a", Category: "misc", Difficulty: model.DifficultyApprentice,
		Type: model.ChallengeStatic, Score: 100,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{a}"}},
		[]model.Hint{{Title: "h", Content: "c", Cost: 0}}); err != nil {
		t.Fatal(err)
	}
	if _, err := chalSvc.SetVisibility(ctx, "vis-a", VisibilityUpdate{IsVisible: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}
	var hint model.Hint
	if err := gdb.Where("challenge_id = ?", 1).First(&hint).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := chalSvc.UnlockHint(ctx, 1, "vis-a", hint.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hint unlock on hidden challenge: %v", err)
	}
	// 恢复后可解锁（0 分提示免积分扣减）。
	if _, err := chalSvc.SetVisibility(ctx, "vis-a", VisibilityUpdate{IsVisible: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := chalSvc.UnlockHint(ctx, 1, "vis-a", hint.ID); err != nil {
		t.Fatalf("hint unlock after re-visible: %v", err)
	}
}
// TestReleaseAtUTCStoredAgainstLocalNow 锁回归：release_at 曾按入参时区原样落库
// （管理 API 收 RFC3339 UTC 字符串），而查询参数 now 由驱动序列化为本地时区
// 字符串。SQLite TEXT 字典序比较跨时区偏移不对称，未到期的题被判为可见
// （E2E visibility 旅程在 UTC+8 本机抓到）。repo 层修复：写入与查询统一 UTC。
// 本测试在任意本机时区都构造出「UTC 写入 + 本地 now」的历史不对称场景。
func TestReleaseAtUTCStoredAgainstLocalNow(t *testing.T) {
	if _, offset := time.Now().Zone(); offset == 0 {
		t.Skip("本机恰为 UTC 时区时无法构造不对称比较场景")
	}
	chalSvc, topicSvc, _ := newVisibilityTestEnv(t)
	ctx := context.Background()

	// 模拟真实路径：前端 toISOString() → RFC3339 UTC → time.Parse 得 UTC 时间。
	// 时钟拨到「本地 23:30」——UTC 表示是次日 07:30 之外的未来时刻，
	// 保证 UTC 字符串字典序上小于本地 now 字符串（曾导致误判可见）。
	futureLocal := time.Now().Add(2 * time.Hour)
	futureUTC, err := time.Parse(time.RFC3339, futureLocal.UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chalSvc.SetVisibility(ctx, "vis-a", VisibilityUpdate{ReleaseAt: &futureUTC}); err != nil {
		t.Fatal(err)
	}

	// 未到期：列表 SQL 比较（release_at <= now）不得把未来题误判为可见。
	_, total, err := topicSvc.ListTraining(ctx, 1, "", nil, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 { // 3 题种子，vis-a 未来发布 → 只见 vis-b/vis-c
		t.Fatalf("future-released challenge leaked into list: total=%d", total)
	}
}
