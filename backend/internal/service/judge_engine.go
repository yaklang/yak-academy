package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// CodeEngine ACM 编程题判题引擎：执行一份 yak 源码，喂数据、限时、收输出。
// 实现为接口便于单测注入 fake（不依赖真实 yak 二进制，project-conventions skill）。
type CodeEngine interface {
	// Run 执行源码：stdin 喂 input，返回 stdout 与判定结果。
	// 返回 (output, nil) = 正常跑完；返回 ("", ErrTimeout) = 超时；
	// 返回 ("", ErrRunError{ExitCode, Stderr}) = 运行出错（编译错误/panic/内存超限）。
	// context 取消（进程关闭）由实现内部处理，不作为判定结果返回。
	Run(ctx context.Context, source string, input string) (string, error)
}

// ErrCodeTimeout 引擎运行超时（ACMJudge.TimeLimitS 超出，判 TLE）。
var ErrCodeTimeout = fmt.Errorf("code judge: timeout")

// ErrCodeRunError 引擎运行出错（编译失败/运行时错误/内存超限，判 RE）。
type ErrCodeRunError struct {
	ExitCode int
	Stderr   string
}

func (e *ErrCodeRunError) Error() string {
	return fmt.Sprintf("code judge: run error (exit %d): %s", e.ExitCode, e.Stderr)
}

// maxCodeOutput 判题输出截断（1MB）：恶意/事故输出不撑爆 worker 内存。
const maxCodeOutput = 1 << 20

// maxCodeSource 判题源码上限（128KiB）：对齐 ICPC limits.code 默认值。
const maxCodeSource = 128 << 10

// YakEngine 真实 yak 引擎实现：yak <file>，stdin 喂数据，整体计时。
// Windows 判题（引擎进程无内存/进程数隔离）仅限本地开发；容器化判题是
// 后续阶段（docs/architecture.md 演进路线），生产部署必须在 Linux + 容器内。
type YakEngine struct {
	Bin string // yak 可执行文件路径（config.YakBin）
}

func NewYakEngine(bin string) *YakEngine { return &YakEngine{Bin: bin} }

func (y *YakEngine) Run(ctx context.Context, source string, input string) (string, error) {
	dir, err := os.MkdirTemp("", "yak-judge-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	// pathutil.Join 只拼服务端自有段；这里是系统临时目录（非 zip 条目），标准库即可。
	src := filepath.Join(dir, "submission.yak")
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		return "", err
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows 下经 cmd /c：yak.exe 是 GUI 子系统二进制，直接 exec.Start
		// 会以 "Access is denied" 失败（CreateProcess 对 GUI 子系统的行为），
		// 由 shell 代启动可绕开。判题机是 Linux 容器，此分支仅本地开发用。
		cmd = exec.Command("cmd", "/c", y.Bin, src)
	} else {
		cmd = exec.Command(y.Bin, src)
	}
	cmd.Stdin = strings.NewReader(input)
	// 输出截断：防恶意/事故输出（死循环 println）撑爆 worker 内存。
	limited := &limitedWriter{max: maxCodeOutput}
	var stderr strings.Builder
	cmd.Stdout = limited
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", &ErrCodeRunError{ExitCode: -1, Stderr: err.Error()}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return "", ErrCodeTimeout
	case err := <-done:
		if err != nil {
			return "", &ErrCodeRunError{ExitCode: cmd.ProcessState.ExitCode(), Stderr: truncate(stderr.String(), 4096)}
		}
		return limited.String(), nil
	}
}

// limitedWriter 输出超限即停止累积（判题不需要超限部分的细节）。
type limitedWriter struct {
	max    int
	builder strings.Builder
	full   bool
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.builder.Len()+len(p) > w.max {
		remain := w.max - w.builder.Len()
		if remain > 0 {
			w.builder.Write(p[:remain])
		}
		w.full = true
		return len(p), nil // 丢弃超限部分，向引擎进程假装写完
	}
	w.builder.Write(p)
	return len(p), nil
}

func (w *limitedWriter) String() string { return w.builder.String() }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// compareOutput ACM 输出对拍（ICPC 默认校验器语义）：按行比较，token 化后
// 忽略 token 间空白数量与行尾空白（trailing spaces/tabs）；全部行命中即 AC。
// 空行差异按 ICPC 惯例也算 WA（Kattis 默认逐 token，但保留行结构）。
func compareOutput(got, want string) bool {
	gotLines := splitLines(got)
	wantLines := splitLines(want)
	if len(gotLines) != len(wantLines) {
		return false
	}
	for i := range gotLines {
		g := strings.Fields(gotLines[i])
		w := strings.Fields(wantLines[i])
		if len(g) != len(w) {
			return false
		}
		for j := range g {
			if g[j] != w[j] {
				return false
			}
		}
	}
	return true
}

// splitLines 按 \n/\r\n 切行，忽略末尾最后一个换行（数据文件以换行结尾是惯例）。
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// verdictFromError 引擎错误 → ACM 判定。
func verdictFromError(err error) model.SubmissionVerdict {
	switch {
	case err == nil:
		return model.VerdictAC
	case isTimeout(err):
		return model.VerdictTLE
	default:
		return model.VerdictRE
	}
}

func isTimeout(err error) bool {
	return err == ErrCodeTimeout || (err != nil && strings.Contains(err.Error(), "timeout"))
}