package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 考试交卷与客观题评分：单选/多选/判断即时判分，多选全对才得分；
// 编程题由 judge exam scope 回写（exam_code.go），此处跳过。
// 交卷事实写 exam.attempt.submitted，评分写 exam.attempt.graded；
// ExamStanding 只能由 ExamDeriver 从评分事件重建。

// SubmitAttempt 交卷：进行中 → submitted（已到期按 expired 自动交卷），幂等。
func (s *ExamService) SubmitAttempt(ctx context.Context, competitionID, userID uint) (*ExamParticipantView, error) {
	if _, _, err := s.requireExamParticipant(ctx, competitionID, userID); err != nil {
		return nil, err
	}
	paper, err := s.EnsurePaper(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	attempt, err := s.exam.FindAttemptByKey(ctx, paper.ID, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: start the exam before submitting", ErrValidation)
		}
		return nil, err
	}
	if attempt.Status.Terminal() {
		return s.Exam(ctx, competitionID, userID)
	}
	now := s.nowUTC()
	at := now
	status := model.ExamAttemptSubmitted
	auto := false
	if !now.Before(attempt.ExpiresAt) {
		at = attempt.ExpiresAt
		status = model.ExamAttemptExpired
		auto = true
	}
	if !s.finalizeAttempt(ctx, attempt, status, at, auto) {
		// 已被 worker 或并发请求交卷：读取最新状态（评分可能仍在进行）。
		if reload, err := s.exam.FindAttempt(ctx, attempt.ID); err == nil {
			attempt = reload
		}
	}
	return s.Exam(ctx, competitionID, userID)
}

// finalizeAttempt 条件交卷 + 评分；返回是否本次真正完成状态迁移。
func (s *ExamService) finalizeAttempt(ctx context.Context, attempt *model.ExamAttempt, status model.ExamAttemptStatus, at time.Time, auto bool) bool {
	n, err := s.exam.FinalizeAttempt(ctx, attempt.ID, status, at, auto)
	if err != nil {
		log.Printf("exam: finalize attempt %d: %v", attempt.ID, err)
		return false
	}
	if n == 0 {
		return false
	}
	attempt.Status = status
	attempt.AutoSubmitted = auto
	attempt.SubmittedAt = &at
	s.appendEvent(ctx, attempt.UserID, model.EventExamAttemptSubmitted, map[string]any{
		"competition_id": attempt.CompetitionID, "paper_id": attempt.PaperID,
		"attempt_id": attempt.ID, "auto_submitted": auto, "submitted_at": at,
	})
	if err := s.gradeAttempt(ctx, attempt); err != nil {
		log.Printf("exam: grade attempt %d: %v", attempt.ID, err)
	}
	return true
}

// gradeAttempt 客观题评分并写回 attempt/answer；幂等（可重复调用）。
func (s *ExamService) gradeAttempt(ctx context.Context, attempt *model.ExamAttempt) error {
	paper, err := s.exam.FindPaper(ctx, attempt.PaperID)
	if err != nil {
		return err
	}
	questions, err := s.exam.ListQuestions(ctx, paper.ID)
	if err != nil {
		return err
	}
	answers, err := s.exam.ListAnswers(ctx, attempt.ID)
	if err != nil {
		return err
	}
	answerByQuestion := make(map[uint]*model.ExamAnswer, len(answers))
	for i := range answers {
		answerByQuestion[answers[i].QuestionID] = &answers[i]
	}
	for i := range questions {
		question := &questions[i]
		if question.Type == model.ExamQuestionCode {
			continue
		}
		answer := answerByQuestion[question.ID]
		if answer == nil || answer.AnswerJSON == "" {
			continue
		}
		correct := examAnswerCorrect(question, answer.AnswerJSON)
		score := 0
		if correct {
			score = question.Score
		}
		if err := s.exam.UpdateAnswerGrading(ctx, attempt.ID, question.ID, correct, score); err != nil {
			return err
		}
	}
	return s.recomputeExamAttemptScores(ctx, attempt)
}

