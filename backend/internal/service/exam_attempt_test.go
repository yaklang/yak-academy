package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func TestExamAttemptLifecycle(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	svc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb))
	svc.SetNow(func() time.Time { return e.now })
	svc.SetEvents(repo.NewEventRepo(e.gdb), nil)

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "初赛作答", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	alice := e.ensureUser(t, "exam-attempt-alice")
	bob := e.ensureUser(t, "exam-attempt-bob")
	carol := e.ensureUser(t, "exam-attempt-carol")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	options := []ExamOption{{Key: "A", Text: "A"}, {Key: "B", Text: "B"}, {Key: "C", Text: "C"}}
	single, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 1, Type: "single", Title: "单选", Options: options, Answer: jsonRaw(t, "A"), Score: 10,
		Explanation: "选 A",
	})
	if err != nil {
		t.Fatalf("create single: %v", err)
	}
	multiple, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 2, Type: "multiple", Title: "多选", Options: options,
		Answer: jsonRaw(t, []string{"A", "C"}), Score: 20,
	})
	if err != nil {
		t.Fatalf("create multiple: %v", err)
	}
	truefalse, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 3, Type: "truefalse", Title: "判断", Answer: jsonRaw(t, true), Score: 5,
	})
	if err != nil {
		t.Fatalf("create truefalse: %v", err)
	}
	// 开赛（试卷/试题只能在 scheduled 阶段编辑）。
	if err := e.gdb.Model(&model.Competition{}).Where("id = ?", comp.ID).
		Update("start_time", e.now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("start competition: %v", err)
	}

	view, err := svc.Exam(ctx, comp.ID, alice.ID)
	if err != nil || view.Attempt != nil || len(view.Questions) != 3 {
		t.Fatalf("exam before start: %+v err=%v", view, err)
	}
	if view.Questions[0].Explanation != "" {
		t.Fatalf("explanation must be hidden before submission")
	}
	if _, err := svc.Exam(ctx, comp.ID, carol.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non participant: want ErrNotFound, got %v", err)
	}

	started, err := svc.StartAttempt(ctx, comp.ID, alice.ID)
	if err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	if started.Attempt == nil || started.Attempt.Status != model.ExamAttemptInProgress {
		t.Fatalf("attempt: %+v", started.Attempt)
	}
	// 时长 5400 秒（默认），竞赛结束 2h 后 → 取 90 分钟。
	if got := started.Attempt.ExpiresAt.Sub(started.Attempt.StartedAt); got != 90*time.Minute {
		t.Fatalf("expires duration: %v", got)
	}
	if started.Attempt.RemainingSeconds != 5400 {
		t.Fatalf("remaining: %d", started.Attempt.RemainingSeconds)
	}
	again, err := svc.StartAttempt(ctx, comp.ID, alice.ID)
	if err != nil || again.Attempt.ID != started.Attempt.ID {
		t.Fatalf("restart idempotent: %+v err=%v", again, err)
	}
	var startedEvents int64
	e.gdb.Model(&model.Event{}).Where("type = ?", model.EventExamAttemptStarted).Count(&startedEvents)
	if startedEvents != 1 {
		t.Fatalf("started events = %d, want 1", startedEvents)
	}

	saved, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, single.ID, jsonRaw(t, "A"), "10.0.0.8")
	if err != nil || saved.Answer != "A" {
		t.Fatalf("save single: %+v err=%v", saved, err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, multiple.ID, jsonRaw(t, []string{"C", "A"}), ""); err != nil {
		t.Fatalf("save multiple: %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, truefalse.ID, jsonRaw(t, false), ""); err != nil {
		t.Fatalf("save truefalse: %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, single.ID, jsonRaw(t, "Z"), ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid option: want ErrValidation, got %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, multiple.ID, jsonRaw(t, []string{"A", "A"}), ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate multiple: want ErrValidation, got %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, truefalse.ID, jsonRaw(t, "yes"), ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid bool: want ErrValidation, got %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, single.ID, json.RawMessage(`null`), ""); err != nil {
		t.Fatalf("clear answer: %v", err)
	}

	view, err = svc.Exam(ctx, comp.ID, alice.ID)
	if err != nil {
		t.Fatalf("exam after save: %v", err)
	}
	if len(view.Answers) != 3 || view.Attempt == nil {
		t.Fatalf("answers: %+v", view.Answers)
	}
	for _, answer := range view.Answers {
		if answer.QuestionID == single.ID && answer.Answer != nil {
			t.Fatalf("cleared answer should be null: %+v", answer)
		}
		if answer.Graded {
			t.Fatalf("grading must not run in attempt lifecycle")
		}
	}
	fresh, err := svc.exam.FindAttempt(ctx, started.Attempt.ID)
	if err != nil || fresh.LastIP != "10.0.0.8" {
		t.Fatalf("last ip: %+v err=%v", fresh, err)
	}

	// 时间推进到到期：Exam 惰性交卷，解析随后可见，保存被拒绝。
	e.now = started.Attempt.ExpiresAt
	expiredView, err := svc.Exam(ctx, comp.ID, alice.ID)
	if err != nil || expiredView.Attempt.Status != model.ExamAttemptExpired || !expiredView.Attempt.AutoSubmitted {
		t.Fatalf("expired view: %+v err=%v", expiredView.Attempt, err)
	}
	if expiredView.Attempt.RemainingSeconds != 0 || expiredView.Attempt.SubmittedAt == nil {
		t.Fatalf("expired attempt: %+v", expiredView.Attempt)
	}
	if expiredView.Questions[0].Explanation != "选 A" {
		t.Fatalf("explanation after submission: %+v", expiredView.Questions[0])
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, single.ID, jsonRaw(t, "A"), ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("save after expiry: want ErrValidation, got %v", err)
	}
	if _, err := svc.StartAttempt(ctx, comp.ID, alice.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("restart after expiry: want ErrValidation, got %v", err)
	}
	var submittedEvents int64
	e.gdb.Model(&model.Event{}).Where("type = ?", model.EventExamAttemptSubmitted).Count(&submittedEvents)
	if submittedEvents != 1 {
		t.Fatalf("submitted events = %d, want 1", submittedEvents)
	}

	// bob 的作答由 worker 到期扫描处理；重复扫描幂等。
	e.now = started.Attempt.StartedAt.Add(10 * time.Minute)
	if _, err := svc.StartAttempt(ctx, comp.ID, bob.ID); err != nil {
		t.Fatalf("start bob: %v", err)
	}
	e.now = started.Attempt.ExpiresAt.Add(20 * time.Minute)
	expired, err := svc.ExpireDueAttempts(ctx)
	if err != nil || expired != 1 {
		t.Fatalf("expire due: %d err=%v", expired, err)
	}
	expired, err = svc.ExpireDueAttempts(ctx)
	if err != nil || expired != 0 {
		t.Fatalf("expire due twice: %d err=%v", expired, err)
	}
}

// 回归：本地时区（+08:00）的“现在”不能把 UTC 存储的未来到期时间误判为过期。
// SQLite 按字符串比较时间，混用偏移量会让 12:20+00:00 看起来早于 20:00+08:00。
func TestExamExpiryComparesInstantsAcrossTimeZones(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	local := time.FixedZone("E2E-LOCAL", 8*3600)
	e.now = time.Date(2026, 1, 1, 20, 0, 0, 0, local)
	e.comps.now = func() time.Time { return e.now }
	svc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb))
	svc.SetNow(func() time.Time { return e.now })
	svc.SetEvents(repo.NewEventRepo(e.gdb), nil)

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "跨时区到期", Start: e.now.Add(time.Hour), End: e.now.Add(2 * time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	if _, err := svc.UpdatePaper(ctx, comp.ID, ExamPaperUpdate{DurationSeconds: intPtr(1800)}); err != nil {
		t.Fatalf("update paper: %v", err)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 1, Type: "single", Title: "题目",
		Options: []ExamOption{{Key: "A", Text: "A"}, {Key: "B", Text: "B"}},
		Answer:  jsonRaw(t, "A"), Score: 10,
	}); err != nil {
		t.Fatalf("create question: %v", err)
	}
	alice := e.ensureUser(t, "exam-tz-alice")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	// 开赛，但竞赛剩余时间（20 分钟）短于试卷时长（30 分钟）：到期时间取
	// 竞赛结束时刻，且由竞赛服务按 UTC 落库。
	start, end := e.now.Add(-time.Hour), e.now.Add(20*time.Minute)
	if _, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{StartTime: &start, EndTime: &end}); err != nil {
		t.Fatalf("start competition: %v", err)
	}
	started, err := svc.StartAttempt(ctx, comp.ID, alice.ID)
	if err != nil || started.Attempt == nil {
		t.Fatalf("start attempt: %+v err=%v", started, err)
	}

	expired, err := svc.ExpireDueAttempts(ctx)
	if err != nil || expired != 0 {
		t.Fatalf("future attempt must not expire: expired=%d err=%v", expired, err)
	}
	fresh, err := svc.exam.FindAttempt(ctx, started.Attempt.ID)
	if err != nil || fresh.Status != model.ExamAttemptInProgress {
		t.Fatalf("attempt status: %+v err=%v", fresh, err)
	}

	e.now = started.Attempt.ExpiresAt
	expired, err = svc.ExpireDueAttempts(ctx)
	if err != nil || expired != 1 {
		t.Fatalf("due attempt should expire: expired=%d err=%v", expired, err)
	}
}

func TestExamAttemptStartGuards(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	svc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb))
	svc.SetNow(func() time.Time { return e.now })

	scheduled, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "未开始的考试", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create scheduled comp: %v", err)
	}
	alice := e.ensureUser(t, "exam-guard-alice")
	if _, err := e.comps.ImportParticipants(ctx, scheduled.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", scheduled.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	if _, err := svc.StartAttempt(ctx, scheduled.ID, alice.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("start before running: want ErrValidation, got %v", err)
	}

	running, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "空试卷考试", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create running comp: %v", err)
	}
	if _, err := e.comps.ImportParticipants(ctx, running.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username},
	}); err != nil {
		t.Fatalf("import running participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", running.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark running entered: %v", err)
	}
	if _, err := svc.StartAttempt(ctx, running.ID, alice.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("start empty paper: want ErrValidation, got %v", err)
	}
}
