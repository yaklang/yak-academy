package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sync"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 动态计分（event-sourcing-rules skill：value = f(solve_count)，linear 衰减）。
// Phase 2 默认值：decay=10，下限 minimum = max(initial*20%, 10)。
// lab.yaml 无此字段，不扩 spec；如需按包配置在 Phase 3 扩展。
const (
	scoreDecay    = 10
	scoreMinRatio = 20 // minimum = initial 的 20%
	scoreMinFloor = 10
)

// autoAnswer 检测点自动判定用的哨兵答案（容器回调触发，非用户提交）。
// 未导出：提交接口拒绝该值，用户无法借 submissions API 伪造"环境判定通过"。
const autoAnswer = "__env_checkpoint_solved__"

// JudgeService 异步判题。提交只入队并返回 pending；判定在 worker 内完成，
// HTTP 请求路径禁止同步判定（event-sourcing-rules skill）。
type JudgeService struct {
	submissions  repo.SubmissionRepo
	challenges   repo.ChallengeRepo
	instances    repo.LabInstanceRepo // flag_mode=instance 时读运行中实例 FlagValue
	events       repo.EventRepo
	competitions repo.CompetitionRepo // 竞赛提交：队伍归属 + 反作弊检测
	bus          *eventbus.Bus
	// engine ACM 编程题（type=code）判题引擎；nil = 引擎未配置，code 题
	// 判 internal_error（平台侧失败，不消耗判定机会语义由 verdict 体现）。
	// 下载端点运行中可换装（judge_engine_admin.go），读写须持 engineMu。
	engine   CodeEngine
	engineMu sync.RWMutex
	// queue 进程内投递通道（低延迟快路径）。持久化由 Submission 表承担：
	// status=pending 即持久队列，轮询兜底重投（judge 幂等护栏防重复判定）。
	queue chan uint
	// runs 自测运行票据表（在线执行框，judge_run.go）：仅内存、不落库。
	runs *runStore
	now  func() time.Time
	// yak 引擎管理态（judge_engine_admin.go）：装配路径/版本、OSS 下载任务。
	yakMu      sync.Mutex
	yakTarget  string // 下载落盘目标（<downloadDir>/yak[.exe]）
	yakPath    string // 当前生效引擎路径（env 指定或下载/探测产物）
	yakVersion string // 尽力而为的版本探测结果（空 = 未知）
	yakOSSBase string // OSS 目录前缀（测试注入 httptest）
	dl         *yakDownloadJob
}

func NewJudgeService(sub repo.SubmissionRepo, chal repo.ChallengeRepo, instances repo.LabInstanceRepo, ev repo.EventRepo, comp repo.CompetitionRepo, bus *eventbus.Bus) *JudgeService {
	return &JudgeService{
		submissions:  sub,
		challenges:   chal,
		instances:    instances,
		events:       ev,
		competitions: comp,
		bus:          bus,
		queue:       make(chan uint, 256),
		runs:        newRunStore(),
		yakOSSBase:  defaultYakOSSBase,
		dl:          &yakDownloadJob{state: yakDLIdle},
		now:         time.Now,
	}
}

// SetCodeEngine 注入 ACM 判题引擎（main 启动装配与引擎下载完成后换装；
// nil/未配置 = code 题提交仍可入队，判定 internal_error）。
func (s *JudgeService) SetCodeEngine(e CodeEngine) {
	s.engineMu.Lock()
	defer s.engineMu.Unlock()
	s.engine = e
}

// codeEngine 并发安全读取判题引擎：下载端点在运行中换装引擎（HTTP 请求
// 触发），与判题 worker / 自测运行 goroutine 的读取构成数据竞争，须持锁。
func (s *JudgeService) codeEngine() CodeEngine {
	s.engineMu.RLock()
	defer s.engineMu.RUnlock()
	return s.engine
}

// Submit 提交答案：写 submission(pending) + submission.created 事件，立即返回。
// 哨兵答案保留给检测点自动判定，用户提交一律拒绝（防伪造环境判定）。
func (s *JudgeService) Submit(ctx context.Context, userID uint, challengeID uint, answer string) (*model.Submission, error) {
	return s.SubmitScoped(ctx, userID, challengeID, 0, answer, "")
}

