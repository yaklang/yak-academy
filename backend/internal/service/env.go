package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/safego"
)

// InstanceSpec 驱动创建实例所需的全部信息（驱动实现不感知 DB）。
type InstanceSpec struct {
	InstanceID  uint
	ChallengeID uint
	UserID      uint
	Image       string
	Port        int
	Env         map[string]string // 注入容器环境变量（如 FLAG）
}

// ImageBuilder 题目包镜像构建（content 上传 image: . 时调用）。
type ImageBuilder interface {
	Build(ctx context.Context, tag, contextDir string) error
}

// InstanceConflictError 单用户同时只允许一个活跃容器：启动第二个题目时返回，
// 携带当前活跃实例及富化信息（api 层映射 409 并附 current 供前端引导关闭）。
type InstanceConflictError struct {
	Current        *model.LabInstance
	Username       string
	ChallengeTitle string
	ChallengeSlug  string
}

func (e *InstanceConflictError) Error() string {
	return fmt.Sprintf("user already has an active instance #%d (challenge %d)",
		e.Current.ID, e.Current.ChallengeID)
}

// InstanceDriver 题目环境驱动接口：实现只与运行时（Docker 等）交互，
// DB 状态由 EnvService 维护（project-conventions skill）。
type InstanceDriver interface {
	// Create 按 spec 启动实例并返回访问地址。
	Create(ctx context.Context, spec InstanceSpec) (endpoint string, err error)
	// Destroy 按 InstanceID 销毁实例（幂等：实例不存在视为成功）。
	Destroy(ctx context.Context, instanceID uint) error
	// HostStats 宿主与全部容器的资源占用快照（性能面板）。
	HostStats(ctx context.Context) (*HostStats, error)
}

// NoopDriver 占位驱动：未配置容器运行时的 stub，调用一律报错。
type NoopDriver struct{}

func (NoopDriver) Create(ctx context.Context, spec InstanceSpec) (string, error) {
	return "", errNotImplemented("env driver create")
}
func (NoopDriver) Destroy(ctx context.Context, instanceID uint) error {
	return errNotImplemented("env driver destroy")
}
func (NoopDriver) HostStats(ctx context.Context) (*HostStats, error) {
	return nil, errNotImplemented("env driver host stats")
}
func (NoopDriver) Build(ctx context.Context, tag, contextDir string) error {
	return fmt.Errorf("%w: docker unavailable: cannot build image %s", ErrValidation, tag)
}

const (
	// instanceTTL 实例存活时长：超时由 reaper 回收。
	instanceTTL = time.Hour
	// hostStatsFreshFor 性能快照新鲜期：命中缓存立即返回，超过后由后台刷新。
	// 页面每 10s 轮询一次，因此面板打开期间每轮都会触发一次后台采集。
	hostStatsFreshFor = 5 * time.Second
	// hostStatsTimeout 单次采集上限：docker daemon 卡住时不无限占用请求或后台 goroutine。
	hostStatsTimeout = 10 * time.Second
	// hostStatsRetryAfter 后台刷新失败后的重试冷却，避免 Docker 故障时页面轮询打成重试风暴。
	hostStatsRetryAfter = 10 * time.Second
	// hostStatsStaleWarning 刷新失败但仍可返回最近一次成功快照时的提示。
	hostStatsStaleWarning = "容器运行时状态刷新失败，展示最近一次成功采样"
)

// hostStatsFlight 一次进行中的性能采集；并发请求共享同一结果，避免重复执行
// docker info/stats（两者都是秒级操作）。
type hostStatsFlight struct {
	done  chan struct{}
	stats *HostStats
	err   error
}

type EnvService struct {
	instances  repo.LabInstanceRepo
	logs       repo.InstanceLogRepo
	challenges repo.ChallengeRepo
	users      repo.UserRepo
	events     repo.EventRepo
	bus        *eventbus.Bus
	driver     InstanceDriver
	publicURL  string // CHECKPOINT_URL 前缀（容器内回达平台）
	// now 时钟注入，测试可固定时间；生产用 time.Now。
	now func() time.Time
	// dataDir 数据卷目录（性能面板 statfs 用量统计；空 = 不统计）。
	dataDir string

	statsMu      sync.Mutex
	statsCache   *HostStats
	statsAt      time.Time
	statsWarning string
	statsRetryAt time.Time
	statsFlight  *hostStatsFlight
	// statsFreshFor 缓存新鲜期；生产取 hostStatsFreshFor，测试可置 0 强制刷新。
	statsFreshFor time.Duration
}

