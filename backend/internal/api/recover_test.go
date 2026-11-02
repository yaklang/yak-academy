package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 回归：handler panic 曾让连接被 net/http 直接关闭、响应不是契约错误信封，
// 且该请求不进入指标与访问日志。observe 恢复后必须返回统一 JSON 500，
// 并把 500 计入指标（进程存活由 net/http 保证，这里断言可观测性）。
func TestObserveRecoversHandlerPanic(t *testing.T) {
	r := newMetricsTestRouter(t, "")
	r.HandlePublic(http.MethodGet, "/panic-test", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic-test", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v (body %q)", err, rec.Body.String())
	}
	if env.Error.Code != errInternal.Code {
		t.Fatalf("error code = %q, want %q", env.Error.Code, errInternal.Code)
	}
	out := r.metrics.renderPrometheus()
	if !strings.Contains(out, `academy_http_requests_total{method="GET",route="/panic-test",status="500"} 1`) {
		t.Fatalf("panic request not recorded as 500:\n%s", out)
	}
}

// 已写出响应后 panic：不能再次写头（否则 httptest/真实连接报 superfluous
// WriteHeader），保留已发出的状态码与内容。
func TestObservePanicAfterHeadersSent(t *testing.T) {
	r := newMetricsTestRouter(t, "")
	r.HandlePublic(http.MethodGet, "/panic-after-write", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic-after-write", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (headers already sent)", rec.Code)
	}
	if body := rec.Body.String(); body != "partial" {
		t.Fatalf("body = %q, want original partial response", body)
	}
	if out := r.metrics.renderPrometheus(); !strings.Contains(out, `route="/panic-after-write",status="200"`) {
		t.Fatalf("request not recorded with sent status:\n%s", out)
	}
}
