package api

// 竞赛域 handler：参数解析 → 调 CompetitionService → 统一响应。
// 鉴权在路由声明（admin / 登录即可），入场与锁定校验在 service 层。

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/service"
)

// maxCompetitionUpload 竞赛题目批量导入 JSON 的请求体上限（8MB，
// 覆盖几十题的数组导入，远低于题目包 zip 的 50MB）。
const maxCompetitionUpload = 8 << 20

// adminListCompetitions 竞赛列表（分页）。
func (h *Handlers) adminListCompetitions(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	items, total, err := h.Competitions.List(req.Context(), page, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

// adminCreateCompetition 创建竞赛。
func (h *Handlers) adminCreateCompetition(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name        string    `json:"name"`
		Description string    `json:"description"`
		StartTime   time.Time `json:"start_time"`
		EndTime     time.Time `json:"end_time"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, errValidation)
		return
	}
	comp, err := h.Competitions.Create(req.Context(), body.Name, body.Description, body.StartTime, body.EndTime)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, comp)
}

func compID(req *http.Request) (uint, error) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, errValidation
	}
	return uint(id), nil
}

// adminCompetitionDetail 竞赛详情（含计数统计）。
func (h *Handlers) adminCompetitionDetail(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	detail, err := h.Competitions.Detail(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// adminUpdateCompetition 修改竞赛配置。
func (h *Handlers) adminUpdateCompetition(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		Name        *string    `json:"name"`
		Description *string    `json:"description"`
		StartTime   *time.Time `json:"start_time"`
		EndTime     *time.Time `json:"end_time"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	detail, err := h.Competitions.Update(req.Context(), id, service.CompetitionUpdate{
		Name: body.Name, Description: body.Description,
		StartTime: body.StartTime, EndTime: body.EndTime,
	})
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// adminCompetitionChallenges 竞赛题目列表。
func (h *Handlers) adminCompetitionChallenges(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	items, err := h.Competitions.AdminChallenges(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// adminBindChallenges 题目绑定/批量导入：{"slugs": [...]} 绑既有题目，
// 其余按题目 JSON（对象/数组）走 content 导入管线后绑定。
func (h *Handlers) adminBindChallenges(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxCompetitionUpload))
	if err != nil {
		writeError(w, errValidation)
		return
	}
	result, err := h.Competitions.BindChallenges(req.Context(), id, raw, h.Content.UploadChallenges)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// adminUnbindChallenges 解绑题目（运行中的竞赛不允许）。
func (h *Handlers) adminUnbindChallenges(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		Slugs []string `json:"slugs"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || len(body.Slugs) == 0 {
		writeError(w, errValidation)
		return
	}
	removed, err := h.Competitions.UnbindChallenges(req.Context(), id, body.Slugs)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

// adminCompetitionParticipants 参赛人员与队伍（分页 + 队伍总览）。
func (h *Handlers) adminCompetitionParticipants(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	page, pageSize := pageParams(req)
	items, teams, total, err := h.Competitions.AdminParticipants(req.Context(), id, page, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize,
		"teams": teams, "items": items,
	})
}

// adminImportParticipants 参赛者与队伍批量导入。
func (h *Handlers) adminImportParticipants(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.ParticipantImportRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	if len(body.Teams) == 0 && len(body.Individuals) == 0 {
		writeError(w, errValidation)
		return
	}
	result, err := h.Competitions.ImportParticipants(req.Context(), id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// adminCompetitionInstances 竞赛容器分页（独立面板）。
func (h *Handlers) adminCompetitionInstances(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	page, pageSize := pageParams(req)
	items, total, err := h.Env.ListCompetitionInstances(req.Context(), id, (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

// adminCompetitionWarnings 反作弊警告（只在管理端展示）。
func (h *Handlers) adminCompetitionWarnings(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	items, err := h.Competitions.AdminWarnings(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// adminCompetitionWriteups WP 提交情况。
func (h *Handlers) adminCompetitionWriteups(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	items, err := h.Competitions.AdminWriteups(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// adminCompetitionLeaderboard 管理端竞赛排行榜（team / individual）。
func (h *Handlers) adminCompetitionLeaderboard(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	board, err := h.Competitions.Leaderboard(req.Context(), id, req.URL.Query().Get("mode"))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// ---------- 参赛者 ----------

// myCompetitionEntry 当前用户参赛/锁定状态（前端路由守卫）。
func (h *Handlers) myCompetitionEntry(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	entry, err := h.Competitions.MyEntry(req.Context(), user.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// enterCompetition 凭邀请码入场。
func (h *Handlers) enterCompetition(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		InviteCode string `json:"invite_code"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.InviteCode == "" {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	entry, err := h.Competitions.Enter(req.Context(), user.ID, id, body.InviteCode)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

// competitionView 竞赛首页（题目/队伍/我的提交/WP 状态）。
func (h *Handlers) competitionView(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	view, err := h.Competitions.View(req.Context(), user.ID, id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// competitionStartInstance 竞赛内启动题目容器。
func (h *Handlers) competitionStartInstance(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	inst, err := h.Competitions.StartInstance(req.Context(), h.Env, user.ID, id, req.PathValue("slug"))
	if err != nil {
		if isInstanceConflict(err) {
			writeConflict(w, err)
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

// competitionSubmitFlag 竞赛内提交 flag（异步判定）。
func (h *Handlers) competitionSubmitFlag(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		ChallengeSlug string `json:"challenge_slug"`
		Answer       string `json:"answer"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.ChallengeSlug == "" || body.Answer == "" {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	sub, err := h.Competitions.SubmitFlag(req.Context(), h.Judge, user.ID, id, body.ChallengeSlug, body.Answer, sourceIP(req))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sub)
}

// competitionLeaderboard 参赛者侧竞赛排行榜。
func (h *Handlers) competitionLeaderboard(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	board, err := h.Competitions.Leaderboard(req.Context(), id, req.URL.Query().Get("mode"))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// competitionSubmitWriteup 比赛结束后提交 WP。
func (h *Handlers) competitionSubmitWriteup(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		ContentMd string `json:"content_md"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.ContentMd == "" {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	if err := h.Competitions.SubmitWriteup(req.Context(), user.ID, id, body.ContentMd); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// sourceIP 提交来源 IP（反作弊基线）：优先反向代理头（部署在 nginx 后面），
// 回退 RemoteAddr（去端口）。
func sourceIP(req *http.Request) string {
	if fwd := req.Header.Get("X-Forwarded-For"); fwd != "" {
		if host, _, err := net.SplitHostPort(fwd); err == nil {
			return host
		}
		return fwd
	}
	if real := req.Header.Get("X-Real-IP"); real != "" {
		return real
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}