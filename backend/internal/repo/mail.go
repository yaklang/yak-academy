package repo

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
)

// 站内邮箱 sqlite 实现。邮件只由系统投递（Deliver），用户侧读 + 标记已读。

type mailRepo struct{ db *gorm.DB }

func NewMailRepo(db *gorm.DB) MailRepo { return &mailRepo{db} }

func (r *mailRepo) Deliver(ctx context.Context, m *model.Mail) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *mailRepo) List(ctx context.Context, userID uint, offset, limit int) ([]model.Mail, int64, error) {
	var (
		list  []model.Mail
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.Mail{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).
		Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *mailRepo) UnreadCount(ctx context.Context, userID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Mail{}).
		Where("user_id = ? AND read = ?", userID, false).Count(&n).Error
	return n, err
}

func (r *mailRepo) MarkRead(ctx context.Context, userID, mailID uint) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.Mail{}).
		Where("id = ? AND user_id = ?", mailID, userID).
		Update("read", true)
	return res.RowsAffected, res.Error
}
