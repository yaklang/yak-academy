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
	}
	for _, c := range cases {
		if got := len(c.cfg.StrictViolations()); got != c.want {
			t.Fatalf("%s: violations = %d (%v), want %d", c.name, got, c.cfg.StrictViolations(), c.want)
		}
	}
}

// TestSmsShowCodeDefault 回显默认值：非严格模式默认开（学员直接看到验证码），
// 生产严格模式默认关（避免找回验证码被任意请求方读取）；显式 0/1 恒优先。
func TestSmsShowCodeDefault(t *testing.T) {
	cases := []struct {
		name   string
		strict string
		show   string
		want   bool
	}{
		{"default non-strict", "", "", true},
		{"default strict", "1", "", false},
		{"explicit off non-strict", "", "0", false},
		{"explicit off strict", "1", "0", false},
		{"explicit on strict", "1", "1", true},
		{"legacy true", "", "true", true},
		{"legacy false", "1", "false", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("ACADEMY_STRICT", c.strict)
			t.Setenv("ACADEMY_SMS_SHOW_CODE", c.show)
			if got := Load().SmsShowCode; got != c.want {
				t.Fatalf("SmsShowCode = %v, want %v (strict=%q show=%q)", got, c.want, c.strict, c.show)
			}
		})
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
