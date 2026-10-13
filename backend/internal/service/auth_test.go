package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func newAuthTestEnv(t *testing.T) *AuthService {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:authtest?mode=memory&cache=shared"), &gorm.Config{})
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
	return NewAuthService(repo.NewUserRepo(gdb), "test-secret")
}

func TestCreateUserBcryptHash(t *testing.T) {
	auth := newAuthTestEnv(t)
	user, err := auth.CreateUser(context.Background(), "alice", "secret123", model.GlobalRoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	if user.GlobalRole != model.GlobalRoleStudent {
		t.Fatalf("role = %s, want student", user.GlobalRole)
	}
	if user.PasswordHash == "secret123" {
		t.Fatal("password must not be stored in plaintext")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("secret123")) != nil {
		t.Fatal("stored hash must verify against original password")
	}
}

func TestCreateUserDefaultRole(t *testing.T) {
	auth := newAuthTestEnv(t)
	user, err := auth.CreateUser(context.Background(), "bob", "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	if user.GlobalRole != model.GlobalRoleStudent {
		t.Fatalf("default role = %s, want student", user.GlobalRole)
	}
}

func TestCreateUserRejectsTeacher(t *testing.T) {
	auth := newAuthTestEnv(t)
	// teacher 经 Normalize 降为 student 后仍可创建（兼容旧入参）；显式非法值同理。
	user, err := auth.CreateUser(context.Background(), "legacy", "pw", model.GlobalRoleTeacher)
	if err != nil {
		t.Fatal(err)
	}
	if user.GlobalRole != model.GlobalRoleStudent {
		t.Fatalf("teacher input must normalize to student, got %s", user.GlobalRole)
	}
}

func TestCreateUserDuplicateRejected(t *testing.T) {
	auth := newAuthTestEnv(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, "carol", "pw", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateUser(ctx, "carol", "other", ""); err == nil {
		t.Fatal("duplicate username must be rejected")
	}
}

func TestUpdateRolesTwoTierOnly(t *testing.T) {
	auth := newAuthTestEnv(t)
	ctx := context.Background()
	user, err := auth.CreateUser(ctx, "dave", "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := auth.UpdateRoles(ctx, user.ID, model.GlobalRoleAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.GlobalRole != model.GlobalRoleAdmin {
		t.Fatalf("global_role = %s, want admin", updated.GlobalRole)
	}
	updated, err = auth.UpdateRoles(ctx, user.ID, model.GlobalRoleStudent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.GlobalRole != model.GlobalRoleStudent {
		t.Fatalf("global_role = %s, want student", updated.GlobalRole)
	}
}

func TestCheckRoleTwoTier(t *testing.T) {
	auth := newAuthTestEnv(t)
	ctx := context.Background()
	student, _ := auth.CreateUser(ctx, "eve", "pw", model.GlobalRoleStudent)
	admin, _ := auth.CreateUser(ctx, "root", "pw", model.GlobalRoleAdmin)

	ok, err := auth.CheckRole(ctx, student, model.GlobalRoleAdmin, "")
	if err != nil || ok {
		t.Fatalf("student must not pass admin check, ok=%v err=%v", ok, err)
	}
	ok, err = auth.CheckRole(ctx, admin, model.GlobalRoleAdmin, "")
	if err != nil || !ok {
		t.Fatalf("admin must pass, ok=%v err=%v", ok, err)
	}
	ok, err = auth.CheckRole(ctx, student, model.GlobalRoleStudent, "")
	if err != nil || !ok {
		t.Fatalf("student must pass student check, ok=%v err=%v", ok, err)
	}
	// 遗留 teacher 角色按普通人处理。
	legacy := &model.User{Username: "old", GlobalRole: model.GlobalRoleTeacher}
	ok, err = auth.CheckRole(ctx, legacy, model.GlobalRoleAdmin, "")
	if err != nil || ok {
		t.Fatalf("legacy teacher must not be admin, ok=%v err=%v", ok, err)
	}
}
