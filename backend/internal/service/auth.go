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

// ErrTokenRevoked token 已被撤销（登出吊销单个 jti）或早于失效阈值（改密后全量失效）。
var ErrTokenRevoked = errors.New("token revoked")

// AuthService 认证与双层 RBAC 判定（rbac-model skill）。
type AuthService struct {
	users       repo.UserRepo
	revocations repo.TokenRevocationRepo
	secret      []byte
	ttl         time.Duration
}

func NewAuthService(users repo.UserRepo, secret string) *AuthService {
	return &AuthService{users: users, secret: []byte(secret), ttl: 24 * time.Hour}
}

// SetRevocationRepo 注入撤销表（main 装配后调用一次；nil = 登出退化为仅本地清除）。
func (s *AuthService) SetRevocationRepo(r repo.TokenRevocationRepo) { s.revocations = r }

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
	return s.issueToken(ctx, user)
}

// IssueToken 为指定用户签发 token（注册建号成功后即时登录，复用登录签发逻辑；
// 不做密码校验，调用方须已通过自己的凭据核验）。
func (s *AuthService) IssueToken(ctx context.Context, user *model.User) (string, time.Time, error) {
	return s.issueToken(ctx, user)
}

func (s *AuthService) issueToken(ctx context.Context, user *model.User) (string, time.Time, error) {
	// iat 只有秒精度，改密/重置阈值按「签发秒 + 1」存储（见 UpdatePassword）：
	// 改密后 1 秒内的新登录会因 iat < 阈值被拒。签发前等待出窗，
	// 保证改密后的新登录永远有效。等待上限 2s 兜底阈值被推到远未来的
	// 病态场景（正常改密只差 1 秒），超限放弃等待（token 会被拒但登录不挂死）。
	if before := user.TokenInvalidBefore; !before.IsZero() {
		if wait := time.Until(before); wait > 0 {
			if wait > 2*time.Second {
				wait = 2 * time.Second
			}
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return "", time.Time{}, ctx.Err()
			}
		}
	}
	now := time.Now()
	expires := now.Add(s.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  user.Username,
		"role": string(user.GlobalRole),
		"jti":  fmt.Sprintf("%d-%d", user.ID, now.UnixNano()),
		"iat":  now.Unix(),
		"exp":  expires.Unix(),
	})
	signed, err := tok.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	// 不活跃判定基线：签发即视为一次登录（登录/注册即登录都经此处）。
	// best-effort——基线写失败不阻塞登录。
	_ = s.users.TouchLogin(ctx, user.ID, now)
	return signed, expires, nil
}

// ValidateToken 中间件验签入口：签名校验 + jti 撤销检查 + 改密失效阈值检查。
// 任何一项不过即拒绝（ErrTokenRevoked / ErrInvalidCredential）。
func (s *AuthService) ValidateToken(ctx context.Context, tokenStr string) (*model.User, error) {
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	}); err != nil {
		return nil, ErrInvalidCredential
	}
	sub, _ := claims["sub"].(string)
	jti, _ := claims["jti"].(string)
	// 从 DB 加载真实用户，保证 token 反映最新角色/状态。
	user, err := s.users.FindByUsername(ctx, sub)
	if err != nil {
		return nil, ErrInvalidCredential
	}
	if !user.TokenInvalidBefore.IsZero() {
		// iat 只有秒精度：阈值按"签发秒 + 1"存储（见 UpdatePassword 调用方），
		// iat < 阈值即拒绝——改密瞬间之前的全部 token（含同秒签发）死透；
		// 代价是改密后 1 秒内的新登录会被拒，重登一次即可。
		if iat, ok := claims["iat"].(float64); !ok || int64(iat) < user.TokenInvalidBefore.Unix() {
			return nil, ErrTokenRevoked
		}
	}
	if jti != "" && s.revocations != nil {
		if rec, err := s.revocations.FindByJTI(ctx, jti); err != nil {
			return nil, err
		} else if rec != nil {
			return nil, ErrTokenRevoked
		}
	}
	return user, nil
}

// Logout 登出：按 token 的 jti 写撤销表，令牌在自然过期前立即失效。
func (s *AuthService) Logout(ctx context.Context, userID uint, tokenStr string) error {
	if s.revocations == nil {
		return nil // 无撤销表（测试场景）：退化为仅前端清除
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		return s.secret, nil
	}); err != nil {
		return ErrInvalidCredential
	}
	jti, _ := claims["jti"].(string)
	if jti == "" {
		return nil // 旧格式 token（无 jti）：无撤销凭据，等同自然过期流程
	}
	exp, _ := claims["exp"].(float64)
	return s.revocations.Revoke(ctx, &model.TokenRevocation{
		JTI: jti, UserID: userID, ExpiresAt: time.Unix(int64(exp), 0),
	})
}

// ChangePassword 本人改密：校验当前密码，成功后 bcrypt 更新并把失效阈值
// 推进到当前时刻（该用户全部已签发 token 立即失效，需重新登录）。
func (s *AuthService) ChangePassword(ctx context.Context, userID uint, currentPassword, newPassword string) error {
	if len(newPassword) < 6 {
		return fmt.Errorf("%w: new password must be at least 6 characters", ErrValidation)
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return ErrInvalidCredential
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// 阈值 = 签发秒 + 1（iat 秒精度）：改密前签发的 token 全部失效（见 ValidateToken）。
	return s.users.UpdatePassword(ctx, userID, string(hash), time.Now().Truncate(time.Second).Add(time.Second))
}

// ResetPasswordByAdmin 管理员重置密码：不校验旧密码；失效阈值同样推进，
// 目标用户全部 token 立即失效。
func (s *AuthService) ResetPasswordByAdmin(ctx context.Context, userID uint, newPassword string) error {
	if len(newPassword) < 6 {
		return fmt.Errorf("%w: password must be at least 6 characters", ErrValidation)
	}
	if _, err := s.users.FindByID(ctx, userID); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// 阈值 = 签发秒 + 1：重置前签发的 token 全部失效（同 ChangePassword 注释）。
	return s.users.UpdatePassword(ctx, userID, string(hash), time.Now().Truncate(time.Second).Add(time.Second))
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