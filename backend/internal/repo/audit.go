package repo

import (
	"context"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// auditLogRepo 管理操作审计 append-only 落库与查询（无更新/删除方法，
// 审计行一旦写入不可改——修正是新事件不是 UPDATE）。
type auditLogRepo struct{ gdb *gorm.DB }

func NewAuditLogRepo(gdb *gorm.DB) AuditLogRepo { return auditLogRepo{gdb} }

func (r auditLogRepo) Create(ctx context.Context, l *model.AdminAuditLog) error {
	return r.gdb.WithContext(ctx).Create(l).Error
}

func (r auditLogRepo) List(ctx context.Context, f AuditFilter, offset, limit int) ([]model.AdminAuditLog, int64, error) {
	q := r.gdb.WithContext(ctx).Model(&model.AdminAuditLog{})
	if f.AdminID != 0 {
		q = q.Where("admin_id = ?", f.AdminID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.AdminAuditLog
	if err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
