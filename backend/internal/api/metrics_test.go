package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /metrics 与 observe 中间件的防线：token 门控三态（关闭 404 / 错 token 403 /
// 对 token 200 + 文本格式）与请求指标按 route pattern 聚合。

func newMetricsTestRouter(t *testing.T, token string) *Router {
	t.Helper()
	r := NewRouter(nil)
	r.SetMetricsToken(token)
	r.routes = append(r.routes, RouteInfo{Method: http.MethodGet, Pattern: "/metrics", Public: true})
	r.mux.HandleFunc("GET /metrics", r.serveMetrics)
	return r
}

func TestMetricsEndpointTokenGate(t *testing.T) {
	// 端点关闭（未配置 token）：404，不泄漏端点存在性细节。
	r := newMetricsTestRouter(t, "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled endpoint: status = %d, want 404", rec.Code)
	}

	// 配置 token：错 token 403 → 对 token 200 + Prometheus 文本格式。
	r = newMetricsTestRouter(t, "scrape-secret")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token: status = %d, want 403", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer scrape-secret")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token: status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "academy_http_requests_total") ||
		!strings.Contains(body, "academy_uptime_seconds") ||
		!strings.Contains(body, "go_goroutines") {
		t.Fatalf("metrics body missing expected series:\n%s", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
}

func TestMetricsRecordObservedRequests(t *testing.T) {
	r := newMetricsTestRouter(t, "")
	r.HandlePublic(http.MethodGet, "/observed-test", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/observed-test", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request #%d: status = %d", i, rec.Code)
		}
	}
	out := r.metrics.renderPrometheus()
	if !strings.Contains(out, `academy_http_requests_total{method="GET",route="/observed-test",status="200"} 3`) {
		t.Fatalf("request counter not aggregated by route pattern:\n%s", out)
	}
	if !strings.Contains(out, `academy_http_request_duration_seconds_count{route="/observed-test"} 3`) {
		t.Fatalf("duration count missing:\n%s", out)
	}
}

func TestMetricsHealthProbeNotRecorded(t *testing.T) {
	r := newMetricsTestRouter(t, "")
	r.HandlePublic(http.MethodGet, "/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if out := r.metrics.renderPrometheus(); strings.Contains(out, `route="/health"`) {
		t.Fatalf("health probe must stay out of metrics:\n%s", out)
	}
}
