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

// newTrainingTestService 内存库 + 两个 room + 两道独立题目（一题有 env 关联教程、一题无）。
func newTrainingTestService(t *testing.T) (*TopicService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:trainingtest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		dbg, _ := gdb.DB()
		dbg.Close()
	})
	chalRepo := repo.NewChallengeRepo(gdb)
	roomRepo := repo.NewRoomRepo(gdb)
	if err := roomRepo.Upsert(context.Background(), &model.Room{
		Slug: "room-sqli", TopicSlug: "web-security", Title: "SQL 注入", Difficulty: model.DifficultyApprentice,
	}); err != nil {
		t.Fatal(err)
	}
	// 有 env 且关联教程的题 与 无 env 无教程的题（分类不同，训练场分组用）。
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		Slug: "sqli-basic", Title: "注入题", Category: "sql-injection",
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeStatic, Score: 100,
		Image: "nginx:alpine", Port: 80, TutorialSlug: "room-sqli",
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{a}"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		Slug: "crypto-encode", Title: "编码题", Category: "cryptography",
		Difficulty: model.DifficultyExpert, Type: model.ChallengeStatic, Score: 200,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{b}"}}, nil); err != nil {
		t.Fatal(err)
	}
	svc := NewTopicService(
		repo.NewTopicRepo(gdb), roomRepo, chalRepo, repo.NewSubmissionRepo(gdb),
	)
	return svc, gdb
}

func TestListTrainingFields(t *testing.T) {
	svc, _ := newTrainingTestService(t)
	items, total, err := svc.ListTraining(context.Background(), 1, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(items))
	}
	sqli, crypto := items[0], items[1]
	// 题目自身字段 + env 标记 + 关联教程富化。
	if sqli.Slug != "sqli-basic" || sqli.Category != "sql-injection" ||
		sqli.Difficulty != model.DifficultyApprentice {
		t.Fatalf("sqli item = %+v", sqli)
	}
	if !sqli.HasEnv || sqli.Score != 100 {
		t.Fatalf("sqli has_env/score = %v/%d, want true/100", sqli.HasEnv, sqli.Score)
	}
	if sqli.TutorialSlug != "room-sqli" || sqli.TutorialTitle != "SQL 注入" {
		t.Fatalf("sqli tutorial = %q/%q, want room-sqli/SQL 注入", sqli.TutorialSlug, sqli.TutorialTitle)
	}
	if sqli.TutorialTopicSlug != "web-security" {
		t.Fatalf("tutorial_topic_slug = %q, want web-security", sqli.TutorialTopicSlug)
	}
	if crypto.HasEnv || crypto.Category != "cryptography" ||
		crypto.Difficulty != model.DifficultyExpert || crypto.TutorialSlug != "" {
		t.Fatalf("crypto item = %+v", crypto)
	}
	// 陌生用户：无解题记录 → solved 全 false。
	if sqli.Solved || crypto.Solved {
		t.Fatal("fresh user should have no solved items")
	}
}

func TestGetChallengeDetail(t *testing.T) {
	svc, gdb := newTrainingTestService(t)
	ctx := context.Background()
	detail, err := svc.GetChallengeDetail(ctx, 1, "sqli-basic")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Title != "注入题" || detail.Difficulty != model.DifficultyApprentice || !detail.HasEnv {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.TutorialTitle != "SQL 注入" {
		t.Fatalf("tutorial_title = %q", detail.TutorialTitle)
	}
	if len(detail.Hints) != 0 {
		t.Fatalf("hints = %+v, want empty", detail.Hints)
	}
	// 已解用户视角。
	if err := gdb.Create(&model.Submission{
		UserID: 5, ChallengeID: detail.ID, Answer: "flag{a}",
		Status: model.SubmissionCorrect,
	}).Error; err != nil {
		t.Fatal(err)
	}
	detail, err = svc.GetChallengeDetail(ctx, 5, "sqli-basic")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Solved {
		t.Fatal("user 5 should see solved=true")
	}
	// 不存在的题目 → gorm.ErrRecordNotFound（api 层映射 404）。
	if _, err := svc.GetChallengeDetail(ctx, 1, "no-such-chal"); err != gorm.ErrRecordNotFound {
		t.Fatalf("err = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestListTrainingSolvedForUser(t *testing.T) {
	svc, gdb := newTrainingTestService(t)
	ctx := context.Background()
	// 用户 5 直接落一条 correct 提交（绕过 judge，制造已解状态）。
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "sqli-basic").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Submission{
		UserID: 5, ChallengeID: chal.ID, Answer: "flag{a}",
		Status: model.SubmissionCorrect,
	}).Error; err != nil {
		t.Fatal(err)
	}
	items, _, err := svc.ListTraining(ctx, 5, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].Solved || items[1].Solved {
		t.Fatalf("solved flags = [%v, %v], want [true, false]", items[0].Solved, items[1].Solved)
	}
	// 其他用户视角不受影响。
	other, _, err := svc.ListTraining(ctx, 6, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if other[0].Solved {
		t.Fatal("other user must not see solved")
	}
}

func TestListTrainingPagination(t *testing.T) {
	svc, _ := newTrainingTestService(t)
	// page_size=1 的第二页应只有第二题。
	items, total, err := svc.ListTraining(context.Background(), 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 1 || items[0].Slug != "crypto-encode" {
		t.Fatalf("page 2 = %+v total=%d", items, total)
	}
}