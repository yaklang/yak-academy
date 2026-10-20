package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// extractCommand 与上游模拟接口同构：{"command":"..."} 的宽容提取。
// cat 白名单匹配依赖它，行为必须稳定。
func TestExtractCommand(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"plain ls", `{"command":"ls"}`, "ls"},
		{"cat flag", `{"command":"cat flag.txt"}`, "cat flag.txt"},
		{"cat abs path", `{"command":"cat /flag.txt"}`, "cat /flag.txt"},
		{"cat bare", `{"command":"cat flag"}`, "cat flag"},
		{"spaces", `{ "command" : "cat flag.txt" }`, "cat flag.txt"},
		{"missing field", `{"cmd":"ls"}`, ""},
		{"empty", `{"command":""}`, ""},
		{"other keys around", `{"a":1,"command":"ls","b":2}`, "ls"},
		{"non json", `command=ls`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractCommand([]byte(c.body)); got != c.want {
				t.Fatalf("extractCommand(%s) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}

func TestJSONEscape(t *testing.T) {
	got := mustJSON(map[string]string{"command": `cat "x"`, "result": "a\nb"})
	want := `{"command":"cat \"x\"","result":"a\nb"}`
	if got != want {
		t.Fatalf("mustJSON = %s, want %s", got, want)
	}
}

// rewriteDemoFlag 必须只替换 flag{uuid} 演示值，页面其余内容原样保留，
// 且改写后 Content-Length 与实际字节数一致（SSRF 一跳转发的就是这份原文）。
func TestRewriteDemoFlag(t *testing.T) {
	page := "<html><body>Example DEMO\n<div>\n\tflag{3aa727b8-1fa3-4570-9f14-759c9905b499}\n</div></body></html>"
	resp := &http.Response{
		Header: http.Header{"Content-Length": []string{strconv.Itoa(len(page))}},
		Body:   io.NopCloser(strings.NewReader(page)),
	}
	if err := rewriteDemoFlag(resp); err != nil {
		t.Fatalf("rewriteDemoFlag: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read rewritten body: %v", err)
	}
	want := strings.Replace(page, "flag{3aa727b8-1fa3-4570-9f14-759c9905b499}", demoFlag, 1)
	if !bytes.Equal(got, []byte(want)) {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(len(want)) {
		t.Fatalf("Content-Length = %s, want %d", resp.Header.Get("Content-Length"), len(want))
	}
}

// 上游未返回 flag{uuid}（如 Host 校验失败的 500 页）时响应必须原样透传，
// 不得凭空注入演示假 flag。
func TestRewriteDemoFlagNoMatchPassthrough(t *testing.T) {
	page := "internal server error"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(page))}
	if err := rewriteDemoFlag(resp); err != nil {
		t.Fatalf("rewriteDemoFlag: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != page {
		t.Fatalf("body = %q, want unchanged %q", got, page)
	}
}

// 实例真 flag（flag{lab-...}）绝不能被演示改写波及——它不是 uuid 形态。
func TestRewriteDemoFlagSparesInstanceFlag(t *testing.T) {
	page := "flag{lab-abc123}"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(page))}
	if err := rewriteDemoFlag(resp); err != nil {
		t.Fatalf("rewriteDemoFlag: %v", err)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(got) != page {
		t.Fatalf("body = %q, want unchanged %q", got, page)
	}
}

// ---- /flag 门禁 ----

// dummyUpstream 起一个只作反代落点的上游（/flag 类测试不真正依赖它的行为）。
func dummyUpstream(t *testing.T) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "upstream")
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}
	return u
}

