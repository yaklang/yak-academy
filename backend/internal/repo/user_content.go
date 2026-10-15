package repo

import (
	"context"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
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

func (r *userRepo) FindByIDs(ctx context.Context, ids ...uint) ([]model.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []model.User
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
}

func (r *userRepo) FindByPhone(ctx context.Context, phone string) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).Where("phone = ?", phone).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *userRepo) List(ctx context.Context, offset, limit int) ([]model.User, int64, error) {
	var (
		list  []model.User
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.User{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("id").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *userRepo) UpdateGlobalRole(ctx context.Context, id uint, role model.GlobalRole) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("global_role", role).Error
}

func (r *userRepo) UpdatePassword(ctx context.Context, id uint, passwordHash string, invalidBefore time.Time) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"password_hash":        passwordHash,
			"token_invalid_before": invalidBefore,
		}).Error
}

func (r *userRepo) UpdateBio(ctx context.Context, id uint, bio string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("bio", bio).Error
}

func (r *userRepo) UpdateAvatar(ctx context.Context, id uint, avatarFile string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("avatar_file", avatarFile).Error
}

type tokenRevocationRepo struct{ db *gorm.DB }

func NewTokenRevocationRepo(db *gorm.DB) TokenRevocationRepo { return &tokenRevocationRepo{db} }

func (r *tokenRevocationRepo) Revoke(ctx context.Context, rec *model.TokenRevocation) error {
	return r.db.WithContext(ctx).Create(rec).Error
}

func (r *tokenRevocationRepo) FindByJTI(ctx context.Context, jti string) (*model.TokenRevocation, error) {
	var rec model.TokenRevocation
	err := r.db.WithContext(ctx).Where("jti = ?", jti).First(&rec).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
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

func (r *contentPackageRepo) FindVersionByID(ctx context.Context, id uint) (*model.ContentVersion, error) {
	var v model.ContentVersion
	if err := r.db.WithContext(ctx).First(&v, id).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *contentPackageRepo) ListVersions(ctx context.Context, packageID uint) ([]model.ContentVersion, error) {
	var list []model.ContentVersion
	err := r.db.WithContext(ctx).Where("package_id = ?", packageID).
		Order("id DESC").Find(&list).Error
	return list, err
}

func (r *contentPackageRepo) UpdateVersionStatus(ctx context.Context, id uint, s model.PackageStatus) error {
	return r.db.WithContext(ctx).Model(&model.ContentVersion{}).Where("id = ?", id).
		Update("status", s).Error
}

// ArchiveOtherVersions 将包内除 exceptID 外的版本置 archived（版本级状态机维护）。
func (r *contentPackageRepo) ArchiveOtherVersions(ctx context.Context, packageID, exceptID uint) error {
	return r.db.WithContext(ctx).Model(&model.ContentVersion{}).
		Where("package_id = ? AND id <> ?", packageID, exceptID).
		Update("status", model.PackageArchived).Error
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

// smsCodeRepo 注册验证码表实现。核销走条件更新（used=0 → 1）保证一次性。
type smsCodeRepo struct{ db *gorm.DB }

func NewSmsCodeRepo(db *gorm.DB) SmsCodeRepo { return &smsCodeRepo{db} }

func (r *smsCodeRepo) Create(ctx context.Context, c *model.SmsCode) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *smsCodeRepo) LatestByPhone(ctx context.Context, phone string) (*model.SmsCode, error) {
	var c model.SmsCode
	if err := r.db.WithContext(ctx).Where("phone = ?", phone).
		Order("id DESC").First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *smsCodeRepo) CountSince(ctx context.Context, phone string, windowStart time.Time) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.SmsCode{}).
		Where("phone = ? AND created_at >= ?", phone, windowStart).
		Count(&n).Error
	return n, err
}

func (r *smsCodeRepo) MarkUsed(ctx context.Context, id uint) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.SmsCode{}).
		Where("id = ? AND used = 0", id).Update("used", true)
	return res.RowsAffected, res.Error
}
