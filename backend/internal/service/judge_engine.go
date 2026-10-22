package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// CodeEngine ACM 编程题判题引擎：执行一份 yak 源码，喂数据、限时、收输出。
// 实现为接口便于单测注入 fake（不依赖真实 yak 二进制，project-conventions skill）。
type CodeEngine interface {
	// Run 执行源码：stdin 喂 input，返回 stdout（已剥内部标记）、用户代码
	// 执行耗时（毫秒，不含引擎启动/收尾）与判定结果。
	// 返回 (output, n, nil) = 正常跑完；返回 ("", 0, ErrCodeTimeout) = 超时；
	// 返回 ("", 0, *ErrCodeRunError{ExitCode, Stderr}) = 运行出错（编译错误/panic）。
	// limit 只约束用户代码的执行段；引擎解释器冷启动与退出滞留由实现内部
	// 兜底（codeBootGrace），不占题目时限。ctx 仅承载调用方取消。
	Run(ctx context.Context, source, input string, limit time.Duration) (string, int64, error)
}

// ErrCodeTimeout 引擎运行超时（用户代码执行段超出 ACMJudge.TimeLimitS，判 TLE）。
var ErrCodeTimeout = fmt.Errorf("code judge: timeout")

// ErrCodeRunError 引擎运行出错（编译失败/运行时错误，判 RE）。
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

const (
	// codeBootMarker / codeDoneMarker 注入脚本的起止标记。yak 是编译型解释器：
	// 装载 lib 并编译完整个脚本后才执行首行——首行标记出现即用户代码开始
	// 执行（计时起点）；末行标记出现即用户代码执行完毕（计时终点，其后只剩
	// yak 退出滞留，直接杀进程回收）。标记行不进入判定输出。
	codeBootMarker = "__ACADEMY_YAK_BOOT__"
	codeDoneMarker = "__ACADEMY_YAK_DONE__"
	// codeBootGrace 引擎启动+收尾的整体兜底上限（不占题目时限）：yak 冷启动
	//（首次运行要初始化 profile 数据库，实测 20s+）与退出滞留（实测 2~7s）
	// 都在这段内消化。超过仍判 TLE——进程从未产出首行输出。
	codeBootGrace = 60 * time.Second
	// codeDrainGrace 主进程退出/被杀后的输出排空窗口：孙辈进程（引擎派生的
	// 子进程）持有管道写端时读端不见 EOF，短窗口后强制关读端终结读取。
	codeDrainGrace = 500 * time.Millisecond
)

// YakEngine 真实 yak 引擎实现：yak <file>，stdin 喂数据。
// 计时模型：进程全程受 codeBootGrace+limit 硬顶保护；首标记（用户代码开始）
// 到末标记（执行完毕）之间受 limit 约束——冷启动/退出滞留不计入题目时限。
// stdout/stderr 走自有 os.Pipe（不交给 exec 的拷贝 goroutine）：cmd.Wait 只等
// 主进程退出，不会被持有管道写端的孙辈进程拖住；主进程结束后经排空窗口
// 强制关读端收尾。
// Windows 判题（引擎进程无内存/进程数隔离）仅限本地开发；容器化判题是
// 后续阶段（docs/architecture.md 演进路线），生产部署必须在 Linux + 容器内。
type YakEngine struct {
	Bin string // yak 可执行文件路径（config.YakBin）
}

func NewYakEngine(bin string) *YakEngine { return &YakEngine{Bin: bin} }