func getFlag(t *testing.T, mux *http.ServeMux, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/flag", nil)
	r.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// htttestGateway 起一个挂网关 mux 的测试服务，返回其 URL。
func htttestGateway(t *testing.T, flag, slug string, up *url.URL) string {
	t.Helper()
	srv := httptest.NewServer(newGatewayMux(flag, slug, up))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestIsLoopbackRequest 门禁的来源判定只认真实连接源地址。
func TestIsLoopbackRequest(t *testing.T) {
	cases := []struct {
		remote string
		want   bool
	}{
		{"127.0.0.1:40000", true},
		{"127.0.0.1:1", true},
		{"[::1]:40000", true},
		{"172.17.0.1:40000", false}, // Docker 网桥：外部流量经它进容器
		{"192.0.2.1:1234", false},
		{"10.0.0.5:8080", false},
		{"localhost:8080", false}, // 非 IP 字面量不放行
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/flag", nil)
		r.RemoteAddr = c.remote
		if got := isLoopbackRequest(r); got != c.want {
			t.Fatalf("isLoopbackRequest(%s) = %v, want %v", c.remote, got, c.want)
		}
	}
}

// TestFlagEndpointLoopbackGate 默认类（SSRF 题/未知 slug）：浏览器直连 403，
// 伪造 Host 头也没用；回环（靶机内部自请求）放行。
// 回归：修复前 GET /flag 无任何校验直接返回 flag。
func TestFlagEndpointLoopbackGate(t *testing.T) {
	mux := newGatewayMux("flag{lab-x}", "vulinbox-ssrf-basic", dummyUpstream(t))

	w := getFlag(t, mux, "192.0.2.1:1234")
	if w.Code != http.StatusForbidden {
		t.Fatalf("external GET /flag = %d, want 403", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "自请求") || !strings.Contains(body, "Host") {
		t.Fatalf("deny message = %q, want loopback guidance", body)
	}

	// 报告的绕过路径：Host 头写 127.0.0.1 直连——来源仍是外部，必须拒绝。
	r := httptest.NewRequest(http.MethodGet, "/flag", nil)
	r.RemoteAddr = "172.17.0.1:40000"
	r.Host = "127.0.0.1:8787"
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, r)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("forged-Host GET /flag = %d, want 403", w2.Code)
	}

	w3 := getFlag(t, mux, "127.0.0.1:40000")
	if w3.Code != http.StatusOK || !strings.Contains(w3.Body.String(), "flag{lab-x}") {
		t.Fatalf("loopback GET /flag = %d %q, want 200 flag", w3.Code, w3.Body.String())
	}
}

// TestFlagEndpointUnlockGate 攻击题：未解锁时对外与回环都 403（封死跨题借
// SSRF 一跳读 flag）。解锁后的放行路径由 TestFlagUnlockViaProxy 全链路覆盖。
func TestFlagEndpointUnlockGate(t *testing.T) {
	mux := newGatewayMux("flag{lab-x}", "vulinbox-sqli-union", dummyUpstream(t))

	if w := getFlag(t, mux, "192.0.2.1:1234"); w.Code != http.StatusForbidden {
		t.Fatalf("external GET /flag before unlock = %d, want 403", w.Code)
	} else if body := w.Body.String(); !strings.Contains(body, "未解锁") {
		t.Fatalf("deny message = %q, want unlock guidance", body)
	}
	// 回环也不豁免：必须先完成本题攻击动作。
	if w := getFlag(t, mux, "127.0.0.1:40000"); w.Code != http.StatusForbidden {
		t.Fatalf("loopback GET /flag before unlock = %d, want 403", w.Code)
	} else if body := w.Body.String(); !strings.Contains(body, "未解锁") {
		t.Fatalf("loopback deny message = %q, want unlock guidance", body)
	}
}

// TestAuthzCmdFlagDenyPointsToCmd authz-cmd 的 /flag 拒绝文案指向命令模拟
// 接口正路（cat flag.txt），回环放行。
func TestAuthzCmdFlagDenyPointsToCmd(t *testing.T) {
	mux := newGatewayMux("flag{lab-x}", "vulinbox-authz-cmd", dummyUpstream(t))

	w := getFlag(t, mux, "192.0.2.1:1234")
	if w.Code != http.StatusForbidden {
		t.Fatalf("external GET /flag = %d, want 403", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "cat flag.txt") {
		t.Fatalf("deny message = %q, want cat flag.txt guidance", body)
	}
	if w := getFlag(t, mux, "127.0.0.1:40000"); w.Code != http.StatusOK {
		t.Fatalf("loopback GET /flag = %d, want 200", w.Code)
	}
}

// TestCatInjectionScopedToAuthzSlug cat 注入只存在于 authz-cmd 与手工容器；
// 其他题的实例上 cat 落回上游白名单行为（command not found），封死跨题捷径。
func TestCatInjectionScopedToAuthzSlug(t *testing.T) {
	// 上游模拟：cmd 接口对 cat 一律 command not found（真实上游白名单只有 ls）。
	upMux := http.NewServeMux()
	upMux.HandleFunc("/vul/auth-bypass/unsafe/api/cmd", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"command":"cat flag.txt","result":"bash: cat: command not found\n"}`)
	})
	upSrv := httptest.NewServer(upMux)
	t.Cleanup(upSrv.Close)
	up, _ := url.Parse(upSrv.URL)

	postCat := func(slug string) string {
		mux := newGatewayMux("flag{lab-secret}", slug, up)
		r := httptest.NewRequest(http.MethodPost, "/vul/auth-bypass/unsafe/api/cmd",
			strings.NewReader(`{"command":"cat flag.txt"}`))
		r.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Body.String()
	}

	if body := postCat("vulinbox-authz-cmd"); !strings.Contains(body, "flag{lab-secret}") {
		t.Fatalf("authz-cmd cat = %q, want real flag", body)
	}
	if body := postCat(""); !strings.Contains(body, "flag{lab-secret}") {
		t.Fatalf("manual container cat = %q, want real flag", body)
	}
	if body := postCat("vulinbox-sqli-union"); strings.Contains(body, "flag{lab-secret}") {
		t.Fatalf("sqli-union cat leaked flag: %q", body)
	} else if !strings.Contains(body, "command not found") {
		t.Fatalf("sqli-union cat = %q, want upstream not-found passthrough", body)
	}
}

// ---- 解锁规则 ----

// TestUnlockRulesTable 每题规则：正向命中 + 关键反例（防"零攻击解锁"）。
func TestUnlockRulesTable(t *testing.T) {
	cases := []struct {
		name string
		slug string
		req  reqSnap
		resp respSnap
		want bool
	}{
		{
			name: "sqli-union hit",
			slug: "vulinbox-sqli-union",
			req: reqSnap{Method: "GET", Path: "/user/id",
				Query: "id=-1%20UNION%20SELECT%20id,username,password%20FROM%20vulin_users%20WHERE%20username%3D%27root%27"},
			resp: respSnap{Body: `[{"id":2,"username":"root","password":"p@ssword"}]`},
			want: true,
		},
		{
			name: "sqli-union plain id=2 also returns root row, no attack",
			slug: "vulinbox-sqli-union",
			req:  reqSnap{Method: "GET", Path: "/user/id", Query: "id=2"},
			resp: respSnap{Body: `[{"id":2,"username":"root","password":"p@ssword"}]`},
			want: false,
		},
		{
			name: "sqli-union lowercase union still counts",
			slug: "vulinbox-sqli-union",
			req:  reqSnap{Method: "GET", Path: "/user/id", Query: "id=-1%20union%20select%201"},
			resp: respSnap{Body: `[{"username":"root"}]`},
			want: true,
		},
		{
			name: "sqli-union wrong path",
			slug: "vulinbox-sqli-union",
			req:  reqSnap{Method: "GET", Path: "/user/name/like", Query: "id=-1 UNION SELECT 1"},
			resp: respSnap{Body: `[{"username":"root"}]`},
			want: false,
		},
		{
			name: "sqli-count hit",
			slug: "vulinbox-sqli-count",
			req: reqSnap{Method: "GET", Path: "/user/name/like/2",
				Query: "name=a%25%27)%20UNION%20SELECT%201,2,3,4,5,COUNT(*),7,8,9%20FROM%20vulin_users--%20"},
			resp: respSnap{Body: `[{"id":1,"username":"2","password":"26"},{"username":"user_a"}]`},
			want: true,
		},
		{
			name: "sqli-count union without count(*)",
			slug: "vulinbox-sqli-count",
			req:  reqSnap{Method: "GET", Path: "/user/name/like/2", Query: "name=a%20UNION%20SELECT%201"},
			resp: respSnap{Body: `[{"password":"26"}]`},
			want: false,
		},
		{
			name: "sqli-count payload but normal rows (no digit password)",
			slug: "vulinbox-sqli-count",
			req: reqSnap{Method: "GET", Path: "/user/name/like/2",
				Query: "name=a%25%27)%20UNION%20SELECT%201,2,3,4,5,COUNT(*),7,8,9%20FROM%20vulin_users--%20"},
			resp: respSnap{Body: `[{"username":"user_a","password":"3f2c9d81"}]`},
			want: false,
		},
		{
			name: "sqli-count parenthesis-free path is not the injection point",
			slug: "vulinbox-sqli-count",
			req: reqSnap{Method: "GET", Path: "/user/name/like",
				Query: "name=a%20UNION%20SELECT%201,2,3,4,5,COUNT(*)"},
			resp: respSnap{Body: `[{"password":"26"}]`},
			want: false,
		},
		{
			name: "login-bypass hit",
			slug: "vulinbox-login-bypass",
			req:  reqSnap{Method: "POST", Path: "/mall/user/login", Body: `{"username":"admin' or 1=1-- ","password":"x"}`},
			resp: respSnap{Body: `{"id":1,"success":true,"message":"Login successful"}`},
			want: true,
		},
		{
			name: "login-bypass honest login is not an attack",
			slug: "vulinbox-login-bypass",
			req:  reqSnap{Method: "POST", Path: "/mall/user/login", Body: `{"username":"user1","password":"password123"}`},
			resp: respSnap{Body: `{"success":true,"message":"Login successful"}`},
			want: false,
		},
		{
			name: "login-bypass quote but failed",
			slug: "vulinbox-login-bypass",
			req:  reqSnap{Method: "POST", Path: "/mall/user/login", Body: `{"username":"a'","password":"x"}`},
			resp: respSnap{Body: `{"success":false,"message":"Login failed"}`},
			want: false,
		},
		{
			name: "session-fixed hit",
			slug: "vulinbox-session-fixed",
			req:  reqSnap{Method: "GET", Path: "/logic/user/profile", Query: "id=1", Header: http.Header{"Cookie": []string{"_cookie=0f4c2b1a-uuid"}}},
			resp: respSnap{Body: `{"id":1,"username":"admin","role":"admin"}`},
			want: true,
		},
		{
			name: "session-fixed no cookie",
			slug: "vulinbox-session-fixed",
			req:  reqSnap{Method: "GET", Path: "/logic/user/profile", Query: "id=1"},
			resp: respSnap{Body: `{"id":1,"username":"admin"}`},
			want: false,
		},
		{
			name: "session-fixed cookie but own session",
			slug: "vulinbox-session-fixed",
			req:  reqSnap{Method: "GET", Path: "/logic/user/profile", Header: http.Header{"Cookie": []string{"_cookie=own-uuid"}}},
			resp: respSnap{Body: `{"id":5,"username":"user1"}`},
			want: false,
		},
		{
			name: "price-tamper hit",
			slug: "vulinbox-mall-price-tamper",
			req:  reqSnap{Method: "POST", Path: "/mall/order/submit", Body: `{"items":[{"id":1}],"total_price":"0.01"}`},
			resp: respSnap{Body: `{"success":true,"message":"订单提交成功"}`},
			want: true,
		},
		{
			name: "price-tamper unmodified price",
			slug: "vulinbox-mall-price-tamper",
			req:  reqSnap{Method: "POST", Path: "/mall/order/submit", Body: `{"total_price":"199.00"}`},
			resp: respSnap{Body: `{"success":true,"message":"订单提交成功"}`},
			want: false,
		},
		{
			name: "jwt-none hit",
			slug: "vulinbox-jwt-none",
			req:  reqSnap{Method: "GET", Path: "/jwt/unsafe-login1/profile"},
			resp: respSnap{Body: `{"id":1,"username":"root"}`},
			want: true,
		},
		{
			name: "jwt-none normal user token",
			slug: "vulinbox-jwt-none",
			req:  reqSnap{Method: "GET", Path: "/jwt/unsafe-login1/profile"},
			resp: respSnap{Body: `{"id":5,"username":"user1"}`},
			want: false,
		},
		{
			name: "jwt-none other login path",
			slug: "vulinbox-jwt-none",
			req:  reqSnap{Method: "GET", Path: "/jwt/unsafe-login2/profile"},
			resp: respSnap{Body: `{"id":1,"username":"root"}`},
			want: false,
		},
		{
			name: "upload-fileheader hit",
			slug: "vulinbox-upload-fileheader",
			req:  reqSnap{Method: "POST", Path: "/upload/case/fileheader", Body: "--X\r\nContent-Disposition: form-data; name=\"file\"; filename=\"shell.php\"\r\n--X\r\n"},
			resp: respSnap{Body: `{"FileTypeHeaderType":"image/gif"}`},
			want: true,
		},
		{
			name: "upload-fileheader honest gif",
			slug: "vulinbox-upload-fileheader",
			req:  reqSnap{Method: "POST", Path: "/upload/case/fileheader", Body: `filename="avatar.gif"`},
			resp: respSnap{Body: `{"FileTypeHeaderType":"image/gif"}`},
			want: false,
		},
		{
			name: "upload-fileheader php rejected",
			slug: "vulinbox-upload-fileheader",
			req:  reqSnap{Method: "POST", Path: "/upload/case/fileheader", Body: `filename="shell.php"`},
			resp: respSnap{Body: `{"FileTypeHeaderType":"text/x-php"}`},
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rule, ok := unlockRules[c.slug]
			if !ok {
				t.Fatalf("no unlock rule for %s", c.slug)
			}
			if got := rule(newFlagGate(), c.req, c.resp); got != c.want {
				t.Fatalf("rule(%s) = %v, want %v", c.slug, got, c.want)
			}
		})
	}
}

// TestXssTwoPathUnlock 观察类：echo 与 safe 两条路径都访问过才解锁。
func TestXssTwoPathUnlock(t *testing.T) {
	rule := unlockRules["vulinbox-xss-escape"]
	gate := newFlagGate()
	if rule(gate, reqSnap{Path: "/xss/echo?name=x"}, respSnap{}) {
		t.Fatal("echo alone must not unlock")
	}
	if !rule(gate, reqSnap{Path: "/xss/safe?name=x"}, respSnap{}) {
		t.Fatal("echo + safe must unlock")
	}
}

// TestMallChainFourStepUnlock 商城业务链综合题：observe（商城登录）、
// tamper（改价 0.01 下单）、replay（内部标记头重放）、codec（编码链端点）
// 四个信号齐备才解锁；缺任何一步都不放行。信号与顺序无关——本用例刻意
// 把 tamper 留到最后：其余三信号齐备后先用手搓改价验证不命中（回归负例），
// 再用热加载形态命中。
func TestMallChainFourStepUnlock(t *testing.T) {
	upMux := http.NewServeMux()
	upMux.HandleFunc("/mall/user/login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":3,"success":true,"message":"Login successful"}`)
	})
	upMux.HandleFunc("/mall/order/submit", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "订单提交成功")
	})
	upSrv := httptest.NewServer(upMux)
	t.Cleanup(upSrv.Close)
	up, _ := url.Parse(upSrv.URL)
	gw := htttestGateway(t, "flag{lab-mall}", "vulinbox-mall-chain", up)

	// 零攻击：/flag 403
	if resp, err := http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag before chain = %d, want 403", resp.StatusCode)
	}

	// 负例：observe（商城登录）+ 原始价下单（无标记头）——observe 命中，
	// 其余未命中。原始价用浏览器结算形态（OrderItems/TotalPrice、Mozilla UA）。
	resp, err := http.Post(gw+"/mall/user/login", "application/json",
		strings.NewReader(`{"username":"user1","password":"password123"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	orig, _ := http.NewRequest(http.MethodPost, gw+"/mall/order/submit",
		strings.NewReader(`{"UserID":3,"OrderItems":[{"ProductName":"商品4","Quantity":"1","TotalPrice":"899.00"}]}`))
	orig.Header.Set("Content-Type", "application/json")
	orig.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0.0.0")
	resp, err = http.DefaultClient.Do(orig)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp, err = http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag after observe only = %d, want still 403", resp.StatusCode)
	}

	// 负例：replay——原价浏览器订单 + Fuzzer 标记头（观察到的原单重放，
	// 改价信号未命中）——observe+replay 仍不放行
	replayed, _ := http.NewRequest(http.MethodPost, gw+"/mall/order/submit",
		strings.NewReader(`{"UserID":3,"OrderItems":[{"ProductName":"商品4","Quantity":"1","TotalPrice":"899.00"}]}`))
	replayed.Header.Set("Content-Type", "application/json")
	replayed.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0.0.0")
	replayed.Header.Set("X-Mall-Internal", "yakit-order")
	resp, err = http.DefaultClient.Do(replayed)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp, err = http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag after observe+replay = %d, want still 403", resp.StatusCode)
	}

	// 负例：编码链先过（四步可乱序做）——只差 tamper 仍不放行
	good, err := http.Get(gw + "/academy/codec/verify?token=Y29kZWN%2BcHJvb2Y%3D")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(good.Body)
	if good.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("codec verify = %d %q, want 200 ok:true", good.StatusCode, body)
	}
	if resp, err = http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag after observe+replay+codec = %d, want still 403", resp.StatusCode)
	}

	// 回归负例（2026-10-19，PR refactor/module-case-integration）：其余三信号
	// 已齐备，此刻 Fuzzer 手搓 snake_case 改价订单必须仍不解锁——旧信号
	// 匹配 snake_case total_price 时这里会提前放行 /flag（手搓即可通关，
	// 热加载名存实亡）。修复后 tamper 锚定浏览器结算形态（OrderItems 的
	// TotalPrice + Mozilla UA），手搓 body 不命中
	curlTamper, _ := http.NewRequest(http.MethodPost, gw+"/mall/order/submit",
		strings.NewReader(`{"product_id":70,"quantity":1,"total_price":"0.01"}`))
	curlTamper.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(curlTamper)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp, err = http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag after Fuzzer-only tamper = %d, want still 403 (tamper must ride browser traffic)", resp.StatusCode)
	}

	// 正例：热加载改价（tamper）——浏览器结算订单被 MITM 热加载脚本把
	// TotalPrice 改写为 0.01（浏览器 UA + OrderItems 形态，冒号后带空格的
	// 格式化 JSON 也容忍）；纯浏览器形态、不带 Fuzzer 标记头
	req, _ := http.NewRequest(http.MethodPost, gw+"/mall/order/submit",
		strings.NewReader(`{"UserID": 3, "OrderItems": [{"ProductName": "商品4", "Quantity": "1", "TotalPrice": "0.01"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130.0.0.0")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// 四步齐备：/flag 放行
	if resp, err = http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "flag{lab-mall}") {
		t.Fatalf("GET /flag after full chain = %d %q, want 200 flag", resp.StatusCode, body)
	}
}

// TestMallChainCodecLastUnlock 乱序回归：编码链作为最后一步完成时，verify
// 端点 handler 内即时裁决四信号——否则编码链之后再无流量触发 unlockRules
// 的 seenAll，/flag 会永远锁死（e2e 实测踩过：前三步齐备后过 verify，
// GET /flag 仍 403）。
func TestMallChainCodecLastUnlock(t *testing.T) {
	upMux := http.NewServeMux()
	upMux.HandleFunc("/mall/user/login", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":3,"success":true,"message":"Login successful"}`)
	})
	upMux.HandleFunc("/mall/order/submit", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "订单提交成功")
	})
	upSrv := httptest.NewServer(upMux)
	t.Cleanup(upSrv.Close)
	up, _ := url.Parse(upSrv.URL)
	gw := htttestGateway(t, "flag{lab-mall}", "vulinbox-mall-chain", up)

	// ①②③：登录观察 + 浏览器改价订单 + 标记头重放（一个请求带 tamper+replay）
	resp, err := http.Post(gw+"/mall/user/login", "application/json",
		strings.NewReader(`{"username":"user1","password":"password123"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	req, _ := http.NewRequest(http.MethodPost, gw+"/mall/order/submit",
		strings.NewReader(`{"UserID":3,"OrderItems":[{"ProductName":"商品4","Quantity":"1","TotalPrice":"0.01"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Firefox/131.0")
	req.Header.Set("X-Mall-Internal", "yakit-order")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// ④ 编码链最后完成：verify 响应即宣告解锁，紧跟的 GET /flag 必须放行
	good, err := http.Get(gw + "/academy/codec/verify?token=Y29kZWN%2BcHJvb2Y%3D")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(good.Body)
	if good.StatusCode != http.StatusOK || !strings.Contains(string(body), "/flag 已解锁") {
		t.Fatalf("codec verify (last step) = %d %q, want 200 unlocked message", good.StatusCode, body)
	}
	resp, err = http.Get(gw + "/flag")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "flag{lab-mall}") {
		t.Fatalf("GET /flag right after codec-last verify = %d %q, want 200 flag", resp.StatusCode, body)
	}
}

// TestCodecVerifyEndpoint Codec 编码链题：网关注入的验证端点，口令
// codec~proof 需 base64 + URL 双重编码（base64 结果含 + 与 =，裸发会被
// URL 解析成空格），验证通过即解锁 /flag。
func TestCodecVerifyEndpoint(t *testing.T) {
	upSrv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upSrv.Close)
	up, _ := url.Parse(upSrv.URL)
	gw := htttestGateway(t, "flag{lab-codec}", "vulinbox-codec-pipeline", up)

	if resp, err := http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag before verify = %d, want 403", resp.StatusCode)
	}

	// 裸 base64（+ 未编码）：URL 解析把 + 变空格，还原不出原口令
	wrong, _ := http.Get(gw + "/academy/codec/verify?token=Y29kZWN+cHJvb2Y=")
	if wrong == nil || wrong.StatusCode != http.StatusBadRequest {
		t.Fatalf("raw base64 token = %v, want 400", wrong)
	}
	if resp, err := http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag after wrong token = %d, want still 403", resp.StatusCode)
	}

	// 正确编码链：base64 → URL 编码（+ 变 %2B、= 变 %3D）
	good, err := http.Get(gw + "/academy/codec/verify?token=Y29kZWN%2BcHJvb2Y%3D")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(good.Body)
	if good.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("encoded token = %d %q, want 200 ok:true", good.StatusCode, body)
	}

	resp, err := http.Get(gw + "/flag")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "flag{lab-codec}") {
		t.Fatalf("GET /flag after verify = %d %q, want 200 flag", resp.StatusCode, body)
	}
}