func NewEnvService(instances repo.LabInstanceRepo, logs repo.InstanceLogRepo, challenges repo.ChallengeRepo, users repo.UserRepo, events repo.EventRepo, bus *eventbus.Bus, d InstanceDriver, publicURL string) *EnvService {
	return &EnvService{
		instances: instances, logs: logs, challenges: challenges, users: users,
		events: events, bus: bus,
		driver: d, publicURL: publicURL, now: time.Now,
		statsFreshFor: hostStatsFreshFor,
	}
}

// SetDataDir 性能面板的数据卷占用统计目录（可选装配；与备份目录同源的
// 数据库所在目录）。空或非 Linux 不统计（DataVolume 为 null）。
func (s *EnvService) SetDataDir(dir string) { s.dataDir = dir }

// PrewarmHostStats 启动时后台采集一次性能快照，让首个进入容器管理页的请求
// 直接命中缓存，而不是在请求里等待 docker stats 的秒级采样窗口。
func (s *EnvService) PrewarmHostStats(ctx context.Context) { s.startStatsRefresh(ctx) }

// HostStats 性能面板快照：优先返回缓存（采样时间随响应返回）；缓存过期时
// 立即返回旧快照并并发刷新，页面轮询随后追平。无缓存（服务刚启动）时等待
// 一次采集结果，失败直接返回错误。
func (s *EnvService) HostStats(ctx context.Context) (*HostStats, error) {
	s.statsMu.Lock()
	if s.statsCache == nil {
		s.statsMu.Unlock()
		return s.refreshHostStats(ctx)
	}
	snapshot := s.snapshotLocked()
	now := s.now()
	stale := now.Sub(s.statsAt) > s.statsFreshFor
	inflight := s.statsFlight != nil
	retryAt := s.statsRetryAt
	s.statsMu.Unlock()

	if stale {
		switch {
		case inflight:
			snapshot.Refreshing = true
		case !now.Before(retryAt):
			snapshot.Refreshing = true
			s.startStatsRefresh(ctx)
		}
	}
	return snapshot, nil
}

