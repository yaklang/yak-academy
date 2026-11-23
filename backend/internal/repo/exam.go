package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// examRepo 考试域读写：试卷、试题与后续的作答/成绩。只被 ExamService 使用，
// CTF/AWD 服务不得引用（独立原则）。
type examRepo struct{ db *gorm.DB }

func NewExamRepo(db *gorm.DB) ExamRepo {
	return &examRepo{db: db}
}

func (r *examRepo) FindPaperByCompetition(ctx context.Context, competitionID uint) (*model.ExamPaper, error) {
	var paper model.ExamPaper
	if err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).First(&paper).Error; err != nil {
		return nil, err
	}
	return &paper, nil
}

func (r *examRepo) FindPaper(ctx context.Context, id uint) (*model.ExamPaper, error) {
	var paper model.ExamPaper
	if err := r.db.WithContext(ctx).First(&paper, id).Error; err != nil {
		return nil, err
	}
	return &paper, nil
}

func (r *examRepo) CreatePaper(ctx context.Context, paper *model.ExamPaper) error {
	return r.db.WithContext(ctx).Create(paper).Error
}

func (r *examRepo) UpdatePaperSelective(ctx context.Context, paperID uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.ExamPaper{}).Where("id = ?", paperID).Updates(fields).Error
}

func (r *examRepo) ListQuestions(ctx context.Context, paperID uint) ([]model.ExamQuestion, error) {
	var list []model.ExamQuestion
	err := r.db.WithContext(ctx).Where("paper_id = ?", paperID).Order("seq asc").Find(&list).Error
	return list, err
}

func (r *examRepo) FindQuestion(ctx context.Context, id uint) (*model.ExamQuestion, error) {
	var question model.ExamQuestion
	if err := r.db.WithContext(ctx).First(&question, id).Error; err != nil {
		return nil, err
	}
	return &question, nil
}

func (r *examRepo) FindQuestionBySeq(ctx context.Context, paperID uint, seq int) (*model.ExamQuestion, error) {
	var question model.ExamQuestion
	if err := r.db.WithContext(ctx).Where("paper_id = ? AND seq = ?", paperID, seq).First(&question).Error; err != nil {
		return nil, err
	}
	return &question, nil
}

func (r *examRepo) CreateQuestion(ctx context.Context, question *model.ExamQuestion) error {
	return r.db.WithContext(ctx).Create(question).Error
}

func (r *examRepo) UpdateQuestionSelective(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.ExamQuestion{}).Where("id = ?", id).Updates(fields).Error
}

func (r *examRepo) DeleteQuestion(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.ExamQuestion{}, id).Error
}

func (r *examRepo) FindAttemptByKey(ctx context.Context, paperID, userID uint) (*model.ExamAttempt, error) {
	var attempt model.ExamAttempt
	if err := r.db.WithContext(ctx).Where("paper_id = ? AND user_id = ?", paperID, userID).
		First(&attempt).Error; err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (r *examRepo) FindAttempt(ctx context.Context, id uint) (*model.ExamAttempt, error) {
	var attempt model.ExamAttempt
	if err := r.db.WithContext(ctx).First(&attempt, id).Error; err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (r *examRepo) CreateAttempt(ctx context.Context, attempt *model.ExamAttempt) error {
	return r.db.WithContext(ctx).Create(attempt).Error
}

func (r *examRepo) UpdateAttemptSelective(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.ExamAttempt{}).Where("id = ?", id).Updates(fields).Error
}

// ExpireAttempt 条件更新：只有仍处于 in_progress 且已到期的行会被置为 expired，
// 重复调用不会覆盖已交卷/已评分的状态。
func (r *examRepo) ExpireAttempt(ctx context.Context, id uint, at time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.ExamAttempt{}).
		Where("id = ? AND status = ?", id, model.ExamAttemptInProgress).
		Updates(map[string]any{
			"status": model.ExamAttemptExpired, "submitted_at": at, "auto_submitted": true,
		})
	return res.RowsAffected, res.Error
}

func (r *examRepo) ListExpiredAttempts(ctx context.Context, now time.Time, limit int) ([]model.ExamAttempt, error) {
	if limit <= 0 {
		limit = 100
	}
	var list []model.ExamAttempt
	err := r.db.WithContext(ctx).
		Where("status = ? AND expires_at <= ?", model.ExamAttemptInProgress, now).
		Order("id asc").Limit(limit).Find(&list).Error
	return list, err
}

func (r *examRepo) FindAnswer(ctx context.Context, attemptID, questionID uint) (*model.ExamAnswer, error) {
	var answer model.ExamAnswer
	if err := r.db.WithContext(ctx).
		Where("attempt_id = ? AND question_id = ?", attemptID, questionID).
		First(&answer).Error; err != nil {
		return nil, err
	}
	return &answer, nil
}

func (r *examRepo) ListAnswers(ctx context.Context, attemptID uint) ([]model.ExamAnswer, error) {
	var list []model.ExamAnswer
	err := r.db.WithContext(ctx).Where("attempt_id = ?", attemptID).Order("id asc").Find(&list).Error
	return list, err
}

// UpsertAnswer 自动保存：同一 (attempt, question) 覆盖答案并重置评分状态。
func (r *examRepo) UpsertAnswer(ctx context.Context, answer *model.ExamAnswer) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "attempt_id"}, {Name: "question_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"answer_json", "graded", "correct", "score", "code_submission_id", "updated_at",
		}),
	}).Create(answer).Error
}
