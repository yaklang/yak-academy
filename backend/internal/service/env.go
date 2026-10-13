package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// InstanceSpec 驱动创建实例所需的全部信息（驱动实现不感知 DB）。
type InstanceSpec struct {
	InstanceID  uint
	ChallengeID uint
	UserID      uint
	Image       string
	Port        int
}

// InstanceDriver 题目环境驱动接口：实现只与运行时（Docker 等）交互，
// DB 状态由 EnvService 维护（project-conventions skill）。
type InstanceDriver interface {
	// Create 按 spec 启动实例并返回访问地址。
	Create(ctx context.Context, spec InstanceSpec) (endpoint string, err error)
	// Destroy 按 InstanceID 销毁实例（幂等：实例不存在视为成功）。
	Destroy(ctx context.Context, instanceID uint) error
}

// NoopDriver 占位驱动：未配置容器运行时的 stub，调用一律报错。
type NoopDriver struct{}

func (NoopDriver) Create(ctx context.Context, spec InstanceSpec) (string, error) {
	return "", errNotImplemented("env driver create")
}
func (NoopDriver) Destroy(ctx context.Context, instanceID uint) error {
	return errNotImplemented("env driver destroy")
}

const (
	// instanceTTL 实例存活时长：超时由 reaper 回收。
	instanceTTL = time.Hour
	// maxInstancesPerUser 每用户活跃（creating/running）实例上限。
	maxInstancesPerUser = 2
)

type EnvService struct {
	instances  repo.LabInstanceRepo
	challenges repo.ChallengeRepo
	events     repo.EventRepo
	bus        *eventbus.Bus
	driver     InstanceDriver
	// now 时钟注入，测试可固定时间；生产用 time.Now。
	now func() time.Time
}

func NewEnvService(instances repo.LabInstanceRepo, challenges repo.ChallengeRepo, events repo.EventRepo, bus *eventbus.Bus, d InstanceDriver) *EnvService {
	return &EnvService{
		instances: instances, challenges: challenges, events: events, bus: bus,
		driver: d, now: time.Now,
	}
}

// EnsureInstance 幂等启动：已有 running 实例直接返回；
// 无则按题目 env 定义创建（并发上限校验 → DB 落 creating → driver.Create → 回写 running）。
// driver 失败回滚 DB 记录（status→stopped），不留半成品。
func (s *EnvService) EnsureInstance(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error) {
	if inst, err := s.instances.FindActive(ctx, userID, challengeID); err != nil {
		return nil, err
	} else if inst != nil {
		return inst, nil
	}
	chal, err := s.challenges.FindByID(ctx, challengeID)
	if err != nil {
		return nil, err
	}
	if chal.Image == "" || chal.Port < 1 {
		return nil, fmt.Errorf("%w: lab has no container environment", ErrValidation)
	}
	active, err := s.instances.CountActive(ctx, userID)
	if err != nil {
		return nil, err
	}
	if active >= maxInstancesPerUser {
		return nil, fmt.Errorf("%w: active instance limit reached (%d)", ErrValidation, maxInstancesPerUser)
	}
	inst := &model.LabInstance{
		UserID: userID, ChallengeID: challengeID,
		Status: model.InstanceCreating,
	}
	if err := s.instances.Create(ctx, inst); err != nil {
		return nil, err
	}
	spec := InstanceSpec{
		InstanceID: inst.ID, ChallengeID: challengeID, UserID: userID,
		Image: chal.Image, Port: chal.Port,
	}
	endpoint, err := s.driver.Create(ctx, spec)
	if err != nil {
		// 回滚：DB 记录标记 stopped，不留 creating 残留。
		if uerr := s.instances.UpdateStatus(ctx, inst.ID, model.InstanceStopped); uerr != nil {
			log.Printf("env: rollback instance %d: %v", inst.ID, uerr)
		}
		return nil, err
	}
	expireAt := s.now().Add(instanceTTL)
	if err := s.instances.UpdateRunning(ctx, inst.ID, endpoint, expireAt); err != nil {
		return nil, err
	}
	s.appendEvent(ctx, userID, model.EventLabStarted, map[string]any{
		"instance_id": inst.ID, "challenge_id": challengeID, "endpoint": endpoint,
	})
	return s.instances.FindByID(ctx, inst.ID)
}