// refreshHostStats 同步等待一次采集（冷启动/无缓存路径）。已有后台采集时
// 等待同一结果；多个冷请求也只会真正执行一次 docker 命令。
func (s *EnvService) refreshHostStats(ctx context.Context) (*HostStats, error) {
	s.statsMu.Lock()
	if s.statsCache != nil {
		snapshot := s.snapshotLocked()
		s.statsMu.Unlock()
		return snapshot, nil
	}
	flight := s.statsFlight
	started := false
	if flight == nil {
		flight = &hostStatsFlight{done: make(chan struct{})}
		s.statsFlight = flight
		started = true
	}
	s.statsMu.Unlock()

	if started {
		var stats *HostStats
		var err error
		if safego.Run("env.host-stats", func() { stats, err = s.collectHostStats(ctx) }) {
			s.finishStatsRefresh(flight, stats, err)
		} else {
			s.finishStatsRefresh(flight, nil, fmt.Errorf("host stats collection panic"))
		}
	}

	select {
	case <-flight.done:
		if flight.stats != nil {
			return flight.stats, nil
		}
		return nil, flight.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// startStatsRefresh 后台采集一次；采集不受触发请求取消影响，但受超时约束。
func (s *EnvService) startStatsRefresh(ctx context.Context) {
	s.statsMu.Lock()
	if s.statsFlight != nil {
		s.statsMu.Unlock()
		return
	}
	flight := &hostStatsFlight{done: make(chan struct{})}
	s.statsFlight = flight
	s.statsMu.Unlock()

	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostStatsTimeout)
	safego.Go("env.host-stats", func() {
		defer cancel()
		defer safego.Guard("env.host-stats", func() {
			s.finishStatsRefresh(flight, nil, fmt.Errorf("host stats collection panic"))
		})
		stats, err := s.collectHostStats(bg)
		s.finishStatsRefresh(flight, stats, err)
	})
}

// finishStatsRefresh 落缓存并唤醒等待者；失败但已有旧快照时保留旧值并附告警，
// 避免运维面板在 Docker 短暂不可用时整块消失。
func (s *EnvService) finishStatsRefresh(flight *hostStatsFlight, stats *HostStats, err error) {
	s.statsMu.Lock()
	if err == nil && stats != nil {
		s.statsCache = stats
		s.statsAt = s.now()
		s.statsWarning = ""
		s.statsRetryAt = time.Time{}
	} else {
		s.statsWarning = hostStatsStaleWarning
		s.statsRetryAt = s.now().Add(hostStatsRetryAfter)
		log.Printf("env: refresh performance snapshot: %v", err)
	}
	flight.stats = s.snapshotLocked()
	flight.err = err
	if flight.stats != nil {
		flight.err = nil
	}
	s.statsFlight = nil
	close(flight.done)
	s.statsMu.Unlock()
}

func (s *EnvService) collectHostStats(ctx context.Context) (*HostStats, error) {
	stats, err := s.driver.HostStats(ctx)
	if err != nil {
		return nil, err
	}
	if s.dataDir != "" {
		stats.DataVolume = volumeUsage(s.dataDir)
	}
	return stats, nil
}

// snapshotLocked 返回缓存的独立副本（调用方不得改写内部缓存）；调用方需持有 statsMu。
func (s *EnvService) snapshotLocked() *HostStats {
	if s.statsCache == nil {
		return nil
	}
	out := cloneHostStats(s.statsCache)
	at := s.statsAt
	out.SampledAt = &at
	out.Refreshing = false
	out.Warning = s.statsWarning
	return out
}

// cloneHostStats 复制快照外层结构与容器列表；内部标量指针只读共享。
func cloneHostStats(in *HostStats) *HostStats {
	if in == nil {
		return nil
	}
	out := *in
	if in.Host != nil {
		host := *in.Host
		out.Host = &host
	}
	if in.DataVolume != nil {
		volume := *in.DataVolume
		out.DataVolume = &volume
	}
	out.Containers = append([]ContainerResource(nil), in.Containers...)
	return &out
}

// EnsureInstance 幂等启动：已有 running 实例直接返回；
// 无则按题目 env 定义创建（并发上限校验 → DB 落 creating → driver.Create → 回写 running）。
// driver 失败回滚 DB 记录（status→stopped），不留半成品。
func (s *EnvService) EnsureInstance(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error) {
	return s.EnsureInstanceScoped(ctx, userID, challengeID, 0)
}

// EnsureInstanceScoped 带竞赛作用域启动：competitionID>0 时实例归属该竞赛
// （竞赛容器独立面板），单容器约束仍全局生效（一人一活跃容器）。
func (s *EnvService) EnsureInstanceScoped(ctx context.Context, userID, challengeID, competitionID uint) (*model.LabInstance, error) {
	if inst, err := s.instances.FindActive(ctx, userID, challengeID); err != nil {
		return nil, err
	} else if inst != nil {
		return inst, nil
	}
	// 单容器约束：该用户在其他题目还有活跃实例 → 409，携带当前实例供前端引导关闭。
	if other, err := s.instances.FindActiveAny(ctx, userID); err != nil {
		return nil, err
	} else if other != nil {
		userName, chals, err := s.lookupUsersAndChallenges(ctx, []model.LabInstance{*other})
		if err != nil {
			return nil, err
		}
		conflict := &InstanceConflictError{Current: other, Username: userName[userID]}
		if c, ok := chals[other.ChallengeID]; ok {
			conflict.ChallengeTitle = c.Title
			conflict.ChallengeSlug = c.Slug
		}
		return nil, conflict
	}
	chal, err := s.challenges.FindByID(ctx, challengeID)
	if err != nil {
		return nil, err
	}
	if chal.Image == "" || chal.Port < 1 {
		return nil, fmt.Errorf("%w: lab has no container environment", ErrValidation)
	}
	flagValue, envVars, err := s.prepareInstanceFlag(ctx, chal)
	if err != nil {
		return nil, err
	}
	// 检测点令牌：每实例随机生成，注入 CHECKPOINT_TOKEN/CHECKPOINT_URL
	// （题目内 checker 达成目标后回调触发自动判定）。
	checkpointToken, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	if envVars == nil {
		envVars = map[string]string{}
	}
	envVars["CHECKPOINT_TOKEN"] = checkpointToken
	envVars["CHECKPOINT_URL"] = fmt.Sprintf("%s/api/v1/checkpoints/%s", s.publicURL, checkpointToken)
	// 挑战标识：靶机入口层（如 vulinbox 镜像的 flaggateway）按题差异化
	// flag 读取门禁（SSRF 自请求放行 / 按题攻击信号解锁）依赖它识别实例归属。
	envVars["CHALLENGE_SLUG"] = chal.Slug
	inst := &model.LabInstance{
		UserID: userID, ChallengeID: challengeID,
		CompetitionID:   competitionID,
		Status:          model.InstanceCreating,
		CheckpointToken: checkpointToken,
	}
	if err := s.instances.Create(ctx, inst); err != nil {
		return nil, err
	}
	spec := InstanceSpec{
		InstanceID: inst.ID, ChallengeID: challengeID, UserID: userID,
		Image: chal.Image, Port: chal.Port, Env: envVars,
	}
	endpoint, err := s.driver.Create(ctx, spec)
	if err != nil {
		// 回滚：DB 记录标记 stopped，不留 creating 残留；失败详情落日志表。
		log.Printf("env: instance %d create failed (user=%d challenge=%d image=%s): %v",
			inst.ID, userID, challengeID, chal.Image, err)
		if uerr := s.instances.UpdateStatus(ctx, inst.ID, model.InstanceStopped); uerr != nil {
			log.Printf("env: rollback instance %d: %v", inst.ID, uerr)
		}
		s.appendLog(ctx, inst.ID, userID, challengeID, model.InstanceActionStopped, "owner",
			model.InstanceStopped, "create failed: "+err.Error())
		return nil, err
	}
	expireAt := s.now().Add(instanceTTL)
	if err := s.instances.UpdateRunning(ctx, inst.ID, endpoint, expireAt, flagValue); err != nil {
		return nil, err
	}
	s.appendLog(ctx, inst.ID, userID, challengeID, model.InstanceActionStarted, "owner",
		model.InstanceRunning, "")
	s.appendEvent(ctx, userID, model.EventLabStarted, map[string]any{
		"instance_id": inst.ID, "challenge_id": challengeID, "endpoint": endpoint,
	})
	return s.instances.FindByID(ctx, inst.ID)
}

// DestroyInstance 销毁实例（属主校验）。driver 失败保留记录，由 reaper 兜底回收。
func (s *EnvService) DestroyInstance(ctx context.Context, userID, challengeID uint) error {
	inst, err := s.instances.FindActive(ctx, userID, challengeID)
	if err != nil {
		return err
	}
	if inst == nil {
		return ErrNotFound
	}
	if err := s.driver.Destroy(ctx, inst.ID); err != nil {
		log.Printf("env: instance %d destroy failed (user=%d challenge=%d): %v",
			inst.ID, userID, challengeID, err)
		s.appendLog(ctx, inst.ID, userID, challengeID, model.InstanceActionStopped, "owner",
			inst.Status, "destroy failed: "+err.Error())
		return err
	}
	if err := s.instances.UpdateStatus(ctx, inst.ID, model.InstanceDestroyed); err != nil {
		return err
	}
	s.appendLog(ctx, inst.ID, userID, challengeID, model.InstanceActionStopped, "owner",
		model.InstanceDestroyed, "")
	s.appendEvent(ctx, userID, model.EventLabStopped, map[string]any{
		"instance_id": inst.ID, "challenge_id": challengeID,
	})
	return nil
}

// GetInstance 查询本人实例状态（无实例返回 nil，不报错）。
func (s *EnvService) GetInstance(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error) {
	return s.instances.FindActive(ctx, userID, challengeID)
}

// ResolveCheckpoint 检测点回调解析：token 命中活跃实例则返回 (实例, nil)，
// 否则 (nil, nil)（api 层 404）。token 即能力凭证（随容器环境注入，仅题目可见）。
func (s *EnvService) ResolveCheckpoint(ctx context.Context, token string) (*model.LabInstance, error) {
	return s.instances.FindByCheckpointToken(ctx, token)
}

// ExtendInstance 续期 running 实例 TTL（不重建容器；expire_at = now+1h）。
func (s *EnvService) ExtendInstance(ctx context.Context, userID, challengeID uint) (*model.LabInstance, error) {
	inst, err := s.instances.FindActive(ctx, userID, challengeID)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, ErrNotFound
	}
	if inst.Status != model.InstanceRunning {
		return nil, fmt.Errorf("%w: only running instances can be extended", ErrValidation)
	}
	// 治理上限：每实例最多续期 2 次（初始 1h + 2×1h = 最长 3h 占用）。
	if inst.ExtendCount >= maxExtends {
		return nil, fmt.Errorf("%w: extend limit reached (%d times); destroy and restart the instance", ErrValidation, maxExtends)
	}
	expireAt := s.now().Add(instanceTTL)
	if err := s.instances.Extend(ctx, inst.ID, expireAt); err != nil {
		return nil, err
	}
	return s.instances.FindByID(ctx, inst.ID)
}

