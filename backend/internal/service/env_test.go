package service

import (
	"context"
	"errors"
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

// fakeDriver 记录调用与 spec 的测试驱动。
type fakeDriver struct {
	createErr  error
	destroyErr error
	specs      []InstanceSpec
	created    int
	destroyed  []uint
}

func (f *fakeDriver) Create(ctx context.Context, spec InstanceSpec) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.specs = append(f.specs, spec)
	f.created++
	return "http://127.0.0.1:32768", nil
}

func (f *fakeDriver) Destroy(ctx context.Context, instanceID uint) error {
	if f.destroyErr != nil {
		return f.destroyErr
	}
	f.destroyed = append(f.destroyed, instanceID)
	return nil
}

// newEnvTestService 构造带 env 题目与无 env 题目的 EnvService，返回两题真实 ID。
func newEnvTestService(t *testing.T, d InstanceDriver) (*EnvService, *gorm.DB, uint, uint) {
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
	chalRepo := repo.NewChallengeRepo(gdb)
	// 题目 A：带 env（镜像 + 端口）。
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		Slug: "chal-env", Title: "容器题", Type: model.ChallengeStatic, Score: 100,
		Image: "nginx:alpine", Port: 80,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{env}"}}, nil); err != nil {
		t.Fatal(err)
	}
	// 题目 B：无 env。
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		Slug: "chal-plain", Title: "纯 flag 题", Type: model.ChallengeStatic, Score: 100,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{plain}"}}, nil); err != nil {
		t.Fatal(err)
	}
	var chalA, chalB model.Challenge
	if err := gdb.Where("slug = ?", "chal-env").First(&chalA).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Where("slug = ?", "chal-plain").First(&chalB).Error; err != nil {
		t.Fatal(err)
	}
	env := NewEnvService(
		repo.NewLabInstanceRepo(gdb), repo.NewInstanceLogRepo(gdb), chalRepo,
		repo.NewUserRepo(gdb),
		repo.NewEventRepo(gdb), eventbus.New(), d,
	)
	return env, gdb, chalA.ID, chalB.ID
}

func TestEnsureInstanceIdempotent(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	first, err := env.EnsureInstance(ctx, 1, chalA)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if first.Status != model.InstanceRunning || first.Endpoint != "http://127.0.0.1:32768" {
		t.Fatalf("unexpected instance: %+v", first)
	}
	if first.ExpireAt.IsZero() {
		t.Fatal("expire_at must be set")
	}
	// 幂等：再次调用不新建实例、不重复调用驱动。
	second, err := env.EnsureInstance(ctx, 1, chalA)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if second.ID != first.ID || driver.created != 1 {
		t.Fatalf("ensure must be idempotent: first=%d second=%d created=%d", first.ID, second.ID, driver.created)
	}
	// spec 携带题目 env 定义。
	if len(driver.specs) != 1 || driver.specs[0].Image != "nginx:alpine" || driver.specs[0].Port != 80 {
		t.Fatalf("spec = %+v, want image=nginx:alpine port=80", driver.specs)
	}
	var n int64
	gdb.Model(&model.LabInstance{}).Count(&n)
	if n != 1 {
		t.Fatalf("instances = %d, want 1", n)
	}
	// lab.instance.started 事件已入事件表。
	var events int64
	gdb.Model(&model.Event{}).Where("type = ?", model.EventLabStarted).Count(&events)
	if events != 1 {
		t.Fatalf("lab.instance.started events = %d, want 1", events)
	}
}

func TestEnsureInstanceRequiresEnvDefinition(t *testing.T) {
	driver := &fakeDriver{}
	env, _, _, chalB := newEnvTestService(t, driver)
	// 无 env 的题目：422 语义，且不建 DB 记录。
	if _, err := env.EnsureInstance(context.Background(), 1, chalB); !errors.Is(err, ErrValidation) {
		t.Fatalf("env-less challenge should get ErrValidation, got %v", err)
	}
	if driver.created != 0 {
		t.Fatal("driver must not be called for env-less challenge")
	}
}

func TestEnsureInstanceSingleContainerConflict(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	// 用户 1 在题目 A 起实例。
	first, err := env.EnsureInstance(ctx, 1, chalA)
	if err != nil {
		t.Fatal(err)
	}
	// 题目 C（带 env）：启动第二个 → 冲突，携带当前实例。
	chalC := model.Challenge{Slug: "chal-env-2", Title: "容器题2", Type: model.ChallengeStatic, Score: 100, Image: "nginx:alpine", Port: 80}
	if err := gdb.Create(&chalC).Error; err != nil {
		t.Fatal(err)
	}
	_, err = env.EnsureInstance(ctx, 1, chalC.ID)
	var conflict *InstanceConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("second challenge should conflict, got %v", err)
	}
	if conflict.Current == nil || conflict.Current.ID != first.ID || conflict.Current.ChallengeID != chalA {
		t.Fatalf("conflict current = %+v, want instance %d of challenge %d", conflict.Current, first.ID, chalA)
	}
	// 同题目幂等：不冲突、不新建。
	again, err := env.EnsureInstance(ctx, 1, chalA)
	if err != nil {
		t.Fatalf("same challenge must stay idempotent: %v", err)
	}
	if again.ID != first.ID || driver.created != 1 {
		t.Fatalf("idempotent broken: again=%d created=%d", again.ID, driver.created)
	}
	// 其他用户不受单容器约束影响。
	if _, err := env.EnsureInstance(ctx, 2, chalA); err != nil {
		t.Fatalf("other user should not be limited: %v", err)
	}
}

