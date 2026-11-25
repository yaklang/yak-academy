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
		{"strict mode rejects wechat mock", Config{JWTSecret: strongSecret, AdminPassword: strongPass, WechatMock: true}, 1},
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

func TestLoadWechatMockAdminUsername(t *testing.T) {
	t.Setenv("ACADEMY_WECHAT_MOCK_ADMIN_USERNAME", "  admin  ")
	if got := Load().WechatMockAdminUsername; got != "admin" {
		t.Fatalf("WechatMockAdminUsername = %q, want trimmed admin", got)
	}
	t.Setenv("ACADEMY_WECHAT_MOCK_ADMIN_USERNAME", "")
	if got := Load().WechatMockAdminUsername; got != "" {
		t.Fatalf("WechatMockAdminUsername = %q, want empty", got)
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

func TestLoadAwdRuntimeEnvs(t *testing.T) {
	t.Setenv("ACADEMY_AWD_ATTACK_IMAGE", "registry.local/awd/attack:1")
	t.Setenv("ACADEMY_AWD_ATTACK_SSH_BIND", "192.0.2.10")
	t.Setenv("ACADEMY_AWD_ATTACK_PUBLIC_HOST", "awd.example.com")
	t.Setenv("ACADEMY_AWD_TARGET_MEMORY", "256m")
	t.Setenv("ACADEMY_AWD_TARGET_CPUS", "0.25")
	t.Setenv("ACADEMY_AWD_ATTACK_MEMORY", "128m")
	t.Setenv("ACADEMY_AWD_ATTACK_CPUS", "0.1")

	cfg := Load()
	if cfg.AwdAttackImage != "registry.local/awd/attack:1" ||
		cfg.AwdAttackSSHBind != "192.0.2.10" ||
		cfg.AwdAttackPublicHost != "awd.example.com" {
		t.Fatalf("awd attack options: %+v", cfg)
	}
	if cfg.AwdTargetMemory != "256m" || cfg.AwdTargetCPUs != "0.25" ||
		cfg.AwdAttackMemory != "128m" || cfg.AwdAttackCPUs != "0.1" {
		t.Fatalf("awd resource limits: %+v", cfg)
	}

	for _, key := range []string{
		"ACADEMY_AWD_ATTACK_IMAGE",
		"ACADEMY_AWD_ATTACK_SSH_BIND",
		"ACADEMY_AWD_ATTACK_PUBLIC_HOST",
		"ACADEMY_AWD_TARGET_MEMORY",
		"ACADEMY_AWD_TARGET_CPUS",
		"ACADEMY_AWD_ATTACK_MEMORY",
		"ACADEMY_AWD_ATTACK_CPUS",
	} {
		t.Setenv(key, "")
	}
	defaults := Load()
	if defaults.AwdAttackImage != "yak-awd-attackbox:latest" ||
		defaults.AwdAttackSSHBind != "0.0.0.0" ||
		defaults.AwdAttackPublicHost != "127.0.0.1" {
		t.Fatalf("awd attack defaults: %+v", defaults)
	}
	if defaults.AwdTargetMemory != "512m" || defaults.AwdTargetCPUs != "0.5" ||
		defaults.AwdAttackMemory != "256m" || defaults.AwdAttackCPUs != "0.25" {
		t.Fatalf("awd resource defaults: %+v", defaults)
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

// TestLoadWechatEnvs 微信 OAuth 环境变量：默认公众号网页授权；mock 开关
// 只做本地演示，严格模式必须视为违规（生产禁止伪造微信身份）。
func TestLoadWechatEnvs(t *testing.T) {
	t.Setenv("ACADEMY_WECHAT_APP_ID", "wx-app-id")
	t.Setenv("ACADEMY_WECHAT_APP_SECRET", "wx-app-secret")
	t.Setenv("ACADEMY_WECHAT_REDIRECT_URL", "https://academy.example.com/login")
	t.Setenv("ACADEMY_WECHAT_MOCK", "1")

	cfg := Load()
	if cfg.WechatAppID != "wx-app-id" || cfg.WechatAppSecret != "wx-app-secret" {
		t.Fatalf("wechat credentials: %+v", cfg)
	}
	if cfg.WechatRedirectURL != "https://academy.example.com/login" {
		t.Fatalf("wechat redirect: %q", cfg.WechatRedirectURL)
	}
	if cfg.WechatMode != "official_account" {
		t.Fatalf("default mode: %q", cfg.WechatMode)
	}
	if !cfg.WechatMock {
		t.Fatal("mock flag")
	}
	found := false
	for _, v := range cfg.StrictViolations() {
		if v == "ACADEMY_WECHAT_MOCK must be disabled in strict mode" {
			found = true
		}
	}
	if !found {
		t.Fatalf("strict violations missing wechat mock: %v", cfg.StrictViolations())
	}
}
