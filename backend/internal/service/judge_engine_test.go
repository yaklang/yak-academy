package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ---------- markerWriter 纯状态机（标记剥离） ----------

func newTestMarkerWriter() (*markerWriter, *[]string) {
	events := &[]string{}
	mw := &markerWriter{
		inner:  &limitedWriter{max: maxCodeOutput},
		onBoot: func() { *events = append(*events, "boot") },
		onDone: func() { *events = append(*events, "done") },
	}
	return mw, events
}

func TestMarkerWriterStripsMarkersAndNoise(t *testing.T) {
	cases := []struct {
		name       string
		chunks     []string
		flush      bool
		wantOut    string
		wantEvents []string
	}{
		{
			name:       "boot and done markers stripped, user output kept",
			chunks:     []string{"__ACADEMY_YAK_BOOT__\n42\n__ACADEMY_YAK_DONE__\n"},
			wantOut:    "42\n",
			wantEvents: []string{"boot", "done"},
		},
		{
			name:       "engine noise before boot marker dropped",
			chunks:     []string{"banner line\n", "__ACADEMY_YAK_BOOT__\nhello\n"},
			wantOut:    "hello\n",
			wantEvents: []string{"boot"},
		},
		{
			name:       "output after done marker still collected",
			chunks:     []string{"__ACADEMY_YAK_BOOT__\na\n__ACADEMY_YAK_DONE__\nb\n"},
			wantOut:    "a\nb\n",
			wantEvents: []string{"boot", "done"},
		},
		{
			name:       "no markers at all passes through (non-cooperative engine)",
			chunks:     []string{"foo\nbar\n"},
			wantOut:    "foo\nbar\n",
			wantEvents: nil,
		},
		{
			name:       "marker split across writes reassembled",
			chunks:     []string{"__ACADE", "MY_YAK_BOOT", "__\nx", "\n"},
			wantOut:    "x\n",
			wantEvents: []string{"boot"},
		},
		{
			name:       "crlf line endings normalized",
			chunks:     []string{"__ACADEMY_YAK_BOOT__\r\nok\r\n"},
			wantOut:    "ok\n",
			wantEvents: []string{"boot"},
		},
		{
			name:       "trailing line without newline flushed after started",
			chunks:     []string{"__ACADEMY_YAK_BOOT__\ntail"},
			flush:      true,
			wantOut:    "tail",
			wantEvents: []string{"boot"},
		},
		{
			name:       "trailing pending flushed when engine never printed marker",
			chunks:     []string{"abc"},
			flush:      true,
			wantOut:    "abc",
			wantEvents: nil,
		},
		{
			name:       "marker-like text with prefix is not the marker",
			chunks:     []string{"x__ACADEMY_YAK_BOOT__\ny\n"},
			wantOut:    "x__ACADEMY_YAK_BOOT__\ny\n",
			wantEvents: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mw, events := newTestMarkerWriter()
			for _, chunk := range c.chunks {
				if _, err := mw.Write([]byte(chunk)); err != nil {
					t.Fatalf("write %q: %v", chunk, err)
				}
			}
			got := mw.result() // result 内部 flush
			if got != c.wantOut {
				t.Fatalf("output = %q, want %q", got, c.wantOut)
			}
			if len(*events) != len(c.wantEvents) {
				t.Fatalf("events = %v, want %v", *events, c.wantEvents)
			}
			for i := range *events {
				if (*events)[i] != c.wantEvents[i] {
					t.Fatalf("events = %v, want %v", *events, c.wantEvents)
				}
			}
		})
	}
}

// ---------- YakEngine 集成（脚本替身模拟 yak 计时形态） ----------
// 替身脚本复刻 yak 的三个计时特征：执行段前后各有一段引擎自身耗时
//（启动装载 / 退出滞留），执行段在两行标记之间。

