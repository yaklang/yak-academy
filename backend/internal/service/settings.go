package service

// 运行期统一设置：system_settings 表的 SMTP 配置域。环境变量只作为首次启动
// 默认值；管理端保存后以数据库配置为准并立即热替换 DynamicEmailSender。
// 密码使用 AES-256-GCM 加密落库，密钥由 JWT secret 经 HKDF 派生（JWT secret
// 轮换后旧密码不可解密，需在设置页重新填写）。

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/mail"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/hkdf"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

const (
	smtpSettingsKey = "smtp"
	settingsKeyInfo = "yak-academy/system-settings/v1"
)

// SMTPEnvConfig 环境变量提供的启动默认值（未保存管理端配置时生效）。
type SMTPEnvConfig struct {
	Addr     string
	Username string
	Password string
	From     string
	TLS      string
}

// SMTPUpdateInput 管理端保存的 SMTP 配置。Password 为空 = 保留已保存密码
// （首次保存且当前来源为环境变量时沿用环境变量密码）。
type SMTPUpdateInput struct {
	Enabled  bool
	Addr     string
	Username string
	Password string
	From     string
	TLS      string
}

// SMTPSettingsView 管理端读取视图：密码只暴露 has_password，不返回原文。
type SMTPSettingsView struct {
	Enabled       bool       `json:"enabled"`
	Addr          string     `json:"addr"`
	Username      string     `json:"username"`
	From          string     `json:"from"`
	TLS           string     `json:"tls"`
	HasPassword   bool       `json:"has_password"`
	PasswordError bool       `json:"password_error"`
	ConfigError   bool       `json:"config_error"`
	Source        string     `json:"source"` // database / environment / none
	UpdatedAt     *time.Time `json:"updated_at"`
	UpdatedBy     string     `json:"updated_by"`
}

type smtpSettingValue struct {
	Enabled        bool   `json:"enabled"`
	Addr           string `json:"addr"`
	Username       string `json:"username"`
	From           string `json:"from"`
	TLS            string `json:"tls"`
	PasswordCipher string `json:"password_cipher,omitempty"`
	UpdatedByName  string `json:"updated_by_name"`
}

type effectiveSMTP struct {
	source        string // database / environment / none
	enabled       bool
	addr          string
	username      string
	from          string
	tls           string
	password      string
	hasPassword   bool
	passwordError bool
	configError   bool
	updatedAt     *time.Time
	updatedBy     string
}

// SettingsService 统一设置（当前仅 SMTP 域；后续配置域按同一模式扩展）。
type SettingsService struct {
	settings    repo.SettingsRepo
	key         []byte
	env         SMTPEnvConfig
	devFallback bool // 非严格模式且未配置/停用时写日志投递（本地演示）
	sender      *DynamicEmailSender
	mu          sync.Mutex
	now         func() time.Time
}

func NewSettingsService(settings repo.SettingsRepo, jwtSecret string, env SMTPEnvConfig, devFallback bool, sender *DynamicEmailSender) *SettingsService {
	if sender == nil {
		sender = NewDynamicEmailSender(NewUnavailableEmailSender())
	}
	return &SettingsService{
		settings:    settings,
		key:         deriveSettingsKey(jwtSecret),
		env:         env,
		devFallback: devFallback,
		sender:      sender,
		now:         time.Now,
	}
}

// Initialize 启动装配：数据库配置 > 环境变量 > 开发/未配置投递器。
// 数据库配置损坏或密码无法解密时只告警并保持不可用，保证管理员仍能登录设置页修复。
func (s *SettingsService) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.load(ctx)
	if err != nil {
		return err
	}
	if e.configError {
		log.Printf("warning: stored SMTP settings are corrupted — open the admin settings page and save again")
		s.sender.Set(s.idleSender())
		return nil
	}
	if e.passwordError {
		log.Printf("warning: stored SMTP password cannot be decrypted (JWT secret changed?) — re-enter it in admin settings")
		s.sender.Set(s.idleSender())
		return nil
	}
	if !e.enabled {
		s.sender.Set(s.idleSender())
		s.logIdle(e.source)
		return nil
	}
	sender, err := s.buildSender(e)
	if err != nil {
		if e.source == "environment" {
			return fmt.Errorf("smtp config: %w", err)
		}
		log.Printf("warning: stored SMTP settings are invalid (%v) — open the admin settings page and save again", err)
		s.sender.Set(s.idleSender())
		return nil
	}
	s.sender.Set(sender)
	log.Printf("email delivery: SMTP %s (%s, %s)", e.addr, e.tls, e.source)
	return nil
}

