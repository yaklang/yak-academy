package service

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// yak 库策略（防高危库执行）：题目级白名单（judge.allowed_libs）+ 平台级黑名单。
// 学生源码在提交/自测「受理前」做静态扫描：命中黑名单库、或题目配了白名单而
// 源码引用白名单之外的库 → 422 拒绝（不进入引擎执行）。属纵深防御一层：
// 引擎真隔离依赖部署侧沙箱，这里保证的是平台侧策略一致（编辑器补全、题面
// 展示、受理拦截同源）。

// yakLibUniverse yaklang 引擎的全部命名空间库名（`lib.Func` 调用形态）。
// 提取自 yaklang common/ 的 yaklang.Import("name", …) 注册表（剔除仅存在于
// yakgrpc 服务上下文的注入）。白名单值合法性与白名单模式扫描的判定宇宙。
var yakLibUniverse = map[string]bool{
	"ai": true, "aiagent": true, "aihistory": true, "aiknowledge": true, "aim": true,
	"aimemory": true, "aireducer": true, "amap": true, "bin": true, "bot": true,
	"browser": true, "brute": true, "bufio": true, "cli": true, "codec": true,
	"container": true, "context": true, "crawler": true, "crawlerx": true, "csrf": true,
	"cve": true, "cwe": true, "db": true, "dictutil": true, "diff": true, "dns": true,
	"dnslog": true, "dyn": true, "elf": true, "env": true, "excel": true, "exec": true,
	"facades": true, "ffmpeg": true, "file": true, "filemonitor": true, "fileparser": true,
	"filescanner": true, "filesys": true, "finscan": true, "fp": true, "fuzz": true,
	"fuzzx": true, "git": true, "grok": true, "gzip": true, "hids": true, "hook": true,
	"http": true, "httpool": true, "httpserver": true, "httptpl": true, "iiop": true,
	"imageutils": true, "io": true, "ja3": true, "java": true, "js": true, "json": true,
	"jsonschema": true, "jsonstream": true, "judge": true, "jwt": true, "ldap": true,
	"liteforge": true, "log": true, "math": true, "mcp": true, "memeditor": true,
	"mfa": true, "mimetype": true, "mitm": true, "mmdb": true, "nasl": true, "net": true,
	"netstack": true, "netutils": true, "nuclei": true, "omnisearch": true, "openapi": true,
	"orderedmap": true, "os": true, "pandoc": true, "pcapdb": true, "pcapx": true,
	"ping": true, "poc": true, "pprof": true, "rag": true, "rdp": true, "re": true,
	"re2": true, "redis": true, "regen": true, "rehs": true, "report": true, "risk": true,
	"rpa": true, "sandbox": true, "sca": true, "servicescan": true, "sfreport": true,
	"simulator": true, "smb": true, "spacengine": true, "ssa": true, "ssh": true,
	"str": true, "subdomain": true, "suricata": true, "sync": true, "synscan": true,
	"syntaxflow": true, "systemd": true, "t3": true, "tcp": true, "tcpmitm": true,
	"time": true, "timezone": true, "tls": true, "toolbox": true, "tools": true,
	"traceroute": true, "twofa": true, "udp": true, "webforest": true, "whisper": true,
	"x": true, "xhtml": true, "xml": true, "xpath": true, "yakit": true, "yaml": true,
	"yso": true, "zip": true,
}

// defaultBlockedYakLibs 平台默认黑名单：本机命令执行 / 宿主文件访问类库。
// 判题引擎在平台主机上运行学生源码，这些库等价于直接拿到宿主 shell / 文件系统，
// 任何训练题都不应放开（os 不在内——ACM stdin 模型依赖 os.Stdin）。
var defaultBlockedYakLibs = []string{
	"exec", "cli", "ssh", // 命令执行 / 远程执行
	"file", "filesys", "filemonitor", "fileparser", "filescanner", // 宿主文件访问
}

// envYakLibBlacklist 部署侧黑名单覆盖（逗号分隔库名；空串 = 用默认）。
const envYakLibBlacklist = "ACADEMY_YAK_LIB_BLACKLIST"

// blockedYakLibs 当前生效黑名单（每次调用求值，测试可用 t.Setenv 注入）。
func blockedYakLibs() map[string]bool {
	raw := strings.TrimSpace(os.Getenv(envYakLibBlacklist))
	list := defaultBlockedYakLibs
	if raw != "" {
		list = strings.Split(raw, ",")
	}
	blocked := make(map[string]bool, len(list))
	for _, name := range list {
		if name = strings.TrimSpace(name); name != "" {
			blocked[name] = true
		}
	}
	return blocked
}

