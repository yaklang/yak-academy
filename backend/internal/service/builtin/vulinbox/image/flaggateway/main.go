// flaggateway 是 yak-academy 内置训练镜像的入口层（容器内监听 :8787 前置）。
// 它解决五件事：
//  1. 平台按实例注入的 FLAG 环境变量（flag_mode=instance）上游 vulinbox 不消费——
//     网关把它写成容器内 /flag.txt，并提供 GET /flag 读取端点。端点带按题门禁：
//     平台注入 CHALLENGE_SLUG 标识实例归属，SSRF 两题/未知 slug 只放行靶机内部
//     自请求（RemoteAddr 回环，实例端口映射后浏览器直连的源地址是 Docker 网桥，
//     伪造 Host 头无效）；8 道攻击题只在网关观察到本题攻击成功信号（unlockRules）
//     后放行（回环也不豁免，封死跨题借 SSRF 读 flag 的捷径）；authz-cmd 的正路
//     是命令模拟接口 cat flag.txt，/flag 保持内部自请求并指路；
//  2. 上游命令模拟接口只实现 ls（cat 报 command not found）——网关对
//     /vul/auth-bypass/unsafe/api/cmd 注入 cat 支持，但仅当 CHALLENGE_SLUG 是
//     authz-cmd（或手工起容器未注入 slug）：cat flag.txt / cat /flag.txt / cat flag
//     返回真 flag，其余命令原样透传上游白名单行为。其他题不注册 cat，靶机上游
//     对 cat 一律 command not found；
//  3. 上游 /ssrf/flag 演示接口每次请求随机生成 flag{uuid}，与实例真 flag
//     （flag{lab-...}）形态相同，学员极易误当本题答案提交——网关在反代响应里
//     把它改写为固定的演示假 flag（flag{test}，一眼假），靶机行为其余不变；
//  4. 主反代挂 unlockTransport：镜像经手流量（请求/响应摘要）喂给本题解锁规则，
//     命中即一次性放行 /flag（规则信号 = 配套教程宣告的攻击成功特征）；
//  5. 其余一切路径与方法原样反代上游 vulinbox（127.0.0.1:8788）。
//
// flag 只对本容器实例有效（FLAG 环境变量逐实例生成），销毁即失效。
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	listenAddr = "0.0.0.0:8787"          // 容器对外入口（平台端口映射指向这里）
	upstream   = "http://127.0.0.1:8788" // 上游 vulinbox 实际监听地址
	flagFile   = "/flag.txt"

	// demoFlag 是 /ssrf/flag 演示接口的固定假 flag：替换上游的随机 uuid，
	// 让"这是演示值、不是本题答案"一眼可辨（实例真 flag 只在 GET /flag）。
	demoFlag = "flag{test}"

	// authzCmdSlug 命令模拟题：正路是 cmd 接口 cat flag.txt，是唯一注册
	// cat 注入的挑战 slug。
	authzCmdSlug = "vulinbox-authz-cmd"

	// mallChainSlug 业务链综合题（模块专精专题唯一题）：商城 899 元商品
	// 1 分钱下单——MITM 观察商城流量 → 热加载脚本改写订单价 → Fuzzer 带
	// 内部标记头重放下单 → Codec 编码链通过网关验证端点。/flag 在三步
	// 信号全部观察到（unlockRules）且编码链端点验证通过（codecVerify
	// 置位）后才解锁。
	mallChainSlug = "vulinbox-mall-chain"

	// codecVerifySlug Codec 编码链验证端点注册的挑战 slug（模块专精综合题
	// 与旧独立 codec 题）：token = URL 编码过的 base64 口令，验证通过
	// 置 gate 解锁。综合题还需同时命中 unlockRules 三步信号。
	codecVerifySlug  = "vulinbox-codec-pipeline"
	codecVerify2Slug = mallChainSlug
	// codecProof 口令原文：base64 后含 '+' 与 '='（Y29kZWN+cHJvb2Y=），
	// '+' 不做 URL 编码会在 query 解析时变成空格——URL 编码是硬需求。
	codecProof = "codec~proof"
)

// mallChainSignals 商城业务链综合题的四步信号键（unlockRules 与编码链
// 验证端点 handler 共用，保证 seenAll 裁决口径一致）。
var mallChainSignals = []string{"observe", "tamper", "replay", "codec"}

