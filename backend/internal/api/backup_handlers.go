package api

import (
	"io"
	"net/http"
	"strconv"
)

// 备份管理端点（ops 硬化）：创建 = SQLite 一致性快照 + 数据目录 tar.gz；
// 下载 = tar.gz 流式下发。PostgreSQL 模式创建返回 422（外部 pg_dump）。

// createBackup 管理端：立即生成一份备份（同步；百 MB 级秒级完成），
// 超出保留份数的旧归档自动清理。
func (h *Handlers) createBackup(w http.ResponseWriter, req *http.Request) {
	info, err := h.Backup.Create(req.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "backup.create", "backup", info.Name, map[string]any{"size_bytes": info.SizeBytes})
	writeJSON(w, http.StatusCreated, info)
}

// listBackups 管理端：现存归档列表（新→旧，受保留份数约束）。
func (h *Handlers) listBackups(w http.ResponseWriter, req *http.Request) {
	items, err := h.Backup.List(req.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// downloadBackup 管理端：下载归档 tar.gz（离机保存 / 异地容灾）。
func (h *Handlers) downloadBackup(w http.ResponseWriter, req *http.Request) {
	f, size, err := h.Backup.Open(req.Context(), req.PathValue("name"))
	if err != nil {
		mapError(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+req.PathValue("name")+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}
