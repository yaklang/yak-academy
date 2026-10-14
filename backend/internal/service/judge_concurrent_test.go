package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 并发提交下的幂等计分：重复判对合法（结算模型=分值只结算一次），但首通计分
// 恰好一次。judge 幂等守卫 + 派生器并发消费都在 -race 监视下跑完整管线。

func TestJudgeConcurrentSubmitsScoreExactlyOnce(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:judgeconcurrent?mode=memory&cache=shared"), &gorm.Config{})
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
	ctx := context.Background()
	chalRepo := repo.NewChallengeRepo(gdb)
	if err := chalRepo.Upsert(ctx, &model.Challenge{
		Slug: "chal-race", Title: "并发题", Type: model.ChallengeStatic, Score: 100,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{race}"}}, nil); err != nil {
		t.Fatal(err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "chal-race").First(&chal).Error; err != nil {
		t.Fatal(err)
	}

	bus := eventbus.New()
	judge := NewJudgeService(repo.NewSubmissionRepo(gdb), chalRepo,
		repo.NewLabInstanceRepo(gdb), repo.NewEventRepo(gdb), bus)
	// 真实派生器订阅同一总线：并发事件流下 ScoreRecord 只允许落一次结算。
	deriver := NewDeriver(repo.NewScoreRepo(gdb), repo.NewProgressRepo(gdb))
	bus.Subscribe(deriver.Handle)
	judge.StartWorker(ctx)

	const userID = 77
	const workers = 20
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := judge.Submit(ctx, userID, chal.ID, "flag{race}"); err != nil {
				t.Errorf("submit: %v", err)
			}
		}()
	}
	wg.Wait()

	// 等全部提交判完（无 pending 残留）。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var pending int64
		gdb.Model(&model.Submission{}).
			Where("status = ? AND user_id = ?", model.SubmissionPending, userID).Count(&pending)
		if pending == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	var leftover int64
	gdb.Model(&model.Submission{}).
		Where("status = ? AND user_id = ?", model.SubmissionPending, userID).Count(&leftover)
	if leftover != 0 {
		t.Fatalf("存在 %d 条未判完的提交", leftover)
	}

	// 全部判对：重复提交正确答案是合法行为。
	var correct int64
	gdb.Model(&model.Submission{}).
		Where("status = ? AND user_id = ?", model.SubmissionCorrect, userID).Count(&correct)
	if correct != workers {
		t.Errorf("correct = %d, want %d（重复判对应为 correct）", correct, workers)
	}

	// 计分恰好一次：只有一份提交带 100 分，其余 correct 均为 0 分。
	var scored, zero int64
	gdb.Model(&model.Submission{}).
		Where("user_id = ? AND status = ? AND score = ?", userID, model.SubmissionCorrect, 100).Count(&scored)
	gdb.Model(&model.Submission{}).
		Where("user_id = ? AND status = ? AND score = 0", userID, model.SubmissionCorrect).Count(&zero)
	if scored != 1 || zero != workers-1 {
		t.Errorf("scored=%d zero=%d, want 1/%d", scored, zero, workers-1)
	}

	// 派生端：ScoreRecord 恰好一次结算（总分=题目分值、solve_count=1）。
	rec, err := repo.NewScoreRepo(gdb).Get(ctx, userID)
	if err != nil {
		t.Fatalf("score record: %v", err)
	}
	if rec == nil || rec.Score != 100 || rec.SolveCount != 1 {
		t.Errorf("score record = %+v, want score=100 solve_count=1（并发下重复结算=事故）", rec)
	}
}
