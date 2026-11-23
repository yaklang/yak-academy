package service

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// AwdRuntime AWD 靶机运行时抽象（Docker 单机 v1；K8s/浏览器终端后续实现）。
type AwdRuntime interface {
	EnsureNetwork(ctx context.Context, competitionID uint) (string, error)
	CreateTarget(ctx context.Context, spec AwdTargetSpec) (AwdTarget, error)
	DestroyTarget(ctx context.Context, containerID string) error
	CreateAttackBox(ctx context.Context, spec AwdAttackBoxSpec) (AwdAttackBoxTarget, error)
	DestroyAttackBox(ctx context.Context, containerID string) error
	DestroyGame(ctx context.Context, competitionID uint) error
}

// AwdTargetSpec 单个竞域单位靶机服务部署参数。
type AwdTargetSpec struct {
	CompetitionID  uint
	GameID         uint
	ServiceID      uint
	ServiceSlug    string
	Image          string
	CompetitorType model.AwdCompetitorType
	CompetitorID   uint
}

// AwdTarget 部署结果（容器 ID + 竞赛内部网络中的稳定主机名）。
type AwdTarget struct {
	ContainerID  string
	InternalHost string
}

// AwdAttackBoxSpec SSH 攻击盒部署参数。
type AwdAttackBoxSpec struct {
	CompetitionID  uint
	GameID         uint
	CompetitorType model.AwdCompetitorType
	CompetitorID   uint
	SSHUser        string
	SSHPassword    string
}

// AwdAttackBoxTarget 攻击盒部署结果（SSH 连接信息）。
type AwdAttackBoxTarget struct {
	ContainerID string
	SSHHost     string
	SSHPort     int
}

// NoopAwdRuntime 未装配 Docker 时的占位实现（AWD 部署返回 422，不影响 CTF）。
type NoopAwdRuntime struct{}

func NewNoopAwdRuntime() NoopAwdRuntime { return NoopAwdRuntime{} }

func (NoopAwdRuntime) EnsureNetwork(context.Context, uint) (string, error) {
	return "", fmt.Errorf("%w: AWD docker runtime is not available", ErrValidation)
}
func (NoopAwdRuntime) CreateTarget(context.Context, AwdTargetSpec) (AwdTarget, error) {
	return AwdTarget{}, fmt.Errorf("%w: AWD docker runtime is not available", ErrValidation)
}
func (NoopAwdRuntime) DestroyTarget(context.Context, string) error {
	return fmt.Errorf("%w: AWD docker runtime is not available", ErrValidation)
}
func (NoopAwdRuntime) CreateAttackBox(context.Context, AwdAttackBoxSpec) (AwdAttackBoxTarget, error) {
	return AwdAttackBoxTarget{}, fmt.Errorf("%w: AWD docker runtime is not available", ErrValidation)
}
func (NoopAwdRuntime) DestroyAttackBox(context.Context, string) error {
	return fmt.Errorf("%w: AWD docker runtime is not available", ErrValidation)
}
func (NoopAwdRuntime) DestroyGame(context.Context, uint) error {
	return fmt.Errorf("%w: AWD docker runtime is not available", ErrValidation)
}

// DockerAwdRuntime Docker CLI 实现：每个竞赛一个 --internal 内部网络；
// 靶机容器只接入该网络，不发布宿主端口、不挂 Docker socket/宿主目录。
type DockerAwdRuntime struct {
	// Binary docker 可执行文件（默认 docker；测试/部署可覆盖）。
	Binary string
	// AttackImage SSH 攻击盒镜像（默认 yak-awd-attackbox:latest，需部署侧构建）。
	AttackImage string
	// AttackSSHPort 攻击盒容器内 sshd 端口（默认 22）。
	AttackSSHPort int
	// SSHBindHost 宿主发布地址（默认 0.0.0.0；生产建议配合防火墙限制）。
	SSHBindHost string
	// PublicHost 返回给选手的 SSH 主机名（默认 127.0.0.1）。
	PublicHost string
}

