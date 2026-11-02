// Package safego 为后台 goroutine 提供 panic 隔离。
//
// net/http 只恢复「请求 goroutine」里的 panic；任何其它 goroutine 的未恢复
// panic 会终止整个进程（判题消费者、兜底轮询、靶机回收、定时备份、引擎
// 下载都属此类）。平台是单进程部署，一个坏任务不能把所有在线用户一起带走：
// 后台入口统一经本包启动，panic 记堆栈后任务失败，进程继续服务。
package safego

import (
	"log"
	"runtime/debug"
)

// Go 启动 fn，panic 被记录后吞掉（适合一次点火的异步任务）。
// 长驻循环应在每轮迭代内部用 Run 包裹工作单元，避免一次 panic 让循环永久停摆。
func Go(label string, fn func()) {
	go func() {
		defer Guard(label, nil)
		fn()
	}()
}

// Run 同步执行 fn，panic 时记录堆栈并返回 false（已恢复）。
func Run(label string, fn func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(label, r)
			ok = false
		}
	}()
	fn()
	return true
}

// Guard 必须在 defer 中直接调用（Go 的 recover 只在直接被 defer 调用的
// 函数里生效）：panic 时记录堆栈并执行 onPanic 收尾（如把任务标记为失败，
// 避免票证永久 pending）。
//
//	defer safego.Guard("judge.executeCodeRun", func() { markFailed() })
func Guard(label string, onPanic func()) {
	if r := recover(); r != nil {
		logPanic(label, r)
		if onPanic != nil {
			onPanic()
		}
	}
}

func logPanic(label string, r any) {
	log.Printf("background panic in %s: %v\n%s", label, r, debug.Stack())
}
