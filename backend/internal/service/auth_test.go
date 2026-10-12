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
	// 密码必须是 bcrypt 哈希而非明文。
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

func TestUpdateRolesPersists(t *testing.T) {
	auth := newAuthTestEnv(t)
	ctx := context.Background()
	user, err := auth.CreateUser(ctx, "dave", "pw", "")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := auth.UpdateRoles(ctx, user.ID, model.GlobalRoleTeacher, []model.CourseRole{
		{TopicSlug: "web-security", Role: "teacher"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GlobalRole != model.GlobalRoleTeacher {
		t.Fatalf("global_role = %s, want teacher", updated.GlobalRole)
	}
	roles, err := auth.ListCourseRolesOf(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].TopicSlug != "web-security" || roles[0].Role != "teacher" {
		t.Fatalf("course roles = %+v", roles)
	}
	// 删除课程级角色。
	if err := auth.DeleteCourseRole(ctx, user.ID, "web-security"); err != nil {
		t.Fatal(err)
	}
	roles, _ = auth.ListCourseRolesOf(ctx, user.ID)
	if len(roles) != 0 {
		t.Fatalf("course roles after delete = %+v", roles)
	}
}

// TestCheckRoleTwoLayer 双层 RBAC 判定（rbac-model skill：全局 → 课程级）。
func TestCheckRoleTwoLayer(t *testing.T) {
	auth := newAuthTestEnv(t)
	ctx := context.Background()
	// student 用户，在 topic-a 拥有课程级 teacher。
	student, _ := auth.CreateUser(ctx, "eve", "pw", model.GlobalRoleStudent)
	if _, err := auth.UpdateRoles(ctx, student.ID, "", []model.CourseRole{
		{TopicSlug: "topic-a", Role: "teacher"},
	}); err != nil {
		t.Fatal(err)
	}
	teacher, _ := auth.CreateUser(ctx, "frank", "pw", model.GlobalRoleTeacher)

	// 1. 全局达标短路：teacher 全域通过（无 topicSlug 也行）。
	ok, err := auth.CheckRole(ctx, teacher, model.GlobalRoleTeacher, "")
	if err != nil || !ok {
		t.Fatalf("global teacher should pass, ok=%v err=%v", ok, err)
	}
	// 2. 全局不足 + 课程级命中：student 在 topic-a 放行。
	ok, err = auth.CheckRole(ctx, student, model.GlobalRoleTeacher, "topic-a")
	if err != nil || !ok {
		t.Fatalf("course-level teacher on topic-a should pass, ok=%v err=%v", ok, err)
	}
	// 3. 全局不足 + 课程级未命中：student 在 topic-b 拒绝。
	ok, err = auth.CheckRole(ctx, student, model.GlobalRoleTeacher, "topic-b")
	if err != nil || ok {
		t.Fatalf("no course role on topic-b must fail, ok=%v err=%v", ok, err)
	}
	// 4. 全局不足且无课程域：拒绝。
	ok, err = auth.CheckRole(ctx, student, model.GlobalRoleTeacher, "")
	if err != nil || ok {
		t.Fatalf("student without topic domain must fail, ok=%v err=%v", ok, err)
	}
	// 5. admin 短路通过全部检查。
	admin, _ := auth.CreateUser(ctx, "root", "pw", model.GlobalRoleAdmin)
	ok, err = auth.CheckRole(ctx, admin, model.GlobalRoleAdmin, "")
	if err != nil || !ok {
		t.Fatalf("admin must short-circuit, ok=%v err=%v", ok, err)
	}
}