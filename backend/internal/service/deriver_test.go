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

// 内容包删除守卫：不存在 Room 的 room.completed 事件跳过；
// 删除后重放不得为已清进度"复活"（event-sourcing-rules「内容包删除例外」）。
func TestDeriverRoomCompletedSkipsDeletedRoom(t *testing.T) {
	deriver, _, progress, events, gdb := newDeriverTestEnv(t)
	ctx := context.Background()
	rooms := repo.NewRoomRepo(gdb)
	if err := rooms.Upsert(ctx, &model.Room{
		Slug: "room-keep", TopicSlug: "topic-a", Title: "保留的教程",
		Difficulty: model.DifficultyApprentice,
	}); err != nil {
		t.Fatal(err)
	}
	lookup := func(ctx context.Context, slug string) bool {
		exists, err := rooms.ExistsBySlug(ctx, slug)
		return err == nil && exists
	}
	deriver.SetRoomLookup(lookup)

	evs := []model.Event{
		{UserID: 9, Type: model.EventRoomCompleted, Payload: `{"room_slug":"room-keep"}`},
		{UserID: 9, Type: model.EventRoomCompleted, Payload: `{"room_slug":"room-gone"}`}, // 从未存在的 Room
	}
	for i := range evs {
		if err := events.Append(ctx, &evs[i]); err != nil {
			t.Fatal(err)
		}
		deriver.Handle(ctx, evs[i])
	}
	completed, _, err := progress.Stats(ctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("completed = %d, want 1（room-gone 应被守卫跳过）", completed)
	}

	// 模拟内容包删除：Room 与进度行清理，事件 append-only 保留。
	if _, err := rooms.DeleteBySlugs(ctx, "room-keep"); err != nil {
		t.Fatal(err)
	}
	if _, err := progress.DeleteByRoomSlugs(ctx, []string{"room-keep"}); err != nil {
		t.Fatal(err)
	}

	// 启动重放：两个事件都因 Room 不存在被跳过，进度不复活。
	replayer := NewDeriver(repo.NewScoreRepo(gdb), repo.NewProgressRepo(gdb))
	replayer.SetRoomLookup(lookup)
	if err := replayer.Replay(ctx, events); err != nil {
		t.Fatalf("replay: %v", err)
	}
	completed, _, err = progress.Stats(ctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	if completed != 0 {
		t.Fatalf("completed after replay = %d, want 0（已删进度不得复活）", completed)
	}
	var eventRows int64
	gdb.Model(&model.Event{}).Count(&eventRows)
	if eventRows != 2 {
		t.Fatalf("events = %d, want 2（append-only 不得删除）", eventRows)
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
	view, err := svc.Leaderboard(ctx, 1, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if view.Total != 2 || len(view.Items) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", view.Total, len(view.Items))
	}
	if view.Items[0].Rank != 1 || view.Items[0].Username != "alice" || view.Items[0].Score != 100 || view.Items[0].SolveCount != 2 {
		t.Fatalf("entry[0] = %+v", view.Items[0])
	}
	if view.Items[1].Rank != 2 || view.Items[1].Username != "bob" {
		t.Fatalf("entry[1] = %+v", view.Items[1])
	}
	// 全场前三与我的排名不受分页影响；统计与积分分布来自派生表聚合。
	if len(view.Top) != 2 || view.Top[0].Username != "alice" {
		t.Fatalf("top = %+v", view.Top)
	}
	if view.Me == nil || view.Me.Rank != 1 || view.Me.Username != "alice" {
		t.Fatalf("me = %+v", view.Me)
	}
	s := view.Stats
	if s.Participants != 2 || s.TotalScore != 150 || s.TotalSolves != 3 || s.AverageScore != 75 || s.TopScore != 100 {
		t.Fatalf("stats = %+v", s)
	}
	if s.UpdatedAt == nil || view.Items[0].LastActiveAt == nil {
		t.Fatalf("timestamps should be present: stats=%+v entry=%+v", s, view.Items[0])
	}
	// max=100 → 档宽 20：bob(50) 落 40–59，alice(100) 落 80+。
	if len(view.Distribution) != 5 || view.Distribution[2].Count != 1 || view.Distribution[4].Count != 1 || view.Distribution[4].Label != "80+" {
		t.Fatalf("distribution = %+v", view.Distribution)
	}
}

func TestLeaderboardEmpty(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:lbrdempty?mode=memory&cache=shared"), &gorm.Config{})
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
	svc := NewUserProgressService(repo.NewScoreRepo(gdb), repo.NewProgressRepo(gdb), users)
	view, err := svc.Leaderboard(ctx, 1, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if view.Total != 0 || view.Items == nil || len(view.Items) != 0 {
		t.Fatalf("total=%d items=%v, want 0/empty", view.Total, view.Items)
	}
	if view.Top == nil || len(view.Top) != 0 || view.Me != nil {
		t.Fatalf("top=%v me=%+v, want empty/nil", view.Top, view.Me)
	}
	if view.Stats.Participants != 0 || view.Stats.TotalScore != 0 || view.Stats.AverageScore != 0 || view.Stats.UpdatedAt != nil {
		t.Fatalf("stats = %+v, want zero", view.Stats)
	}
	if view.Distribution == nil || len(view.Distribution) != 0 {
		t.Fatalf("distribution = %v, want empty", view.Distribution)
	}
}

func TestLeaderboardTiesShareRankAcrossPages(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:lbrdtietest?mode=memory&cache=shared"), &gorm.Config{})
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
	for _, name := range []string{"alice", "bob", "carol"} {
		if err := users.Create(ctx, &model.User{Username: name, GlobalRole: model.GlobalRoleStudent}); err != nil {
			t.Fatal(err)
		}
	}
	scores := repo.NewScoreRepo(gdb)
	svc := NewUserProgressService(scores, repo.NewProgressRepo(gdb), users)
	// alice 100 分 2 题；bob / carol 同为 50 分 1 题（并列）。
	if err := scores.UpdateScore(ctx, 1, 100, 2); err != nil {
		t.Fatal(err)
	}
	if err := scores.UpdateScore(ctx, 2, 50, 1); err != nil {
		t.Fatal(err)
	}
	if err := scores.UpdateScore(ctx, 3, 50, 1); err != nil {
		t.Fatal(err)
	}
	page1, err := svc.Leaderboard(ctx, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Items) != 2 || page1.Items[0].Rank != 1 || page1.Items[1].Rank != 2 {
		t.Fatalf("page1 ranks = %+v, want 1,2", page1.Items)
	}
	// 第二页的并列者必须仍是 rank 2（count-based），而非 offset 错位的 3。
	page2, err := svc.Leaderboard(ctx, 1, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 1 || page2.Items[0].Rank != 2 {
		t.Fatalf("page2 = %+v, want single entry with rank 2", page2.Items)
	}
}

// TestDistributionBands 积分分布是纯展示派生：负分单独一档，非负分固定
// 5 档且最后一档吸收所有越界桶；总数必须与 participants 一致。
func TestDistributionBands(t *testing.T) {
	cases := []struct {
		name    string
		max     int
		buckets []repo.ScoreBucket
		want    []ScoreDistributionBand
	}{
		{
			name: "zero score", max: 0,
			buckets: []repo.ScoreBucket{{Bucket: 0, Count: 3}},
			want:    []ScoreDistributionBand{{Label: "0", Count: 3}},
		},
		{
			name: "negative band", max: 0,
			buckets: []repo.ScoreBucket{{Bucket: -1, Count: 2}, {Bucket: 0, Count: 3}},
			want:    []ScoreDistributionBand{{Label: "负分", Count: 2}, {Label: "0", Count: 3}},
		},
		{
			name: "100 top", max: 100,
			buckets: []repo.ScoreBucket{{Bucket: 0, Count: 1}, {Bucket: 2, Count: 1}, {Bucket: 5, Count: 1}},
			want: []ScoreDistributionBand{
				{Label: "0–19", Count: 1}, {Label: "20–39", Count: 0}, {Label: "40–59", Count: 1},
				{Label: "60–79", Count: 0}, {Label: "80+", Count: 1},
			},
		},
		{
			name: "50 top", max: 50,
			buckets: []repo.ScoreBucket{{Bucket: 0, Count: 2}, {Bucket: 1, Count: 3}, {Bucket: 5, Count: 1}},
			want: []ScoreDistributionBand{
				{Label: "0–9", Count: 2}, {Label: "10–19", Count: 3}, {Label: "20–29", Count: 0},
				{Label: "30–39", Count: 0}, {Label: "40+", Count: 1},
			},
		},
		{
			name: "large top", max: 12345,
			buckets: []repo.ScoreBucket{{Bucket: 0, Count: 4}, {Bucket: 9, Count: 2}},
			want: []ScoreDistributionBand{
				{Label: "0–2499", Count: 4}, {Label: "2500–4999", Count: 0}, {Label: "5000–7499", Count: 0},
				{Label: "7500–9999", Count: 0}, {Label: "10000+", Count: 2},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildDistribution(c.max, c.buckets)
			if len(got) != len(c.want) {
				t.Fatalf("bands = %+v, want %+v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("band[%d] = %+v, want %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}
