package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// 回归：collector 的 requests/duration 是普通 map，observe 在并发请求
// goroutine 里直接写；并发完成窗口一旦重叠，Go runtime 报
// `fatal error: concurrent map writes` 并终止进程（fatal 不可 recover）。
// 修复前该测试在 fork 的测试进程内直接触发 fatal（进程退出，无法断言）；
// 修复后全部请求完成且计数准确。
func TestMetricsCollectorConcurrentRecord(t *testing.T) {
	r := newMetricsTestRouter(t, "")
	r.HandlePublic(http.MethodGet, "/concurrent-metrics", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	const (
		workers   = 16
		perWorker = 200
	)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // 尽量让所有 worker 同时进入 record 窗口
			for j := 0; j < perWorker; j++ {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/concurrent-metrics", nil))
			}
		}()
	}
	close(start)
	wg.Wait()

	out := r.metrics.renderPrometheus()
	want := `academy_http_requests_total{method="GET",route="/concurrent-metrics",status="200"} 3200`
	if !strings.Contains(out, want) {
		t.Fatalf("concurrent requests not fully counted:\n%s", out)
	}
}
