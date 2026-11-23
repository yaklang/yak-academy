package repo

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newAwdLockTestRepo(t *testing.T) AwdRepo {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:awdlock-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		_ = sqlDB.Close()
	})
	return NewAwdRepo(gdb)
}

func TestAwdRepoLockSerializesSameKey(t *testing.T) {
	r := newAwdLockTestRepo(t)
	ctx := context.Background()
	entered := make(chan struct{})
	release := make(chan struct{})
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- r.WithAwdLock(ctx, "awd:round:1", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first lock not acquired")
	}

	secondEntered := make(chan struct{})
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- r.WithAwdLock(ctx, "awd:round:1", func(context.Context) error {
			close(secondEntered)
			return nil
		})
	}()
	select {
	case <-secondEntered:
		t.Fatal("second lock entered while first still held")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first lock: %v", err)
	}
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("second lock not acquired after release")
	}
	if err := <-secondErr; err != nil {
		t.Fatalf("second lock: %v", err)
	}
}

func TestAwdRepoLockAllowsDifferentKeys(t *testing.T) {
	r := newAwdLockTestRepo(t)
	ctx := context.Background()
	entered := make(chan struct{})
	release := make(chan struct{})
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- r.WithAwdLock(ctx, "awd:round:1", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first lock not acquired")
	}

	otherEntered := make(chan error, 1)
	go func() {
		otherEntered <- r.WithAwdLock(ctx, "awd:round:2", func(context.Context) error { return nil })
	}()
	select {
	case err := <-otherEntered:
		if err != nil {
			t.Fatalf("different key lock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("different key blocked by first lock")
	}
	close(release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first lock: %v", err)
	}
}

func TestAwdRepoLockHonorsContextCancellation(t *testing.T) {
	r := newAwdLockTestRepo(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- r.WithAwdLock(context.Background(), "awd:round:9", func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first lock not acquired")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := r.WithAwdLock(ctx, "awd:round:9", func(context.Context) error {
		t.Fatal("callback ran despite canceled wait")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait error = %v, want DeadlineExceeded", err)
	}
	close(release)
	if err := <-firstErr; err != nil {
		t.Fatalf("first lock: %v", err)
	}
}

func TestAwdAdvisoryLockIDStable(t *testing.T) {
	a := awdAdvisoryLockID("awd:round:42")
	b := awdAdvisoryLockID("awd:round:42")
	c := awdAdvisoryLockID("awd:round:43")
	if a != b {
		t.Fatalf("lock ID not stable: %d vs %d", a, b)
	}
	if a == c {
		t.Fatalf("different keys mapped to same lock ID %d", a)
	}
}
