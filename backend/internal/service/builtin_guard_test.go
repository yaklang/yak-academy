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

// 内置题集防写护栏（题目管理 tab 化的前置约束）：内置题目（is_builtin）
// 对普通管理操作只读——上传重传 / 删除 / 可见性开关 / 附件增删一律 422，
// 更新与清理只经「一键导入」随目录整体进行（content-package-spec skill：
// 目录是内置内容的唯一事实源）。配套的列表搜索/筛选走 repo filter 单测。

func newGuardTestEnv(t *testing.T) (*ChallengeService, *ContentService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:chalguardtest?mode=memory&cache=shared"), &gorm.Config{})
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
	chalSvc := NewChallengeService(repo.NewChallengeRepo(gdb), repo.NewSubmissionRepo(gdb),
		repo.NewEventRepo(gdb), repo.NewScoreRepo(gdb), nil)
	contentSvc := NewContentService(
		repo.NewContentPackageRepo(gdb),
		repo.NewTopicRepo(gdb),
		repo.NewRoomRepo(gdb),
		chalSvc,
		NoopDriver{},
		repo.NewProgressRepo(gdb),
	)
	return chalSvc, contentSvc, gdb
}

func seedGuardChallenges(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	rows := []model.Challenge{
		{Slug: "guard-builtin", Title: "内置题", Category: "web", Bank: model.BankTutorial,
			Difficulty: model.DifficultyApprentice, Type: model.ChallengeStatic, Score: 100, IsBuiltin: true},
		{Slug: "guard-ctf", Title: "自建 CTF 题", Category: "sqli", Bank: model.BankCTF,
			Difficulty: model.DifficultyPractitioner, Type: model.ChallengeStatic, Score: 200},
		{Slug: "guard-acm-a", Title: "A+B Problem", Category: "basics", Bank: model.BankACM,
			Difficulty: model.DifficultyApprentice, Type: model.ChallengeStatic, Score: 100},
		{Slug: "guard-acm-b", Title: "最长上升子序列", Category: "dp", Bank: model.BankACM,
			Difficulty: model.DifficultyExpert, Type: model.ChallengeStatic, Score: 350},
	}
	for i := range rows {
		if err := gdb.WithContext(ctx).Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
}

// 上传撞内置 slug → 422，且库内行不被改动。
func TestUploadBuiltinChallengeRefused(t *testing.T) {
	_, contentSvc, gdb := newGuardTestEnv(t)
	seedGuardChallenges(t, gdb)
	body := `[{"slug":"guard-builtin","title":"覆盖内置","category":"web",` +
		`"difficulty":"apprentice","score":100,"challenge":{"type":"static","flag":"flag{x}"}}]`
	_, _, err := contentSvc.UploadChallenges(context.Background(), []byte(body))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("upload builtin slug: err = %v, want ErrValidation", err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "guard-builtin").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if chal.Title != "内置题" || chal.Score != 100 {
		t.Fatalf("rejected upload must not modify the builtin row: %+v", chal)
	}
	// 新 slug 的普通上传不受护栏影响（回归确认护栏不误伤）。
	ok := `[{"slug":"guard-fresh","title":"新建题","category":"web",` +
		`"difficulty":"apprentice","score":100,"bank":"acm","challenge":{"type":"static","flag":"flag{y}"}}]`
	if _, _, err := contentSvc.UploadChallenges(context.Background(), []byte(ok)); err != nil {
		t.Fatalf("fresh upload should pass: %v", err)
	}
}

// 删除内置题 → 422；自建题照常可删。
func TestDeleteBuiltinChallengeRefused(t *testing.T) {
	chalSvc, _, gdb := newGuardTestEnv(t)
	seedGuardChallenges(t, gdb)
	if err := chalSvc.DeleteChallenge(context.Background(), "guard-builtin"); !errors.Is(err, ErrValidation) {
		t.Fatalf("delete builtin: err = %v, want ErrValidation", err)
	}
	var n int64
	gdb.Model(&model.Challenge{}).Where("slug = ?", "guard-builtin").Count(&n)
	if n != 1 {
		t.Fatal("builtin row deleted, want preserved")
	}
	if err := chalSvc.DeleteChallenge(context.Background(), "guard-ctf"); err != nil {
		t.Fatalf("delete non-builtin should pass: %v", err)
	}
}

// 可见性开关对内置题 → 422；自建题照常可切换。
func TestSetVisibilityBuiltinChallengeRefused(t *testing.T) {
	chalSvc, _, gdb := newGuardTestEnv(t)
	seedGuardChallenges(t, gdb)
	hidden := false
	if _, err := chalSvc.SetVisibility(context.Background(), "guard-builtin", VisibilityUpdate{IsVisible: &hidden}); !errors.Is(err, ErrValidation) {
		t.Fatalf("visibility builtin: err = %v, want ErrValidation", err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "guard-builtin").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if !chal.IsVisible {
		t.Fatal("builtin visibility mutated by rejected update")
	}
	if _, err := chalSvc.SetVisibility(context.Background(), "guard-acm-a", VisibilityUpdate{IsVisible: &hidden}); err != nil {
		t.Fatalf("visibility non-builtin should pass: %v", err)
	}
}

// 管理端列表搜索/筛选：关键字（slug/title/category 前缀、大小写不敏感）、
// bank/category/difficulty/来源过滤与组合。直接走 repo filter（AdminListChallenges
// 仅透传 filter；富化路径不改变过滤语义）。
func TestChallengeRepoListFilterAndSearch(t *testing.T) {
	gdb := gdbForList(t)
	r := repo.NewChallengeRepo(gdb)
	ctx := context.Background()

	builtin := true
	notBuiltin := false
	cases := []struct {
		name   string
		filter repo.ChallengeListFilter
		want   []string
	}{
		{"empty = all", repo.ChallengeListFilter{}, []string{"guard-builtin", "guard-ctf", "guard-acm-a", "guard-acm-b"}},
		{"bank acm", repo.ChallengeListFilter{Bank: string(model.BankACM)}, []string{"guard-acm-a", "guard-acm-b"}},
		{"bank ctf", repo.ChallengeListFilter{Bank: string(model.BankCTF)}, []string{"guard-ctf"}},
		{"builtin only", repo.ChallengeListFilter{IsBuiltin: &builtin}, []string{"guard-builtin"}},
		{"non-builtin only", repo.ChallengeListFilter{IsBuiltin: &notBuiltin}, []string{"guard-ctf", "guard-acm-a", "guard-acm-b"}},
		{"difficulty expert", repo.ChallengeListFilter{Difficulty: string(model.DifficultyExpert)}, []string{"guard-acm-b"}},
		{"category dp", repo.ChallengeListFilter{Category: "dp"}, []string{"guard-acm-b"}},
		{"search slug prefix", repo.ChallengeListFilter{Search: "guard-acm"}, []string{"guard-acm-a", "guard-acm-b"}},
		{"search title prefix (case-insensitive)", repo.ChallengeListFilter{Search: "a+b"}, []string{"guard-acm-a"}},
		{"search category prefix", repo.ChallengeListFilter{Search: "sq"}, []string{"guard-ctf"}},
		{"search no hit", repo.ChallengeListFilter{Search: "zzz"}, []string{}},
		{"combined bank+difficulty", repo.ChallengeListFilter{Bank: string(model.BankACM), Difficulty: string(model.DifficultyApprentice)}, []string{"guard-acm-a"}},
	}
	for _, tc := range cases {
		list, total, err := r.List(ctx, tc.filter, 0, 100)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := make([]string, 0, len(list))
		for _, c := range list {
			got = append(got, c.Slug)
		}
		if int(total) != len(tc.want) || !slugsEqual(got, tc.want) {
			t.Errorf("%s: got %v (total %d), want %v", tc.name, got, total, tc.want)
		}
	}

	// 搜索串大小写不敏感：标题「最长上升子序列」不会命中英文搜索，但
	// slug 前缀大小写不同必须命中（SQLite LIKE 对 ASCII 前缀已由 LOWER 兜底）。
	list, _, err := r.List(ctx, repo.ChallengeListFilter{Search: "GUARD-CTF"}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Slug != "guard-ctf" {
		t.Fatalf("case-insensitive slug search: got %d rows", len(list))
	}
}

func slugsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// gdbForList 供列表过滤用例的独立内存库。
func gdbForList(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:challisttest?mode=memory&cache=shared"), &gorm.Config{})
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
	seedGuardChallenges(t, gdb)
	return gdb
}