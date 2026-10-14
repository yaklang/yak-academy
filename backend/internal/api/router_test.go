package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

// 声明式鉴权中间件测试：JWT 解析 → DB 加载用户 → 两档 Rank 判定（rbac-model skill）。

func newRouterTestEnv(t *testing.T) (*Router, *service.AuthService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:apiroutertest?mode=memory&cache=shared"), &gorm.Config{})
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
	auth := service.NewAuthService(repo.NewUserRepo(gdb), "test-secret")
	router := NewRouter(auth)
	// 探针路由：登录即可 / 仅管理员，写回用户名验证 ctx 注入。
	router.Handle(http.MethodGet, "/api/v1/whoami", "", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(UserFrom(req.Context()).Username))
	})
	router.Handle(http.MethodGet, "/api/v1/admin-probe", "admin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return router, auth, gdb
}

func authedGet(router *Router, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRouterRejectsMissingAndForgedToken(t *testing.T) {
	router, _, _ := newRouterTestEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/whoami", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}
	if rec := authedGet(router, "/api/v1/whoami", "forged-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged token: status = %d, want 401", rec.Code)
	}
}

func TestRouterStudentBlockedFromAdminRoutes(t *testing.T) {
	router, auth, _ := newRouterTestEnv(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, "student1", "pw", model.GlobalRoleStudent); err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.Login(ctx, "student1", "pw")
	if err != nil {
		t.Fatal(err)
	}
	rec := authedGet(router, "/api/v1/admin-probe", token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("student on admin route: status = %d, want 403", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "FORBIDDEN" {
		t.Fatalf("code = %q, want FORBIDDEN（禁止 404 掩盖权限问题）", env.Error.Code)
	}
	// 登录即可的路由通过，且 ctx 注入了真实用户。
	rec = authedGet(router, "/api/v1/whoami", token)
	if rec.Code != http.StatusOK || rec.Body.String() != "student1" {
		t.Fatalf("whoami = %d %q, want 200 student1", rec.Code, rec.Body.String())
	}
}

func TestRouterAdminPassesAndLegacyTeacherDemoted(t *testing.T) {
	router, auth, gdb := newRouterTestEnv(t)
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, "root", "pw", model.GlobalRoleAdmin); err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := auth.Login(ctx, "root", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if rec := authedGet(router, "/api/v1/admin-probe", adminToken); rec.Code != http.StatusOK {
		t.Fatalf("admin: status = %d, want 200", rec.Code)
	}
	// 历史 teacher 角色直写 DB（绕过 Normalize），鉴权必须按普通人处理 → 403。
	hash, err := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.NewUserRepo(gdb).Create(ctx, &model.User{
		Username: "legacy-teacher", PasswordHash: string(hash), GlobalRole: model.GlobalRoleTeacher,
	}); err != nil {
		t.Fatal(err)
	}
	teacherToken, _, err := auth.Login(ctx, "legacy-teacher", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if rec := authedGet(router, "/api/v1/admin-probe", teacherToken); rec.Code != http.StatusForbidden {
		t.Fatalf("legacy teacher: status = %d, want 403", rec.Code)
	}
}
