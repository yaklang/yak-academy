package service

// 验证码目标与共用流程：历史账号找回密码（邮箱主通道、手机号兼容）。
// 手机号/邮箱自助注册已下线；验证码只用于 password_reset 用途。
// 目标必须二选一（email / phone），验证码按目标渠道落库、按 purpose 隔离；
// 同目标 60 秒限频、10 分钟窗口最多 3 条，与 docs/architecture.md §3.1 一致。

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

const (
	verificationChannelEmail = "email"
	verificationChannelPhone = "phone"

	smsCodeTTL      = 5 * time.Minute
	smsSendInterval = 60 * time.Second
	smsWindow       = 10 * time.Minute
	smsWindowLimit  = 3
)

// ErrSmsThrottled 发码限频（60s 内重复 / 窗口超量），handler 转 200 + retry_after。
// 邮箱与手机号通道共用该哨兵（历史命名保留）。
var ErrSmsThrottled = errors.New("sms send throttled")

// verificationTarget 已校验的验证码目标：邮箱（小写归一）或手机号。
type verificationTarget struct {
	channel string
	value   string
}

func (t verificationTarget) isEmail() bool { return t.channel == verificationChannelEmail }

// parseVerificationTarget 从请求字段解析唯一目标：email / phone 必须且只能填一个。
func parseVerificationTarget(email, phone string) (verificationTarget, error) {
	email = strings.TrimSpace(email)
	phone = strings.TrimSpace(phone)
	switch {
	case email == "" && phone == "":
		return verificationTarget{}, fmt.Errorf("%w: email or phone is required", ErrValidation)
	case email != "" && phone != "":
		return verificationTarget{}, fmt.Errorf("%w: email and phone are mutually exclusive", ErrValidation)
	case email != "":
		return newEmailTarget(email)
	default:
		return newPhoneTarget(phone)
	}
}

func newEmailTarget(email string) (verificationTarget, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) {
		return verificationTarget{}, fmt.Errorf("%w: invalid email address", ErrValidation)
	}
	return verificationTarget{channel: verificationChannelEmail, value: email}, nil
}

func newPhoneTarget(phone string) (verificationTarget, error) {
	phone = strings.TrimSpace(phone)
	if !validPhone(phone) {
		return verificationTarget{}, fmt.Errorf("%w: invalid phone number", ErrValidation)
	}
	return verificationTarget{channel: verificationChannelPhone, value: phone}, nil
}

// validEmail 校验邮箱形态：必须是单个裸地址（不接受 "Name <addr>" 展示名），
// 无空白/控制字符，总长 ≤254（RFC 5321 投递上限）。
func validEmail(email string) bool {
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return addr.Name == "" && strings.EqualFold(addr.Address, email) && strings.Contains(email, "@")
}

func (t verificationTarget) codeModel(code, purpose string, now time.Time) *model.SmsCode {
	c := &model.SmsCode{
		Code:      code,
		Purpose:   purpose,
		ExpiresAt: now.Add(smsCodeTTL),
		CreatedAt: now,
	}
	if t.isEmail() {
		c.Email = t.value
	} else {
		c.Phone = t.value
	}
	return c
}

func latestVerificationCode(ctx context.Context, codes repo.SmsCodeRepo, target verificationTarget, purpose string) (*model.SmsCode, error) {
	if target.isEmail() {
		return codes.LatestByEmail(ctx, target.value, purpose)
	}
	return codes.LatestByPhone(ctx, target.value, purpose)
}

func countVerificationCodes(ctx context.Context, codes repo.SmsCodeRepo, target verificationTarget, purpose string, windowStart time.Time) (int64, error) {
	if target.isEmail() {
		return codes.CountSinceByEmail(ctx, target.value, purpose, windowStart)
	}
	return codes.CountSinceByPhone(ctx, target.value, purpose, windowStart)
}

// verificationThrottle 发码限频（注册与找回密码共用）：同目标同用途 60 秒内
// 仅一次、10 分钟窗口内最多 3 次。命中限频返回 (retryAfter, ErrSmsThrottled)。
func verificationThrottle(ctx context.Context, codes repo.SmsCodeRepo, target verificationTarget, purpose string, now time.Time) (int, error) {
	// 60 秒内已发过：直接返回剩余等待时间（不重复发送）。
	if latest, err := latestVerificationCode(ctx, codes, target, purpose); err == nil {
		if wait := smsSendInterval - now.Sub(latest.CreatedAt); wait > 0 {
			return int(wait.Seconds()) + 1, ErrSmsThrottled
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	// 10 分钟窗口内最多 3 条。
	n, err := countVerificationCodes(ctx, codes, target, purpose, now.Add(-smsWindow))
	if err != nil {
		return 0, err
	}
	if n >= smsWindowLimit {
		return int(smsWindow.Seconds()), ErrSmsThrottled
	}
	return 0, nil
}

// sendVerificationCode 生成并投递验证码：先限频，再入库，最后投递。
// code 返回给调用方供回显模式（仅本地演示）使用；retryAfter > 0 = 限频。
func sendVerificationCode(ctx context.Context, codes repo.SmsCodeRepo, now time.Time, target verificationTarget, purpose string, deliver func(code string) error) (string, int, error) {
	if retryAfter, err := verificationThrottle(ctx, codes, target, purpose, now); err != nil {
		return "", retryAfter, err
	}
	code, err := genCode()
	if err != nil {
		return "", 0, err
	}
	if err := codes.Create(ctx, target.codeModel(code, purpose, now)); err != nil {
		return "", 0, err
	}
	if err := deliver(code); err != nil {
		return "", 0, err
	}
	return code, 0, nil
}

func findUserByTarget(ctx context.Context, users repo.UserRepo, target verificationTarget) (*model.User, error) {
	if target.isEmail() {
		return users.FindByEmail(ctx, target.value)
	}
	return users.FindByPhone(ctx, target.value)
}

func targetMissingError(target verificationTarget) error {
	if target.isEmail() {
		return fmt.Errorf("%w: email not registered", ErrValidation)
	}
	return fmt.Errorf("%w: phone not registered", ErrValidation)
}

// deliverVerificationCode 按目标渠道投递：邮箱走 SMTP/开发投递器，手机号走
// 旧版短信投递器。
func deliverVerificationCode(ctx context.Context, sms SmsSender, email EmailSender, target verificationTarget, code, purpose string) error {
	if target.isEmail() {
		return email.Send(ctx, target.value, code, purpose)
	}
	return sms.Send(target.value, code)
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
