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

func (r *contentPackageRepo) FindLatestVersion(ctx context.Context, packageID uint) (*model.ContentVersion, error) {
	var v model.ContentVersion
	if err := r.db.WithContext(ctx).Where("package_id = ?", packageID).
		Order("id DESC").First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *contentPackageRepo) UpdateLatestVersion(ctx context.Context, id uint, version string) error {
	return r.db.WithContext(ctx).Model(&model.ContentPackage{}).Where("id = ?", id).
		Update("latest_version", version).Error
}

func (r *contentPackageRepo) List(ctx context.Context, offset, limit int) ([]model.ContentPackage, int64, error) {
	var (
		list  []model.ContentPackage
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.ContentPackage{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}