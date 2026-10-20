package service

import (
	"strings"
	"testing"
)

// 回归：mall-chain 教程缺少「/flag 403 排查」信息（2026-10-20 补充，随
// refactor/module-case-integration）。
// 修复前行为：/flag 的 403 文案不指明缺哪个信号，教程也没有任何自查手段——
//   学习者四步做了三步（实操反馈：漏 replay，codec 已 200）时对着 403 无从
//   排查，只能盲猜重做。
// 修复后行为：教程第 6 步附「卡在 403？逐信号自查」——四信号判定特征表、
//   codec 端点两态响应文案的含义（唯一进度反馈）、三个常见卡点（多件商品
//   替换串不命中静默不改 / X-Mall-Internal 头加错请求 / 实例重启信号清零
//   需重走），并说明最后一步落账瞬间解锁、四步与顺序无关。
func TestBuiltinMallChainLessonHasFlag403SelfCheck(t *testing.T) {
	raw, err := builtinFS.ReadFile("builtin/vulinbox/tutorials/topics/yakit-module-mastery/rooms/yakit-mall-chain/lesson.md")
	if err != nil {
		t.Fatalf("read lesson.md from embed fs: %v", err)
	}
	lesson := string(raw)

	for _, want := range []string{
		"逐信号自查",        // 排查小节标题
		"业务链信号齐备后",     // codec 两态文案之「还有信号未齐」
		"四步信号齐备，",      // codec 两态文案之「全部齐备」（带逗号，区别于既有的「四步信号齐备后」）
		"静默不改",          // 卡点：购物车多件商品替换串不命中
		"全部清零",          // 卡点：实例重启信号清零
		"最后一步落账的瞬间",    // 解锁时机：任一顺序补齐即解锁
	} {
		if !strings.Contains(lesson, want) {
			t.Errorf("lesson.md missing self-check marker %q", want)
		}
	}
}
