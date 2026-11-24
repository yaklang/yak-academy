package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

type wechatTestEnv struct {
	users  repo.UserRepo
	oauth  *WechatOAuthClient
	wechat *WechatAuthService
	auth   *AuthService
	now    time.Time
}

func newWechatTestEnv(t *testing.T, cfg WechatOAuthConfig) *wechatTestEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:wechattest-"+time.Now().Format("150405.000000000")+"?mode=memory&cache=shared"), &gorm.Config{})
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
	oauth, err := NewWechatOAuthClient(cfg)
	if err != nil {
		t.Fatalf("wechat client: %v", err)
	}
	env := &wechatTestEnv{
		users: repo.NewUserRepo(gdb), oauth: oauth,
		now: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC),
	}
	env.wechat = NewWechatAuthService(env.users, oauth, []byte("wechat-state-secret"))
	env.wechat.now = func() time.Time { return env.now }
	env.auth = NewAuthService(env.users, "test-secret")
	return env
}

func mockWechatConfig() WechatOAuthConfig {
	return WechatOAuthConfig{
		Mock: true, RedirectURL: "http://127.0.0.1:15173/login",
	}
}

// TestWechatAuthorizeURL 官方两种授权入口与 scope：公众号网页授权、开放平台扫码。
func TestWechatAuthorizeURL(t *testing.T) {
	official, err := NewWechatOAuthClient(WechatOAuthConfig{
		AppID: "wx-official", AppSecret: "secret",
		RedirectURL: "https://academy.example.com/login",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := official.AuthorizeURL("state-1")
	for _, want := range []string{
		"https://open.weixin.qq.com/connect/oauth2/authorize?",
		"appid=wx-official", "scope=snsapi_userinfo",
		"redirect_uri=https%3A%2F%2Facademy.example.com%2Flogin",
		"state=state-1", "#wechat_redirect",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("official authorize URL missing %q: %s", want, got)
		}
	}

	openPlatform, err := NewWechatOAuthClient(WechatOAuthConfig{
		AppID: "wx-open", AppSecret: "secret",
		RedirectURL: "https://academy.example.com/login",
		Mode:        "open_platform",
	})
	if err != nil {
		t.Fatal(err)
	}
	got = openPlatform.AuthorizeURL("state-2")
	if !strings.Contains(got, "https://open.weixin.qq.com/connect/qrconnect?") ||
		!strings.Contains(got, "scope=snsapi_login") {
		t.Fatalf("open platform authorize URL = %s", got)
	}
}

func TestWechatConfigValidation(t *testing.T) {
	if _, err := NewWechatOAuthClient(WechatOAuthConfig{AppID: "only-id"}); err == nil {
		t.Fatal("partial config must be rejected")
	}
	if _, err := NewWechatOAuthClient(WechatOAuthConfig{
		AppID: "id", AppSecret: "secret", RedirectURL: "not-a-url",
	}); err == nil {
		t.Fatal("relative redirect URL must be rejected")
	}
	disabled, err := NewWechatOAuthClient(WechatOAuthConfig{})
	if err != nil || disabled.Enabled() {
		t.Fatalf("empty config should be disabled without error: enabled=%v err=%v", disabled.Enabled(), err)
	}
}

// TestWechatMockFlowCreatesAndReusesUser 微信注册走完整 state 流程：同一微信
// 身份二次授权复用账号，不重复建号；另一个身份建新账号。
func TestWechatMockFlowCreatesAndReusesUser(t *testing.T) {
	env := newWechatTestEnv(t, mockWechatConfig())
	ctx := context.Background()

	start, err := env.wechat.Start("/training", "e2e-openid-1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(start.AuthorizeURL, "code=mock") ||
		!strings.Contains(start.AuthorizeURL, "state="+start.State) {
		t.Fatalf("mock authorize URL = %s", start.AuthorizeURL)
	}
	user, redirect, err := env.wechat.Authenticate(ctx, "mock", start.State, start.StateCookie)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if redirect != "/training" {
		t.Fatalf("redirect = %q", redirect)
	}
	if user.GlobalRole != model.GlobalRoleStudent || user.PasswordHash != "" {
		t.Fatalf("wechat user = role %q password %q, want passwordless student", user.GlobalRole, user.PasswordHash)
	}
	if user.WechatOpenID != "mock:e2e-openid-1" || user.AccountCode == "" {
		t.Fatalf("wechat identity = %q account code %q", user.WechatOpenID, user.AccountCode)
	}
	if _, _, err := env.auth.Login(ctx, user.Username, "anything"); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("password login for wechat user = %v, want invalid credential", err)
	}
	if err := env.auth.ChangePassword(ctx, user.ID, "anything", "new-pass-1"); !errors.Is(err, ErrPasswordNotSet) {
		t.Fatalf("change password for wechat user = %v, want ErrPasswordNotSet", err)
	}
	token, _, err := env.auth.IssueToken(ctx, user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	me, err := env.auth.ValidateToken(ctx, token)
	if err != nil {
		t.Fatalf("validate token: %v", err)
	}
	if me.PasswordSet {
		t.Fatal("wechat user must expose password_set=false")
	}

	start2, err := env.wechat.Start("", "e2e-openid-1")
	if err != nil {
		t.Fatalf("start again: %v", err)
	}
	again, redirect, err := env.wechat.Authenticate(ctx, "mock", start2.State, start2.StateCookie)
	if err != nil {
		t.Fatalf("authenticate again: %v", err)
	}
	if redirect != "/learn" {
		t.Fatalf("default redirect = %q", redirect)
	}
	if again.ID != user.ID {
		t.Fatalf("same wechat identity created different users: %d vs %d", user.ID, again.ID)
	}

	start3, err := env.wechat.Start("", "e2e-openid-2")
	if err != nil {
		t.Fatalf("start third: %v", err)
	}
	other, _, err := env.wechat.Authenticate(ctx, "mock", start3.State, start3.StateCookie)
	if err != nil {
		t.Fatalf("authenticate third: %v", err)
	}
	if other.ID == user.ID {
		t.Fatal("different wechat identity must create a different user")
	}
}

// TestWechatStateValidation state 是登录 CSRF 的唯一凭证：nonce 不匹配、
// Cookie 被篡改、过期都必须拒绝。
func TestWechatStateValidation(t *testing.T) {
	env := newWechatTestEnv(t, mockWechatConfig())
	ctx := context.Background()
	start, err := env.wechat.Start("", "e2e-openid")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.wechat.Authenticate(ctx, "mock", "wrong-state", start.StateCookie); !errors.Is(err, ErrWechatStateInvalid) {
		t.Fatalf("wrong state = %v, want ErrWechatStateInvalid", err)
	}
	if _, _, err := env.wechat.Authenticate(ctx, "mock", start.State, start.StateCookie+"x"); !errors.Is(err, ErrWechatStateInvalid) {
		t.Fatalf("tampered cookie = %v, want ErrWechatStateInvalid", err)
	}
	env.now = env.now.Add(WechatStateTTL + time.Second)
	if _, _, err := env.wechat.Authenticate(ctx, "mock", start.State, start.StateCookie); !errors.Is(err, ErrWechatStateInvalid) {
		t.Fatalf("expired state = %v, want ErrWechatStateInvalid", err)
	}
}

func TestWechatStartRejectsOpenRedirect(t *testing.T) {
	env := newWechatTestEnv(t, mockWechatConfig())
	for _, target := range []string{"https://evil.example.com", "//evil.example.com", "/\\evil"} {
		if _, err := env.wechat.Start(target, "id"); !errors.Is(err, ErrValidation) {
			t.Fatalf("redirect %q = %v, want ErrValidation", target, err)
		}
	}
}

// TestWechatExchangeOfficialAPI 真实接口形态（httptest 替身）：token 请求带
// appid/secret/code/grant_type，userinfo 用 access_token/openid 取昵称与 unionid。
func TestWechatExchangeOfficialAPI(t *testing.T) {
	var tokenQuery, infoQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"access_token":"at-1","expires_in":7200,"openid":"openid-1","scope":"snsapi_userinfo","unionid":"union-token"}`))
		case "/userinfo":
			infoQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"openid":"openid-1","nickname":"微信昵称","headimgurl":"https://img.example.com/a.png","unionid":"union-info"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewWechatOAuthClient(WechatOAuthConfig{
		AppID: "app-1", AppSecret: "secret-1",
		RedirectURL:   "https://academy.example.com/login",
		TokenEndpoint: server.URL + "/token", UserInfoEndpoint: server.URL + "/userinfo",
	})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := client.Exchange(context.Background(), "code-1")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if identity.OpenID != "openid-1" || identity.UnionID != "union-info" ||
		identity.Nickname != "微信昵称" || identity.AvatarURL == "" {
		t.Fatalf("identity = %+v", identity)
	}
	for _, want := range []string{"appid=app-1", "secret=secret-1", "code=code-1", "grant_type=authorization_code"} {
		if !strings.Contains(tokenQuery, want) {
			t.Fatalf("token query missing %q: %s", want, tokenQuery)
		}
	}
	for _, want := range []string{"access_token=at-1", "openid=openid-1"} {
		if !strings.Contains(infoQuery, want) {
			t.Fatalf("userinfo query missing %q: %s", want, infoQuery)
		}
	}
}

func TestWechatExchangeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":40029,"errmsg":"invalid code"}`))
	}))
	defer server.Close()
	client, err := NewWechatOAuthClient(WechatOAuthConfig{
		AppID: "id", AppSecret: "secret", RedirectURL: "https://academy.example.com/login",
		TokenEndpoint: server.URL + "/token", UserInfoEndpoint: server.URL + "/userinfo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Exchange(context.Background(), "bad"); !errors.Is(err, ErrWechatAuthFailed) {
		t.Fatalf("exchange error = %v, want ErrWechatAuthFailed", err)
	}
}

func TestWechatUsernameGeneration(t *testing.T) {
	if got := wechatUsernameBase("  张三  ", "openid"); !strings.HasPrefix(got, "张三_") {
		t.Fatalf("nickname username = %q, want 张三_ prefix", got)
	}
	if got := wechatUsernameBase("🙂", "openid-xyz"); !strings.HasPrefix(got, "wx_") {
		t.Fatalf("emoji-only nickname should fall back to wx_, got %q", got)
	}
	long := strings.Repeat("昵", 40)
	if got := truncateUTF8(long, 64); len([]byte(got)) > 64 {
		t.Fatalf("truncateUTF8 returned %d bytes", len([]byte(got)))
	}
}
