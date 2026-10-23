package api

import "net/http"

// 性能面板端点（ops 硬化）：容器管理页顶部的宿主/容器资源占用快照。
// 数据源 docker info/stats + Linux /proc 采样 + 数据卷 statfs（见 service/perf.go）。

// performance 管理端：主机与全部容器的资源占用快照。
func (h *Handlers) performance(w http.ResponseWriter, req *http.Request) {
	stats, err := h.Env.HostStats(req.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
