package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 自测运行（在线执行框 / custom invocation）：学生在题目页直接跑 yak 源码，
// 不产生 Submission 行、不写事件流、不计分——与判题（judge/judgeCode）完全
// 隔离的运营态操作，学生可无限次自测。异步票据模型与判题一致：HTTP 路径只
// 创建任务 + 轮询结果，引擎执行在独立 goroutine（禁止在请求路径同步执行）。
const (
	// maxRunStdin 自定义输入上限（样例模式直接读库内数据组，不受此限）。
	maxRunStdin = 256 << 10
	// runTicketTTL 完成票据保留时长（过期 404，防内存无界增长）。
	runTicketTTL = 10 * time.Minute
	// runResultTruncate 轮询响应 stdout 截断（引擎侧已有 1MB 硬顶，这里
	// 再收窄响应体，避免轮询大 JSON）。
	runResultTruncate = 64 << 10
	// maxConcurrentRuns 全局并发自测运行上限（保护判题机；提交判定不共用
	// 此信号量，正式判题优先）。
	maxConcurrentRuns = 2
)

// codeRunStatus 自测运行状态机：pending（已受理排队）→ running → done（终态）。
type codeRunStatus string

const (
	runPending codeRunStatus = "pending"
	runRunning codeRunStatus = "running"
	runDone    codeRunStatus = "done"
)

// codeRun 自测运行票据（仅内存，不落库：无审计/重放诉求，重启即清）。
type codeRun struct {
	id          string
	userID      uint
	slug        string
	mode        string // sample | custom
	sample      string // sample 模式组名
	status      codeRunStatus
	outcome     string // ok | tle | re（done 时）
	stdout      string
	stderr      string
	timeMs      int64
	matches     *bool // sample 模式：输出与期望一致（token 级空白不敏感）
	createdAt   time.Time
	finishedAt  time.Time
}