// SMTP 返回当前生效配置（管理端读取）。
func (s *SettingsService) SMTP(ctx context.Context) (SMTPSettingsView, error) {
	e, err := s.load(ctx)
	if err != nil {
		return SMTPSettingsView{}, err
	}
	return e.view(), nil
}

// UpdateSMTP 校验并保存配置，成功后立即热加载。先构造 sender 再落库：配置
// 不可用时不写入、不切换，保持旧配置继续服务。
func (s *SettingsService) UpdateSMTP(ctx context.Context, in SMTPUpdateInput, admin *model.User) (SMTPSettingsView, error) {
	if admin == nil {
		return SMTPSettingsView{}, fmt.Errorf("%w: admin is required", ErrValidation)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.load(ctx)
	if err != nil {
		return SMTPSettingsView{}, err
	}

	in.Addr = strings.TrimSpace(in.Addr)
	in.Username = strings.TrimSpace(in.Username)
	in.From = strings.TrimSpace(in.From)
	in.TLS = normalizeSMTPTLS(in.TLS)
	if err := validateSMTPUpdate(in); err != nil {
		return SMTPSettingsView{}, err
	}

	password := in.Password
	if password == "" {
		password = current.password
	}
	if in.Enabled && in.Username != "" && password == "" {
		if current.passwordError && current.hasPassword {
			return SMTPSettingsView{}, fmt.Errorf("%w: saved smtp password cannot be decrypted; enter it again", ErrValidation)
		}
		return SMTPSettingsView{}, fmt.Errorf("%w: smtp password is required when username is set", ErrValidation)
	}

	var sender EmailSender
	if in.Enabled {
		built, err := NewSMTPEmailSender(in.Addr, in.Username, password, in.From, in.TLS)
		if err != nil {
			return SMTPSettingsView{}, fmt.Errorf("%w: %s", ErrValidation, smtpConfigErrorDetail(err))
		}
		sender = built
	}

	cipherText, err := s.encryptPassword(password)
	if err != nil {
		return SMTPSettingsView{}, err
	}
	value := smtpSettingValue{
		Enabled:        in.Enabled,
		Addr:           in.Addr,
		Username:       in.Username,
		From:           in.From,
		TLS:            in.TLS,
		PasswordCipher: cipherText,
		UpdatedByName:  admin.Username,
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return SMTPSettingsView{}, fmt.Errorf("marshal smtp settings: %w", err)
	}
	now := s.now().UTC()
	rec := &model.SystemSetting{
		Key:       smtpSettingsKey,
		Value:     string(raw),
		UpdatedBy: admin.ID,
		UpdatedAt: now,
	}
	if err := s.settings.Put(ctx, rec); err != nil {
		return SMTPSettingsView{}, err
	}
	if sender != nil {
		s.sender.Set(sender)
		log.Printf("email delivery: SMTP %s (%s, admin settings)", in.Addr, in.TLS)
	} else {
		s.sender.Set(s.idleSender())
		s.logIdle("admin settings")
	}
	return SMTPSettingsView{
		Enabled:     in.Enabled,
		Addr:        in.Addr,
		Username:    in.Username,
		From:        in.From,
		TLS:         in.TLS,
		HasPassword: cipherText != "",
		Source:      "database",
		UpdatedAt:   &now,
		UpdatedBy:   admin.Username,
	}, nil
}

// TestSMTP 使用当前已生效配置发送测试邮件（不创建验证码、不落验证码表）。
func (s *SettingsService) TestSMTP(ctx context.Context, to string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("%w: smtp test recipient is required", ErrValidation)
	}
	if !validEmail(to) {
		return fmt.Errorf("%w: invalid email address", ErrValidation)
	}
	e, err := s.load(ctx)
	if err != nil {
		return err
	}
	if !e.enabled || e.configError || e.passwordError {
		return fmt.Errorf("%w: smtp is not configured", ErrValidation)
	}
	if err := s.sender.SendTest(ctx, to); err != nil {
		if IsNotImplemented(err) {
			return fmt.Errorf("%w: smtp is not configured", ErrValidation)
		}
		log.Printf("smtp test failed: %v", err)
		return fmt.Errorf("%w: smtp test failed", ErrValidation)
	}
	return nil
}

