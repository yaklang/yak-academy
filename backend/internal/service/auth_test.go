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
	updated, err := auth.UpdateRoles(ctx, user.ID, model.GlobalRoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if updated.GlobalRole != model.GlobalRoleAdmin {
		t.Fatalf("global_role = %s, want admin", updated.GlobalRole)
	}
	updated, err = auth.UpdateRoles(ctx, user.ID, model.GlobalRoleStudent)
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

	// 两档 Rank 判定（中间件 authorize 的同一套逻辑，rbac-model skill）。
	if student.GlobalRole.Normalize().Rank() >= model.GlobalRoleAdmin.Rank() {
		t.Fatal("student must not reach admin rank")
	}
	if admin.GlobalRole.Rank() < model.GlobalRoleAdmin.Rank() {
		t.Fatal("admin must reach admin rank")
	}
	// 遗留 teacher 角色按普通人处理。
	legacy := &model.User{Username: "old", GlobalRole: model.GlobalRoleTeacher}
	if legacy.GlobalRole.Normalize().Rank() >= model.GlobalRoleAdmin.Rank() {
		t.Fatal("legacy teacher must normalize below admin")
	}
}
