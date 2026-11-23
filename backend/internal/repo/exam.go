package repo

import (
	"context"

	"gorm.io/gorm"

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