// prepareInstanceFlag flag_mode=instance 时生成唯一 flag 并准备 FLAG 环境变量。
func (s *EnvService) prepareInstanceFlag(ctx context.Context, chal *model.Challenge) (flagValue string, env map[string]string, err error) {
	mode := chal.FlagMode
	if mode == "" {
		mode = model.FlagModeShared
	}
	if mode != model.FlagModeInstance {
		return "", nil, nil
	}
	flags, err := s.challenges.FindFlags(ctx, chal.ID)
	if err != nil {
		return "", nil, err
	}
	if len(flags) == 0 {
		return "", nil, fmt.Errorf("%w: instance flag_mode requires a flag template", ErrValidation)
	}
	token, err := randomToken(16)
	if err != nil {
		return "", nil, err
	}
	flagValue = strings.ReplaceAll(flags[0].Value, "{token}", token)
	return flagValue, map[string]string{"FLAG": flagValue}, nil
}

func randomToken(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AdminLabInstance 管理端容器面板条目：LabInstance + 用户/题目富化。
type AdminLabInstance struct {
	model.LabInstance
	Username       string `json:"username"`
	ChallengeTitle string `json:"challenge_title"`
	ChallengeSlug  string `json:"challenge_slug"`
}

// ListInstances 全量实例分页（管理端容器面板；批量富化用户名与题目名）。
func (s *EnvService) ListInstances(ctx context.Context, offset, limit int) ([]AdminLabInstance, int64, error) {
	list, total, err := s.instances.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	userName, chals, err := s.lookupUsersAndChallenges(ctx, list)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AdminLabInstance, 0, len(list))
	for _, inst := range list {
		out = append(out, buildAdminLabInstance(inst, userName, chals))
	}
	return out, total, nil
}

