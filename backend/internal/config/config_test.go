package config

import "testing"

func TestStrictViolations(t *testing.T) {
	strongSecret := "0123456789abcdef0123456789abcdef" // 32 字符
	strongPass := "a-strong-password"

	cases := []struct {
		name string
		cfg  Config
		want int // 期望违规条数
	}{
		{"all defaults", Config{JWTSecret: DefaultJWTSecret, AdminPassword: DefaultAdminPassword}, 2},
		{"default secret only", Config{JWTSecret: DefaultJWTSecret, AdminPassword: strongPass}, 1},
		{"default password only", Config{JWTSecret: strongSecret, AdminPassword: DefaultAdminPassword}, 1},
		{"short secret", Config{JWTSecret: "short", AdminPassword: strongPass}, 1},
		{"short password", Config{JWTSecret: strongSecret, AdminPassword: "short"}, 1},
		{"strong credentials", Config{JWTSecret: strongSecret, AdminPassword: strongPass}, 0},
		{"strict mode rejects verification echo", Config{JWTSecret: strongSecret, AdminPassword: strongPass, VerificationShowCode: true}, 1},
	}
	for _, c := range cases {
		if got := len(c.cfg.StrictViolations()); got != c.want {
			t.Fatalf("%s: violations = %d (%v), want %d", c.name, got, c.cfg.StrictViolations(), c.want)
		}
	}
}

// TestVerificationShowCodeDefault 回显默认关闭（生产安全）；新变量优先，
// 旧 ACADEMY_SMS_SHOW_CODE 兼容。
func TestVerificationShowCodeDefault(t *testing.T) {
	cases := []struct {
		name   string
		show   string
		legacy string
		want   bool
	}{
		{"default off", "", "", false},
		{"new on", "1", "", true},
		{"new off", "0", "", false},
		{"legacy on", "", "1", true},
		{"legacy true", "", "true", true},
		{"legacy off", "", "0", false},
		{"new off overrides legacy on", "0", "1", false},
		{"new on with legacy off", "1", "0", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("ACADEMY_SHOW_VERIFICATION_CODE", c.show)
			t.Setenv("ACADEMY_SMS_SHOW_CODE", c.legacy)
			if got := Load().VerificationShowCode; got != c.want {
				t.Fatalf("VerificationShowCode = %v, want %v (show=%q legacy=%q)", got, c.want, c.show, c.legacy)
			}
		})
	}
}

func TestLoadSMTPEnvs(t *testing.T) {
	t.Setenv("ACADEMY_SMTP_ADDR", "smtp.example.com:587")
	t.Setenv("ACADEMY_SMTP_USERNAME", "mailer@example.com")
	t.Setenv("ACADEMY_SMTP_PASSWORD", "secret")
	t.Setenv("ACADEMY_SMTP_FROM", "Yak Academy <no-reply@example.com>")
	t.Setenv("ACADEMY_SMTP_TLS", "STARTTLS")

	cfg := Load()
	if cfg.SMTPAddr != "smtp.example.com:587" || cfg.SMTPUsername != "mailer@example.com" {
		t.Fatalf("smtp options: %+v", cfg)
	}
	if cfg.SMTPPassword != "secret" || cfg.SMTPFrom != "Yak Academy <no-reply@example.com>" {
		t.Fatalf("smtp credentials/from: %+v", cfg)
	}
	if cfg.SMTPTLS != "starttls" {
		t.Fatalf("smtp tls normalized: %q", cfg.SMTPTLS)
	}
}

func TestLoadWriteupStoreEnvs(t *testing.T) {
	t.Setenv("ACADEMY_WRITEUPS_STORE", "s3")
	t.Setenv("ACADEMY_WRITEUPS_S3_ENDPOINT", "https://minio.example.com:9000")
	t.Setenv("ACADEMY_WRITEUPS_S3_BUCKET", "writeups")
	t.Setenv("ACADEMY_WRITEUPS_S3_PREFIX", "/academy/wp/")
	t.Setenv("ACADEMY_STRICT", "1")

	cfg := Load()
	if !cfg.Strict {
		t.Fatal("strict flag")
	}
	if cfg.WriteupsStoreKind != "s3" {
		t.Fatalf("store kind: %q", cfg.WriteupsStoreKind)
	}
	if cfg.WriteupsS3.Endpoint != "https://minio.example.com:9000" || cfg.WriteupsS3.Bucket != "writeups" {
		t.Fatalf("s3 options: %+v", cfg.WriteupsS3)
	}
	// 前后斜杠剥除：键前缀拼进对象键时不产生空段。
	if cfg.WriteupsS3.Prefix != "academy/wp" {
		t.Fatalf("prefix normalized: %q", cfg.WriteupsS3.Prefix)
	}

	t.Setenv("ACADEMY_WRITEUPS_STORE", "")
	if cfg := Load(); cfg.WriteupsStoreKind != "fs" {
		t.Fatalf("default kind: %q", cfg.WriteupsStoreKind)
	}
}