// DestroyInstance 销毁实例（属主校验）。driver 失败保留记录，由 reaper 兜底回收。
func (s *EnvService) DestroyInstance(ctx context.Context, userID, challengeID uint) error {
	inst, err := s.instances.FindActive(ctx, userID, challengeID)
	if err != nil {
		return err
	}
	if inst == nil {
		return ErrNotFound
	}
	if err := s.driver.Destroy(ctx, inst.ID); err != nil {
		return err
	}
	if err := s.instances.UpdateStatus(ctx, inst.ID, model.InstanceDestroyed); err != nil {
		return err
	}
	s.appendEvent(ctx, userID, model.EventLabStopped, map[string]any{
		"instance_id": inst.ID, "challenge_id": challengeID,
	})
	return nil
}

// GetInstance 查询本人实例状态（无实例返回 nil，不报错）。
func (s *EnvService) GetInstance(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error) {
	return s.instances.FindActive(ctx, userID, challengeID)
}

// StartReaper 后台回收循环：定时清理过期 running 实例。
// 单实例部署（第一版约束），进程内循环即可，无需分布式锁。
func (s *EnvService) StartReaper(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.reapExpired(ctx)
			}
		}
	}()
}

// reapExpired 回收 ExpireAt 过期的 running 实例：先销毁驱动资源，成功才改 destroyed；
// 驱动失败保留记录（下轮重试）。
func (s *EnvService) reapExpired(ctx context.Context) {
	expired, err := s.instances.ListExpired(ctx, s.now())
	if err != nil {
		log.Printf("env: list expired: %v", err)
		return
	}
	for _, inst := range expired {
		if err := s.driver.Destroy(ctx, inst.ID); err != nil {
			log.Printf("env: reap instance %d: %v", inst.ID, err)
			continue
		}
		if err := s.instances.UpdateStatus(ctx, inst.ID, model.InstanceDestroyed); err != nil {
			log.Printf("env: mark destroyed %d: %v", inst.ID, err)
		}
	}
}

// appendEvent 写事件表（append-only）并广播。
func (s *EnvService) appendEvent(ctx context.Context, userID uint, t model.EventType, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	ev := model.Event{UserID: userID, Type: t, Payload: string(raw)}
	if err := s.events.Append(ctx, &ev); err != nil {
		log.Printf("env: append event %s: %v", t, err)
		return
	}
	s.bus.Publish(ctx, ev)
}

// DockerDriver 通过 docker CLI 管理容器（无 SDK 依赖；接口已隔离实现）。
type DockerDriver struct{}

func NewDockerDriver() *DockerDriver { return &DockerDriver{} }

// containerName 实例容器命名（纯函数便于测试）。
func containerName(instanceID uint) string {
	return fmt.Sprintf("yak-lab-%d", instanceID)
}

// Create 启动容器：随机宿主端口映射 127.0.0.1::<port>，读回实际端口生成 endpoint。
func (DockerDriver) Create(ctx context.Context, spec InstanceSpec) (string, error) {
	name := containerName(spec.InstanceID)
	if out, err := exec.CommandContext(ctx, "docker", "run", "-d",
		"--name", name,
		"-p", fmt.Sprintf("127.0.0.1::%d", spec.Port),
		spec.Image,
	).CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker run %s: %v: %s", spec.Image, err, strings.TrimSpace(string(out)))
	}
	// docker port 读回映射（格式 <hostPort> -> <containerPort>）。
	out, err := exec.CommandContext(ctx, "docker", "port", name, fmt.Sprint(spec.Port)).CombinedOutput()
	if err != nil {
		// 映射查询失败则清理刚起的容器，不留半成品。
		_, _ = exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput()
		return "", fmt.Errorf("docker port: %v: %s", err, strings.TrimSpace(string(out)))
	}
	hostPort := parseHostPort(string(out))
	if hostPort == "" {
		_, _ = exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput()
		return "", fmt.Errorf("docker port: no mapping for port %d", spec.Port)
	}
	return "http://127.0.0.1:" + hostPort, nil
}

// parseHostPort 解析 docker port 输出行（如 "127.0.0.1:32768 -> 80/tcp"），纯函数便于测试。
func parseHostPort(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 3 {
			addr := fields[0] // host:port
			if i := strings.LastIndex(addr, ":"); i >= 0 {
				port := addr[i+1:]
				if port != "" && port != "0" {
					return port
				}
			}
		}
	}
	return ""
}

// Destroy 删除容器（幂等：No such container 视为成功）。
func (DockerDriver) Destroy(ctx context.Context, instanceID uint) error {
	out, err := exec.CommandContext(ctx, "docker", "rm", "-f", containerName(instanceID)).CombinedOutput()
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "no such container") {
			return nil
		}
		return fmt.Errorf("docker rm: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}