package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// AWD checker 框架：CHECK（服务可用）/ PUT（写入 flag）/ GET（读取并核对 flag）。
// 内置 HTTP / TCP / command 三种；command 遵循 RuCTF 退出码：
// 101 OK / 102 check fail / 103 put fail / 104 get fail / 110 checker error。

const (
	AwdCheckOK    = "ok"
	AwdCheckFail  = "fail"
	AwdCheckError = "error"
)

type AwdCheckTarget struct {
	Host        string
	ServiceSlug string
	Ports       []AwdPort
	Flag        string
}

func (t AwdCheckTarget) port(name string) (int, error) {
	if len(t.Ports) == 0 {
		return 0, fmt.Errorf("service has no ports")
	}
	if name == "" {
		return t.Ports[0].Port, nil
	}
	for _, p := range t.Ports {
		if p.Name == name {
			return p.Port, nil
		}
	}
	return 0, fmt.Errorf("port %q not found", name)
}

type AwdCheckResult struct {
	Status  string
	Output  string
	Latency time.Duration
}

type AwdChecker interface {
	Run(ctx context.Context, action string, target AwdCheckTarget) AwdCheckResult
}

// NewAwdChecker 解析 checker_config（空 = HTTP 默认配置）。
func NewAwdChecker(raw string) (AwdChecker, error) {
	var cfg map[string]any
	if strings.TrimSpace(raw) == "" {
		cfg = map[string]any{}
	} else if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("%w: checker_config must be a JSON object", ErrValidation)
	}
	kind := strings.ToLower(awdCfgString(cfg, "type", "http"))
	timeout := time.Duration(awdCfgInt(cfg, "timeout_seconds", 5)) * time.Second
	if timeout < time.Second || timeout > 120*time.Second {
		return nil, fmt.Errorf("%w: checker timeout_seconds out of range", ErrValidation)
	}
	switch kind {
	case "http":
		return &httpAwdChecker{
			scheme:     awdCfgString(cfg, "scheme", "http"),
			portName:   awdCfgString(cfg, "port_name", ""),
			checkPath:  awdCfgString(cfg, "check_path", "/"),
			flagPath:   awdCfgString(cfg, "flag_path", ""),
			flagMethod: strings.ToUpper(awdCfgString(cfg, "flag_method", "POST")),
			flagField:  awdCfgString(cfg, "flag_field", "flag"),
			timeout:    timeout,
		}, nil
	case "tcp":
		return &tcpAwdChecker{portName: awdCfgString(cfg, "port_name", ""), timeout: timeout}, nil
	case "command":
		rawCmd, _ := cfg["command"].([]any)
		cmd := make([]string, 0, len(rawCmd))
		for _, item := range rawCmd {
			if s, ok := item.(string); ok && s != "" {
				cmd = append(cmd, s)
			}
		}
		if len(cmd) == 0 {
			return nil, fmt.Errorf("%w: command checker requires command[]", ErrValidation)
		}
		return &commandAwdChecker{command: cmd, portName: awdCfgString(cfg, "port_name", ""), timeout: timeout}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported checker type %q", ErrValidation, kind)
	}
}

func awdCfgString(cfg map[string]any, key, fallback string) string {
	if v, ok := cfg[key].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func awdCfgInt(cfg map[string]any, key string, fallback int) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}

// ---------- HTTP checker ----------

type httpAwdChecker struct {
	scheme, portName, checkPath, flagPath, flagMethod, flagField string
	timeout                                                      time.Duration
}

