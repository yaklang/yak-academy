package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

// Handlers 持有全部 service 依赖。handler 只做参数解析 → 调 service → 统一响应。
type Handlers struct {
	Auth      *service.AuthService
	Content   *service.ContentService
	Challenge *service.ChallengeService
	Judge     *service.JudgeService
	Progress  *service.UserProgressService
	Topics    *service.TopicService
	Env       *service.EnvService
	Badges    *service.BadgeService
}

// NewHandlers 构造 handler 集合。
func NewHandlers(auth *service.AuthService, content *service.ContentService, challenge *service.ChallengeService, judge *service.JudgeService, progress *service.UserProgressService, topics *service.TopicService, env *service.EnvService, badges *service.BadgeService) *Handlers {
	return &Handlers{Auth: auth, Content: content, Challenge: challenge, Judge: judge, Progress: progress, Topics: topics, Env: env, Badges: badges}
}

func (h *Handlers) Register(r *Router) {
	// 公开接口：登录（JWT）、检测点回调（token 能力鉴权，容器内 checker 调用）
	// 与健康检查（进程存活探针，无鉴权；compose healthcheck 用）。
	r.HandlePublic(http.MethodPost, "/api/v1/auth/login", h.login)
	r.HandlePublic(http.MethodPost, "/api/v1/checkpoints/{token}", h.checkpoint)
	r.HandlePublic(http.MethodGet, "/health", h.health)

	// student：浏览、提交、进度、排行榜
	r.Handle(http.MethodGet, "/api/v1/me", "", h.me)
	r.Handle(http.MethodPost, "/api/v1/auth/logout", "", h.logout)
	r.Handle(http.MethodPost, "/api/v1/auth/change-password", "", h.changePassword)
	r.Handle(http.MethodGet, "/api/v1/topics", "", h.listTopics)
	r.Handle(http.MethodGet, "/api/v1/topics/{topicSlug}", "", h.getTopic)
	r.Handle(http.MethodPut, "/api/v1/topics/{topicSlug}", "admin", h.updateTopic)
	r.Handle(http.MethodGet, "/api/v1/topics/{topicSlug}/rooms/{roomSlug}", "", h.getRoom)
	r.Handle(http.MethodPost, "/api/v1/submissions", "", h.submitAnswer)
	r.Handle(http.MethodGet, "/api/v1/submissions/{id}", "", h.getSubmission)
	r.Handle(http.MethodPost, "/api/v1/challenges/{slug}/instance", "", h.startInstance)
	r.Handle(http.MethodGet, "/api/v1/challenges/{slug}/instance", "", h.getInstance)
	r.Handle(http.MethodDelete, "/api/v1/challenges/{slug}/instance", "", h.destroyInstance)
	r.Handle(http.MethodPost, "/api/v1/challenges/{slug}/instance/extend", "", h.extendInstance)
	r.Handle(http.MethodGet, "/api/v1/challenges", "", h.listChallenges)
	r.Handle(http.MethodGet, "/api/v1/challenges/{slug}", "", h.getChallenge)
	r.Handle(http.MethodGet, "/api/v1/challenges/{slug}/attachments/{filename}", "", h.downloadAttachment)
	r.Handle(http.MethodPost, "/api/v1/challenges/{slug}/hints/{hintId}/unlock", "", h.unlockHint)
	r.Handle(http.MethodGet, "/api/v1/progress", "", h.myProgress)
	r.Handle(http.MethodGet, "/api/v1/leaderboard", "", h.leaderboard)
	r.Handle(http.MethodGet, "/api/v1/badges", "", h.listBadges)

	// admin：内容包、题目、路径、容器、用户（普通人无管理端能力）
	r.Handle(http.MethodPost, "/api/v1/content/packages", "admin", h.uploadPackage)
	r.Handle(http.MethodGet, "/api/v1/content/packages", "admin", h.listPackages)
	r.Handle(http.MethodGet, "/api/v1/content/packages/{id}", "admin", h.getPackage)
	r.Handle(http.MethodGet, "/api/v1/content/packages/{id}/versions", "admin", h.listVersions)
	r.Handle(http.MethodPost, "/api/v1/content/packages/{id}/publish", "admin", h.publishPackage)
	r.Handle(http.MethodPost, "/api/v1/content/packages/{id}/archive", "admin", h.archivePackage)
	r.Handle(http.MethodPost, "/api/v1/content/packages/{id}/rollback", "admin", h.rollbackPackage)
	r.Handle(http.MethodGet, "/api/v1/admin/challenges", "admin", h.adminListChallenges)
	r.Handle(http.MethodPost, "/api/v1/admin/challenges", "admin", h.uploadChallenges)
	r.Handle(http.MethodPost, "/api/v1/admin/challenges/package", "admin", h.uploadChallengePackage)
	r.Handle(http.MethodDelete, "/api/v1/admin/challenges/{slug}", "admin", h.deleteChallenge)
	r.Handle(http.MethodPatch, "/api/v1/admin/challenges/{slug}/visibility", "admin", h.updateChallengeVisibility)
	r.Handle(http.MethodGet, "/api/v1/admin/challenges/{slug}/attachments", "admin", h.listAttachments)
	r.Handle(http.MethodPost, "/api/v1/admin/challenges/{slug}/attachments", "admin", h.uploadAttachment)
	r.Handle(http.MethodDelete, "/api/v1/admin/challenges/{slug}/attachments/{filename}", "admin", h.deleteAttachment)
	r.Handle(http.MethodGet, "/api/v1/admin/lab-instances", "admin", h.listInstances)
	r.Handle(http.MethodGet, "/api/v1/admin/lab-instances/active", "admin", h.listActiveInstances)
	r.Handle(http.MethodGet, "/api/v1/admin/lab-instances/logs", "admin", h.listInstanceLogs)
	r.Handle(http.MethodDelete, "/api/v1/admin/lab-instances/{id}", "admin", h.adminDestroyInstance)
	r.Handle(http.MethodGet, "/api/v1/admin/users", "admin", h.listUsers)
	r.Handle(http.MethodPost, "/api/v1/admin/users", "admin", h.createUser)
	r.Handle(http.MethodPatch, "/api/v1/admin/users/{id}/roles", "admin", h.updateRoles)
	r.Handle(http.MethodPost, "/api/v1/admin/users/{id}/reset-password", "admin", h.resetPasswordByAdmin)
}

