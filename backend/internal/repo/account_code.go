package repo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

const accountCodeRetries = 5

// isAccountCodeConflict 错误是否为 account_code 唯一索引冲突（并发建号取到
// 同一 MAX 的重试信号）。gorm 未开启 Translate，错误以驱动原文返回，按子串
// 跨方言判别（SQLite "UNIQUE constraint failed: users.account_code" /
// PostgreSQL 'duplicate key value violates unique constraint "..."account_code'）。
func isAccountCodeConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	unique := strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
	return unique && strings.Contains(msg, "account_code")
}

// nextAccountCode 取下一编码：同年前缀下字典序最大编码的序号 + 1
// （零填充保证字典序 = 数值序）；该年无编码则从 1 起。t 零值取当前时间。
// 格式约定见 model/account_code.go。
func (r *userRepo) nextAccountCode(ctx context.Context, t time.Time) (string, error) {
	year := t.Year()
	if t.IsZero() {
		year = time.Now().Year()
	}
	var codes []string
	if err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("account_code LIKE ?", model.AccountCodePrefix(year)+"%").
		Order("account_code DESC").Limit(1).
		Pluck("account_code", &codes).Error; err != nil {
		return "", err
	}
	seq := 1
	if len(codes) > 0 {
		prev, ok := model.AccountCodeSeq(codes[0])
		if !ok {
			return "", fmt.Errorf("account code %q malformed", codes[0])
		}
		seq = prev + 1
	}
	code := model.FormatAccountCode(year, seq)
	if code == "" {
		return "", fmt.Errorf("account code seq %d overflow for year %d", seq, year)
	}
	return code, nil
}

// createWithAccountCode 为新用户分配账号编码并落库。唯一冲突（并发建号取到
// 同一 MAX）时重取新码重试；非编码类错误（如用户名撞唯一索引）原样上抛。
func (r *userRepo) createWithAccountCode(ctx context.Context, u *model.User) error {
	var lastErr error
	for range accountCodeRetries {
		code, err := r.nextAccountCode(ctx, u.CreatedAt)
		if err != nil {
			return err
		}
		u.AccountCode = code
		if err := r.db.WithContext(ctx).Create(u).Error; err == nil {
			return nil
		} else {
			lastErr = err
			if !isAccountCodeConflict(err) {
				return err
			}
		}
	}
	return fmt.Errorf("assign account code: retries exhausted: %w", lastErr)
}

// FindByAccountCode 账号编码查人（邀请赛表格导入解析编码引用）。
func (r *userRepo) FindByAccountCode(ctx context.Context, code string) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).Where("account_code = ?", code).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// TouchLogin 记录最近登录时刻（不活跃用户判定基线）。调用方 best-effort：
// 失败不应阻塞登录主流程。
func (r *userRepo) TouchLogin(ctx context.Context, id uint, t time.Time) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("last_login_at", t).Error
}
