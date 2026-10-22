package service

import (
	"context"
	"errors"
	"strings"
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

// 独立内存库名（project-conventions：测试文件间禁止共用 cache=shared 库名）。
func newJudgeRunTestService(t *testing.T) (*JudgeService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:judgeruntest?mode=memory&cache=shared"), &gorm.Config{})
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
	svc := NewJudgeService(
		repo.NewSubmissionRepo(gdb), repo.NewChallengeRepo(gdb),
		repo.NewLabInstanceRepo(gdb), repo.NewEventRepo(gdb),
		repo.NewCompetitionRepo(gdb), eventbus.New(),
	)
	return svc, gdb
}

// scriptedRunEngine 记录每次输入并按测试注入的 fn 出结果（含阻塞能力）。
type scriptedRunEngine struct {
	mu     sync.Mutex
	inputs []string
	fn     func(input string) (string, error)
}

func (e *scriptedRunEngine) Run(_ context.Context, _, input string, _ time.Duration) (string, int64, error) {
	e.mu.Lock()
	e.inputs = append(e.inputs, input)
	fn := e.fn
	e.mu.Unlock()
	if fn == nil {
		return "", 0, nil
	}
	out, err := fn(input)
	return out, 50, err
}

func (e *scriptedRunEngine) collected() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.inputs...)
}

