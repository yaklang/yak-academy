package safego

import "testing"

// 后台任务 panic 必须被隔离：恢复后返回 false，进程继续执行后续任务。
func TestRunRecoversPanic(t *testing.T) {
	ok := Run("test.panic", func() { panic("boom") })
	if ok {
		t.Fatal("Run returned true for a panicking task")
	}
	ran := false
	if !Run("test.ok", func() { ran = true }) {
		t.Fatal("Run reported failure for a healthy task")
	}
	if !ran {
		t.Fatal("healthy task did not run")
	}
}

// Go 启动的 goroutine panic 不能传播到调用方；后续 Go 任务仍可执行。
func TestGoIsolatesPanic(t *testing.T) {
	done := make(chan struct{})
	Go("test.go-panic", func() {
		defer close(done)
		panic("boom")
	})
	<-done

	ran := make(chan struct{})
	Go("test.go-ok", func() { close(ran) })
	<-ran
}

// Guard 作为 defer 直接调用时必须捕获 panic 并执行收尾回调。
func TestGuardRunsCleanupOnPanic(t *testing.T) {
	cleaned := false
	func() {
		defer Guard("test.guard", func() { cleaned = true })
		panic("boom")
	}()
	if !cleaned {
		t.Fatal("cleanup callback did not run for a panicking task")
	}
}