func TestInstanceLifecycleLogs(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	if _, err := env.EnsureInstance(ctx, 7, chalA); err != nil {
		t.Fatal(err)
	}
	if err := env.DestroyInstance(ctx, 7, chalA); err != nil {
		t.Fatal(err)
	}
	var logs []model.InstanceLog
	if err := gdb.Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("logs = %d, want 2 (started+stopped)", len(logs))
	}
	if logs[0].Action != model.InstanceActionStarted || logs[0].Source != "owner" ||
		logs[0].Status != model.InstanceRunning || logs[0].Detail != "" {
		t.Fatalf("started log = %+v", logs[0])
	}
	if logs[0].UserID != 7 || logs[0].ChallengeID != chalA || logs[0].InstanceID == 0 {
		t.Fatalf("started log refs wrong: %+v", logs[0])
	}
	if logs[1].Action != model.InstanceActionStopped || logs[1].Source != "owner" ||
		logs[1].Status != model.InstanceDestroyed {
		t.Fatalf("stopped log = %+v", logs[1])
	}
}

func TestDriverFailureLoggedWithDetail(t *testing.T) {
	driver := &fakeDriver{createErr: errors.New("docker run: no such image")}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	if _, err := env.EnsureInstance(context.Background(), 1, chalA); err == nil {
		t.Fatal("expected driver error")
	}
	var logs []model.InstanceLog
	if err := gdb.Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Action != model.InstanceActionStopped {
		t.Fatalf("failure log = %+v", logs)
	}
	if !strings.Contains(logs[0].Detail, "no such image") {
		t.Fatalf("detail should carry failure reason: %+v", logs[0])
	}
}

func TestReaperLogsSource(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	inst, err := env.EnsureInstance(ctx, 1, chalA)
	if err != nil {
		t.Fatal(err)
	}
	env.now = func() time.Time { return inst.ExpireAt.Add(time.Minute) }
	env.reapExpired(ctx)
	var logs []model.InstanceLog
	if err := gdb.Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("logs = %d, want 2 (started + reaped)", len(logs))
	}
	last := logs[len(logs)-1]
	if last.Source != "reaper" || last.Action != model.InstanceActionStopped ||
		!strings.Contains(last.Detail, "ttl expired") {
		t.Fatalf("reaper log = %+v", last)
	}
}

func TestListActiveInstancesAndLogs(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	if err := gdb.Create(&model.User{Username: "alice", PasswordHash: "x", GlobalRole: model.GlobalRoleStudent}).Error; err != nil {
		t.Fatal(err)
	}
	var alice model.User
	if err := gdb.Where("username = ?", "alice").First(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := env.EnsureInstance(ctx, alice.ID, chalA); err != nil {
		t.Fatal(err)
	}
	// 活跃概览：一条，富化用户与题目。
	active, err := env.ListActiveInstances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("active = %d, want 1", len(active))
	}
	if active[0].Username != "alice" || active[0].ChallengeTitle != "容器题" || active[0].ChallengeSlug != "chal-env" {
		t.Fatalf("active enriched = %+v", active[0])
	}
	// 销毁后活跃概览清空。
	if err := env.DestroyInstance(ctx, alice.ID, chalA); err != nil {
		t.Fatal(err)
	}
	active, err = env.ListActiveInstances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("active after destroy = %d, want 0", len(active))
	}
	// 日志分页：两条（started/stopped），富化正确。
	logs, total, err := env.ListLogs(ctx, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(logs) != 2 {
		t.Fatalf("logs total=%d len=%d, want 2/2", total, len(logs))
	}
	if logs[0].Username != "alice" || logs[0].ChallengeSlug != "chal-env" {
		t.Fatalf("log enriched = %+v", logs[0])
	}
}