func (s *SettingsService) load(ctx context.Context) (effectiveSMTP, error) {
	rec, err := s.settings.Get(ctx, smtpSettingsKey)
	if err != nil {
		return effectiveSMTP{}, err
	}
	if rec == nil {
		if strings.TrimSpace(s.env.Addr) == "" {
			return effectiveSMTP{source: "none", tls: normalizeSMTPTLS(s.env.TLS)}, nil
		}
		return effectiveSMTP{
			source:      "environment",
			enabled:     true,
			addr:        strings.TrimSpace(s.env.Addr),
			username:    strings.TrimSpace(s.env.Username),
			from:        strings.TrimSpace(s.env.From),
			tls:         normalizeSMTPTLS(s.env.TLS),
			password:    s.env.Password,
			hasPassword: s.env.Password != "",
		}, nil
	}

	updatedAt := rec.UpdatedAt
	e := effectiveSMTP{source: "database", updatedAt: &updatedAt}
	var value smtpSettingValue
	if err := json.Unmarshal([]byte(rec.Value), &value); err != nil {
		e.configError = true
		return e, nil
	}
	e.enabled = value.Enabled
	e.addr = value.Addr
	e.username = value.Username
	e.from = value.From
	e.tls = normalizeSMTPTLS(value.TLS)
	e.updatedBy = value.UpdatedByName
	e.hasPassword = value.PasswordCipher != ""
	if value.PasswordCipher != "" {
		password, err := decryptSettingsSecret(s.key, value.PasswordCipher)
		if err != nil {
			e.passwordError = true
		} else {
			e.password = password
		}
	}
	return e, nil
}

func (e effectiveSMTP) view() SMTPSettingsView {
	return SMTPSettingsView{
		Enabled:       e.enabled,
		Addr:          e.addr,
		Username:      e.username,
		From:          e.from,
		TLS:           e.tls,
		HasPassword:   e.hasPassword,
		PasswordError: e.passwordError,
		ConfigError:   e.configError,
		Source:        e.source,
		UpdatedAt:     e.updatedAt,
		UpdatedBy:     e.updatedBy,
	}
}

func (s *SettingsService) buildSender(e effectiveSMTP) (*SMTPEmailSender, error) {
	return NewSMTPEmailSender(e.addr, e.username, e.password, e.from, e.tls)
}

func (s *SettingsService) idleSender() EmailSender {
	if s.devFallback {
		return DevEmailSender{}
	}
	return NewUnavailableEmailSender()
}

func (s *SettingsService) logIdle(source string) {
	if s.devFallback {
		log.Printf("warning: no active SMTP delivery (%s) — email verification codes will be written to the server log", source)
		return
	}
	log.Printf("warning: no active SMTP delivery (%s) — email verification endpoints will return 501", source)
}

func normalizeSMTPTLS(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "starttls"
	}
	return mode
}

// validateSMTPUpdate 对停用状态也做格式校验：避免保存一份“看起来能保存、
// 启用时才报错”的脏配置。启用路径仍由 NewSMTPEmailSender 做完整校验。
func validateSMTPUpdate(in SMTPUpdateInput) error {
	switch in.TLS {
	case "starttls", "implicit", "none":
	default:
		return fmt.Errorf("%w: invalid smtp tls mode", ErrValidation)
	}
	if !in.Enabled {
		if in.Addr != "" {
			if _, _, err := splitSMTPAddr(in.Addr); err != nil {
				return fmt.Errorf("%w: invalid smtp address", ErrValidation)
			}
		}
		if in.From != "" {
			if strings.ContainsAny(in.From, "\r\n") {
				return fmt.Errorf("%w: invalid smtp from address", ErrValidation)
			}
			if _, err := mail.ParseAddress(in.From); err != nil {
				return fmt.Errorf("%w: invalid smtp from address", ErrValidation)
			}
		}
	}
	return nil
}

// smtpConfigErrorDetail 把底层配置校验错误收敛为可登记中文映射的固定文案。
func smtpConfigErrorDetail(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "smtp address is empty"):
		return "smtp address is required"
	case strings.Contains(msg, "invalid smtp address"), strings.Contains(msg, "invalid smtp port"):
		return "invalid smtp address"
	case strings.Contains(msg, "tls mode"):
		return "invalid smtp tls mode"
	case strings.Contains(msg, "from address is empty"):
		return "smtp from address is required"
	case strings.Contains(msg, "from address"):
		return "invalid smtp from address"
	default:
		return "invalid smtp configuration"
	}
}

func deriveSettingsKey(jwtSecret string) []byte {
	reader := hkdf.New(sha256.New, []byte(jwtSecret), nil, []byte(settingsKeyInfo))
	key := make([]byte, 32)
	_, _ = io.ReadFull(reader, key)
	return key
}

func (s *SettingsService) encryptPassword(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("settings cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("settings cipher: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("settings nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptSettingsSecret(key []byte, cipherText string) (string, error) {
	encoded, ok := strings.CutPrefix(cipherText, "v1:")
	if !ok {
		return "", errors.New("unsupported settings cipher version")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode settings secret: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("settings secret is truncated")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt settings secret: %w", err)
	}
	return string(plain), nil
}