// TestFlagUnlockViaProxy 全链路：反代观察到攻击成功信号后 /flag 才放行。
// 回归：修复前浏览器零攻击直连 GET /flag 即得 flag。
func TestFlagUnlockViaProxy(t *testing.T) {
	upMux := http.NewServeMux()
	upMux.HandleFunc("/user/id", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":2,"username":"root","password":"p@ssword"}]`)
	})
	upSrv := httptest.NewServer(upMux)
	t.Cleanup(upSrv.Close)
	up, _ := url.Parse(upSrv.URL)
	gw := htttestGateway(t, "flag{lab-e2e}", "vulinbox-sqli-union", up)

	// 攻击前：直连 403（报告的 bug 场景）。
	if resp, err := http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag before attack = %d, want 403", resp.StatusCode)
	}

	// 正常查询（无注入）不解锁。
	if _, err := http.Get(gw + "/user/id?id=2"); err != nil {
		t.Fatal(err)
	}
	if resp, err := http.Get(gw + "/flag"); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /flag after plain id query = %d, want still 403", resp.StatusCode)
	}

	// UNION 攻击：上游返回 root 行 → 规则命中解锁。
	attack := gw + "/user/id?id=-1%20UNION%20SELECT%20id,username,password%20FROM%20vulin_users%20WHERE%20username%3D%27root%27"
	if resp, err := http.Get(attack); err != nil {
		t.Fatal(err)
	} else if body, _ := io.ReadAll(resp.Body); !strings.Contains(string(body), "root") {
		t.Fatalf("attack response missing root row: %s", body)
	}

	resp, err := http.Get(gw + "/flag")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "flag{lab-e2e}") {
		t.Fatalf("GET /flag after attack = %d %q, want 200 flag", resp.StatusCode, body)
	}
}
