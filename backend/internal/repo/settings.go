package repo

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// settingsRepo system_settings 落库（键唯一；Put 为 upsert，跨 SQLite/PG
// 使用 ON CONFLICT，避免先查后写的并发覆盖）。
type settingsRepo struct{ gdb *gorm.DB }

func NewSettingsRepo(gdb *gorm.DB) SettingsRepo { return settingsRepo{gdb} }

func (r settingsRepo) Get(ctx context.Context, key string) (*model.SystemSetting, error) {
	var s model.SystemSetting
	err := r.gdb.WithContext(ctx).Where("key = ?", key).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r settingsRepo) Put(ctx context.Context, s *model.SystemSetting) error {
	return r.gdb.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(s).Error
}
