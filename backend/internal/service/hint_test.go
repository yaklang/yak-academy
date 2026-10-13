package service

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func newHintTestEnv(t *testing.T) (*ChallengeService, *TopicService, *Deriver, repo.ScoreRepo, repo.EventRepo, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:hintunlocktest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	chalRepo := repo.NewChallengeRepo(gdb)
	eventRepo := repo.NewEventRepo(gdb)
	scoreRepo := repo.NewScoreRepo(gdb)
	bus := eventbus.New()
	deriver := NewDeriver(scoreRepo, repo.NewProgressRepo(gdb))
	bus.Subscribe(deriver.Handle)
	chalSvc := NewChallengeService(chalRepo, repo.NewSubmissionRepo(gdb), eventRepo, scoreRepo, bus)
	topicSvc := NewTopicService(repo.NewTopicRepo(gdb), repo.NewRoomRepo(gdb), chalRepo, repo.NewSubmissionRepo(gdb), eventRepo)
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		Slug: "hint-chal", Title: "提示题", Category: "web", Difficulty: model.DifficultyApprentice,
		Type: model.ChallengeStatic, Score: 100, Description: "desc",
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{x}"}}, []model.Hint{
		{Title: "免费", Content: "免费正文", Cost: 0},
		{Title: "付费", Content: "付费正文", Cost: 30},
	}); err != nil {
		t.Fatal(err)
	}
	return chalSvc, topicSvc, deriver, scoreRepo, eventRepo, gdb
}

func TestUnlockHintDeductsScoreAndRedactsDetail(t *testing.T) {
	chalSvc, topicSvc, _, scores, events, gdb := newHintTestEnv(t)
	ctx := context.Background()
	if err := scores.UpdateScore(ctx, 1, 100, 1); err != nil {
		t.Fatal(err)
	}
	var paid model.Hint
	if err := gdb.Where("title = ?", "付费").First(&paid).Error; err != nil {
		t.Fatal(err)
	}

	detail, err := topicSvc.GetChallengeDetail(ctx, 1, "hint-chal")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Hints) != 2 {
		t.Fatalf("hints=%d", len(detail.Hints))
	}
	for _, h := range detail.Hints {
		if h.Unlocked || h.Content != "" {
			t.Fatalf("locked hint must redact: %+v", h)
		}
	}

	res, err := chalSvc.UnlockHint(ctx, 1, "hint-chal", paid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Unlocked || res.Content != "付费正文" || res.Score != 70 {
		t.Fatalf("unlock result = %+v, want content + score 70", res)
	}
	rec, err := scores.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Score != 70 {
		t.Fatalf("score=%d, want 70", rec.Score)
	}
	var n int64
	gdb.Model(&model.Event{}).Where("type = ?", model.EventHintUnlocked).Count(&n)
	if n != 1 {
		t.Fatalf("hint.unlocked events = %d, want 1", n)
	}

	// 幂等：再解锁不重复扣分。
	res2, err := chalSvc.UnlockHint(ctx, 1, "hint-chal", paid.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Score != 70 {
		t.Fatalf("idempotent score=%d, want 70", res2.Score)
	}
	ok, err := events.HasHintUnlocked(ctx, 1, paid.ID)
	if err != nil || !ok {
		t.Fatalf("HasHintUnlocked = %v %v", ok, err)
	}
	detail, err = topicSvc.GetChallengeDetail(ctx, 1, "hint-chal")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range detail.Hints {
		if h.ID == paid.ID {
			found = true
			if !h.Unlocked || h.Content != "付费正文" {
				t.Fatalf("detail after unlock = %+v", h)
			}
		}
	}
	if !found {
		t.Fatal("paid hint missing in detail")
	}
}

func TestUnlockHintInsufficientScore(t *testing.T) {
	chalSvc, _, _, _, _, gdb := newHintTestEnv(t)
	ctx := context.Background()
	var paid model.Hint
	if err := gdb.Where("title = ?", "付费").First(&paid).Error; err != nil {
		t.Fatal(err)
	}
	_, err := chalSvc.UnlockHint(ctx, 2, "hint-chal", paid.ID)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err=%v, want ErrValidation", err)
	}
}

func TestUnlockHintWrongChallenge(t *testing.T) {
	chalSvc, _, _, scores, _, gdb := newHintTestEnv(t)
	ctx := context.Background()
	_ = scores.UpdateScore(ctx, 1, 100, 0)
	var paid model.Hint
	_ = gdb.Where("title = ?", "付费").First(&paid)
	_, err := chalSvc.UnlockHint(ctx, 1, "no-such", paid.ID)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err=%v, want not found", err)
	}
}

func TestDeriverHintUnlockedDeducts(t *testing.T) {
	deriver, scores, _, _, _ := newDeriverTestEnv(t)
	ctx := context.Background()
	_ = scores.UpdateScore(ctx, 9, 50, 0)
	deriver.Handle(ctx, model.Event{UserID: 9, Type: model.EventHintUnlocked,
		Payload: `{"hint_id":1,"cost":15}`})
	rec, err := scores.Get(ctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Score != 35 {
		t.Fatalf("score=%d, want 35", rec.Score)
	}
}
