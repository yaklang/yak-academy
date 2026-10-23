package api

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"
)

// 可观测性（ops 硬化）：Prometheus 文本格式指标端点 + 结构化访问日志。
// 指标按「注册的 route pattern」聚合（非原始路径），标签基数 = 路由数，
// 不会随 topicSlug/资源 ID 爆炸。访问日志走 slog（ACADEMY_LOG_JSON=1 时
// JSON 行，默认 text 键值行），采集/告警由外部 Prometheus + Alertmanager
// 完成（平台不内置，见 docs/architecture.md §11）。

// collector 进程内指标聚合（Router 持有；GetMetrics 导出给测试）。
type collector struct {
	start time.Time
	// requests[method|route|status] 计数；duration[route] 累计 count/sum。
	requests map[string]uint64
	duration map[string]*durAccum
}

type durAccum struct {
	count uint64
	sumMs float64
}

func newCollector() *collector {
	return &collector{
		start:    time.Now(),
		requests: map[string]uint64{},
		duration: map[string]*durAccum{},
	}
}

func (c *collector) record(method, route string, status int, d time.Duration) {
	key := method + "|" + route + "|" + fmt.Sprint(status)
	c.requests[key]++
	acc := c.duration[route]
	if acc == nil {
		acc = &durAccum{}
		c.duration[route] = acc
	}
	acc.count++
	acc.sumMs += float64(d.Microseconds()) / 1000.0
}

// renderPrometheus 输出 Prometheus 文本格式（计数器行序确定，输出可比对）。
func (c *collector) renderPrometheus() string {
	var b strings.Builder
	b.WriteString("# HELP academy_http_requests_total Total HTTP requests by method, route pattern and status.\n")
	b.WriteString("# TYPE academy_http_requests_total counter\n")
	keys := make([]string, 0, len(c.requests))
	for k := range c.requests {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		parts := strings.SplitN(k, "|", 3)
		fmt.Fprintf(&b, "academy_http_requests_total{method=%q,route=%q,status=%q} %d\n",
			parts[0], parts[1], parts[2], c.requests[k])
	}
	b.WriteString("# HELP academy_http_request_duration_seconds HTTP request durations (sum + count).\n")
	b.WriteString("# TYPE academy_http_request_duration_seconds summary\n")
	routes := make([]string, 0, len(c.duration))
	for r := range c.duration {
		routes = append(routes, r)
	}
	slices.Sort(routes)
	for _, r := range routes {
		acc := c.duration[r]
		fmt.Fprintf(&b, "academy_http_request_duration_seconds_sum{route=%q} %.6f\n", r, acc.sumMs/1000.0)
		fmt.Fprintf(&b, "academy_http_request_duration_seconds_count{route=%q} %d\n", r, acc.count)
	}
	fmt.Fprintf(&b, "# HELP academy_uptime_seconds Process uptime in seconds.\n# TYPE academy_uptime_seconds gauge\nacademy_uptime_seconds %.3f\n",
		time.Since(c.start).Seconds())
	fmt.Fprintf(&b, "# HELP go_goroutines Number of goroutines.\n# TYPE go_goroutines gauge\ngo_goroutines %d\n", runtime.NumGoroutine())
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(&b, "# HELP go_memstats_alloc_bytes Bytes allocated and still in use.\n# TYPE go_memstats_alloc_bytes gauge\ngo_memstats_alloc_bytes %d\n", ms.Alloc)
	fmt.Fprintf(&b, "# HELP go_memstats_sys_bytes Bytes obtained from system.\n# TYPE go_memstats_sys_bytes gauge\ngo_memstats_sys_bytes %d\n", ms.Sys)
	return b.String()
}

// statusRecorder 捕获 handler 写下的状态码（指标与访问日志用）。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// serveMetrics /metrics 响应：未配置 token = 端点关闭（404，不暴露任何信息）；
// 配置后 Bearer 恒时比对，通过即下发文本指标。与 /checkpoints/{token} 同为
// 「token 即能力」的公开端点形态（Prometheus scrape 配 bearer_token 即可）。
func (r *Router) serveMetrics(w http.ResponseWriter, req *http.Request) {
	if r.metricsToken == "" {
		writeError(w, &apiError{"NOT_FOUND", http.StatusNotFound, "metrics endpoint disabled (set ACADEMY_METRICS_TOKEN to enable)"})
		return
	}
	provided := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(r.metricsToken)) != 1 {
		writeError(w, &apiError{"FORBIDDEN", http.StatusForbidden, "invalid metrics token"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(r.metrics.renderPrometheus()))
}

// observe 包装 handler：请求完成后记指标 + slog 访问日志（用户在鉴权中间件
// 注入 ctx 后可得，未登录路由 user_id 省略）。/metrics 与 /health 不包
//（探针/采集高频，避免日志噪音）。
func (r *Router) observe(method, pattern string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h(rec, req)
		d := time.Since(start)
		r.metrics.record(method, pattern, rec.status, d)
		attrs := []any{
			"method", method, "route", pattern, "path", req.URL.Path,
			"status", rec.status, "duration_ms", float64(d.Microseconds()) / 1000.0,
			"remote", req.RemoteAddr,
		}
		if u := UserFrom(req.Context()); u != nil {
			attrs = append(attrs, "user_id", u.ID)
		}
		r.accessLog.Info("http_request", attrs...)
	}
}

// EnableJSONLogs 访问日志切 JSON 行格式（ACADEMY_LOG_JSON=1；main 在开始
// 服务前调用一次，此后只读无竞态）。默认 text 键值行，同为结构化输出。
func (r *Router) EnableJSONLogs() {
	r.accessLog = slog.New(slog.NewJSONHandler(os.Stdout, nil))
}
