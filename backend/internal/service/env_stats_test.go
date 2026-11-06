package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// 性能快照缓存回归：docker stats 单次采样约 1-2s，容器管理页不能在每次进入时
// 同步等待；缓存命中立即返回，过期后后台刷新，刷新失败保留最近成功快照。

type statsTestClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *statsTestClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *statsTestClock) get() time.Time { return c.now() }

func (c *statsTestClock) set(at time.Time) {
	c.mu.Lock()
	c.at = at
	c.mu.Unlock()
}

func (c *statsTestClock) add(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

type statsTestDriver struct {
	mu     sync.Mutex
	calls  int
	stats  *HostStats
	err    error
	called chan struct{}
	block  chan struct{}
}

func (d *statsTestDriver) Create(context.Context, InstanceSpec) (string, error) { return "", nil }
func (d *statsTestDriver) Destroy(context.Context, uint) error                  { return nil }

func (d *statsTestDriver) HostStats(ctx context.Context) (*HostStats, error) {
	d.mu.Lock()
	d.calls++
	stats := cloneHostStats(d.stats)
	err := d.err
	called := d.called
	block := d.block
	d.mu.Unlock()

	if called != nil {
		select {
		case called <- struct{}{}:
		default:
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return stats, err
}

func (d *statsTestDriver) set(stats *HostStats, err error) {
	d.mu.Lock()
	d.stats = stats
	d.err = err
	d.mu.Unlock()
}

func (d *statsTestDriver) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func newStatsTestEnv(t *testing.T, driver InstanceDriver, clock *statsTestClock) *EnvService {
	t.Helper()
	env, _, _, _ := newEnvTestService(t, driver)
	env.now = clock.now
	return env
}

func waitForStats(t *testing.T, env *EnvService, cond func(*HostStats) bool) *HostStats {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last *HostStats
	for time.Now().Before(deadline) {
		stats, err := env.HostStats(context.Background())
		if err == nil {
			last = stats
			if cond(stats) {
				return stats
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("performance snapshot condition not met; last=%+v", last)
	return nil
}

func sampleStats(ncpu int, containerName string) *HostStats {
	return &HostStats{
		NCPU:       ncpu,
		Host:       &HostUsage{MemTotalBytes: 8 << 30},
		Containers: []ContainerResource{{ID: "c1", Name: containerName}},
	}
}

func TestHostStatsFreshCacheHitDoesNotRecollect(t *testing.T) {
	clock := &statsTestClock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	driver := &statsTestDriver{stats: sampleStats(8, "one")}
	env := newStatsTestEnv(t, driver, clock)

	first, err := env.HostStats(context.Background())
	if err != nil {
		t.Fatalf("first HostStats: %v", err)
	}
	if first.SampledAt == nil || !first.SampledAt.Equal(clock.get()) {
		t.Fatalf("first sampled_at = %v, want %v", first.SampledAt, clock.get())
	}

	second, err := env.HostStats(context.Background())
	if err != nil {
		t.Fatalf("second HostStats: %v", err)
	}
	if got := driver.callCount(); got != 1 {
		t.Fatalf("fresh cache hit must not recollect: calls=%d", got)
	}
	if second.Refreshing {
		t.Fatal("fresh cache hit must not report refreshing")
	}
	// 返回副本：调用方改写结果不能污染缓存。
	second.Containers[0].Name = "mutated"
	third, err := env.HostStats(context.Background())
	if err != nil {
		t.Fatalf("third HostStats: %v", err)
	}
	if third.Containers[0].Name != "one" {
		t.Fatalf("cached snapshot was mutated: %+v", third.Containers[0])
	}
}

func TestHostStatsStaleCacheReturnsImmediatelyAndRefreshes(t *testing.T) {
	clock := &statsTestClock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	driver := &statsTestDriver{stats: sampleStats(8, "old")}
	env := newStatsTestEnv(t, driver, clock)

	if _, err := env.HostStats(context.Background()); err != nil {
		t.Fatalf("warm HostStats: %v", err)
	}
	clock.add(10 * time.Second)
	driver.set(sampleStats(16, "new"), nil)

	stale, err := env.HostStats(context.Background())
	if err != nil {
		t.Fatalf("stale HostStats: %v", err)
	}
	if !stale.Refreshing {
		t.Fatal("stale cache should report refreshing while background collection runs")
	}
	if stale.NCPU != 8 || stale.Containers[0].Name != "old" {
		t.Fatalf("stale response must return last successful snapshot: %+v", stale)
	}

	fresh := waitForStats(t, env, func(s *HostStats) bool {
		return s.NCPU == 16 && !s.Refreshing
	})
	if fresh.Containers[0].Name != "new" {
		t.Fatalf("background refresh did not replace snapshot: %+v", fresh.Containers[0])
	}
	if got := driver.callCount(); got != 2 {
		t.Fatalf("expected one background refresh: calls=%d", got)
	}
}

func TestHostStatsRefreshFailureKeepsSnapshotAndBacksOff(t *testing.T) {
	clock := &statsTestClock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	driver := &statsTestDriver{stats: sampleStats(8, "cached")}
	env := newStatsTestEnv(t, driver, clock)

	if _, err := env.HostStats(context.Background()); err != nil {
		t.Fatalf("warm HostStats: %v", err)
	}
	clock.add(10 * time.Second)
	driver.set(nil, errors.New("docker stats: daemon unavailable"))

	failed := waitForStats(t, env, func(s *HostStats) bool {
		return s.Warning != "" && !s.Refreshing
	})
	if failed.NCPU != 8 || failed.Containers[0].Name != "cached" {
		t.Fatalf("failed refresh must keep last good snapshot: %+v", failed)
	}
	if failed.Warning != hostStatsStaleWarning {
		t.Fatalf("warning = %q, want %q", failed.Warning, hostStatsStaleWarning)
	}
	calls := driver.callCount()

	// 失败后的冷却期内不再触发刷新（页面轮询不会打成重试风暴）。
	clock.add(time.Second)
	quiet, err := env.HostStats(context.Background())
	if err != nil {
		t.Fatalf("HostStats during backoff: %v", err)
	}
	if quiet.Refreshing {
		t.Fatal("must not refresh again during backoff")
	}
	if got := driver.callCount(); got != calls {
		t.Fatalf("refresh during backoff: calls=%d, want %d", got, calls)
	}

	// 冷却结束且 Docker 恢复后，下一次请求重新刷新并清除告警。
	clock.add(hostStatsRetryAfter)
	driver.set(sampleStats(16, "recovered"), nil)
	recovering, err := env.HostStats(context.Background())
	if err != nil {
		t.Fatalf("HostStats after backoff: %v", err)
	}
	if !recovering.Refreshing {
		t.Fatal("after backoff the stale cache should trigger a refresh")
	}
	recovered := waitForStats(t, env, func(s *HostStats) bool {
		return s.NCPU == 16 && !s.Refreshing
	})
	if recovered.Warning != "" {
		t.Fatalf("successful refresh must clear warning: %q", recovered.Warning)
	}
}

func TestHostStatsColdRequestsShareOneCollection(t *testing.T) {
	clock := &statsTestClock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	driver := &statsTestDriver{
		stats:  sampleStats(8, "shared"),
		called: make(chan struct{}, 2),
		block:  make(chan struct{}),
	}
	env := newStatsTestEnv(t, driver, clock)

	type result struct {
		stats *HostStats
		err   error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			stats, err := env.HostStats(context.Background())
			results <- result{stats: stats, err: err}
		}()
	}

	select {
	case <-driver.called:
	case <-time.After(time.Second):
		t.Fatal("cold request did not reach driver")
	}
	select {
	case <-driver.called:
		t.Fatal("second cold request must join the in-flight collection")
	case <-time.After(50 * time.Millisecond):
	}
	close(driver.block)

	for i := 0; i < 2; i++ {
		select {
		case res := <-results:
			if res.err != nil {
				t.Fatalf("cold HostStats: %v", res.err)
			}
			if res.stats.NCPU != 8 || res.stats.Containers[0].Name != "shared" {
				t.Fatalf("unexpected shared result: %+v", res.stats)
			}
		case <-time.After(time.Second):
			t.Fatal("cold request did not return")
		}
	}
	if got := driver.callCount(); got != 1 {
		t.Fatalf("cold requests must share one collection: calls=%d", got)
	}
}

func TestHostStatsPreloadServesFirstRequestFromCache(t *testing.T) {
	clock := &statsTestClock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	driver := &statsTestDriver{
		stats:  sampleStats(8, "preloaded"),
		called: make(chan struct{}, 1),
	}
	env := newStatsTestEnv(t, driver, clock)

	env.PrewarmHostStats(context.Background())
	select {
	case <-driver.called:
	case <-time.After(time.Second):
		t.Fatal("prewarm did not collect")
	}

	stats := waitForStats(t, env, func(s *HostStats) bool { return s.Containers[0].Name == "preloaded" })
	if stats.Refreshing {
		t.Fatal("first request after prewarm must be served from fresh cache")
	}
	if got := driver.callCount(); got != 1 {
		t.Fatalf("request after prewarm must not recollect: calls=%d", got)
	}
}

func TestHostStatsColdFailureIsReturned(t *testing.T) {
	clock := &statsTestClock{at: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)}
	driver := &statsTestDriver{err: errors.New("docker unavailable")}
	env := newStatsTestEnv(t, driver, clock)

	if _, err := env.HostStats(context.Background()); err == nil {
		t.Fatal("cold failure must be returned to caller")
	}
}
