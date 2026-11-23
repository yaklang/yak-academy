package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/yaklang/yak-academy/backend/internal/service"
)

// 管理端考试：试卷配置 + 试题 CRUD。考试域接口与 AWD/CTF 完全分离，
// 仅复用 /admin/competitions 前缀与 admin 鉴权。

func examQuestionID(req *http.Request) (uint, error) {
	id, err := strconv.ParseUint(req.PathValue("qid"), 10, 64)
	if err != nil || id == 0 {
		return 0, errValidation
	}
	return uint(id), nil
}

func (h *Handlers) adminGetExamPaper(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.Exam.Paper(req.Context(), id)
	if err != nil {
		mapError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminUpdateExamPaper(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.ExamPaperUpdate
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	view, err := h.Exam.UpdatePaper(req.Context(), id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "exam.paper.update", "competition", strconv.FormatUint(uint64(id), 10), nil)
	writeJSON(w, http.StatusOK, view)
}

func (h *Handlers) adminCreateExamQuestion(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.ExamQuestionInput
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	question, err := h.Exam.CreateQuestion(req.Context(), id, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "exam.question.create", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"question_id": question.ID, "seq": question.Seq})
	writeJSON(w, http.StatusCreated, question)
}

func (h *Handlers) adminUpdateExamQuestion(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	qid, err := examQuestionID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	var body service.ExamQuestionInput
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, errValidation)
		return
	}
	question, err := h.Exam.UpdateQuestion(req.Context(), id, qid, body)
	if err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "exam.question.update", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"question_id": qid, "seq": question.Seq})
	writeJSON(w, http.StatusOK, question)
}

func (h *Handlers) adminDeleteExamQuestion(w http.ResponseWriter, req *http.Request) {
	id, err := compID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	qid, err := examQuestionID(req)
	if err != nil {
		writeError(w, errValidation)
		return
	}
	if err := h.Exam.DeleteQuestion(req.Context(), id, qid); err != nil {
		mapError(w, err)
		return
	}
	h.audit(req, "exam.question.delete", "competition", strconv.FormatUint(uint64(id), 10),
		map[string]any{"question_id": qid})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
