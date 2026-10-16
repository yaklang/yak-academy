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

// GetFullJSON（GET /admin/challenges/{slug}）：编辑回填用单题完整定义。
// 覆盖：上传字段全量回读、tutorial 题库透传、多 flag 列表、未知 slug 404。

func newFullJSONTestEnv(t *testing.T) (*ChallengeService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:fulljsontest?mode=memory&cache=shared"), &gorm.Config{})
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
	svc := NewChallengeService(chalRepo, repo.NewSubmissionRepo(gdb), repo.NewEventRepo(gdb), repo.NewScoreRepo(gdb), nil)
	return svc, gdb
}

func TestGetFullJSONRoundTripsUploadFields(t *testing.T) {
	svc, gdb := newFullJSONTestEnv(t)
	ctx := context.Background()
	// 与上传 JSON 等价的一题：env + hints + links + tutorial_slug + instance 模式。
	if err := repo.NewChallengeRepo(gdb).Upsert(ctx, &model.Challenge{
		Slug: "edit-roundtrip", Title: "回填验证题", Category: "web", Bank: model.BankCTF,
		Difficulty: model.DifficultyPractitioner, Description: "# 背景\n目标",
		Type: model.ChallengeRegex, Score: 150, FlagMode: model.FlagModeInstance,
		Image: "yak-academy/edit-roundtrip:latest", Port: 8080,
		Links:        `[{"name":"工具","url":"https://example.com/tool"}]`,
		TutorialSlug: "sqli-basics",
	}, []model.Flag{{Type: model.ChallengeRegex, Value: `flag\{lab-\{token\}\}`}},
		[]model.Hint{
			{Title: "提示一", Content: "看正则", Cost: 10},
			{Title: "提示二", Content: "看模板", Cost: 20},
		}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetFullJSON(ctx, "edit-roundtrip")
	if err != nil {
		t.Fatalf("GetFullJSON: %v", err)
	}
	if got.Slug != "edit-roundtrip" || got.Title != "回填验证题" || got.Category != "web" ||
		got.Bank != model.BankCTF || got.Difficulty != model.DifficultyPractitioner ||
		got.DescriptionMD != "# 背景\n目标" || got.Score != 150 ||
		got.FlagMode != model.FlagModeInstance || got.TutorialSlug != "sqli-basics" {
		t.Fatalf("scalar fields wrong: %+v", got)
	}
	if got.Challenge.Type != model.ChallengeRegex || got.Challenge.Flag != `flag\{lab-\{token\}\}` {
		t.Fatalf("judge fields wrong: %+v", got.Challenge)
	}
	if got.Env == nil || got.Env.Image != "yak-academy/edit-roundtrip:latest" || got.Env.Port != 8080 {
		t.Fatalf("env wrong: %+v", got.Env)
	}
	if len(got.Hints) != 2 || got.Hints[0].Title != "提示一" || got.Hints[1].Cost != 20 {
		t.Fatalf("hints wrong: %+v", got.Hints)
	}
	if len(got.Links) != 1 || got.Links[0].Name != "工具" || got.Links[0].URL != "https://example.com/tool" {
		t.Fatalf("links wrong: %+v", got.Links)
	}
	if len(got.Flags) != 1 || got.Flags[0] != `flag\{lab-\{token\}\}` {
		t.Fatalf("flags wrong: %+v", got.Flags)
	}
}

func TestGetFullJSONTutorialBankPassthrough(t *testing.T) {
	// 内置题集 bank=tutorial：读侧原样返回（重传省略 bank 即保留，上传校验另有用例）。
	svc, gdb := newFullJSONTestEnv(t)
	ctx := context.Background()
	if err := repo.NewChallengeRepo(gdb).Upsert(ctx, &model.Challenge{
		Slug: "yakit-demo", Title: "内置题", Category: "yakit", Bank: model.BankTutorial,
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeStatic, Score: 60,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{builtin}"}}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetFullJSON(ctx, "yakit-demo")
	if err != nil {
		t.Fatalf("GetFullJSON: %v", err)
	}
	if got.Bank != model.BankTutorial {
		t.Fatalf("bank = %s, want tutorial（原样透传）", got.Bank)
	}
	// 无 env 题目 env 为 nil（不返回空对象）。
	if got.Env != nil {
		t.Fatalf("env = %+v, want nil", got.Env)
	}
	if len(got.Hints) != 0 || got.Links != nil && len(got.Links) != 0 {
		t.Fatalf("hints/links should be empty: %+v %+v", got.Hints, got.Links)
	}
}

func TestGetFullJSONMultiFlagKeepsCompleteList(t *testing.T) {
	// 题目包 zip 可多 flag：flags 完整返回，challenge.flag 取首个供单 flag 表单。
	svc, gdb := newFullJSONTestEnv(t)
	ctx := context.Background()
	if err := repo.NewChallengeRepo(gdb).Upsert(ctx, &model.Challenge{
		Slug: "multi-flag", Title: "多 flag 题", Category: "misc",
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeStatic, Score: 50,
	}, []model.Flag{
		{Type: model.ChallengeStatic, Value: "flag{first}"},
		{Type: model.ChallengeStatic, Value: "flag{second}"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetFullJSON(ctx, "multi-flag")
	if err != nil {
		t.Fatalf("GetFullJSON: %v", err)
	}
	if len(got.Flags) != 2 || got.Flags[1] != "flag{second}" {
		t.Fatalf("flags = %+v, want 完整两条", got.Flags)
	}
	if got.Challenge.Flag != "flag{first}" {
		t.Fatalf("challenge.flag = %q, want flags[0]", got.Challenge.Flag)
	}
}

func TestGetFullJSONNotFound(t *testing.T) {
	svc, _ := newFullJSONTestEnv(t)
	if _, err := svc.GetFullJSON(context.Background(), "no-such"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