// ListCompetitionInstances 竞赛容器分页（管理端竞赛容器独立面板）：
// 只含该竞赛绑定题目上启动的实例（competition_id = 指定竞赛）。
func (s *EnvService) ListCompetitionInstances(ctx context.Context, competitionID uint, offset, limit int) ([]AdminLabInstance, int64, error) {
	list, total, err := s.instances.ListByCompetition(ctx, competitionID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	userName, chals, err := s.lookupUsersAndChallenges(ctx, list)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AdminLabInstance, 0, len(list))
	for _, inst := range list {
		out = append(out, buildAdminLabInstance(inst, userName, chals))
	}
	return out, total, nil
}

// AdminDestroyInstance 管理员强制销毁任意实例（管理端容器面板）。
// 与属主销毁同语义：driver.Destroy → destroyed → lab.instance.stopped 事件
// （payload 加 source=admin 区分，event-sourcing-rules skill：append-only）。
func (s *EnvService) AdminDestroyInstance(ctx context.Context, instanceID uint) error {
	inst, err := s.instances.FindByID(ctx, instanceID)
	if err != nil {
		return err
	}
	if err := s.driver.Destroy(ctx, inst.ID); err != nil {
		log.Printf("env: admin destroy instance %d failed: %v", inst.ID, err)
		s.appendLog(ctx, inst.ID, inst.UserID, inst.ChallengeID, model.InstanceActionStopped, "admin",
			inst.Status, "destroy failed: "+err.Error())
		return err
	}
	if err := s.instances.UpdateStatus(ctx, inst.ID, model.InstanceDestroyed); err != nil {
		return err
	}
	s.appendLog(ctx, inst.ID, inst.UserID, inst.ChallengeID, model.InstanceActionStopped, "admin",
		model.InstanceDestroyed, "")
	s.appendEvent(ctx, inst.UserID, model.EventLabStopped, map[string]any{
		"instance_id": inst.ID, "challenge_id": inst.ChallengeID, "source": "admin",
	})
	return nil
}

// StartReaper 后台回收循环：定时清理过期 running 实例。
// 单实例部署（第一版约束），进程内循环即可，无需分布式锁。
func (s *EnvService) StartReaper(ctx context.Context, interval time.Duration) {
	safego.Go("env.reaper", func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 单轮 panic 不终止回收循环：过期实例留给下一轮重试。
				safego.Run("env.reapExpired", func() { s.reapExpired(ctx) })
			}
		}
	})
}

