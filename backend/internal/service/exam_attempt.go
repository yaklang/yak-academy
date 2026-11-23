package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/safego"
)

// 考试作答生命周期：单次开始（服务端计时）、自动保存、到期自动交卷。
// 交卷评分/成绩在 exam_grading.go（客观题）与编程题 judge scope（后续）完成；
// 本文件不计算分数，只维护作答事实与事件。

const examExpireInterval = 5 * time.Second

const examExpireBatch = 100

// ExamParticipantView 选手考试页：题目已裁剪（不含标准答案），解析只在
// 交卷后返回；attempt=nil 表示尚未开始作答。
type ExamParticipantView struct {
	Competition ExamParticipantCompetition `json:"competition"`
	Paper       ExamPaperPublic            `json:"paper"`
	Attempt     *ExamAttemptView           `json:"attempt"`
	Questions   []ExamQuestionPublic       `json:"questions"`
	Answers     []ExamAnswerView           `json:"answers"`
	ServerTime  time.Time                  `json:"server_time"`
}

type ExamParticipantCompetition struct {
	ID          uint                    `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Kind        model.CompetitionKind   `json:"kind"`
	Format      model.CompetitionFormat `json:"format"`
	Status      model.CompetitionStatus `json:"status"`
	StartTime   time.Time               `json:"start_time"`
	EndTime     time.Time               `json:"end_time"`
}

type ExamPaperPublic struct {
	ID              uint   `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	DurationSeconds int    `json:"duration_seconds"`
	PassScore       int    `json:"pass_score"`
	TotalScore      int    `json:"total_score"`
}

