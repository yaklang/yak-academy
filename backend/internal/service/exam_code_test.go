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

// TestExamCodeGrading 编程题：judge exam scope 提交 → submission.judged 回写
// 考试分；最新提交未判定前旧分作废；迟到判定被忽略。
func TestExamCodeGrading(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	svc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb))
	svc.SetNow(func() time.Time { return e.now })
	svc.SetEvents(repo.NewEventRepo(e.gdb), nil)
	svc.SetJudge(e.judge)

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "初赛编程题", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	alice := e.ensureUser(t, "exam-code-alice")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	pass := 20
	if _, err := svc.UpdatePaper(ctx, comp.ID, ExamPaperUpdate{PassScore: &pass}); err != nil {
		t.Fatalf("update paper: %v", err)
	}
	single, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 1, Type: "single", Title: "单选",
		Options: []ExamOption{{Key: "A", Text: "A"}, {Key: "B", Text: "B"}},
		Answer:  jsonRaw(t, "A"), Score: 10,
	})
	if err != nil {
		t.Fatalf("create single: %v", err)
	}
	challenge := &model.Challenge{Slug: "exam-code-question", Title: "编程题", Type: model.ChallengeCode}
	if err := e.gdb.Create(challenge).Error; err != nil {
		t.Fatalf("create code challenge: %v", err)
	}
	code, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 2, Type: "code", Title: "编程题", Score: 30, ChallengeID: challenge.ID,
	})
	if err != nil {
		t.Fatalf("create code question: %v", err)
	}
	if err := e.gdb.Model(&model.Competition{}).Where("id = ?", comp.ID).
		Update("start_time", e.now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("start competition: %v", err)
	}
	started, err := svc.StartAttempt(ctx, comp.ID, alice.ID)
	if err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, single.ID, jsonRaw(t, "A"), ""); err != nil {
		t.Fatalf("save single: %v", err)
	}

	first, err := svc.SubmitCode(ctx, comp.ID, alice.ID, code.ID, "print(1)", "10.0.0.9")
	if err != nil {
		t.Fatalf("submit code: %v", err)
	}
	if first.Status != model.SubmissionPending || first.SubmissionID == 0 {
		t.Fatalf("submission receipt: %+v", first)
	}
	attempt, err := svc.exam.FindAttempt(ctx, started.Attempt.ID)
	if err != nil {
		t.Fatalf("find attempt: %v", err)
	}
	if attempt.CodeScore != 0 || attempt.TotalScore != 0 {
		t.Fatalf("pending code must not score: %+v", attempt)
	}
	examView, err := svc.Exam(ctx, comp.ID, alice.ID)
	if err != nil {
		t.Fatalf("exam view: %v", err)
	}
	codeDraft := ""
	for _, answer := range examView.Answers {
		if answer.QuestionID == code.ID {
			codeDraft, _ = answer.Answer.(string)
		}
	}
	if codeDraft != "print(1)" {
		t.Fatalf("code draft not persisted: %q", codeDraft)
	}
	judged := func(submissionID uint, correct bool) {
		payload, _ := json.Marshal(map[string]any{
			"exam_attempt_id": attempt.ID, "submission_id": submissionID,
			"challenge_id": challenge.ID, "correct": correct, "score": 999,
			"verdict": map[bool]string{true: "ac", false: "wa"}[correct],
		})
		svc.HandleJudgeEvent(ctx, model.Event{
			Type: model.EventSubmissionJudged, UserID: alice.ID, Payload: string(payload),
		})
	}
	judged(first.SubmissionID, true)
	attempt, _ = svc.exam.FindAttempt(ctx, attempt.ID)
	if attempt.CodeScore != 30 || attempt.TotalScore != 30 {
		t.Fatalf("code score after AC: %+v", attempt)
	}

	// 第二次提交：旧分立即作废（最新提交为准）；旧判定迟到不得覆盖。
	if err := e.gdb.Model(&model.Submission{}).Where("id = ?", first.SubmissionID).
		Update("status", model.SubmissionCorrect).Error; err != nil {
		t.Fatalf("mark first judged: %v", err)
	}
	second, err := svc.SubmitCode(ctx, comp.ID, alice.ID, code.ID, "print(2)", "")
	if err != nil {
		t.Fatalf("submit code again: %v", err)
	}
	attempt, _ = svc.exam.FindAttempt(ctx, attempt.ID)
	if attempt.CodeScore != 0 {
		t.Fatalf("new pending submission must clear old score: %+v", attempt)
	}
	judged(first.SubmissionID, true)
	attempt, _ = svc.exam.FindAttempt(ctx, attempt.ID)
	if attempt.CodeScore != 0 {
		t.Fatalf("stale judgment must be ignored: %+v", attempt)
	}
	judged(second.SubmissionID, true)
	attempt, _ = svc.exam.FindAttempt(ctx, attempt.ID)
	if attempt.CodeScore != 30 {
		t.Fatalf("second judgment: %+v", attempt)
	}

	view, err := svc.SubmitAttempt(ctx, comp.ID, alice.ID)
	if err != nil {
		t.Fatalf("submit attempt: %v", err)
	}
	if view.Attempt.ObjectiveScore != 10 || view.Attempt.CodeScore != 30 ||
		view.Attempt.TotalScore != 40 || !view.Attempt.Passed {
		t.Fatalf("final scores: %+v", view.Attempt)
	}
	var standing model.ExamStanding
	if err := e.gdb.Where("competition_id = ? AND user_id = ?", comp.ID, alice.ID).First(&standing).Error; err != nil {
		t.Fatalf("standing: %v", err)
	}
	if standing.Score != 40 || standing.CodeScore != 30 {
		t.Fatalf("standing: %+v", standing)
	}
}

