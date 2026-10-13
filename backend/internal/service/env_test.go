package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// fakeDriver 记录调用数的测试驱动。
type fakeDriver struct {
	createErr   error
	destroyErr  error
	created     int
	destroyed   []uint
}

func (f *fakeDriver) Create(ctx context.Context, challengeID, userID uint) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.created++
	return "127.0.0.1:8081", nil
}

func (f *fakeDriver) Destroy(ctx context.Context, instanceID uint) error {
	if f.destroyErr != nil {
		return f.destroyErr
	}
	f.destroyed = append(f.destroyed, instanceID)
	return nil
}

func newEnvTestService(t *testing.T, d InstanceDriver) (*EnvService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:envtest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		dbg, _ := gdb.DB()
		dbg.Close() // 关闭共享内存库，下个测试重建
	})
	return NewEnvService(repo.NewLabInstanceRepo(gdb), d), gdb
}

func TestEnsureInstanceIdempotent(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb := newEnvTestService(t, driver)
	ctx := context.Background()
	first, err := env.EnsureInstance(ctx, 1, 10)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if first.Status != model.InstanceRunning || first.Endpoint != "127.0.0.1:8081" {
		t.Fatalf("unexpected instance: %+v", first)
	}
	if first.ExpireAt.IsZero() {
		t.Fatal("expire_at must be set")
	}
	// 幂等：再次调用不新建实例、不重复调用驱动。
	second, err := env.EnsureInstance(ctx, 1, 10)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if second.ID != first.ID || driver.created != 1 {
		t.Fatalf("ensure must be idempotent: first=%d second=%d created=%d", first.ID, second.ID, driver.created)
	}
	var n int64
	gdb.Model(&model.LabInstance{}).Count(&n)
	if n != 1 {
		t.Fatalf("instances = %d, want 1", n)
	}
}

func TestEnsureInstancePerUserLimit(t *testing.T) {
	driver := &fakeDriver{}
	env, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	// 用户 1 在两道题上各起一个实例（上限 2）。
	if _, err := env.EnsureInstance(ctx, 1, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := env.EnsureInstance(ctx, 1, 20); err != nil {
		t.Fatal(err)
	}
	// 第三个活跃实例超限。
	if _, err := env.EnsureInstance(ctx, 1, 30); !errors.Is(err, ErrValidation) {
		t.Fatalf("third instance should hit limit, got %v", err)
	}
	// 其他用户不受影响。
	if _, err := env.EnsureInstance(ctx, 2, 10); err != nil {
		t.Fatalf("other user should not be limited: %v", err)
	}
}

func TestEnsureInstanceDriverFailureRollsBack(t *testing.T) {
	driver := &fakeDriver{createErr: errors.New("driver down")}
	env, gdb := newEnvTestService(t, driver)
	if _, err := env.EnsureInstance(context.Background(), 1, 10); err == nil {
		t.Fatal("expected driver error")
	}
	// 回滚：不留 creating 残留；记录置 stopped。
	var inst model.LabInstance
	if err := gdb.First(&inst).Error; err != nil {
		t.Fatalf("rollback record should persist: %v", err)
	}
	if inst.Status != model.InstanceStopped {
		t.Fatalf("status = %s, want stopped", inst.Status)
	}
	// 回滚后可重试（旧记录不阻塞——FindActive 只看 creating/running）。
	driver.createErr = nil
	if _, err := env.EnsureInstance(context.Background(), 1, 10); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

func TestDestroyInstanceOwnerOnly(t *testing.T) {
	driver := &fakeDriver{}
	env, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	inst, err := env.EnsureInstance(ctx, 7, 10)
	if err != nil {
		t.Fatal(err)
	}
	// 非属主：404 语义。
	if err := env.DestroyInstance(ctx, 8, 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner should get ErrNotFound, got %v", err)
	}
	if len(driver.destroyed) != 0 {
		t.Fatal("driver must not be called for non-owner")
	}
	if err := env.DestroyInstance(ctx, 7, 10); err != nil {
		t.Fatalf("owner destroy: %v", err)
	}
	if len(driver.destroyed) != 1 || driver.destroyed[0] != inst.ID {
		t.Fatalf("driver.destroyed = %v, want [%d]", driver.destroyed, inst.ID)
	}
	// 再查：活跃实例已无。
	got, err := env.GetInstance(ctx, 7, 10)
	if err != nil || got != nil {
		t.Fatalf("instance after destroy = %v err=%v, want nil", got, err)
	}
}

func TestReaperRecyclesExpired(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb := newEnvTestService(t, driver)
	ctx := context.Background()
	inst, err := env.EnsureInstance(ctx, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	// 固定时钟到过期之后。
	past := inst.ExpireAt.Add(time.Minute)
	env.now = func() time.Time { return past }
	env.reapExpired(ctx)
	if len(driver.destroyed) != 1 {
		t.Fatalf("reaper should destroy 1, got %v", driver.destroyed)
	}
	var after model.LabInstance
	if err := gdb.First(&after, inst.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != model.InstanceDestroyed {
		t.Fatalf("status = %s, want destroyed", after.Status)
	}
}

func TestReaperKeepsFailedDestroy(t *testing.T) {
	driver := &fakeDriver{destroyErr: errors.New("runtime busy")}
	env, gdb := newEnvTestService(t, driver)
	ctx := context.Background()
	inst, err := env.EnsureInstance(ctx, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	env.now = func() time.Time { return inst.ExpireAt.Add(time.Minute) }
	env.reapExpired(ctx)
	// 驱动失败：记录保留 running，下轮重试。
	var after model.LabInstance
	if err := gdb.First(&after, inst.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != model.InstanceRunning {
		t.Fatalf("status = %s, want running (kept for retry)", after.Status)
	}
	// 驱动恢复后同轮语义：再次 reap 成功回收。
	driver.destroyErr = nil
	env.reapExpired(ctx)
	if err := gdb.First(&after, inst.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != model.InstanceDestroyed {
		t.Fatalf("status after retry = %s, want destroyed", after.Status)
	}
}

func TestNoopDriverUnavailable(t *testing.T) {
	env, _ := newEnvTestService(t, NoopDriver{})
	if _, err := env.EnsureInstance(context.Background(), 1, 10); err == nil {
		t.Fatal("noop driver must refuse create")
	}
}