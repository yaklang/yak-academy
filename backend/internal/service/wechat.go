package service

// 微信 OAuth 注册/登录。只使用微信官方网页授权接口，不引入短信/邮件成本：
//   公众号网页授权：open.weixin.qq.com/connect/oauth2/authorize（scope=snsapi_userinfo）
//   开放平台网站应用：open.weixin.qq.com/connect/qrconnect（scope=snsapi_login）
//   换取 token：api.weixin.qq.com/sns/oauth2/access_token
//   用户资料：api.weixin.qq.com/sns/userinfo
// 接口形态参考 markbates/goth、silenceper/wechat 与 Casdoor 的微信 IdP 实现。
//
// 安全：state 由 HttpOnly Cookie 保存的签名会话 + 随机 nonce 双校验；回调成功
// 只返回 token JSON，不把 JWT 放进 URL。微信注册账号没有密码（PasswordHash 空），
// 只能走微信登录；管理员重置密码后才可密码登录。

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

const (
	// WechatStateCookieName 微信 OAuth state 会话 Cookie（HttpOnly + SameSite=Lax）。
	WechatStateCookieName = "academy_wechat_state"
	// WechatStateTTL 微信授权 state 有效期。
	WechatStateTTL = 10 * time.Minute

	wechatModeOfficialAccount = "official_account"
	wechatModeOpenPlatform    = "open_platform"

	wechatOfficialAuthorizeEndpoint = "https://open.weixin.qq.com/connect/oauth2/authorize"
	wechatOpenPlatformAuthorizeURL  = "https://open.weixin.qq.com/connect/qrconnect"
	wechatTokenEndpoint             = "https://api.weixin.qq.com/sns/oauth2/access_token"
	wechatUserInfoEndpoint          = "https://api.weixin.qq.com/sns/userinfo"
	wechatResponseLimit             = 1 << 20
)

var (
	// ErrWechatAuthFailed 微信 code 换取身份失败（不向用户泄露微信 errcode/errmsg）。
	ErrWechatAuthFailed = errors.New("wechat authorization failed")
	// ErrWechatStateInvalid state 缺失/签名错误/过期（防登录 CSRF）。
	ErrWechatStateInvalid = errors.New("wechat state is invalid or expired")
	// ErrPasswordNotSet 微信注册账号没有密码（密码登录/改密不可用）。
	ErrPasswordNotSet = errors.New("account has no password")
)

// WechatOAuthConfig 微信 OAuth 配置。空 AppID/AppSecret/RedirectURL 且非 Mock
// = 未启用（status 返回 enabled=false，start 返回 501）。
type WechatOAuthConfig struct {
	AppID       string
	AppSecret   string
	RedirectURL string // 必须指向前端登录页，例如 https://academy.example.com/login
	Mode        string // official_account（默认）| open_platform
	Scope       string // 空 = 按 mode 默认（snsapi_userinfo / snsapi_login）
	Mock        bool   // 仅本地/E2E：不访问微信，直接回调并建号；严格模式禁止

	// 测试与私有化网关覆盖（空 = 微信官方地址）。
	AuthorizeEndpoint string
	TokenEndpoint     string
	UserInfoEndpoint  string
	HTTPClient        *http.Client
}

// WechatOAuthClient 微信网页授权客户端。
type WechatOAuthClient struct {
	appID       string
	appSecret   string
	redirectURL string
	mode        string
	scope       string
	mock        bool

	authorizeEndpoint string
	tokenEndpoint     string
	userInfoEndpoint  string
	httpClient        *http.Client
	enabled           bool
}

// WechatIdentity 微信回调得到的用户身份（openid 必填，unionid/昵称可为空）。
type WechatIdentity struct {
	OpenID    string
	UnionID   string
	Nickname  string
	AvatarURL string
}

