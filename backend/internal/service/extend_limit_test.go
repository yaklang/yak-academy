package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 治理项（基础短板 Item 4）：续期次数上限（每实例 2 次）。

func TestExtendInstanceCountAndLimit(t *testing.T) {
	driver := &fakeDriver{}
	env, _, chalA, _ := newEnvTestService(t, driver)
	ctx := context.Background()
	fixed := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	env.now = func() time.Time { return fixed }

	inst, err := env.EnsureInstance(ctx, 1, chalA)
	if err != nil {
		t.Fatal(err)
	}
	if inst.ExtendCount != 0 {
		t.Fatalf("initial extend_count = %d, want 0", inst.ExtendCount)
	}

	// 第 1、2 次续期成功，计数递增。
	for i := 1; i <= 2; i++ {
		extended, err := env.ExtendInstance(ctx, 1, chalA)
		if err != nil {
			t.Fatalf("extend #%d: %v", i, err)
		}
		if extended.ExtendCount != i {
			t.Fatalf("extend #%d count = %d", i, extended.ExtendCount)
		}
	}

	// 第 3 次达到上限 → 422 语义（ErrValidation）。
	if _, err := env.ExtendInstance(ctx, 1, chalA); !errors.Is(err, ErrValidation) {
		t.Fatalf("extend beyond limit: %v", err)
	}

	// 销毁重建后重新计数（新实例）。
	if err := env.DestroyInstance(ctx, 1, chalA); err != nil {
		t.Fatal(err)
	}
	inst2, err := env.EnsureInstance(ctx, 1, chalA)
	if err != nil {
		t.Fatal(err)
	}
	if inst2.ExtendCount != 0 {
		t.Fatalf("restarted instance extend_count = %d, want 0", inst2.ExtendCount)
	}
}