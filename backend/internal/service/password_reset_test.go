package service

// 找回密码（手机验证码重置）域测试：真实 SQLite 仓库。
// 覆盖 发码限频与注册同策略、未注册手机号拒绝、验证码核销一次性/过期/错误、
// 用途隔离（注册码不可重置、重置码不可注册）、成功重置后新密码可用且
// token 失效阈值推进（旧 token 全量吊销语义）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

type resetEnv struct {
	reset *PasswordResetService
	reg   *RegisterService
	users repo.UserRepo
	codes repo.SmsCodeRepo
	sent  []string // 投递探针记录已发验证码
	now   time.Time
}

func newResetEnv(t *testing.T) *resetEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:pwdresetenv?mode=memory&cache=shared"), &gorm.Config{})
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
	env := &resetEnv{
		users: repo.NewUserRepo(gdb),
		codes: repo.NewSmsCodeRepo(gdb),
		now:   time.Now(),
	}
	sender := senderFunc(func(phone, code string) error { env.sent = append(env.sent, code); return nil })
	env.reset = NewPasswordResetService(env.users, env.codes, sender)
	env.reset.now = func() time.Time { return env.now }
	env.reg = NewRegisterService(env.users, env.codes, sender)
	env.reg.now = func() time.Time { return env.now }
	return env
}

