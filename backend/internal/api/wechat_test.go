package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

func newWechatAPIEnv(t *testing.T) (*Router, *service.AuthService) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:wechatapi-"+time.Now().Format("150405.000000000")+"?mode=memory&cache=shared"), &gorm.Config{})
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
	users := repo.NewUserRepo(gdb)
	auth := service.NewAuthService(users, "api-test-secret")
	if err := auth.EnsureAdmin(context.Background(), "admin", "api-admin-pass"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	oauth, err := service.NewWechatOAuthClient(service.WechatOAuthConfig{
		Mock: true, RedirectURL: "http://127.0.0.1:15173/login",
	})
	if err != nil {
		t.Fatalf("wechat client: %v", err)
	}
	wechat := service.NewWechatAuthService(users, oauth, []byte("api-test-secret"), "admin")
	router := NewRouter(auth)
	NewHandlers(auth, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, wechat, nil, nil, nil, false).Register(router)
	return router, auth
}

// TestWechatHTTPFlow status → start（state Cookie）→ callback（JWT）→ /me。
func TestWechatHTTPFlow(t *testing.T) {
	router, _ := newWechatAPIEnv(t)

	// 旧注册入口必须已下线（不在路由表中，返回 404，不得再建号）。
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/auth/register"},
		{http.MethodPost, "/api/v1/auth/email/code"},
		{http.MethodPost, "/api/v1/auth/sms/code"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404 (legacy registration must be removed)", tc.method, tc.path, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/wechat/status", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var status struct {
		Enabled   bool `json:"enabled"`
		Mock      bool `json:"mock"`
		MockAdmin bool `json:"mock_admin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Enabled || !status.Mock || !status.MockAdmin {
		t.Fatalf("status = %+v, want enabled mock with mock_admin", status)
	}

	// start：返回微信授权地址 + 两个 HttpOnly Cookie（state / mock identity）。
	req = httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/wechat/start?redirect=%2Ftraining&mock_id=api-openid", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d: %s", rec.Code, rec.Body.String())
	}
	var start struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("authorize url: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" || u.Query().Get("code") != "mock" {
		t.Fatalf("mock authorize url = %s", start.AuthorizeURL)
	}
	var stateCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == service.WechatStateCookieName {
			stateCookie = cookie
		}
		if !cookie.HttpOnly {
			t.Fatalf("cookie %s must be HttpOnly", cookie.Name)
		}
	}
	if stateCookie == nil {
		t.Fatal("missing state cookie")
	}

	body := strings.NewReader(`{"code":"mock","state":"` + state + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/wechat/callback", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(stateCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("callback = %d: %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Token    string `json:"token"`
		Redirect string `json:"redirect"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatalf("decode callback: %v", err)
	}
	if login.Token == "" || login.Redirect != "/training" {
		t.Fatalf("callback = %+v", login)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d: %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Username    string `json:"username"`
		PasswordSet bool   `json:"password_set"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.Username == "" || me.PasswordSet {
		t.Fatalf("me = %+v, want username and password_set=false", me)
	}
}

// TestWechatMockAdminHTTPFlow Mock 管理员入口：start?as=admin 回调后直接签发
// 配置的 admin 账号 token；未知 as 值必须 422。
func TestWechatMockAdminHTTPFlow(t *testing.T) {
	router, _ := newWechatAPIEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/wechat/start?as=student", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("start with unknown as = %d, want 422", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/wechat/start?as=admin&redirect=%2Fadmin", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start mock admin = %d: %s", rec.Code, rec.Body.String())
	}
	var start struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	u, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("authorize url: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatalf("mock admin authorize url = %s", start.AuthorizeURL)
	}
	var stateCookie *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == service.WechatStateCookieName {
			stateCookie = cookie
		}
	}
	if stateCookie == nil {
		t.Fatal("missing state cookie")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/wechat/callback",
		strings.NewReader(`{"code":"mock","state":"`+state+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(stateCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("callback mock admin = %d: %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Token    string `json:"token"`
		Redirect string `json:"redirect"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatalf("decode callback: %v", err)
	}
	if login.Token == "" || login.Redirect != "/admin" {
		t.Fatalf("callback = %+v", login)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me = %d: %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Username   string `json:"username"`
		GlobalRole string `json:"global_role"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.Username != "admin" || me.GlobalRole != "admin" {
		t.Fatalf("me = %+v, want seeded admin account", me)
	}
}

func TestWechatCallbackRequiresStateCookie(t *testing.T) {
	router, _ := newWechatAPIEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/wechat/callback",
		strings.NewReader(`{"code":"mock","state":"x"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("callback without state cookie = %d, want 401", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if !strings.Contains(env.Error.Message, "微信登录状态已失效") {
		t.Fatalf("error message = %q", env.Error.Message)
	}
}