type ExamQuestionPublic struct {
	ID          uint                   `json:"id"`
	Seq         int                    `json:"seq"`
	Type        model.ExamQuestionType `json:"type"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	Options     []ExamOption           `json:"options"`
	Score       int                    `json:"score"`
	ChallengeID uint                   `json:"challenge_id"`
	// Explanation 仅交卷后返回。
	Explanation string `json:"explanation,omitempty"`
}

type ExamAttemptView struct {
	ID               uint                    `json:"id"`
	Status           model.ExamAttemptStatus `json:"status"`
	StartedAt        time.Time               `json:"started_at"`
	ExpiresAt        time.Time               `json:"expires_at"`
	SubmittedAt      *time.Time              `json:"submitted_at,omitempty"`
	AutoSubmitted    bool                    `json:"auto_submitted"`
	RemainingSeconds int                     `json:"remaining_seconds"`
	ObjectiveScore   int                     `json:"objective_score"`
	CodeScore        int                     `json:"code_score"`
	TotalScore       int                     `json:"total_score"`
	Passed           bool                    `json:"passed"`
}

type ExamAnswerView struct {
	QuestionID       uint `json:"question_id"`
	Answer           any  `json:"answer"`
	Graded           bool `json:"graded"`
	Correct          bool `json:"correct"`
	Score            int  `json:"score"`
	CodeSubmissionID uint `json:"code_submission_id"`
}

func (s *ExamService) requireExamParticipant(ctx context.Context, competitionID, userID uint) (*model.Competition, *model.Participant, error) {
	comp, err := s.requireExamCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, err
	}
	part, err := s.comps.FindParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, nil, err
	}
	if part == nil || part.EnteredAt == nil {
		return nil, nil, ErrNotFound
	}
	return comp, part, nil
}

// Exam 选手考试页（未开始返回 attempt=null、题目可见；答案不含标准答案）。
func (s *ExamService) Exam(ctx context.Context, competitionID, userID uint) (*ExamParticipantView, error) {
	comp, _, err := s.requireExamParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	paper, err := s.EnsurePaper(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	questions, err := s.exam.ListQuestions(ctx, paper.ID)
	if err != nil {
		return nil, err
	}
	attempt, err := s.exam.FindAttemptByKey(ctx, paper.ID, userID)
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	now := s.now()
	if attempt != nil && attempt.Status == model.ExamAttemptInProgress && !now.Before(attempt.ExpiresAt) {
		s.expireAttempt(ctx, attempt, attempt.ExpiresAt)
		if reload, err := s.exam.FindAttempt(ctx, attempt.ID); err == nil {
			attempt = reload
		}
	}
	answers := []model.ExamAnswer{}
	if attempt != nil {
		answers, err = s.exam.ListAnswers(ctx, attempt.ID)
		if err != nil {
			return nil, err
		}
	}
	terminal := attempt != nil && attempt.Status.Terminal()
	questionViews := make([]ExamQuestionPublic, 0, len(questions))
	for i := range questions {
		question := &questions[i]
		options := []ExamOption{}
		if question.OptionsJSON != "" {
			_ = json.Unmarshal([]byte(question.OptionsJSON), &options)
		}
		view := ExamQuestionPublic{
			ID: question.ID, Seq: question.Seq, Type: question.Type,
			Title: question.Title, Content: question.Content, Options: options,
			Score: question.Score, ChallengeID: question.ChallengeID,
		}
		if terminal {
			view.Explanation = question.Explanation
		}
		questionViews = append(questionViews, view)
	}
	answerViews := make([]ExamAnswerView, 0, len(answers))
	for _, answer := range answers {
		var parsed any
		if answer.AnswerJSON != "" {
			_ = json.Unmarshal([]byte(answer.AnswerJSON), &parsed)
		}
		answerViews = append(answerViews, ExamAnswerView{
			QuestionID: answer.QuestionID, Answer: parsed, Graded: answer.Graded,
			Correct: answer.Correct, Score: answer.Score,
			CodeSubmissionID: answer.CodeSubmissionID,
		})
	}
	var attemptView *ExamAttemptView
	if attempt != nil {
		attemptView = buildExamAttemptView(attempt, now)
	}
	return &ExamParticipantView{
		Competition: ExamParticipantCompetition{
			ID: comp.ID, Name: comp.Name, Description: comp.Description,
			Kind: comp.Kind, Format: comp.Format, Status: comp.Status(now),
			StartTime: comp.StartTime, EndTime: comp.EndTime,
		},
		Paper: ExamPaperPublic{
			ID: paper.ID, Title: paper.Title, Description: paper.Description,
			DurationSeconds: paper.DurationSeconds, PassScore: paper.PassScore,
			TotalScore: paper.TotalScore,
		},
		Attempt: attemptView, Questions: questionViews, Answers: answerViews,
		ServerTime: now,
	}, nil
}

// StartAttempt 开始作答（单次；重复请求在仍进行中时幂等返回当前页面）。
func (s *ExamService) StartAttempt(ctx context.Context, competitionID, userID uint) (*ExamParticipantView, error) {
	comp, _, err := s.requireExamParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if comp.Status(now) != model.CompetitionRunning {
		return nil, fmt.Errorf("%w: competition is not running", ErrValidation)
	}
	paper, err := s.EnsurePaper(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	questions, err := s.exam.ListQuestions(ctx, paper.ID)
	if err != nil {
		return nil, err
	}
	if len(questions) == 0 {
		return nil, fmt.Errorf("%w: exam paper has no questions", ErrValidation)
	}
	existing, err := s.exam.FindAttemptByKey(ctx, paper.ID, userID)
	if err == nil {
		if existing.Status == model.ExamAttemptInProgress {
			if !now.Before(existing.ExpiresAt) {
				s.expireAttempt(ctx, existing, existing.ExpiresAt)
				return nil, fmt.Errorf("%w: exam time is up", ErrValidation)
			}
			return s.Exam(ctx, competitionID, userID)
		}
		return nil, fmt.Errorf("%w: exam attempt is already submitted", ErrValidation)
	}
	if !isNotFound(err) {
		return nil, err
	}
	expires := now.Add(time.Duration(paper.DurationSeconds) * time.Second)
	if comp.EndTime.Before(expires) {
		expires = comp.EndTime
	}
	if !expires.After(now) {
		return nil, fmt.Errorf("%w: competition has ended", ErrValidation)
	}
	attempt := &model.ExamAttempt{
		PaperID: paper.ID, CompetitionID: competitionID, UserID: userID,
		Status: model.ExamAttemptInProgress, StartedAt: now, ExpiresAt: expires,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.exam.CreateAttempt(ctx, attempt); err != nil {
		// 并发开始：唯一索引冲突时回读并返回当前作答。
		if _, findErr := s.exam.FindAttemptByKey(ctx, paper.ID, userID); findErr == nil {
			return s.Exam(ctx, competitionID, userID)
		}
		return nil, err
	}
	s.appendEvent(ctx, userID, model.EventExamAttemptStarted, map[string]any{
		"competition_id": competitionID, "paper_id": paper.ID, "attempt_id": attempt.ID,
		"expires_at": expires,
	})
	return s.Exam(ctx, competitionID, userID)
}

// SaveAnswer 自动保存一道题（仅进行中；到期惰性交卷）。
func (s *ExamService) SaveAnswer(ctx context.Context, competitionID, userID, questionID uint, raw json.RawMessage, ip string) (*ExamAnswerView, error) {
	comp, _, err := s.requireExamParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
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
	attempt, err := s.exam.FindAttemptByKey(ctx, paper.ID, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: start the exam before saving answers", ErrValidation)
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
	answerJSON, err := normalizeExamParticipantAnswer(question, raw)
	if err != nil {
		return nil, err
	}
	answer := &model.ExamAnswer{
		AttemptID: attempt.ID, QuestionID: question.ID, AnswerJSON: answerJSON,
		Graded: false, Correct: false, Score: 0, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.exam.UpsertAnswer(ctx, answer); err != nil {
		return nil, err
	}
	if ip != "" && ip != attempt.LastIP {
		if err := s.exam.UpdateAttemptSelective(ctx, attempt.ID, map[string]any{"last_ip": ip}); err != nil {
			return nil, err
		}
	}
	var parsed any
	if answerJSON != "" {
		_ = json.Unmarshal([]byte(answerJSON), &parsed)
	}
	return &ExamAnswerView{QuestionID: question.ID, Answer: parsed}, nil
}

func buildExamAttemptView(attempt *model.ExamAttempt, now time.Time) *ExamAttemptView {
	remaining := 0
	if attempt.Status == model.ExamAttemptInProgress {
		remaining = int(attempt.ExpiresAt.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	return &ExamAttemptView{
		ID: attempt.ID, Status: attempt.Status, StartedAt: attempt.StartedAt,
		ExpiresAt: attempt.ExpiresAt, SubmittedAt: attempt.SubmittedAt,
		AutoSubmitted: attempt.AutoSubmitted, RemainingSeconds: remaining,
		ObjectiveScore: attempt.ObjectiveScore, CodeScore: attempt.CodeScore,
		TotalScore: attempt.TotalScore, Passed: attempt.Passed,
	}
}

// normalizeExamParticipantAnswer 校验答案形状；null/空 = 清除作答（存空串）。
func normalizeExamParticipantAnswer(question *model.ExamQuestion, raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	var options []ExamOption
	if question.OptionsJSON != "" {
		_ = json.Unmarshal([]byte(question.OptionsJSON), &options)
	}
	switch question.Type {
	case model.ExamQuestionSingle:
		var key string
		if err := json.Unmarshal(trimmed, &key); err != nil {
			return "", fmt.Errorf("%w: single answer must be a string option key", ErrValidation)
		}
		key = strings.TrimSpace(key)
		if !examOptionExists(options, key) {
			return "", fmt.Errorf("%w: answer key %q is not an option", ErrValidation, key)
		}
		encoded, _ := json.Marshal(key)
		return string(encoded), nil
	case model.ExamQuestionMultiple:
		var keys []string
		if err := json.Unmarshal(trimmed, &keys); err != nil {
			return "", fmt.Errorf("%w: multiple answer must be an array of option keys", ErrValidation)
		}
		if len(keys) == 0 {
			return "", nil
		}
		seen := make(map[string]bool, len(keys))
		normalized := make([]string, 0, len(keys))
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if !examOptionExists(options, key) {
				return "", fmt.Errorf("%w: answer key %q is not an option", ErrValidation, key)
			}
			if seen[key] {
				return "", fmt.Errorf("%w: duplicate answer key %q", ErrValidation, key)
			}
			seen[key] = true
			normalized = append(normalized, key)
		}
		sort.Strings(normalized)
		encoded, _ := json.Marshal(normalized)
		return string(encoded), nil
	case model.ExamQuestionTrueFalse:
		var value bool
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", fmt.Errorf("%w: truefalse answer must be true or false", ErrValidation)
		}
		encoded, _ := json.Marshal(value)
		return string(encoded), nil
	case model.ExamQuestionCode:
		return "", fmt.Errorf("%w: code questions are submitted through the judge endpoint", ErrValidation)
	}
	return "", fmt.Errorf("%w: invalid question type", ErrValidation)
}

// StartWorker 启动到期自动交卷 worker（单进程；生产为单后端副本）。
func (s *ExamService) StartWorker(ctx context.Context) {
	safego.Go("exam.expireAttempts", func() { s.pollExpiredAttempts(ctx) })
}

func (s *ExamService) pollExpiredAttempts(ctx context.Context) {
	ticker := time.NewTicker(examExpireInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			safego.Run("exam.expireAttempts", func() { s.ExpireDueAttempts(ctx) })
		}
	}
}

// ExpireDueAttempts 单轮到期扫描（worker 与测试共用）。
func (s *ExamService) ExpireDueAttempts(ctx context.Context) (int, error) {
	now := s.now()
	attempts, err := s.exam.ListExpiredAttempts(ctx, now, examExpireBatch)
	if err != nil {
		log.Printf("exam: list expired attempts: %v", err)
		return 0, err
	}
	expired := 0
	for i := range attempts {
		attempt := &attempts[i]
		at := attempt.ExpiresAt
		if at.After(now) {
			at = now
		}
		if s.expireAttempt(ctx, attempt, at) {
			expired++
		}
	}
	return expired, nil
}

// expireAttempt 条件交卷（幂等）；返回是否本次真正由进行中转为过期。
func (s *ExamService) expireAttempt(ctx context.Context, attempt *model.ExamAttempt, at time.Time) bool {
	n, err := s.exam.ExpireAttempt(ctx, attempt.ID, at)
	if err != nil {
		log.Printf("exam: expire attempt %d: %v", attempt.ID, err)
		return false
	}
	if n == 0 {
		return false
	}
	attempt.Status = model.ExamAttemptExpired
	attempt.AutoSubmitted = true
	attempt.SubmittedAt = &at
	s.appendEvent(ctx, attempt.UserID, model.EventExamAttemptSubmitted, map[string]any{
		"competition_id": attempt.CompetitionID, "paper_id": attempt.PaperID,
		"attempt_id": attempt.ID, "auto_submitted": true, "submitted_at": at,
	})
	return true
}

func (s *ExamService) appendEvent(ctx context.Context, userID uint, t model.EventType, payload map[string]any) {
	if s.events == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	ev := model.Event{UserID: userID, Type: t, Payload: string(raw)}
	if err := s.events.Append(ctx, &ev); err != nil {
		log.Printf("exam: append event %s: %v", t, err)
		return
	}
	if s.bus != nil {
		s.bus.Publish(ctx, ev)
	}
}

// SetEvents 注入事件表与总线（开始/交卷审计）。未注入时只落业务表。
func (s *ExamService) SetEvents(events repo.EventRepo, bus *eventbus.Bus) {
	s.events = events
	s.bus = bus
}