// reapExpired 回收 ExpireAt 过期的 running 实例：先销毁驱动资源，成功才改 destroyed；
// 驱动失败保留记录（下轮重试）。
func (s *EnvService) reapExpired(ctx context.Context) {
	expired, err := s.instances.ListExpired(ctx, s.now())
	if err != nil {
		log.Printf("env: list expired: %v", err)
		return
	}
	for _, inst := range expired {
		if err := s.driver.Destroy(ctx, inst.ID); err != nil {
			log.Printf("env: reap instance %d: %v", inst.ID, err)
			continue
		}
		if err := s.instances.UpdateStatus(ctx, inst.ID, model.InstanceDestroyed); err != nil {
			log.Printf("env: mark destroyed %d: %v", inst.ID, err)
			continue
		}
		s.appendLog(ctx, inst.ID, inst.UserID, inst.ChallengeID, model.InstanceActionStopped, "reaper",
			model.InstanceDestroyed, "ttl expired")
	}
}

// AdminInstanceLog 管理端容器日志条目：InstanceLog + 用户/题目富化。
type AdminInstanceLog struct {
	model.InstanceLog
	Username       string `json:"username"`
	ChallengeTitle string `json:"challenge_title"`
	ChallengeSlug  string `json:"challenge_slug"`
}

// ListActiveInstances 当前全部活跃容器（creating/running，管理端概览；无分页）。
func (s *EnvService) ListActiveInstances(ctx context.Context) ([]AdminLabInstance, error) {
	list, err := s.instances.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	userName, chals, err := s.lookupUsersAndChallenges(ctx, list)
	if err != nil {
		return nil, err
	}
	out := make([]AdminLabInstance, 0, len(list))
	for _, inst := range list {
		out = append(out, buildAdminLabInstance(inst, userName, chals))
	}
	return out, nil
}

// ListActiveByChallenge 某题目全部用户的活跃实例（题目删除前销毁容器用）。
func (s *EnvService) ListActiveByChallenge(ctx context.Context, challengeID uint) ([]model.LabInstance, error) {
	return s.instances.ListActiveByChallenge(ctx, challengeID)
}

// DestroyCompetitionInstances 销毁某竞赛全部运行容器（竞赛删除级联；
// 逐台 AdminDestroyInstance，失败即返回错误中止删除流程——防孤儿容器）。
func (s *EnvService) DestroyCompetitionInstances(ctx context.Context, competitionID uint) error {
	instances, _, err := s.instances.ListByCompetition(ctx, competitionID, 0, 1<<30)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if inst.Status != model.InstanceCreating && inst.Status != model.InstanceRunning {
			continue
		}
		if err := s.AdminDestroyInstance(ctx, inst.ID); err != nil {
			return fmt.Errorf("destroy instance %d: %w", inst.ID, err)
		}
	}
	return nil
}