// SubmitScoped 带作用域提交：competitionID>0 即竞赛提交（记录来源 IP，
// 判定/计分/派生全部按竞赛分组）。训练场提交走 Submit（competitionID=0）。
func (s *JudgeService) SubmitScoped(ctx context.Context, userID, challengeID, competitionID uint, answer, sourceIP string) (*model.Submission, error) {
	if answer == autoAnswer {
		return nil, fmt.Errorf("%w: reserved answer", ErrValidation)
	}
	if err := s.checkCodeSubmit(ctx, userID, challengeID, answer); err != nil {
		return nil, err
	}
	sub := &model.Submission{
		UserID:        userID,
		ChallengeID:   challengeID,
		CompetitionID: competitionID,
		SourceIP:      sourceIP,
		Answer:        answer,
		Status:        model.SubmissionPending,
	}
	if err := s.submissions.Create(ctx, sub); err != nil {
		return nil, err
	}
	created := map[string]any{
		"submission_id": sub.ID, "challenge_id": challengeID,
	}
	if competitionID > 0 {
		created["competition_id"] = competitionID
	}
	s.appendEvent(ctx, userID, model.EventSubmissionCreated, created)
	// 队列满静默丢弃：submission 已落库（pending），由 pending 轮询兜底重投。
	select {
	case s.queue <- sub.ID:
	default:
	}
	return sub, nil
}

// checkCodeSubmit ACM 题提交前置校验：源码大小上限 + 同题并发判定去重。
// flag 判定题（type != code）零成本直通。
func (s *JudgeService) checkCodeSubmit(ctx context.Context, userID, challengeID uint, answer string) error {
	chal, err := s.challenges.FindByID(ctx, challengeID)
	if err != nil {
		return err
	}
	if chal.Type != model.ChallengeCode {
		return nil
	}
	if len(answer) > maxCodeSource {
		return fmt.Errorf("%w: source exceeds %d KiB limit", ErrValidation, maxCodeSource>>10)
	}
	// 库策略拦截（受理前静态扫描）：黑名单库（exec/file 等）或题目白名单外的库
	// 一律 422，不进入引擎执行（yak_libpolicy.go）。配置行读取失败时放行——
	// 数据不一致由 judgeCode 判 internal_error 兜底，不在此处引入新失败面。
	if conf, err := s.challenges.FindACM(ctx, challengeID); err == nil {
		if err := checkYakLibPolicy(answer, parseAllowedLibs(conf.AllowedLibs)); err != nil {
			return err
		}
	}
	pending, err := s.submissions.HasPendingCode(ctx, userID, challengeID)
	if err != nil {
		return err
	}
	if pending {
		return fmt.Errorf("%w: a previous code submission is still being judged", ErrValidation)
	}
	return nil
}

