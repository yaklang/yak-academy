package repo

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// sqlite 实现集合。实现细节（gorm）不得泄漏到接口之外（project-conventions skill）。

type userRepo struct{ db *gorm.DB }

func NewUserRepo(db *gorm.DB) UserRepo { return &userRepo{db} }

func (r *userRepo) Create(ctx context.Context, u *model.User) error {
	return r.db.WithContext(ctx).Create(u).Error
}

func (r *userRepo) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) FindByID(ctx context.Context, id uint) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) UpdateGlobalRole(ctx context.Context, id uint, role model.GlobalRole) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("global_role", role).Error
}

func (r *userRepo) UpsertCourseRole(ctx context.Context, cr *model.CourseRole) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "topic_slug"}},
		DoUpdates: clause.AssignmentColumns([]string{"role"}),
	}).Create(cr).Error
}

func (r *userRepo) ListCourseRoles(ctx context.Context, userID uint) ([]model.CourseRole, error) {
	var list []model.CourseRole
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&list).Error
	return list, err
}

type contentPackageRepo struct{ db *gorm.DB }

func NewContentPackageRepo(db *gorm.DB) ContentPackageRepo { return &contentPackageRepo{db} }

func (r *contentPackageRepo) Create(ctx context.Context, p *model.ContentPackage) error {
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *contentPackageRepo) FindByID(ctx context.Context, id uint) (*model.ContentPackage, error) {
	var p model.ContentPackage
	if err := r.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *contentPackageRepo) FindByName(ctx context.Context, name string) (*model.ContentPackage, error) {
	var p model.ContentPackage
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *contentPackageRepo) UpdateStatus(ctx context.Context, id uint, s model.PackageStatus) error {
	return r.db.WithContext(ctx).Model(&model.ContentPackage{}).Where("id = ?", id).
		Update("status", s).Error
}

func (r *contentPackageRepo) AddVersion(ctx context.Context, v *model.ContentVersion) error {
	return r.db.WithContext(ctx).Create(v).Error
}