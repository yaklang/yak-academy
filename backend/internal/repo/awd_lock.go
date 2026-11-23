package repo

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"gorm.io/gorm"
)

// AWD 回合锁：回合创建/结算/启动恢复必须串行，否则两个并发请求会重复
// 生成回合或重复结算。生产（PostgreSQL）用会话级 advisory lock，多副本/
// 多进程共享同一把锁；开发/e2e（SQLite，单后端副本）用进程内键控互斥。
const awdAdvisoryLockRetry = 50 * time.Millisecond

// WithAwdLock 在命名锁内执行 fn（同一 key 串行，不同 key 互不影响）。
func (r *awdRepo) WithAwdLock(ctx context.Context, key string, fn func(context.Context) error) error {
	if r.db.Dialector.Name() == "postgres" {
		return r.withPostgresAdvisoryLock(ctx, key, fn)
	}
	release, err := r.locks.acquire(ctx, key)
	if err != nil {
		return err
	}
	defer release()
	return fn(ctx)
}

// withPostgresAdvisoryLock 在一条固定连接上持锁：advisory lock 是会话级的，
// 必须在同一连接上 unlock。轮询 try-lock 而非阻塞等待，保证 ctx 取消可退出。
func (r *awdRepo) withPostgresAdvisoryLock(ctx context.Context, key string, fn func(context.Context) error) error {
	lockID := awdAdvisoryLockID(key)
	return r.db.WithContext(ctx).Connection(func(conn *gorm.DB) (runErr error) {
		for {
			var acquired bool
			if err := conn.Raw("SELECT pg_try_advisory_lock(?)", lockID).Row().Scan(&acquired); err != nil {
				return err
			}
			if acquired {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(awdAdvisoryLockRetry):
			}
		}
		// defer 保证 fn panic 时也会释放会话锁，连接回到池里不残留锁。
		defer func() {
			if err := conn.Exec("SELECT pg_advisory_unlock(?)", lockID).Error; err != nil && runErr == nil {
				runErr = err
			}
		}()
		return fn(ctx)
	})
}

// awdAdvisoryLockID 把业务 key 映射为 PostgreSQL bigint 锁 ID（fnv64a 稳定，
// 多副本对同一 key 得到相同 ID）。
func awdAdvisoryLockID(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64())
}

// awdKeyedLock SQLite 模式下的进程内键控互斥：每个 key 一个容量 1 的
// channel 作为信号量，等待期间响应 ctx 取消。
type awdKeyedLock struct {
	mu    sync.Mutex
	gates map[string]chan struct{}
}

func newAwdKeyedLock() *awdKeyedLock {
	return &awdKeyedLock{gates: make(map[string]chan struct{})}
}

func (l *awdKeyedLock) acquire(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	gate, ok := l.gates[key]
	if !ok {
		gate = make(chan struct{}, 1)
		l.gates[key] = gate
	}
	l.mu.Unlock()

	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
