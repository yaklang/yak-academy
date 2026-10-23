package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 注册（手机验证码）参数与策略见 docs/architecture.md §3.1：
// 6 位验证码、5 分钟有效、一次性核销；同手机号 60s 限频、10 分钟窗口最多 3 次；
// 角色固定 student（禁止自选）。登录仍是用户名 + 密码，手机号不参与登录。

const (
	smsCodeTTL      = 5 * time.Minute
	smsSendInterval = 60 * time.Second
	smsWindow       = 10 * time.Minute
	smsWindowLimit  = 3
)

// ErrSmsThrottled 发码限频（60s 内重复 / 窗口超量），handler 转 200 + retry_after。
var ErrSmsThrottled = errors.New("sms send throttled")

// SmsSender 短信投递抽象：第一版只有开发投递器（写服务日志），
// 生产以环境变量切换真实通道（见 config SmsProvider；不接第三方）。
type SmsSender interface {
	Send(phone, code string) error
}

// DevSmsSender 开发投递器：验证码写服务日志，本地联调与 E2E 从日志取码。
type DevSmsSender struct{}

func (DevSmsSender) Send(phone, code string) error {
	log.Printf("[sms] dev deliver: phone=%s code=%s", phone, code)
	return nil
}

// RegisterService 自助注册（手机验证码）。
type RegisterService struct {
	users repo.UserRepo
	codes repo.SmsCodeRepo
	sms   SmsSender
	now   func() time.Time
}

func NewRegisterService(users repo.UserRepo, codes repo.SmsCodeRepo, sms SmsSender) *RegisterService {
	if sms == nil {
		sms = DevSmsSender{}
	}
	return &RegisterService{users: users, codes: codes, sms: sms, now: time.Now}
}

// SendCode 向手机号发送注册验证码。返回 (code, retryAfter, err)：
// code 非空表示本次实际生成并投递了验证码（供 handler 在回显模式下带回响应，
// 见 config SmsShowCode）；retryAfter > 0 表示限频剩余秒数（ErrSmsThrottled）。
func (s *RegisterService) SendCode(ctx context.Context, phone string) (string, int, error) {
	phone = strings.TrimSpace(phone)
	if !validPhone(phone) {
		return "", 0, fmt.Errorf("%w: invalid phone number", ErrValidation)
	}
	now := s.now()
	// 手机号已注册先于限频拒绝：让学员等满 60s 倒计时只会再得到同一个拒绝，
	// 先告知事实并引导去登录（前端据此提示）。
	if _, err := s.users.FindByPhone(ctx, phone); err == nil {
		return "", 0, fmt.Errorf("%w: phone already registered", ErrValidation)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, err
	}
	// 60 秒内已发过 + 10 分钟窗口内最多 3 条（与找回密码发码共用同一套限频策略，
	// 按 purpose 分别计数，见 smsThrottle）。
	if retryAfter, err := smsThrottle(ctx, s.codes, phone, model.SmsPurposeRegister, now); err != nil {
		return "", retryAfter, err
	}
	code, err := genCode()
	if err != nil {
		return "", 0, err
	}
	if err := s.codes.Create(ctx, &model.SmsCode{
		Phone: phone, Code: code, Purpose: model.SmsPurposeRegister, ExpiresAt: now.Add(smsCodeTTL),
	}); err != nil {
		return "", 0, err
	}
	if err := s.sms.Send(phone, code); err != nil {
		return "", 0, err
	}
	return code, 0, nil
}

