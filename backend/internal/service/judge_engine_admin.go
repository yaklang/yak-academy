package service

// yak 引擎管理（管理端 ACM tab 状态卡，契约 docs/openapi.yaml
// GET/POST /admin/challenges/engine/yak）：引擎二进制从 yaklang 官方 OSS
// 异步下载到数据目录并即时换装，免去「手工放二进制 + 配 env + 重启」部署链。
// 与 vulinbox 镜像构建（同步 1-3 分钟）不同，yak 约 220MB 经 nginx 代理
// 同步转发放有超时风险，故为后台任务 + 进度轮询；任务态仅存进程内存，
// 服务重启即回 idle（产物文件在磁盘，ConfigureYakEngine 重启时自动探测装配）。

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/pathutil"
)

const (
	// defaultYakOSSBase yaklang 官方 OSS 目录前缀，按运行时 GOOS/GOARCH 拼
	// yak_<os>_<arch>[.exe]（与 vulinbox 镜像构建同源的下载通道）。
	defaultYakOSSBase = "https://oss-qn.yaklang.com/yak/latest/"
	// yakDownloadTimeout 整次下载硬超时：OSS 直连通常 <1 分钟，跨境慢链路
	// 留足余量；超时即 failed，可重试（.part 残留会被下次覆盖）。
	yakDownloadTimeout = 30 * time.Minute
	// yakProbeTimeout 版本探测超时：yak 首次启动有 10-15s 自初始化（插件
	// 同步），探测失败只影响 version 展示，不影响装配。
	yakProbeTimeout = 90 * time.Second
)

type yakDownloadState string

const (
	yakDLIdle    yakDownloadState = "idle"
	yakDLRunning yakDownloadState = "running"
	yakDLDone    yakDownloadState = "done"
	yakDLFailed  yakDownloadState = "failed"
)

// YakDownloadView 下载任务进度（received/total 字节；total=-1 未知大小）。
type YakDownloadView struct {
	State    string `json:"state"` // idle | running | done | failed
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Error    string `json:"error,omitempty"`
}

// YakEngineStatus 引擎状态卡视图（契约 components/schemas/YakEngineStatus）。
type YakEngineStatus struct {
	Configured bool            `json:"configured"`
	Path       string          `json:"path"`
	Version    string          `json:"version"`
	Download   YakDownloadView `json:"download"`
}

// yakDownloadJob 一次下载任务的运行时状态（received 热读走原子计数，
// 其余字段持 yakMu 读写）。
type yakDownloadJob struct {
	state    yakDownloadState
	total    int64
	err      string
	received atomic.Int64
}

// yakBinaryName 下载产物文件名（Windows 带 .exe；判题机是 Linux 容器，
// 开发机 Windows 也能经同一端点取引擎）。
func yakBinaryName() string {
	if runtime.GOOS == "windows" {
		return "yak.exe"
	}
	return "yak"
}

// ConfigureYakEngine 启动装配（main 调用一次）：ACADEMY_YAK_BIN 显式路径
// 优先；为空时探测下载目标 <downloadDir>/yak[.exe]，存在即装配——
// 下载过一次后重启无需再配 env。
func (s *JudgeService) ConfigureYakEngine(envBin, downloadDir string) {
	s.yakMu.Lock()
	defer s.yakMu.Unlock()
	s.yakTarget = pathutil.Join(downloadDir, yakBinaryName())
	if envBin != "" {
		s.yakPath = envBin
	} else {
		s.yakPath = s.yakTarget
		if _, err := os.Stat(s.yakTarget); err == nil {
			s.yakVersion = probeYakVersion(s.yakTarget)
		}
	}
	if _, err := os.Stat(s.yakPath); err == nil {
		s.SetCodeEngine(NewYakEngine(s.yakPath))
	}
}