// upstreamFlagRe 匹配上游 /ssrf/flag 生成的 flag{<uuid>} 演示值。
var upstreamFlagRe = regexp.MustCompile(`flag\{[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\}`)

func main() {
	flag := strings.TrimSpace(os.Getenv("FLAG"))
	if flag == "" {
		// 未注入 FLAG（共享模式实例/手工起容器）：写入占位 flag，读取路径仍可演示。
		flag = "flag{no-instance-flag}"
	}
	if err := os.WriteFile(flagFile, []byte(flag+"\n"), 0o644); err != nil {
		log.Fatalf("write %s: %v", flagFile, err)
	}
	slug := strings.TrimSpace(os.Getenv("CHALLENGE_SLUG"))

	up, err := url.Parse(upstream)
	if err != nil {
		log.Fatalf("parse upstream: %v", err)
	}

	log.Printf("flaggateway listening on %s (upstream %s, challenge %q)", listenAddr, upstream, slug)
	log.Fatal(http.ListenAndServe(listenAddr, newGatewayMux(flag, slug, up)))
}

// newGatewayMux 装配网关路由：/flag 门禁与命令模拟接口是网关自有逻辑，
// /ssrf/flag 反代时改写演示 flag，其余路径原样反代上游。
func newGatewayMux(flag, slug string, up *url.URL) *http.ServeMux {
	proxy := httputil.NewSingleHostReverseProxy(up)
	gate := newFlagGate()
	if rule, ok := unlockRules[slug]; ok {
		proxy.Transport = &unlockTransport{base: http.DefaultTransport, gate: gate, rule: rule}
	}

	mux := http.NewServeMux()

	// GET /flag：按题门禁（见文件头注释第 1 条），三类拒绝文案各自指路。
	mux.HandleFunc("/flag", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case unlockRules[slug] != nil || slug == codecVerifySlug:
			// 攻击题（含 Codec 编码链题与商城业务链综合题）：观察到本题
			// 攻击成功信号前，对外一律 403（回环也不豁免）。
			if !gate.unlocked.Load() {
				writeFlagDenied(w, denyLocked)
				return
			}
		case slug == authzCmdSlug:
			if !isLoopbackRequest(r) {
				writeFlagDenied(w, denyAuthzCmd)
				return
			}
		default:
			// SSRF 两题/未知 slug：只放行靶机内部自请求（SSRF 打通后由上游替浏览器发起）。
			if !isLoopbackRequest(r) {
				writeFlagDenied(w, denyLoopback)
				return
			}
		}
		fmt.Fprintf(w, "%s\n", flag)
	})

	// /ssrf/flag：上游演示接口，仅改写响应里的 flag{uuid} 为固定假 flag。
	// 上游的 Host 自我校验（不含 127.0.0.1 回 500）与页面其余内容原样保留。
	ssrfFlagProxy := httputil.NewSingleHostReverseProxy(up)
	ssrfFlagProxy.ModifyResponse = rewriteDemoFlag
	mux.Handle("/ssrf/flag", ssrfFlagProxy)

	// 命令模拟接口：注入 cat（上游白名单只有 ls），仅 authz-cmd
	// 与手工容器注册。
	if slug == "" || slug == authzCmdSlug {
		mux.HandleFunc("/vul/auth-bypass/unsafe/api/cmd", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				proxy.ServeHTTP(w, r)
				return
			}
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
				return
			}
			// 与上游同构：{"command": "..."}；cat 只认 flag 文件，其余命令交回上游。
			cmd := extractCommand(body)
			if cmd != "" && (cmd == "cat flag.txt" || cmd == "cat /flag.txt" || cmd == "cat flag") {
				writeJSON(w, map[string]string{"command": cmd, "result": flag + "\n"})
				return
			}
			// 其余命令（含 ls 与任意不支持命令）原样转发上游，保留白名单/报错行为。
			passCmdThrough(w, r, body, proxy)
		})
	}

	// Codec 编码链验证端点（模块专精专题注册）：token 必须是「口令 base64
	// 编码后再 URL 编码」的两层变换结果。+ 与 = 不做 URL 编码会在 query
	// 解析时被破坏（+ 变空格）——编码链错一环即 400，端点通过即记入 gate。
	// 综合题（mall-chain）在端点通过之外还要求 unlockRules 三步信号齐备。
	if slug == codecVerifySlug || slug == codecVerify2Slug {
		mux.HandleFunc("/academy/codec/verify", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "GET only", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			token := r.URL.Query().Get("token")
			decoded, err := base64.StdEncoding.DecodeString(token)
			if err != nil || string(decoded) != codecProof {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"ok":false,"message":"编码链不正确：token 应为口令 base64 编码后再 URL 编码的结果（+ 与 = 必须转义，否则传输途中被破坏）。用 Codec 编解码工具对照两个方向自查。"}`)
				return
			}
			if slug == codecVerify2Slug {
				// 综合题：编码链只是四步链的一步——记入信号后即时裁决（编码链
				// 常是最后一步，此后未必再有流量触发 unlockRules 的 seenAll，
				// 这里不裁决 /flag 会永远锁死）。
				gate.markSeen("codec")
				if gate.seenAll(mallChainSignals...) {
					gate.unlocked.Store(true)
					log.Printf("flaggateway: mall-chain signals complete (codec last), /flag unlocked")
					fmt.Fprint(w, `{"ok":true,"message":"编码链验证通过：四步信号齐备，/flag 已解锁，读取本实例 flag 并提交。"}`)
					return
				}
				fmt.Fprint(w, `{"ok":true,"message":"编码链验证通过：业务链信号齐备后 /flag 即解锁。"}`)
				return
			}
			gate.unlocked.Store(true)
			fmt.Fprint(w, `{"ok":true,"message":"编码链验证通过：/flag 已解锁，读取本实例 flag 并提交。"}`)
		})
	}

	// 其余路径全部反代上游。
	mux.Handle("/", proxy)

	return mux
}

// ---- /flag 门禁 ----

// isLoopbackRequest 判定请求是否由靶机内部发起（连接源地址是回环）。
// 实例端口映射 127.0.0.1::<port>：浏览器等外部流量经 Docker 网桥进容器，
// RemoteAddr 是网桥 IP；只有容器内进程（如上游 SSRF fetcher）从 127.0.0.1
// 连入。RemoteAddr 来自 TCP 连接，不像 Host 头可随意伪造。
func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// denyKind /flag 拒绝文案类别。
type denyKind int

const (
	denyLocked   denyKind = iota // 攻击题：完成攻击后解锁
	denyAuthzCmd                 // authz-cmd：正路是 cmd 接口 cat flag.txt
	denyLoopback                 // 默认/SSRF：只接受靶机内部自请求
)

// writeFlagDenied 写 403 拒绝与对应指路文案。
func writeFlagDenied(w http.ResponseWriter, kind denyKind) {
	var msg string
	switch kind {
	case denyLocked:
		msg = "flag 未解锁：先完成本题的攻击动作（见配套教程），攻击成功后这个端点会自动开放。"
	case denyAuthzCmd:
		msg = "/flag 只接受靶机内部自请求。本题读 flag 的路径不在这里：去命令模拟接口 POST /vul/auth-bypass/unsafe/api/cmd 用白名单命令 cat flag.txt。"
	default:
		msg = "/flag 只接受来自靶机内部的自请求（校验的是连接来源，不是 Host 头——伪造 Host 也没用）。本题的攻击思路：让靶机替你来读。"
	}
	http.Error(w, msg, http.StatusForbidden)
}

// ---- 解锁观察 ----

// flagGate /flag 解锁状态：攻击题观察到成功信号后一次性放行；
// seen 供观察类规则（xss 转义对照）跨请求记忆访问过的路径。
type flagGate struct {
	unlocked atomic.Bool
	mu       sync.Mutex
	seen     map[string]bool
}

func newFlagGate() *flagGate {
	return &flagGate{seen: map[string]bool{}}
}

func (g *flagGate) markSeen(key string) {
	g.mu.Lock()
	g.seen[key] = true
	g.mu.Unlock()
}

func (g *flagGate) seenAll(keys ...string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, k := range keys {
		if !g.seen[k] {
			return false
		}
	}
	return true
}

// reqSnap / respSnap 解锁规则可见的请求/响应摘要。
type reqSnap struct {
	Method string
	Path   string
	Query  string // 原始（未解码）查询串
	Header http.Header
	Body   string
}

// target 返回 URL 解码后的完整请求目标（路径 + 查询串）：注入 payload
// 常在路径参数或编码过的 query 里，规则匹配统一用它。
func (r reqSnap) target() string {
	raw := r.Path
	if r.Query != "" {
		raw += "?" + r.Query
	}
	if dec, err := url.QueryUnescape(raw); err == nil {
		return dec
	}
	return raw
}

type respSnap struct {
	Status int
	Body   string
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), sub)
}

// browserLikeUA 判定浏览器流量形态：主流浏览器 UA 都带 Mozilla/5.0 引导串，
// 脚本与工具（Yak、curl、Python requests、Fuzzer 空头）默认不带。仅用于
// tamper 信号的形态锚定——攻击者可伪造，但伪造 UA 已超出本题教学边界。
func browserLikeUA(h http.Header) bool {
	return strings.Contains(h.Get("User-Agent"), "Mozilla")
}

// 解锁规则里的结构性匹配。
var (
	// 闭合注入 UNION COUNT：结果行 password 位是纯数字（用户总数），
	// 正常用户的 password 不会是纯数字（锚定值边界，数字开头的哈希不算）。
	countPasswordRe = regexp.MustCompile(`"password"\s*:\s*"?\d+"?\s*[,}]`)
	// 价格篡改：订单总价被改成 0.xx（正常商品总价不会以 0. 开头）。
	priceZeroRe = regexp.MustCompile(`"total_price"\s*:\s*"?\s*0\.`)
	// 业务链综合题的改价信号：订单项总价精确等于 0.01（1 分钱下单）。浏览器
	// 结算 body 是 {"UserID":N,"OrderItems":[{"ProductName":…,"TotalPrice":"0.01"}]}
	// （上游购物车页 submitOrder 由 DOM 拼 JSON.stringify），容忍冒号两侧空白。
	mallChainPriceRe = regexp.MustCompile(`"TotalPrice"\s*:\s*"0\.01"`)
	// 上传文件名绕过：multipart 文件名以 .php 结尾。
	phpFilenameRe = regexp.MustCompile(`filename="?[^"\r\n;]*\.php`)
)

