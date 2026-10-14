package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 账号生命周期（登出撤销 / 改密失效 / admin 重置）。
// 撤销走 token_revocations 表 + 失效阈值，不进事件表（鉴权状态非派生数据）。

func newAuthLifecycleEnv(t *testing.T) (*AuthService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:authlifecycle?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	svc := NewAuthService(repo.NewUserRepo(gdb), "lifecycle-test-secret")
	svc.SetRevocationRepo(repo.NewTokenRevocationRepo(gdb))
	ctx := context.Background()
	if _, err := svc.CreateUser(ctx, "alice", "old-pass", model.GlobalRoleStudent); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, "root", "root-pass", model.GlobalRoleAdmin); err != nil {
		t.Fatal(err)
	}
	return svc, gdb
}

func TestLogoutRevokesToken(t *testing.T) {
	svc, gdb := newAuthLifecycleEnv(t)
	ctx := context.Background()
	token, _, err := svc.Login(ctx, "alice", "old-pass")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(ctx, token); err != nil {
		t.Fatalf("fresh token should validate: %v", err)
	}
	if err := svc.Logout(ctx, 1, token); err != nil {
		t.Fatal(err)
	}
	// jti 已入撤销表。
	var n int64
	gdb.Model(&model.TokenRevocation{}).Where("user_id = ?", 1).Count(&n)
	if n != 1 {
		t.Fatalf("revocation rows = %d, want 1", n)
	}
	if _, err := svc.ValidateToken(ctx, token); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("err = %v, want ErrTokenRevoked", err)
	}
}

func TestLogoutDoesNotAffectOtherTokens(t *testing.T) {
	svc, _ := newAuthLifecycleEnv(t)
	ctx := context.Background()
	t1, _, _ := svc.Login(ctx, "alice", "old-pass")
	t2, _, _ := svc.Login(ctx, "alice", "old-pass")
	if err := svc.Logout(ctx, 1, t1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(ctx, t2); err != nil {
		t.Fatalf("other token should stay valid: %v", err)
	}
}

func TestChangePasswordInvalidatesAllTokens(t *testing.T) {
	svc, gdb := newAuthLifecycleEnv(t)
	ctx := context.Background()
	old1, _, _ := svc.Login(ctx, "alice", "old-pass")
	old2, _, _ := svc.Login(ctx, "alice", "old-pass")
	if err := svc.ChangePassword(ctx, 1, "old-pass", "new-pass"); err != nil {
		t.Fatal(err)
	}
	// 全部旧 token 失效（阈值判定，与 jti 无关）。
	for i, tok := range []string{old1, old2} {
		if _, err := svc.ValidateToken(ctx, tok); !errors.Is(err, ErrTokenRevoked) {
			t.Fatalf("old token %d err = %v, want ErrTokenRevoked", i, err)
		}
	}
	// 新密码可登录；阈值含 +1s 窗口（iat 秒精度取舍，见 ValidateToken 注释），
	// 等窗口滑过后新 token 有效。
	time.Sleep(1100 * time.Millisecond)
	fresh, _, err := svc.Login(ctx, "alice", "new-pass")
	if err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, fresh); err != nil {
		t.Fatalf("fresh token should validate: %v", err)
	}
	// 旧密码失效。
	if _, _, err := svc.Login(ctx, "alice", "old-pass"); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("old password err = %v, want ErrInvalidCredential", err)
	}
	// 失效阈值已落库。
	var user model.User
	if err := gdb.Where("username = ?", "alice").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.TokenInvalidBefore.IsZero() {
		t.Fatal("token_invalid_before should be set")
	}
}

func TestChangePasswordWrongCurrentRejected(t *testing.T) {
	svc, _ := newAuthLifecycleEnv(t)
	ctx := context.Background()
	if err := svc.ChangePassword(ctx, 1, "wrong", "new-pass"); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("err = %v, want ErrInvalidCredential", err)
	}
}

func TestChangePasswordTooShortRejected(t *testing.T) {
	svc, _ := newAuthLifecycleEnv(t)
	ctx := context.Background()
	if err := svc.ChangePassword(ctx, 1, "old-pass", "123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestAdminResetPasswordInvalidatesTargetTokens(t *testing.T) {
	svc, _ := newAuthLifecycleEnv(t)
	ctx := context.Background()
	tok, _, _ := svc.Login(ctx, "alice", "old-pass")
	if err := svc.ResetPasswordByAdmin(ctx, 1, "reset-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(ctx, tok); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("err = %v, want ErrTokenRevoked", err)
	}
	if _, _, err := svc.Login(ctx, "alice", "reset-pass"); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
}

func TestAdminResetPasswordValidation(t *testing.T) {
	svc, _ := newAuthLifecycleEnv(t)
	ctx := context.Background()
	if err := svc.ResetPasswordByAdmin(ctx, 1, "123"); !errors.Is(err, ErrValidation) {
		t.Fatalf("short password err = %v, want ErrValidation", err)
	}
	if err := svc.ResetPasswordByAdmin(ctx, 9999, "long-enough"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("unknown user err = %v, want ErrRecordNotFound", err)
	}
}

// ValidateToken 阈值语义：签发时间等于阈值时刻的 token 仍有效（>= 阈值），
// 早于阈值的拒绝——防止改密瞬间的新登录被误杀。
func TestValidateTokenThresholdSemantics(t *testing.T) {
	svc, gdb := newAuthLifecycleEnv(t)
	ctx := context.Background()
	if err := svc.ChangePassword(ctx, 1, "old-pass", "new-pass"); err != nil {
		t.Fatal(err)
	}
	var user model.User
	if err := gdb.Where("username = ?", "alice").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	// 直接推进阈值到未来，当前签发的 token（iat=now < 阈值）应被拒。
	future := time.Now().Add(time.Minute)
	if err := gdb.Model(&model.User{}).Where("id = ?", user.ID).
		Update("token_invalid_before", future).Error; err != nil {
		t.Fatal(err)
	}
	tok, _, _ := svc.Login(ctx, "alice", "new-pass")
	if _, err := svc.ValidateToken(ctx, tok); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("err = %v, want ErrTokenRevoked", err)
	}
}
// TestChangePasswordImmediateRelogin 锁回归：改密阈值按「签发秒 + 1」存储，
// iat 秒精度下改密后 1 秒内的新登录曾被拒（用户改完密码立刻用新密码登录
// 反而失败，E2E 账号生命周期旅程抓到）。issueToken 签发前等待出窗修复。
func TestChangePasswordImmediateRelogin(t *testing.T) {
	svc, _ := newAuthLifecycleEnv(t)
	ctx := context.Background()
	if err := svc.ChangePassword(ctx, 1, "old-pass", "new-pass"); err != nil {
		t.Fatal(err)
	}
	// 改密后立即重登：新 token 必须通过校验（等待出窗后 iat >= 阈值）。
	tok, _, err := svc.Login(ctx, "alice", "new-pass")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateToken(ctx, tok); err != nil {
		t.Fatalf("immediate re-login token rejected: %v", err)
	}
}
