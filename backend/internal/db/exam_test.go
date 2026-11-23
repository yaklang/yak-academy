package db

import (
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// TestExamDomainMigration 考试域五张表可自动迁移，关键唯一约束生效：
// 一试卷一题序、一试卷一人一次、一作答一题一行、一份成绩一行。
func TestExamDomainMigration(t *testing.T) {
	gdb := newTestDB(t, false)
	for _, entity := range []any{
		&model.ExamPaper{}, &model.ExamQuestion{}, &model.ExamAttempt{},
		&model.ExamAnswer{}, &model.ExamStanding{},
	} {
		if !gdb.Migrator().HasTable(entity) {
			t.Fatalf("missing table for %T", entity)
		}
	}

	now := time.Now()
	comp := &model.Competition{
		Name: "考试域迁移", Format: model.CompetitionFormatExam,
		StartTime: now, EndTime: now.Add(time.Hour),
	}
	if err := gdb.Create(comp).Error; err != nil {
		t.Fatalf("create competition: %v", err)
	}
	paper := &model.ExamPaper{
		CompetitionID: comp.ID, Title: "初赛", DurationSeconds: 3600, PassScore: 60,
	}
	if err := gdb.Create(paper).Error; err != nil {
		t.Fatalf("create paper: %v", err)
	}
	question := &model.ExamQuestion{
		PaperID: paper.ID, Seq: 1, Type: model.ExamQuestionSingle,
		Title: "YAK 是什么", OptionsJSON: `[{"key":"A","text":"安全实训平台"}]`,
		AnswerJSON: `"A"`, Score: 5,
	}
	if err := gdb.Create(question).Error; err != nil {
		t.Fatalf("create question: %v", err)
	}
	if err := gdb.Create(&model.ExamQuestion{PaperID: paper.ID, Seq: 1}).Error; err == nil {
		t.Fatalf("duplicate question seq should fail")
	}
	codeQuestion := &model.ExamQuestion{
		PaperID: paper.ID, Seq: 2, Type: model.ExamQuestionCode, Title: "编程题", Score: 20, ChallengeID: 7,
	}
	if err := gdb.Create(codeQuestion).Error; err != nil {
		t.Fatalf("create code question: %v", err)
	}

	attempt := &model.ExamAttempt{
		PaperID: paper.ID, CompetitionID: comp.ID, UserID: 9,
		Status: model.ExamAttemptInProgress, StartedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := gdb.Create(attempt).Error; err != nil {
		t.Fatalf("create attempt: %v", err)
	}
	if err := gdb.Create(&model.ExamAttempt{
		PaperID: paper.ID, CompetitionID: comp.ID, UserID: 9,
		Status: model.ExamAttemptInProgress, StartedAt: now, ExpiresAt: now.Add(time.Hour),
	}).Error; err == nil {
		t.Fatalf("duplicate attempt for same paper/user should fail")
	}
	answer := &model.ExamAnswer{AttemptID: attempt.ID, QuestionID: question.ID, AnswerJSON: `"A"`}
	if err := gdb.Create(answer).Error; err != nil {
		t.Fatalf("create answer: %v", err)
	}
	if err := gdb.Create(&model.ExamAnswer{AttemptID: attempt.ID, QuestionID: question.ID}).Error; err == nil {
		t.Fatalf("duplicate answer should fail")
	}
	standing := &model.ExamStanding{CompetitionID: comp.ID, UserID: 9, Score: 5, Passed: true}
	if err := gdb.Create(standing).Error; err != nil {
		t.Fatalf("create standing: %v", err)
	}
	if err := gdb.Create(&model.ExamStanding{CompetitionID: comp.ID, UserID: 9}).Error; err == nil {
		t.Fatalf("duplicate standing should fail")
	}
}

func TestExamEnums(t *testing.T) {
	for _, typ := range []model.ExamQuestionType{
		model.ExamQuestionSingle, model.ExamQuestionMultiple,
		model.ExamQuestionTrueFalse, model.ExamQuestionCode,
	} {
		if !typ.Valid() {
			t.Fatalf("type %q should be valid", typ)
		}
	}
	if model.ExamQuestionType("essay").Valid() {
		t.Fatalf("essay should be invalid")
	}
	if !model.ExamAttemptInProgress.Valid() || model.ExamAttemptInProgress.Terminal() {
		t.Fatalf("in_progress state helpers wrong")
	}
	for _, status := range []model.ExamAttemptStatus{model.ExamAttemptSubmitted, model.ExamAttemptExpired} {
		if !status.Valid() || !status.Terminal() {
			t.Fatalf("status %q should be terminal", status)
		}
	}
}