// GetSubmission 查询判定结果（属主校验：仅本人可查，他人 404）。
func (s *JudgeService) GetSubmission(ctx context.Context, userID, submissionID uint) (*model.Submission, error) {
	sub, err := s.submissions.FindByID(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	if sub.UserID != userID {
		return nil, ErrNotFound
	}
	return sub, nil
}

// AutoSolve 检测点自动判定（题目容器内 checker 回调触发）：以哨兵答案入队，
// worker 走与手动提交完全相同的管线判过计分（幂等/防重复通关/动态计分/room.completed）。
// 实例必须仍活跃（token 随实例销毁失效）。
func (s *JudgeService) AutoSolve(ctx context.Context, inst *model.LabInstance) (*model.Submission, error) {
	sub := &model.Submission{
		UserID:      inst.UserID,
		ChallengeID: inst.ChallengeID,
		Answer:      autoAnswer,
		Status:      model.SubmissionPending,
	}
	if err := s.submissions.Create(ctx, sub); err != nil {
		return nil, err
	}
	s.appendEvent(ctx, inst.UserID, model.EventSubmissionCreated, map[string]any{
		"submission_id": sub.ID, "challenge_id": inst.ChallengeID, "source": "checkpoint",
	})
	select {
	case s.queue <- sub.ID:
	default:
	}
	return sub, nil
}

// IsSolved 当前用户该题是否已通关（Room 详情 Lab.solved 字段用）。
func (s *JudgeService) IsSolved(ctx context.Context, userID, challengeID uint) bool {
	solved, err := s.submissions.HasCorrect(ctx, userID, challengeID, 0)
	return err == nil && solved
}

// StartWorker 启动异步判题 worker：消费 goroutine（进程内快路径）+ 轮询 goroutine
// （Submission 表为持久队列，每 2s 扫描 pending 重投——重启/队列满丢失由此兜底）。
// judge 幂等护栏：非 pending 的 submission 直接跳过，重复投递不产生重复判定/计分。
func (s *JudgeService) StartWorker(ctx context.Context) {
	go s.consume(ctx)
	go s.pollPending(ctx)
}

func (s *JudgeService) consume(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-s.queue:
			s.judge(ctx, id)
		}
	}
}

// pollPending 先做一次即时扫描（覆盖重启滞留），之后按 2s 周期重投 pending。
func (s *JudgeService) pollPending(ctx context.Context) {
	s.requeuePending(ctx)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.requeuePending(ctx)
		}
	}
}

// requeuePending 扫描全部 pending submission 重新入队（judge 幂等，重复投递安全）。
func (s *JudgeService) requeuePending(ctx context.Context) {
	pending, err := s.submissions.ListPending(ctx)
	if err != nil {
		log.Printf("judge: requeue pending: %v", err)
		return
	}
	for _, id := range pending {
		select {
		case s.queue <- id:
		default:
			return // 队列满则本轮停止，下个周期继续
		}
	}
}

// judge 判定流程：
//  0. 幂等护栏：重读 submission，status != pending 直接跳过（双投递/并发判定防护，
//     保证 submission.judged 事件与计分只发生一次）
//  1. 读 challenge：flag_mode=instance 比对运行中实例 FlagValue；shared 读 Flag 表
//  2. static 恒时比较（compareFlag）；regex 用 MatchString（恒时要求仅适用 static）
//  3. 动态计分 value = max(initial - decay*n, minimum)
//  4. UpdateStatus + appendEvent(submission.judged)
func (s *JudgeService) judge(ctx context.Context, submissionID uint) {
	sub, err := s.submissions.FindByID(ctx, submissionID)
	if err != nil {
		log.Printf("judge: load submission %d: %v", submissionID, err)
		return
	}
	if sub.Status != model.SubmissionPending {
		return // 已判定：幂等跳过
	}
	chal, err := s.challenges.FindByID(ctx, sub.ChallengeID)
	if err != nil {
		log.Printf("judge: load challenge: %v", err)
		return
	}
	// ACM 编程题：引擎对拍测试数据（不走 flag 比对）。
	if chal.Type == model.ChallengeCode {
		s.judgeCode(ctx, sub, chal)
		return
	}
	// 哨兵答案 = 检测点回调判定通过，跳过 flag 比对直接判对
	// （真实 flag 比对仅服务端内存与 DB，哨兵不泄露任何可用答案）。
	correct := sub.Answer == autoAnswer
	if !correct {
		correct, err = s.matchAnswer(ctx, sub, chal)
		if err != nil {
			log.Printf("judge: match answer: %v", err)
			return
		}
	}
	// 同一用户同一题在相同作用域已 correct → 不重复计分（score=0），派生器跳过零分。
	// 作用域 = competition_id：竞赛首通与训练场首通相互独立。
	if correct {
		if solved, err := s.submissions.HasCorrect(ctx, sub.UserID, sub.ChallengeID, sub.CompetitionID); err == nil && solved {
			s.finish(ctx, sub, model.SubmissionCorrect, 0)
			return
		}
	}
	if !correct {
		// 竞赛提交：判定前跑反作弊检测（flag 命中他人实例 / 来源 IP 异常）。
		if sub.CompetitionID > 0 {
			s.detectCheat(ctx, sub, chal)
		}
		s.finish(ctx, sub, model.SubmissionIncorrect, 0)
		return
	}
	// 竞赛提交：答对也检测（正确 flag 可能是"借来"的，跨队来源仍要告警）。
	if sub.CompetitionID > 0 {
		s.detectCheat(ctx, sub, chal)
	}
	score, err := s.dynamicScore(ctx, sub.ChallengeID, sub.CompetitionID)
	if err != nil {
		log.Printf("judge: dynamic score: %v", err)
		s.finish(ctx, sub, model.SubmissionCorrect, 0)
		return
	}
	s.finish(ctx, sub, model.SubmissionCorrect, score)
	// 题目关联了教程 Room 时记 Room 通关事件（教程进度物化视图用；
	// 纯题目不指向 Room，不产生 Room 侧进度）。
	if chal.TutorialSlug != "" {
		s.AppendRoomCompleted(ctx, sub.UserID, chal.TutorialSlug)
	}
}