// NewWechatOAuthClient 校验并构造客户端。三个关键配置都为空 = 明确未启用；
// 只填了部分 = 配置错误（返回 error，避免线上以为已启用却永远失败）。
func NewWechatOAuthClient(cfg WechatOAuthConfig) (*WechatOAuthClient, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = wechatModeOfficialAccount
	}
	if mode != wechatModeOfficialAccount && mode != wechatModeOpenPlatform {
		return nil, fmt.Errorf("wechat mode must be official_account or open_platform")
	}
	scope := strings.TrimSpace(cfg.Scope)
	if scope == "" {
		if mode == wechatModeOpenPlatform {
			scope = "snsapi_login"
		} else {
			scope = "snsapi_userinfo"
		}
	}
	redirectURL := strings.TrimSpace(cfg.RedirectURL)
	if redirectURL != "" {
		if err := validateWechatRedirectURL(redirectURL); err != nil {
			return nil, err
		}
	}
	appID := strings.TrimSpace(cfg.AppID)
	appSecret := strings.TrimSpace(cfg.AppSecret)
	enabled := cfg.Mock || (appID != "" && appSecret != "" && redirectURL != "")
	if !cfg.Mock {
		if appID == "" && appSecret == "" && redirectURL == "" {
			enabled = false
		} else if appID == "" || appSecret == "" || redirectURL == "" {
			return nil, fmt.Errorf("wechat oauth requires app id, app secret and redirect URL together")
		}
	}
	if cfg.Mock && redirectURL == "" {
		return nil, fmt.Errorf("wechat mock mode requires redirect URL")
	}
	authorizeEndpoint := strings.TrimSpace(cfg.AuthorizeEndpoint)
	if authorizeEndpoint == "" {
		if mode == wechatModeOpenPlatform {
			authorizeEndpoint = wechatOpenPlatformAuthorizeURL
		} else {
			authorizeEndpoint = wechatOfficialAuthorizeEndpoint
		}
	}
	tokenEndpoint := strings.TrimSpace(cfg.TokenEndpoint)
	if tokenEndpoint == "" {
		tokenEndpoint = wechatTokenEndpoint
	}
	userInfoEndpoint := strings.TrimSpace(cfg.UserInfoEndpoint)
	if userInfoEndpoint == "" {
		userInfoEndpoint = wechatUserInfoEndpoint
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &WechatOAuthClient{
		appID: appID, appSecret: appSecret, redirectURL: redirectURL,
		mode: mode, scope: scope, mock: cfg.Mock,
		authorizeEndpoint: authorizeEndpoint, tokenEndpoint: tokenEndpoint,
		userInfoEndpoint: userInfoEndpoint, httpClient: client, enabled: enabled,
	}, nil
}

func validateWechatRedirectURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.Fragment != "" {
		return fmt.Errorf("invalid wechat redirect URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid wechat redirect URL scheme")
	}
	return nil
}

// Enabled 是否已配置（或处于 Mock）。
func (c *WechatOAuthClient) Enabled() bool { return c != nil && c.enabled }

// Mock 是否本地模拟模式。
func (c *WechatOAuthClient) Mock() bool { return c != nil && c.mock }

// Mode 当前授权模式。
func (c *WechatOAuthClient) Mode() string {
	if c == nil {
		return ""
	}
	return c.mode
}

// AuthorizeURL 构造微信授权地址。state 同时写入 query，由调用方存入 HttpOnly Cookie。
func (c *WechatOAuthClient) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("appid", c.appID)
	q.Set("redirect_uri", c.redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", c.scope)
	q.Set("state", state)
	return c.authorizeEndpoint + "?" + q.Encode() + "#wechat_redirect"
}

// MockAuthorizeURL Mock 模式下直接跳回前端登录页，由登录页把 code/state 交回后端。
func (c *WechatOAuthClient) MockAuthorizeURL(state string) string {
	u, err := url.Parse(c.redirectURL)
	if err != nil {
		return c.redirectURL
	}
	q := u.Query()
	q.Set("code", "mock")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

type wechatTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	OpenID       string `json:"openid"`
	Scope        string `json:"scope"`
	UnionID      string `json:"unionid"`
	ErrCode      int    `json:"errcode"`
	ErrMsg       string `json:"errmsg"`
}

