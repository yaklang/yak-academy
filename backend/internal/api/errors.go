// Package api HTTP 层。只做：参数解析 → 调 service → 统一响应。
// 禁止业务逻辑、禁止直接触碰 repo/model 数据访问（project-conventions skill）。
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/service"
)

// 统一错误码（api-contract skill：信封 {"error":{"code","message"}}）。
// message 为面向用户的中文提示（统一报错接入口 error_messages.go 映射）；
// 英文原文保留在服务端 error 日志（slog api_error）。
type apiError struct {
	Code    string
	Status  int
	Message string
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

var (
	errUnauthorized = &apiError{"UNAUTHORIZED", http.StatusUnauthorized, "invalid or missing token"}
	errBadCred      = &apiError{"UNAUTHORIZED", http.StatusUnauthorized, "invalid credentials"}
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

// writeError 统一报错接入口（api-contract skill）：所有错误响应的唯一出口——
// ① 经 error_messages.go 把英文原文映射为中文（信封 message）；② 写一条
// 内置 error 日志（slog api_error，英文原文 + 中文映射同记；
// ACADEMY_LOG_JSON=1 时随 EnableJSONLogs 切 JSON 行）。新增错误输出必须走
// 这里并在中文目录登记映射，禁止 handler 自行 Encode 错误响应。
func writeError(w http.ResponseWriter, e *apiError) {
	zh := zhMessage(e.Code, errorDetail(e))
	slog.Default().Error("api_error",
		"code", e.Code, "status", e.Status,
		"message", e.Message, "message_zh", zh)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Status)
	var env errorEnvelope
	env.Error.Code = e.Code
	env.Error.Message = zh
	_ = json.NewEncoder(w).Encode(env)
}

// errorDetail 从英文 message 剥出映射用 detail：VALIDATION_FAILED 的
// "validation failed: xxx" 取 xxx，其余码整体作为 detail。
func errorDetail(e *apiError) string {
	if e.Code == errValidation.Code {
		d := strings.TrimPrefix(e.Message, errValidation.Message+": ")
		if d == errValidation.Message { // 裸 "validation failed"：无 detail
			return ""
		}
		return d
	}
	if e.Message == "" || e.Message == codeDefaultMessage(e.Code) {
		return ""
	}
	return e.Message
}

// codeDefaultMessage 错误码的静态英文 message（apiError 定义处的原串，
// 用于识别"没有 detail 的码级错误"）。
func codeDefaultMessage(code string) string {
	switch code {
	case errUnauthorized.Code:
		return errUnauthorized.Message
	case errForbidden.Code:
		return errForbidden.Message
	case errNotFound.Code:
		return errNotFound.Message
	case errValidation.Code:
		return errValidation.Message
	case errInternal.Code:
		return errInternal.Message
	}
	return "\x00none"
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
	case errors.Is(err, service.ErrInvalidCredential):
		writeError(w, errBadCred)
	case errors.Is(err, service.ErrValidation):
		msg := err.Error()
		if msg == "" || msg == service.ErrValidation.Error() {
			writeError(w, errValidation)
			return
		}
		writeError(w, &apiError{"VALIDATION_FAILED", http.StatusUnprocessableEntity, msg})
	case isInstanceConflict(err):
		writeConflict(w, err)
	case errors.Is(err, service.ErrHasSolves):
		writeError(w, &apiError{"CONFLICT", http.StatusConflict,
			"challenge has solve history; submissions are append-only, delete refused"})
	case service.IsNotImplemented(err):
		writeError(w, &apiError{"NOT_IMPLEMENTED", http.StatusNotImplemented, err.Error()})
	default:
		slog.Default().Error("api_internal_error", "err", err.Error(), "code", errInternal.Code)
		writeError(w, errInternal)
	}
}

// isInstanceConflict 判定单容器冲突错误（携带当前活跃实例）。
func isInstanceConflict(err error) bool {
	var conflict *service.InstanceConflictError
	return errors.As(err, &conflict)
}

// writeConflict 409 响应：信封外附当前活跃实例（含题目名/slug 富化，
// 前端据此提示"先关闭上一个容器"并给出跳转）。同样走统一接入口的
// 中文映射与 error 日志。
func writeConflict(w http.ResponseWriter, err error) {
	var conflict *service.InstanceConflictError
	env := conflictEnvelope{}
	env.Error.Code = "INSTANCE_CONFLICT"
	env.Error.Message = zhMessage("INSTANCE_CONFLICT", "")
	slog.Default().Error("api_error",
		"code", env.Error.Code, "status", http.StatusConflict,
		"message", "user already has an active instance, close it first",
		"message_zh", env.Error.Message)
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
