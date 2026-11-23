package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 考试编程题：复用 judge 的 exam scope（Submission.ExamAttemptID），
// 判题事件回写 ExamAnswer 并重算考试分；全局 XP/竞赛积分派生跳过该作用域。

const maxExamCodeSource = 64 << 10

type ExamCodeSubmissionView struct {
	QuestionID   uint                    `json:"question_id"`
	SubmissionID uint                    `json:"submission_id"`
	Status       model.SubmissionStatus  `json:"status"`
	Verdict      model.SubmissionVerdict `json:"verdict,omitempty"`
}

// SubmitCode 提交编程题源码（仅进行中；同题重复提交以最新一次判定为准）。
func (s *ExamService) SubmitCode(ctx context.Context, competitionID, userID, questionID uint, answer, sourceIP string) (*ExamCodeSubmissionView, error) {
	comp, _, err := s.requireExamParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	now := s.nowUTC()
	if comp.Status(now) != model.CompetitionRunning {
		return nil, fmt.Errorf("%w: competition is not running", ErrValidation)
	}
	paper, err := s.EnsurePaper(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	question, err := s.exam.FindQuestion(ctx, questionID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if question.PaperID != paper.ID {
		return nil, ErrNotFound
	}
	if question.Type != model.ExamQuestionCode || question.ChallengeID == 0 {
		return nil, fmt.Errorf("%w: not a code question", ErrValidation)
	}
	attempt, err := s.exam.FindAttemptByKey(ctx, paper.ID, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: start the exam before submitting code", ErrValidation)
		}
		return nil, err
	}
	if attempt.Status != model.ExamAttemptInProgress {
		return nil, fmt.Errorf("%w: exam attempt is already submitted", ErrValidation)
	}
	if !now.Before(attempt.ExpiresAt) {
		s.expireAttempt(ctx, attempt, attempt.ExpiresAt)
		return nil, fmt.Errorf("%w: exam time is up", ErrValidation)
	}
	if s.judge == nil {
		return nil, fmt.Errorf("%w: code judge is not configured", ErrValidation)
	}
	sub, err := s.judge.SubmitExam(ctx, userID, question.ChallengeID, competitionID, attempt.ID, answer, sourceIP)
	if err != nil {
		return nil, err
	}
	// 绑定最新一次提交；旧提交的迟到判定会被 UpdateAnswerCodeGrading 的
	// submission_id 条件拒绝。answer_json 保存源码草稿供刷新后恢复。
	sourceJSON, _ := json.Marshal(answer)
	if err := s.exam.UpsertAnswer(ctx, &model.ExamAnswer{
		AttemptID: attempt.ID, QuestionID: question.ID, AnswerJSON: string(sourceJSON),
		CodeSubmissionID: sub.ID, Graded: false, Correct: false, Score: 0,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return nil, err
	}
	// 最新提交未判定前不计分（旧提交分数作废）。
	if err := s.recomputeExamAttemptScores(ctx, attempt); err != nil {
		return nil, err
	}
	if sourceIP != "" && sourceIP != attempt.LastIP {
		if err := s.exam.UpdateAttemptSelective(ctx, attempt.ID, map[string]any{"last_ip": sourceIP}); err != nil {
			return nil, err
		}
	}
	return &ExamCodeSubmissionView{
		QuestionID: question.ID, SubmissionID: sub.ID,
		Status: sub.Status, Verdict: sub.Verdict,
	}, nil
}

// HandleJudgeEvent 订阅 submission.judged：只处理 exam_attempt_id > 0 的判定，
// 回写编程题得分并发布 exam.attempt.graded（成绩派生由 ExamDeriver 完成）。
func (s *ExamService) HandleJudgeEvent(ctx context.Context, ev model.Event) {
	if ev.Type != model.EventSubmissionJudged {
		return
	}
	var payload struct {
		ExamAttemptID uint   `json:"exam_attempt_id"`
		SubmissionID  uint   `json:"submission_id"`
		ChallengeID   uint   `json:"challenge_id"`
		Correct       bool   `json:"correct"`
		Verdict       string `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
		return
	}
	if payload.ExamAttemptID == 0 || payload.SubmissionID == 0 || payload.ChallengeID == 0 {
		return
	}
	attempt, err := s.exam.FindAttempt(ctx, payload.ExamAttemptID)
	if err != nil {
		log.Printf("exam: judge event for unknown attempt %d: %v", payload.ExamAttemptID, err)
		return
	}
	if attempt.UserID != ev.UserID {
		log.Printf("exam: judge event user mismatch attempt=%d event_user=%d", attempt.ID, ev.UserID)
		return
	}
	paper, err := s.exam.FindPaper(ctx, attempt.PaperID)
	if err != nil {
		return
	}
	question, err := s.exam.FindQuestionByChallenge(ctx, paper.ID, payload.ChallengeID)
	if err != nil {
		// 非试卷内题目的判定（异常数据）不参与考试计分。
		return
	}
	if question.Type != model.ExamQuestionCode {
		return
	}
	score := 0
	if payload.Correct {
		score = question.Score
	}
	updated, err := s.exam.UpdateAnswerCodeGrading(
		ctx, attempt.ID, question.ID, payload.SubmissionID, payload.Correct, score)
	if err != nil {
		log.Printf("exam: grade code answer attempt=%d question=%d: %v", attempt.ID, question.ID, err)
		return
	}
	if updated == 0 {
		return // 迟到判定（已有更新提交）或作答未绑定
	}
	if err := s.recomputeExamAttemptScores(ctx, attempt); err != nil {
		log.Printf("exam: recompute code scores attempt=%d: %v", attempt.ID, err)
	}
}