type wechatUserInfoResponse struct {
	OpenID     string `json:"openid"`
	Nickname   string `json:"nickname"`
	HeadImgURL string `json:"headimgurl"`
	UnionID    string `json:"unionid"`
	ErrCode    int    `json:"errcode"`
	ErrMsg     string `json:"errmsg"`
}

// Exchange 用微信回调 code 换取 openid/unionid/昵称。token 与 userinfo 请求
// 都经 url.Values 编码，避免手工拼接把 code/secret 泄漏进日志或 URL 结构。
func (c *WechatOAuthClient) Exchange(ctx context.Context, code string) (WechatIdentity, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return WechatIdentity{}, fmt.Errorf("%w: code is required", ErrWechatAuthFailed)
	}
	q := url.Values{}
	q.Set("appid", c.appID)
	q.Set("secret", c.appSecret)
	q.Set("code", code)
	q.Set("grant_type", "authorization_code")
	var token wechatTokenResponse
	if err := c.getJSON(ctx, c.tokenEndpoint+"?"+q.Encode(), &token); err != nil {
		return WechatIdentity{}, err
	}
	if token.ErrCode != 0 {
		log.Printf("wechat token exchange failed: errcode=%d", token.ErrCode)
		return WechatIdentity{}, ErrWechatAuthFailed
	}
	if strings.TrimSpace(token.OpenID) == "" || strings.TrimSpace(token.AccessToken) == "" {
		return WechatIdentity{}, fmt.Errorf("%w: token response missing openid/access_token", ErrWechatAuthFailed)
	}
	identity := WechatIdentity{OpenID: token.OpenID, UnionID: token.UnionID}
	// userinfo 失败不阻断登录：token 已证明 openid 归属，昵称仅用于用户名候选。
	infoQuery := url.Values{}
	infoQuery.Set("access_token", token.AccessToken)
	infoQuery.Set("openid", token.OpenID)
	infoQuery.Set("lang", "zh_CN")
	var info wechatUserInfoResponse
	if err := c.getJSON(ctx, c.userInfoEndpoint+"?"+infoQuery.Encode(), &info); err != nil {
		log.Printf("wechat userinfo unavailable: %v", err)
	} else if info.ErrCode != 0 {
		log.Printf("wechat userinfo failed: errcode=%d", info.ErrCode)
	} else {
		identity.Nickname = info.Nickname
		identity.AvatarURL = info.HeadImgURL
		if info.UnionID != "" {
			identity.UnionID = info.UnionID
		}
	}
	return identity, nil
}

func (c *WechatOAuthClient) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("%w: build request", ErrWechatAuthFailed)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: request failed", ErrWechatAuthFailed)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, wechatResponseLimit))
	if err != nil {
		return fmt.Errorf("%w: read response", ErrWechatAuthFailed)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: http %d", ErrWechatAuthFailed, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: decode response", ErrWechatAuthFailed)
	}
	return nil
}

type wechatStateSession struct {
	Nonce      string `json:"n"`
	Redirect   string `json:"r"`
	MockOpenID string `json:"m,omitempty"`
	ExpiresAt  int64  `json:"e"`
}

// WechatStartResult 发起微信授权的结果：前端跳转 authorize_url；state_cookie
// 由 API 层写入 HttpOnly Cookie（不返回给前端）。
type WechatStartResult struct {
	AuthorizeURL string
	State        string
	StateCookie  string
	MaxAge       int
	// MockID 仅 Mock 模式返回，供 API 层写入稳定的本地模拟身份 Cookie。
	MockID string
}

// WechatAuthService 微信注册/登录：state 校验 + 微信身份换取 + 本地建号。
type WechatAuthService struct {
	users       repo.UserRepo
	oauth       *WechatOAuthClient
	stateSecret []byte
	now         func() time.Time
}

func NewWechatAuthService(users repo.UserRepo, oauth *WechatOAuthClient, stateSecret []byte) *WechatAuthService {
	return &WechatAuthService{
		users: users, oauth: oauth, stateSecret: stateSecret, now: time.Now,
	}
}

