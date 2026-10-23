package service

// 找回密码（手机验证码重置）：注册手机号是唯一自助找回凭据——
// 发码（purpose=password_reset，限频策略与注册发码一致）→ 凭码核销后更新密码，
// 该账号全部已签发 token 随即失效（同本人改密/管理员重置语义）。
// 成功返回用户名与账号编码：忘记用户名的学员凭手机号一并提供找回。
// 竞赛批量建号未绑定手机号，此类账号走管理端重置（POST /admin/users/{id}/reset-password）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// PasswordResetService 找回密码（手机验证码重置）。
type PasswordResetService struct {
	users repo.UserRepo
	codes repo.SmsCodeRepo
	sms   SmsSender
	now   func() time.Time
}

func NewPasswordResetService(users repo.UserRepo, codes repo.SmsCodeRepo, sms SmsSender) *PasswordResetService {
	if sms == nil {
		sms = DevSmsSender{}
	}
	return &PasswordResetService{users: users, codes: codes, sms: sms, now: time.Now}
}

// SendResetCode 向已注册手机号发送找回验证码。返回值语义同注册发码
// RegisterService.SendCode：(code, retryAfter, err)，code 供 handler 回显模式。
func (s *PasswordResetService) SendResetCode(ctx context.Context, phone string) (string, int, error) {
	phone = strings.TrimSpace(phone)
	if !validPhone(phone) {
		return "", 0, fmt.Errorf("%w: invalid phone number", ErrValidation)
	}
	// 未注册先于限频拒绝（与注册发码的「已注册先拒」对称）：等满倒计时只会
	// 再得到同一个拒绝，不如直接告知走注册或联系管理员。
	if _, err := s.users.FindByPhone(ctx, phone); errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, fmt.Errorf("%w: phone not registered", ErrValidation)
	} else if err != nil {
		return "", 0, err
	}
	now := s.now()
	if retryAfter, err := smsThrottle(ctx, s.codes, phone, model.SmsPurposePasswordReset, now); err != nil {
		return "", retryAfter, err
	}
	code, err := genCode()
	if err != nil {
		return "", 0, err
	}
	if err := s.codes.Create(ctx, &model.SmsCode{
		Phone: phone, Code: code, Purpose: model.SmsPurposePasswordReset, ExpiresAt: now.Add(smsCodeTTL),
	}); err != nil {
		return "", 0, err
	}
	if err := s.sms.Send(phone, code); err != nil {
		return "", 0, err
	}
	return code, 0, nil
}

// ResetPassword 核销找回验证码并更新密码，返回账号（含用户名与账号编码）。
func (s *PasswordResetService) ResetPassword(ctx context.Context, phone, code, newPassword string) (*model.User, error) {
	phone = strings.TrimSpace(phone)
	if !validPhone(phone) {
		return nil, fmt.Errorf("%w: invalid phone number", ErrValidation)
	}
	if len(newPassword) < 6 {
		return nil, fmt.Errorf("%w: password must be at least 6 characters", ErrValidation)
	}
	// 账号存在性先于核销判定：未注册手机号不应消耗任何验证码。
	user, err := s.users.FindByPhone(ctx, phone)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: phone not registered", ErrValidation)
	} else if err != nil {
		return nil, err
	}
	now := s.now()
	latest, err := s.codes.LatestByPhone(ctx, phone, model.SmsPurposePasswordReset)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	} else if err != nil {
		return nil, err
	}
	if latest.Used || !latest.ExpiresAt.After(now) {
		return nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	}
	// 验证码比对恒时（同注册核销），防时序侧信道。
	if !constantTimeEq(latest.Code, code) {
		return nil, fmt.Errorf("%w: incorrect code", ErrValidation)
	}
	// 先核销再改密：并发同码重置只有一个成功（条件更新保证一次性）。
	if n, err := s.codes.MarkUsed(ctx, latest.ID); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	// 阈值 = 签发秒 + 1：重置前签发的 token 全部失效（同 ChangePassword 语义）。
	if err := s.users.UpdatePassword(ctx, user.ID, string(hash), now.Truncate(time.Second).Add(time.Second)); err != nil {
		return nil, err
	}
	return user, nil
}
