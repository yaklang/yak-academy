package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func jsonRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// TestExamPaperManagement 试卷默认值、配置校验与开赛锁定。
func TestExamPaperManagement(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	svc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb), repo.NewUserRepo(e.gdb))
	svc.SetNow(func() time.Time { return e.now })

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "初赛考试", Start: e.now.Add(24 * time.Hour), End: e.now.Add(26 * time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	paper, err := svc.EnsurePaper(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure paper: %v", err)
	}
	if paper.Title != comp.Name || paper.DurationSeconds != 5400 || paper.PassScore != 60 || paper.TotalScore != 0 {
		t.Fatalf("paper defaults: %+v", paper)
	}
	again, err := svc.EnsurePaper(ctx, comp.ID)
	if err != nil || again.ID != paper.ID {
		t.Fatalf("ensure paper idempotent: %+v err=%v", again, err)
	}

	title, duration, pass := "初赛笔试卷", 3600, 60
	view, err := svc.UpdatePaper(ctx, comp.ID, ExamPaperUpdate{
		Title: &title, DurationSeconds: &duration, PassScore: &pass,
	})
	if err != nil {
		t.Fatalf("update paper: %v", err)
	}
	if view.Paper.Title != title || view.Paper.DurationSeconds != 3600 || len(view.Questions) != 0 {
		t.Fatalf("paper view: %+v", view)
	}
	if _, err := svc.UpdatePaper(ctx, comp.ID, ExamPaperUpdate{DurationSeconds: intPtr(10)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("short duration: want ErrValidation, got %v", err)
	}
	if _, err := svc.UpdatePaper(ctx, comp.ID, ExamPaperUpdate{PassScore: intPtr(-1)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative pass score: want ErrValidation, got %v", err)
	}

	ctf, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "不是考试", Start: e.now.Add(24 * time.Hour), End: e.now.Add(26 * time.Hour), Format: "ctf",
	})
	if err != nil {
		t.Fatalf("create ctf comp: %v", err)
	}
	if _, err := svc.Paper(ctx, ctf.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("non exam: want ErrValidation, got %v", err)
	}

	running, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "进行中的考试", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create running comp: %v", err)
	}
	lockedTitle := "x"
	if _, err := svc.UpdatePaper(ctx, running.ID, ExamPaperUpdate{Title: &lockedTitle}); !errors.Is(err, ErrValidation) {
		t.Fatalf("locked paper: want ErrValidation, got %v", err)
	}
}