// judgeCode ACM 编程题判定：逐组运行引擎对拍测试数据（不再 lazy 首败即停
// ——部分得分制要求每组都有结论）。全组通过 = ac；否则 verdict = 首个失败
// 组的判定（wa/tle/re），但通过的组仍按组分值计入 score。
// internal_error（引擎未配置/读题失败）是平台侧失败，不消耗"判定机会"语义。
func (s *JudgeService) judgeCode(ctx context.Context, sub *model.Submission, chal *model.Challenge) {
	conf, err := s.challenges.FindACM(ctx, chal.ID)
	if err != nil {
		log.Printf("judge: load acm config %d: %v", chal.ID, err)
		s.finishCode(ctx, sub, model.VerdictInternalError)
		return
	}
	data, err := s.challenges.FindTestdata(ctx, chal.ID)
	if err != nil {
		log.Printf("judge: load testdata %d: %v", chal.ID, err)
		s.finishCode(ctx, sub, model.VerdictInternalError)
		return
	}
	engine := s.codeEngine()
	if engine == nil {
		log.Printf("judge: code engine not configured (challenge %s)", chal.Slug)
		s.finishCode(ctx, sub, model.VerdictInternalError)
		return
	}
	// 逐组全跑：每组结论独立（pass 得组分，fail 记 verdict），全部通过才算 ac。
	verdict := model.VerdictAC
	groups := make([]CodeGroupResult, 0, len(data))
	passedWeight := 0
	totalWeight := 0
	for i := range data {
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(conf.TimeLimitS)*time.Second)
		out, runErr := engine.Run(runCtx, sub.Answer, data[i].Input)
		cancel()
		group := CodeGroupResult{Name: data[i].Name, Sample: data[i].Sample}
		switch {
		case runErr == nil && compareOutput(out, data[i].Answer):
			group.Passed = true
			group.Score = data[i].Score
			passedWeight += data[i].Score
		case runErr != nil:
			group.Verdict = string(verdictFromError(runErr))
		default:
			group.Verdict = string(model.VerdictWA)
		}
		totalWeight += data[i].Score
		if !group.Passed && verdict == model.VerdictAC {
			verdict = model.SubmissionVerdict(group.Verdict)
		}
		groups = append(groups, group)
	}
	if verdict == model.VerdictInternalError {
		s.finishCode(ctx, sub, verdict)
		return
	}
	// 部分得分：score = 动态题分 × 通过组权重 / 总权重；全过即全额。
	// 组权重全 0（未配置分值）时退化为全有全无——通过组数 = 组数才得分。
	fullScore, err := s.dynamicScore(ctx, sub.ChallengeID, sub.CompetitionID)
	if err != nil {
		log.Printf("judge: dynamic score: %v", err)
		fullScore = chal.Score
	}
	earned := partialScore(fullScore, passedWeight, totalWeight, len(data) > 0 && verdict == model.VerdictAC)
	if verdict != model.VerdictAC {
		// 竞赛提交：错误答案也走反作弊检测（源码提交不涉及实例 flag，仅 IP 基线推进）。
		if sub.CompetitionID > 0 {
			s.detectCheat(ctx, sub, chal)
		}
		s.finishCode(ctx, sub, verdict, earned, groups)
		return
	}
	// 同作用域已通过 → 不重复计分（score=0），与 flag 题一致。
	if solved, err := s.submissions.HasCorrect(ctx, sub.UserID, sub.ChallengeID, sub.CompetitionID); err == nil && solved {
		s.finishCode(ctx, sub, model.VerdictAC, 0, groups)
		return
	}
	if sub.CompetitionID > 0 {
		s.detectCheat(ctx, sub, chal)
	}
	s.finishCode(ctx, sub, verdict, earned, groups)
	if chal.TutorialSlug != "" {
		s.AppendRoomCompleted(ctx, sub.UserID, chal.TutorialSlug)
	}
}

