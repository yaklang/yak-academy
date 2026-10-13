package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

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
}

// NewHandlers 构造 handler 集合。
func NewHandlers(auth *service.AuthService, content *service.ContentService, challenge *service.ChallengeService, judge *service.JudgeService, progress *service.UserProgressService, topics *service.TopicService, env *service.EnvService) *Handlers {
	return &Handlers{Auth: auth, Content: content, Challenge: challenge, Judge: judge, Progress: progress, Topics: topics, Env: env}
}

func (h *Handlers) Register(r *Router) {
	// 公开接口（仅登录）
	r.HandlePublic(http.MethodPost, "/api/v1/auth/login", h.login)

	// student：浏览、提交、进度、排行榜
	r.Handle(http.MethodGet, "/api/v1/me", "", h.me)
	r.Handle(http.MethodGet, "/api/v1/topics", "", h.listTopics)
	r.Handle(http.MethodGet, "/api/v1/topics/{topicSlug}", "", h.getTopic)
	r.Handle(http.MethodPut, "/api/v1/topics/{topicSlug}", "teacher", h.updateTopic)
	r.Handle(http.MethodGet, "/api/v1/topics/{topicSlug}/rooms/{roomSlug}", "", h.getRoom)
	r.Handle(http.MethodPost, "/api/v1/submissions", "", h.submitAnswer)
	r.Handle(http.MethodGet, "/api/v1/submissions/{id}", "", h.getSubmission)
	r.Handle(http.MethodPost, "/api/v1/challenges/{slug}/instance", "", h.startInstance)
	r.Handle(http.MethodGet, "/api/v1/challenges/{slug}/instance", "", h.getInstance)
	r.Handle(http.MethodDelete, "/api/v1/challenges/{slug}/instance", "", h.destroyInstance)
	r.Handle(http.MethodGet, "/api/v1/challenges", "", h.listChallenges)
	r.Handle(http.MethodGet, "/api/v1/challenges/{slug}", "", h.getChallenge)
	r.Handle(http.MethodGet, "/api/v1/learning-paths", "", h.listPaths)
	r.Handle(http.MethodGet, "/api/v1/learning-paths/{id}", "", h.getPath)
	r.Handle(http.MethodGet, "/api/v1/progress", "", h.myProgress)
	r.Handle(http.MethodGet, "/api/v1/leaderboard", "", h.leaderboard)

	// teacher：内容包上传与查询
	r.Handle(http.MethodPost, "/api/v1/content/packages", "teacher", h.uploadPackage)
	r.Handle(http.MethodGet, "/api/v1/content/packages", "teacher", h.listPackages)
	r.Handle(http.MethodGet, "/api/v1/content/packages/{id}", "teacher", h.getPackage)
	r.Handle(http.MethodGet, "/api/v1/content/packages/{id}/versions", "teacher", h.listVersions)

	// admin：发布/归档/回滚、题目管理、容器管理与用户管理
	r.Handle(http.MethodPost, "/api/v1/content/packages/{id}/publish", "admin", h.publishPackage)
	r.Handle(http.MethodPost, "/api/v1/content/packages/{id}/archive", "admin", h.archivePackage)
	r.Handle(http.MethodPost, "/api/v1/content/packages/{id}/rollback", "admin", h.rollbackPackage)
	r.Handle(http.MethodGet, "/api/v1/admin/challenges", "teacher", h.adminListChallenges)
	r.Handle(http.MethodPost, "/api/v1/admin/challenges", "admin", h.uploadChallenges)
	r.Handle(http.MethodDelete, "/api/v1/admin/challenges/{slug}", "admin", h.deleteChallenge)
	r.Handle(http.MethodGet, "/api/v1/admin/lab-instances", "admin", h.listInstances)
	r.Handle(http.MethodDelete, "/api/v1/admin/lab-instances/{id}", "admin", h.adminDestroyInstance)
	r.Handle(http.MethodGet, "/api/v1/admin/users", "admin", h.listUsers)
	r.Handle(http.MethodPost, "/api/v1/admin/users", "admin", h.createUser)
	r.Handle(http.MethodPatch, "/api/v1/admin/users/{id}/roles", "admin", h.updateRoles)
	r.Handle(http.MethodDelete, "/api/v1/admin/users/{id}/course-roles/{topicSlug}", "admin", h.deleteCourseRole)
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
		writeError(w, errUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": expires})
}

func (h *Handlers) me(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	writeJSON(w, http.StatusOK, user)
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

func (h *Handlers) listPaths(w http.ResponseWriter, req *http.Request) {
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "list learning paths"})
}

func (h *Handlers) getPath(w http.ResponseWriter, req *http.Request) {
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "get learning path"})
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

// getChallenge 题目详情（描述 markdown + hints + 关联教程）。
func (h *Handlers) getChallenge(w http.ResponseWriter, req *http.Request) {
	user := UserFrom(req.Context())
	detail, err := h.Topics.GetChallengeDetail(req.Context(), user.ID, req.PathValue("slug"))
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
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

const maxPackageSize = 50 << 20 // 内容包 zip 上限 50MB

func (h *Handlers) uploadPackage(w http.ResponseWriter, req *http.Request) {
	// TODO(Phase2): multipart 解析 zip → Content.Upload 五步管线。
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
		GlobalRole  string `json:"global_role"`
		CourseRoles []struct {
			TopicSlug string `json:"topic_slug"`
			Role      string `json:"role"`
		} `json:"course_roles"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	courseRoles := make([]model.CourseRole, len(body.CourseRoles))
	for i, cr := range body.CourseRoles {
		courseRoles[i] = model.CourseRole{TopicSlug: cr.TopicSlug, Role: cr.Role}
	}
	user, err := h.Auth.UpdateRoles(req.Context(), uint(id), model.GlobalRole(body.GlobalRole), courseRoles)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *Handlers) deleteCourseRole(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	if err := h.Auth.DeleteCourseRole(req.Context(), uint(id), req.PathValue("topicSlug")); err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
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