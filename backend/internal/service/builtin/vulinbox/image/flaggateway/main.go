// flaggateway 是 yak-academy 内置训练镜像的入口层（容器内监听 :8787 前置）。
// 它解决两件事：
//  1. 平台按实例注入的 FLAG 环境变量（flag_mode=instance）上游 vulinbox 不消费——
//     网关把它写成容器内 /flag.txt，并提供 GET /flag 读取端点；
//  2. 上游命令模拟接口只实现 ls（cat 报 command not found）——网关对
//     /vul/auth-bypass/unsafe/api/cmd 注入 cat 支持，cat flag.txt / cat /flag.txt
//     返回真 flag，其余命令原样透传上游白名单行为。
//
// 其余一切路径与方法原样反代上游 vulinbox（127.0.0.1:8788），不改变靶机行为。
// flag 只对本容器实例有效（FLAG 环境变量逐实例生成），销毁即失效。
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

const (
	listenAddr = "0.0.0.0:8787" // 容器对外入口（平台端口映射指向这里）
	upstream    = "http://127.0.0.1:8788" // 上游 vulinbox 实际监听地址
	flagFile    = "/flag.txt"
)

func main() {
	flag := strings.TrimSpace(os.Getenv("FLAG"))
	if flag == "" {
		// 未注入 FLAG（共享模式实例/手工起容器）：写入占位 flag，读取路径仍可演示。
		flag = "flag{no-instance-flag}"
	}
	if err := os.WriteFile(flagFile, []byte(flag+"\n"), 0o644); err != nil {
		log.Fatalf("write %s: %v", flagFile, err)
	}

	up, err := url.Parse(upstream)
	if err != nil {
		log.Fatalf("parse upstream: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(up)

	mux := http.NewServeMux()

	// GET /flag：明文返回本实例 flag（供 SSRF/教程场景从靶机自身读取）。
	mux.HandleFunc("/flag", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s\n", flag)
	})

	// 命令模拟接口：注入 cat（上游白名单只有 ls）。
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

	// 其余路径全部反代上游。
	mux.Handle("/", proxy)

	log.Printf("flaggateway listening on %s (upstream %s)", listenAddr, upstream)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
}

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