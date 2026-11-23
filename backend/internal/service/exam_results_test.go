package service

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func TestExamAdminResultsAndCSV(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	svc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb), repo.NewUserRepo(e.gdb))
	svc.SetNow(func() time.Time { return e.now })
	svc.SetEvents(repo.NewEventRepo(e.gdb), nil)

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "初赛成绩单", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	if _, err := svc.UpdatePaper(ctx, comp.ID, ExamPaperUpdate{DurationSeconds: intPtr(1800), PassScore: intPtr(10)}); err != nil {
		t.Fatalf("update paper: %v", err)
	}
	question, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 1, Type: "single", Title: "题目",
		Options: []ExamOption{{Key: "A", Text: "A"}, {Key: "B", Text: "B"}},
		Answer:  jsonRaw(t, "A"), Score: 10,
	})
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	alice := e.ensureUser(t, "exam-results-alice")
	bob := e.ensureUser(t, "exam-results-bob")
	carol := e.ensureUser(t, "exam-results-carol")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username, carol.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).
		Where("competition_id = ? AND user_id IN ?", comp.ID, []uint{alice.ID, bob.ID}).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	if err := e.gdb.Model(&model.Competition{}).Where("id = ?", comp.ID).
		Update("start_time", e.now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("start competition: %v", err)
	}

	e.now = e.now.Add(1 * time.Minute)
	if _, err := svc.StartAttempt(ctx, comp.ID, alice.ID); err != nil {
		t.Fatalf("start alice: %v", err)
	}
	e.now = e.now.Add(30 * time.Second)
	if _, err := svc.SaveAnswer(ctx, comp.ID, alice.ID, question.ID, jsonRaw(t, "A"), ""); err != nil {
		t.Fatalf("save alice: %v", err)
	}
	if _, err := svc.SubmitAttempt(ctx, comp.ID, alice.ID); err != nil {
		t.Fatalf("submit alice: %v", err)
	}

	e.now = e.now.Add(1 * time.Minute)
	bobAttempt, err := svc.StartAttempt(ctx, comp.ID, bob.ID)
	if err != nil {
		t.Fatalf("start bob: %v", err)
	}
	e.now = bobAttempt.Attempt.ExpiresAt.Add(time.Minute)
	if expired, err := svc.ExpireDueAttempts(ctx); err != nil || expired != 1 {
		t.Fatalf("expire bob: expired=%d err=%v", expired, err)
	}

	view, err := svc.AdminResults(ctx, comp.ID)
	if err != nil {
		t.Fatalf("admin results: %v", err)
	}
	if view.Paper.TotalScore != 10 || view.Paper.PassScore != 10 {
		t.Fatalf("paper: %+v", view.Paper)
	}
	if len(view.Results) != 3 {
		t.Fatalf("results: %+v", view.Results)
	}
	got := map[uint]ExamResultRow{}
	for _, row := range view.Results {
		got[row.UserID] = row
	}
	if row := got[alice.ID]; row.Rank != 1 || row.Status != ExamResultSubmitted ||
		row.TotalScore != 10 || !row.Passed || row.DurationSeconds != 30 {
		t.Fatalf("alice row: %+v", row)
	}
	if row := got[bob.ID]; row.Rank != 2 || row.Status != ExamResultExpired || row.TotalScore != 0 {
		t.Fatalf("bob row: %+v", row)
	}
	if row := got[carol.ID]; row.Rank != 0 || row.Status != ExamResultNotStarted || row.Entered {
		t.Fatalf("carol row: %+v", row)
	}
	summary := view.Summary
	if summary.Participants != 3 || summary.Entered != 2 || summary.Started != 2 ||
		summary.Submitted != 1 || summary.Expired != 1 || summary.Passed != 1 ||
		summary.AverageScore != 5 || summary.HighestScore != 10 {
		t.Fatalf("summary: %+v", summary)
	}

	raw, filename, err := svc.ResultsCSV(ctx, comp.ID)
	if err != nil {
		t.Fatalf("results csv: %v", err)
	}
	if !strings.HasSuffix(filename, "-exam-results.csv") {
		t.Fatalf("filename: %s", filename)
	}
	if !strings.HasPrefix(string(raw), "\xEF\xBB\xBF") {
		t.Fatal("CSV must start with UTF-8 BOM")
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("csv rows: %d", len(records))
	}
	if records[1][0] != "1" || records[1][1] != alice.Username || records[1][3] != "已交卷" || records[1][7] != "是" {
		t.Fatalf("alice csv: %+v", records[1])
	}
	if records[2][0] != "2" || records[2][1] != bob.Username || records[2][3] != "超时交卷" {
		t.Fatalf("bob csv: %+v", records[2])
	}
	if records[3][0] != "" || records[3][1] != carol.Username || records[3][3] != "未开始" {
		t.Fatalf("carol csv: %+v", records[3])
	}
}