// ensurePhoneUser 建一个绑定手机号的既有账号（学号式账号编码由 userRepo.Create 分配）。
func (e *resetEnv) ensurePhoneUser(t *testing.T, username, phone string) *model.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("old-pass-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u := &model.User{Username: username, PasswordHash: string(hash), GlobalRole: model.GlobalRoleStudent, Phone: phone}
	if err := e.users.Create(context.Background(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func TestPasswordResetHappyPath(t *testing.T) {
	env := newResetEnv(t)
	ctx := context.Background()
	user := env.ensurePhoneUser(t, "forgetter", "13900000001")

	code, _, err := env.reset.SendResetCode(ctx, "13900000001")
	if err != nil {
		t.Fatalf("send reset code: %v", err)
	}
	if len(env.sent) != 1 || env.sent[0] != code {
		t.Fatalf("sender probe want delivered code %q, got %v", code, env.sent)
	}
	got, err := env.reset.ResetPassword(ctx, "13900000001", code, "new-pass-456")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if got.Username != "forgetter" || got.AccountCode != user.AccountCode {
		t.Fatalf("returned identity = (%s, %s), want (forgetter, %s)", got.Username, got.AccountCode, user.AccountCode)
	}
	// 状态断言查库：新密码可登录（bcrypt 比对）、旧密码失效、token 失效阈值已推进。
	fresh, err := env.users.FindByUsername(ctx, "forgetter")
	if err != nil {
		t.Fatalf("refind: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(fresh.PasswordHash), []byte("new-pass-456")) != nil {
		t.Fatal("new password should verify")
	}
	if bcrypt.CompareHashAndPassword([]byte(fresh.PasswordHash), []byte("old-pass-123")) == nil {
		t.Fatal("old password should be rejected")
	}
	if fresh.TokenInvalidBefore.IsZero() || !fresh.TokenInvalidBefore.After(env.now.Add(-time.Second)) {
		t.Fatalf("token invalid-before threshold not advanced: %v", fresh.TokenInvalidBefore)
	}
}

func TestPasswordResetCodeRejections(t *testing.T) {
	env := newResetEnv(t)
	ctx := context.Background()
	env.ensurePhoneUser(t, "u-reject", "13900000002")

	code, _, err := env.reset.SendResetCode(ctx, "13900000002")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	// 错误验证码。
	if _, err := env.reset.ResetPassword(ctx, "13900000002", "000000", "new-pass-456"); !errors.Is(err, ErrValidation) {
		t.Fatalf("wrong code: want ErrValidation, got %v", err)
	}
	// 成功核销一次。
	if _, err := env.reset.ResetPassword(ctx, "13900000002", code, "new-pass-456"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	// 同码重放（已核销）。
	if _, err := env.reset.ResetPassword(ctx, "13900000002", code, "again-789"); !errors.Is(err, ErrValidation) {
		t.Fatalf("replay: want ErrValidation, got %v", err)
	}
	// 过期：重新发一码后把时钟推过 TTL。
	env.now = env.now.Add(61 * time.Second)
	code2, _, err := env.reset.SendResetCode(ctx, "13900000002")
	if err != nil {
		t.Fatalf("send2: %v", err)
	}
	env.now = env.now.Add(6 * time.Minute)
	if _, err := env.reset.ResetPassword(ctx, "13900000002", code2, "again-789"); !errors.Is(err, ErrValidation) {
		t.Fatalf("expired: want ErrValidation, got %v", err)
	}
	// 新密码过短。
	env.now = env.now.Add(61 * time.Second)
	code3, _, err := env.reset.SendResetCode(ctx, "13900000002")
	if err != nil {
		t.Fatalf("send3: %v", err)
	}
	if _, err := env.reset.ResetPassword(ctx, "13900000002", code3, "12345"); !errors.Is(err, ErrValidation) {
		t.Fatalf("short password: want ErrValidation, got %v", err)
	}
}

func TestPasswordResetUnregisteredPhone(t *testing.T) {
	env := newResetEnv(t)
	ctx := context.Background()
	// 未注册：发码即拒（先于限频）。
	if _, _, err := env.reset.SendResetCode(ctx, "13900000003"); !errors.Is(err, ErrValidation) {
		t.Fatalf("send to unregistered: want ErrValidation, got %v", err)
	}
	// 核销侧同样拒绝，且不消耗任何验证码。
	if _, err := env.reset.ResetPassword(ctx, "13900000003", "123456", "new-pass-456"); !errors.Is(err, ErrValidation) {
		t.Fatalf("reset unregistered: want ErrValidation, got %v", err)
	}
}

func TestPasswordResetThrottleSamePolicyAsRegister(t *testing.T) {
	env := newResetEnv(t)
	ctx := context.Background()
	env.ensurePhoneUser(t, "u-throttle", "13900000004")

	if _, _, err := env.reset.SendResetCode(ctx, "13900000004"); err != nil {
		t.Fatalf("first send: %v", err)
	}
	// 60 秒内重复：ErrSmsThrottled + 剩余秒数。
	_, retry, err := env.reset.SendResetCode(ctx, "13900000004")
	if !errors.Is(err, ErrSmsThrottled) || retry <= 0 {
		t.Fatalf("want throttled with retry_after, got (%d, %v)", retry, err)
	}
	// 出 60 秒但仍在 10 分钟窗口：第 2、3 条放行，第 4 条窗口拒绝。
	env.now = env.now.Add(61 * time.Second)
	if _, _, err := env.reset.SendResetCode(ctx, "13900000004"); err != nil {
		t.Fatalf("second send: %v", err)
	}
	env.now = env.now.Add(61 * time.Second)
	if _, _, err := env.reset.SendResetCode(ctx, "13900000004"); err != nil {
		t.Fatalf("third send: %v", err)
	}
	env.now = env.now.Add(61 * time.Second)
	if _, _, err := env.reset.SendResetCode(ctx, "13900000004"); !errors.Is(err, ErrSmsThrottled) {
		t.Fatalf("fourth send: want ErrSmsThrottled, got %v", err)
	}
}

// TestPasswordResetPurposeIsolation 验证码用途隔离：注册码不可用于重置、
// 重置码不可用于注册（LatestByPhone 按 purpose 过滤）。
func TestPasswordResetPurposeIsolation(t *testing.T) {
	env := newResetEnv(t)
	ctx := context.Background()

	// 重置码不可用于注册：已注册手机号发重置码后，拿它走注册（用户名撞车无关，
	// 断言在验证码核销一步就以「未发过注册码」拒绝）。
	env.ensurePhoneUser(t, "u-iso", "13900000005")
	resetCode, _, err := env.reset.SendResetCode(ctx, "13900000005")
	if err != nil {
		t.Fatalf("send reset code: %v", err)
	}
	if _, _, _, err := env.reg.Register(ctx, "13900000005", resetCode, "intruder-1", "pass-123456", issueTokenStub); !errors.Is(err, ErrValidation) {
		t.Fatalf("register with reset code: want ErrValidation, got %v", err)
	}

	// 注册码不可用于重置：未注册手机号走注册发码（注册发码要求未注册），
	// 拿注册码直接重置——手机号本就未注册，最先被「phone not registered」拒绝；
	// 补充注册成功后再试，仍被「未发过重置码」拒绝（purposes 不同表行）。
	regCode, _, err := env.reg.SendCode(ctx, "13900000006")
	if err != nil {
		t.Fatalf("send register code: %v", err)
	}
	if _, _, _, err := env.reg.Register(ctx, "13900000006", regCode, "iso-reg", "pass-123456", issueTokenStub); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := env.reset.ResetPassword(ctx, "13900000006", regCode, "hijack-123456"); !errors.Is(err, ErrValidation) {
		t.Fatalf("reset with register code: want ErrValidation, got %v", err)
	}
	// 注册与重置的限频窗口也按用途独立：紧接的重置发码不受 60s 注册发码限制。
	if _, _, err := env.reset.SendResetCode(ctx, "13900000006"); err != nil {
		t.Fatalf("reset send after register send (independent windows): %v", err)
	}
}
