package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 真实 Docker 门控测试：AWD 靶机隔离/加固与 SSH 攻击盒。默认 skip，
// ACADEMY_TEST_DOCKER=1 时在 CI backend-docker job / 本地手动执行。

func awdDockerCleanup(t *testing.T, runtime *DockerAwdRuntime, competitionID uint) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = runtime.DestroyGame(ctx, competitionID)
	})
}

func waitAwdHTTPInsideContainer(ctx context.Context, t *testing.T, name, url string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.CommandContext(ctx, "docker", "exec", name, "wget", "-qO-", url).Run(); err == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("容器 %s 内 %s 20s 内不可达", name, url)
}

// TestDockerAwdRuntimeTargetHardening 靶机容器真实创建：内部网络、无宿主端口、
// 无挂载、资源限额与最小能力集；nginx 在加固参数下仍可启动并响应。
func TestDockerAwdRuntimeTargetHardening(t *testing.T) {
	ctx := requireDocker(t)
	runtime := NewDockerAwdRuntime()
	runtime.TargetMemory = "128m"
	runtime.TargetCPUs = "0.25"
	const competitionID = 94311
	awdDockerCleanup(t, runtime, competitionID)

	network, err := runtime.EnsureNetwork(ctx, competitionID)
	if err != nil {
		t.Fatalf("ensure network: %v", err)
	}
	if network != awdNetworkName(competitionID) {
		t.Fatalf("network = %q, want %q", network, awdNetworkName(competitionID))
	}
	if got := dockerInspectFormat(ctx, t, network, "{{.Internal}}"); got != "true" {
		t.Fatalf("network internal = %q, want true", got)
	}

	target, err := runtime.CreateTarget(ctx, AwdTargetSpec{
		CompetitionID: competitionID, GameID: 1, ServiceID: 2,
		ServiceSlug: "web", Image: dockerTestImage,
		CompetitorType: model.AwdCompetitorUser, CompetitorID: 7,
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	name := awdContainerName(AwdTargetSpec{
		CompetitionID: competitionID, ServiceSlug: "web",
		CompetitorType: model.AwdCompetitorUser, CompetitorID: 7,
	})
	if target.InternalHost != name || target.ContainerID == "" {
		t.Fatalf("target = %+v, want host %s", target, name)
	}
	waitAwdHTTPInsideContainer(ctx, t, name, "http://127.0.0.1/")

	if got := dockerInspectFormat(ctx, t, name, "{{.HostConfig.Memory}}"); got != "134217728" {
		t.Errorf("memory limit = %s, want 134217728", got)
	}
	if got := dockerInspectFormat(ctx, t, name, "{{.HostConfig.NanoCpus}}"); got != "250000000" {
		t.Errorf("nano cpus = %s, want 250000000", got)
	}
	if got := dockerInspectFormat(ctx, t, name, "{{.HostConfig.NetworkMode}}"); got != network {
		t.Errorf("network mode = %s, want %s", got, network)
	}
	if got := dockerInspectFormat(ctx, t, name, "{{json .HostConfig.PortBindings}}"); got != "{}" && got != "null" {
		t.Errorf("port bindings = %s, want none", got)
	}
	if got := dockerInspectFormat(ctx, t, name, "{{json .Mounts}}"); got != "[]" && got != "null" {
		t.Errorf("mounts = %s, want none", got)
	}
	capDrop := dockerInspectFormat(ctx, t, name, "{{json .HostConfig.CapDrop}}")
	if !strings.Contains(capDrop, "ALL") {
		t.Errorf("cap drop = %s, want ALL", capDrop)
	}
	capAdd := dockerInspectFormat(ctx, t, name, "{{json .HostConfig.CapAdd}}")
	if !strings.Contains(capAdd, "NET_BIND_SERVICE") {
		t.Errorf("cap add = %s, want NET_BIND_SERVICE", capAdd)
	}
	if got := dockerInspectFormat(ctx, t, name, "{{.HostConfig.Privileged}}"); got != "false" {
		t.Errorf("privileged = %s, want false", got)
	}

	if err := runtime.DestroyGame(ctx, competitionID); err != nil {
		t.Fatalf("destroy game: %v", err)
	}
	if !containerGone(ctx, t, name) {
		t.Errorf("target container still exists after destroy")
	}
	if err := exec.CommandContext(ctx, "docker", "network", "inspect", network).Run(); err == nil {
		t.Errorf("awd network still exists after destroy")
	}
}

// TestDockerAwdRuntimeAttackBoxSSH 构建仓库内的攻击盒镜像，验证 SSH 密码登录、
// 内部网络内访问靶机、无宿主目录/Docker socket 挂载。
func TestDockerAwdRuntimeAttackBoxSSH(t *testing.T) {
	ctx := requireDocker(t)
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	dockerfile := filepath.Join(repoRoot, "deploy", "awd-attackbox.Dockerfile")
	if _, err := os.Stat(dockerfile); err != nil {
		t.Fatalf("attack box Dockerfile missing: %v", err)
	}
	tag := fmt.Sprintf("yak-academy-test/awd-attackbox:%d", os.Getpid())
	t.Cleanup(func() {
		_, _ = exec.CommandContext(context.Background(), "docker", "rmi", "-f", tag).CombinedOutput()
	})
	build := exec.CommandContext(ctx, "docker", "build", "-f", dockerfile, "-t", tag, repoRoot)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build attack box image: %v: %s", err, out)
	}

	runtime := NewDockerAwdRuntime()
	runtime.AttackImage = tag
	runtime.SSHBindHost = "127.0.0.1"
	runtime.PublicHost = "127.0.0.1"
	runtime.AttackMemory = "128m"
	runtime.AttackCPUs = "0.25"
	const competitionID = 94312
	awdDockerCleanup(t, runtime, competitionID)

	target, err := runtime.CreateTarget(ctx, AwdTargetSpec{
		CompetitionID: competitionID, GameID: 1, ServiceID: 2,
		ServiceSlug: "web", Image: dockerTestImage,
		CompetitorType: model.AwdCompetitorUser, CompetitorID: 7,
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	waitAwdHTTPInsideContainer(ctx, t, target.InternalHost, "http://127.0.0.1/")

	box, err := runtime.CreateAttackBox(ctx, AwdAttackBoxSpec{
		CompetitionID: competitionID, GameID: 1,
		CompetitorType: model.AwdCompetitorUser, CompetitorID: 7,
		SSHUser: "awd7", SSHPassword: "awd-test-pass",
	})
	if err != nil {
		t.Fatalf("create attack box: %v", err)
	}
	boxName := awdAttackBoxName(AwdAttackBoxSpec{
		CompetitionID: competitionID, CompetitorType: model.AwdCompetitorUser, CompetitorID: 7,
	})
	if box.SSHHost != "127.0.0.1" || box.SSHPort <= 0 {
		t.Fatalf("attack box address = %s:%d", box.SSHHost, box.SSHPort)
	}
	if got := dockerInspectFormat(ctx, t, boxName, "{{json .HostConfig.PortBindings}}"); !strings.Contains(got, "127.0.0.1") {
		t.Errorf("attack box port bindings = %s, want 127.0.0.1", got)
	}
	if got := dockerInspectFormat(ctx, t, boxName, "{{json .Mounts}}"); got != "[]" && got != "null" {
		t.Errorf("attack box mounts = %s, want none", got)
	}
	if got := dockerInspectFormat(ctx, t, boxName, "{{json .HostConfig.CapAdd}}"); !strings.Contains(got, "SYS_CHROOT") {
		t.Errorf("attack box cap add = %s, want SYS_CHROOT", got)
	}

	addr := net.JoinHostPort(box.SSHHost, strconv.Itoa(box.SSHPort))
	client := dialAwdAttackBox(t, addr, "awd7", "awd-test-pass")
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("ssh session: %v", err)
	}
	defer session.Close()
	out, err := session.Output("id -u && curl -fsS http://" + target.InternalHost + "/")
	if err != nil {
		t.Fatalf("ssh command: %v", err)
	}
	if !strings.Contains(string(out), "Welcome to nginx") {
		t.Fatalf("target not reachable from attack box: %s", out)
	}
}

// dialAwdAttackBox 轮询 SSH 密码登录（容器启动/sshd 就绪有延迟）。
func dialAwdAttackBox(t *testing.T, addr, user, password string) *ssh.Client {
	t.Helper()
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         3 * time.Second,
	}
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		client, err := ssh.Dial("tcp", addr, config)
		if err == nil {
			return client
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("SSH %s 30s 内不可登录: %v", addr, lastErr)
	return nil
}
