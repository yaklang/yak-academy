package service

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 真实 Docker 集成测试：ACADEMY_TEST_DOCKER=1 时才执行（本地按需 / CI backend-docker
// job），默认 skip——常规单测保持零外部依赖（project-conventions skill：外部运行时
// 默认用手写 fake，本文件是对 DockerDriver 真实 CLI 路径的受控验证层）。

const dockerTestImage = "nginx:alpine"

// requireDocker 门控 + daemon 可用性探测，返回带超时的 ctx（拉镜像/起容器较慢）。
func requireDocker(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("ACADEMY_TEST_DOCKER") != "1" {
		t.Skip("需真实 Docker：设 ACADEMY_TEST_DOCKER=1 运行")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("docker daemon 不可用: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func dockerInspectFormat(ctx context.Context, t *testing.T, name, format string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, "docker", "inspect", name, "--format", format).CombinedOutput()
	if err != nil {
		t.Fatalf("docker inspect %s: %v: %s", name, err, out)
	}
	return strings.TrimSpace(string(out))
}

// waitEndpointReachable 轮询直到 endpoint 返回任意 HTTP 响应（容器内服务就绪）。
func waitEndpointReachable(t *testing.T, endpoint string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("endpoint %s 15s 内不可达", endpoint)
}

func containerGone(ctx context.Context, t *testing.T, name string) bool {
	t.Helper()
	out, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", "name=^/"+name+"$",
		"--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		t.Fatalf("docker ps: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out)) == ""
}

