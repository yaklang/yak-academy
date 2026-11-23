package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func setupGradingExam(t *testing.T) (*compEnv, *ExamService, *CompetitionView, *model.User, *model.User, []*ExamQuestionAdminView) {
	t.Helper()
	e := newCompEnv(t)
	ctx := context.Background()
	svc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb))
	svc.SetNow(func() time.Time { return e.now })
	svc.SetEvents(repo.NewEventRepo(e.gdb), nil)

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "初赛评分", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	alice := e.ensureUser(t, "exam-grade-alice")
	bob := e.ensureUser(t, "exam-grade-bob")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	title, passScore := "初赛评分卷", 20
	if _, err := svc.UpdatePaper(ctx, comp.ID, ExamPaperUpdate{Title: &title, PassScore: &passScore}); err != nil {
		t.Fatalf("update paper: %v", err)
	}
	options := []ExamOption{{Key: "A", Text: "A"}, {Key: "B", Text: "B"}, {Key: "C", Text: "C"}}
	questions := make([]*ExamQuestionAdminView, 0, 3)
	for _, input := range []ExamQuestionInput{
		{Seq: 1, Type: "single", Title: "单选", Options: options, Answer: jsonRaw(t, "A"), Score: 10, Explanation: "单选解析"},
		{Seq: 2, Type: "multiple", Title: "多选", Options: options, Answer: jsonRaw(t, []string{"A", "C"}), Score: 20, Explanation: "多选解析"},
		{Seq: 3, Type: "truefalse", Title: "判断", Answer: jsonRaw(t, true), Score: 5},
	} {
		question, err := svc.CreateQuestion(ctx, comp.ID, input)
		if err != nil {
			t.Fatalf("create question seq %d: %v", input.Seq, err)
		}
		questions = append(questions, question)
	}
	if err := e.gdb.Model(&model.Competition{}).Where("id = ?", comp.ID).
		Update("start_time", e.now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("start competition: %v", err)
	}
	return e, svc, comp, alice, bob, questions
}

