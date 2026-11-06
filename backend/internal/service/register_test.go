package service

// 注册（邮箱主通道 + 手机号兼容）域测试：真实 SQLite 仓库（project-conventions
// skill：禁 mock repo）。覆盖发码限频（60s/窗口 3 次）、目标查重前置、验证码过期、
// 一次性核销、重复用户名/邮箱/手机号拒绝、角色固定 student、成功注册即签发 token。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

type sentEmail struct {
	to      string
	code    string
	purpose string
}

type regEnv struct {
	reg       *RegisterService
	users     repo.UserRepo
	codes     repo.SmsCodeRepo
	sent      []string // 短信投递探针记录已发验证码
	emailSent []sentEmail
	now       time.Time
}

func newRegEnv(t *testing.T) *regEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:regtestenv?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		dbg, _ := gdb.DB()
		dbg.Close()
	})
	env := &regEnv{
		users: repo.NewUserRepo(gdb),
		codes: repo.NewSmsCodeRepo(gdb),
		now:   time.Now(),
	}
	sender := senderFunc(func(phone, code string) error { env.sent = append(env.sent, code); return nil })
	emailSender := emailSenderFunc(func(_ context.Context, to, code, purpose string) error {
		env.emailSent = append(env.emailSent, sentEmail{to: to, code: code, purpose: purpose})
		return nil
	})
	env.reg = NewRegisterService(env.users, env.codes, sender, emailSender)
	env.reg.now = func() time.Time { return env.now }
	return env
}

// senderFunc 把投递探针适配成 SmsSender。
type senderFunc func(phone, code string) error

func (f senderFunc) Send(phone, code string) error { return f(phone, code) }

// emailSenderFunc 把投递探针适配成 EmailSender。
type emailSenderFunc func(ctx context.Context, to, code, purpose string) error

func (f emailSenderFunc) Send(ctx context.Context, to, code, purpose string) error {
	return f(ctx, to, code, purpose)
}

func issueTokenStub(_ *model.User) (string, time.Time, error) { return "tok", time.Time{}, nil }

// registerWithPhone 旧版手机号注册路径的测试入口。
func registerWithPhone(ctx context.Context, svc *RegisterService, phone, code, username, password string) (string, time.Time, *model.User, error) {
	return svc.Register(ctx, RegisterInput{Phone: phone, Code: code, Username: username, Password: password}, issueTokenStub)
}

func TestSendCodeThrottleAndWindow(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()

	if _, _, err := env.reg.SendCode(ctx, "13800000001"); err != nil {
		t.Fatalf("first send: %v", err)
	}
	// 60 秒内重复：ErrSmsThrottled + 剩余秒数。
	_, retry, err := env.reg.SendCode(ctx, "13800000001")
	if !errors.Is(err, ErrSmsThrottled) || retry <= 0 {
		t.Fatalf("want throttled with retry_after, got (%d, %v)", retry, err)
	}
	// 出 60 秒但仍在 10 分钟窗口：第 2、3 条放行。
	env.now = env.now.Add(61 * time.Second)
	if _, _, err := env.reg.SendCode(ctx, "13800000001"); err != nil {
		t.Fatalf("second send: %v", err)
	}
	env.now = env.now.Add(61 * time.Second)
	if _, _, err := env.reg.SendCode(ctx, "13800000001"); err != nil {
		t.Fatalf("third send: %v", err)
	}
	// 窗口内第 4 条：限频到窗口结束。
	env.now = env.now.Add(61 * time.Second)
	_, retry, err = env.reg.SendCode(ctx, "13800000001")
	if !errors.Is(err, ErrSmsThrottled) || retry != int(smsWindow.Seconds()) {
		t.Fatalf("want window throttle, got (%d, %v)", retry, err)
	}
	// 窗口滑出后恢复。
	env.now = env.now.Add(smsWindow + time.Minute)
	if _, _, err := env.reg.SendCode(ctx, "13800000001"); err != nil {
		t.Fatalf("send after window: %v", err)
	}
	if len(env.sent) != 4 {
		t.Fatalf("delivered = %d, want 4", len(env.sent))
	}
}