// TestExamQuestionManagement 试题 CRUD：题型/答案校验、总分重算、开赛锁定。
func TestExamQuestionManagement(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	svc := NewExamService(repo.NewExamRepo(e.gdb), repo.NewCompetitionRepo(e.gdb), repo.NewChallengeRepo(e.gdb), repo.NewUserRepo(e.gdb))
	svc.SetNow(func() time.Time { return e.now })

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "初赛考试题", Start: e.now.Add(24 * time.Hour), End: e.now.Add(26 * time.Hour), Format: "exam",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	options := []ExamOption{{Key: "A", Text: "选项 A"}, {Key: "B", Text: "选项 B"}, {Key: "C", Text: "选项 C"}}
	single, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 1, Type: "single", Title: "单选题", Content: "题干", Options: options,
		Answer: jsonRaw(t, "A"), Score: 10,
	})
	if err != nil {
		t.Fatalf("create single: %v", err)
	}
	if single.Answer != "A" || single.Score != 10 || len(single.Options) != 3 {
		t.Fatalf("single view: %+v", single)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 1, Type: "single", Title: "重复题序", Options: options, Answer: jsonRaw(t, "A"), Score: 1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate seq: want ErrValidation, got %v", err)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 2, Type: "single", Title: "错误答案", Options: options, Answer: jsonRaw(t, "Z"), Score: 1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("answer not in options: want ErrValidation, got %v", err)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 2, Type: "single", Title: "选项不足", Options: options[:1], Answer: jsonRaw(t, "A"), Score: 1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("too few options: want ErrValidation, got %v", err)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 2, Type: "single", Title: "坏 key", Options: []ExamOption{{Key: "a", Text: "x"}, {Key: "B", Text: "y"}},
		Answer: jsonRaw(t, "a"), Score: 1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid option key: want ErrValidation, got %v", err)
	}

	multiple, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 2, Type: "multiple", Title: "多选题", Options: options,
		Answer: jsonRaw(t, []string{"C", "A"}), Score: 20,
	})
	if err != nil {
		t.Fatalf("create multiple: %v", err)
	}
	if !reflect.DeepEqual(multiple.Answer, []any{"A", "C"}) {
		t.Fatalf("multiple answer should be normalized: %#v", multiple.Answer)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 3, Type: "multiple", Title: "多选重复", Options: options,
		Answer: jsonRaw(t, []string{"A", "A"}), Score: 1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate multiple answer: want ErrValidation, got %v", err)
	}

	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 3, Type: "truefalse", Title: "判断题", Answer: jsonRaw(t, true), Score: 5,
	}); err != nil {
		t.Fatalf("create truefalse: %v", err)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 4, Type: "code", Title: "编程题", Score: 20,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("code without challenge: want ErrValidation, got %v", err)
	}
	challenge := &model.Challenge{Slug: "exam-code", Title: "考试编程题", Type: model.ChallengeCode}
	if err := e.gdb.Create(challenge).Error; err != nil {
		t.Fatalf("create code challenge: %v", err)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 4, Type: "code", Title: "编程题", Score: 20, ChallengeID: challenge.ID,
	}); err != nil {
		t.Fatalf("create code question: %v", err)
	}

	paper, err := svc.EnsurePaper(ctx, comp.ID)
	if err != nil || paper.TotalScore != 55 {
		t.Fatalf("total score: %+v err=%v", paper, err)
	}

	updated, err := svc.UpdateQuestion(ctx, comp.ID, multiple.ID, ExamQuestionInput{
		Seq: 5, Type: "multiple", Title: "多选题（改）", Options: options,
		Answer: jsonRaw(t, []string{"A", "B"}), Score: 30,
	})
	if err != nil {
		t.Fatalf("update question: %v", err)
	}
	if updated.Seq != 5 || updated.Score != 30 {
		t.Fatalf("updated view: %+v", updated)
	}
	paper, _ = svc.EnsurePaper(ctx, comp.ID)
	if paper.TotalScore != 65 {
		t.Fatalf("total after update: %+v", paper)
	}
	if err := svc.DeleteQuestion(ctx, comp.ID, single.ID); err != nil {
		t.Fatalf("delete question: %v", err)
	}
	paper, _ = svc.EnsurePaper(ctx, comp.ID)
	if paper.TotalScore != 55 {
		t.Fatalf("total after delete: %+v", paper)
	}
	if err := svc.DeleteQuestion(ctx, comp.ID, single.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: want ErrNotFound, got %v", err)
	}

	// 开赛锁定。
	if err := e.gdb.Model(&model.Competition{}).Where("id = ?", comp.ID).
		Update("start_time", e.now.Add(-time.Minute)).Error; err != nil {
		t.Fatalf("start competition: %v", err)
	}
	if _, err := svc.CreateQuestion(ctx, comp.ID, ExamQuestionInput{
		Seq: 9, Type: "truefalse", Title: "锁定后新增", Answer: jsonRaw(t, false), Score: 1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("locked create: want ErrValidation, got %v", err)
	}
	if _, err := svc.UpdateQuestion(ctx, comp.ID, multiple.ID, ExamQuestionInput{
		Seq: 5, Type: "multiple", Title: "锁定后修改", Options: options,
		Answer: jsonRaw(t, []string{"A"}), Score: 30,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("locked update: want ErrValidation, got %v", err)
	}
	if err := svc.DeleteQuestion(ctx, comp.ID, multiple.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("locked delete: want ErrValidation, got %v", err)
	}
}
