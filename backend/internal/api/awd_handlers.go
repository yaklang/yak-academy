package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/yaklang/yak-academy/backend/internal/service"
)

// AWD 管理端：比赛配置 + 服务定义。AWD 与 CTF 路由/服务独立，
// 仅复用 Competition 的 format 校验与入场锁定中间件。

func (h *Handlers) adminGetAwdGame(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.Game(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminUpdateAwdGame(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.AwdGameUpdate
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.UpdateGame(req.Context(), id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.game.update", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminListAwdServices(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.Game(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": view.Services})
}

func awdServiceID(req *http.Request) (uint, error) {
	id, err := strconv.ParseUint(req.PathValue("sid"), 10, 64)
	if err != nil || id == 0 {
		return 0, errValidation
	}
	return uint(id), nil
}

func (h *Handlers) adminCreateAwdService(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.AwdServiceInput
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	svc, err := h.AWD.CreateService(req.Context(), id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.service.create", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"service_id": svc.ID, "slug": svc.Slug})
	writeJSON(w, http.StatusCreated, svc)
}

func (h *Handlers) adminUpdateAwdService(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	sid, err := awdServiceID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.AwdServiceInput
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	svc, err := h.AWD.UpdateService(req.Context(), id, sid, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.service.update", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"service_id": sid, "slug": svc.Slug})
	writeJSON(w, http.StatusOK, svc)
}

func (h *Handlers) adminDeleteAwdService(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	sid, err := awdServiceID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	if err := h.AWD.DeleteService(req.Context(), id, sid); err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.service.delete", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"service_id": sid})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
