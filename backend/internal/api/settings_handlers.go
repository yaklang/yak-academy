package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yaklang/yak-academy/backend/internal/service"
)

// 系统设置（统一管理）：SMTP 配置读写与投递自检。密码只写不读；保存后
// service 立即热替换投递器，无需重启。

func (h *Handlers) getSMTPSettings(w http.ResponseWriter, req *http.Request) {
	view, err := h.Settings.SMTP(req.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) updateSMTPSettings(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Enabled  bool   `json:"enabled"`
		Addr     string `json:"addr"`
		Username string `json:"username"`
		Password string `json:"password"`
		From     string `json:"from"`
		TLS      string `json:"tls"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.Settings.UpdateSMTP(req.Context(), service.SMTPUpdateInput{
		Enabled:  body.Enabled,
		Addr:     body.Addr,
		Username: body.Username,
		Password: body.Password,
		From:     body.From,
		TLS:      body.TLS,
	}, UserFrom(req.Context()))
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "settings.smtp.update", "settings", "smtp", map[string]any{
		"enabled": view.Enabled, "addr": view.Addr, "tls": view.TLS, "password_changed": body.Password != "",
	})
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) testSMTPSettings(w http.ResponseWriter, req *http.Request) {
	var body struct {
		To string `json:"to"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	to := strings.TrimSpace(body.To)
	if err := h.Settings.TestSMTP(req.Context(), to); err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "settings.smtp.test", "settings", "smtp", map[string]any{"to": to})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