// CodeGroupResult 编程题逐组判定结论（submission.judged 事件 payload.groups
// 与学生端提交结果反馈共用；样例组标记便于前端区分展示）。
type CodeGroupResult struct {
	Name    string `json:"name"`
	Sample  bool   `json:"sample"`
	Passed  bool   `json:"passed"`
	Verdict string `json:"verdict,omitempty"` // 失败组的判定（wa/tle/re）
	Score   int    `json:"score"`              // 组权重（配置分值；非实得分）
}

// partialScore 部分得分换算：全过 = 题面全额；部分过 = 全额 × 通过权重/总权重，
// 四舍五入；权重全 0（未配置组分值）时部分通过不得分（全有全无语义），
// 只有全部通过才给全额——保持未配置分值题目与旧行为完全一致。
func partialScore(full, passedWeight, totalWeight int, allPassed bool) int {
	if allPassed {
		return full
	}
	if totalWeight <= 0 || passedWeight <= 0 {
		return 0
	}
	return (full*passedWeight + totalWeight/2) / totalWeight
}

// finishCode ACM 判定写回：verdict 一并落库；score 含部分得分（ac 全额/部分
// 过按权重折算）；groups 逐组结论进事件 payload（学生端反馈与审计重放同源）。
// internal_error 保持 pending 语义上"平台失败"——这里仍写 incorrect + verdict，
// 学生端以 verdict=internal_error 提示重试（区别于代码本身错误）。
func (s *JudgeService) finishCode(ctx context.Context, sub *model.Submission, verdict model.SubmissionVerdict, extra ...any) {
	sc := 0
	var groups []CodeGroupResult
	for _, e := range extra {
		switch v := e.(type) {
		case int:
			sc = v
		case []CodeGroupResult:
			groups = v
		}
	}
	status := model.SubmissionIncorrect
	if verdict == model.VerdictAC {
		status = model.SubmissionCorrect
	}
	// 逐组明细落 Submission.groups（学生端轮询反馈；与事件 payload 同源）。
	groupsJSON := ""
	if len(groups) > 0 {
		if b, err := json.Marshal(groups); err == nil {
			groupsJSON = string(b)
		} else {
			log.Printf("judge: marshal groups: %v", err)
		}
	}
	if err := s.submissions.UpdateVerdict(ctx, sub.ID, status, sc, verdict, groupsJSON); err != nil {
		log.Printf("judge: update verdict: %v", err)
		return
	}
	payload := map[string]any{
		"submission_id": sub.ID, "challenge_id": sub.ChallengeID,
		"correct": status == model.SubmissionCorrect, "score": sc,
		"verdict": string(verdict),
	}
	if len(groups) > 0 {
		payload["groups"] = groups
	}
	if sub.CompetitionID > 0 {
		payload["competition_id"] = sub.CompetitionID
		if sub.SourceIP != "" {
			payload["source_ip"] = sub.SourceIP
		}
	}
	s.appendEvent(ctx, sub.UserID, model.EventSubmissionJudged, payload)
}

