package service

// 找回密码（邮箱验证码主通道，旧账号可用手机号）：发码（purpose=password_reset，
// 限频策略与注册发码一致）→ 凭码核销后更新密码，该账号全部已签发 token 随即失效
// （同本人改密/管理员重置语义）。成功返回用户名与账号编码：忘记用户名的学员凭
// 邮箱（旧账号可凭手机号）一并找回。竞赛批量建号未绑定邮箱/手机号，此类账号走
// 管理端重置（POST /admin/users/{id}/reset-password）。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// PasswordResetService 找回密码（邮箱/手机验证码重置）。
type PasswordResetService struct {
	users repo.UserRepo
	codes repo.SmsCodeRepo
	sms   SmsSender
	email EmailSender
	now   func() time.Time
}

func NewPasswordResetService(users repo.UserRepo, codes repo.SmsCodeRepo, sms SmsSender, email EmailSender) *PasswordResetService {
	if sms == nil {
		sms = DevSmsSender{}
	}
	if email == nil {
		email = DevEmailSender{}
	}
	return &PasswordResetService{users: users, codes: codes, sms: sms, email: email, now: time.Now}
}

// SendResetCode 向已注册目标发送找回验证码。email / phone 必须且只能填一个。
// 返回值语义同 RegisterService.SendEmailCode：(code, retryAfter, err)。
func (s *PasswordResetService) SendResetCode(ctx context.Context, email, phone string) (string, int, error) {
	target, err := parseVerificationTarget(email, phone)
	if err != nil {
		return "", 0, err
	}
	// 未注册先于限频拒绝（与注册发码的「已注册先拒」对称）：等满倒计时只会
	// 再得到同一个拒绝，不如直接告知走注册或联系管理员。
	if _, err := findUserByTarget(ctx, s.users, target); errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, targetMissingError(target)
	} else if err != nil {
		return "", 0, err
	}
	return sendVerificationCode(ctx, s.codes, s.now(), target, model.SmsPurposePasswordReset,
		func(code string) error {
			return deliverVerificationCode(ctx, s.sms, s.email, target, code, model.SmsPurposePasswordReset)
		})
}

// PasswordResetInput 重置密码参数：Email / Phone 必须且只能填一个。
type PasswordResetInput struct {
	Email       string
	Phone       string
	Code        string
	NewPassword string
}

// ResetPassword 核销找回验证码并更新密码，返回账号（含用户名与账号编码）。
func (s *PasswordResetService) ResetPassword(ctx context.Context, in PasswordResetInput) (*model.User, error) {
	target, err := parseVerificationTarget(in.Email, in.Phone)
	if err != nil {
		return nil, err
	}
	if len(in.Code) != 6 {
		return nil, fmt.Errorf("%w: code must be 6 digits", ErrValidation)
	}
	if len(in.NewPassword) < 6 {
		return nil, fmt.Errorf("%w: password must be at least 6 characters", ErrValidation)
	}
	// 账号存在性先于核销判定：未注册目标不应消耗任何验证码。
	user, err := findUserByTarget(ctx, s.users, target)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, targetMissingError(target)
	} else if err != nil {
		return nil, err
	}
	now := s.now()
	latest, err := latestVerificationCode(ctx, s.codes, target, model.SmsPurposePasswordReset)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	} else if err != nil {
		return nil, err
	}
	if latest.Used || !latest.ExpiresAt.After(now) {
		return nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	}
	// 验证码比对恒时（同注册核销），防时序侧信道。
	if !constantTimeEq(latest.Code, in.Code) {
		return nil, fmt.Errorf("%w: incorrect code", ErrValidation)
	}
	// 先核销再改密：并发同码重置只有一个成功（条件更新保证一次性）。
	if n, err := s.codes.MarkUsed(ctx, latest.ID); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	// 阈值 = 签发秒 + 1：重置前签发的 token 全部失效（同 ChangePassword 语义）。
	if err := s.users.UpdatePassword(ctx, user.ID, string(hash), now.Truncate(time.Second).Add(time.Second)); err != nil {
		return nil, err
	}
	return user, nil
}
