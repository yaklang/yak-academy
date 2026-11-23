package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/yaklang/yak-academy/backend/internal/service"
)

// 证书：管理端赛后按排名/分数预览与签发；公开端凭验证码读取证书快照。

func (h *Handlers) adminListCertificates(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.Certificates.AdminCertificates(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminPreviewCertificates(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.CertificateRuleInput
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.Certificates.CertificatePreview(req.Context(), id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminIssueCertificates(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.CertificateRuleInput
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	result, err := h.Certificates.IssueCertificates(req.Context(), user.ID, id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "competition.certificate.issue", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"rule_id": result.Rule.ID, "title": result.Rule.Title, "issued": len(result.Certificates)})
	writeJSON(w, http.StatusCreated, result)
}

// verifyCertificate 公开验证（无需登录；只返回证书快照，不含账号标识）。
func (h *Handlers) verifyCertificate(w http.ResponseWriter, req *http.Request) {
	view, err := h.Certificates.VerifyCertificate(req.Context(), req.PathValue("code"))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