// matchAnswer shared 读 Flag 表；instance 仅对 running 实例 FlagValue 恒时比较（销毁后需重启）。
func (s *JudgeService) matchAnswer(ctx context.Context, sub *model.Submission, chal *model.Challenge) (bool, error) {
	mode := chal.FlagMode
	if mode == "" {
		mode = model.FlagModeShared
	}
	if mode == model.FlagModeInstance {
		inst, err := s.instances.FindActive(ctx, sub.UserID, sub.ChallengeID)
		if err != nil {
			return false, err
		}
		if inst == nil || inst.Status != model.InstanceRunning || inst.FlagValue == "" {
			return false, nil
		}
		return compareFlag(sub.Answer, inst.FlagValue), nil
	}
	flags, err := s.challenges.FindFlags(ctx, sub.ChallengeID)
	if err != nil {
		return false, err
	}
	return matchAnyFlag(flags, sub.Answer), nil
}

// finish 写回判定结果 + submission.judged 事件。竞赛提交在 payload 中
// 附带 competition_id 与来源 IP（竞赛派生器据此写 CompetitionScore 表）。
func (s *JudgeService) finish(ctx context.Context, sub *model.Submission, status model.SubmissionStatus, score int) {
	if err := s.submissions.UpdateStatus(ctx, sub.ID, status, score); err != nil {
		log.Printf("judge: update status: %v", err)
		return
	}
	payload := map[string]any{
		"submission_id": sub.ID, "challenge_id": sub.ChallengeID,
		"correct": status == model.SubmissionCorrect, "score": score,
	}
	if sub.CompetitionID > 0 {
		payload["competition_id"] = sub.CompetitionID
		if sub.SourceIP != "" {
			payload["source_ip"] = sub.SourceIP
		}
	}
	s.appendEvent(ctx, sub.UserID, model.EventSubmissionJudged, payload)
}

// AppendRoomCompleted 判定首通（correct 且计分）后写 room.completed 事件
// （教题一体：Room 通关即题目通关，teaching-integration skill）。
func (s *JudgeService) AppendRoomCompleted(ctx context.Context, userID uint, roomSlug string) {
	s.appendEvent(ctx, userID, model.EventRoomCompleted, map[string]any{"room_slug": roomSlug})
}

// matchAnyFlag 任一 flag 命中即通过。
func matchAnyFlag(flags []model.Flag, answer string) bool {
	for _, f := range flags {
		if f.Type == model.ChallengeRegex {
			re, err := regexp.Compile(f.Value)
			if err != nil {
				continue // 校验阶段已保证合法，防御性跳过
			}
			if re.MatchString(answer) {
				return true
			}
			continue
		}
		if compareFlag(answer, f.Value) {
			return true
		}
	}
	return false
}

// dynamicScore linear 衰减：value = max(initial - decay*n, minimum)，
// n 为该题在指定作用域的已通过人数（solve_count 按竞赛分组）。
func (s *JudgeService) dynamicScore(ctx context.Context, challengeID, competitionID uint) (int, error) {
	chal, err := s.challenges.FindByID(ctx, challengeID)
	if err != nil {
		return 0, err
	}
	var n int64
	if competitionID > 0 {
		n, err = s.submissions.CountCorrectSolves(ctx, challengeID, competitionID)
	} else {
		n, err = s.challenges.CountSolves(ctx, challengeID)
	}
	if err != nil {
		return 0, err
	}
	return linearDecay(chal.Score, int(n)), nil
}