func NewDockerAwdRuntime() *DockerAwdRuntime {
	return &DockerAwdRuntime{
		Binary: "docker", AttackImage: "yak-awd-attackbox:latest", AttackSSHPort: 22,
		SSHBindHost: "0.0.0.0", PublicHost: "127.0.0.1",
	}
}

func awdNetworkName(competitionID uint) string {
	return fmt.Sprintf("yak-awd-%d", competitionID)
}

var awdNameUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

func awdContainerName(spec AwdTargetSpec) string {
	raw := fmt.Sprintf("yak-awd-%d-%s-%s-%d", spec.CompetitionID, spec.ServiceSlug, spec.CompetitorType, spec.CompetitorID)
	name := strings.ToLower(awdNameUnsafe.ReplaceAllString(raw, "-"))
	name = strings.Trim(name, "-")
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.Trim(name, "-")
}

func (r *DockerAwdRuntime) run(ctx context.Context, args ...string) (string, error) {
	bin := r.Binary
	if bin == "" {
		bin = "docker"
	}
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("docker %s: %v: %s", strings.Join(args, " "), err, sanitizeToolOutput(text))
	}
	return text, nil
}

func (r *DockerAwdRuntime) EnsureNetwork(ctx context.Context, competitionID uint) (string, error) {
	name := awdNetworkName(competitionID)
	out, err := r.run(ctx, "network", "create", "--internal", "--label",
		fmt.Sprintf("yak-academy.awd.competition=%d", competitionID), name)
	if err == nil {
		return name, nil
	}
	// 已存在视为成功；其它错误透出。
	if strings.Contains(out, "already exists") {
		return name, nil
	}
	return "", err
}

func (r *DockerAwdRuntime) CreateTarget(ctx context.Context, spec AwdTargetSpec) (AwdTarget, error) {
	network, err := r.EnsureNetwork(ctx, spec.CompetitionID)
	if err != nil {
		return AwdTarget{}, err
	}
	name := awdContainerName(spec)
	if spec.Image == "" {
		return AwdTarget{}, fmt.Errorf("%w: target image is empty", ErrValidation)
	}
	// 同名旧容器先清理（失败忽略：不存在/已停止）。
	_, _ = r.run(ctx, "rm", "-f", name)
	out, err := r.run(ctx,
		"run", "-d",
		"--name", name,
		"--hostname", name,
		"--network", network,
		"--network-alias", name,
		"--label", fmt.Sprintf("yak-academy.awd.competition=%d", spec.CompetitionID),
		"--label", fmt.Sprintf("yak-academy.awd.game=%d", spec.GameID),
		"--label", fmt.Sprintf("yak-academy.awd.service=%d", spec.ServiceID),
		"--label", fmt.Sprintf("yak-academy.awd.competitor=%s-%d", spec.CompetitorType, spec.CompetitorID),
		"--memory", "512m",
		"--cpus", "0.5",
		"--pids-limit", "256",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--restart", "no",
		spec.Image,
	)
	if err != nil {
		return AwdTarget{}, err
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return AwdTarget{}, fmt.Errorf("docker run returned empty container id")
	}
	return AwdTarget{ContainerID: id, InternalHost: name}, nil
}

func (r *DockerAwdRuntime) DestroyTarget(ctx context.Context, containerID string) error {
	if strings.TrimSpace(containerID) == "" {
		return nil
	}
	_, err := r.run(ctx, "rm", "-f", containerID)
	return err
}

func awdAttackBoxName(spec AwdAttackBoxSpec) string {
	raw := fmt.Sprintf("yak-awd-box-%d-%s-%d", spec.CompetitionID, spec.CompetitorType, spec.CompetitorID)
	name := strings.ToLower(awdNameUnsafe.ReplaceAllString(raw, "-"))
	return strings.Trim(name, "-")
}

