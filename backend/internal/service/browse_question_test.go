package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func newBasicBrowseEnv(t *testing.T) (*TopicService, *gorm.DB, uint) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:basictraining?mode=memory&cache=shared"), &gorm.Config{})
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
	chal := &model.Challenge{
		Slug: "basic-single-detail", Title: "单选详情", Category: "basics",
		Bank: model.BankBasic, Difficulty: model.DifficultyApprentice,
		Type: model.ChallengeSingle, Score: 10, Description: "题干",
		IsVisible: true,
	}
	if err := chalRepo.Upsert(context.Background(), chal, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := chalRepo.UpsertQuestion(context.Background(), &model.ChallengeQuestion{
		ChallengeID: chal.ID,
		OptionsJSON: `[{"key":"A","text":"甲"},{"key":"B","text":"乙"}]`,
		AnswerJSON:  `"B"`, Explanation: "乙正确",
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewTopicService(
		repo.NewTopicRepo(gdb), repo.NewRoomRepo(gdb), chalRepo,
		repo.NewSubmissionRepo(gdb), repo.NewEventRepo(gdb),
	)
	return svc, gdb, chal.ID
}

func TestBasicQuestionListAndDetail(t *testing.T) {
	svc, gdb, chalID := newBasicBrowseEnv(t)
	ctx := context.Background()

	items, total, err := svc.ListTraining(ctx, 1, string(model.BankBasic), nil, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(items))
	}
	if items[0].Type != model.ChallengeSingle || items[0].Bank != string(model.BankBasic) {
		t.Fatalf("list item type/bank = %s/%s", items[0].Type, items[0].Bank)
	}

	detail, err := svc.GetChallengeDetail(ctx, 1, "basic-single-detail")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Question == nil || detail.Question.Type != model.ChallengeSingle {
		t.Fatalf("question view = %+v", detail.Question)
	}
	if len(detail.Question.Options) != 2 || detail.Question.Options[1].Key != "B" {
		t.Fatalf("options = %+v", detail.Question.Options)
	}
	if detail.Question.Explanation != "" {
		t.Fatalf("unsolved explanation = %q, want empty", detail.Question.Explanation)
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, `"answer"`) {
		t.Fatalf("student detail leaked answer: %s", body)
	}
	if !strings.Contains(body, `"question"`) || !strings.Contains(body, `"options"`) {
		t.Fatalf("student detail missing question/options: %s", body)
	}

	// 造一条通关提交 → 解析可见。
	if err := repo.NewSubmissionRepo(gdb).Create(ctx, &model.Submission{
		UserID: 1, ChallengeID: chalID, Answer: `"B"`, Status: model.SubmissionCorrect, Score: 10,
	}); err != nil {
		t.Fatal(err)
	}
	detail, err = svc.GetChallengeDetail(ctx, 1, "basic-single-detail")
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Solved || detail.Question.Explanation != "乙正确" {
		t.Fatalf("solved detail = solved:%v explanation:%q", detail.Solved, detail.Question.Explanation)
	}
}
