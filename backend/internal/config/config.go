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

	// EnvDriver 实例驱动：auto（默认，启动时探测 docker CLI，可用即 docker，否则 noop）/
	// docker（显式要求 docker CLI 可用）/ noop（环境接口 501）。
	EnvDriver string

	// AttachmentsDir 题目附件落盘根目录（<dir>/<slug>/<filename>）。
	AttachmentsDir string
	// AvatarsDir 头像落盘根目录（<dir>/<userID>/<file>）。
	AvatarsDir string
	// WriteupsDir 竞赛 WP 附带 zip 落盘根目录（<dir>/<competitionID>/<wpID>.zip）。
	WriteupsDir string
	// InstanceMemory/InstanceCPUs 题目容器资源限额（docker --memory/--cpus 语法；
	// 空串 = 不限。默认 512Mi / 0.5 核）。
	InstanceMemory string
	InstanceCPUs   string

	// PublicURL 平台对外可达地址：注入题目容器的 CHECKPOINT_URL 前缀
	// （容器内经 host.docker.internal 回达宿主）。
	PublicURL string
}

// 内置默认值仅用于本地开发；生产必须通过环境变量覆盖（main 启动时对照告警）。
const (
	DefaultJWTSecret     = "dev-only-secret"
	DefaultAdminUsername = "admin"
	DefaultAdminPassword = "admin123"
)

func Load() *Config {
	return &Config{
		Addr:           getenv("ACADEMY_ADDR", ":8080"),
		Database:       getenv("ACADEMY_DB", "academy.db"),
		JWTSecret:      getenv("ACADEMY_JWT_SECRET", DefaultJWTSecret),
		AdminUsername:  getenv("ACADEMY_ADMIN_USERNAME", DefaultAdminUsername),
		AdminPassword:  getenv("ACADEMY_ADMIN_PASSWORD", DefaultAdminPassword),
		EnvDriver:      getenv("ACADEMY_ENV_DRIVER", "auto"),
		AttachmentsDir:  getenv("ACADEMY_ATTACHMENTS_DIR", "attachments"),
		AvatarsDir:     getenv("ACADEMY_AVATARS_DIR", "avatars"),
		WriteupsDir:    getenv("ACADEMY_WRITEUPS_DIR", "writeups"),
		InstanceMemory:  getenv("ACADEMY_INSTANCE_MEMORY", "512m"),
		InstanceCPUs:    getenv("ACADEMY_INSTANCE_CPUS", "0.5"),
		PublicURL:      getenv("ACADEMY_PUBLIC_URL", "http://host.docker.internal:8080"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}