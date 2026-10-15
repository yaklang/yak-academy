package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

// mapError 单元测试：内部错误 → 契约错误信封（api-contract skill 错误码表）。
// 501 用例回归防护：曾因重复 case 出现空分支，stub 错误被吞成空 200。

func TestMapErrorNotImplementedReturns501(t *testing.T) {
	rec := httptest.NewRecorder()
	mapError(rec, &service.NotImplementedError{What: "env driver"})
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "NOT_IMPLEMENTED" || env.Error.Message == "" {
		t.Fatalf("envelope = %+v, want NOT_IMPLEMENTED with message", env.Error)
	}
}

func TestMapErrorStatusCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		code string
	}{
		{"gorm record not found", gorm.ErrRecordNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"service not found", service.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"validation with reason", fmt.Errorf("%w: bad zip", service.ErrValidation), http.StatusUnprocessableEntity, "VALIDATION_FAILED"},
		{"has solve history", service.ErrHasSolves, http.StatusConflict, "CONFLICT"},
		{"unknown internal", errors.New("boom"), http.StatusInternalServerError, "INTERNAL"},
		{"login bad credential", service.ErrInvalidCredential, http.StatusUnauthorized, "UNAUTHORIZED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mapError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			var env errorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Error.Code != tc.code {
				t.Fatalf("code = %q, want %q", env.Error.Code, tc.code)
			}
		})
	}
}

// 撤销 token 走 authenticate 专路（"token revoked" 文案），不进 mapError——
// 这里直测 authenticate 的 401 信封。
func TestAuthenticateRevokedTokenReturns401(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:apirevokedtest?mode=memory&cache=shared"), &gorm.Config{})
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
	auth := service.NewAuthService(repo.NewUserRepo(gdb), "revoked-test-secret")
	auth.SetRevocationRepo(repo.NewTokenRevocationRepo(gdb))
	ctx := context.Background()
	if _, err := auth.CreateUser(ctx, "alice", "pass-123", model.GlobalRoleStudent); err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.Login(ctx, "alice", "pass-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, 1, token); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(auth)
	// 探测路由：任意已注册路由都会先过 authenticate，这里用 me 的路由形态。
	NewHandlers(auth, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).Register(router)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("code = %q, want UNAUTHORIZED", env.Error.Code)
	}
}

// health 端点：无鉴权静态响应（compose healthcheck 依赖，鉴权中间件不得拦截）。
func TestHealthEndpointPublic(t *testing.T) {
	router := NewRouter(nil)
	NewHandlers(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).Register(router)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Status != "ok" {
		t.Fatalf("body = %q err = %v", rec.Body.String(), err)
	}
}

func TestMapErrorInstanceConflictCarriesCurrent(t *testing.T) {
	rec := httptest.NewRecorder()
	mapError(rec, &service.InstanceConflictError{
		Current:        &model.LabInstance{ID: 7, ChallengeID: 3},
		Username:       "alice",
		ChallengeTitle: "SQLi basics",
		ChallengeSlug:  "sqli-basics",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var env conflictEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "INSTANCE_CONFLICT" {
		t.Fatalf("code = %q, want INSTANCE_CONFLICT", env.Error.Code)
	}
	if env.Current == nil || env.Current.ChallengeSlug != "sqli-basics" || env.Current.Username != "alice" {
		t.Fatalf("current = %+v, want enriched active instance", env.Current)
	}
}
