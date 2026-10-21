package service

// yak 引擎管理端单测：启动装配（env 优先 / 下载目录自动探测）、OSS 异步
// 下载（成功换装 / 并发 422 / 失败态），OSS 以 httptest 假服务器注入
// yakOSSBase，不触真实网络、不依赖真实 yak 二进制。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func newEngineAdminService(t *testing.T) *JudgeService {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:judgeengineadmin?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		dbg, _ := gdb.DB()
		dbg.Close()
	})
	return NewJudgeService(
		repo.NewSubmissionRepo(gdb), repo.NewChallengeRepo(gdb), repo.NewLabInstanceRepo(gdb),
		repo.NewEventRepo(gdb), repo.NewCompetitionRepo(gdb), eventbus.New(),
	)
}

// waitDownloadState 轮询下载任务到达目标态（异步 goroutine 的汇合点）。
func waitDownloadState(t *testing.T, svc *JudgeService, state string) YakEngineStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := svc.YakEngineStatus(); st.Download.State == state {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("download state %q not reached (now %q)", state, svc.YakEngineStatus().Download.State)
	return YakEngineStatus{}
}

// 启动装配：ACADEMY_YAK_BIN 显式路径优先；为空时探测下载目录产物，
// 存在即装配（下载过一次后重启免配 env）。
func TestConfigureYakEngineEnvOverrideAndAutodetect(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, yakBinaryName())

	// env 路径存在 → 直接装配该路径。
	envBin := filepath.Join(dir, "custom-yak")
	if err := os.WriteFile(envBin, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := newEngineAdminService(t)
	svc.ConfigureYakEngine(envBin, dir)
	st := svc.YakEngineStatus()
	if !st.Configured || st.Path != envBin {
		t.Fatalf("env override: configured=%v path=%q, want configured path %q", st.Configured, st.Path, envBin)
	}

	// env 为空且目录无产物 → 未装配，path 展示下载目标。
	svc2 := newEngineAdminService(t)
	svc2.ConfigureYakEngine("", dir)
	st = svc2.YakEngineStatus()
	if st.Configured || st.Path != target {
		t.Fatalf("empty dir: configured=%v path=%q, want unconfigured path %q", st.Configured, st.Path, target)
	}

	// env 为空且目录有产物 → 自动探测装配。
	if err := os.WriteFile(target, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc3 := newEngineAdminService(t)
	svc3.ConfigureYakEngine("", dir)
	if st := svc3.YakEngineStatus(); !st.Configured || st.Path != target {
		t.Fatalf("autodetect: configured=%v path=%q, want configured path %q", st.Configured, st.Path, target)
	}
}

// 下载全流程：OSS 请求正确的 GOOS/GOARCH 资产名 → .part 落盘 → 原子改名
// → 即时换装（Configured=true）；received/total 与响应体一致。
func TestDownloadYakEngineSuccessFlow(t *testing.T) {
	payload := []byte("yak-binary\x00")
	for len(payload) < 64*1024 {
		payload = append(payload, payload...)
	}
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.Write(payload)
	}))
	t.Cleanup(server.Close)

	svc := newEngineAdminService(t)
	svc.yakOSSBase = server.URL + "/"
	dir := t.TempDir()
	target := filepath.Join(dir, yakBinaryName())
	svc.ConfigureYakEngine("", dir)

	if err := svc.DownloadYakEngine(); err != nil {
		t.Fatalf("DownloadYakEngine: %v", err)
	}
	st := waitDownloadState(t, svc, "done")

	wantAsset := fmt.Sprintf("/yak_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		wantAsset += ".exe"
	}
	if gotPath != wantAsset {
		t.Errorf("oss request path = %q, want %q", gotPath, wantAsset)
	}
	if st.Download.Received != int64(len(payload)) || st.Download.Total != int64(len(payload)) {
		t.Errorf("progress = %d/%d, want %d/%d", st.Download.Received, st.Download.Total, len(payload), len(payload))
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("downloaded file mismatch: err=%v len=%d want=%d", err, len(got), len(payload))
	}
	if _, err := os.Stat(target + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".part residue after success: %v", err)
	}
	if !st.Configured || st.Path != target {
		t.Errorf("post-download status: configured=%v path=%q, want configured %q", st.Configured, st.Path, target)
	}
}

// 下载进行中再触发 → ErrValidation（契约 422）；放行后首个任务正常完成。
func TestDownloadYakEngineRejectsConcurrent(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // 挂起首个任务，制造 running 窗口
		w.Write([]byte("fake-yak"))
	}))
	t.Cleanup(server.Close)

	svc := newEngineAdminService(t)
	svc.yakOSSBase = server.URL + "/"
	dir := t.TempDir()
	svc.ConfigureYakEngine("", dir)

	if err := svc.DownloadYakEngine(); err != nil {
		t.Fatalf("first download: %v", err)
	}
	if err := svc.DownloadYakEngine(); !errors.Is(err, ErrValidation) {
		t.Fatalf("concurrent download err = %v, want ErrValidation", err)
	}
	once.Do(func() { close(release) })
	waitDownloadState(t, svc, "done")
}

// OSS 非 200 → failed 态（携带错误信息），不留 .part、不装配。
func TestDownloadYakEngineFailureState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	svc := newEngineAdminService(t)
	svc.yakOSSBase = server.URL + "/"
	dir := t.TempDir()
	target := filepath.Join(dir, yakBinaryName())
	svc.ConfigureYakEngine("", dir)

	if err := svc.DownloadYakEngine(); err != nil {
		t.Fatalf("DownloadYakEngine: %v", err)
	}
	st := waitDownloadState(t, svc, "failed")
	if st.Download.Error == "" {
		t.Error("failed state missing error message")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target exists after failure: %v", err)
	}
	if _, err := os.Stat(target + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".part residue after failure: %v", err)
	}
	if st.Configured {
		t.Error("engine configured after failed download")
	}
}

// 未装配（ConfigureYakEngine 未调用）时触发下载 → ErrValidation。
func TestDownloadYakEngineRequiresConfigure(t *testing.T) {
	svc := newEngineAdminService(t)
	if err := svc.DownloadYakEngine(); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}
