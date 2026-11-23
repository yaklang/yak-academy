package repo

// PostgreSQL advisory lock 集成测试：真实 PG 上验证跨连接（模拟多副本）串行、
// 释放后可再次获取、等待期间响应 ctx 取消。默认 skip，开启方式与
// postgres_compat_test.go 相同：ACADEMY_TEST_PG_DSN 或 ACADEMY_TEST_DOCKER=1。

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"gorm.io/gorm"
)

func awdLockPostgresDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("ACADEMY_TEST_PG_DSN"); dsn != "" {
		return dsn
	}
	if os.Getenv("ACADEMY_TEST_DOCKER") != "1" {
		t.Skip("需真实 PostgreSQL：设 ACADEMY_TEST_PG_DSN 或 ACADEMY_TEST_DOCKER=1")
	}
	return startPostgresContainer(t)
}

func TestPostgresAwdLockSerializesAcrossConnections(t *testing.T) {
	dsn := awdLockPostgresDSN(t)
	gdbA, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open postgres A: %v", err)
	}
	gdbB, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open postgres B: %v", err)
	}
	t.Cleanup(func() {
		for _, gdb := range []*gorm.DB{gdbA, gdbB} {
			sqlDB, _ := gdb.DB()
			_ = sqlDB.Close()
		}
	})
	repoA := NewAwdRepo(gdbA)
	repoB := NewAwdRepo(gdbB)
	ctx := context.Background()

	entered := make(chan struct{})
	release := make(chan struct{})
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- repoA.WithAwdLock(ctx, "awd:round:pg-test", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first advisory lock not acquired")
	}

	secondEntered := make(chan struct{})
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- repoB.WithAwdLock(ctx, "awd:round:pg-test", func(context.Context) error {
			close(secondEntered)
			return nil
		})
	}()
	select {
	case <-secondEntered:
		t.Fatal("second connection entered while advisory lock held")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first lock: %v", err)
	}
	select {
	case <-secondEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("second advisory lock not acquired after release")
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("second lock: %v", err)
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	held := make(chan struct{})
	heldRelease := make(chan struct{})
	heldErr := make(chan error, 1)
	go func() {
		heldErr <- repoA.WithAwdLock(context.Background(), "awd:round:pg-cancel", func(context.Context) error {
			close(held)
			<-heldRelease
			return nil
		})
	}()
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("advisory lock for cancellation test not acquired")
	}
	err = repoB.WithAwdLock(waitCtx, "awd:round:pg-cancel", func(context.Context) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter error = %v, want DeadlineExceeded", err)
	}
	close(heldRelease)
	if err := <-heldErr; err != nil {
		t.Fatalf("held lock: %v", err)
	}
}
