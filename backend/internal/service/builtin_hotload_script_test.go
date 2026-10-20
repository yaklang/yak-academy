package service

import (
	"regexp"
	"strings"
	"testing"
)

// 回归：yakit-mall-chain 教程内嵌的 MITM 热加载脚本必须使用 yak 引擎真实存在
// 的钩子与 API（2026-10-19 修复，随 refactor/module-case-integration）。
// 修复前行为：脚本钩子名写成 hijackSave——引擎只认 beforeRequest /
//   hijackHTTPRequest / hijackSaveHTTPFlow 等，未知的直接静默忽略（热加载永远
//   不生效）；脚本里还调用了引擎不存在的 isPost()/fisContains()，replace() 在
//   引擎里直接 panic。用户照教程抄进 Yakit 后报告「按教程走热加载没有生效」。
// 修复后行为：教程两处代码块均为 beforeRequest = func(req)（req 即原始请求报
//   文，返回值即真正发出的请求），用 poc.GetHTTPPacketBody 取 body、
//   str.ReplaceAll 改价、poc.ReplaceHTTPPacketBody 写回（自动重算
//   Content-Length），替换串 "TotalPrice":"89" → "0.01" 与网关改价信号一致。
//   脚本已在 yak 引擎 1.4.9 的 hotpatch-mitm 离线管线验证：命中改写、无关流
//   量原样透传（CI 无 yak 引擎，行为验证留在本地，这里锚定内容本身）。
func TestBuiltinMallChainHotloadScriptUsesRealHooks(t *testing.T) {
	raw, err := builtinFS.ReadFile("builtin/vulinbox/tutorials/topics/yakit-module-mastery/rooms/yakit-mall-chain/lesson.md")
	if err != nil {
		t.Fatalf("read lesson.md from embed fs: %v", err)
	}
	lesson := string(raw)

	fence := regexp.MustCompile("(?s)```[a-zA-Z]*\n(.*?)\n```")
	blocks := fence.FindAllStringSubmatch(lesson, -1)
	if len(blocks) == 0 {
		t.Fatal("lesson.md contains no fenced code block")
	}

	hotload := 0
	for _, m := range blocks {
		block := m[1]
		if !strings.Contains(block, "beforeRequest") {
			continue
		}
		hotload++
		for _, want := range []string{
			// 引擎真实钩子：单参数 req 即原始报文，返回值即发出的请求
			"beforeRequest = func(req)",
			// 只放行改写结算订单；body 取/改/写回都走 poc 包
			"poc.GetHTTPPacketBody(req)",
			"str.ReplaceAll(body",
			`"TotalPrice":"89"`,
			`"TotalPrice":"0.01"`,
			"poc.ReplaceHTTPPacketBody(req, body)",
			"return req",
		} {
			if !strings.Contains(block, want) {
				t.Errorf("hot-load block #%d missing %q:\n%s", hotload, want, block)
			}
		}
	}
	if hotload != 2 {
		t.Fatalf("expected 2 hot-load script blocks (intro + step 3), got %d", hotload)
	}

	// 引擎不支持 / 会 panic 的写法禁止再次出现（正文与代码块都不得含）
	for _, bad := range []string{
		"hijackSave", // 不是引擎钩子名（真名 hijackSaveHTTPFlow，且只改历史存储不影响转发）
		"isPost(",
		"fisContains(",
		"replace(body", // replace() 在 yak 引擎里直接 panic
	} {
		if strings.Contains(lesson, bad) {
			t.Errorf("lesson.md still contains unsupported-by-engine pattern %q", bad)
		}
	}
}
