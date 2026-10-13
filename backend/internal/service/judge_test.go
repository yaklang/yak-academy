package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// newJudgeTestEnv 进程内 SQLite + 已发布的题目（static flag + regex flag 各一题）。
type judgeEnv struct {
	gdb   *gorm.DB
	judge *JudgeService
	bus   *eventbus.Bus
}

func newJudgeTestEnv(t *testing.T) *judgeEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:judgetest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		dbg, _ := gdb.DB()
		dbg.Close() // 关闭共享内存库，下个测试重建
	})
	chalRepo := repo.NewChallengeRepo(gdb)
	subRepo := repo.NewSubmissionRepo(gdb)
	evRepo := repo.NewEventRepo(gdb)
	bus := eventbus.New()
	judge := NewJudgeService(subRepo, chalRepo, evRepo, bus)
	// 造两道题：static（100 分）与 regex（200 分）。
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		RoomSlug: "room-static", Title: "静态题", Type: model.ChallengeStatic, Score: 100,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{static}"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		RoomSlug: "room-regex", Title: "正则题", Type: model.ChallengeRegex, Score: 200,
	}, []model.Flag{{Type: model.ChallengeRegex, Value: `^flag\{[a-z]+\}$`}}, nil); err != nil {
		t.Fatal(err)
	}
	return &judgeEnv{gdb: gdb, judge: judge, bus: bus}
}

// chalID 按 room slug 取题目 ID。
func (e *judgeEnv) chalID(t *testing.T, roomSlug string) uint {
	t.Helper()
	var chal model.Challenge
	if err := e.gdb.Where("room_slug = ?", roomSlug).First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	return chal.ID
}

// waitCorrect 轮询等待判定收敛（worker 异步），上限 5s。
func (e *judgeEnv) waitStatus(t *testing.T, subID uint) model.SubmissionStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var sub model.Submission
		if err := e.gdb.First(&sub, subID).Error; err == nil && sub.Status != model.SubmissionPending {
			return sub.Status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("judgement did not converge in time")
	return ""
}

func (e *judgeEnv) sub(t *testing.T, id uint) model.Submission {
	t.Helper()
	var sub model.Submission
	if err := e.gdb.First(&sub, id).Error; err != nil {
		t.Fatal(err)
	}
	return sub
}

func TestJudgeCorrectStaticFlag(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	env.judge.StartWorker(ctx)
	sub, err := env.judge.Submit(ctx, 1, env.chalID(t, "room-static"), "flag{static}")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Status != model.SubmissionPending {
		t.Fatalf("submit should return pending, got %s", sub.Status)
	}
	if got := env.waitStatus(t, sub.ID); got != model.SubmissionCorrect {
		t.Fatalf("status = %s, want correct", got)
	}
	// 首个解题者：n=0 → value = 100。
	if s := env.sub(t, sub.ID); s.Score != 100 {
		t.Fatalf("score = %d, want 100", s.Score)
	}
}

func TestJudgeIncorrectFlag(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	env.judge.StartWorker(ctx)
	sub, _ := env.judge.Submit(ctx, 1, env.chalID(t, "room-static"), "wrong")
	if got := env.waitStatus(t, sub.ID); got != model.SubmissionIncorrect {
		t.Fatalf("status = %s, want incorrect", got)
	}
	if s := env.sub(t, sub.ID); s.Score != 0 {
		t.Fatalf("score = %d, want 0", s.Score)
	}
}

func TestJudgeRegexFlag(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	env.judge.StartWorker(ctx)
	sub, _ := env.judge.Submit(ctx, 1, env.chalID(t, "room-regex"), "flag{anything}")
	if got := env.waitStatus(t, sub.ID); got != model.SubmissionCorrect {
		t.Fatalf("regex flag should match, got %s", got)
	}
}

func TestJudgeNoDoubleScore(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	env.judge.StartWorker(ctx)
	id := env.chalID(t, "room-static")
	first, _ := env.judge.Submit(ctx, 7, id, "flag{static}")
	if got := env.waitStatus(t, first.ID); got != model.SubmissionCorrect {
		t.Fatalf("first = %s", got)
	}
	second, _ := env.judge.Submit(ctx, 7, id, "flag{static}")
	if got := env.waitStatus(t, second.ID); got != model.SubmissionCorrect {
		t.Fatalf("second = %s", got)
	}
	// 第二次 correct 不计分（首通已计）。
	if s := env.sub(t, second.ID); s.Score != 0 {
		t.Fatalf("repeat correct score = %d, want 0", s.Score)
	}
	// submission.judged 事件中带正分数的只有一次（首通）。
	var judgedEvents []model.Event
	env.gdb.Where("type = ?", model.EventSubmissionJudged).Find(&judgedEvents)
	scored := 0
	for _, ev := range judgedEvents {
		if strings.Contains(ev.Payload, `"score":100`) {
			scored++
		}
	}
	if scored != 1 {
		t.Fatalf("scored events = %d, want 1", scored)
	}
}

