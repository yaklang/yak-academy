package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func newSettingsTestService(t *testing.T, env SMTPEnvConfig, devFallback bool, secret string) (*SettingsService, *DynamicEmailSender, *gorm.DB) {
	t.Helper()
	name := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	dynamic := NewDynamicEmailSender(NewUnavailableEmailSender())
	svc := NewSettingsService(repo.NewSettingsRepo(gdb), secret, env, devFallback, dynamic)
	return svc, dynamic, gdb
}

func settingsAdmin(id uint, name string) *model.User {
	return &model.User{ID: id, Username: name, GlobalRole: model.GlobalRoleAdmin}
}

func TestUpdateSMTPEncryptsPasswordAndHotReloads(t *testing.T) {
	svc, dynamic, gdb := newSettingsTestService(t, SMTPEnvConfig{}, true, "test-jwt-secret")
	view, err := svc.UpdateSMTP(context.Background(), SMTPUpdateInput{
		Enabled: true, Addr: "smtp.example.com:587", Username: "mailer@example.com",
		Password: "super-secret", From: "Yak Academy <no-reply@example.com>", TLS: "starttls",
	}, settingsAdmin(7, "admin"))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !view.Enabled || !view.HasPassword || view.Source != "database" || view.UpdatedBy != "admin" {
		t.Fatalf("unexpected view: %+v", view)
	}
	sender, ok := dynamic.current().(*SMTPEmailSender)
	if !ok {
		t.Fatalf("dynamic sender = %T, want *SMTPEmailSender", dynamic.current())
	}
	if sender.addr != "smtp.example.com:587" || sender.password != "super-secret" || sender.tlsMode != "starttls" {
		t.Fatalf("sender fields: addr=%q tls=%q password=%q", sender.addr, sender.tlsMode, sender.password)
	}

	var rec model.SystemSetting
	if err := gdb.Where("key = ?", smtpSettingsKey).First(&rec).Error; err != nil {
		t.Fatalf("load setting: %v", err)
	}
	if strings.Contains(rec.Value, "super-secret") {
		t.Fatalf("password stored in plaintext: %s", rec.Value)
	}
	if !strings.Contains(rec.Value, "v1:") {
		t.Fatalf("password ciphertext missing version prefix: %s", rec.Value)
	}

	// 模拟重启：相同 JWT secret 能解密并重新装配。
	restartedDynamic := NewDynamicEmailSender(NewUnavailableEmailSender())
	restarted := NewSettingsService(repo.NewSettingsRepo(gdb), "test-jwt-secret", SMTPEnvConfig{}, true, restartedDynamic)
	if err := restarted.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	again, ok := restartedDynamic.current().(*SMTPEmailSender)
	if !ok || again.password != "super-secret" {
		t.Fatalf("restart did not restore SMTP sender: %T %+v", restartedDynamic.current(), restartedDynamic.current())
	}
}

func TestUpdateSMTPKeepsPasswordWhenBlank(t *testing.T) {
	svc, dynamic, _ := newSettingsTestService(t, SMTPEnvConfig{}, true, "test-jwt-secret")
	if _, err := svc.UpdateSMTP(context.Background(), SMTPUpdateInput{
		Enabled: true, Addr: "smtp.example.com:587", Username: "mailer@example.com",
		Password: "first-secret", From: "no-reply@example.com", TLS: "starttls",
	}, settingsAdmin(1, "admin")); err != nil {
		t.Fatalf("first save: %v", err)
	}
	view, err := svc.UpdateSMTP(context.Background(), SMTPUpdateInput{
		Enabled: true, Addr: "smtp2.example.com:465", Username: "mailer@example.com",
		From: "no-reply@example.com", TLS: "implicit",
	}, settingsAdmin(1, "admin"))
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if !view.HasPassword {
		t.Fatal("blank password should keep the saved one")
	}
	sender, ok := dynamic.current().(*SMTPEmailSender)
	if !ok || sender.password != "first-secret" || sender.addr != "smtp2.example.com:465" {
		t.Fatalf("unexpected sender after update: %T %+v", dynamic.current(), dynamic.current())
	}
}