// health 存活探针：无鉴权静态响应（部署编排健康检查用，不暴露内部状态）。
func (h *Handlers) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handlers) login(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
		writeError(w, errValidation)
		return
	}
	token, expires, err := h.Auth.Login(req.Context(), body.Username, body.Password)
	if err != nil {
		// 登录失败与缺 token 区分文案，避免前端误报「invalid or missing token」。
		writeError(w, &apiError{"UNAUTHORIZED", http.StatusUnauthorized, "invalid username or password"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": expires})
}

func (h *Handlers) me(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	writeJSON(w, http.StatusOK, user)
}

// logout 登出：按当前 token 的 jti 写撤销表（吊销至自然过期）。
func (h *Handlers) logout(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	if err := h.Auth.Logout(req.Context(), user.ID, TokenFrom(req)); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// changePassword 本人改密：成功后该用户全部 token 失效（前端需跳登录页）。
func (h *Handlers) changePassword(w http.ResponseWriter, req *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	if err := h.Auth.ChangePassword(req.Context(), user.ID, body.CurrentPassword, body.NewPassword); err != nil {
		if errors.Is(err, service.ErrInvalidCredential) {
			writeError(w, &apiError{"UNAUTHORIZED", http.StatusUnauthorized, "current password incorrect"})
			return
		}
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// resetPasswordByAdmin 管理员重置密码：目标用户全部 token 立即失效。
func (h *Handlers) resetPasswordByAdmin(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errNotFound)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	if err := h.Auth.ResetPasswordByAdmin(req.Context(), uint(id), body.Password); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handlers) listTopics(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	items, total, err := h.Topics.ListTopics(req.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

func (h *Handlers) getTopic(w http.ResponseWriter, req *http.Request) {
	detail, err := h.Topics.GetTopic(req.Context(), req.PathValue("topicSlug"))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *Handlers) getRoom(w http.ResponseWriter, req *http.Request) {
	detail, err := h.Topics.GetRoomDetail(req.Context(), req.PathValue("topicSlug"), req.PathValue("roomSlug"))
	if err != nil {
		mapError(w, err)
		return
	}
	user := UserFrom(req.Context())
	// 浏览即记录 in-progress（completed=false 行，幂等；进度是事件派生的物化视图）。
	if err := h.Progress.MarkRoomViewed(req.Context(), user.ID, detail.Room.Slug); err != nil {
		mapError(w, err)
		return
	}
	// related_challenge_slug：题目独立资源，Room 只反查关联（tutorial_slug 指向本 Room）。
	var related *string
	if detail.Challenge != nil {
		slug := detail.Challenge.Slug
		related = &slug
	}
	type roomResp struct {
		Slug       string           `json:"slug"`
		TopicSlug  string           `json:"topic_slug"`
		Title      string           `json:"title"`
		Difficulty model.Difficulty `json:"difficulty"`
		LessonMD   string           `json:"lesson_md"`
		VideoURL   string           `json:"video_url"`
		// 可空：无关联题目时为 null（前端显示"前往题目"用）。
		RelatedChallengeSlug *string `json:"related_challenge_slug"`
	}
	resp := roomResp{
		Slug: detail.Room.Slug, TopicSlug: detail.Room.TopicSlug,
		Title: detail.Room.Title, Difficulty: detail.Room.Difficulty,
		LessonMD: detail.Room.LessonMD, VideoURL: detail.Room.VideoURL,
		RelatedChallengeSlug: related,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) submitAnswer(w http.ResponseWriter, req *http.Request) {
	var body struct {
		ChallengeSlug string `json:"challenge_slug"`
		Answer        string `json:"answer"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.ChallengeSlug == "" || body.Answer == "" {
		writeError(w, errValidation)
		return
	}
	chal, err := h.Topics.FindChallengeBySlug(req.Context(), body.ChallengeSlug)
	if err != nil {
		mapError(w, err)
		return
	}
	user := UserFrom(req.Context())
	sub, err := h.Judge.Submit(req.Context(), user.ID, chal.ID, body.Answer)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, sub)
}

func (h *Handlers) getSubmission(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	sub, err := h.Judge.GetSubmission(req.Context(), user.ID, uint(id))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

// resolveChallenge 实例操作前的公共解析：按题目 slug 查（题目独立资源的主键入口）。
func (h *Handlers) resolveChallenge(req *http.Request) (*model.Challenge, error) {
	return h.Topics.FindChallengeBySlug(req.Context(), req.PathValue("slug"))
}

func (h *Handlers) startInstance(w http.ResponseWriter, req *http.Request) {
	chal, err := h.resolveChallenge(req)
	if err != nil {
		mapError(w, err)
		return
	}
	user := UserFrom(req.Context())
	inst, err := h.Env.EnsureInstance(req.Context(), user.ID, chal.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h *Handlers) getInstance(w http.ResponseWriter, req *http.Request) {
	chal, err := h.resolveChallenge(req)
	if err != nil {
		mapError(w, err)
		return
	}
	user := UserFrom(req.Context())
	inst, err := h.Env.GetInstance(req.Context(), user.ID, chal.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst) // 无实例时为 null（契约 nullable）
}

func (h *Handlers) destroyInstance(w http.ResponseWriter, req *http.Request) {
	chal, err := h.resolveChallenge(req)
	if err != nil {
		mapError(w, err)
		return
	}
	user := UserFrom(req.Context())
	if err := h.Env.DestroyInstance(req.Context(), user.ID, chal.ID); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handlers) extendInstance(w http.ResponseWriter, req *http.Request) {
	chal, err := h.resolveChallenge(req)
	if err != nil {
		mapError(w, err)
		return
	}
	user := UserFrom(req.Context())
	inst, err := h.Env.ExtendInstance(req.Context(), user.ID, chal.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h *Handlers) unlockHint(w http.ResponseWriter, req *http.Request) {
	hintID, err := strconv.ParseUint(req.PathValue("hintId"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	user := UserFrom(req.Context())
	res, err := h.Challenge.UnlockHint(req.Context(), user.ID, req.PathValue("slug"), uint(hintID))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// listChallenges 训练场列表（跨分类全量题目，当前用户视角）。
func (h *Handlers) listChallenges(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	page, pageSize := pageParams(req)
	items, total, err := h.Topics.ListTraining(req.Context(), user.ID, (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

// getChallenge 题目详情（描述 markdown + hints + 关联教程 + 附件列表）。
// 附件元数据在 ChallengeService，浏览视图在 TopicService——在此薄拼装，避免跨 service 注入。
func (h *Handlers) getChallenge(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	slug := req.PathValue("slug")
	detail, err := h.Topics.GetChallengeDetail(req.Context(), user.ID, slug)
	if err != nil {
		mapError(w, err)
		return
	}
	if detail.Attachments, err = h.Challenge.ListStudentAttachments(req.Context(), slug); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// updateChallengeVisibility 管理端：更新题目可见性/定时发布。
// release_at 三态：key 缺省 = 不改；JSON null = 清空定时；RFC3339 值 = 设置。
// （*string 无法区分缺省与 null，用 RawMessage 按 key 判定。）
func (h *Handlers) updateChallengeVisibility(w http.ResponseWriter, req *http.Request) {
	var body struct {
		IsVisible *bool             `json:"is_visible"`
		ReleaseAt json.RawMessage   `json:"release_at"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	if body.IsVisible == nil && body.ReleaseAt == nil {
		writeError(w, errValidation)
		return
	}
	u := service.VisibilityUpdate{IsVisible: body.IsVisible}
	if body.ReleaseAt != nil {
		if string(body.ReleaseAt) == "null" {
			u.ClearRelease = true
		} else {
			var s string
			if err := json.Unmarshal(body.ReleaseAt, &s); err != nil {
				writeError(w, errValidation)
				return
			}
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				writeError(w, errValidation)
				return
			}
			u.ReleaseAt = &t
		}
	}
	chal, err := h.Challenge.SetVisibility(req.Context(), req.PathValue("slug"), u)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, chal)
}

// listAttachments 管理端：题目附件元数据列表。
func (h *Handlers) listAttachments(w http.ResponseWriter, req *http.Request) {
	list, err := h.Challenge.ListAttachments(req.Context(), req.PathValue("slug"))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// uploadAttachment 管理端：上传题目附件（multipart file 字段，单文件 ≤50MB）。
func (h *Handlers) uploadAttachment(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, errValidation)
		return
	}
	file, hdr, err := req.FormFile("file")
	if err != nil {
		writeError(w, errValidation)
		return
	}
	defer file.Close()
	att, err := h.Challenge.UploadAttachment(req.Context(), req.PathValue("slug"), hdr.Filename, file)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, att)
}

// deleteAttachment 管理端：删除单个附件（元数据 + 磁盘文件）。
func (h *Handlers) deleteAttachment(w http.ResponseWriter, req *http.Request) {
	if err := h.Challenge.DeleteAttachment(req.Context(), req.PathValue("slug"), req.PathValue("filename")); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// downloadAttachment 学生端：下载题目附件（octet-stream + Content-Disposition，
// 前端凭 Authorization header 拉取 blob 后保存）。
func (h *Handlers) downloadAttachment(w http.ResponseWriter, req *http.Request) {
	f, filename, err := h.Challenge.OpenAttachment(req.Context(), req.PathValue("slug"), req.PathValue("filename"))
	if err != nil {
		mapError(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if size, err := f.Seek(0, io.SeekEnd); err == nil && size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			mapError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

// uploadChallenges 管理端：上传题目定义 JSON（单对象或数组，upsert）。
func (h *Handlers) uploadChallenges(w http.ResponseWriter, req *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(req.Body, 4<<20)) // JSON 上限 4MB
	if err != nil {
		mapError(w, err)
		return
	}
	imported, slugs, err := h.Content.UploadChallenges(req.Context(), raw)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported, "slugs": slugs})
}

// uploadChallengePackage 管理端：上传实训常用题目包 zip（challenge.yml + 容器文件）。
func (h *Handlers) uploadChallengePackage(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, errValidation)
		return
	}
	file, _, err := req.FormFile("package")
	if err != nil {
		writeError(w, errValidation)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxPackageSize+1))
	if err != nil {
		mapError(w, err)
		return
	}
	if len(data) > maxPackageSize {
		writeError(w, &apiError{"VALIDATION_FAILED", http.StatusUnprocessableEntity, "package exceeds 50MB limit"})
		return
	}
	imported, slugs, err := h.Content.UploadChallengePackage(req.Context(), data)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported, "slugs": slugs})
}