func (c *httpAwdChecker) Run(ctx context.Context, action string, target AwdCheckTarget) AwdCheckResult {
	start := time.Now()
	result := func(status, output string) AwdCheckResult {
		return AwdCheckResult{Status: status, Output: truncateAwdOutput(output), Latency: time.Since(start)}
	}
	port, err := target.port(c.portName)
	if err != nil {
		return result(AwdCheckError, err.Error())
	}
	scheme := c.scheme
	if scheme == "" {
		scheme = "http"
	}
	base := fmt.Sprintf("%s://%s:%d", scheme, target.Host, port)
	client := &http.Client{Timeout: c.timeout}
	switch action {
	case "check":
		resp, err := c.request(ctx, client, http.MethodGet, base+c.checkPath, nil)
		if err != nil {
			return result(AwdCheckError, err.Error())
		}
		defer resp.Body.Close()
		body := readAwdBody(resp.Body)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return result(AwdCheckOK, body)
		}
		return result(AwdCheckFail, fmt.Sprintf("status %d: %s", resp.StatusCode, body))
	case "put", "get":
		if c.flagPath == "" {
			return result(AwdCheckFail, "checker has no flag_path")
		}
		endpoint := base + c.flagPath
		if action == "put" {
			if target.Flag == "" {
				return result(AwdCheckError, "put requires flag")
			}
			payload, _ := json.Marshal(map[string]string{c.flagField: target.Flag})
			method := c.flagMethod
			if method == "" {
				method = http.MethodPost
			}
			var body io.Reader
			if method == http.MethodGet {
				endpoint += "?" + url.Values{c.flagField: []string{target.Flag}}.Encode()
			} else {
				body = bytes.NewReader(payload)
			}
			resp, err := c.request(ctx, client, method, endpoint, body)
			if err != nil {
				return result(AwdCheckError, err.Error())
			}
			defer resp.Body.Close()
			out := readAwdBody(resp.Body)
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return result(AwdCheckOK, out)
			}
			return result(AwdCheckFail, fmt.Sprintf("status %d: %s", resp.StatusCode, out))
		}
		resp, err := c.request(ctx, client, http.MethodGet, endpoint, nil)
		if err != nil {
			return result(AwdCheckError, err.Error())
		}
		defer resp.Body.Close()
		body := readAwdBody(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return result(AwdCheckFail, fmt.Sprintf("status %d: %s", resp.StatusCode, body))
		}
		if target.Flag != "" && !strings.Contains(body, target.Flag) {
			return result(AwdCheckFail, "flag not found in response")
		}
		return result(AwdCheckOK, body)
	default:
		return result(AwdCheckError, "unknown checker action")
	}
}

func (c *httpAwdChecker) request(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}

// ---------- TCP checker ----------

type tcpAwdChecker struct {
	portName string
	timeout  time.Duration
}

func (c *tcpAwdChecker) Run(ctx context.Context, action string, target AwdCheckTarget) AwdCheckResult {
	start := time.Now()
	if action != "check" {
		return AwdCheckResult{Status: AwdCheckFail, Output: "tcp checker supports check only"}
	}
	port, err := target.port(c.portName)
	if err != nil {
		return AwdCheckResult{Status: AwdCheckError, Output: err.Error(), Latency: time.Since(start)}
	}
	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", target.Host, port))
	if err != nil {
		return AwdCheckResult{Status: AwdCheckFail, Output: err.Error(), Latency: time.Since(start)}
	}
	conn.Close()
	return AwdCheckResult{Status: AwdCheckOK, Latency: time.Since(start)}
}

// ---------- command checker ----------

type commandAwdChecker struct {
	command  []string
	portName string
	timeout  time.Duration
}

func (c *commandAwdChecker) Run(ctx context.Context, action string, target AwdCheckTarget) AwdCheckResult {
	start := time.Now()
	port, err := target.port(c.portName)
	if err != nil {
		return AwdCheckResult{Status: AwdCheckError, Output: err.Error(), Latency: time.Since(start)}
	}
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, c.command[0], c.command[1:]...)
	cmd.Env = append(cmd.Environ(),
		"AWD_ACTION="+action, "AWD_HOST="+target.Host, fmt.Sprintf("AWD_PORT=%d", port),
		"AWD_FLAG="+target.Flag, "AWD_SERVICE="+target.ServiceSlug,
	)
	out, runErr := cmd.CombinedOutput()
	output := truncateAwdOutput(string(out))
	if runCtx.Err() == context.DeadlineExceeded {
		return AwdCheckResult{Status: AwdCheckError, Output: "checker timeout: " + output, Latency: time.Since(start)}
	}
	code := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			return AwdCheckResult{Status: AwdCheckError, Output: runErr.Error() + ": " + output, Latency: time.Since(start)}
		}
	}
	status := commandAwdStatus(action, code)
	return AwdCheckResult{Status: status, Output: output, Latency: time.Since(start)}
}

func commandAwdStatus(action string, code int) string {
	switch action {
	case "check":
		switch code {
		case 101:
			return AwdCheckOK
		case 102:
			return AwdCheckFail
		}
	case "put":
		switch code {
		case 101:
			return AwdCheckOK
		case 103:
			return AwdCheckFail
		}
	case "get":
		switch code {
		case 101:
			return AwdCheckOK
		case 104:
			return AwdCheckFail
		}
	}
	return AwdCheckError
}

func truncateAwdOutput(out string) string {
	out = strings.TrimSpace(out)
	if len(out) > 4096 {
		return out[:4096] + "…"
	}
	return out
}

func readAwdBody(r io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(r, 8192))
	return truncateAwdOutput(string(data))
}