// waitForRunDone 轮询到终态（默认最多 5s）。
func waitForRunDone(t *testing.T, svc *JudgeService, userID uint, slug, id string) *CodeRunView {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		view, err := svc.GetCodeRun(context.Background(), userID, slug, id)
		if err != nil {
			t.Fatalf("poll run: %v", err)
		}
		if view.Status == "done" {
			return view
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s not done in 5s (status %s)", id, view.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCreateCodeRunValidations(t *testing.T) {
	svc, gdb := newJudgeRunTestService(t)
	chal := seedACMChallenge(t, gdb, "run-validate", 5)
	_ = chal
	// 非 code 题 → NotFound
	flag := &model.Challenge{Slug: "run-flag", Title: "F", Category: "ctf", Bank: model.BankCTF,
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeStatic, Score: 50}
	if err := gdb.Create(flag).Error; err != nil {
		t.Fatalf("seed flag challenge: %v", err)
	}
	// 引擎未配置：所有正当参数也 422
	_, err := svc.CreateCodeRun(context.Background(), 1, "run-validate", "println(1)", "01", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("engine-missing err = %v, want ErrValidation", err)
	}
	// 装配引擎后再走参数矩阵
	svc.SetCodeEngine(&scriptedRunEngine{})

	cases := []struct {
		name            string
		slug, source    string
		sample, stdin   string
		wantValidation  bool
		wantNotFound    bool
	}{
		{"flag challenge rejected", "run-flag", "println(1)", "x", "", false, true},
		{"unknown slug", "no-such", "println(1)", "x", "", false, true},
		{"both sample and stdin", "run-validate", "println(1)", "01", "1", true, false},
		{"neither sample nor stdin", "run-validate", "println(1)", "", "", true, false},
		{"source too large", "run-validate", strings.Repeat("a", maxCodeSource+1), "01", "", true, false},
		{"stdin too large", "run-validate", "println(1)", "", strings.Repeat("a", maxRunStdin+1), true, false},
		{"unknown sample", "run-validate", "println(1)", "nope", "", true, false},
		{"hidden sample rejected", "run-validate", "println(1)", "02", "", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := svc.CreateCodeRun(context.Background(), 1, c.slug, c.source, c.sample, c.stdin)
			if c.wantValidation && !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
			if c.wantNotFound && !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
			if !c.wantValidation && !c.wantNotFound && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestCodeRunSampleModeMatchesAndIsolation(t *testing.T) {
	svc, gdb := newJudgeRunTestService(t)
	seedACMChallenge(t, gdb, "run-sample", 5)
	var submissions, events int64
	gdb.Model(&model.Submission{}).Count(&submissions)
	gdb.Model(&model.Event{}).Count(&events)

	// 引擎直接回显样例 01 组的期望输出（seedACMChallenge：01 期望 "3\n"）
	engine := &scriptedRunEngine{fn: func(input string) (string, error) { return "3\n", nil }}
	svc.SetCodeEngine(engine)
	id, err := svc.CreateCodeRun(context.Background(), 7, "run-sample", "println(a+b)", "01", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	view := waitForRunDone(t, svc, 7, "run-sample", id)
	if view.Outcome != "ok" || view.Stdout != "3\n" {
		t.Fatalf("view = %+v, want ok with sample output", view)
	}
	if view.Matches == nil || !*view.Matches {
		t.Fatalf("matches = %v, want true", view.Matches)
	}
	// 引擎喂的是样例 01 组的输入（seedACMChallenge：01 输入 "1 2\n"）
	if got := engine.collected(); len(got) != 1 || got[0] != "1 2\n" {
		t.Fatalf("engine inputs = %q, want sample group input", got)
	}

	// 隔离不变式：自测运行不写 Submission、不写 Event（运营态，非判定）
	var subsAfter, evsAfter int64
	gdb.Model(&model.Submission{}).Count(&subsAfter)
	gdb.Model(&model.Event{}).Count(&evsAfter)
	if subsAfter != submissions || evsAfter != events {
		t.Fatalf("isolation broken: submissions %d→%d, events %d→%d",
			submissions, subsAfter, events, evsAfter)
	}
}

func TestCodeRunSampleMismatchAndCustomStdin(t *testing.T) {
	svc, gdb := newJudgeRunTestService(t)
	seedACMChallenge(t, gdb, "run-mismatch", 5)
	svc.SetCodeEngine(&scriptedRunEngine{fn: func(string) (string, error) {
		return "999\n", nil // 与样例 01 期望不符
	}})
	id, err := svc.CreateCodeRun(context.Background(), 7, "run-mismatch", "s", "01", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view := waitForRunDone(t, svc, 7, "run-mismatch", id); view.Matches == nil || *view.Matches {
		t.Fatalf("matches = %v, want false", view.Matches)
	}

	// 自定义输入：引擎收到 stdin 原文；matches 恒为 null
	engine := &scriptedRunEngine{fn: func(string) (string, error) { return "echo", nil }}
	svc.SetCodeEngine(engine)
	cid, err := svc.CreateCodeRun(context.Background(), 7, "run-mismatch", "s", "", "hello\nworld\n")
	if err != nil {
		t.Fatalf("create custom: %v", err)
	}
	view := waitForRunDone(t, svc, 7, "run-mismatch", cid)
	if view.Matches != nil {
		t.Fatalf("custom matches = %v, want nil", view.Matches)
	}
	if view.Stdout != "echo" {
		t.Fatalf("stdout = %q, want echo", view.Stdout)
	}
	if got := engine.collected(); len(got) == 0 || got[len(got)-1] != "hello\nworld\n" {
		t.Fatalf("engine inputs = %q, want custom stdin", got)
	}
}

func TestCodeRunTLEAndREOutcomes(t *testing.T) {
	svc, gdb := newJudgeRunTestService(t)
	seedACMChallenge(t, gdb, "run-tle", 5)
	svc.SetCodeEngine(&scriptedRunEngine{fn: func(string) (string, error) {
		return "", ErrCodeTimeout
	}})
	id, err := svc.CreateCodeRun(context.Background(), 7, "run-tle", "s", "", "1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view := waitForRunDone(t, svc, 7, "run-tle", id); view.Outcome != "tle" {
		t.Fatalf("outcome = %q, want tle", view.Outcome)
	}

	svc.SetCodeEngine(&scriptedRunEngine{fn: func(string) (string, error) {
		return "", &ErrCodeRunError{ExitCode: 2, Stderr: "boom"}
	}})
	rid, err := svc.CreateCodeRun(context.Background(), 7, "run-tle", "s", "", "1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view := waitForRunDone(t, svc, 7, "run-tle", rid); view.Outcome != "re" || view.Stderr != "boom" {
		t.Fatalf("view = %+v, want re with stderr boom", view)
	}
}

func TestCodeRunOneInFlightPerUser(t *testing.T) {
	svc, gdb := newJudgeRunTestService(t)
	seedACMChallenge(t, gdb, "run-inflight", 5)
	gate := make(chan struct{})
	svc.SetCodeEngine(&scriptedRunEngine{fn: func(string) (string, error) {
		<-gate // 阻塞第一个运行
		return "x", nil
	}})
	id, err := svc.CreateCodeRun(context.Background(), 7, "run-inflight", "s", "", "1")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	// 同用户第二个 → 422；其他用户不受影响
	if _, err := svc.CreateCodeRun(context.Background(), 7, "run-inflight", "s", "", "2"); !errors.Is(err, ErrValidation) {
		t.Fatalf("second same-user err = %v, want ErrValidation", err)
	}
	other, err := svc.CreateCodeRun(context.Background(), 8, "run-inflight", "s", "", "3")
	if err != nil {
		t.Fatalf("other user create: %v", err)
	}
	close(gate)
	waitForRunDone(t, svc, 7, "run-inflight", id)
	waitForRunDone(t, svc, 8, "run-inflight", other)
}

func TestCodeRunOwnershipAndSlugBinding(t *testing.T) {
	svc, gdb := newJudgeRunTestService(t)
	seedACMChallenge(t, gdb, "run-owner", 5)
	svc.SetCodeEngine(&scriptedRunEngine{})
	id, err := svc.CreateCodeRun(context.Background(), 7, "run-owner", "s", "", "1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForRunDone(t, svc, 7, "run-owner", id)
	if _, err := svc.GetCodeRun(context.Background(), 8, "run-owner", id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other user err = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetCodeRun(context.Background(), 7, "run-other", id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong slug err = %v, want ErrNotFound", err)
	}
}

func TestCodeRunTicketTTLExpiry(t *testing.T) {
	svc, gdb := newJudgeRunTestService(t)
	seedACMChallenge(t, gdb, "run-ttl", 5)
	clock := time.Now()
	svc.now = func() time.Time { return clock }
	svc.SetCodeEngine(&scriptedRunEngine{fn: func(string) (string, error) { return "ok", nil }})
	id, err := svc.CreateCodeRun(context.Background(), 7, "run-ttl", "s", "", "1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	waitForRunDone(t, svc, 7, "run-ttl", id)
	clock = clock.Add(runTicketTTL + 2*time.Minute)
	if _, err := svc.GetCodeRun(context.Background(), 7, "run-ttl", id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired err = %v, want ErrNotFound", err)
	}
}

func TestCodeRunStdoutTruncation(t *testing.T) {
	svc, gdb := newJudgeRunTestService(t)
	seedACMChallenge(t, gdb, "run-trunc", 5)
	big := strings.Repeat("x", runResultTruncate+1024)
	svc.SetCodeEngine(&scriptedRunEngine{fn: func(string) (string, error) { return big, nil }})
	id, err := svc.CreateCodeRun(context.Background(), 7, "run-trunc", "s", "", "1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	view := waitForRunDone(t, svc, 7, "run-trunc", id)
	if !view.Truncated || len(view.Stdout) != runResultTruncate {
		t.Fatalf("truncated = %v len = %d, want true/%d", view.Truncated, len(view.Stdout), runResultTruncate)
	}
}