// adminListChallenges 管理端：题目全量列表（题目管理表格）。
func (h *Handlers) adminListChallenges(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	items, total, err := h.Topics.AdminListChallenges(req.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

// deleteChallenge 管理端：删除题目（级联 flags/hints）。
func (h *Handlers) deleteChallenge(w http.ResponseWriter, req *http.Request) {
	if err := h.Challenge.DeleteChallenge(req.Context(), req.PathValue("slug")); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// listInstances 管理端：全部容器实例（分页）。
func (h *Handlers) listInstances(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	items, total, err := h.Env.ListInstances(req.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

// listActiveInstances 管理端：当前活跃容器概览（creating/running，含题目信息，无分页）。
func (h *Handlers) listActiveInstances(w http.ResponseWriter, req *http.Request) {
	items, err := h.Env.ListActiveInstances(req.Context())
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// listInstanceLogs 管理端：容器生命周期日志（分页）。
func (h *Handlers) listInstanceLogs(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	items, total, err := h.Env.ListLogs(req.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

// adminDestroyInstance 管理端：强制销毁实例。
func (h *Handlers) adminDestroyInstance(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	if err := h.Env.AdminDestroyInstance(req.Context(), uint(id)); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h *Handlers) myProgress(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	stats, err := h.Progress.MyProgress(req.Context(), user.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handlers) leaderboard(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	items, total, err := h.Progress.Leaderboard(req.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

// checkpoint 环境检测点回调（公开接口，token 即能力凭证）：
// token 命中活跃实例 → 入队自动判定（哨兵答案，异步判过）。
func (h *Handlers) checkpoint(w http.ResponseWriter, req *http.Request) {
	inst, err := h.Env.ResolveCheckpoint(req.Context(), req.PathValue("token"))
	if err != nil {
		mapError(w, err)
		return
	}
	if inst == nil {
		writeError(w, errNotFound)
		return
	}
	sub, err := h.Judge.AutoSolve(req.Context(), inst)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": sub.Status})
}

// listBadges 当前用户徽章全集（计算型读模型：目录 + earned）。
func (h *Handlers) listBadges(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	items, err := h.Badges.List(req.Context(), user.ID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

const maxPackageSize = 50 << 20 // 内容包 zip 上限 50MB

// uploadPackage 管理端：上传教程包 zip → content 五步管线（Parse → Validate → Persist），
// Publish 由管理员显式触发。超过 50MB 直接拒绝，避免大文件整体读入内存。
func (h *Handlers) uploadPackage(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, errValidation)
		return
	}
	file, _, err := req.FormFile("package")
	if err != nil {
		writeError(w, errValidation)
		return
	}
	defer file.Close()
	// LimitReader：超过 50MB 直接拒绝，避免把大文件整个读进内存。
	data, err := io.ReadAll(io.LimitReader(file, maxPackageSize+1))
	if err != nil {
		mapError(w, err)
		return
	}
	if len(data) > maxPackageSize {
		writeError(w, &apiError{"VALIDATION_FAILED", http.StatusUnprocessableEntity, "package exceeds 50MB limit"})
		return
	}
	pkg, err := h.Content.Upload(req.Context(), data)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	writeJSON(w, http.StatusCreated, pkg)
}

func (h *Handlers) listPackages(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	items, total, err := h.Content.ListPackages(req.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

func (h *Handlers) getPackage(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	pkg, err := h.Content.GetPackage(req.Context(), uint(id))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pkg)
}

func (h *Handlers) publishPackage(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	pkg, err := h.Content.Publish(req.Context(), uint(id))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pkg)
}

func (h *Handlers) listVersions(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	versions, err := h.Content.ListVersions(req.Context(), uint(id))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (h *Handlers) archivePackage(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	pkg, err := h.Content.Archive(req.Context(), uint(id))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pkg)
}

func (h *Handlers) rollbackPackage(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		VersionID uint `json:"version_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.VersionID == 0 {
		writeError(w, errValidation)
		return
	}
	pkg, err := h.Content.Rollback(req.Context(), uint(id), body.VersionID)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pkg)
}

func (h *Handlers) updateTopic(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	topic, err := h.Topics.UpdateTopic(req.Context(), req.PathValue("topicSlug"), body.Title, body.Summary)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, topic)
}

func (h *Handlers) listUsers(w http.ResponseWriter, req *http.Request) {
	page, pageSize := pageParams(req)
	items, total, err := h.Auth.ListUsers(req.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "page": page, "page_size": pageSize, "items": items,
	})
}

func (h *Handlers) createUser(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		GlobalRole string `json:"global_role"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	user, err := h.Auth.CreateUser(req.Context(), body.Username, body.Password, model.GlobalRole(body.GlobalRole))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handlers) updateRoles(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body struct {
		GlobalRole string `json:"global_role"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	user, err := h.Auth.UpdateRoles(req.Context(), uint(id), model.GlobalRole(body.GlobalRole))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func pageParams(req *http.Request) (page, pageSize int) {
	page, _ = strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(req.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}