func (y *YakEngine) Run(ctx context.Context, source, input string, limit time.Duration) (string, int64, error) {
	dir, err := os.MkdirTemp("", "yak-judge-*")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(dir)
	// pathutil.Join 只拼服务端自有段；这里是系统临时目录（非 zip 条目），标准库即可。
	src := filepath.Join(dir, "submission.yak")
	prog := "println(\"" + codeBootMarker + "\")\n" +
		source + "\n" +
		"println(\"" + codeDoneMarker + "\")\n"
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		return "", 0, err
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
	rOut, wOut, err := os.Pipe()
	if err != nil {
		return "", 0, err
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		rOut.Close()
		wOut.Close()
		return "", 0, err
	}
	cmd.Stdout = wOut
	cmd.Stderr = wErr
	if err := cmd.Start(); err != nil {
		rOut.Close()
		wOut.Close()
		rErr.Close()
		wErr.Close()
		return "", 0, &ErrCodeRunError{ExitCode: -1, Stderr: err.Error()}
	}
	// 父进程关掉写端副本：读端的 EOF 完全由子进程侧决定。
	wOut.Close()
	wErr.Close()

	var mu sync.Mutex
	var markAt, doneAt time.Time
	bootCh := make(chan struct{}, 1)
	doneCh := make(chan struct{}, 1)
	mw := &markerWriter{
		inner: &limitedWriter{max: maxCodeOutput},
		onBoot: func() {
			mu.Lock()
			markAt = time.Now()
			mu.Unlock()
			select {
			case bootCh <- struct{}{}:
			default:
			}
		},
		onDone: func() {
			mu.Lock()
			doneAt = time.Now()
			mu.Unlock()
			select {
			case doneCh <- struct{}{}:
			default:
			}
		},
	}
	// markerWriter 只由 stdout 读 goroutine 写入；result() 在读 goroutine
	// 结束后调用（finish 内保证），无并发写。
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 32*1024)
		for {
			n, rerr := rOut.Read(buf)
			if n > 0 {
				mw.Write(buf[:n])
			}
			if rerr != nil {
				return
			}
		}
	}()
	stderrDone := make(chan struct{})
	var stderrBuf stderrCollector
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, rErr)
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	execTimer := time.NewTimer(0) // 初值即停：等 boot 信号才武装
	if !execTimer.Stop() {
		<-execTimer.C
	}
	hardTimer := time.NewTimer(codeBootGrace + limit)
	defer execTimer.Stop()
	defer hardTimer.Stop()

	execMs := func() int64 {
		mu.Lock()
		defer mu.Unlock()
		if markAt.IsZero() {
			return 0
		}
		end := doneAt
		if end.IsZero() {
			end = time.Now()
		}
		return end.Sub(markAt).Milliseconds()
	}
	// drain 统一收尾：等读 goroutine 结束——孙辈进程（引擎派生的子进程）
	// 持有管道写端时读端不见 EOF，短排空窗口后强制关读端终结
	//（markerWriter 此后无并发写，result 可安全读取）。
	drain := func() {
		select {
		case <-readerDone:
		case <-time.After(codeDrainGrace):
			_ = rOut.Close()
			<-readerDone
		}
		_ = rErr.Close()
		<-stderrDone
		_ = rOut.Close()
	}
	// halt 杀主进程并收尾（无 exec 拷贝 goroutine，Wait 在主进程死亡即返回，
	// 不被孙辈拖住）。
	halt := func() {
		_ = cmd.Process.Kill()
		<-waitCh
		drain()
	}
	for {
		select {
		case <-bootCh:
			execTimer.Reset(limit)
		case <-doneCh:
			// 用户代码执行完毕：杀主进程不等 yak 退出滞留（实测每组多耗
			// 2~7s）；完成语义不因 Kill 的非零退出码变成 RE。
			halt()
			return mw.result(), execMs(), nil
		case <-execTimer.C:
			halt()
			return "", 0, ErrCodeTimeout
		case <-hardTimer.C:
			halt()
			return "", 0, ErrCodeTimeout
		case <-ctx.Done():
			halt()
			return "", 0, ErrCodeTimeout
		case werr := <-waitCh:
			// 进程自行退出：编译错误（RE）/ 用户代码崩溃（RE）/ 末标记与
			// 退出的竞态（等价于正常跑完，输出与耗时已采集）。
			drain()
			if werr != nil {
				exitCode := -1
				if exitErr, ok := werr.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				}
				return "", 0, &ErrCodeRunError{ExitCode: exitCode, Stderr: stderrBuf.string()}
			}
			return mw.result(), execMs(), nil
		}
	}
}

// stderrCollector stderr 收集（64KB 截断：编译错误诊断足够，防事故输出撑爆）。
type stderrCollector struct {
	buf strings.Builder
}

func (c *stderrCollector) Write(p []byte) (int, error) {
	if c.buf.Len() >= 64<<10 {
		return len(p), nil
	}
	remain := (64 << 10) - c.buf.Len()
	if len(p) > remain {
		c.buf.Write(p[:remain])
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *stderrCollector) string() string { return truncate(c.buf.String(), 4096) }

// markerWriter 判题 stdout 的标记剥离器，Write 由 stdout 读 goroutine 逐块调用。
// 首标记之前的一切输出先暂存：首标记最终出现则整段丢弃（引擎噪声）；进程
// 结束都没出现则原样放行（兼容不配合标记协议的引擎实现）。
type markerWriter struct {
	inner   *limitedWriter
	onBoot  func()
	onDone  func()
	started bool
	noise   []byte // 首标记前已完结的行（含换行暂存，见首标记即丢弃）
	line    []byte // 正在积累的当前行（两种阶段共用，比对标记用）
}

func (w *markerWriter) Write(p []byte) (int, error) {
	rest := p
	for len(rest) > 0 {
		i := bytes.IndexByte(rest, '\n')
		if i < 0 {
			w.line = append(w.line, rest...)
			break
		}
		w.line = append(w.line, rest[:i]...)
		rest = rest[i+1:]
		w.consumeLine()
	}
	return len(p), nil // 与 limitedWriter 同：向读取方假装全部写完
}

// consumeLine 处理一条完整行（行内容已在 w.line，不含换行）。
func (w *markerWriter) consumeLine() {
	content := strings.TrimSuffix(string(w.line), "\r") // Windows 引擎可能输出 \r\n
	w.line = w.line[:0]
	switch {
	case !w.started && content == codeBootMarker:
		w.started = true
		w.noise = nil // 首标记前的都是引擎噪声，整段丢弃
		w.onBoot()
	case w.started && content == codeDoneMarker:
		w.onDone() // 末标记行丢弃；其后若有残留输出（goroutine 噪声）仍照常放行
	case !w.started:
		w.noise = append(w.noise, content...)
		w.noise = append(w.noise, '\n') // 未见首标记：内容连同换行继续暂存
	default:
		w.inner.Write([]byte(content))
		w.inner.Write([]byte{'\n'}) // 统一归一为 \n 结尾
	}
}

// flush 读端关闭后收尾（readerDone 已等待，无并发写）：释放无换行结尾的
// 尾行；引擎从未打首标记时把噪声暂存段原样放行。
func (w *markerWriter) flush() {
	if w.started {
		w.inner.Write(w.line)
		w.line = nil
		return
	}
	w.inner.Write(w.noise)
	w.inner.Write(w.line)
	w.noise = nil
	w.line = nil
}

func (w *markerWriter) result() string {
	w.flush()
	return w.inner.String()
}

// limitedWriter 输出超限即停止累积（判题不需要超限部分的细节）。
type limitedWriter struct {
	max     int
	builder strings.Builder
	full    bool
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
