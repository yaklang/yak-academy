package service

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// AwdRuntime AWD 靶机运行时抽象（Docker 单机 v1；K8s/浏览器终端后续实现）。
type AwdRuntime interface {
	EnsureNetwork(ctx context.Context, competitionID uint) (string, error)
	CreateTarget(ctx context.Context, spec AwdTargetSpec) (AwdTarget, error)
	DestroyTarget(ctx context.Context, containerID string) error
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
func (NoopAwdRuntime) DestroyGame(context.Context, uint) error {
	return fmt.Errorf("%w: AWD docker runtime is not available", ErrValidation)
}

// DockerAwdRuntime Docker CLI 实现：每个竞赛一个 --internal 内部网络；
// 靶机容器只接入该网络，不发布宿主端口、不挂 Docker socket/宿主目录。
type DockerAwdRuntime struct {
	// Binary docker 可执行文件（默认 docker；测试/部署可覆盖）。
	Binary string
}

func NewDockerAwdRuntime() *DockerAwdRuntime {
	return &DockerAwdRuntime{Binary: "docker"}
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
