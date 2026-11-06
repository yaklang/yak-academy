package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 注册参数与策略见 docs/architecture.md §3.1：6 位验证码、5 分钟有效、
// 一次性核销；同目标 60s 限频、10 分钟窗口最多 3 次；角色固定 student。
// 邮箱是主通道；手机号路径仅为旧客户端/旧账号兼容保留。登录仍是用户名 + 密码。

const (
	smsCodeTTL      = 5 * time.Minute
	smsSendInterval = 60 * time.Second
	smsWindow       = 10 * time.Minute
	smsWindowLimit  = 3
)

// ErrSmsThrottled 发码限频（60s 内重复 / 窗口超量），handler 转 200 + retry_after。
// 邮箱与手机号通道共用该哨兵（历史命名保留）。
var ErrSmsThrottled = errors.New("sms send throttled")

// SmsSender 旧版短信投递抽象：第一版只有开发投递器（写服务日志），
// 生产如需短信通道需另行装配（当前主通道为邮箱）。
type SmsSender interface {
	Send(phone, code string) error
}

// DevSmsSender 开发投递器：验证码写服务日志，本地联调与 E2E 从日志取码。
type DevSmsSender struct{}

func (DevSmsSender) Send(phone, code string) error {
	log.Printf("[sms] dev deliver: phone=%s code=%s", phone, code)
	return nil
}

// EmailSender 邮箱验证码投递抽象：SMTP/事务邮件服务或本地开发投递器。
type EmailSender interface {
	Send(ctx context.Context, to, code, purpose string) error
}

// DevEmailSender 开发投递器：验证码写服务日志（仅非严格模式使用）。
type DevEmailSender struct{}

func (DevEmailSender) Send(_ context.Context, to, code, purpose string) error {
	log.Printf("[email] dev deliver: to=%s purpose=%s code=%s", to, purpose, code)
	return nil
}

type unavailableEmailSender struct{}

func (unavailableEmailSender) Send(context.Context, string, string, string) error {
	return errNotImplemented("email delivery not configured")
}

// NewUnavailableEmailSender 严格模式未配置 SMTP 时的投递器：发码接口返回 501，
// 避免把验证码写进生产日志。
func NewUnavailableEmailSender() EmailSender { return unavailableEmailSender{} }

// RegisterService 自助注册（邮箱验证码主通道，手机号兼容）。
type RegisterService struct {
	users repo.UserRepo
	codes repo.SmsCodeRepo
	sms   SmsSender
	email EmailSender
	now   func() time.Time
}

func NewRegisterService(users repo.UserRepo, codes repo.SmsCodeRepo, sms SmsSender, email EmailSender) *RegisterService {
	if sms == nil {
		sms = DevSmsSender{}
	}
	if email == nil {
		email = DevEmailSender{}
	}
	return &RegisterService{users: users, codes: codes, sms: sms, email: email, now: time.Now}
}

// SendCode 向手机号发送注册验证码（旧版兼容）。返回值语义同 SendEmailCode。
func (s *RegisterService) SendCode(ctx context.Context, phone string) (string, int, error) {
	target, err := newPhoneTarget(phone)
	if err != nil {
		return "", 0, err
	}
	return s.sendRegisterCode(ctx, target)
}

// SendEmailCode 向邮箱发送注册验证码（主通道）。返回 (code, retryAfter, err)：
// code 非空表示本次实际生成并投递了验证码（供 handler 在回显模式下带回响应）；
// retryAfter > 0 表示限频剩余秒数（ErrSmsThrottled）。
func (s *RegisterService) SendEmailCode(ctx context.Context, email string) (string, int, error) {
	target, err := newEmailTarget(email)
	if err != nil {
		return "", 0, err
	}
	return s.sendRegisterCode(ctx, target)
}

func (s *RegisterService) sendRegisterCode(ctx context.Context, target verificationTarget) (string, int, error) {
	// 目标已注册先于限频拒绝：让学员等满 60s 倒计时只会再得到同一个拒绝，
	// 先告知事实并引导去登录（前端据此提示）。
	if _, err := findUserByTarget(ctx, s.users, target); err == nil {
		return "", 0, targetExistsError(target)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", 0, err
	}
	return sendVerificationCode(ctx, s.codes, s.now(), target, model.SmsPurposeRegister,
		func(code string) error {
			return deliverVerificationCode(ctx, s.sms, s.email, target, code, model.SmsPurposeRegister)
		})
}

// RegisterInput 自助注册参数：Email 与 Phone 必须且只能填写一个（邮箱为主通道）。
type RegisterInput struct {
	Email    string
	Phone    string
	Code     string
	Username string
	Password string
}

// Register 凭验证码建号（角色固定 student）并直接签发登录 token。
func (s *RegisterService) Register(ctx context.Context, in RegisterInput, issue func(*model.User) (string, time.Time, error)) (string, time.Time, *model.User, error) {
	target, err := parseVerificationTarget(in.Email, in.Phone)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	if len(in.Code) != 6 {
		return "", time.Time{}, nil, fmt.Errorf("%w: code must be 6 digits", ErrValidation)
	}
	if len(in.Username) < 3 || len(in.Username) > 64 {
		return "", time.Time{}, nil, fmt.Errorf("%w: username must be 3-64 characters", ErrValidation)
	}
	if len(in.Password) < 6 {
		return "", time.Time{}, nil, fmt.Errorf("%w: password must be at least 6 characters", ErrValidation)
	}
	now := s.now()
	latest, err := latestVerificationCode(ctx, s.codes, target, model.SmsPurposeRegister)
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
	if !constantTimeEq(latest.Code, in.Code) {
		return "", time.Time{}, nil, fmt.Errorf("%w: incorrect code", ErrValidation)
	}
	// 先核销再建号：并发同码注册只有一个成功（条件更新保证一次性）。
	if n, err := s.codes.MarkUsed(ctx, latest.ID); err != nil {
		return "", time.Time{}, nil, err
	} else if n == 0 {
		return "", time.Time{}, nil, fmt.Errorf("%w: code not sent or expired", ErrValidation)
	}
	// 用户名/目标查重（验证码已消耗，重复属调用方输入问题）。
	if _, err := s.users.FindByUsername(ctx, in.Username); err == nil {
		return "", time.Time{}, nil, fmt.Errorf("%w: username already exists", ErrValidation)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", time.Time{}, nil, err
	}
	if _, err := findUserByTarget(ctx, s.users, target); err == nil {
		return "", time.Time{}, nil, targetExistsError(target)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", time.Time{}, nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return "", time.Time{}, nil, err
	}
	user := &model.User{
		Username: in.Username, PasswordHash: string(hash),
		GlobalRole: model.GlobalRoleStudent,
	}
	if target.isEmail() {
		user.Email = target.value
	} else {
		user.Phone = target.value
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
