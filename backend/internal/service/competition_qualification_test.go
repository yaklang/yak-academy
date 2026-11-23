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

// TestQualificationPreviewAndApply 晋级：Preview 按个人榜筛选（零分不入选），
// Apply 导入目标邀请赛参赛者并落规则/名单；重复应用、目标非未开始、来源非
// CTF 均拒绝。
func TestQualificationPreviewAndApply(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	source, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "初赛来源", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	target, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "复赛目标", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	chal := e.seedChallenge(t, "qualification-score", "shared")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	if _, err := e.comps.BindChallenges(ctx, source.ID, body, nil); err != nil {
		t.Fatalf("bind: %v", err)
	}
	alice := e.ensureUser(t, "qual-alice")
	bob := e.ensureUser(t, "qual-bob")
	carol := e.ensureUser(t, "qual-carol")
	imported, err := e.comps.ImportParticipants(ctx, source.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username, carol.Username},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	codes := map[string]string{}
	for _, p := range imported.Participants {
		codes[p.Username] = p.InviteCode
	}
	for _, u := range []*model.User{alice, bob, carol} {
		if _, err := e.comps.Enter(ctx, u.ID, source.ID, codes[u.Username], model.GlobalRoleStudent); err != nil {
			t.Fatalf("enter %s: %v", u.Username, err)
		}
	}
	for _, u := range []*model.User{alice, bob} {
		if _, err := e.comps.SubmitFlag(ctx, e.judge, u.ID, source.ID, chal.Slug, "flag{qualification-score}", "1.1.1.1"); err != nil {
			t.Fatalf("submit %s: %v", u.Username, err)
		}
		e.drainJudge()
	}

	preview, err := e.comps.PreviewQualification(ctx, source.ID, target.ID, "min_score", 90)
	if err != nil {
		t.Fatalf("preview min_score: %v", err)
	}
	if preview.TotalParticipants != 3 || len(preview.Candidates) != 2 {
		t.Fatalf("min_score preview = %+v", preview)
	}
	if preview.Candidates[0].Username != alice.Username || preview.Candidates[0].Score != 100 {
		t.Fatalf("first candidate: %+v", preview.Candidates[0])
	}
	topN, err := e.comps.PreviewQualification(ctx, source.ID, target.ID, "top_n", 2)
	if err != nil || len(topN.Candidates) != 2 {
		t.Fatalf("top_n preview = %+v err=%v", topN, err)
	}
	topPercent, err := e.comps.PreviewQualification(ctx, source.ID, target.ID, "top_percent", 67)
	if err != nil || len(topPercent.Candidates) != 2 {
		t.Fatalf("top_percent preview = %+v err=%v", topPercent, err)
	}

	result, err := e.comps.ApplyQualification(ctx, 1, source.ID, target.ID, "min_score", 90)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Imported != 2 || len(result.Missing) != 0 || len(result.Preview.Candidates) != 2 {
		t.Fatalf("apply result: %+v", result)
	}
	targetParts, _, err := repo.NewCompetitionRepo(e.gdb).ListParticipants(ctx, target.ID, 0, 10)
	if err != nil || len(targetParts) != 2 {
		t.Fatalf("target participants = %+v err=%v", targetParts, err)
	}
	for _, p := range targetParts {
		if p.InviteCode == "" {
			t.Fatalf("participant missing invite code: %+v", p)
		}
	}
	history, err := e.comps.ListQualifications(ctx, target.ID)
	if err != nil || len(history) != 1 || len(history[0].Usernames) != 2 || history[0].SourceName != source.Name {
		t.Fatalf("history = %+v err=%v", history, err)
	}
	if _, err := e.comps.ApplyQualification(ctx, 1, source.ID, target.ID, "min_score", 90); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate apply: want ErrValidation, got %v", err)
	}
	if _, err := e.comps.PreviewQualification(ctx, source.ID, target.ID, "min_score", 90); err == nil {
		// 目标已应用晋级但仍在 scheduled：preview 本身允许；这里只确认不 panic。
	}
	// 目标必须未开始：把目标改到运行中后预览拒绝。
	if _, err := e.comps.Update(ctx, target.ID, CompetitionUpdate{StartTime: ptrTime(e.now.Add(-time.Minute))}); err != nil {
		t.Fatalf("start target: %v", err)
	}
	if _, err := e.comps.PreviewQualification(ctx, source.ID, target.ID, "min_score", 90); !errors.Is(err, ErrValidation) {
		t.Fatalf("running target: want ErrValidation, got %v", err)
	}
	awd, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 来源", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create awd: %v", err)
	}
	if _, err := e.comps.PreviewQualification(ctx, awd.ID, target.ID, "min_score", 0); !errors.Is(err, ErrValidation) {
		t.Fatalf("awd source: want ErrValidation, got %v", err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// 跨阶段晋级：考试已交卷者按成绩单排名进入目标邀请赛。
func TestQualificationFromExamSource(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	source, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "考试初赛", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour),
		Kind: "invitational", Format: "exam",
	})
	if err != nil {
		t.Fatalf("create exam source: %v", err)
	}
	target, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "复赛目标", Start: e.now.Add(4 * time.Hour), End: e.now.Add(6 * time.Hour),
		Kind: "invitational", Format: "ctf",
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	examSvc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb), repo.NewUserRepo(e.gdb))
	examSvc.SetNow(func() time.Time { return e.now })
	e.comps.SetQualificationSources(nil, examSvc)
	if _, err := examSvc.UpdatePaper(ctx, source.ID, ExamPaperUpdate{DurationSeconds: intPtr(1800), PassScore: intPtr(10)}); err != nil {
		t.Fatalf("paper: %v", err)
	}
	question, err := examSvc.CreateQuestion(ctx, source.ID, ExamQuestionInput{
		Seq: 1, Type: "single", Title: "题目",
		Options: []ExamOption{{Key: "A", Text: "A"}, {Key: "B", Text: "B"}},
		Answer:  jsonRaw(t, "A"), Score: 10,
	})
	if err != nil {
		t.Fatalf("question: %v", err)
	}
	alice := e.ensureUser(t, "qual-exam-alice")
	bob := e.ensureUser(t, "qual-exam-bob")
	imported, err := e.comps.ImportParticipants(ctx, source.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	codes := map[string]string{}
	for _, p := range imported.Participants {
		codes[p.Username] = p.InviteCode
	}
	start := e.now.Add(-time.Minute)
	if _, err := e.comps.Update(ctx, source.ID, CompetitionUpdate{StartTime: &start}); err != nil {
		t.Fatalf("start exam: %v", err)
	}
	for _, u := range []*model.User{alice, bob} {
		if _, err := e.comps.Enter(ctx, u.ID, source.ID, codes[u.Username], model.GlobalRoleStudent); err != nil {
			t.Fatalf("enter %s: %v", u.Username, err)
		}
	}
	if _, err := examSvc.StartAttempt(ctx, source.ID, alice.ID); err != nil {
		t.Fatalf("alice start: %v", err)
	}
	if _, err := examSvc.SaveAnswer(ctx, source.ID, alice.ID, question.ID, jsonRaw(t, "A"), ""); err != nil {
		t.Fatalf("alice answer: %v", err)
	}
	if _, err := examSvc.SubmitAttempt(ctx, source.ID, alice.ID); err != nil {
		t.Fatalf("alice submit: %v", err)
	}
	if _, err := examSvc.StartAttempt(ctx, source.ID, bob.ID); err != nil {
		t.Fatalf("bob start: %v", err)
	}
	if _, err := examSvc.SubmitAttempt(ctx, source.ID, bob.ID); err != nil {
		t.Fatalf("bob submit: %v", err)
	}

	preview, err := e.comps.PreviewQualification(ctx, source.ID, target.ID, "top_n", 1)
	if err != nil {
		t.Fatalf("preview exam: %v", err)
	}
	if preview.TotalParticipants != 2 || len(preview.Candidates) != 1 ||
		preview.Candidates[0].Username != alice.Username || preview.Candidates[0].Score != 10 {
		t.Fatalf("exam preview: %+v", preview)
	}
	result, err := e.comps.ApplyQualification(ctx, 1, source.ID, target.ID, "top_n", 1)
	if err != nil || result.Imported != 1 {
		t.Fatalf("apply exam qualification: %+v err=%v", result, err)
	}
	parts, total, err := repo.NewCompetitionRepo(e.gdb).ListParticipants(ctx, target.ID, 0, 10)
	if err != nil || total != 1 || parts[0].UserID != alice.ID {
		t.Fatalf("target participants: %+v total=%d err=%v", parts, total, err)
	}
}