// TestExamObjectiveGrading 客观题评分：单选/多选全对得分、判断错误 0 分；
// 交卷幂等；成绩派生与事件重放一致。
func TestExamObjectiveGrading(t *testing.T) {
	e, svc, comp, alice, bob, questions := setupGradingExam(t)
	ctx := context.Background()

	if _, err := svc.StartAttempt(ctx, comp.ID, alice.ID); err != nil {
		t.Fatalf("start alice: %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, questions[0].ID, jsonRaw(t, "A"), ""); err != nil {
		t.Fatalf("save single: %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, questions[1].ID, jsonRaw(t, []string{"C", "A"}), ""); err != nil {
		t.Fatalf("save multiple: %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, questions[2].ID, jsonRaw(t, false), ""); err != nil {
		t.Fatalf("save truefalse: %v", err)
	}
	view, err := svc.SubmitAttempt(ctx, comp.ID, alice.ID)
	if err != nil {
		t.Fatalf("submit alice: %v", err)
	}
	if view.Attempt.Status != model.ExamAttemptSubmitted || view.Attempt.AutoSubmitted {
		t.Fatalf("attempt status: %+v", view.Attempt)
	}
	if view.Attempt.ObjectiveScore != 30 || view.Attempt.CodeScore != 0 || view.Attempt.TotalScore != 30 {
		t.Fatalf("scores: %+v", view.Attempt)
	}
	if !view.Attempt.Passed {
		t.Fatalf("pass score 20 should pass with 30")
	}
	if view.Attempt.SubmittedAt == nil || view.Questions[0].Explanation != "单选解析" {
		t.Fatalf("result view: %+v %+v", view.Attempt, view.Questions[0])
	}
	grades := map[uint]ExamAnswerView{}
	for _, answer := range view.Answers {
		grades[answer.QuestionID] = answer
	}
	if !grades[questions[0].ID].Correct || grades[questions[0].ID].Score != 10 {
		t.Fatalf("single grade: %+v", grades[questions[0].ID])
	}
	if !grades[questions[1].ID].Correct || grades[questions[1].ID].Score != 20 {
		t.Fatalf("multiple grade: %+v", grades[questions[1].ID])
	}
	if grades[questions[2].ID].Correct || grades[questions[2].ID].Score != 0 || !grades[questions[2].ID].Graded {
		t.Fatalf("truefalse grade: %+v", grades[questions[2].ID])
	}

	// 幂等：重复交卷不再产生 graded 事件，成绩不变。
	if _, err := svc.SubmitAttempt(ctx, comp.ID, alice.ID); err != nil {
		t.Fatalf("submit again: %v", err)
	}
	var gradedEvents int64
	e.gdb.Model(&model.Event{}).Where("type = ?", model.EventExamAttemptGraded).Count(&gradedEvents)
	if gradedEvents != 1 {
		t.Fatalf("graded events = %d, want 1", gradedEvents)
	}
	var standing model.ExamStanding
	if err := e.gdb.Where("competition_id = ? AND user_id = ?", comp.ID, alice.ID).First(&standing).Error; err != nil {
		t.Fatalf("standing: %v", err)
	}
	if standing.Score != 30 || !standing.Passed || standing.DurationSeconds < 0 {
		t.Fatalf("alice standing: %+v", standing)
	}

	// bob 未作答直接交卷：0 分、不通过。
	if _, err := svc.StartAttempt(ctx, comp.ID, bob.ID); err != nil {
		t.Fatalf("start bob: %v", err)
	}
	bobView, err := svc.SubmitAttempt(ctx, comp.ID, bob.ID)
	if err != nil {
		t.Fatalf("submit bob: %v", err)
	}
	if bobView.Attempt.TotalScore != 0 || bobView.Attempt.Passed {
		t.Fatalf("bob result: %+v", bobView.Attempt)
	}

	// 删除派生成绩后重放事件可恢复（ExamStanding 只由 deriver 写）。
	if err := e.gdb.Where("competition_id = ?", comp.ID).Delete(&model.ExamStanding{}).Error; err != nil {
		t.Fatalf("clear standings: %v", err)
	}
	deriver := NewExamDeriver(repo.NewExamRepo(e.gdb))
	if err := deriver.Replay(ctx, repo.NewEventRepo(e.gdb)); err != nil {
		t.Fatalf("replay: %v", err)
	}
	var count int64
	e.gdb.Model(&model.ExamStanding{}).Where("competition_id = ?", comp.ID).Count(&count)
	if count != 2 {
		t.Fatalf("standings after replay = %d, want 2", count)
	}
	standing = model.ExamStanding{}
	if err := e.gdb.Where("competition_id = ? AND user_id = ?", comp.ID, alice.ID).First(&standing).Error; err != nil {
		t.Fatalf("alice standing after replay: %v", err)
	}
	if math.Abs(float64(standing.Score)-30) > 0.001 {
		t.Fatalf("alice score after replay: %+v", standing)
	}
}

// TestExamAutoSubmitGrades 到期自动交卷同样评分并进入成绩。
func TestExamAutoSubmitGrades(t *testing.T) {
	e, svc, comp, _, bob, questions := setupGradingExam(t)
	ctx := context.Background()
	started, err := svc.StartAttempt(ctx, comp.ID, bob.ID)
	if err != nil {
		t.Fatalf("start bob: %v", err)
	}
	if _, err := svc.SaveAnswer(ctx, comp.ID, bob.ID, questions[0].ID, jsonRaw(t, "A"), ""); err != nil {
		t.Fatalf("save bob answer: %v", err)
	}
	e.now = started.Attempt.ExpiresAt
	expired, err := svc.ExpireDueAttempts(ctx)
	if err != nil || expired != 1 {
		t.Fatalf("expire due: %d err=%v", expired, err)
	}
	attempt, err := svc.exam.FindAttempt(ctx, started.Attempt.ID)
	if err != nil {
		t.Fatalf("find attempt: %v", err)
	}
	if attempt.Status != model.ExamAttemptExpired || !attempt.AutoSubmitted {
		t.Fatalf("expired attempt: %+v", attempt)
	}
	if attempt.ObjectiveScore != 10 || attempt.TotalScore != 10 || attempt.Passed {
		t.Fatalf("auto grading: %+v", attempt)
	}
	var standing model.ExamStanding
	if err := e.gdb.Where("competition_id = ? AND user_id = ?", comp.ID, bob.ID).First(&standing).Error; err != nil {
		t.Fatalf("bob standing: %v", err)
	}
	if standing.Score != 10 || standing.Passed {
		t.Fatalf("bob standing: %+v", standing)
	}
}