// smsThrottle 发码限频（注册与找回密码共用）：同手机号同用途 60 秒内仅一次、
// 10 分钟窗口内最多 3 次。命中限频返回 (retryAfter, ErrSmsThrottled)。
func smsThrottle(ctx context.Context, codes repo.SmsCodeRepo, phone, purpose string, now time.Time) (int, error) {
	// 60 秒内已发过：直接返回剩余等待时间（不重复发送）。
	if latest, err := codes.LatestByPhone(ctx, phone, purpose); err == nil {
		if wait := smsSendInterval - now.Sub(latest.CreatedAt); wait > 0 {
			return int(wait.Seconds()) + 1, ErrSmsThrottled
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	// 10 分钟窗口内最多 3 条。
	n, err := codes.CountSince(ctx, phone, purpose, now.Add(-smsWindow))
	if err != nil {
		return 0, err
	}
	if n >= smsWindowLimit {
		return int(smsWindow.Seconds()), ErrSmsThrottled
	}
	return 0, nil
}

// Register 凭验证码建号（角色固定 student）并直接签发登录 token。
func (s *RegisterService) Register(ctx context.Context, phone, code, username, password string, issue func(*model.User) (string, time.Time, error)) (string, time.Time, *model.User, error) {
	phone = strings.TrimSpace(phone)
	if !validPhone(phone) {
		return "", time.Time{}, nil, fmt.Errorf("%w: invalid phone number", ErrValidation)
	}
	if len(code) != 6 {
		return "", time.Time{}, nil, fmt.Errorf("%w: code must be 6 digits", ErrValidation)
	}
	if len(username) < 3 || len(username) > 64 {
		return "", time.Time{}, nil, fmt.Errorf("%w: username must be 3-64 characters", ErrValidation)
	}
	if len(password) < 6 {
		return "", time.Time{}, nil, fmt.Errorf("%w: password must be at least 6 characters", ErrValidation)
	}
	now := s.now()
	latest, err := s.codes.LatestByPhone(ctx, phone, model.SmsPurposeRegister)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", time.Time{}, nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
		}
		return "", time.Time{}, nil, err
	}
	if latest.Used || !latest.ExpiresAt.After(now) {
		return "", time.Time{}, nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	}
	// 验证码比对恒时（逐字符 XOR，judge flag 同款），防时序侧信道。
	if !constantTimeEq(latest.Code, code) {
		return "", time.Time{}, nil, fmt.Errorf("%w: incorrect code", ErrValidation)
	}
	// 先核销再建号：并发同码注册只有一个成功（条件更新保证一次性）。
	if n, err := s.codes.MarkUsed(ctx, latest.ID); err != nil {
		return "", time.Time{}, nil, err
	} else if n == 0 {
		return "", time.Time{}, nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	}
	// 用户名/手机号查重（验证码已消耗，重复属调用方输入问题）。
	if _, err := s.users.FindByUsername(ctx, username); err == nil {
		return "", time.Time{}, nil, fmt.Errorf("%w: username already exists", ErrValidation)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", time.Time{}, nil, err
	}
	if _, err := s.users.FindByPhone(ctx, phone); err == nil {
		return "", time.Time{}, nil, fmt.Errorf("%w: phone already registered", ErrValidation)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", time.Time{}, nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	user := &model.User{
		Username: username, PasswordHash: string(hash),
		GlobalRole: model.GlobalRoleStudent, Phone: phone,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return "", time.Time{}, nil, err
	}
	token, expires, err := issue(user)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	return token, expires, user, nil
}

func validPhone(p string) bool {
	if len(p) != 11 || p[0] != '1' {
		return false
	}
	if p[1] < '3' || p[1] > '9' {
		return false
	}
	for i := 2; i < len(p); i++ {
		if p[i] < '0' || p[i] > '9' {
			return false
		}
	}
	return true
}

// genCode 密码学随机 6 位数字（crypto/rand，拒绝取模偏置：rejection sampling）。
func genCode() (string, error) {
	digits := make([]byte, 6)
	for i := range digits {
		for {
			n, err := rand.Int(rand.Reader, big.NewInt(10))
			if err != nil {
				return "", err
			}
			if n.Int64() < 10 {
				digits[i] = byte('0' + n.Int64())
				break
			}
		}
	}
	return string(digits), nil
}

// constantTimeEq 逐字符 XOR 比较（长度不同直接 false，不泄漏内容）。
func constantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
