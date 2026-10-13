package repo

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
)

type pathRepo struct{ db *gorm.DB }

func NewPathRepo(db *gorm.DB) PathRepo { return &pathRepo{db} }

func (r *pathRepo) List(ctx context.Context) ([]model.LearningPath, error) {
	var list []model.LearningPath
	err := r.db.WithContext(ctx).Order("id").Find(&list).Error
	return list, err
}

func (r *pathRepo) FindByID(ctx context.Context, id uint) (*model.LearningPath, error) {
	var p model.LearningPath
	if err := r.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *pathRepo) ListItems(ctx context.Context, pathID uint) ([]model.PathItem, error) {
	var list []model.PathItem
	err := r.db.WithContext(ctx).Where("path_id = ?", pathID).Order("`order`").Find(&list).Error
	return list, err
}

func (r *pathRepo) Create(ctx context.Context, path *model.LearningPath, items []model.PathItem) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(path).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].PathID = path.ID
			items[i].ID = 0
		}
		if len(items) == 0 {
			return nil
		}
		return tx.Create(&items).Error
	})
}

func (r *pathRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("path_id = ?", id).Delete(&model.PathItem{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&model.LearningPath{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
