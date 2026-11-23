package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 管理端试卷/试题管理：试卷与题目仅在竞赛开始前可编辑（开赛后锁定，
// 防止赛题在作答期间变化）；题目分值变化后重算 paper.total_score。
// 标准答案与解析只经管理端接口返回，选手端视图另行裁剪。

const (
	maxExamQuestionContent = 64 << 10
	maxExamExplanation     = 16 << 10
	maxExamOptions         = 26
	maxExamOptionText      = 500
)

var examOptionKeyRe = regexp.MustCompile(`^[A-Z0-9]{1,8}$`)

type ExamPaperUpdate struct {
	Title           *string `json:"title"`
	Description     *string `json:"description"`
	DurationSeconds *int    `json:"duration_seconds"`
	PassScore       *int    `json:"pass_score"`
}

type ExamQuestionInput struct {
	Seq         int             `json:"seq"`
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Content     string          `json:"content"`
	Options     []ExamOption    `json:"options"`
	Answer      json.RawMessage `json:"answer"`
	Explanation string          `json:"explanation"`
	Score       int             `json:"score"`
	ChallengeID uint            `json:"challenge_id"`
}

// Paper 管理端读取试卷（不存在时自动创建）。
func (s *ExamService) Paper(ctx context.Context, competitionID uint) (*ExamPaperView, error) {
	paper, err := s.EnsurePaper(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	questions, err := s.exam.ListQuestions(ctx, paper.ID)
	if err != nil {
		return nil, err
	}
	views := make([]ExamQuestionAdminView, 0, len(questions))
	for i := range questions {
		views = append(views, newExamQuestionAdminView(&questions[i]))
	}
	return &ExamPaperView{Paper: *paper, Questions: views}, nil
}

func (s *ExamService) requireEditablePaper(ctx context.Context, competitionID uint) (*model.Competition, *model.ExamPaper, error) {
	comp, err := s.requireExamCompetition(ctx, competitionID)
	if err != nil {
		return nil, nil, err
	}
	if comp.Status(s.now()) != model.CompetitionScheduled {
		return nil, nil, fmt.Errorf("%w: exam paper is locked after the competition starts", ErrValidation)
	}
	paper, err := s.EnsurePaper(ctx, competitionID)
	if err != nil {
		return nil, nil, err
	}
	return comp, paper, nil
}

// UpdatePaper 修改试卷配置：时长 300..86400 秒，通过分 0..100000。
func (s *ExamService) UpdatePaper(ctx context.Context, competitionID uint, in ExamPaperUpdate) (*ExamPaperView, error) {
	_, paper, err := s.requireEditablePaper(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if in.Title == nil && in.Description == nil && in.DurationSeconds == nil && in.PassScore == nil {
		return nil, fmt.Errorf("%w: empty exam paper update", ErrValidation)
	}
	fields := map[string]any{}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" || len(title) > 200 {
			return nil, fmt.Errorf("%w: title is required and must be <=200 characters", ErrValidation)
		}
		fields["title"] = title
	}
	if in.Description != nil {
		if len(*in.Description) > 2000 {
			return nil, fmt.Errorf("%w: description exceeds 2000 characters", ErrValidation)
		}
		fields["description"] = strings.TrimSpace(*in.Description)
	}
	if in.DurationSeconds != nil {
		if *in.DurationSeconds < 300 || *in.DurationSeconds > 86400 {
			return nil, fmt.Errorf("%w: duration_seconds must be in 300..86400", ErrValidation)
		}
		fields["duration_seconds"] = *in.DurationSeconds
	}
	if in.PassScore != nil {
		if *in.PassScore < 0 || *in.PassScore > 100000 {
			return nil, fmt.Errorf("%w: pass_score must be in 0..100000", ErrValidation)
		}
		fields["pass_score"] = *in.PassScore
	}
	if err := s.exam.UpdatePaperSelective(ctx, paper.ID, fields); err != nil {
		return nil, err
	}
	return s.Paper(ctx, competitionID)
}

