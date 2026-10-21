// Package config 加载服务配置（环境变量 + 默认值）。
package config

import (
	"os"
	"path/filepath"
	"strings"
)

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
	// （容器内经 host.docker.internal 回达宿主；Linux 宿主由 DockerDriver
	// 的 --add-host host-gateway 注入解析，默认值开箱即用）。
	PublicURL string

	// InstancePublicHost 题目实例 endpoint 的主机名（学员浏览器直连靶机的
	// 地址方向，与 PublicURL 的容器回达方向相反）：空（默认）= 127.0.0.1
	// 且端口仅发布宿主回环（本地开发，浏览器就在宿主上）；部署到服务器时
	// 设为学员浏览器可达的宿主地址（如内网 IP），端口随之发布全网卡。
	InstancePublicHost string

	// YakBin yak 引擎可执行文件路径（ACM 编程题判题用；空 = 引擎未配置，
	// code 题判定 internal_error）。判题进程以本进程子进程运行（限时杀进程），
	// 生产部署应指向容器内 yak 二进制并配合最小权限运行。
	YakBin string
	// YakDownloadDir yak 引擎二进制的管理端 OSS 下载落盘目录（题目管理
	// ACM tab「从 OSS 下载引擎」；空 = SQLite 数据库所在目录）。下载成功
	// 后无需重启即装配；重启时自动探测该目录下的 yak 二进制。
	YakDownloadDir string

	// SmsShowCode 验证码回显模式：开启后 /auth/sms/code 响应携带验证码原文、
	// 注册页内联显示，学员可自助完成注册。仅供内网/演示部署——默认投递器只写
	// 服务日志，学员拿不到码会卡死在注册第一步；正式部署保持关闭。
	SmsShowCode bool
}

// 内置默认值仅用于本地开发；生产必须通过环境变量覆盖（main 启动时对照告警）。
const (
	DefaultJWTSecret     = "dev-only-secret"
	DefaultAdminUsername = "admin"
	DefaultAdminPassword = "admin123"
)

func Load() *Config {
	return &Config{
		Addr:               getenv("ACADEMY_ADDR", ":8080"),
		Database:           getenv("ACADEMY_DB", "academy.db"),
		JWTSecret:          getenv("ACADEMY_JWT_SECRET", DefaultJWTSecret),
		AdminUsername:      getenv("ACADEMY_ADMIN_USERNAME", DefaultAdminUsername),
		AdminPassword:      getenv("ACADEMY_ADMIN_PASSWORD", DefaultAdminPassword),
		EnvDriver:          getenv("ACADEMY_ENV_DRIVER", "auto"),
		AttachmentsDir:     getenv("ACADEMY_ATTACHMENTS_DIR", "attachments"),
		AvatarsDir:         getenv("ACADEMY_AVATARS_DIR", "avatars"),
		WriteupsDir:        getenv("ACADEMY_WRITEUPS_DIR", "writeups"),
		InstanceMemory:     getenv("ACADEMY_INSTANCE_MEMORY", "512m"),
		InstanceCPUs:       getenv("ACADEMY_INSTANCE_CPUS", "0.5"),
		PublicURL:          getenv("ACADEMY_PUBLIC_URL", "http://host.docker.internal:8080"),
		InstancePublicHost: getenv("ACADEMY_INSTANCE_PUBLIC_HOST", ""),
		YakBin:             getenv("ACADEMY_YAK_BIN", ""),
		YakDownloadDir:     getenv("ACADEMY_YAK_DIR", filepath.Dir(getenv("ACADEMY_DB", "academy.db"))),
		SmsShowCode:        getenvBool("ACADEMY_SMS_SHOW_CODE"),
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getenvBool 开关型环境变量："1"/"true"（大小写不敏感）为开，其余/未设置为关。
func getenvBool(key string) bool {
	switch v := os.Getenv(key); strings.ToLower(v) {
	case "1", "true":
		return true
	default:
		return false
	}
}