// ListLogs 容器生命周期日志分页（管理端容器日志表格；批量富化用户名与题目）。
func (s *EnvService) ListLogs(ctx context.Context, offset, limit int) ([]AdminInstanceLog, int64, error) {
	list, total, err := s.logs.List(ctx, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	instances := make([]model.LabInstance, 0, len(list))
	for _, l := range list {
		instances = append(instances, model.LabInstance{UserID: l.UserID, ChallengeID: l.ChallengeID})
	}
	userName, chals, err := s.lookupUsersAndChallenges(ctx, instances)
	if err != nil {
		return nil, 0, err
	}
	out := make([]AdminInstanceLog, 0, len(list))
	for _, l := range list {
		item := AdminInstanceLog{InstanceLog: l, Username: userName[l.UserID]}
		if c, ok := chals[l.ChallengeID]; ok {
			item.ChallengeTitle = c.Title
			item.ChallengeSlug = c.Slug
		}
		out = append(out, item)
	}
	return out, total, nil
}

// lookupUsersAndChallenges 批量取用户名与题目信息（富化用；空列表安全）。
func (s *EnvService) lookupUsersAndChallenges(ctx context.Context, list []model.LabInstance) (map[uint]string, map[uint]model.Challenge, error) {
	userIDSet := make(map[uint]struct{})
	chalIDSet := make(map[uint]struct{})
	for _, inst := range list {
		userIDSet[inst.UserID] = struct{}{}
		chalIDSet[inst.ChallengeID] = struct{}{}
	}
	userIDs := make([]uint, 0, len(userIDSet))
	for id := range userIDSet {
		userIDs = append(userIDs, id)
	}
	chalIDs := make([]uint, 0, len(chalIDSet))
	for id := range chalIDSet {
		chalIDs = append(chalIDs, id)
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, nil, err
	}
	userName := make(map[uint]string, len(users))
	for _, u := range users {
		userName[u.ID] = u.Username
	}
	chals, err := s.challenges.FindByIDs(ctx, chalIDs...)
	if err != nil {
		return nil, nil, err
	}
	chalMap := make(map[uint]model.Challenge, len(chals))
	for _, c := range chals {
		chalMap[c.ID] = c
	}
	return userName, chalMap, nil
}

// buildAdminLabInstance 单条活跃实例条目富化。
func buildAdminLabInstance(inst model.LabInstance, userName map[uint]string, chals map[uint]model.Challenge) AdminLabInstance {
	item := AdminLabInstance{LabInstance: inst, Username: userName[inst.UserID]}
	if c, ok := chals[inst.ChallengeID]; ok {
		item.ChallengeTitle = c.Title
		item.ChallengeSlug = c.Slug
	}
	return item
}

// appendLog 写容器生命周期日志（append-only，失败只记服务端日志不影响主流程）。
func (s *EnvService) appendLog(ctx context.Context, instanceID, userID, challengeID uint, action model.InstanceAction, source string, status model.InstanceStatus, detail string) {
	l := &model.InstanceLog{
		InstanceID: instanceID, UserID: userID, ChallengeID: challengeID,
		Action: action, Source: source, Status: status, Detail: detail,
	}
	if err := s.logs.Create(ctx, l); err != nil {
		log.Printf("env: append instance log (instance=%d action=%s): %v", instanceID, action, err)
	}
}

// appendEvent 写事件表（append-only）并广播。
func (s *EnvService) appendEvent(ctx context.Context, userID uint, t model.EventType, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	ev := model.Event{UserID: userID, Type: t, Payload: string(raw)}
	if err := s.events.Append(ctx, &ev); err != nil {
		log.Printf("env: append event %s: %v", t, err)
		return
	}
	s.bus.Publish(ctx, ev)
}

// maxExtends 单实例续期次数上限（治理约束）。
const maxExtends = 2

// DockerDriver 通过 docker CLI 管理容器（无 SDK 依赖；接口已隔离实现）。
// Memory/CPUs 为题目容器资源限额（docker --memory/--cpus；空串 = 不限）。
// PublicHost 为实例 endpoint 主机名（浏览器直连靶机的地址）：空/回环 →
// 端口仅发布宿主 127.0.0.1（本地开发，浏览器就在宿主上）；对外主机名
// （服务器部署的内网 IP 等，ACADEMY_INSTANCE_PUBLIC_HOST）→ 端口发布
// 全网卡，endpoint 用该主机名拼装——否则部署后学员浏览器拿到的
// 127.0.0.1 指向自己的机器，靶机不可达。
type DockerDriver struct {
	Memory     string
	CPUs       string
	PublicHost string
}

func NewDockerDriver(memory, cpus, publicHost string) *DockerDriver {
	return &DockerDriver{Memory: memory, CPUs: cpus, PublicHost: publicHost}
}

// containerName 实例容器命名（纯函数便于测试）。
func containerName(instanceID uint) string {
	return fmt.Sprintf("yak-lab-%d", instanceID)
}

// instanceHost 归一 endpoint 主机名：空视作 127.0.0.1，IPv6 加方括号；
// 显式配置（含 localhost）保持原样展示（纯函数便于测试）。
func instanceHost(publicHost string) string {
	host := publicHost
	if host == "" {
		host = "127.0.0.1"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host
}

// publishBind 按对外主机名决定端口发布的宿主 IP：回环族（127.0.0.1/
// localhost/::1）只绑 127.0.0.1（本地开发不扩大暴露面），其余对外主机名
// 绑全网卡（空串 = 0.0.0.0）。
func publishBind(host string) string {
	switch host {
	case "127.0.0.1", "localhost", "[::1]":
		return "127.0.0.1"
	}
	return ""
}

// Create 启动容器：随机宿主端口（绑定范围见 publishBind），可选 -e 环境变量
// 与资源限额，读回实际端口按对外主机名拼 endpoint。
// --add-host host-gateway：Linux 宿主上容器内默认解析不了 host.docker.internal，
// 显式映射到网关 IP 后 CHECKPOINT_URL 默认值跨平台开箱即用（Docker Desktop
// 自带该解析，显式条目与其不冲突）。
func (d DockerDriver) Create(ctx context.Context, spec InstanceSpec) (string, error) {
	name := containerName(spec.InstanceID)
	host := instanceHost(d.PublicHost)
	args := []string{"run", "-d", "--name", name,
		"-p", fmt.Sprintf("%s::%d", publishBind(host), spec.Port),
		"--add-host", "host.docker.internal:host-gateway"}
	if d.Memory != "" {
		args = append(args, "--memory", d.Memory)
	}
	if d.CPUs != "" {
		args = append(args, "--cpus", d.CPUs)
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, spec.Image)
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker run %s: %v: %s", spec.Image, err, sanitizeToolOutput(string(out)))
	}
	// docker port 读回映射（格式 <hostPort> -> <containerPort>）。
	out, err := exec.CommandContext(ctx, "docker", "port", name, fmt.Sprint(spec.Port)).CombinedOutput()
	if err != nil {
		// 映射查询失败则清理刚起的容器，不留半成品。
		_, _ = exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput()
		return "", fmt.Errorf("docker port: %v: %s", err, sanitizeToolOutput(string(out)))
	}
	hostPort := parseHostPort(string(out))
	if hostPort == "" {
		_, _ = exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput()
		return "", fmt.Errorf("docker port: no mapping for port %d", spec.Port)
	}
	return "http://" + host + ":" + hostPort, nil
}

// parseHostPort 解析 docker port 输出行，纯函数便于测试。
// 实际输出是单字段 "<bind>:36186"（回环部署 127.0.0.1、全网卡 0.0.0.0，
// 后者可能再跟一行 [::]）；兼容 "host:port -> 80/tcp" 形式
// （docker ps 风格），两种都取首字段的端口。
func parseHostPort(out string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		addr := fields[0] // host:port
		if i := strings.LastIndex(addr, ":"); i >= 0 {
			port := addr[i+1:]
			if port != "" && port != "0" {
				return port
			}
		}
	}
	return ""
}