func TestSendCodeInvalidPhoneAndRegistered(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	for _, p := range []string{"", "12345678901", "12800000001", "1380000000", "1380000000a"} {
		if _, _, err := env.reg.SendCode(ctx, p); !errors.Is(err, ErrValidation) {
			t.Fatalf("phone %q: want ErrValidation, got %v", p, err)
		}
	}
	// 已注册手机号不发码。
	if err := env.users.Create(ctx, &model.User{Username: "taken", PasswordHash: "x", Phone: "13800000002"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.reg.SendCode(ctx, "13800000002"); !errors.Is(err, ErrValidation) {
		t.Fatalf("registered phone: want ErrValidation, got %v", err)
	}
	if len(env.sent) != 0 {
		t.Fatalf("no code should be delivered, got %v", env.sent)
	}
}

// TestSendCodeRegisteredBeatsThrottle 已注册判定优先于限频：注册成功后
// 60s 内（限频窗口内）同号再发码，学员应立刻知道"已注册、去登录"，
// 而不是先看满 60s 倒计时再收获同一个拒绝。
func TestSendCodeRegisteredBeatsThrottle(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	const phone = "13800000003"
	// 发码 → 注册成功（此刻验证码刚创建，60s 限频窗口内）。
	if _, _, err := env.reg.SendCode(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := registerWithPhone(ctx, env.reg, phone, env.sent[0], "regthr", "pass-123"); err != nil {
		t.Fatalf("register: %v", err)
	}
	// 限频窗口内再发码：必须是"已注册"（ErrValidation），不是限频。
	_, _, err := env.reg.SendCode(ctx, phone)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("registered phone inside throttle window: want ErrValidation, got %v", err)
	}
	if errors.Is(err, ErrSmsThrottled) {
		t.Fatal("registered-phone rejection must not be masked by sms throttle")
	}
	if len(env.sent) != 1 {
		t.Fatalf("no new code should be delivered, got %v", env.sent)
	}
}

func TestRegisterHappyPath(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	if _, _, err := env.reg.SendCode(ctx, "13900000001"); err != nil {
		t.Fatal(err)
	}
	token, _, user, err := registerWithPhone(ctx, env.reg, "13900000001", env.sent[0], "newbie", "pass-123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if token != "tok" {
		t.Fatalf("token = %q", token)
	}
	if user.GlobalRole != model.GlobalRoleStudent {
		t.Fatalf("role = %v, want student（禁止自选角色）", user.GlobalRole)
	}
	// 建号可立即登录使用（用户名 + 密码）。
	if _, err := env.users.FindByUsername(ctx, "newbie"); err != nil {
		t.Fatalf("created user not found: %v", err)
	}
	// 验证码已核销：同码再注册（换用户名）必须失败。
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000001", env.sent[0], "another", "pass-123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("reused code: want ErrValidation, got %v", err)
	}
	if _, err := env.users.FindByUsername(ctx, "another"); err == nil {
		t.Fatal("second user must not be created")
	}
}

func TestRegisterWrongOrExpiredCode(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	if _, _, err := env.reg.SendCode(ctx, "13900000002"); err != nil {
		t.Fatal(err)
	}
	// 错误验证码。
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000002", "000000", "user1", "pass-123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("wrong code: want ErrValidation, got %v", err)
	}
	// 正确验证码但已过期。
	env.now = env.now.Add(smsCodeTTL + time.Second)
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000002", env.sent[0], "user1", "pass-123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("expired code: want ErrValidation, got %v", err)
	}
	// 从未发码的手机号。
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000003", "123456", "user1", "pass-123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("no code sent: want ErrValidation, got %v", err)
	}
	// 错误验证码不消耗核销：过期前用正确码仍可注册。
	env2 := newRegEnv(t)
	if _, _, err := env2.reg.SendCode(ctx, "13900000004"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := registerWithPhone(ctx, env2.reg, "13900000004", "999999", "user2", "pass-123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("wrong code attempt: %v", err)
	}
	if _, _, _, err := registerWithPhone(ctx, env2.reg, "13900000004", env2.sent[0], "user2", "pass-123"); err != nil {
		t.Fatalf("correct code after wrong attempt: %v", err)
	}
}

func TestRegisterDuplicatesAndValidation(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	// 既有用户名与既有手机号。
	if err := env.users.Create(ctx, &model.User{Username: "existing", PasswordHash: "x", Phone: "13900000005"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.reg.SendCode(ctx, "13900000006"); err != nil {
		t.Fatal(err)
	}
	code := env.sent[0]
	// 重复用户名。
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000006", code, "existing", "pass-123"); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "username") {
		t.Fatalf("dup username: %v", err)
	}
	// 重复手机号走竞态路径：发码在前、他人注册该号在后（SendCode 会拒绝
	// 已注册手机号，无法走正常流程），直接落一条验证码模拟该窗口。
	if err := env.codes.Create(ctx, &model.SmsCode{
		Phone: "13900000005", Code: "654321", ExpiresAt: env.now.Add(smsCodeTTL),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000005", "654321", "fresh-name", "pass-123"); !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "phone") {
		t.Fatalf("dup phone: %v", err)
	}
	// 参数校验：用户名过短 / 密码过短 / 验证码位数。
	env.now = env.now.Add(61 * time.Second)
	if _, _, err := env.reg.SendCode(ctx, "13900000006"); err != nil {
		t.Fatal(err)
	}
	code = env.sent[len(env.sent)-1]
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000006", code, "ab", "pass-123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("short username: %v", err)
	}
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000006", code, "okname", "12345"); !errors.Is(err, ErrValidation) {
		t.Fatalf("short password: %v", err)
	}
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000006", "12345", "okname", "pass-123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("5-digit code: %v", err)
	}
}

func TestGenCodeShape(t *testing.T) {
	for i := 0; i < 50; i++ {
		code, err := genCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 6 {
			t.Fatalf("code %q: want 6 digits", code)
		}
		for _, c := range code {
			if c < '0' || c > '9' {
				t.Fatalf("code %q: non-digit", code)
			}
		}
	}
}

