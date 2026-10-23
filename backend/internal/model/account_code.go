package model

import (
	"fmt"
	"strconv"
	"strings"
)

// 用户账号编码（学号式固定编码）：YA{YY}-{NNNNN}，如 YA26-00042。
// 年份取建号年（两位）+ 当年零填充五位序号；与登录用户名解耦，是管理端
// 用户管理与邀请赛表格导入使用的稳定外部标识。设计依据学号/工号通行惯例
// （年份前缀 + 零填充序号），详见 docs/architecture.md。
// 序号上限 99999：零填充是「字典序 = 数值序」的前提（运行时 MAX+1 分配依赖
// 字典序取最大值），超限报错而非静默变长。
// 分配唯一挂点在 userRepo.Create（同年序号 MAX+1，唯一冲突重试）；存量
// 回填在 db.Migrate（按 created_at 年份分组、id 升序）。

// AccountCodePrefix 某年的编码前缀（2026 → "YA26-"）。
func AccountCodePrefix(year int) string {
	return fmt.Sprintf("YA%02d-", year%100)
}

// FormatAccountCode 年份 + 序号 → 完整编码；序号越界返回空串（调用方视为错误）。
func FormatAccountCode(year, seq int) string {
	if seq < 1 || seq > 99999 {
		return ""
	}
	return fmt.Sprintf("%s%05d", AccountCodePrefix(year), seq)
}

// AccountCodeSeq 从编码剥出序号（"YA26-00042" → 42）；格式不符返回 false。
func AccountCodeSeq(code string) (int, bool) {
	i := strings.LastIndex(code, "-")
	if i < 0 {
		return 0, false
	}
	seq, err := strconv.Atoi(code[i+1:])
	if err != nil || seq < 1 {
		return 0, false
	}
	return seq, true
}