// Destroy 删除容器（幂等：No such container 视为成功）。
func (DockerDriver) Destroy(ctx context.Context, instanceID uint) error {
	out, err := exec.CommandContext(ctx, "docker", "rm", "-f", containerName(instanceID)).CombinedOutput()
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "no such container") {
			return nil
		}
		return fmt.Errorf("docker rm: %v: %s", err, sanitizeToolOutput(string(out)))
	}
	return nil
}

// Build 在 contextDir 执行 docker build -t tag（题目包 image: .）。
func (DockerDriver) Build(ctx context.Context, tag, contextDir string) error {
	out, err := exec.CommandContext(ctx, "docker", "build", "-t", tag, contextDir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker build %s: %v: %s", tag, err, sanitizeToolOutput(string(out)))
	}
	return nil
}

// sanitizeToolOutput 清洗透传给用户的外部工具（docker/yak 引擎）输出：
// 去 emoji/表情符号区段（docker registry 代理常带 🚫👀🔗 等字符）、去控制
// 字符，并折叠空白。真实报错文本保持原样，只是剥离装饰符号。
func sanitizeToolOutput(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			if r == '\n' || r == '\t' {
				space = true // 换行/制表折叠为单个空格，错误信息保持单行可读
			}
			continue
		case r >= 0x80 && unicode.Is(unicode.S, r) && !unicode.Is(unicode.Latin, r) && !unicode.Is(unicode.Han, r):
			space = true // 非 ASCII 的 Symbol 系区段（So/Sk/Sm/Sc，emoji 所在）；ASCII 符号（-> + = | 等）是报错原文的一部分，保留
			continue
		case r == ' ':
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// DockerAvailable 探测 docker CLI 与守护进程是否可用（EnvDriver=auto 的选择依据）。
// 超时 5s：Windows 上进程内首次 docker CLI 调用冷启动可超 2s。
func DockerAvailable(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(probeCtx, "docker", "info").Run() == nil
}

// dockerImageExists 探测本地镜像是否已构建（vulinbox 镜像状态展示）。
// 包级 var 缝隙：单测替换为受控实现，不依赖真实 docker。
var dockerImageExists = func(ctx context.Context, tag string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(probeCtx, "docker", "image", "inspect", tag).Run() == nil
}
