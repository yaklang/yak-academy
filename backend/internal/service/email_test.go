package service

// SMTP 验证码投递测试：纯函数（邮件正文/配置校验）与本地 fake SMTP 协议往返。
// 网络测试只监听 127.0.0.1 随机端口，不依赖外部邮件服务。

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

func TestUnavailableEmailSender(t *testing.T) {
	err := NewUnavailableEmailSender().Send(context.Background(), "user@example.com", "123456", model.SmsPurposeRegister)
	if !IsNotImplemented(err) {
		t.Fatalf("want NotImplementedError, got %v", err)
	}
}

func TestBuildVerificationEmail(t *testing.T) {
	body := buildVerificationEmail("Yak Academy <no-reply@example.com>", "user@example.com", "123456",
		model.SmsPurposePasswordReset, time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC))
	for _, want := range []string{
		"From: Yak Academy <no-reply@example.com>",
		"To: user@example.com",
		"Subject: =?UTF-8?",
		"密码重置验证码",
		"你的 Yak Academy 密码重置验证码是：123456",
		"5 分钟内有效",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("mail body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "\n") && !strings.Contains(body, "\r\n") {
		t.Error("mail body must use CRLF")
	}
}

func TestNewSMTPEmailSenderValidation(t *testing.T) {
	cases := []struct {
		name, addr, user, pass, from, mode string
	}{
		{"empty addr", "", "u", "p", "from@example.com", "starttls"},
		{"bad tls mode", "smtp.example.com:587", "u", "p", "from@example.com", "ssl3"},
		{"empty from", "smtp.example.com:587", "", "", "", "starttls"},
		{"bad from", "smtp.example.com:587", "u", "p", "not-an-address", "starttls"},
		{"newline from", "smtp.example.com:587", "u", "p", "a@example.com\r\nBcc: x@example.com", "starttls"},
		{"bad addr", "smtp.example.com:not-a-port", "u", "p", "from@example.com", "starttls"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewSMTPEmailSender(c.addr, c.user, c.pass, c.from, c.mode); err == nil {
				t.Fatal("want validation error")
			}
		})
	}

	// 省略端口按 587，TLS 空按 starttls。
	s, err := NewSMTPEmailSender("smtp.example.com", "mailer@example.com", "p", "", "")
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if s.addr != "smtp.example.com:587" || s.tlsMode != "starttls" {
		t.Fatalf("defaults: addr=%q mode=%q", s.addr, s.tlsMode)
	}
	if s.fromAddress != "mailer@example.com" {
		t.Fatalf("from fallback: %q", s.fromAddress)
	}
	s2, err := NewSMTPEmailSender("smtp.example.com", "u", "p", "Yak 学院 <no-reply@example.com>", "starttls")
	if err != nil {
		t.Fatalf("unicode display name: %v", err)
	}
	if !strings.Contains(s2.fromHeader, "=?UTF-8?") {
		t.Fatalf("unicode display name not RFC2047 encoded: %q", s2.fromHeader)
	}
}

func TestSMTPEmailSenderSendsVerificationCode(t *testing.T) {
	addr, messages := startFakeSMTPServer(t)
	sender, err := NewSMTPEmailSender(addr, "", "", "no-reply@example.com", "none")
	if err != nil {
		t.Fatalf("sender: %v", err)
	}
	if err := sender.Send(context.Background(), "student@example.com", "654321", model.SmsPurposeRegister); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case msg := <-messages:
		for _, want := range []string{"To: student@example.com", "注册验证码", "654321"} {
			if !strings.Contains(msg, want) {
				t.Errorf("message missing %q:\n%s", want, msg)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fake SMTP server did not receive message")
	}
}

// startFakeSMTPServer 最小 SMTP 服务器：接受 EHLO/MAIL/RCPT/DATA/QUIT，
// 把 DATA 正文送到 messages。仅测试用，不实现 STARTTLS/AUTH。
func startFakeSMTPServer(t *testing.T) (string, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	messages := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := bufio.NewWriter(conn)
		write := func(line string) {
			_, _ = fmt.Fprintf(w, "%s\r\n", line)
			_ = w.Flush()
		}
		write("220 fake SMTP ready")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.TrimSpace(strings.ToUpper(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				_, _ = fmt.Fprint(w, "250-fake\r\n250 OK\r\n")
				_ = w.Flush()
			case strings.HasPrefix(cmd, "HELO"):
				write("250 OK")
			case strings.HasPrefix(cmd, "MAIL FROM:"):
				write("250 OK")
			case strings.HasPrefix(cmd, "RCPT TO:"):
				write("250 OK")
			case cmd == "DATA":
				write("354 End data with <CR><LF>.<CR><LF>")
				var body strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" || l == ".\n" {
						break
					}
					body.WriteString(l)
				}
				messages <- body.String()
				write("250 OK")
			case cmd == "QUIT":
				write("221 Bye")
				return
			default:
				write("250 OK")
			}
		}
	}()
	return ln.Addr().String(), messages
}