// CreateQuestion 新增试题（seq 在试卷内唯一）。
func (s *ExamService) CreateQuestion(ctx context.Context, competitionID uint, in ExamQuestionInput) (*ExamQuestionAdminView, error) {
	_, paper, err := s.requireEditablePaper(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	question, err := s.buildExamQuestion(ctx, paper.ID, in)
	if err != nil {
		return nil, err
	}
	if _, err := s.exam.FindQuestionBySeq(ctx, paper.ID, question.Seq); err == nil {
		return nil, fmt.Errorf("%w: question seq %d already exists", ErrValidation, question.Seq)
	} else if !isNotFound(err) {
		return nil, err
	}
	if err := s.exam.CreateQuestion(ctx, question); err != nil {
		if isExamUniqueConflict(err) {
			return nil, fmt.Errorf("%w: question seq %d already exists", ErrValidation, question.Seq)
		}
		return nil, err
	}
	if err := s.recalcExamPaperTotal(ctx, paper.ID); err != nil {
		return nil, err
	}
	view := newExamQuestionAdminView(question)
	return &view, nil
}

// UpdateQuestion 整题替换（试卷与题目必须属于本竞赛）。
func (s *ExamService) UpdateQuestion(ctx context.Context, competitionID, questionID uint, in ExamQuestionInput) (*ExamQuestionAdminView, error) {
	_, paper, err := s.requireEditablePaper(ctx, competitionID)
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
	updated, err := s.buildExamQuestion(ctx, paper.ID, in)
	if err != nil {
		return nil, err
	}
	if existing, err := s.exam.FindQuestionBySeq(ctx, paper.ID, updated.Seq); err == nil && existing.ID != question.ID {
		return nil, fmt.Errorf("%w: question seq %d already exists", ErrValidation, updated.Seq)
	} else if err != nil && !isNotFound(err) {
		return nil, err
	}
	if err := s.exam.UpdateQuestionSelective(ctx, question.ID, map[string]any{
		"seq": updated.Seq, "type": updated.Type, "title": updated.Title,
		"content": updated.Content, "options_json": updated.OptionsJSON,
		"answer_json": updated.AnswerJSON, "explanation": updated.Explanation,
		"score": updated.Score, "challenge_id": updated.ChallengeID,
	}); err != nil {
		return nil, err
	}
	if err := s.recalcExamPaperTotal(ctx, paper.ID); err != nil {
		return nil, err
	}
	updated.ID = question.ID
	updated.PaperID = question.PaperID
	updated.CreatedAt = question.CreatedAt
	updated.UpdatedAt = s.now()
	view := newExamQuestionAdminView(updated)
	return &view, nil
}

// DeleteQuestion 删除试题并重算总分。
func (s *ExamService) DeleteQuestion(ctx context.Context, competitionID, questionID uint) error {
	_, paper, err := s.requireEditablePaper(ctx, competitionID)
	if err != nil {
		return err
	}
	question, err := s.exam.FindQuestion(ctx, questionID)
	if err != nil {
		return mapRecordNotFound(err)
	}
	if question.PaperID != paper.ID {
		return ErrNotFound
	}
	if err := s.exam.DeleteQuestion(ctx, question.ID); err != nil {
		return err
	}
	return s.recalcExamPaperTotal(ctx, paper.ID)
}

func (s *ExamService) recalcExamPaperTotal(ctx context.Context, paperID uint) error {
	questions, err := s.exam.ListQuestions(ctx, paperID)
	if err != nil {
		return err
	}
	total := 0
	for _, question := range questions {
		total += question.Score
	}
	return s.exam.UpdatePaperSelective(ctx, paperID, map[string]any{"total_score": total})
}

func (s *ExamService) buildExamQuestion(ctx context.Context, paperID uint, in ExamQuestionInput) (*model.ExamQuestion, error) {
	typ := model.ExamQuestionType(strings.TrimSpace(in.Type))
	if !typ.Valid() {
		return nil, fmt.Errorf("%w: invalid question type", ErrValidation)
	}
	if in.Seq < 1 || in.Seq > 999 {
		return nil, fmt.Errorf("%w: seq must be in 1..999", ErrValidation)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 200 {
		return nil, fmt.Errorf("%w: title is required and must be <=200 characters", ErrValidation)
	}
	if len(in.Content) > maxExamQuestionContent {
		return nil, fmt.Errorf("%w: content exceeds 64KiB", ErrValidation)
	}
	if in.Score < 1 || in.Score > 1000 {
		return nil, fmt.Errorf("%w: score must be in 1..1000", ErrValidation)
	}
	if len(in.Explanation) > maxExamExplanation {
		return nil, fmt.Errorf("%w: explanation exceeds 16KiB", ErrValidation)
	}
	options, err := normalizeExamOptions(typ, in.Options)
	if err != nil {
		return nil, err
	}
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	answerJSON, err := s.normalizeExamAnswer(ctx, typ, options, in.Answer, in.ChallengeID)
	if err != nil {
		return nil, err
	}
	return &model.ExamQuestion{
		PaperID: paperID, Seq: in.Seq, Type: typ, Title: title,
		Content: strings.TrimSpace(in.Content), OptionsJSON: string(optionsJSON),
		AnswerJSON: answerJSON, Explanation: strings.TrimSpace(in.Explanation),
		Score: in.Score, ChallengeID: in.ChallengeID,
	}, nil
}

// isExamUniqueConflict 唯一索引冲突跨方言判别（与 account_code 的写法一致）。
func isExamUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}

func normalizeExamOptions(typ model.ExamQuestionType, options []ExamOption) ([]ExamOption, error) {
	if typ == model.ExamQuestionTrueFalse || typ == model.ExamQuestionCode {
		if len(options) > 0 {
			return nil, fmt.Errorf("%w: options are only supported for single/multiple questions", ErrValidation)
		}
		return []ExamOption{}, nil
	}
	if len(options) < 2 || len(options) > maxExamOptions {
		return nil, fmt.Errorf("%w: single/multiple questions require 2..26 options", ErrValidation)
	}
	seen := make(map[string]bool, len(options))
	normalized := make([]ExamOption, 0, len(options))
	for _, option := range options {
		key := strings.TrimSpace(option.Key)
		text := strings.TrimSpace(option.Text)
		if !examOptionKeyRe.MatchString(key) {
			return nil, fmt.Errorf("%w: invalid option key %q (uppercase letters/digits, 1-8)", ErrValidation, option.Key)
		}
		if seen[key] {
			return nil, fmt.Errorf("%w: duplicate option key %q", ErrValidation, key)
		}
		if text == "" || len(text) > maxExamOptionText {
			return nil, fmt.Errorf("%w: option %q text is required and must be <=500 characters", ErrValidation, key)
		}
		seen[key] = true
		normalized = append(normalized, ExamOption{Key: key, Text: text})
	}
	return normalized, nil
}

func (s *ExamService) normalizeExamAnswer(ctx context.Context, typ model.ExamQuestionType, options []ExamOption, raw json.RawMessage, challengeID uint) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	switch typ {
	case model.ExamQuestionSingle:
		if len(trimmed) == 0 {
			return "", fmt.Errorf("%w: single question requires an answer key", ErrValidation)
		}
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
		if len(trimmed) == 0 {
			return "", fmt.Errorf("%w: multiple question requires answer keys", ErrValidation)
		}
		var keys []string
		if err := json.Unmarshal(trimmed, &keys); err != nil {
			return "", fmt.Errorf("%w: multiple answer must be an array of option keys", ErrValidation)
		}
		if len(keys) == 0 {
			return "", fmt.Errorf("%w: multiple answer cannot be empty", ErrValidation)
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
		if len(trimmed) == 0 {
			return "", fmt.Errorf("%w: truefalse question requires a boolean answer", ErrValidation)
		}
		var value bool
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return "", fmt.Errorf("%w: truefalse answer must be true or false", ErrValidation)
		}
		encoded, _ := json.Marshal(value)
		return string(encoded), nil
	case model.ExamQuestionCode:
		if challengeID == 0 {
			return "", fmt.Errorf("%w: code question requires challenge_id", ErrValidation)
		}
		if s.challenges == nil {
			return "", fmt.Errorf("%w: challenge repository is not configured", ErrValidation)
		}
		challenge, err := s.challenges.FindByID(ctx, challengeID)
		if err != nil || challenge == nil {
			return "", fmt.Errorf("%w: challenge %d not found", ErrValidation, challengeID)
		}
		if challenge.Type != model.ChallengeCode {
			return "", fmt.Errorf("%w: challenge %d is not a code challenge", ErrValidation, challengeID)
		}
		return "null", nil
	}
	return "", fmt.Errorf("%w: invalid question type", ErrValidation)
}

func examOptionExists(options []ExamOption, key string) bool {
	for _, option := range options {
		if option.Key == key {
			return true
		}
	}
	return false
}