func (s *WechatAuthService) Enabled() bool { return s != nil && s.oauth.Enabled() }
func (s *WechatAuthService) Mock() bool    { return s != nil && s.oauth.Mock() }
func (s *WechatAuthService) Mode() string {
	if s == nil {
		return ""
	}
	return s.oauth.Mode()
}

// Start 发起微信授权：生成随机 nonce 与签名 state 会话；redirect 只允许站内
// 绝对路径（防 open redirect）。Mock 模式直接跳回前端登录页。
func (s *WechatAuthService) Start(redirect, mockID string) (WechatStartResult, error) {
	if !s.Enabled() {
		return WechatStartResult{}, errNotImplemented("wechat login not configured")
	}
	if len(s.stateSecret) == 0 {
		return WechatStartResult{}, fmt.Errorf("wechat state secret is empty")
	}
	redirect, err := sanitizeWechatAppRedirect(redirect)
	if err != nil {
		return WechatStartResult{}, err
	}
	nonce, err := randomHex(32)
	if err != nil {
		return WechatStartResult{}, err
	}
	session := wechatStateSession{
		Nonce: nonce, Redirect: redirect,
		ExpiresAt: s.now().Add(WechatStateTTL).Unix(),
	}
	if s.Mock() {
		mockID = strings.TrimSpace(mockID)
		if mockID == "" {
			mockID, err = randomHex(12)
			if err != nil {
				return WechatStartResult{}, err
			}
		}
		if len(mockID) > 64 {
			return WechatStartResult{}, fmt.Errorf("%w: mock id is too long", ErrValidation)
		}
		session.MockOpenID = mockID
	}
	cookieValue, err := s.signState(session)
	if err != nil {
		return WechatStartResult{}, err
	}
	authorizeURL := s.oauth.AuthorizeURL(nonce)
	if s.Mock() {
		authorizeURL = s.oauth.MockAuthorizeURL(nonce)
	}
	return WechatStartResult{
		AuthorizeURL: authorizeURL, State: nonce, StateCookie: cookieValue,
		MaxAge: int(WechatStateTTL.Seconds()), MockID: session.MockOpenID,
	}, nil
}

// Authenticate 校验 state Cookie 与回调 state，换取微信身份并查找/创建用户，
// 返回登录后站内跳转路径。错误只返回哨兵/通用信息，不泄露微信原始 errmsg。
func (s *WechatAuthService) Authenticate(ctx context.Context, code, state, stateCookie string) (*model.User, string, error) {
	if !s.Enabled() {
		return nil, "", errNotImplemented("wechat login not configured")
	}
	state = strings.TrimSpace(state)
	if state == "" || strings.TrimSpace(stateCookie) == "" {
		return nil, "", ErrWechatStateInvalid
	}
	session, err := s.parseState(stateCookie)
	if err != nil {
		return nil, "", err
	}
	if subtle.ConstantTimeCompare([]byte(session.Nonce), []byte(state)) != 1 {
		return nil, "", ErrWechatStateInvalid
	}
	var identity WechatIdentity
	if s.Mock() {
		if session.MockOpenID == "" {
			return nil, "", ErrWechatStateInvalid
		}
		identity = WechatIdentity{OpenID: "mock:" + session.MockOpenID, Nickname: "微信用户"}
	} else {
		identity, err = s.oauth.Exchange(ctx, code)
		if err != nil {
			return nil, "", err
		}
	}
	identity.OpenID = strings.TrimSpace(identity.OpenID)
	identity.UnionID = strings.TrimSpace(identity.UnionID)
	if identity.OpenID == "" || len(identity.OpenID) > 128 ||
		strings.ContainsAny(identity.OpenID, " \t\r\n") {
		return nil, "", fmt.Errorf("%w: invalid openid", ErrWechatAuthFailed)
	}
	user, err := s.findOrCreateUser(ctx, identity)
	if err != nil {
		return nil, "", err
	}
	return user, session.Redirect, nil
}

