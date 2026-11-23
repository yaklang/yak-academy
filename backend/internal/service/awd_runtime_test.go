package service

import (
	"strings"
	"testing"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

func argsContain(args []string, want ...string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		ok := true
		for j := range want {
			if args[i+j] != want[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// TestAwdTargetRunArgsHardening 靶机容器加固参数：内部网络、资源限额、
// cap-drop ALL + 最小能力集、无宿主端口/目录/套接字挂载。
func TestAwdTargetRunArgsHardening(t *testing.T) {
	runtime := NewDockerAwdRuntime()
	runtime.TargetMemory = "384m"
	runtime.TargetCPUs = "0.3"
	args := runtime.targetRunArgs(AwdTargetSpec{
		CompetitionID: 12, GameID: 7, ServiceID: 3,
		ServiceSlug: "web", Image: "nginx:alpine",
		CompetitorType: model.AwdCompetitorTeam, CompetitorID: 42,
	}, "yak-awd-12", "yak-awd-12-web-team-42")

	for _, want := range [][]string{
		{"--network", "yak-awd-12"},
		{"--network-alias", "yak-awd-12-web-team-42"},
		{"--label", "yak-academy.awd.competition=12"},
		{"--label", "yak-academy.awd.game=7"},
		{"--memory", "384m"},
		{"--cpus", "0.3"},
		{"--pids-limit", "256"},
		{"--cap-drop", "ALL"},
		{"--cap-add", "NET_BIND_SERVICE"},
		{"--security-opt", "no-new-privileges"},
	} {
		if !argsContain(args, want...) {
			t.Fatalf("target args missing %v: %v", want, args)
		}
	}
	joined := strings.Join(args, " ")
	for _, banned := range []string{"-v ", "docker.sock", " --privileged", "-p "} {
		if strings.Contains(joined, banned) {
			t.Fatalf("target args contain banned %q: %s", banned, joined)
		}
	}
}

// TestAwdAttackBoxRunArgsHardening 攻击盒：SSH 端口按配置绑定/随机宿主端口，
// 凭据走环境变量，cap-drop ALL 后只加回 sshd 登录必需能力。
func TestAwdAttackBoxRunArgsHardening(t *testing.T) {
	runtime := NewDockerAwdRuntime()
	runtime.AttackImage = "yak-awd-attackbox:test"
	runtime.AttackMemory = "128m"
	runtime.AttackCPUs = "0.2"
	args := runtime.attackBoxRunArgs(AwdAttackBoxSpec{
		CompetitionID: 12, GameID: 7,
		CompetitorType: model.AwdCompetitorUser, CompetitorID: 42,
		SSHUser: "awd42", SSHPassword: "s3cret",
	}, awdEdgeNetworkName(12), "yak-awd-box-12-user-42", "192.0.2.10", 22)

	for _, want := range [][]string{
		{"--network", "yak-awd-edge-12"},
		{"-p", "192.0.2.10::22"},
		{"-e", "USER_NAME=awd42"},
		{"-e", "USER_PASSWORD=s3cret"},
		{"--memory", "128m"},
		{"--cpus", "0.2"},
		{"--cap-drop", "ALL"},
		{"--cap-add", "SETUID"},
		{"--cap-add", "SYS_CHROOT"},
		{"--security-opt", "no-new-privileges"},
		{"yak-awd-attackbox:test"},
	} {
		if !argsContain(args, want...) {
			t.Fatalf("attack box args missing %v: %v", want, args)
		}
	}
	joined := strings.Join(args, " ")
	for _, banned := range []string{"-v ", "docker.sock", " --privileged"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("attack box args contain banned %q: %s", banned, joined)
		}
	}
}

// TestAwdRuntimeDefaultsLimits 配置留空时回落到默认资源限额，避免 docker run
// 收到空 --memory/--cpus 参数。
func TestAwdRuntimeDefaultsLimits(t *testing.T) {
	runtime := &DockerAwdRuntime{AttackImage: "img"}
	if got := awdDefault(runtime.TargetMemory, "512m"); got != "512m" {
		t.Fatalf("target memory default = %q", got)
	}
	args := runtime.targetRunArgs(AwdTargetSpec{Image: "img"}, "net", "name")
	if !argsContain(args, "--memory", "512m") || !argsContain(args, "--cpus", "0.5") {
		t.Fatalf("default target limits missing: %v", args)
	}
	attackArgs := runtime.attackBoxRunArgs(AwdAttackBoxSpec{SSHUser: "u", SSHPassword: "p"}, "net", "box", "0.0.0.0", 22)
	if !argsContain(attackArgs, "--memory", "256m") || !argsContain(attackArgs, "--cpus", "0.25") {
		t.Fatalf("default attack limits missing: %v", attackArgs)
	}
}