func (r *DockerAwdRuntime) CreateAttackBox(ctx context.Context, spec AwdAttackBoxSpec) (AwdAttackBoxTarget, error) {
	if r.AttackImage == "" {
		return AwdAttackBoxTarget{}, fmt.Errorf("%w: AWD attack box image is not configured", ErrValidation)
	}
	if strings.TrimSpace(spec.SSHUser) == "" || strings.TrimSpace(spec.SSHPassword) == "" {
		return AwdAttackBoxTarget{}, fmt.Errorf("%w: SSH credentials are required", ErrValidation)
	}
	network, err := r.EnsureNetwork(ctx, spec.CompetitionID)
	if err != nil {
		return AwdAttackBoxTarget{}, err
	}
	name := awdAttackBoxName(spec)
	containerPort := r.AttackSSHPort
	if containerPort <= 0 {
		containerPort = 22
	}
	bind := r.SSHBindHost
	if bind == "" {
		bind = "0.0.0.0"
	}
	_, _ = r.run(ctx, "rm", "-f", name)
	out, err := r.run(ctx,
		"run", "-d",
		"--name", name,
		"--hostname", name,
		"--network", network,
		"--label", fmt.Sprintf("yak-academy.awd.competition=%d", spec.CompetitionID),
		"--label", "yak-academy.awd.role=attackbox",
		"--label", fmt.Sprintf("yak-academy.awd.competitor=%s-%d", spec.CompetitorType, spec.CompetitorID),
		"-p", fmt.Sprintf("%s::%d", bind, containerPort),
		"-e", "PASSWORD_ACCESS=true",
		"-e", "SUDO_ACCESS=false",
		"-e", "USER_NAME="+spec.SSHUser,
		"-e", "USER_PASSWORD="+spec.SSHPassword,
		"--memory", "256m",
		"--cpus", "0.25",
		"--pids-limit", "128",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--restart", "no",
		r.AttackImage,
	)
	if err != nil {
		return AwdAttackBoxTarget{}, err
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return AwdAttackBoxTarget{}, fmt.Errorf("docker run returned empty attack box container id")
	}
	portOut, err := r.run(ctx, "port", name, fmt.Sprintf("%d/tcp", containerPort))
	if err != nil {
		_ = r.DestroyTarget(ctx, id)
		return AwdAttackBoxTarget{}, err
	}
	port, err := parseDockerPublishedPort(portOut)
	if err != nil {
		_ = r.DestroyTarget(ctx, id)
		return AwdAttackBoxTarget{}, err
	}
	host := r.PublicHost
	if host == "" {
		host = "127.0.0.1"
	}
	return AwdAttackBoxTarget{ContainerID: id, SSHHost: host, SSHPort: port}, nil
}

func (r *DockerAwdRuntime) DestroyAttackBox(ctx context.Context, containerID string) error {
	return r.DestroyTarget(ctx, containerID)
}

func parseDockerPublishedPort(out string) (int, error) {
	// docker port 输出如 "0.0.0.0:49153" / "[::]:49153"；取最后一个 : 后的端口。
	line := strings.TrimSpace(strings.Split(out, "\n")[0])
	idx := strings.LastIndex(line, ":")
	if idx < 0 || idx == len(line)-1 {
		return 0, fmt.Errorf("cannot parse docker port output %q", out)
	}
	port, err := strconv.Atoi(line[idx+1:])
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid docker published port %q", line)
	}
	return port, nil
}

func (r *DockerAwdRuntime) DestroyGame(ctx context.Context, competitionID uint) error {
	out, err := r.run(ctx, "ps", "-aq", "--filter",
		fmt.Sprintf("label=yak-academy.awd.competition=%d", competitionID))
	if err == nil && strings.TrimSpace(out) != "" {
		ids := strings.Fields(out)
		args := append([]string{"rm", "-f"}, ids...)
		if _, err := r.run(ctx, args...); err != nil {
			return err
		}
	}
	_, err = r.run(ctx, "network", "rm", awdNetworkName(competitionID))
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return err
	}
	return nil
}
