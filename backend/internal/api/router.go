package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

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
}

func NewRouter(auth *service.AuthService) *Router {
	return &Router{mux: http.NewServeMux(), auth: auth}
}

// Handle 注册路由。requiredRole 为空串表示仅需登录（student 即可）。
func (r *Router) Handle(method, pattern string, required model.GlobalRole, h http.HandlerFunc) {
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
		ctx := context.WithValue(req.Context(), userKey, user)
		h(w, req.WithContext(ctx))
	})
}

// HandlePublic 注册公开路由（仅 /auth/login）。
func (r *Router) HandlePublic(method, pattern string, h http.HandlerFunc) {
	r.mux.HandleFunc(method+" "+pattern, h)
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
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		return r.auth.Secret(), nil
	}); err != nil {
		writeError(w, errUnauthorized)
		return nil, false
	}
	sub, _ := claims["sub"].(string)
	// 从 DB 加载真实用户，保证 token 反映最新角色/状态。
	user, err := r.auth.FindUser(req.Context(), sub)
	if err != nil {
		writeError(w, errUnauthorized)
		return nil, false
	}
	return user, true
}

// authorize 双层角色判定：全局 → 课程级；admin 短路（rbac-model skill）。
// 课程域从路径参数提取（Room/Topic 路由天然携带 topicSlug）。
// 鉴权失败必须 403，禁止 404 掩盖。
func (r *Router) authorize(w http.ResponseWriter, req *http.Request, user *model.User, required model.GlobalRole) bool {
	if required == "" {
		return true // 仅需登录
	}
	if user.GlobalRole.Rank() >= required.Rank() {
		return true // 全局达标（admin 在此短路）
	}
	// 全局不足 → 查课程级角色（双层判定，rbac-model skill）。
	topicSlug := req.PathValue("topicSlug")
	ok, err := r.auth.CheckRole(req.Context(), user, required, topicSlug)
	if err != nil {
		writeError(w, &apiError{"INTERNAL", http.StatusInternalServerError, "role check failed"})
		return false
	}
	if !ok {
		writeError(w, errForbidden)
		return false
	}
	return true
}

// UserFrom 从请求上下文取当前用户（handler 使用）。
func UserFrom(ctx context.Context) *model.User {
	user, _ := ctx.Value(userKey).(*model.User)
	return user
}