func TestUpdateSMTPValidation(t *testing.T) {
	svc, _, _ := newSettingsTestService(t, SMTPEnvConfig{}, true, "test-jwt-secret")
	admin := settingsAdmin(1, "admin")
	cases := []struct {
		name string
		in   SMTPUpdateInput
		want string
	}{
		{"empty addr", SMTPUpdateInput{Enabled: true, From: "no-reply@example.com", TLS: "starttls"}, "smtp address is required"},
		{"bad addr", SMTPUpdateInput{Enabled: true, Addr: "smtp.example.com:not-port", From: "no-reply@example.com", TLS: "starttls"}, "invalid smtp address"},
		{"bad tls", SMTPUpdateInput{Enabled: true, Addr: "smtp.example.com:587", From: "no-reply@example.com", TLS: "ssl3"}, "invalid smtp tls mode"},
		{"missing from", SMTPUpdateInput{Enabled: true, Addr: "smtp.example.com:587", TLS: "starttls"}, "smtp from address is required"},
		{"missing password", SMTPUpdateInput{Enabled: true, Addr: "smtp.example.com:587", Username: "mailer@example.com", From: "no-reply@example.com", TLS: "starttls"}, "smtp password is required when username is set"},
		{"disabled bad tls", SMTPUpdateInput{Enabled: false, TLS: "ssl3"}, "invalid smtp tls mode"},
		{"disabled bad addr", SMTPUpdateInput{Enabled: false, Addr: "smtp.example.com:not-port", TLS: "starttls"}, "invalid smtp address"},
		{"disabled bad from", SMTPUpdateInput{Enabled: false, From: "not-an-address", TLS: "starttls"}, "invalid smtp from address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.UpdateSMTP(context.Background(), tc.in, admin)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want detail %q", err.Error(), tc.want)
			}
		})
	}
}

func TestSMTPSettingsPasswordDecryptErrorAfterSecretRotation(t *testing.T) {
	svc, _, gdb := newSettingsTestService(t, SMTPEnvConfig{}, true, "old-secret")
	if _, err := svc.UpdateSMTP(context.Background(), SMTPUpdateInput{
		Enabled: true, Addr: "smtp.example.com:587", Username: "mailer@example.com",
		Password: "old-password", From: "no-reply@example.com", TLS: "starttls",
	}, settingsAdmin(1, "admin")); err != nil {
		t.Fatalf("save: %v", err)
	}

	dynamic := NewDynamicEmailSender(NewUnavailableEmailSender())
	rotated := NewSettingsService(repo.NewSettingsRepo(gdb), "new-secret", SMTPEnvConfig{}, true, dynamic)
	if err := rotated.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	view, err := rotated.SMTP(context.Background())
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if !view.PasswordError || !view.HasPassword {
		t.Fatalf("want password_error with has_password, got %+v", view)
	}
	if _, ok := dynamic.current().(*SMTPEmailSender); ok {
		t.Fatal("undecryptable password must not produce a live sender")
	}
	_, err = rotated.UpdateSMTP(context.Background(), SMTPUpdateInput{
		Enabled: true, Addr: "smtp.example.com:587", Username: "mailer@example.com",
		From: "no-reply@example.com", TLS: "starttls",
	}, settingsAdmin(1, "admin"))
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "cannot be decrypted") {
		t.Fatalf("blank password after rotation = %v", err)
	}
	if _, err := rotated.UpdateSMTP(context.Background(), SMTPUpdateInput{
		Enabled: true, Addr: "smtp.example.com:587", Username: "mailer@example.com",
		Password: "new-password", From: "no-reply@example.com", TLS: "starttls",
	}, settingsAdmin(1, "admin")); err != nil {
		t.Fatalf("re-save password: %v", err)
	}
}