// writeFakeEngine 写一个"引擎"脚本：echo 首标记 → 睡 execSleep → echo 输出
// → echo 末标记 → 睡 teardownSleep（模拟 yak 退出滞留）。返回脚本路径。
// Windows 用 cmd 批处理（ping 计次近似睡 N 秒：N+1 次间隔 N 秒）。
func writeFakeEngine(t *testing.T, execSleep, teardownSleep time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	sleepCmd := func(d time.Duration) string {
		s := int(d.Seconds())
		if s < 1 {
			t.Fatal("fake engine sleeps must be whole seconds (windows ping granularity)")
		}
		if runtime.GOOS == "windows" {
			return "ping -n " + strconv.Itoa(s+1) + " 127.0.0.1 >nul\r\n"
		}
		return "sleep " + strconv.Itoa(s) + "\n"
	}
	var bin, script string
	if runtime.GOOS == "windows" {
		bin = filepath.Join(dir, "fake-engine.cmd")
		script = "@echo off\r\n" +
			"echo __ACADEMY_YAK_BOOT__\r\n" +
			sleepCmd(execSleep) +
			"echo hello\r\n" +
			"echo __ACADEMY_YAK_DONE__\r\n" +
			sleepCmd(teardownSleep)
	} else {
		bin = filepath.Join(dir, "fake-engine")
		script = "#!/bin/sh\n" +
			"echo __ACADEMY_YAK_BOOT__\n" +
			sleepCmd(execSleep) +
			"echo hello\n" +
			"echo __ACADEMY_YAK_DONE__\n" +
			sleepCmd(teardownSleep)
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake engine: %v", err)
	}
	return bin
}

// 引擎启动/退出滞留不计入题目时限：执行段 2s、时限 3s 时必须正常完成——
// 修复前（整个进程生命周期受限时）启动+执行 3s+ 已超时。
func TestYakEngineTimingExcludesBootAndTeardown(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real processes")
	}
	engine := NewYakEngine(writeFakeEngine(t, 2*time.Second, 8*time.Second))

	start := time.Now()
	out, execMs, err := engine.Run(context.Background(), "ignored", "", 3*time.Second)
	wall := time.Since(start)

	if err != nil {
		t.Fatalf("err = %v, want nil (exec segment within limit despite boot/teardown)", err)
	}
	if out != "hello\n" {
		t.Fatalf("output = %q, want %q", out, "hello\n")
	}
	if execMs < 1500 {
		t.Fatalf("execMs = %d, want >= 1500 (script sleeps 2s between markers)", execMs)
	}
	if wall >= 6*time.Second {
		t.Fatalf("wall = %v: done marker should kill before the 8s teardown linger", wall)
	}
}

// 执行段超时限判 TLE：首标记出现后死等（模拟用户死循环）。
func TestYakEngineTimeoutCountsOnlyExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real processes")
	}
	dir := t.TempDir()
	var bin, script string
	if runtime.GOOS == "windows" {
		bin = filepath.Join(dir, "loop.cmd")
		script = "@echo off\r\necho __ACADEMY_YAK_BOOT__\r\nping -n 60 127.0.0.1 >nul\r\necho __ACADEMY_YAK_DONE__\r\n"
	} else {
		bin = filepath.Join(dir, "loop")
		script = "#!/bin/sh\necho __ACADEMY_YAK_BOOT__\nsleep 60\necho __ACADEMY_YAK_DONE__\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake engine: %v", err)
	}

	start := time.Now()
	_, _, err := NewYakEngine(bin).Run(context.Background(), "ignored", "", 2*time.Second)
	wall := time.Since(start)

	if !errors.Is(err, ErrCodeTimeout) {
		t.Fatalf("err = %v, want ErrCodeTimeout", err)
	}
	if wall >= 10*time.Second {
		t.Fatalf("wall = %v: exec timer must kill at limit, not at the 60s script end", wall)
	}
}

// 引擎失败路径：进程非零退出（模拟编译错误）→ *ErrCodeRunError 带退出码。
func TestYakEngineNonZeroExitIsRunError(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns real processes")
	}
	dir := t.TempDir()
	var bin, script string
	if runtime.GOOS == "windows" {
		bin = filepath.Join(dir, "fail.cmd")
		script = "@echo off\r\necho compile error detail >&2\r\nexit /b 7\r\n"
	} else {
		bin = filepath.Join(dir, "fail")
		script = "#!/bin/sh\necho 'compile error detail' >&2\nexit 7\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake engine: %v", err)
	}

	_, _, err := NewYakEngine(bin).Run(context.Background(), "ignored", "", 5*time.Second)
	var runErr *ErrCodeRunError
	if !errors.As(err, &runErr) {
		t.Fatalf("err = %v, want *ErrCodeRunError", err)
	}
	if runErr.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", runErr.ExitCode)
	}
	if !strings.Contains(runErr.Stderr, "compile error detail") {
		t.Fatalf("stderr = %q, want compile detail", runErr.Stderr)
	}
}
