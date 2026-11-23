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
	// TargetMemory/TargetCPUs 靶机资源限额（docker --memory/--cpus 语法）。
	TargetMemory string
	TargetCPUs   string
	// AttackMemory/AttackCPUs 攻击盒资源限额。
	AttackMemory string
	AttackCPUs   string
}

// awdTargetCaps 靶机最小能力集：cap-drop ALL 后只保留运行常见 Web 服务所需
// （bind 80、nginx/apache 降权到非 root）。不开放其他能力。
var awdTargetCaps = []string{"NET_BIND_SERVICE", "SETUID", "SETGID", "CHOWN", "DAC_OVERRIDE"}

// awdAttackBoxCaps SSH 攻击盒额外能力：sshd 登录需要切换用户、chroot、pty
// 属主调整与 wtmp 写入；选手侧工具（tcpdump/nmap SYN 扫描）需要 NET_RAW。
// 仍 cap-drop ALL 后按最小集逐项加回。
var awdAttackBoxCaps = []string{
	"NET_BIND_SERVICE", "SETUID", "SETGID", "CHOWN", "FOWNER", "DAC_OVERRIDE",
	"SYS_CHROOT", "KILL", "AUDIT_WRITE", "NET_RAW",
}

func NewDockerAwdRuntime() *DockerAwdRuntime {
	return &DockerAwdRuntime{
		Binary: "docker", AttackImage: "yak-awd-attackbox:latest", AttackSSHPort: 22,
		SSHBindHost: "0.0.0.0", PublicHost: "127.0.0.1",
		TargetMemory: "512m", TargetCPUs: "0.5",
		AttackMemory: "256m", AttackCPUs: "0.25",
	}
}

func awdDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func awdCapArgs(caps []string) []string {
	args := []string{"--cap-drop", "ALL"}
	for _, capability := range caps {
		args = append(args, "--cap-add", capability)
	}
	return args
}

func awdNetworkName(competitionID uint) string {
	return fmt.Sprintf("yak-awd-%d", competitionID)
}