func TestTestSMTPUsesConfiguredSender(t *testing.T) {
	addr, messages := startFakeSMTPServer(t)
	svc, _, _ := newSettingsTestService(t, SMTPEnvConfig{}, true, "test-jwt-secret")
	if _, err := svc.UpdateSMTP(context.Background(), SMTPUpdateInput{
		Enabled: true, Addr: addr, From: "no-reply@example.com", TLS: "none",
	}, settingsAdmin(1, "admin")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := svc.TestSMTP(context.Background(), "admin@example.com"); err != nil {
		t.Fatalf("test send: %v", err)
	}
	select {
	case msg := <-messages:
		if !strings.Contains(msg, "To: admin@example.com") || !strings.Contains(msg, "这是一封来自 Yak Academy 管理端的测试邮件") {
			t.Fatalf("unexpected test message:\n%s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fake SMTP server did not receive test message")
	}
}

func TestTestSMTPValidation(t *testing.T) {
	svc, _, _ := newSettingsTestService(t, SMTPEnvConfig{}, true, "test-jwt-secret")
	if err := svc.TestSMTP(context.Background(), ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty recipient = %v", err)
	}
	if err := svc.TestSMTP(context.Background(), "not-an-email"); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid recipient = %v", err)
	}
	if err := svc.TestSMTP(context.Background(), "admin@example.com"); !errors.Is(err, ErrValidation) {
		t.Fatalf("unconfigured sender = %v", err)
	}
}

func TestUpdateSMTPDisabledFallsBackToDev(t *testing.T) {
	svc, dynamic, _ := newSettingsTestService(t, SMTPEnvConfig{}, true, "test-jwt-secret")
	view, err := svc.UpdateSMTP(context.Background(), SMTPUpdateInput{
		Enabled: false, Addr: "smtp.example.com:587", Username: "mailer@example.com",
		Password: "secret", From: "no-reply@example.com", TLS: "starttls",
	}, settingsAdmin(1, "admin"))
	if err != nil {
		t.Fatalf("save disabled: %v", err)
	}
	if view.Enabled {
		t.Fatal("view should be disabled")
	}
	if _, ok := dynamic.current().(DevEmailSender); !ok {
		t.Fatalf("dynamic sender = %T, want DevEmailSender", dynamic.current())
	}
	if err := svc.TestSMTP(context.Background(), "admin@example.com"); !errors.Is(err, ErrValidation) {
		t.Fatalf("test while disabled = %v", err)
	}
}

func TestInitializeEnvDefaults(t *testing.T) {
	svc, dynamic, _ := newSettingsTestService(t, SMTPEnvConfig{
		Addr: "smtp.env.example.com:465", Username: "env@example.com", Password: "env-secret",
		From: "no-reply@example.com", TLS: "implicit",
	}, true, "test-jwt-secret")
	if err := svc.Initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	view, err := svc.SMTP(context.Background())
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if view.Source != "environment" || !view.Enabled || !view.HasPassword {
		t.Fatalf("unexpected env view: %+v", view)
	}
	sender, ok := dynamic.current().(*SMTPEmailSender)
	if !ok || sender.addr != "smtp.env.example.com:465" || sender.tlsMode != "implicit" {
		t.Fatalf("env sender: %T %+v", dynamic.current(), dynamic.current())
	}
}

func TestInitializeInvalidEnvFails(t *testing.T) {
	svc, _, _ := newSettingsTestService(t, SMTPEnvConfig{Addr: "smtp.example.com:not-port"}, true, "test-jwt-secret")
	if err := svc.Initialize(context.Background()); err == nil {
		t.Fatal("invalid env SMTP config should fail startup")
	}
}

func TestUpdateSMTPDisabledInStrictModeUsesUnavailable(t *testing.T) {
	svc, dynamic, _ := newSettingsTestService(t, SMTPEnvConfig{}, false, "test-jwt-secret")
	if _, err := svc.UpdateSMTP(context.Background(), SMTPUpdateInput{Enabled: false}, settingsAdmin(1, "admin")); err != nil {
		t.Fatalf("save disabled: %v", err)
	}
	if _, ok := dynamic.current().(*SMTPEmailSender); ok {
		t.Fatal("strict mode disabled config must not use SMTP")
	}
	if _, ok := dynamic.current().(DevEmailSender); ok {
		t.Fatal("strict mode must not fall back to log delivery")
	}
}