func TestJudgeDynamicDecay(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	env.judge.StartWorker(ctx)
	id := env.chalID(t, "room-static")
	// 三个用户先通关（solve_count=3）。
	for user := uint(1); user <= 3; user++ {
		sub, _ := env.judge.Submit(ctx, user, id, "flag{static}")
		if got := env.waitStatus(t, sub.ID); got != model.SubmissionCorrect {
			t.Fatalf("user %d = %s", user, got)
		}
	}
	// 第四个用户：n=3 → value = 100 - 10*3 = 70。
	sub, _ := env.judge.Submit(ctx, 99, id, "flag{static}")
	if got := env.waitStatus(t, sub.ID); got != model.SubmissionCorrect {
		t.Fatalf("status = %s", got)
	}
	if s := env.sub(t, sub.ID); s.Score != 70 {
		t.Fatalf("score = %d, want 70", s.Score)
	}
}

func TestJudgeLinearDecayFloor(t *testing.T) {
	// 纯函数：initial=100, minimum=max(20,10)=20，n=9 → 100-90=10 → floor 20。
	if got := linearDecay(100, 9); got != 20 {
		t.Fatalf("linearDecay(100,9) = %d, want 20", got)
	}
	if got := linearDecay(100, 0); got != 100 {
		t.Fatalf("linearDecay(100,0) = %d, want 100", got)
	}
}

func TestJudgeGetSubmissionOwnerOnly(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	sub, _ := env.judge.Submit(ctx, 42, env.chalID(t, "room-static"), "flag{static}")
	if _, err := env.judge.GetSubmission(ctx, 42, sub.ID); err != nil {
		t.Fatalf("owner should read own submission: %v", err)
	}
	if _, err := env.judge.GetSubmission(ctx, 43, sub.ID); err != ErrNotFound {
		t.Fatalf("other user should get ErrNotFound, got %v", err)
	}
}

func TestJudgeSubmitIsAsync(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	// 不启动 worker：提交必须立即返回 pending（无同步判定）。
	sub, err := env.judge.Submit(ctx, 1, env.chalID(t, "room-static"), "flag{static}")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Status != model.SubmissionPending {
		t.Fatalf("submit without worker must stay pending, got %s", sub.Status)
	}
}

func TestJudgeQueueFullStillAccepted(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	// 容量 1 的队列：塞满后 Submit 不再报错，submission 落库保持 pending。
	env.judge.queue = make(chan uint, 1)
	env.judge.queue <- 1 // 占满
	sub, err := env.judge.Submit(ctx, 1, env.chalID(t, "room-static"), "flag{static}")
	if err != nil {
		t.Fatalf("queue full should still accept: %v", err)
	}
	if sub.Status != model.SubmissionPending {
		t.Fatalf("status = %s, want pending", sub.Status)
	}
	// 排空队列启动 worker：pending 轮询兜底把滞留记录判定完结。
	env.judge.queue = make(chan uint, 256)
	env.judge.StartWorker(ctx)
	if got := env.waitStatus(t, sub.ID); got != model.SubmissionCorrect {
		t.Fatalf("recovered status = %s, want correct", got)
	}
}

func TestJudgeWorkerRecoversStrandedPending(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	// 模拟崩溃窗口：不启动 worker 直接提交，产生滞留 pending。
	sub, err := env.judge.Submit(ctx, 5, env.chalID(t, "room-static"), "flag{static}")
	if err != nil {
		t.Fatal(err)
	}
	// 重启 worker：即时 pending 扫描入队并判定。
	env.judge.StartWorker(ctx)
	if got := env.waitStatus(t, sub.ID); got != model.SubmissionCorrect {
		t.Fatalf("status = %s, want correct", got)
	}
}

func TestJudgePollRecoversDirectDBPending(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	// 不经 Submit 直接落库 pending（模拟外部写入/重启前残留）：
	// 轮询兜底应扫描到并判定完结。
	sub := &model.Submission{
		UserID: 3, ChallengeID: env.chalID(t, "room-static"),
		Answer: "flag{static}", Status: model.SubmissionPending,
	}
	if err := env.gdb.Create(sub).Error; err != nil {
		t.Fatal(err)
	}
	env.judge.requeuePending(ctx)
	env.judge.StartWorker(context.Background()) // 空转 consume（pending 已投递）
	if got := env.waitStatus(t, sub.ID); got != model.SubmissionCorrect {
		t.Fatalf("status = %s, want correct", got)
	}
}

func TestJudgeIdempotentGuardOnDoubleDelivery(t *testing.T) {
	env := newJudgeTestEnv(t)
	ctx := context.Background()
	sub, err := env.judge.Submit(ctx, 9, env.chalID(t, "room-static"), "flag{static}")
	if err != nil {
		t.Fatal(err)
	}
	// 快路径与轮询同时投递（双入队）：同步调 judge 两次。
	env.judge.judge(ctx, sub.ID)
	env.judge.judge(ctx, sub.ID)
	// submission.judged 事件只产生一次（无重复计分事件）。
	var judged int64
	env.gdb.Model(&model.Event{}).Where("type = ?", model.EventSubmissionJudged).Count(&judged)
	if judged != 1 {
		t.Fatalf("judged events = %d, want 1", judged)
	}
	if s := env.sub(t, sub.ID); s.Status != model.SubmissionCorrect || s.Score != 100 {
		t.Fatalf("submission = %s/%d, want correct/100", s.Status, s.Score)
	}
}