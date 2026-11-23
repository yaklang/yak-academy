package api

import (
	"encoding/json"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yaklang/yak-academy/backend/internal/model"
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

func awdRoundID(req *http.Request) (uint, error) {
	id, err := strconv.ParseUint(req.PathValue("rid"), 10, 64)
	if err != nil || id == 0 {
		return 0, errValidation
	}
	return uint(id), nil
}

func (h *Handlers) adminStartAwdGame(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.Start(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.game.start", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminPauseAwdGame(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.Pause(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.game.pause", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminResumeAwdGame(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.Resume(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.game.resume", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminEndAwdGame(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.End(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.game.end", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminUploadAwdServicePackage(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, errValidation)
		return
	}
	file, _, err := req.FormFile("file")
	if err != nil {
		writeError(w, errValidation)
		return
	}
	defer file.Close()
	svc, err := h.AWD.UploadServicePackage(req.Context(), id, file)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.service.package", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"service_id": svc.ID, "slug": svc.Slug, "image": svc.Image})
	writeJSON(w, http.StatusCreated, svc)
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

// awdSubmitAttack 参赛者提交 flag：通过入队（202 pending），业务拒绝返回
// 200 + accepted=false（运营记录仍落库，便于赛后审计）。
func (h *Handlers) awdSubmitAttack(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		Flag string `json:"flag"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || strings.TrimSpace(body.Flag) == "" {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	receipt, err := h.AWD.SubmitAttack(req.Context(), id, user.ID, body.Flag)
	if err != nil {
		mapError(w, err)
		return
	}
	status := http.StatusAccepted
	if receipt.Status == model.AwdAttackRejected {
		status = http.StatusOK
	}
	writeJSON(w, status, receipt)
}

// adminSettleAwdRound 结算回合：写 SLA/攻击/防守/checkdown 账本并重建排名。
func (h *Handlers) adminSettleAwdRound(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	rid, err := awdRoundID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.SettleRound(req.Context(), id, rid)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.round.settle", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"round_id": rid, "events": view.Events})
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminAwdConsole(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.AdminConsole(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminDeployAwdTargets(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	result, err := h.AWD.DeployTargets(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.deploy.targets", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"created": result.Created, "reused": result.Reused})
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) adminDeployAwdAttackBoxes(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	result, err := h.AWD.DeployAttackBoxes(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.deploy.attack_boxes", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"created": result.Created, "reused": result.Reused})
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) adminStartAwdRound(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	result, err := h.AWD.StartRound(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.round.start", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"round_id": result.Round.ID, "number": result.Round.Number, "flags": result.Flags})
	writeJSON(w, http.StatusOK, result)
}

func (h *Handlers) adminListAwdStandings(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	items, err := h.AWD.Standings(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handlers) adminAwdFinalRanking(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.AWD.FinalRanking(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func writeAwdCSV(w http.ResponseWriter, data []byte, filename string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handlers) adminAwdFinalRankingCSV(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	data, filename, err := h.AWD.FinalRankingCSV(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeAwdCSV(w, data, filename)
}

func (h *Handlers) adminAwdAttacksCSV(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	data, filename, err := h.AWD.AttacksCSV(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeAwdCSV(w, data, filename)
}

func (h *Handlers) adminAwdIncidentsCSV(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	data, filename, err := h.AWD.IncidentsCSV(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeAwdCSV(w, data, filename)
}

func (h *Handlers) adminCreateAwdIncident(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.AwdIncidentCreate
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	incident, err := h.AWD.CreateIncident(req.Context(), id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.incident.create", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"incident_id": incident.ID, "kind": incident.Kind})
	writeJSON(w, http.StatusCreated, incident)
}

func awdReportID(req *http.Request) (uint, error) {
	id, err := strconv.ParseUint(req.PathValue("rid"), 10, 64)
	if err != nil || id == 0 {
		return 0, errValidation
	}
	return uint(id), nil
}

// awdGetReport 参赛者自己的战报（未提交返回 report=null）。
func (h *Handlers) awdGetReport(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	report, err := h.AWD.MyReport(req.Context(), id, user.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report})
}

func (h *Handlers) awdSaveReport(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		ContentMD string `json:"content_md"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	report, err := h.AWD.SaveReport(req.Context(), id, user.ID, body.ContentMD)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handlers) awdUploadReportZip(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, errValidation)
		return
	}
	file, _, err := req.FormFile("file")
	if err != nil {
		writeError(w, errValidation)
		return
	}
	defer file.Close()
	user := UserFrom(req.Context())
	report, err := h.AWD.UploadReportZip(req.Context(), id, user.ID, file)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, report)
}

func (h *Handlers) adminListAwdReports(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	items, err := h.AWD.ListReports(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handlers) adminReviewAwdReport(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	rid, err := awdReportID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.AwdReportReviewInput
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	report, err := h.AWD.ReviewReport(req.Context(), id, rid, user.ID, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "awd.report.review", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"report_id": rid, "score": body.Score})
	writeJSON(w, http.StatusCreated, report)
}

func (h *Handlers) adminDownloadAwdReportZip(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	rid, err := awdReportID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	path, name, err := h.AWD.ReportZipPath(req.Context(), id, rid)
	if err != nil {
		mapError(w, err)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)})
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Type", "application/zip")
	http.ServeFile(w, req, path)
}

// awdParticipantView 参赛者 AWD 首页（需已入场；只返回本人服务与攻击盒凭据）。
func (h *Handlers) awdParticipantView(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	view, err := h.AWD.ParticipantView(req.Context(), id, user.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) awdParticipantScoreboard(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	items, err := h.AWD.ParticipantScoreboard(req.Context(), id, user.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handlers) awdFinalRanking(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	view, err := h.AWD.ParticipantFinalRanking(req.Context(), id, user.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
