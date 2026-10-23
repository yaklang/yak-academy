package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// zhMessage 表驱动：码级默认 → 精确匹配 → 包含规则（含特异性排序）→ 兜底。
// "phone already registered" 两段文案断言与 e2e bugfix-register-phone-taken
// spec 锚定的 UI 文案同步，改这里必须同步那边。
func TestZhMessage(t *testing.T) {
	cases := []struct {
		name, code, detail, want string
	}{
		{"码级默认（无 detail）", "NOT_FOUND", "", "资源不存在或已被删除"},
		{"未知 detail 回落码级默认", "VALIDATION_FAILED", "something brand new", "请求参数有误，请检查后重试"},
		{"未知码兜底", "WEIRD_CODE", "", "操作失败，请稍后重试"},
		{"精确：已注册手机号", "VALIDATION_FAILED", "phone already registered",
			"该手机号已注册过账号：请返回登录；忘记用户名或密码请联系管理员"},
		{"精确：登录凭据", "UNAUTHORIZED", "invalid credentials", "用户名或密码不正确"},
		{"精确：CONFLICT 通关记录（不得落进泛化 challenge 规则）", "CONFLICT",
			"challenge has solve history; submissions are append-only, delete refused",
			"该题目已有通关记录，不能删除（提交记录只增不删）"},
		{"包含：where 前缀 + 动态尾参", "VALIDATION_FAILED",
			`rooms[0]: duplicate slug "x"`, "slug 重复（同一内容包内 slug 必须唯一）"},
		{"包含：动态值尾部", "VALIDATION_FAILED",
			"source exceeds 1MB limit", "源码超出大小限制"},
		{"特异性：is builtin 先于泛化 challenge", "VALIDATION_FAILED",
			`challenge "web-lab" is builtin`, "内置题目由内置导入统一管理，不能单独修改或删除"},
		{"动态中段：黑名单库名保留", "VALIDATION_FAILED",
			"source uses blacklisted yak lib(s): exec, file (平台高危库黑名单，任何题目不可用)",
			"源码引用了平台黑名单的高危库（任何题目不可用）：exec, file"},
		{"动态中段：白名单外库名保留", "VALIDATION_FAILED",
			"source uses yak lib(s) outside the challenge whitelist: codec",
			"源码引用了题目白名单之外的库：codec"},
		{"精确：竞赛 kind 非法", "VALIDATION_FAILED", "invalid competition kind",
			"竞赛类型无效（公开赛 public / 邀请赛 invitational 二选一，创建后不可更改）"},
		{"精确：邀请赛禁自助报名", "VALIDATION_FAILED", "cannot register for an invitational competition",
			"邀请赛不开放自助报名，参赛账号由管理员导入或生成"},
		{"精确：公开赛未报名先入场", "VALIDATION_FAILED", "register before entering a public competition",
			"请先在竞赛广场报名，再进入公开赛"},
		{"精确：公开赛禁导入", "VALIDATION_FAILED",
			"participants can only be imported into an invitational competition",
			"只有邀请赛支持导入参赛者；公开赛由用户在广场自行报名"},
		{"精确：生成前缀非法", "VALIDATION_FAILED",
			"prefix must be 1-16 lowercase letters, digits or hyphens",
			"生成前缀需为 1-16 位小写字母、数字或连字符（如 class26）"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := zhMessage(tc.code, tc.detail); got != tc.want {
				t.Fatalf("zhMessage(%q, %q) = %q, want %q", tc.code, tc.detail, got, tc.want)
			}
		})
	}
}

// errorDetail 剥取：VALIDATION_FAILED 去 "validation failed: " 前缀；
// 码级静态 message（与定义原串一致）视为无 detail。
func TestErrorDetail(t *testing.T) {
	cases := []struct {
		e    *apiError
		want string
	}{
		{&apiError{"VALIDATION_FAILED", 422, "validation failed: phone already registered"}, "phone already registered"},
		{&apiError{"VALIDATION_FAILED", 422, "validation failed"}, ""},
		{&apiError{"NOT_FOUND", 404, "resource not found"}, ""},
		{&apiError{"UNAUTHORIZED", 401, "invalid credentials"}, "invalid credentials"},
	}
	for _, tc := range cases {
		if got := errorDetail(tc.e); got != tc.want {
			t.Fatalf("errorDetail(%+v) = %q, want %q", tc.e, got, tc.want)
		}
	}
}

// writeError 集成：信封 message 为中文、code 保留，且写一条内置 error 日志
// （msg=api_error，英文原文与中文映射同记）——统一报错接入口的两项硬约束。
func TestWriteErrorLogsAndTranslates(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	rec := httptest.NewRecorder()
	writeError(rec, &apiError{"VALIDATION_FAILED", http.StatusUnprocessableEntity,
		"validation failed: phone already registered"})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	var env struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("code = %q", env.Error.Code)
	}
	if !strings.Contains(env.Error.Message, "该手机号已注册过账号") ||
		!strings.Contains(env.Error.Message, "请返回登录") {
		t.Fatalf("zh message = %q, want phone-taken guidance", env.Error.Message)
	}

	logLine := buf.String()
	for _, want := range []string{
		"api_error", "phone already registered", "该手机号已注册过账号", "VALIDATION_FAILED",
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("error log missing %q: %s", want, logLine)
		}
	}
}