func TestDockerDriverCreateAppliesLimitsAndDestroys(t *testing.T) {
	ctx := requireDocker(t)
	driver := NewDockerDriver("256m", "0.25", "")
	const instanceID = 94201 // 高位编号，避免与其他测试容器撞名
	t.Cleanup(func() { _ = driver.Destroy(context.Background(), instanceID) })

	endpoint, err := driver.Create(ctx, InstanceSpec{
		InstanceID: instanceID, ChallengeID: 1, UserID: 7,
		Image: dockerTestImage, Port: 80,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatalf("endpoint = %q, want 127.0.0.1 随机端口", endpoint)
	}
	waitEndpointReachable(t, endpoint)

	name := containerName(instanceID)
	// 资源限额必须落到容器配置：--memory 256m = 268435456 字节；--cpus 0.25 = 250000000 NanoCPUs。
	if got := dockerInspectFormat(ctx, t, name, "{{.HostConfig.Memory}}"); got != "268435456" {
		t.Errorf("memory limit = %s, want 268435456", got)
	}
	if got := dockerInspectFormat(ctx, t, name, "{{.HostConfig.NanoCpus}}"); got != "250000000" {
		t.Errorf("nano_cpus = %s, want 250000000", got)
	}
	// --add-host host-gateway：容器内必须可解析 host.docker.internal
	// （Linux 宿主默认无此解析，CHECKPOINT_URL 回达依赖它；Docker Desktop 兼容）。
	if got := dockerInspectFormat(ctx, t, name, "{{.HostConfig.ExtraHosts}}"); !strings.Contains(got, "host.docker.internal:host-gateway") {
		t.Errorf("extra hosts = %q, want host.docker.internal:host-gateway", got)
	}
	if out, err := exec.CommandContext(ctx, "docker", "exec", name, "getent", "hosts", "host.docker.internal").CombinedOutput(); err != nil {
		t.Errorf("容器内解析 host.docker.internal: %v: %s", err, strings.TrimSpace(string(out)))
	}

	if err := driver.Destroy(ctx, instanceID); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if !containerGone(ctx, t, name) {
		t.Errorf("容器销毁后仍存在: %s", name)
	}
	// 幂等：销毁不存在的实例视为成功。
	if err := driver.Destroy(ctx, instanceID); err != nil {
		t.Errorf("重复销毁应幂等: %v", err)
	}
}

// TestDockerDriverCreatePublicHostPublishesAllInterfaces 对外主机名部署形态：
// 端口发布全网卡（0.0.0.0，非仅回环），endpoint 用配置的主机名拼装——
// 学员浏览器经服务器地址直连靶机（回归：endpoint 曾硬编码 127.0.0.1，
// 服务器部署后学员拿到的地址指向自己的机器，不可达）。
func TestDockerDriverCreatePublicHostPublishesAllInterfaces(t *testing.T) {
	ctx := requireDocker(t)
	const host = "192.0.2.10" // RFC 5737 文档地址：只验绑定与 endpoint 拼接，无需真实可达
	driver := NewDockerDriver("128m", "0.25", host)
	const instanceID = 94202
	t.Cleanup(func() { _ = driver.Destroy(context.Background(), instanceID) })

	endpoint, err := driver.Create(ctx, InstanceSpec{
		InstanceID: instanceID, ChallengeID: 1, UserID: 7,
		Image: dockerTestImage, Port: 80,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(endpoint, "http://"+host+":") {
		t.Fatalf("endpoint = %q, want 主机名 %s 而非回环地址", endpoint, host)
	}
	out, err := exec.CommandContext(ctx, "docker", "port", containerName(instanceID), "80").CombinedOutput()
	if err != nil {
		t.Fatalf("docker port: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "0.0.0.0:") || strings.Contains(string(out), "127.0.0.1:") {
		t.Fatalf("端口绑定 = %q, want 全网卡 0.0.0.0（非仅回环）", strings.TrimSpace(string(out)))
	}
}

func TestDockerDriverBuild(t *testing.T) {
	ctx := requireDocker(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM "+dockerTestImage+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const tag = "yak-academy-test/build-probe"
	t.Cleanup(func() {
		_, _ = exec.CommandContext(context.Background(), "docker", "rmi", tag).CombinedOutput()
	})
	if err := (DockerDriver{}).Build(ctx, tag, dir); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := exec.CommandContext(ctx, "docker", "image", "inspect", tag).Run(); err != nil {
		t.Errorf("build 后镜像不存在: %v", err)
	}
}

// TestDockerEnsureInstanceFullSlice 走一条完整真实链路：EnsureInstance（DB 记录 →
// docker run → running + endpoint + TTL）→ 容器真实可访问 → DestroyInstance（容器消失）。
func TestDockerEnsureInstanceFullSlice(t *testing.T) {
	ctx := requireDocker(t)
	gdb, err := gorm.Open(sqlite.Open("file:envdockertest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	chalRepo := repo.NewChallengeRepo(gdb)
	if err := chalRepo.Upsert(ctx, &model.Challenge{
		Slug: "chal-docker-slice", Title: "Docker 集成题", Type: model.ChallengeStatic, Score: 100,
		Image: dockerTestImage, Port: 80,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{docker}"}}, nil); err != nil {
		t.Fatal(err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "chal-docker-slice").First(&chal).Error; err != nil {
		t.Fatal(err)
	}

	driver := NewDockerDriver("128m", "0.2", "")
	env := NewEnvService(
		repo.NewLabInstanceRepo(gdb), repo.NewInstanceLogRepo(gdb), chalRepo,
		repo.NewUserRepo(gdb), repo.NewEventRepo(gdb), eventbus.New(), driver,
		"http://127.0.0.1:18080",
	)
	const userID = 901
	t.Cleanup(func() {
		_ = env.DestroyInstance(context.Background(), userID, chal.ID)
	})

	inst, err := env.EnsureInstance(ctx, userID, chal.ID)
	if err != nil {
		t.Fatalf("ensure instance: %v", err)
	}
	t.Cleanup(func() { _ = driver.Destroy(context.Background(), inst.ID) })
	if inst.Status != model.InstanceRunning {
		t.Fatalf("status = %s, want running", inst.Status)
	}
	if inst.ExpireAt.IsZero() || !inst.ExpireAt.After(time.Now()) {
		t.Errorf("expire_at 未设置或已过期: %v", inst.ExpireAt)
	}
	if inst.CheckpointToken == "" {
		t.Errorf("检测点 token 未生成")
	}
	waitEndpointReachable(t, inst.Endpoint)

	if err := env.DestroyInstance(ctx, userID, chal.ID); err != nil {
		t.Fatalf("destroy instance: %v", err)
	}
	if !containerGone(ctx, t, containerName(inst.ID)) {
		t.Errorf("实例销毁后容器仍存在: %s", containerName(inst.ID))
	}
}

// TestDockerHostStats 性能面板真实路径：docker info/stats 解析 + /proc 采样
//（Linux 部署形态下宿主全局视图）。
func TestDockerHostStats(t *testing.T) {
	ctx := requireDocker(t)
	driver := NewDockerDriver("", "", "")
	stats, err := driver.HostStats(ctx)
	if err != nil {
		t.Fatalf("HostStats: %v", err)
	}
	if stats.NCPU < 1 {
		t.Errorf("NCPU = %d, want >= 1", stats.NCPU)
	}
	if stats.MemTotalBytes <= 0 {
		t.Errorf("MemTotalBytes = %d, want > 0", stats.MemTotalBytes)
	}
	if stats.ContainersTotal < 1 {
		t.Errorf("ContainersTotal = %d（docker daemon 至少有测试自身容器）", stats.ContainersTotal)
	}
	for _, c := range stats.Containers {
		if c.ID == "" || c.Name == "" {
			t.Errorf("容器行缺 ID/Name: %+v", c)
		}
		if c.MemLimitBytes < c.MemUsedBytes {
			t.Errorf("容器 %s 内存超限: used %d > limit %d", c.Name, c.MemUsedBytes, c.MemLimitBytes)
		}
	}
	if stats.Host == nil {
		t.Fatalf("Host 段缺失")
	}
}