// detectCheat 竞赛反作弊检测（只在管理端产生警告，不打断提交/判定本身）：
//  1. cross_team_flag：提交串恰好命中"本竞赛其他队伍成员"的运行中实例 flag
//     （flag_mode=instance 的题，flag 按实例生成，跨队共享即违规信号）；
//  2. ip_anomaly：来源 IP 与本人参赛记录上的最近 IP 基线不一致
//     （可能账号被他人代打/共享）。
// 检测失败只记日志，不阻断判题主流程。
func (s *JudgeService) detectCheat(ctx context.Context, sub *model.Submission, chal *model.Challenge) {
	if s.competitions == nil {
		return
	}
	part, err := s.competitions.FindParticipant(ctx, sub.CompetitionID, sub.UserID)
	if err != nil {
		log.Printf("judge: cheat detect participant: %v", err)
		return
	}
	if part == nil {
		return
	}
	// IP 异常：有基线且本次来源不同 → 告警并推进基线。
	if sub.SourceIP != "" && part.LastIP != "" && part.LastIP != sub.SourceIP {
		if err := s.competitions.CreateWarning(ctx, &model.CheatWarning{
			CompetitionID: sub.CompetitionID,
			UserID:        sub.UserID,
			TeamID:        part.TeamID,
			Kind:          model.WarningIPAnomaly,
			Reason:        fmt.Sprintf("来源 IP 变化：上次 %s，本次 %s", part.LastIP, sub.SourceIP),
		}); err != nil {
			log.Printf("judge: create ip warning: %v", err)
		}
	}
	if part.LastIP != sub.SourceIP {
		if err := s.competitions.UpdateParticipantIP(ctx, sub.CompetitionID, sub.UserID, sub.SourceIP); err != nil {
			log.Printf("judge: update participant ip: %v", err)
		}
	}
	// 跨队 flag：仅 instance 模式题目有意义（每实例 flag 唯一）。
	if chal.FlagMode != model.FlagModeInstance {
		return
	}
	teams, err := s.competitions.ListTeams(ctx, sub.CompetitionID)
	if err != nil {
		log.Printf("judge: cheat detect teams: %v", err)
		return
	}
	var rivalTeams []uint
	for _, t := range teams {
		if t.ID != part.TeamID {
			rivalTeams = append(rivalTeams, t.ID)
		}
	}
	if len(rivalTeams) == 0 {
		return // 无队伍区分（全员个人参赛），跳过跨队检测
	}
	members, _, err := s.competitions.ListParticipants(ctx, sub.CompetitionID, 0, 1<<30)
	if err != nil {
		log.Printf("judge: cheat detect members: %v", err)
		return
	}
	for _, m := range members {
		if m.UserID == sub.UserID || m.TeamID == part.TeamID {
			continue // 本人 / 同队不算
		}
		if !containsUint(rivalTeams, m.TeamID) {
			continue
		}
		inst, err := s.instances.FindActive(ctx, m.UserID, sub.ChallengeID)
		if err != nil {
			log.Printf("judge: cheat detect instance: %v", err)
			continue
		}
		if inst == nil || inst.Status != model.InstanceRunning || inst.FlagValue == "" {
			continue
		}
		if compareFlag(sub.Answer, inst.FlagValue) {
			if err := s.competitions.CreateWarning(ctx, &model.CheatWarning{
				CompetitionID: sub.CompetitionID,
				UserID:        sub.UserID,
				TeamID:        part.TeamID,
				Kind:          model.WarningCrossTeamFlag,
				Reason: fmt.Sprintf("提交串命中其他队伍成员(user_id=%d)题目 %s 运行中实例的 flag",
					m.UserID, chal.Slug),
			}); err != nil {
				log.Printf("judge: create cross-team warning: %v", err)
			}
			return // 一次提交最多记一条跨队警告
		}
	}
}

func containsUint(list []uint, v uint) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// linearDecay 计分公式（纯函数，便于测试）。
func linearDecay(initial, solveCount int) int {
	minimum := initial * scoreMinRatio / 100
	if minimum < scoreMinFloor {
		minimum = scoreMinFloor
	}
	value := initial - scoreDecay*solveCount
	if value < minimum {
		return minimum
	}
	return value
}

// appendEvent 写事件表（append-only）并广播给派生器。
func (s *JudgeService) appendEvent(ctx context.Context, userID uint, t model.EventType, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	ev := model.Event{UserID: userID, Type: t, Payload: string(raw)}
	if err := s.events.Append(ctx, &ev); err != nil {
		log.Printf("judge: append event %s: %v", t, err)
		return
	}
	s.bus.Publish(ctx, ev)
}