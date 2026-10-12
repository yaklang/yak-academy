// Package service 业务逻辑层。数据访问只经 repo 接口（project-conventions skill）。
package service

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// AuthService 认证与双层 RBAC 判定（rbac-model skill）。
type AuthService struct {
	users repo.UserRepo
	secret []byte
	ttl   time.Duration
}

func NewAuthService(users repo.UserRepo, secret string) *AuthService {
	return &AuthService{users: users, secret: []byte(secret), ttl: 24 * time.Hour}
}

// Secret 暴露给 api 层的 JWT 验签密钥。
func (s *AuthService) Secret() []byte { return s.secret }

// IssueToken 登录成功后签发 JWT。TODO(Phase2): 密码校验（bcrypt）。
func (s *AuthService) IssueToken(ctx context.Context, username, password string) (string, time.Time, error) {
	_, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(s.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  username,
		"role": string(model.GlobalRoleStudent),
		"exp":  expires.Unix(),
	})
	signed, err := tok.SignedString(s.secret)
	return signed, expires, err
}

// CheckRole 双层角色判定：全局角色 Rank 达标即通过；不足时查课程级角色。
// 属主规则（如"自己班级"）不在此处，放各 service 内（rbac-model skill）。
func (s *AuthService) CheckRole(ctx context.Context, user *model.User, required model.GlobalRole, topicSlug string) (bool, error) {
	if user.GlobalRole.Rank() >= required.Rank() {
		return true, nil // 全局达标（admin 在此短路）
	}
	if topicSlug == "" {
		return false, nil
	}
	roles, err := s.users.ListCourseRoles(ctx, user.ID)
	if err != nil {
		return false, err
	}
	for _, cr := range roles {
		if cr.TopicSlug == topicSlug && cr.Role == string(required) {
			return true, nil
		}
	}
	return false, nil
}