func TestEnsureInstanceDriverFailureRollsBack(t *testing.T) {
	driver := &fakeDriver{createErr: errors.New("driver down")}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	if _, err := env.EnsureInstance(context.Background(), 1, chalA); err == nil {
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
	if _, err := env.EnsureInstance(context.Background(), 1, chalA); err != nil {
		t.Fatalf("retry after rollback: %v", err)
	}
}

func TestDestroyInstanceOwnerOnly(t *testing.T) {
	driver := &fakeDriver{}
	env, _, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	inst, err := env.EnsureInstance(ctx, 7, chalA)
	if err != nil {
		t.Fatal(err)
	}
	// 非属主：404 语义。
	if err := env.DestroyInstance(ctx, 8, chalA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-owner should get ErrNotFound, got %v", err)
	}
	if len(driver.destroyed) != 0 {
		t.Fatal("driver must not be called for non-owner")
	}
	if err := env.DestroyInstance(ctx, 7, chalA); err != nil {
		t.Fatalf("owner destroy: %v", err)
	}
	if len(driver.destroyed) != 1 || driver.destroyed[0] != inst.ID {
		t.Fatalf("driver.destroyed = %v, want [%d]", driver.destroyed, inst.ID)
	}
	// 再查：活跃实例已无；lab.instance.stopped 事件已写。
	got, err := env.GetInstance(ctx, 7, chalA)
	if err != nil || got != nil {
		t.Fatalf("instance after destroy = %v err=%v, want nil", got, err)
	}
}

func TestReaperRecyclesExpired(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	inst, err := env.EnsureInstance(ctx, 1, chalA)
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
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	inst, err := env.EnsureInstance(ctx, 1, chalA)
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
	env, _, chalA, _ := newEnvTestService(t, NoopDriver{})
	if _, err := env.EnsureInstance(context.Background(), 1, chalA); err == nil {
		t.Fatal("noop driver must refuse create")
	}
}

func TestDockerParseHostPort(t *testing.T) {
	// docker port 实际输出：单字段 <host:port>。
	if got := parseHostPort("127.0.0.1:36186\n"); got != "36186" {
		t.Fatalf("parseHostPort single-field = %q, want 36186", got)
	}
	// 兼容 <host:port> -> <containerPort[/proto]> 形式。
	if got := parseHostPort("127.0.0.1:32768 -> 80/tcp\n"); got != "32768" {
		t.Fatalf("parseHostPort = %q, want 32768", got)
	}
	if got := parseHostPort("0.0.0.0:55001 -> 80/tcp\n[::]:55001 -> 80/tcp\n"); got != "55001" {
		t.Fatalf("parseHostPort multi-line = %q, want 55001", got)
	}
	if got := parseHostPort(""); got != "" {
		t.Fatalf("parseHostPort empty = %q, want empty", got)
	}
	if got := parseHostPort("127.0.0.1:0 -> 80/tcp\n"); got != "" {
		t.Fatalf("parseHostPort zero port = %q, want empty", got)
	}
}

func TestDockerContainerName(t *testing.T) {
	if got := containerName(42); got != "yak-lab-42" {
		t.Fatalf("containerName = %q, want yak-lab-42", got)
	}
}

func TestListInstancesEnrichesUserAndChallenge(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	// 造一个用户与一个运行中实例。
	if err := gdb.Create(&model.User{Username: "alice", PasswordHash: "x", GlobalRole: model.GlobalRoleStudent}).Error; err != nil {
		t.Fatal(err)
	}
	var alice model.User
	if err := gdb.Where("username = ?", "alice").First(&alice).Error; err != nil {
		t.Fatal(err)
	}
	inst, err := env.EnsureInstance(context.Background(), alice.ID, chalA)
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := env.ListInstances(context.Background(), 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(items))
	}
	got := items[0]
	if got.Username != "alice" || got.ChallengeTitle != "容器题" || got.ChallengeSlug != "chal-env" {
		t.Fatalf("enriched = %+v", got)
	}
	if got.ID != inst.ID || got.Status != model.InstanceRunning {
		t.Fatalf("instance fields wrong: %+v", got.LabInstance)
	}
}

func TestAdminDestroyInstance(t *testing.T) {
	driver := &fakeDriver{}
	env, gdb, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	inst, err := env.EnsureInstance(ctx, 7, chalA)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.AdminDestroyInstance(ctx, inst.ID); err != nil {
		t.Fatalf("admin destroy: %v", err)
	}
	if len(driver.destroyed) != 1 || driver.destroyed[0] != inst.ID {
		t.Fatalf("driver.destroyed = %v, want [%d]", driver.destroyed, inst.ID)
	}
	var after model.LabInstance
	if err := gdb.First(&after, inst.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Status != model.InstanceDestroyed {
		t.Fatalf("status = %s, want destroyed", after.Status)
	}
	// lab.instance.stopped 事件写入且 payload 标记 source=admin。
	var events []model.Event
	gdb.Where("type = ? AND payload LIKE ?", model.EventLabStopped, "%admin%").Find(&events)
	if len(events) != 1 {
		t.Fatalf("admin-sourced stop events = %d, want 1", len(events))
	}
	// 不存在的实例：404 语义（api 层将 gorm.ErrRecordNotFound 映射为 404）。
	if err := env.AdminDestroyInstance(ctx, 9999); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing instance should get record-not-found, got %v", err)
	}
}