// yakIdentPattern 标识符 token（剥除注释/字符串后扫描）。
var yakIdentPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// stripYakLiterals 剥除注释与字符串/字符/反引号字面量（含 f-string 整体剥除——
// 插值段里的库引用随之漏检，可接受的保守取舍：误杀为零、漏检面小）。
// segStart 跟踪当前 plain 段起点：命中字面量边界时先吐出 [segStart:i)。
func stripYakLiterals(src string) string {
	var b strings.Builder
	i, n, segStart := 0, len(src), 0
	for i < n {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/': // 行注释（保留换行）
			b.WriteString(src[segStart:i])
			for i < n && src[i] != '\n' {
				i++
			}
			segStart = i
		case c == '/' && i+1 < n && src[i+1] == '*': // 块注释
			b.WriteString(src[segStart:i])
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2
			} else {
				i = n
			}
			segStart = i
			b.WriteByte(' ')
		case c == '"' || c == '\'' || c == '`': // 字符串（f/r/x 前缀变体同样引号定界）
			b.WriteString(src[segStart:i])
			quote := c
			i++
			for i < n && src[i] != quote {
				if src[i] == '\\' && quote != '`' && i+1 < n {
					i++ // 转义（raw 反引号无转义）
				}
				i++
			}
			i++ // 收尾引号
			segStart = i
			b.WriteByte(' ')
		default:
			i++
		}
	}
	b.WriteString(src[segStart:n])
	return b.String()
}

// usedYakLibs 源码引用到的库标识符（universe 交集，按名排序保证报错稳定）。
// 标识符级命中：`exec.Command` 与 `a := exec` 别名引用同样检出（静态防绕过）；
// 代价是变量名恰与库同名的遮蔽写法（如 `time := 1`）也会命中——保守方向，
// 报错信息点名标识符，学生改名即可。
func usedYakLibs(src string) []string {
	seen := map[string]bool{}
	for _, ident := range yakIdentPattern.FindAllString(stripYakLiterals(src), -1) {
		if yakLibUniverse[ident] {
			seen[ident] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// checkYakLibPolicy 受理前库策略校验：黑名单恒生效；allowed 非空时同时限白名单。
// 返回 nil = 放行。错误一律 ErrValidation（422），信息点名违规库便于修正。
func checkYakLibPolicy(src string, allowed []string) error {
	blocked := blockedYakLibs()
	allowSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowSet[a] = true
	}
	var hitBlocked, hitOutside []string
	for _, lib := range usedYakLibs(src) {
		if blocked[lib] {
			hitBlocked = append(hitBlocked, lib)
			continue
		}
		if len(allowSet) > 0 && !allowSet[lib] {
			hitOutside = append(hitOutside, lib)
		}
	}
	if len(hitBlocked) > 0 {
		return fmt.Errorf("%w: source uses blacklisted yak lib(s): %s (平台高危库黑名单，任何题目不可用)",
			ErrValidation, strings.Join(hitBlocked, ", "))
	}
	if len(hitOutside) > 0 {
		return fmt.Errorf("%w: source uses yak lib(s) outside the challenge whitelist: %s",
			ErrValidation, strings.Join(hitOutside, ", "))
	}
	return nil
}

// serializeAllowedLibs 白名单列表 → JSON 列文本（空列表存空串 = 未配置）。
func serializeAllowedLibs(libs []string) string {
	if len(libs) == 0 {
		return ""
	}
	b, err := json.Marshal(libs)
	if err != nil {
		return ""
	}
	return string(b)
}

// parseAllowedLibs JSON 列文本 → 白名单列表（空/坏值 = 未配置，nil）。
func parseAllowedLibs(col string) []string {
	if strings.TrimSpace(col) == "" {
		return nil
	}
	var libs []string
	if err := json.Unmarshal([]byte(col), &libs); err != nil {
		return nil
	}
	return libs
}

// validateAllowedLibs 上传路径白名单值校验：库名必须在 universe、不得重复、
// 不得配置黑名单库（黑名单优先于白名单，配置即 422 防误配）。
func validateAllowedLibs(where string, libs []string) error {
	blocked := blockedYakLibs()
	seen := map[string]bool{}
	for _, lib := range libs {
		if !yakLibUniverse[lib] {
			return fmt.Errorf("%w: %s: judge.allowed_libs %q is not a yak lib", ErrValidation, where, lib)
		}
		if blocked[lib] {
			return fmt.Errorf("%w: %s: judge.allowed_libs %q is blacklisted and can never be enabled", ErrValidation, where, lib)
		}
		if seen[lib] {
			return fmt.Errorf("%w: %s: duplicate judge.allowed_libs entry %q", ErrValidation, where, lib)
		}
		seen[lib] = true
	}
	return nil
}

// effectiveAllowedLibs 学生端回显的白名单：配置值扣除当前黑名单（防御：
// 黑名单可在题目配置之后经 env 扩充），未配置返回 nil。
func effectiveAllowedLibs(col string) []string {
	libs := parseAllowedLibs(col)
	if libs == nil {
		return nil
	}
	blocked := blockedYakLibs()
	out := make([]string, 0, len(libs))
	for _, lib := range libs {
		if !blocked[lib] {
			out = append(out, lib)
		}
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

// sortedBlockedList 学生端回显的黑名单（按名排序保证展示稳定）。
func sortedBlockedList() []string {
	blocked := blockedYakLibs()
	out := make([]string, 0, len(blocked))
	for name := range blocked {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
