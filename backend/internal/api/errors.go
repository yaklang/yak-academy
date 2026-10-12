// Package api HTTP 层。只做：参数解析 → 调 service → 统一响应。
// 禁止业务逻辑、禁止直接触碰 repo/model 数据访问（project-conventions skill）。
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/service"
)

// 统一错误码（api-contract skill：信封 {"error":{"code","message"}}）。
type apiError struct {
	Code    string
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

var (
	errUnauthorized = &apiError{"UNAUTHORIZED", http.StatusUnauthorized, "invalid or missing token"}
	errForbidden    = &apiError{"FORBIDDEN", http.StatusForbidden, "insufficient role"}
	errNotFound     = &apiError{"NOT_FOUND", http.StatusNotFound, "resource not found"}
	errValidation   = &apiError{"VALIDATION_FAILED", http.StatusUnprocessableEntity, "validation failed"}
	errInternal     = &apiError{"INTERNAL", http.StatusInternalServerError, "internal error"}
)

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, e *apiError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	var env errorEnvelope
	env.Error.Code = e.Code
	env.Error.Message = e.Message
	_ = json.NewEncoder(w).Encode(env)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// mapError 将内部错误映射为统一信封。gorm.ErrRecordNotFound → 404；
// stub 未实现 → 501（api-contract skill：stub 允许 501，但路由必须注册）。
func mapError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, service.ErrNotFound):
		writeError(w, errNotFound)
	case errors.Is(err, service.ErrValidation):
		writeError(w, errValidation)
	case service.IsNotImplemented(err):
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}{struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{"NOT_IMPLEMENTED", err.Error()}})
	default:
		writeError(w, errInternal)
	}
}