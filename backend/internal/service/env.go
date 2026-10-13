package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// EnvService 题目环境生命周期：幂等创建 / 销毁 / TTL 回收。
// driver 只负责与运行时（Docker 等）交互；DB 状态由本服务维护（project-conventions skill）。
type InstanceDriver interface {
	// Create 启动实例并返回访问地址。
	Create(ctx context.Context, challengeID, userID uint) (endpoint string, err error)
	Destroy(ctx context.Context, instanceID uint) error
}

// NoopDriver 占位驱动：真实驱动（Docker）落地前的 stub，调用一律报错。
type NoopDriver struct{}

func (NoopDriver) Create(ctx context.Context, challengeID, userID uint) (string, error) {
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
	instances repo.LabInstanceRepo
	driver    InstanceDriver
	// now 时钟注入，测试可固定时间；生产用 time.Now。
	now func() time.Time
}

func NewEnvService(instances repo.LabInstanceRepo, d InstanceDriver) *EnvService {
	return &EnvService{instances: instances, driver: d, now: time.Now}
}

// EnsureInstance 幂等启动：已有 running 实例直接返回；
// 无则创建（并发上限校验 → DB 落 creating → driver.Create → 回写 running）。
// driver 失败回滚 DB 记录（status→destroyed），不留半成品。
func (s *EnvService) EnsureInstance(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error) {
	if inst, err := s.instances.FindActive(ctx, userID, challengeID); err != nil {
		return nil, err
	} else if inst != nil {
		return inst, nil
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
	endpoint, err := s.driver.Create(ctx, challengeID, userID)
	if err != nil {
		// 回滚：DB 记录标记 destroyed，不留 creating 残留。
		if uerr := s.instances.UpdateStatus(ctx, inst.ID, model.InstanceStopped); uerr != nil {
			log.Printf("env: rollback instance %d: %v", inst.ID, uerr)
		}
		return nil, err
	}
	expireAt := s.now().Add(instanceTTL)
	if err := s.instances.UpdateRunning(ctx, inst.ID, endpoint, expireAt); err != nil {
		return nil, err
	}
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
	return s.instances.UpdateStatus(ctx, inst.ID, model.InstanceDestroyed)
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