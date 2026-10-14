package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ErrInvalidCredential 登录失败（用户不存在或密码错误，对外不区分）。
var ErrInvalidCredential = errors.New("invalid username or password")

// AuthService 认证与双层 RBAC 判定（rbac-model skill）。
type AuthService struct {
	users  repo.UserRepo
	secret []byte
	ttl    time.Duration
}

func NewAuthService(users repo.UserRepo, secret string) *AuthService {
	return &AuthService{users: users, secret: []byte(secret), ttl: 24 * time.Hour}
}

// Secret 暴露给 api 层的 JWT 验签密钥。
func (s *AuthService) Secret() []byte { return s.secret }

// Login 校验用户名密码并签发 JWT。密码使用 bcrypt 比对。
func (s *AuthService) Login(ctx context.Context, username, password string) (string, time.Time, error) {
	user, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", time.Time{}, ErrInvalidCredential
		}
		return "", time.Time{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return "", time.Time{}, ErrInvalidCredential
	}
	return s.issueToken(user)
}

func (s *AuthService) issueToken(user *model.User) (string, time.Time, error) {
	expires := time.Now().Add(s.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  user.Username,
		"role": string(user.GlobalRole),
		"exp":  expires.Unix(),
	})
	signed, err := tok.SignedString(s.secret)
	return signed, expires, err
}

// FindUser 按 username 加载真实用户（api 中间件鉴权用，保证 token 反映最新角色）。
func (s *AuthService) FindUser(ctx context.Context, username string) (*model.User, error) {
	return s.users.FindByUsername(ctx, username)
}

// CreateUser 管理端创建用户（bcrypt 哈希）。角色只允许 student / admin。
func (s *AuthService) CreateUser(ctx context.Context, username, password string, role model.GlobalRole) (*model.User, error) {
	if username == "" || password == "" {
		return nil, ErrValidation
	}
	if role == "" {
		role = model.GlobalRoleStudent
	}
	role = role.Normalize()
	if role != model.GlobalRoleStudent && role != model.GlobalRoleAdmin {
		return nil, fmt.Errorf("%w: global_role must be student or admin", ErrValidation)
	}
	if _, err := s.users.FindByUsername(ctx, username); err == nil {
		return nil, ErrValidation // 用户名已存在
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &model.User{Username: username, PasswordHash: string(hash), GlobalRole: role}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// UpdateRoles 修改全局角色（仅 student / admin）。
func (s *AuthService) UpdateRoles(ctx context.Context, userID uint, globalRole model.GlobalRole) (*model.User, error) {
	if globalRole != "" {
		role := globalRole.Normalize()
		if role != model.GlobalRoleStudent && role != model.GlobalRoleAdmin {
			return nil, fmt.Errorf("%w: global_role must be student or admin", ErrValidation)
		}
		if err := s.users.UpdateGlobalRole(ctx, userID, role); err != nil {
			return nil, err
		}
	}
	return s.users.FindByID(ctx, userID)
}

// ListUsers 用户列表（分页）。
func (s *AuthService) ListUsers(ctx context.Context, offset, limit int) ([]model.User, int64, error) {
	return s.users.List(ctx, offset, limit)
}

// EnsureAdmin users 表为空时创建初始管理员。
// 这是唯一的种子数据：只创建账号，不创建任何内容（content-package-spec skill）。
func (s *AuthService) EnsureAdmin(ctx context.Context, username, password string) error {
	if _, err := s.users.FindByUsername(ctx, username); err == nil {
		return nil // 已存在（含非空表场景），跳过
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.users.Create(ctx, &model.User{
		Username:     username,
		PasswordHash: string(hash),
		GlobalRole:   model.GlobalRoleAdmin,
	}); err != nil {
		return err
	}
	log.Printf("created initial admin account %q (please change the password)", username)
	return nil
}