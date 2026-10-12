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

// newDeriverTestEnv 进程内 SQLite + Deriver 真实依赖。
func newDeriverTestEnv(t *testing.T) (*Deriver, repo.ScoreRepo, repo.ProgressRepo, repo.EventRepo, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:derivertest?mode=memory&cache=shared"), &gorm.Config{})
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
	scores := repo.NewScoreRepo(gdb)
	progress := repo.NewProgressRepo(gdb)
	events := repo.NewEventRepo(gdb)
	deriver := NewDeriver(scores, progress)
	return deriver, scores, progress, events, gdb
}

func TestDeriverSubmissionJudgedUpdatesScore(t *testing.T) {
	deriver, scores, _, events, _ := newDeriverTestEnv(t)
	ctx := context.Background()
	// 落库一个 correct 事件（含 score），广播派生。
	ev := model.Event{UserID: 1, Type: model.EventSubmissionJudged,
		Payload: `{"correct":true,"score":100,"submission_id":1,"challenge_id":1}`}
	if err := events.Append(ctx, &ev); err != nil {
		t.Fatal(err)
	}
	deriver.Handle(ctx, ev)
	rec, err := scores.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Score != 100 || rec.SolveCount != 1 {
		t.Fatalf("score=%d solve_count=%d, want 100/1", rec.Score, rec.SolveCount)
	}
}

func TestDeriverZeroScoreSkipped(t *testing.T) {
	deriver, scores, _, _, _ := newDeriverTestEnv(t)
	ctx := context.Background()
	// 重复通关的 correct 事件（score=0）不进派生表。
	deriver.Handle(ctx, model.Event{UserID: 2, Type: model.EventSubmissionJudged,
		Payload: `{"correct":true,"score":0,"submission_id":9,"challenge_id":1}`})
	if _, err := scores.Get(ctx, 2); err == nil {
		t.Fatal("zero-score event must not create score record")
	}
}

func TestDeriverRoomCompletedIdempotent(t *testing.T) {
	deriver, _, progress, _, gdb := newDeriverTestEnv(t)
	ctx := context.Background()
	ev := model.Event{UserID: 3, Type: model.EventRoomCompleted,
		Payload: `{"room_slug":"room-a"}`}
	// 同一事件消费两次（进程内 bus 至少一次语义）。
	deriver.Handle(ctx, ev)
	deriver.Handle(ctx, ev)
	var rows int64
	gdb.Model(&model.Progress{}).Where("user_id = ? AND room_slug = ?", 3, "room-a").Count(&rows)
	if rows != 1 {
		t.Fatalf("progress rows = %d, want 1 (idempotent)", rows)
	}
	completed, inProgress, err := progress.Stats(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 || inProgress != 0 {
		t.Fatalf("completed=%d in_progress=%d, want 1/0", completed, inProgress)
	}
}

func TestDeriverReplayRecoversFromEvents(t *testing.T) {
	deriver, scores, progress, events, _ := newDeriverTestEnv(t)
	ctx := context.Background()
	// 模拟崩溃窗口：事件已落库但派生器没消费。
	evs := []model.Event{
		{UserID: 5, Type: model.EventSubmissionJudged,
			Payload: `{"correct":true,"score":100,"submission_id":1,"challenge_id":1}`},
		{UserID: 5, Type: model.EventRoomCompleted, Payload: `{"room_slug":"room-b"}`},
	}
	for i := range evs {
		if err := events.Append(ctx, &evs[i]); err != nil {
			t.Fatal(err)
		}
	}
	// 启动补偿 Replay。
	if err := deriver.Replay(ctx, events); err != nil {
		t.Fatalf("replay: %v", err)
	}
	rec, err := scores.Get(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Score != 100 {
		t.Fatalf("score = %d, want 100", rec.Score)
	}
	completed, _, err := progress.Stats(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completed = %d, want 1", completed)
	}
}

func TestDeriverMarkInProgressThenCompleted(t *testing.T) {
	deriver, _, progress, _, _ := newDeriverTestEnv(t)
	ctx := context.Background()
	// 浏览 Room 记 in-progress，通关事件覆盖为 completed（FirstOrCreate Assign 语义）。
	if err := progress.MarkInProgress(ctx, 7, "room-c"); err != nil {
		t.Fatal(err)
	}
	completed, inProgress, err := progress.Stats(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 0 || inProgress != 1 {
		t.Fatalf("completed=%d in_progress=%d, want 0/1", completed, inProgress)
	}
	deriver.Handle(ctx, model.Event{UserID: 7, Type: model.EventRoomCompleted,
		Payload: `{"room_slug":"room-c"}`})
	completed, inProgress, err = progress.Stats(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 || inProgress != 0 {
		t.Fatalf("completed=%d in_progress=%d, want 1/0", completed, inProgress)
	}
}

func TestLeaderboardAssemblesEntries(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:lbrdtest?mode=memory&cache=shared"), &gorm.Config{})
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
	ctx := context.Background()
	users := repo.NewUserRepo(gdb)
	if err := users.Create(ctx, &model.User{Username: "alice", GlobalRole: model.GlobalRoleStudent}); err != nil {
		t.Fatal(err)
	}
	if err := users.Create(ctx, &model.User{Username: "bob", GlobalRole: model.GlobalRoleStudent}); err != nil {
		t.Fatal(err)
	}
	svc := NewUserProgressService(repo.NewScoreRepo(gdb), repo.NewProgressRepo(gdb), users)
	// alice 100 分 2 题、bob 50 分 1 题（派生表由 ScoreRepo 写入——测试直接构造派生数据）。
	if err := repo.NewScoreRepo(gdb).UpdateScore(ctx, 1, 100, 2); err != nil {
		t.Fatal(err)
	}
	if err := repo.NewScoreRepo(gdb).UpdateScore(ctx, 2, 50, 1); err != nil {
		t.Fatal(err)
	}
	entries, total, err := svc.Leaderboard(ctx, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(entries) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(entries))
	}
	if entries[0].Rank != 1 || entries[0].Username != "alice" || entries[0].Score != 100 || entries[0].SolveCount != 2 {
		t.Fatalf("entry[0] = %+v", entries[0])
	}
	if entries[1].Rank != 2 || entries[1].Username != "bob" {
		t.Fatalf("entry[1] = %+v", entries[1])
	}
}