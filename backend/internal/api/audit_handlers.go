package api

import (
	"log"
	"net/http"
	"strconv"

	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 管理操作审计（ops 硬化）：全部 admin 写操作 handler 成功路径调 audit()
// 落一条 append-only 审计行。best-effort：主操作已成功，审计写失败仅服务
// 日志告警、不阻断响应（回滚业务换审计完整性的代价不成比例）。

// audit 记一条管理操作（当前登录管理员取自请求上下文）。
func (h *Handlers) audit(req *http.Request, action, targetType, targetID string, detail any) {
	if h.Audit == nil {
		return
	}
	admin := UserFrom(req.Context())
	if err := h.Audit.Record(req.Context(), admin, action, targetType, targetID, req.RemoteAddr, detail); err != nil {
		log.Printf("audit %s %s/%s: %v", action, targetType, targetID, err)
	}
}

// listAuditLogs 管理端：审计分页查询（新→旧，可按管理员/动作过滤）。
func (h *Handlers) listAuditLogs(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	q := req.URL.Query()
	filter := repo.AuditFilter{Action: q.Get("action")}
	if v := q.Get("admin_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			writeError(w, errValidation)
			return
		}
		filter.AdminID = uint(id)
	}
	items, total, err := h.Audit.List(req.Context(), filter, (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}