// TestSendCodeReturnsDeliveredCode 回显模式的取值正确性：SendCode 返回的
// code 必须与投递器收到的一致（handler 仅在本地演示回显开启时原样带给前端）。
func TestSendCodeReturnsDeliveredCode(t *testing.T) {
	env := newRegEnv(t)
	code, _, err := env.reg.SendCode(context.Background(), "13800000009")
	if err != nil {
		t.Fatal(err)
	}
	if len(env.sent) != 1 || code != env.sent[0] {
		t.Fatalf("returned code %q != delivered %v", code, env.sent)
	}
	// 限频路径不回显（retry_after 携带等待秒数，code 为空）。
	empty, retry, err := env.reg.SendCode(context.Background(), "13800000009")
	if !errors.Is(err, ErrSmsThrottled) || empty != "" || retry <= 0 {
		t.Fatalf("throttled send: want empty code + retry_after, got (%q, %d, %v)", empty, retry, err)
	}
}

func TestSendEmailCodeValidationAndRegistered(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	for _, email := range []string{"", "not-an-email", "a@", "Name <a@example.com>", "a b@example.com"} {
		if _, _, err := env.reg.SendEmailCode(ctx, email); !errors.Is(err, ErrValidation) {
			t.Fatalf("email %q: want ErrValidation, got %v", email, err)
		}
	}
	if err := env.users.Create(ctx, &model.User{Username: "mail-taken", PasswordHash: "x", Email: "taken@example.com"}); err != nil {
		t.Fatal(err)
	}
	// 大小写归一后仍能识别已注册邮箱。
	if _, _, err := env.reg.SendEmailCode(ctx, " Taken@Example.COM "); !errors.Is(err, ErrValidation) {
		t.Fatalf("registered email: want ErrValidation, got %v", err)
	}
	if len(env.emailSent) != 0 {
		t.Fatalf("no email should be delivered, got %v", env.emailSent)
	}
}

func TestSendEmailCodeThrottleIsPerEmail(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	first, _, err := env.reg.SendEmailCode(ctx, "First@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(env.emailSent) != 1 || env.emailSent[0].to != "first@example.com" || env.emailSent[0].code != first {
		t.Fatalf("delivery = %+v, code = %q", env.emailSent, first)
	}
	if env.emailSent[0].purpose != model.SmsPurposeRegister {
		t.Fatalf("purpose = %q", env.emailSent[0].purpose)
	}
	// 同邮箱（大小写不同）仍受限频；其他邮箱不受影响。
	if _, _, err := env.reg.SendEmailCode(ctx, "first@example.com"); !errors.Is(err, ErrSmsThrottled) {
		t.Fatalf("same email throttle: %v", err)
	}
	if _, _, err := env.reg.SendEmailCode(ctx, "second@example.com"); err != nil {
		t.Fatalf("different email should not be throttled: %v", err)
	}
}

func TestRegisterWithEmailHappyPath(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	code, _, err := env.reg.SendEmailCode(ctx, " Newbie@Example.com ")
	if err != nil {
		t.Fatal(err)
	}
	token, _, user, err := env.reg.Register(ctx, RegisterInput{
		Email: "newbie@example.com", Code: code, Username: "mail-newbie", Password: "pass-123",
	}, issueTokenStub)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if token != "tok" || user.GlobalRole != model.GlobalRoleStudent {
		t.Fatalf("token/role = %q/%v", token, user.GlobalRole)
	}
	if user.Email != "newbie@example.com" || user.Phone != "" {
		t.Fatalf("stored identity = email %q phone %q", user.Email, user.Phone)
	}
	if _, err := env.users.FindByEmail(ctx, "newbie@example.com"); err != nil {
		t.Fatalf("created user not found by email: %v", err)
	}
	// 验证码一次性：同码换用户名不能再注册。
	if _, _, _, err := env.reg.Register(ctx, RegisterInput{
		Email: "newbie@example.com", Code: code, Username: "another", Password: "pass-123",
	}, issueTokenStub); !errors.Is(err, ErrValidation) {
		t.Fatalf("reused email code: want ErrValidation, got %v", err)
	}
}

func TestRegisterEmailChannelIsolation(t *testing.T) {
	env := newRegEnv(t)
	ctx := context.Background()
	// 邮箱注册码不能用于手机号注册（目标与 code 渠道必须一致）。
	code, _, err := env.reg.SendEmailCode(ctx, "isolated@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := registerWithPhone(ctx, env.reg, "13900000099", code, "iso-user", "pass-123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("email code used for phone: want ErrValidation, got %v", err)
	}
	// 邮箱与手机号同时填写必须拒绝（避免验证码渠道歧义）。
	if _, _, _, err := env.reg.Register(ctx, RegisterInput{
		Email: "isolated@example.com", Phone: "13900000099", Code: code,
		Username: "iso-user", Password: "pass-123",
	}, issueTokenStub); !errors.Is(err, ErrValidation) {
		t.Fatalf("both identifiers: want ErrValidation, got %v", err)
	}
}