// awdEdgeNetworkName 攻击盒的对外网络：Docker 不会为只接入 --internal 网络的
// 容器建立端口发布规则（moby#36174），因此攻击盒需要同时接入普通 edge 网络
// 才能发布 SSH 端口；靶机仍然只接内部网络。
func awdEdgeNetworkName(competitionID uint) string {
	return fmt.Sprintf("yak-awd-edge-%d", competitionID)
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

func (r *DockerAwdRuntime) ensureNetwork(ctx context.Context, competitionID uint, name string, internal bool) (string, error) {
	args := []string{"network", "create"}
	if internal {
		args = append(args, "--internal")
	}
	args = append(args, "--label",
		fmt.Sprintf("yak-academy.awd.competition=%d", competitionID), name)
	out, err := r.run(ctx, args...)
	if err == nil {
		return name, nil
	}
	// 已存在视为成功；其它错误透出。
	if strings.Contains(out, "already exists") {
		return name, nil
	}
	return "", err
}

func (r *DockerAwdRuntime) EnsureNetwork(ctx context.Context, competitionID uint) (string, error) {
	return r.ensureNetwork(ctx, competitionID, awdNetworkName(competitionID), true)
}

func (r *DockerAwdRuntime) ensureEdgeNetwork(ctx context.Context, competitionID uint) (string, error) {
	return r.ensureNetwork(ctx, competitionID, awdEdgeNetworkName(competitionID), false)
}

// targetRunArgs 靶机 docker run 参数（纯函数便于单测断言加固项）。
func (r *DockerAwdRuntime) targetRunArgs(spec AwdTargetSpec, network, name string) []string {
	args := []string{
		"run", "-d",
		"--name", name,
		"--hostname", name,
		"--network", network,
		"--network-alias", name,
		"--label", fmt.Sprintf("yak-academy.awd.competition=%d", spec.CompetitionID),
		"--label", fmt.Sprintf("yak-academy.awd.game=%d", spec.GameID),
		"--label", fmt.Sprintf("yak-academy.awd.service=%d", spec.ServiceID),
		"--label", fmt.Sprintf("yak-academy.awd.competitor=%s-%d", spec.CompetitorType, spec.CompetitorID),
		"--memory", awdDefault(r.TargetMemory, "512m"),
		"--cpus", awdDefault(r.TargetCPUs, "0.5"),
		"--pids-limit", "256",
	}
	args = append(args, awdCapArgs(awdTargetCaps)...)
	args = append(args, "--security-opt", "no-new-privileges", "--restart", "no", spec.Image)
	return args
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
	out, err := r.run(ctx, r.targetRunArgs(spec, network, name)...)
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

// attackBoxRunArgs 攻击盒 docker run 参数（纯函数便于单测断言加固项）。
// 攻击盒在 edge 网络上创建以发布 SSH 端口，创建后再接入竞赛内部网络。
func (r *DockerAwdRuntime) attackBoxRunArgs(spec AwdAttackBoxSpec, edgeNetwork, name, bind string, containerPort int) []string {
	args := []string{
		"run", "-d",
		"--name", name,
		"--hostname", name,
		"--network", edgeNetwork,
		"--label", fmt.Sprintf("yak-academy.awd.competition=%d", spec.CompetitionID),
		"--label", "yak-academy.awd.role=attackbox",
		"--label", fmt.Sprintf("yak-academy.awd.competitor=%s-%d", spec.CompetitorType, spec.CompetitorID),
		"-p", fmt.Sprintf("%s::%d", bind, containerPort),
		"-e", "PASSWORD_ACCESS=true",
		"-e", "SUDO_ACCESS=false",
		"-e", "USER_NAME=" + spec.SSHUser,
		"-e", "USER_PASSWORD=" + spec.SSHPassword,
		"--memory", awdDefault(r.AttackMemory, "256m"),
		"--cpus", awdDefault(r.AttackCPUs, "0.25"),
		"--pids-limit", "128",
	}
	args = append(args, awdCapArgs(awdAttackBoxCaps)...)
	args = append(args, "--security-opt", "no-new-privileges", "--restart", "no", r.AttackImage)
	return args
}

func (r *DockerAwdRuntime) CreateAttackBox(ctx context.Context, spec AwdAttackBoxSpec) (AwdAttackBoxTarget, error) {
	if r.AttackImage == "" {
		return AwdAttackBoxTarget{}, fmt.Errorf("%w: AWD attack box image is not configured", ErrValidation)
	}
	if strings.TrimSpace(spec.SSHUser) == "" || strings.TrimSpace(spec.SSHPassword) == "" {
		return AwdAttackBoxTarget{}, fmt.Errorf("%w: SSH credentials are required", ErrValidation)
	}
	gameNetwork, err := r.EnsureNetwork(ctx, spec.CompetitionID)
	if err != nil {
		return AwdAttackBoxTarget{}, err
	}
	edgeNetwork, err := r.ensureEdgeNetwork(ctx, spec.CompetitionID)
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
	out, err := r.run(ctx, r.attackBoxRunArgs(spec, edgeNetwork, name, bind, containerPort)...)
	if err != nil {
		return AwdAttackBoxTarget{}, err
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return AwdAttackBoxTarget{}, fmt.Errorf("docker run returned empty attack box container id")
	}
	// 创建后再接入竞赛内部网络：既能通过内部 DNS 访问靶机，又保留 edge 上的 SSH 端口发布。
	if _, err := r.run(ctx, "network", "connect", gameNetwork, name); err != nil {
		_ = r.DestroyTarget(ctx, id)
		return AwdAttackBoxTarget{}, fmt.Errorf("connect attack box to game network: %w", err)
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
	var firstErr error
	for _, network := range []string{awdNetworkName(competitionID), awdEdgeNetworkName(competitionID)} {
		if _, err := r.run(ctx, "network", "rm", network); err != nil &&
			!strings.Contains(err.Error(), "not found") && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
