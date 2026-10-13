// Package api HTTP 层。只做：参数解析 → 调 service → 统一响应。
// 禁止业务逻辑、禁止直接触碰 repo/model 数据访问（project-conventions skill）。
package api

import (
	"encoding/json"
	"errors"
	"log"
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

// conflictEnvelope 单容器冲突响应：错误信封外附 current（当前活跃实例），
// 供前端提示"先关闭上一个容器"（api-contract skill：409 契约）。
type conflictEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Current *service.AdminLabInstance `json:"current"`
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
// stub 未实现 → 501；单容器冲突 → 409 + current（api-contract skill）。
// 兜底 500 前在服务端打印真实原因——"internal error" 对用户无信息量，
// 也让偶发故障可从日志定位（docker run 失败输出等）。
func mapError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, service.ErrNotFound):
		writeError(w, errNotFound)
	case errors.Is(err, service.ErrValidation):
		writeError(w, errValidation)
	case isInstanceConflict(err):
		writeConflict(w, err)
	case service.IsNotImplemented(err):
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
		log.Printf("api: internal error: %v", err)
		writeError(w, errInternal)
	}
}

// isInstanceConflict 判定单容器冲突错误（携带当前活跃实例）。
func isInstanceConflict(err error) bool {
	var conflict *service.InstanceConflictError
	return errors.As(err, &conflict)
}

// writeConflict 409 响应：信封外附当前活跃实例（含题目名/slug 富化，
// 前端据此提示"先关闭上一个容器"并给出跳转）。
func writeConflict(w http.ResponseWriter, err error) {
	var conflict *service.InstanceConflictError
	env := conflictEnvelope{}
	env.Error.Code = "INSTANCE_CONFLICT"
	env.Error.Message = "user already has an active instance, close it first"
	if errors.As(err, &conflict) && conflict.Current != nil {
		current := &service.AdminLabInstance{
			Username:       conflict.Username,
			ChallengeTitle: conflict.ChallengeTitle,
			ChallengeSlug:  conflict.ChallengeSlug,
		}
		current.LabInstance = *conflict.Current
		env.Current = current
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(env)
}