// CodeRunView 自测运行轮询响应（契约 /challenges/{slug}/judge/runs/{id}）。
type CodeRunView struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Mode      string `json:"mode"`
	Sample    string `json:"sample,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	Truncated bool   `json:"truncated"`
	TimeMs    int64  `json:"time_ms"`
	Matches   *bool  `json:"matches"`
}

// runStore 进程内票据表。字段读写都持锁（引擎执行在锁外，只有状态写回进锁）。
type runStore struct {
	mu    sync.Mutex
	runs  map[string]*codeRun
	sem   chan struct{} // 全局并发闸
	order []string      // 创建序（TTL 清扫遍历用，量小无需更复杂结构）
}

func newRunStore() *runStore {
	return &runStore{runs: map[string]*codeRun{}, sem: make(chan struct{}, maxConcurrentRuns)}
}

// CreateCodeRun 受理一次自测运行（sample 组名或自定义 stdin 二选一）。
// 校验：题目可见且为编程题、库策略静态扫描、引擎已配置、源码/输入限额、
// 同用户仅一个进行中。
func (s *JudgeService) CreateCodeRun(ctx context.Context, userID uint, slug, source, sample, stdin string) (string, error) {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return "", mapRecordNotFound(err)
	}
	if chal == nil || !chal.Published(s.now()) || chal.Type != model.ChallengeCode {
		return "", ErrNotFound
	}
	// 库策略拦截先于引擎装配检查：策略拒绝是受理层闸口，不依赖引擎存在
	//（yak_libpolicy.go）——自测不是绕过白名单的后门。
	conf, err := s.challenges.FindACM(ctx, chal.ID)
	if err != nil {
		return "", mapRecordNotFound(err)
	}
	if err := checkYakLibPolicy(source, parseAllowedLibs(conf.AllowedLibs)); err != nil {
		return "", err
	}
	if s.codeEngine() == nil {
		return "", fmt.Errorf("%w: code engine not configured (self-test unavailable)", ErrValidation)
	}
	if len(source) > maxCodeSource {
		return "", fmt.Errorf("%w: source exceeds %d KiB limit", ErrValidation, maxCodeSource>>10)
	}
	if (sample == "") == (stdin == "") {
		return "", fmt.Errorf("%w: exactly one of sample or stdin is required", ErrValidation)
	}
	if len(stdin) > maxRunStdin {
		return "", fmt.Errorf("%w: stdin exceeds %d KiB limit", ErrValidation, maxRunStdin>>10)
	}
	input, answer := stdin, "" // sample 模式：输入与期望输出取自库内样例组
	if sample != "" {
		data, err := s.challenges.FindTestdata(ctx, chal.ID)
		if err != nil {
			return "", err
		}
		var group *model.JudgeTestdata
		for i := range data {
			if data[i].Name == sample && data[i].Sample {
				group = &data[i]
				break
			}
		}
		if group == nil {
			return "", fmt.Errorf("%w: sample %q not found", ErrValidation, sample)
		}
		input, answer = group.Input, group.Answer
	}
	st := s.runs
	st.mu.Lock()
	defer st.mu.Unlock()
	s.sweepRunsLocked()
	for _, r := range st.runs {
		if r.userID == userID && r.status != runDone {
			return "", fmt.Errorf("%w: a previous self-test run is still executing", ErrValidation)
		}
	}
	id, err := randomRunID()
	if err != nil {
		return "", err
	}
	mode := "custom"
	if sample != "" {
		mode = "sample"
	}
	run := &codeRun{
		id: id, userID: userID, slug: slug, mode: mode, sample: sample,
		status: runPending, createdAt: s.now(),
	}
	st.runs[id] = run
	st.order = append(st.order, id)
	// 两种模式共用题目时限（自测与判题同一约束，学生看到的耗时即真实判题尺度）；
	// 数据组内容已拷入局部变量，题目随后被改/删不影响本次运行。
	go s.executeCodeRun(run, source, input, answer, conf.TimeLimitS)
	return id, nil
}

// GetCodeRun 轮询自测运行结果（仅创建者可查；过期/不存在 404）。
func (s *JudgeService) GetCodeRun(ctx context.Context, userID uint, slug, id string) (*CodeRunView, error) {
	st := s.runs
	st.mu.Lock()
	defer st.mu.Unlock()
	s.sweepRunsLocked()
	run, ok := st.runs[id]
	if !ok || run.userID != userID || run.slug != slug {
		return nil, ErrNotFound
	}
	view := &CodeRunView{
		ID: run.id, Status: string(run.status), Mode: run.mode, Sample: run.sample,
		Matches: run.matches,
	}
	if run.status == runDone {
		view.Outcome = run.outcome
		view.Stderr = run.stderr
		view.TimeMs = run.timeMs
		if len(run.stdout) > runResultTruncate {
			view.Stdout = run.stdout[:runResultTruncate]
			view.Truncated = true
		} else {
			view.Stdout = run.stdout
		}
	}
	return view, nil
}

// executeCodeRun 引擎执行（goroutine）：并发闸 → running → 限时运行 → 终态写回。
func (s *JudgeService) executeCodeRun(run *codeRun, source, input, answer string, timeLimitS int) {
	st := s.runs
	st.sem <- struct{}{}
	defer func() { <-st.sem }()
	st.mu.Lock()
	if run.status != runPending { // 防御：票据被清扫后不再执行
		st.mu.Unlock()
		return
	}
	run.status = runRunning
	st.mu.Unlock()

	start := s.now()
	engine := s.codeEngine() // 引擎只增不撤（下载换装），创建时已校验非 nil；防御断言
	if engine == nil {
		st.mu.Lock()
		run.status, run.outcome, run.stderr = runDone, "re", "code engine unavailable"
		st.mu.Unlock()
		return
	}
	// 时限只约束用户代码执行段（引擎冷启动/退出滞留不占），耗时取引擎测得
	// 的执行段毫秒数——学生看到的耗时即真实判题尺度。
	out, execMs, err := engine.Run(context.Background(), source, input, time.Duration(timeLimitS)*time.Second)
	elapsed := s.now().Sub(start)

	st.mu.Lock()
	defer st.mu.Unlock()
	run.status = runDone
	if err == nil && execMs > 0 {
		run.timeMs = execMs
	} else {
		run.timeMs = elapsed.Milliseconds()
	}
	switch {
	case err == nil:
		run.outcome = "ok"
		run.stdout = out
		if run.mode == "sample" {
			matches := compareOutput(out, answer)
			run.matches = &matches
		}
	case isTimeout(err):
		run.outcome = "tle"
	default:
		run.outcome = "re"
		if re, ok := err.(*ErrCodeRunError); ok {
			run.stderr = re.Stderr
		} else {
			run.stderr = truncate(err.Error(), 4096)
		}
	}
	run.finishedAt = s.now()
}

// sweepRunsLocked 清扫过期票据（调用方持锁）：完成超过 TTL 或滞留超过
// TTL+时限兜底（running 卡死，如引擎进程僵死）的一并移除。
func (s *JudgeService) sweepRunsLocked() {
	st := s.runs
	now := s.now()
	keep := st.order[:0]
	for _, id := range st.order {
		r, ok := st.runs[id]
		if !ok {
			continue
		}
		ref := r.finishedAt
		if ref.IsZero() {
			ref = r.createdAt
		}
		if now.Sub(ref) > runTicketTTL+time.Minute {
			delete(st.runs, id)
			continue
		}
		keep = append(keep, id)
	}
	st.order = keep
}

func randomRunID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToLower(hex.EncodeToString(buf)), nil
}