// unlockRules 攻击题的 flag 解锁规则：网关作为反代可观察全部经手流量，
// 规则信号 = 配套教程宣告的攻击成功特征（匹配统一对解码后文本大小写不敏感）。
// 命中即 /flag 对外放行——学员不需要再借 SSRF 一跳。
var unlockRules = map[string]unlockRule{
	// UNION 拖库：负数 id + UNION 查询读出 root 行。
	// 必须要求 union：?id=2 不注入也直接返回 root 行，那不是攻击。
	"vulinbox-sqli-union": func(g *flagGate, req reqSnap, resp respSnap) bool {
		return req.Method == http.MethodGet && req.Path == "/user/id" &&
			containsFold(req.target(), "union") && strings.Contains(resp.Body, "root")
	},
	// 闭合 + UNION COUNT 统计：COUNT(*) payload 打进带括号模糊查询并拿到占位结果行。
	"vulinbox-sqli-count": func(g *flagGate, req reqSnap, resp respSnap) bool {
		if req.Method != http.MethodGet || req.Path != "/user/name/like/2" {
			return false
		}
		t := strings.ToLower(req.target())
		return strings.Contains(t, "union") && strings.Contains(t, "count(*)") &&
			countPasswordRe.MatchString(resp.Body)
	},
	// 万能密码登录绕过：带引号的注入 body 换来 Login successful。
	"vulinbox-login-bypass": func(g *flagGate, req reqSnap, resp respSnap) bool {
		return req.Method == http.MethodPost && req.Path == "/mall/user/login" &&
			strings.Contains(req.Body, "'") && strings.Contains(resp.Body, "Login successful")
	},
	// 会话重放：携带 _cookie 重放 admin 会话，profile 返回 admin。
	"vulinbox-session-fixed": func(g *flagGate, req reqSnap, resp respSnap) bool {
		return req.Path == "/logic/user/profile" &&
			strings.Contains(req.Header.Get("Cookie"), "_cookie=") &&
			strings.Contains(resp.Body, "admin")
	},
	// 价格篡改：总价改成 0.xx 的订单仍提交成功。
	"vulinbox-mall-price-tamper": func(g *flagGate, req reqSnap, resp respSnap) bool {
		return req.Method == http.MethodPost && req.Path == "/mall/order/submit" &&
			priceZeroRe.MatchString(req.Body) && strings.Contains(resp.Body, "订单提交成功")
	},
	// JWT none 伪造：用伪造 token 访问 profile 拿到 root（正常 user1 token 返回 user1）。
	"vulinbox-jwt-none": func(g *flagGate, req reqSnap, resp respSnap) bool {
		return req.Path == "/jwt/unsafe-login1/profile" && strings.Contains(resp.Body, "root")
	},
	// 文件头绕过：GIF 魔数 + .php 文件名骗过上传头检测。
	"vulinbox-upload-fileheader": func(g *flagGate, req reqSnap, resp respSnap) bool {
		return req.Method == http.MethodPost && strings.HasPrefix(req.Path, "/upload/") &&
			phpFilenameRe.MatchString(req.Body) && strings.Contains(resp.Body, "image/gif")
	},
	// XSS 转义对照（观察类）：回显页与安全过滤页都被访问过即完成本题动作。
	"vulinbox-xss-escape": func(g *flagGate, req reqSnap, resp respSnap) bool {
		if strings.HasPrefix(req.Path, "/xss/echo") {
			g.markSeen("echo")
		}
		if strings.HasPrefix(req.Path, "/xss/safe") {
			g.markSeen("safe")
		}
		return g.seenAll("echo", "safe")
	},

	// ---- 模块专精综合题（模块训练专题唯一题）----
	// 商城业务链攻击：上游购物车里 899 元档的商品 1 分钱下单。四个动作信号
	// 缺一不可，分别对应教程教的 Yakit 模块，路径复用上游既有靶场端点：
	//   ① observe：MITM 观察商城登录（POST /mall/user/login 成功）——
	//      代理观察是整条链的起点；
	//   ② tamper：浏览器结算订单的订单项 TotalPrice 被改写为 0.01——MITM
	//      热加载 Yak 脚本自动改包的产物。信号锚定浏览器订单形态（body 带
	//      OrderItems 的 TotalPrice、UA 为浏览器）：Fuzzer 默认 UA 手搓的
	//      snake_case total_price 不再命中，强制走热加载改浏览器流量这条路；
	//   ③ replay：同一订单请求携带自定义头 X-Mall-Internal（yakit 开头）
	//      重放——浏览器表单发不出的请求形态，Fuzzer 的手搓标记。
	//   ④ codec：口令经 base64→URL 编码送达网关注入的 /academy/codec/verify
	//      端点——Codec 模块的两层变换，端点通过后记入信号（见端点 handler
	//      的综合题分支）。四步信号齐备，gate 才放行 /flag。
	"vulinbox-mall-chain": func(g *flagGate, req reqSnap, resp respSnap) bool {
		if req.Method == http.MethodPost && req.Path == "/mall/user/login" &&
			containsFold(req.Body, "user1") && resp.Status == http.StatusOK {
			g.markSeen("observe")
		}
		if req.Method == http.MethodPost && req.Path == "/mall/order/submit" {
			if mallChainPriceRe.MatchString(req.Body) && browserLikeUA(req.Header) {
				g.markSeen("tamper")
			}
			if internal := req.Header.Get("X-Mall-Internal"); strings.HasPrefix(strings.ToLower(internal), "yakit") {
				g.markSeen("replay")
			}
		}
		return g.seenAll(mallChainSignals...)
	},
	// 注：vulinbox-mall-chain 的编码链验证端点在 newGatewayMux 按 slug 注册；
	// 旧独立 codec 题（vulinbox-codec-pipeline）端点通过即整体解锁。
}

