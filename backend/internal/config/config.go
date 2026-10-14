// Package config 加载服务配置（环境变量 + 默认值）。
package config

import "os"

type Config struct {
	Addr      string // HTTP 监听地址
	Database  string // SQLite 文件路径
	JWTSecret string

	// 初始管理员（users 表为空时启动种子创建，见 AuthService.EnsureAdmin）。
	AdminUsername string
	AdminPassword string

	// EnvDriver 实例驱动：noop（默认，接口 501）/ docker（docker CLI 需可用）。
	EnvDriver string
}

// 内置默认值仅用于本地开发；生产必须通过环境变量覆盖（main 启动时对照告警）。
const (
	DefaultJWTSecret     = "dev-only-secret"
	DefaultAdminUsername = "admin"
	DefaultAdminPassword = "admin123"
)

func Load() *Config {
	return &Config{
		Addr:          getenv("ACADEMY_ADDR", ":8080"),
		Database:      getenv("ACADEMY_DB", "academy.db"),
		JWTSecret:     getenv("ACADEMY_JWT_SECRET", DefaultJWTSecret),
		AdminUsername: getenv("ACADEMY_ADMIN_USERNAME", DefaultAdminUsername),
		AdminPassword: getenv("ACADEMY_ADMIN_PASSWORD", DefaultAdminPassword),
		EnvDriver:     getenv("ACADEMY_ENV_DRIVER", "noop"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}