// TestJudgeSubmitExamScope 编程题提交绑定 exam_attempt_id，事件 payload 带作用域；
// 缺少 attempt id 直接拒绝。
func TestJudgeSubmitExamScope(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	user := e.ensureUser(t, "judge-exam-scope")
	challenge := &model.Challenge{Slug: "judge-exam-scope", Title: "编程题", Type: model.ChallengeCode}
	if err := e.gdb.Create(challenge).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	if _, err := e.judge.SubmitExam(ctx, user.ID, challenge.ID, 7, 0, "print(1)", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing attempt: want ErrValidation, got %v", err)
	}
	sub, err := e.judge.SubmitExam(ctx, user.ID, challenge.ID, 7, 42, "print(1)", "10.0.0.7")
	if err != nil {
		t.Fatalf("submit exam code: %v", err)
	}
	if sub.ExamAttemptID != 42 || sub.CompetitionID != 7 || sub.SourceIP != "10.0.0.7" {
		t.Fatalf("submission scope: %+v", sub)
	}
	var event model.Event
	if err := e.gdb.Where("type = ?", model.EventSubmissionCreated).Order("id desc").First(&event).Error; err != nil {
		t.Fatalf("created event: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.Payload), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["exam_attempt_id"] != float64(42) {
		t.Fatalf("event payload: %v", payload)
	}
}

// TestExamSubmissionSkipsGlobalScore 考试编程题判定不进入全局 XP/竞赛积分。
func TestExamSubmissionSkipsGlobalScore(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	deriver := NewDeriver(repo.NewScoreRepo(e.gdb), repo.NewProgressRepo(e.gdb))
	payload, _ := json.Marshal(map[string]any{
		"submission_id": 1, "challenge_id": 2, "correct": true, "score": 100,
		"competition_id": 3, "exam_attempt_id": 4,
	})
	deriver.Handle(ctx, model.Event{
		UserID: 5, Type: model.EventSubmissionJudged, Payload: string(payload),
	})
	var scores, competitionScores int64
	e.gdb.Model(&model.ScoreRecord{}).Count(&scores)
	e.gdb.Model(&model.CompetitionScore{}).Count(&competitionScores)
	if scores != 0 || competitionScores != 0 {
		t.Fatalf("exam submission leaked into derived scores: xp=%d competition=%d", scores, competitionScores)
	}
}
