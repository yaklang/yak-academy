package repo

import (
	"context"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
)

type labInstanceRepo struct{ db *gorm.DB }

func NewLabInstanceRepo(db *gorm.DB) LabInstanceRepo { return &labInstanceRepo{db: db} }

func (r *labInstanceRepo) Create(ctx context.Context, i *model.LabInstance) error {
	return r.db.WithContext(ctx).Create(i).Error
}

func (r *labInstanceRepo) FindByID(ctx context.Context, id uint) (*model.LabInstance, error) {
	var i model.LabInstance
	if err := r.db.WithContext(ctx).First(&i, id).Error; err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *labInstanceRepo) FindActive(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error) {
	var i model.LabInstance
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND challenge_id = ? AND status IN ?", userID, challengeID,
			[]model.InstanceStatus{model.InstanceCreating, model.InstanceRunning}).
		Order("id DESC").First(&i).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *labInstanceRepo) UpdateRunning(ctx context.Context, id uint, endpoint string, expireAt time.Time, flagValue string) error {
	return r.db.WithContext(ctx).Model(&model.LabInstance{}).Where("id = ?", id).
		Updates(map[string]any{
			"status": model.InstanceRunning, "endpoint": endpoint, "expire_at": expireAt,
			"flag_value": flagValue,
		}).Error
}

func (r *labInstanceRepo) UpdateExpireAt(ctx context.Context, id uint, expireAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.LabInstance{}).Where("id = ?", id).
		Update("expire_at", expireAt).Error
}

func (r *labInstanceRepo) UpdateStatus(ctx context.Context, id uint, status model.InstanceStatus) error {
	return r.db.WithContext(ctx).Model(&model.LabInstance{}).
		Where("id = ?", id).Update("status", status).Error
}

func (r *labInstanceRepo) ListExpired(ctx context.Context, now time.Time) ([]model.LabInstance, error) {
	var list []model.LabInstance
	err := r.db.WithContext(ctx).
		Where("status = ? AND expire_at < ?", model.InstanceRunning, now).
		Find(&list).Error
	return list, err
}

func (r *labInstanceRepo) FindActiveAny(ctx context.Context, userID uint) (*model.LabInstance, error) {
	var i model.LabInstance
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND status IN ?", userID,
			[]model.InstanceStatus{model.InstanceCreating, model.InstanceRunning}).
		Order("id DESC").First(&i).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

func (r *labInstanceRepo) List(ctx context.Context, offset, limit int) ([]model.LabInstance, int64, error) {
	var (
		list  []model.LabInstance
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.LabInstance{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *labInstanceRepo) ListActive(ctx context.Context) ([]model.LabInstance, error) {
	var list []model.LabInstance
	err := r.db.WithContext(ctx).
		Where("status IN ?", []model.InstanceStatus{model.InstanceCreating, model.InstanceRunning}).
		Order("id DESC").Find(&list).Error
	return list, err
}

func (r *labInstanceRepo) ListActiveByChallenge(ctx context.Context, challengeID uint) ([]model.LabInstance, error) {
	var list []model.LabInstance
	err := r.db.WithContext(ctx).
		Where("challenge_id = ? AND status IN ?", challengeID,
			[]model.InstanceStatus{model.InstanceCreating, model.InstanceRunning}).
		Order("id").Find(&list).Error
	return list, err
}

func (r *labInstanceRepo) FindByCheckpointToken(ctx context.Context, token string) (*model.LabInstance, error) {
	var i model.LabInstance
	err := r.db.WithContext(ctx).
		Where("checkpoint_token = ? AND status IN ?", token,
			[]model.InstanceStatus{model.InstanceCreating, model.InstanceRunning}).
		First(&i).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &i, nil
}

type instanceLogRepo struct{ db *gorm.DB }

func NewInstanceLogRepo(db *gorm.DB) InstanceLogRepo { return &instanceLogRepo{db: db} }

func (r *instanceLogRepo) Create(ctx context.Context, l *model.InstanceLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

func (r *instanceLogRepo) List(ctx context.Context, offset, limit int) ([]model.InstanceLog, int64, error) {
	var (
		list  []model.InstanceLog
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.InstanceLog{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}
