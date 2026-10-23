package repo

import (
	"context"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
)

// sqlite 实现集合。实现细节（gorm）不得泄漏到接口之外（project-conventions skill）。

type userRepo struct{ db *gorm.DB }

func NewUserRepo(db *gorm.DB) UserRepo { return &userRepo{db} }

// Create 建号。账号编码为空时统一在此分配（学号式 YA{YY}-{NNNNN}，同年
// MAX+1，唯一冲突重试）——注册、管理端建号、竞赛批量生成全走本入口，
// 保证编码不重不漏；已带编码（迁移回填/测试固定码）则原样落库。
func (r *userRepo) Create(ctx context.Context, u *model.User) error {
	if u.AccountCode == "" {
		return r.createWithAccountCode(ctx, u)
	}
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

// List 管理端用户分页（filter 零值 = 全量）。active/solved_any 与过滤条件
// 共用同一 SQL 口径（COALESCE(last_login_at, created_at) 与 EXISTS 提交
// correct 行），时间比较绑定参数（与 LabInstance 过期扫描同款方言中立做法）。
func (r *userRepo) List(ctx context.Context, filter UserListFilter, offset, limit int) ([]UserItem, int64, error) {
	now := filter.Now
	if now.IsZero() {
		now = time.Now()
	}
	scope := userFilterScope(filter, now)
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.User{}).Scopes(scope).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []UserItem
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Select("users.*, (COALESCE(users.last_login_at, users.created_at) >= ?) AS active, "+
			"EXISTS (SELECT 1 FROM submissions WHERE submissions.user_id = users.id AND submissions.status = ?) AS solved_any",
			now.AddDate(0, -6, 0), model.SubmissionCorrect).
		Scopes(scope).
		Order("users.id").Offset(offset).Limit(limit).
		Find(&items).Error
	return items, total, err
}

// userFilterScope 用户列表过滤条件（零值字段不参与 WHERE）。不活跃 = 最近
// 登录（无登录记录则注册时间）早于 6 个月 cutoff；从未完成题目 = 不存在
// status=correct 的提交行。
func userFilterScope(f UserListFilter, now time.Time) func(*gorm.DB) *gorm.DB {
	cutoff := now.AddDate(0, -6, 0)
	solved := "EXISTS (SELECT 1 FROM submissions WHERE submissions.user_id = users.id AND submissions.status = ?)"
	return func(db *gorm.DB) *gorm.DB {
		if f.Q != "" {
			like := "%" + strings.ToLower(f.Q) + "%"
			db = db.Where(
				"LOWER(username) LIKE ? OR LOWER(account_code) LIKE ? OR phone LIKE ?",
				like, like, like,
			)
		}
		if f.Role != "" {
			db = db.Where("global_role = ?", f.Role)
		}
		if f.Active != nil {
			if *f.Active {
				db = db.Where("COALESCE(last_login_at, created_at) >= ?", cutoff)
			} else {
				db = db.Where("COALESCE(last_login_at, created_at) < ?", cutoff)
			}
		}
		if f.NoSolves != nil {
			if *f.NoSolves {
				db = db.Where("NOT "+solved, model.SubmissionCorrect)
			} else {
				db = db.Where(solved, model.SubmissionCorrect)
			}
		}
		return db
	}
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

// Delete 删除内容包与全部版本行（事务）。内容表/派生表的级联清理由
// ContentService.DeletePackage 按历代 Payload 编排（本层只管包自身的行）。
func (r *contentPackageRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("package_id = ?", id).Delete(&model.ContentVersion{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.ContentPackage{}, id).Error
	})
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
