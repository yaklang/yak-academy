package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

type ctxKey string

const userKey ctxKey = "user"

// Router 承载路由表与角色声明。声明式鉴权：路由注册时声明 RequireRole，
// 中间件统一执行；handler 内禁止散落权限判断（rbac-model skill）。
type Router struct {
	mux    *http.ServeMux
	auth   *service.AuthService
	routes []RouteInfo
	// entryLock 竞赛入场锁定检查（可选注入；nil = 未启用，测试/早期装配兼容）。
	entryLock EntryLockChecker
	// metricsToken /metrics 采集令牌（ACADEMY_METRICS_TOKEN；空 = 端点关闭 404）。
	metricsToken string
	// metrics 请求指标聚合 + accessLog 结构化访问日志（见 metrics.go）。
	metrics   *collector
	accessLog *slog.Logger
}

// EntryLockChecker 竞赛入场锁：已入场且竞赛未结束的参赛者，
// 平台其余接口一律 403（锁定直到比赛结束，登出重进同样受控）。
type EntryLockChecker interface {
	// LockedCompetition 返回 (竞赛ID, true) 表示该用户当前被锁定。
	LockedCompetition(ctx context.Context, userID uint) (uint, bool)
}

// SetEntryLock 注入入场锁检查（main 装配后调用一次）。
func (r *Router) SetEntryLock(c EntryLockChecker) { r.entryLock = c }

// lockedAllowlist 锁定期间仍可访问的路径前缀：
// /me*（登录态维持、账号面板、头像、站内邮箱邀请回看）、
// /auth/logout（登出后重进仍被锁——锁定状态在服务端）、/competitions*（竞赛域）。
var lockedAllowlist = []string{
	"/api/v1/me",
	"/api/v1/auth/logout",
	"/api/v1/competitions",
}

func lockedPathAllowed(path string) bool {
	for _, p := range lockedAllowlist {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// RouteInfo 单条路由的注册元数据（Routes() 快照，契约测试比对 openapi.yaml 用）。
type RouteInfo struct {
	Method  string
	Pattern string
	Role    model.GlobalRole // 空 = 仅需登录
	Public  bool
}

func NewRouter(auth *service.AuthService) *Router {
	return &Router{
		mux:       http.NewServeMux(),
		auth:      auth,
		metrics:   newCollector(),
		accessLog: slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}
}

// SetMetricsToken 注入 /metrics 采集令牌（main 装配后、Register 前调用；
// 空 = 端点关闭，路由仍注册以保持契约一致）。
func (r *Router) SetMetricsToken(token string) { r.metricsToken = token }

// Handle 注册路由。requiredRole 为空串表示仅需登录（student 即可）。
func (r *Router) Handle(method, pattern string, required model.GlobalRole, h http.HandlerFunc) {
	r.routes = append(r.routes, RouteInfo{Method: method, Pattern: pattern, Role: required})
	full := method + " " + pattern
	r.mux.HandleFunc(full, func(w http.ResponseWriter, req *http.Request) {
		// 中间件：JWT 解析 → 双层角色判定
		user, ok := r.authenticate(w, req)
		if !ok {
			return
		}
		if !r.authorize(w, req, user, required) {
			return
		}
		// 竞赛入场锁：已入场未结束的参赛者只放行竞赛域接口（管理员不受限，
		// 管理端需要跨域操作）。锁定与角色判定不同层：状态检查而非权限。
		if r.entryLock != nil && user.GlobalRole.Normalize() != model.GlobalRoleAdmin {
			if _, locked := r.entryLock.LockedCompetition(req.Context(), user.ID); locked && !lockedPathAllowed(req.URL.Path) {
				writeError(w, &apiError{"FORBIDDEN", http.StatusForbidden, "competition in progress: only competition endpoints are accessible"})
				return
			}
		}
		ctx := context.WithValue(req.Context(), userKey, user)
		r.observe(method, pattern, h)(w, req.WithContext(ctx))
	})
}

// HandlePublic 注册公开路由（仅 /auth/login、/checkpoints/{token}、/health、/metrics）。
func (r *Router) HandlePublic(method, pattern string, h http.HandlerFunc) {
	r.routes = append(r.routes, RouteInfo{Method: method, Pattern: pattern, Public: true})
	// /health 是 compose/systemd 探针高频路径：不进指标与访问日志（与 /metrics 同理）。
	if pattern == "/health" {
		r.mux.HandleFunc(method+" "+pattern, h)
		return
	}
	r.mux.HandleFunc(method+" "+pattern, r.observe(method, pattern, h))
}

// Routes 返回已注册路由的快照（契约测试与调试用，不暴露 mux 本体）。
func (r *Router) Routes() []RouteInfo {
	out := make([]RouteInfo, len(r.routes))
	copy(out, r.routes)
	return out
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) authenticate(w http.ResponseWriter, req *http.Request) (*model.User, bool) {
	header := req.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeError(w, errUnauthorized)
		return nil, false
	}
	tokenStr := strings.TrimPrefix(header, "Bearer ")
	// ValidateToken 统一执行：签名 → jti 撤销表 → 改密失效阈值 → 加载最新用户。
	user, err := r.auth.ValidateToken(req.Context(), tokenStr)
	if err != nil {
		if errors.Is(err, service.ErrTokenRevoked) {
			writeError(w, &apiError{"UNAUTHORIZED", http.StatusUnauthorized, "token revoked, please login again"})
		} else {
			writeError(w, errUnauthorized)
		}
		return nil, false
	}
	return user, true
}

// TokenFrom 从 Authorization 头提取原始 token（登出 handler 用）。
func TokenFrom(req *http.Request) string {
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
}

// authorize 两档角色判定：普通人(student) / 管理员(admin)。
// required 为空 = 仅需登录；否则比较 Rank（admin 短路）。鉴权失败 403。
func (r *Router) authorize(w http.ResponseWriter, req *http.Request, user *model.User, required model.GlobalRole) bool {
	if required == "" {
		return true
	}
	// 历史 teacher 等值经 Normalize 后按普通人处理。
	role := user.GlobalRole.Normalize()
	if role.Rank() >= required.Normalize().Rank() {
		return true
	}
	writeError(w, errForbidden)
	return false
}

// UserFrom 从请求上下文取当前用户（handler 使用）。
func UserFrom(ctx context.Context) *model.User {
	user, _ := ctx.Value(userKey).(*model.User)
	return user
}