func (s *WechatAuthService) signState(session wechatStateSession) (string, error) {
	payload, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.stateSecret)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func (s *WechatAuthService) parseState(value string) (wechatStateSession, error) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return wechatStateSession{}, ErrWechatStateInvalid
	}
	mac := hmac.New(sha256.New, s.stateSecret)
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return wechatStateSession{}, ErrWechatStateInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return wechatStateSession{}, ErrWechatStateInvalid
	}
	var session wechatStateSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return wechatStateSession{}, ErrWechatStateInvalid
	}
	if session.Nonce == "" || session.ExpiresAt < s.now().Unix() {
		return wechatStateSession{}, ErrWechatStateInvalid
	}
	return session, nil
}

// findOrCreateUser 按 openid 优先、unionid 次之查找；都没有则创建 student 账号。
// 微信账号的 PasswordHash 为空（PasswordSet=false），只能微信登录。
func (s *WechatAuthService) findOrCreateUser(ctx context.Context, identity WechatIdentity) (*model.User, error) {
	if user, err := s.users.FindByWechatOpenID(ctx, identity.OpenID); err == nil {
		s.refreshWechatProfile(ctx, user, identity)
		return user, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if identity.UnionID != "" {
		if user, err := s.users.FindByWechatUnionID(ctx, identity.UnionID); err == nil {
			s.refreshWechatProfile(ctx, user, identity)
			return user, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	baseName := wechatUsernameBase(identity.Nickname, identity.OpenID)
	for attempt := 0; attempt < 8; attempt++ {
		name := baseName
		if attempt > 0 {
			name = wechatUsernameCandidate(baseName, identity.OpenID, attempt)
		}
		user := &model.User{
			Username:       name,
			GlobalRole:     model.GlobalRoleStudent,
			WechatOpenID:   identity.OpenID,
			WechatUnionID:  identity.UnionID,
			WechatNickname: truncateUTF8(identity.Nickname, 64),
		}
		err := s.users.Create(ctx, user)
		if err == nil {
			return user, nil
		}
		// 并发回调：另一个请求已创建同 openid 账号，直接取回。
		if existing, findErr := s.users.FindByWechatOpenID(ctx, identity.OpenID); findErr == nil {
			return existing, nil
		}
		if _, findErr := s.users.FindByUsername(ctx, name); findErr == nil {
			continue // 用户名撞车，换后缀重试
		}
		return nil, err
	}
	return nil, fmt.Errorf("%w: cannot allocate username", ErrValidation)
}

func (s *WechatAuthService) refreshWechatProfile(ctx context.Context, user *model.User, identity WechatIdentity) {
	openID := user.WechatOpenID
	if openID == "" {
		openID = identity.OpenID
	}
	unionID := user.WechatUnionID
	if unionID == "" {
		unionID = identity.UnionID
	}
	nickname := user.WechatNickname
	if nickname == "" {
		nickname = identity.Nickname
	}
	if err := s.users.UpdateWechatIdentity(ctx, user.ID, openID, unionID, nickname); err != nil {
		log.Printf("wechat identity refresh failed: user=%d err=%v", user.ID, err)
	}
}

func sanitizeWechatAppRedirect(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/learn", nil
	}
	if len(raw) > 512 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("%w: redirect must be a local path", ErrValidation)
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: redirect must be a local path", ErrValidation)
	}
	return raw, nil
}

func wechatUsernameBase(nickname, openID string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(nickname) {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), r == '_', r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			// 昵称中的空格直接丢弃，避免生成无法口令输入的登录名。
		default:
			// 表情/标点不进入用户名。
		}
	}
	base := truncateUTF8(b.String(), 48)
	if base == "" {
		return "wx_" + shortHash(openID)[:8]
	}
	if utf8.RuneCountInString(base) < 3 {
		// 两字中文昵称合法，但用户名最短 3 字符：保留昵称并补稳定后缀。
		base += "_" + shortHash(openID)[:4]
	}
	return base
}

func wechatUsernameCandidate(base, openID string, attempt int) string {
	suffix := "_" + shortHash(fmt.Sprintf("%s:%d", openID, attempt))[:6]
	return truncateUTF8(base, 64-len(suffix)) + suffix
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