// recomputeExamAttemptScores 从已评分的作答事实重算客观/编程分并发布 graded
// 事件（客观题交卷与编程题判题回写共用；幂等，可重复调用）。
func (s *ExamService) recomputeExamAttemptScores(ctx context.Context, attempt *model.ExamAttempt) error {
	paper, err := s.exam.FindPaper(ctx, attempt.PaperID)
	if err != nil {
		return err
	}
	questions, err := s.exam.ListQuestions(ctx, paper.ID)
	if err != nil {
		return err
	}
	answers, err := s.exam.ListAnswers(ctx, attempt.ID)
	if err != nil {
		return err
	}
	answerByQuestion := make(map[uint]*model.ExamAnswer, len(answers))
	for i := range answers {
		answerByQuestion[answers[i].QuestionID] = &answers[i]
	}
	objectiveScore, codeScore := 0, 0
	for i := range questions {
		question := &questions[i]
		answer := answerByQuestion[question.ID]
		if answer == nil || !answer.Graded {
			continue
		}
		// 得分事实一律以 answer 表为准（judge 回写与交卷评分同源）。
		if answer.Correct {
			if question.Type == model.ExamQuestionCode {
				codeScore += question.Score
			} else {
				objectiveScore += question.Score
			}
		}
	}
	total := objectiveScore + codeScore
	passed := total >= paper.PassScore
	now := s.nowUTC()
	if err := s.exam.UpdateAttemptSelective(ctx, attempt.ID, map[string]any{
		"objective_score": objectiveScore, "code_score": codeScore,
		"total_score": total, "passed": passed, "updated_at": now,
	}); err != nil {
		return err
	}
	attempt.ObjectiveScore = objectiveScore
	attempt.CodeScore = codeScore
	attempt.TotalScore = total
	attempt.Passed = passed
	attempt.UpdatedAt = now
	duration := 0
	if attempt.SubmittedAt != nil {
		duration = int(attempt.SubmittedAt.Sub(attempt.StartedAt).Seconds())
		if duration < 0 {
			duration = 0
		}
	}
	s.appendEvent(ctx, attempt.UserID, model.EventExamAttemptGraded, map[string]any{
		"competition_id": attempt.CompetitionID, "paper_id": attempt.PaperID,
		"attempt_id": attempt.ID, "objective_score": objectiveScore,
		"code_score": codeScore, "total_score": total, "passed": passed,
		"duration_seconds": duration, "submitted_at": attempt.SubmittedAt,
	})
	if s.deriver != nil {
		if err := s.deriver.Derive(ctx, attempt.CompetitionID); err != nil {
			log.Printf("exam: derive standings competition=%d: %v", attempt.CompetitionID, err)
		}
	}
	return nil
}

// examAnswerCorrect 客观题精确比对（多选集合相等，顺序无关）。
func examAnswerCorrect(question *model.ExamQuestion, answerJSON string) bool {
	switch question.Type {
	case model.ExamQuestionSingle:
		var want, got string
		if json.Unmarshal([]byte(question.AnswerJSON), &want) != nil ||
			json.Unmarshal([]byte(answerJSON), &got) != nil {
			return false
		}
		return want == got
	case model.ExamQuestionMultiple:
		var want, got []string
		if json.Unmarshal([]byte(question.AnswerJSON), &want) != nil ||
			json.Unmarshal([]byte(answerJSON), &got) != nil {
			return false
		}
		sort.Strings(want)
		sort.Strings(got)
		if len(want) != len(got) {
			return false
		}
		for i := range want {
			if want[i] != got[i] {
				return false
			}
		}
		return true
	case model.ExamQuestionTrueFalse:
		var want, got bool
		if json.Unmarshal([]byte(question.AnswerJSON), &want) != nil ||
			json.Unmarshal([]byte(answerJSON), &got) != nil {
			return false
		}
		return want == got
	}
	return false
}