// unlockRule 单题解锁规则：喂入经手流量的摘要，返回是否命中本题攻击成功信号。
type unlockRule func(g *flagGate, req reqSnap, resp respSnap) bool

// observeCap 观察缓冲上限：超出按原样透传（放弃判定）。需要判定的是小
// JSON/HTML 页，正常靶机流量远小于它。
const observeCap = 4 << 20

// unlockTransport 包装主反代 Transport：把经手请求/响应的摘要喂给本题
// 解锁规则，命中一次性放行 /flag。仅对配置了解锁规则的挑战启用。
type unlockTransport struct {
	base http.RoundTripper
	gate *flagGate
	rule unlockRule
}

func (t *unlockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	snap := reqSnap{Method: req.Method, Path: req.URL.Path, Query: req.URL.RawQuery, Header: req.Header}
	if req.Body != nil {
		raw, restored, _ := snapshotBody(req.Body)
		snap.Body = string(raw)
		req.Body = restored
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	raw, restored, oversized := snapshotBody(resp.Body)
	resp.Body = restored
	if oversized {
		return resp, nil
	}
	match := t.rule(t.gate, snap, respSnap{
		Status: resp.StatusCode,
		Body:   decodeBodyText(raw, resp.Header.Get("Content-Encoding")),
	})
	if match && t.gate.unlocked.CompareAndSwap(false, true) {
		log.Printf("flaggateway: attack signal matched, /flag unlocked")
	}
	return resp, nil
}

// snapshotBody 读尽 body（≤observeCap）供规则匹配，返回原始字节与等价回填
// reader。回填字节流与原流完全一致（超限时 = 已读前缀 + 未读余量），
// Content-Length 语义不变；未超限时原 body 已读尽并关闭。
func snapshotBody(rc io.ReadCloser) (raw []byte, restored io.ReadCloser, oversized bool) {
	raw, err := io.ReadAll(io.LimitReader(rc, observeCap+1))
	if err != nil || len(raw) > observeCap {
		return raw, &rejoinCloser{Reader: io.MultiReader(bytes.NewReader(raw), rc), Closer: rc}, true
	}
	rc.Close()
	return raw, io.NopCloser(bytes.NewReader(raw)), false
}

// rejoinCloser 把已读前缀与未读余量拼回单一流，Close 透传原 body。
type rejoinCloser struct {
	io.Reader
	io.Closer
}

// decodeBodyText 规则匹配用的响应文本：gzip 响应解压后返回，其余原样。
func decodeBodyText(raw []byte, encoding string) string {
	if strings.EqualFold(encoding, "gzip") {
		if zr, err := gzip.NewReader(bytes.NewReader(raw)); err == nil {
			if out, err := io.ReadAll(zr); err == nil {
				return string(out)
			}
		}
	}
	return string(raw)
}

// ---- 演示 flag 改写 ----

// rewriteDemoFlag 把上游 /ssrf/flag 响应里的随机 flag{uuid} 替换为
// 固定演示假 flag（flag{test}），页面其余内容不变。
func rewriteDemoFlag(resp *http.Response) error {
	body, err := readAllAndRestore(resp)
	if err != nil {
		return err
	}
	replaced := upstreamFlagRe.ReplaceAll(body, []byte(demoFlag))
	if !bytes.Equal(replaced, body) {
		resp.Body = io.NopCloser(bytes.NewReader(replaced))
		resp.ContentLength = int64(len(replaced))
		resp.Header.Set("Content-Length", strconv.Itoa(len(replaced)))
	}
	return nil
}

// readAllAndRestore 读尽响应体并把可复读的新 body 回填（供调用方改写/转发）。
func readAllAndRestore(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// ---- 命令模拟接口辅助 ----

// extractCommand 从 {"command":"..."} JSON body 提取命令（宽容解析：仅取字面值）。
func extractCommand(body []byte) string {
	s := string(body)
	i := strings.Index(s, "\"command\"")
	if i < 0 {
		return ""
	}
	rest := s[i:]
	j := strings.Index(rest, ":")
	if j < 0 {
		return ""
	}
	rest = rest[j+1:]
	k := strings.Index(rest, "\"")
	if k < 0 {
		return ""
	}
	rest = rest[k+1:]
	m := strings.Index(rest, "\"")
	if m < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:m])
}

// writeJSON 写 JSON 响应（与上游模拟接口同构）。
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, mustJSON(v))
}

func mustJSON(v any) string {
	// 简单 map 序列化，避免引入 encoding/json 之外的依赖分歧。
	switch t := v.(type) {
	case map[string]string:
		var b strings.Builder
		b.WriteString("{\"command\":\"")
		b.WriteString(jsonEscape(t["command"]))
		b.WriteString("\",\"result\":\"")
		b.WriteString(jsonEscape(t["result"]))
		b.WriteString("\"}")
		return b.String()
	}
	return "{}"
}

// jsonEscape 转义 JSON 字符串字面值中的敏感字符。
func jsonEscape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\t", "\\t")
	return r.Replace(s)
}

// passCmdThrough 把原始请求体回填后交给反代（上游拿到与直连时一致的请求）。
func passCmdThrough(w http.ResponseWriter, r *http.Request, body []byte, proxy *httputil.ReverseProxy) {
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	proxy.ServeHTTP(w, r)
}