// YakEngineStatus 聚合引擎装配态与下载任务态（GET /admin/challenges/engine/yak）。
func (s *JudgeService) YakEngineStatus() YakEngineStatus {
	s.yakMu.Lock()
	dl := s.dl
	status := YakEngineStatus{
		Path:    s.yakPath,
		Version: s.yakVersion,
		Download: YakDownloadView{
			State:    string(dl.state),
			Received: dl.received.Load(),
			Total:    dl.total,
			Error:    dl.err,
		},
	}
	s.yakMu.Unlock()
	status.Configured = s.codeEngine() != nil
	return status
}

// DownloadYakEngine 触发异步下载（POST …/engine/yak/download，202）。
// 已有任务进行中返回 ErrValidation（422）；下载本体在后台 goroutine，
// 完成后 .part 原子改名落盘并即时换装引擎（无需重启）。
func (s *JudgeService) DownloadYakEngine() error {
	s.yakMu.Lock()
	if s.dl.state == yakDLRunning {
		s.yakMu.Unlock()
		return fmt.Errorf("%w: a yak engine download is already in progress", ErrValidation)
	}
	s.dl = &yakDownloadJob{state: yakDLRunning}
	target := s.yakTarget
	base := s.yakOSSBase
	s.yakMu.Unlock()
	if target == "" { // ConfigureYakEngine 未被调用（装配线缺配置）
		return fmt.Errorf("%w: yak download dir not configured", ErrValidation)
	}
	go s.runYakDownload(base, target)
	return nil
}

// runYakDownload 下载本体：OSS GET → <target>.part 写入 → chmod 0755 →
// 原子改名 → 即时换装 + 版本探测。任何失败置 failed 并清理 .part。
func (s *JudgeService) runYakDownload(base, target string) {
	fail := func(err error) {
		os.Remove(target + ".part")
		s.yakMu.Lock()
		s.dl.state, s.dl.err = yakDLFailed, truncate(err.Error(), 1024)
		s.yakMu.Unlock()
		log.Printf("yak engine download failed: %v", err)
	}
	asset := fmt.Sprintf("yak_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	client := &http.Client{Timeout: yakDownloadTimeout}
	resp, err := client.Get(base + asset)
	if err != nil {
		fail(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Errorf("oss returned %s for %s", resp.Status, asset))
		return
	}
	if err := os.MkdirAll(parentDir(target), 0o755); err != nil {
		fail(err)
		return
	}
	part := target + ".part"
	f, err := os.Create(part)
	if err != nil {
		fail(err)
		return
	}
	s.yakMu.Lock()
	s.dl.total = resp.ContentLength // -1 = 未知，进度条退化为不定态
	s.yakMu.Unlock()
	_, copyErr := io.Copy(f, &countingReader{r: resp.Body, n: &s.dl.received})
	closeErr := f.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		fail(copyErr)
		return
	}
	if err := os.Chmod(part, 0o755); err != nil {
		fail(err)
		return
	}
	if err := os.Rename(part, target); err != nil {
		fail(err)
		return
	}
	// 即时换装：判题 worker 与自测运行经 codeEngine() 无锁竞争地切换到新引擎。
	s.SetCodeEngine(NewYakEngine(target))
	version := probeYakVersion(target)
	s.yakMu.Lock()
	s.yakPath, s.yakVersion = target, version
	s.dl.state = yakDLDone
	s.yakMu.Unlock()
	log.Printf("yak engine ready: %s (version %q)", target, version)
}

// parentDir 取父目录（pathutil.Join 只拼服务端自有段；此处是对既有路径
// 取目录，标准库即可）。
func parentDir(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i > 0 {
		return p[:i]
	}
	return "."
}

// countingReader io.Copy 装饰器：边拷贝边推进下载进度（原子计数）。
type countingReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// probeYakVersion 尽力而为的版本探测（`yak version` 输出 "Version: x.y.z"）。
// yak 首次启动自初始化可达 15s，超时/失败返回空串，不影响装配与判题。
func probeYakVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), yakProbeTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// 同 YakEngine.Run：GUI 子系统二进制须经 shell 代启动。
		cmd = exec.CommandContext(ctx, "cmd", "/c", bin, "version")
	} else {
		cmd = exec.CommandContext(ctx, bin, "version")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Version: "); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
