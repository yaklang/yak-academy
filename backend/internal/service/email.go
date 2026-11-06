package service

// SMTP 验证码投递：支持 starttls（587/25）/ implicit TLS（465）/ none（本地或
// 内网明文中继）。只依赖标准库；配置可由管理端设置页写入（密码加密落库），
// 环境变量仅作为首次启动默认值，密码不写日志。

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

const smtpTimeout = 10 * time.Second

// SMTPEmailSender SMTP/事务邮件服务投递器。
type SMTPEmailSender struct {
	addr        string
	host        string
	username    string
	password    string
	fromAddress string
	fromHeader  string
	tlsMode     string
}

// EmailTester 管理端投递自检能力（仅真实 SMTP 投递器实现；开发日志与未配置
// 投递器不实现，测试邮件返回“未配置”）。
type EmailTester interface {
	SendTest(ctx context.Context, to string) error
}

// DynamicEmailSender 可热替换的投递器：配置保存后原子换 sender，已装配的
// 注册/找回服务持有同一引用即可立即生效，无需重启。
type DynamicEmailSender struct {
	mu     sync.RWMutex
	sender EmailSender
}

func NewDynamicEmailSender(initial EmailSender) *DynamicEmailSender {
	if initial == nil {
		initial = NewUnavailableEmailSender()
	}
	return &DynamicEmailSender{sender: initial}
}

func (d *DynamicEmailSender) Set(sender EmailSender) {
	if sender == nil {
		sender = NewUnavailableEmailSender()
	}
	d.mu.Lock()
	d.sender = sender
	d.mu.Unlock()
}

func (d *DynamicEmailSender) Send(ctx context.Context, to, code, purpose string) error {
	return d.current().Send(ctx, to, code, purpose)
}

func (d *DynamicEmailSender) SendTest(ctx context.Context, to string) error {
	tester, ok := d.current().(EmailTester)
	if !ok {
		return errNotImplemented("email delivery not configured")
	}
	return tester.SendTest(ctx, to)
}

func (d *DynamicEmailSender) current() EmailSender {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.sender
}

// NewSMTPEmailSender 校验并构造 SMTP 投递器。addr 为 host:port（省略端口按
// 587）；from 可含显示名，空则取 username。tlsMode 空按 starttls。
func NewSMTPEmailSender(addr, username, password, from, tlsMode string) (*SMTPEmailSender, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("smtp address is empty")
	}
	host, port, err := splitSMTPAddr(addr)
	if err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(tlsMode))
	if mode == "" {
		mode = "starttls"
	}
	switch mode {
	case "starttls", "implicit", "none":
	default:
		return nil, fmt.Errorf("unknown smtp tls mode %q (want starttls, implicit or none)", tlsMode)
	}
	from = strings.TrimSpace(from)
	if from == "" {
		from = username
	}
	if from == "" {
		return nil, errors.New("smtp from address is empty")
	}
	if strings.ContainsAny(from, "\r\n") {
		return nil, errors.New("smtp from address contains newline")
	}
	parsed, err := mail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("invalid smtp from address: %w", err)
	}
	fromHeader := parsed.Address
	if parsed.Name != "" {
		// 非 ASCII 显示名按 RFC 2047 编码，避免 SMTPUTF8 未开启时头部乱码。
		fromHeader = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("UTF-8", parsed.Name), parsed.Address)
	}
	return &SMTPEmailSender{
		addr:        net.JoinHostPort(host, port),
		host:        host,
		username:    username,
		password:    password,
		fromAddress: parsed.Address,
		fromHeader:  fromHeader,
		tlsMode:     mode,
	}, nil
}

func splitSMTPAddr(addr string) (host, port string, err error) {
	if !strings.Contains(addr, ":") {
		return addr, "587", nil
	}
	host, port, err = net.SplitHostPort(addr)
	if err != nil {
		return "", "", fmt.Errorf("invalid smtp address %q: %w", addr, err)
	}
	if host == "" || port == "" {
		return "", "", fmt.Errorf("invalid smtp address %q (want host:port)", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("invalid smtp port %q", port)
	}
	return host, port, nil
}

func (s *SMTPEmailSender) Send(ctx context.Context, to, code, purpose string) error {
	parsed, err := parseRecipient(to)
	if err != nil {
		return err
	}
	return s.deliver(ctx, parsed.Address, buildVerificationEmail(s.fromHeader, parsed.Address, code, purpose, time.Now()))
}

// SendTest 管理端投递自检：发送固定文案测试邮件，不创建验证码、不写验证码表。
func (s *SMTPEmailSender) SendTest(ctx context.Context, to string) error {
	parsed, err := parseRecipient(to)
	if err != nil {
		return err
	}
	return s.deliver(ctx, parsed.Address, buildTestEmail(s.fromHeader, parsed.Address, time.Now()))
}

func parseRecipient(to string) (*mail.Address, error) {
	to = strings.TrimSpace(to)
	if strings.ContainsAny(to, "\r\n") {
		return nil, errors.New("recipient address contains newline")
	}
	parsed, err := mail.ParseAddress(to)
	if err != nil {
		return nil, fmt.Errorf("invalid recipient address: %w", err)
	}
	return parsed, nil
}

func (s *SMTPEmailSender) deliver(ctx context.Context, recipient, message string) error {
	client, err := s.dial(ctx)
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	defer client.Close()
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(s.fromAddress); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp finish message: %w", err)
	}
	// Data 关闭即表示服务端已接收；QUIT 失败不应触发上层重试（会重复发信）。
	_ = client.Quit()
	return nil
}

func (s *SMTPEmailSender) dial(ctx context.Context) (*smtp.Client, error) {
	tlsConfig := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: smtpTimeout}
	var (
		conn net.Conn
		err  error
	)
	if s.tlsMode == "implicit" {
		conn, err = tls.DialWithDialer(dialer, "tcp", s.addr, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", s.addr)
	}
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(smtpTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if s.tlsMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, errors.New("smtp server does not advertise STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("smtp starttls: %w", err)
		}
	}
	return client, nil
}

// buildVerificationEmail 生成 UTF-8 纯文本验证码邮件（RFC 5322 + MIME）。
func buildVerificationEmail(fromHeader, to, code, purpose string, now time.Time) string {
	label := "注册"
	if purpose == model.SmsPurposePasswordReset {
		label = "密码重置"
	}
	body := fmt.Sprintf("你的 Yak Academy %s验证码是：%s\r\n\r\n验证码 5 分钟内有效，仅可使用一次。\r\n如果不是你本人操作，请忽略本邮件。\r\n", label, code)
	return buildPlainTextEmail(fromHeader, to, "Yak Academy "+label+"验证码", body, now)
}

// buildTestEmail 管理端投递自检邮件（明确标注测试用途，避免被误认为验证码）。
func buildTestEmail(fromHeader, to string, now time.Time) string {
	return buildPlainTextEmail(fromHeader, to, "Yak Academy SMTP 测试邮件",
		"这是一封来自 Yak Academy 管理端的测试邮件。\r\n\r\n收到本邮件说明 SMTP 地址、TLS 模式与账号密码配置可用。\r\n", now)
}

func buildPlainTextEmail(fromHeader, to, subject, body string, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", fromHeader)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}
