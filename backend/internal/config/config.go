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
}

func Load() *Config {
	return &Config{
		Addr:          getenv("ACADEMY_ADDR", ":8080"),
		Database:      getenv("ACADEMY_DB", "academy.db"),
		JWTSecret:     getenv("ACADEMY_JWT_SECRET", "dev-only-secret"),
		AdminUsername: getenv("ACADEMY_ADMIN_USERNAME", "admin"),
		AdminPassword: getenv("ACADEMY_ADMIN_PASSWORD", "admin123"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}