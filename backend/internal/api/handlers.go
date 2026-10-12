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
	Auth     *service.AuthService
	Content  *service.ContentService
	Judge    *service.JudgeService
	Progress *service.UserProgressService
	Topics   *service.TopicService
}

// NewHandlers 构造 handler 集合。
func NewHandlers(auth *service.AuthService, content *service.ContentService, judge *service.JudgeService, progress *service.UserProgressService, topics *service.TopicService) *Handlers {
	return &Handlers{Auth: auth, Content: content, Judge: judge, Progress: progress, Topics: topics}
}

func (h *Handlers) Register(r *Router) {
	// 公开接口（仅登录）
	r.HandlePublic(http.MethodPost, "/api/v1/auth/login", h.login)

	// student：浏览、提交、进度、排行榜
	r.Handle(http.MethodGet, "/api/v1/me", "", h.me)
	r.Handle(http.MethodGet, "/api/v1/topics", "", h.listTopics)
	r.Handle(http.MethodGet, "/api/v1/topics/{topicSlug}", "", h.getTopic)
	r.Handle(http.MethodGet, "/api/v1/topics/{topicSlug}/rooms/{roomSlug}", "", h.getRoom)
	r.Handle(http.MethodPost, "/api/v1/submissions", "", h.submitAnswer)
	r.Handle(http.MethodGet, "/api/v1/submissions/{id}", "", h.getSubmission)
	r.Handle(http.MethodGet, "/api/v1/learning-paths", "", h.listPaths)
	r.Handle(http.MethodGet, "/api/v1/learning-paths/{id}", "", h.getPath)
	r.Handle(http.MethodGet, "/api/v1/progress", "", h.myProgress)
	r.Handle(http.MethodGet, "/api/v1/leaderboard", "", h.leaderboard)

	// teacher：内容包上传与查询
	r.Handle(http.MethodPost, "/api/v1/content/packages", "teacher", h.uploadPackage)
	r.Handle(http.MethodGet, "/api/v1/content/packages/{id}", "teacher", h.getPackage)

	// admin：发布与用户管理
	r.Handle(http.MethodPost, "/api/v1/content/packages/{id}/publish", "admin", h.publishPackage)
	r.Handle(http.MethodPost, "/api/v1/admin/users", "admin", h.createUser)
	r.Handle(http.MethodPatch, "/api/v1/admin/users/{id}/roles", "admin", h.updateRoles)
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
	type roomResp struct {
		Slug       string           `json:"slug"`
		TopicSlug  string           `json:"topic_slug"`
		Title      string           `json:"title"`
		Difficulty model.Difficulty `json:"difficulty"`
		LessonMD   string           `json:"lesson_md"`
		Lab        *model.Challenge `json:"lab"`
	}
	resp := roomResp{
		Slug: detail.Room.Slug, TopicSlug: detail.Room.TopicSlug,
		Title: detail.Room.Title, Difficulty: detail.Room.Difficulty,
		LessonMD: detail.Room.LessonMD, Lab: detail.Challenge,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handlers) submitAnswer(w http.ResponseWriter, req *http.Request) {
	// TODO(Phase2): room_slug → challengeID 解析后调 Judge.Submit。
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "submit answer"})
}

func (h *Handlers) getSubmission(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseUint(req.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	// TODO(Phase2): Judge.GetSubmission(ctx, id)。
	_ = id
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "get submission"})
}

func (h *Handlers) listPaths(w http.ResponseWriter, req *http.Request) {
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "list learning paths"})
}

func (h *Handlers) getPath(w http.ResponseWriter, req *http.Request) {
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "get learning path"})
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
	data, err := io.ReadAll(file)
	if err != nil {
		mapError(w, err)
		return
	}
	pkg, err := h.Content.Upload(req.Context(), data)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	writeJSON(w, http.StatusCreated, pkg)
}

func (h *Handlers) getPackage(w http.ResponseWriter, req *http.Request) {
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "get package"})
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

func (h *Handlers) createUser(w http.ResponseWriter, req *http.Request) {
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "create user"})
}

func (h *Handlers) updateRoles(w http.ResponseWriter, req *http.Request) {
	writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, "update roles"})
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