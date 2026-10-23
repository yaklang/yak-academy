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

// adminCreateCompetition 创建竞赛（kind 创建后不可更改）。
func (h *Handlers) adminCreateCompetition(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name        string    `json:"name"`
		Description string    `json:"description"`
		Kind        string    `json:"kind"`
		StartTime   time.Time `json:"start_time"`
		EndTime     time.Time `json:"end_time"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Name == "" {
		writeError(w, errValidation)
		return
	}
	comp, err := h.Competitions.Create(req.Context(), body.Name, body.Description, body.StartTime, body.EndTime, body.Kind)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "competition.create", "competition", strconv.FormatUint(uint64(comp.ID), 10),
		map[string]any{"name": body.Name, "kind": comp.Kind})
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
	h.audit(req, "competition.update", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, detail)
}

// adminEndCompetition 手动结束竞赛（仅 running；end_time 置为当前时刻）。
func (h *Handlers) adminEndCompetition(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	detail, err := h.Competitions.End(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "competition.end", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, detail)
}

// adminDeleteCompetition 删除竞赛（running 422 拒绝；级联清理见 service.Delete）。
func (h *Handlers) adminDeleteCompetition(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	if err := h.Competitions.Delete(req.Context(), id); err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "competition.delete", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	h.audit(req, "competition.bind", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"added": result.Added})
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
	h.audit(req, "competition.unbind", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"slugs": body.Slugs, "removed": removed})
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
	h.audit(req, "competition.import_participants", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"teams": len(body.Teams), "individuals": len(body.Individuals),
			"participants": len(result.Participants), "missing": len(result.Missing)})
	writeJSON(w, http.StatusOK, result)
}

// adminGenerateUsers 一键生成参赛账号（仅邀请赛；自动组队 + 初始密码导出）。
func (h *Handlers) adminGenerateUsers(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.GenerateUsersRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Prefix == "" {
		writeError(w, errValidation)
		return
	}
	result, err := h.Competitions.GenerateUsers(req.Context(), id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	created := 0
	for _, a := range result.Accounts {
		if a.Created {
			created++
		}
	}
	h.audit(req, "competition.generate_users", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"prefix": body.Prefix, "count": body.Count, "team_size": body.TeamSize, "created": created})
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

// listPublicCompetitions 竞赛广场：分页列出公开赛（含报名状态）。
func (h *Handlers) listPublicCompetitions(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	page, pageSize := pageParams(req)
	items, total, err := h.Competitions.ListPublic(req.Context(), user.ID, page, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

// registerCompetition 公开赛自助报名（幂等）。
func (h *Handlers) registerCompetition(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	item, err := h.Competitions.Register(req.Context(), user.ID, id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

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

// enterCompetition 入场：邀请码（邀请赛/公开赛通用）或公开赛已报名免码
// （body 可整体省略）。
func (h *Handlers) enterCompetition(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		InviteCode string `json:"invite_code"`
	}
	// 空请求体合法（公开赛已报名免码入场），仅解析失败拒绝。
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil && err != io.EOF {
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

// competitionMyWriteups 本队/本人全部 WP（结束后的 WP 标签页）。
func (h *Handlers) competitionMyWriteups(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	items, err := h.Competitions.MyWriteups(req.Context(), user.ID, id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// competitionSaveWriteup 比赛结束后提交/修改某题 WP 文字。
func (h *Handlers) competitionSaveWriteup(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	challengeID, err := strconv.ParseUint(req.PathValue("challengeId"), 10, 64)
	if err != nil || challengeID == 0 {
		writeError(w, errValidation)
		return
	}
	var body struct {
		ContentMd string `json:"content_md"`
	}
	// content_md 允许空串（仅保留 zip 场景），仅拒绝 JSON 解析失败。
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	if err := h.Competitions.SaveWriteup(req.Context(), user.ID, id, uint(challengeID), body.ContentMd); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// competitionUploadWriteupZip 比赛结束后上传/替换某题 WP 附带 zip（multipart file）。
func (h *Handlers) competitionUploadWriteupZip(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	challengeID, err := strconv.ParseUint(req.PathValue("challengeId"), 10, 64)
	if err != nil || challengeID == 0 {
		writeError(w, errValidation)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, service.MaxWriteupZipBytes+1<<20)
	file, hdr, err := req.FormFile("file")
	if err != nil {
		writeError(w, errValidation)
		return
	}
	defer file.Close()
	user := UserFrom(req.Context())
	name, err := h.Competitions.UploadWriteupZip(req.Context(), user.ID, id, uint(challengeID), hdr.Filename, file)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "zip_name": name})
}

// adminDownloadWriteupZip 管理端下载某份 WP 的附带 zip。
func (h *Handlers) adminDownloadWriteupZip(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	wid, err := strconv.ParseUint(req.PathValue("wid"), 10, 64)
	if err != nil || wid == 0 {
		writeError(w, errValidation)
		return
	}
	r, filename, size, err := h.Competitions.OpenWriteupZip(req.Context(), id, uint(wid))
	if err != nil {
		mapError(w, err)
		return
	}
	defer r.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, r)
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