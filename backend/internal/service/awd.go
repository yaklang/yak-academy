package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/pathutil"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// AWD 独立域服务：只依赖 AwdRepo + CompetitionRepo，不引用 CTF Challenge/
// CompetitionScore/JudgeService（AWD 与 CTF 独立）。
type AwdCompetitionService struct {
	awd     repo.AwdRepo
	comps   repo.CompetitionRepo
	builder ImageBuilder
	runtime AwdRuntime
	now     func() time.Time
}

func NewAwdCompetitionService(awd repo.AwdRepo, comps repo.CompetitionRepo) *AwdCompetitionService {
	return &AwdCompetitionService{awd: awd, comps: comps, now: time.Now}
}

// SetImageBuilder 注入服务包镜像构建器（docker / noop）。
func (s *AwdCompetitionService) SetImageBuilder(builder ImageBuilder) {
	s.builder = builder
}

// SetRuntime 注入 AWD 靶机运行时（docker / noop/fake）。
func (s *AwdCompetitionService) SetRuntime(runtime AwdRuntime) {
	s.runtime = runtime
}

// SetNow 测试注入时钟。
func (s *AwdCompetitionService) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// Start 开始比赛：仅 pending；竞赛必须在进行中；至少一个服务和一个参赛者。
func (s *AwdCompetitionService) Start(ctx context.Context, competitionID uint) (*AwdGameView, error) {
	comp, err := s.requireAwdCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if comp.Status(s.now()) != model.CompetitionRunning {
		return nil, fmt.Errorf("%w: competition must be running before starting AWD", ErrValidation)
	}
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGamePending {
		return nil, fmt.Errorf("%w: AWD game is not pending", ErrValidation)
	}
	services, err := s.awd.ListServices(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("%w: at least one AWD service is required", ErrValidation)
	}
	participants, _, err := s.comps.ListParticipants(ctx, competitionID, 0, 1)
	if err != nil {
		return nil, err
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("%w: at least one participant is required", ErrValidation)
	}
	now := s.now()
	if err := s.awd.UpdateGameSelective(ctx, game.ID, map[string]any{
		"status": model.AwdGameRunning, "started_at": now, "ended_at": nil, "current_round": 0,
	}); err != nil {
		return nil, err
	}
	return s.Game(ctx, competitionID)
}

// Pause 暂停比赛（running → paused）。
func (s *AwdCompetitionService) Pause(ctx context.Context, competitionID uint) (*AwdGameView, error) {
	return s.transition(ctx, competitionID, model.AwdGamePaused, model.AwdGameRunning)
}

// Resume 继续比赛（paused → running）。
func (s *AwdCompetitionService) Resume(ctx context.Context, competitionID uint) (*AwdGameView, error) {
	return s.transition(ctx, competitionID, model.AwdGameRunning, model.AwdGamePaused)
}

// End 结束比赛（running/paused → ended）。
func (s *AwdCompetitionService) End(ctx context.Context, competitionID uint) (*AwdGameView, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGameRunning && game.Status != model.AwdGamePaused {
		return nil, fmt.Errorf("%w: AWD game is not running or paused", ErrValidation)
	}
	if err := s.awd.UpdateGameSelective(ctx, game.ID, map[string]any{
		"status": model.AwdGameEnded, "ended_at": s.now(),
	}); err != nil {
		return nil, err
	}
	return s.Game(ctx, competitionID)
}

func (s *AwdCompetitionService) transition(ctx context.Context, competitionID uint, to, from model.AwdGameStatus) (*AwdGameView, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != from {
		return nil, fmt.Errorf("%w: AWD game is %s, expected %s", ErrValidation, game.Status, from)
	}
	if err := s.awd.UpdateGameSelective(ctx, game.ID, map[string]any{"status": to}); err != nil {
		return nil, err
	}
	return s.Game(ctx, competitionID)
}

// AwdGameView 管理端 AWD 配置视图：比赛 + 服务定义。
type AwdGameView struct {
	Game     model.AwdGame      `json:"game"`
	Services []model.AwdService `json:"services"`
}

// AwdGameUpdate 比赛配置修改（仅 pending 可改）。
type AwdGameUpdate struct {
	CompetitorType *string         `json:"competitor_type"`
	RoundSeconds   *int            `json:"round_seconds"`
	ScoringConfig  json.RawMessage `json:"scoring_config"`
}

// AwdPort 服务端口定义。
type AwdPort struct {
	Name     string `json:"name"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// AwdServiceInput 创建/修改服务定义的入参。Ptr 字段 nil = 不改（更新）/用默认（创建）。
type AwdServiceInput struct {
	Slug               string          `json:"slug"`
	Name               string          `json:"name"`
	Image              string          `json:"image"`
	Ports              []AwdPort       `json:"ports"`
	CheckerConfig      json.RawMessage `json:"checker_config"`
	BaseScore          *int            `json:"base_score"`
	SlaWeight          *int            `json:"sla_weight"`
	AttackPoints       *int            `json:"attack_points"`
	DefenseLoss        *int            `json:"defense_loss"`
	CheckdownPenalty   *int            `json:"checkdown_penalty"`
	FlagLifetimeRounds *int            `json:"flag_lifetime_rounds"`
}

var awdSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// MaxAwdServicePackageBytes 服务包 zip 上传上限（32MB；解包后另限 64MB）。
const MaxAwdServicePackageBytes = 32 << 20

const maxAwdServiceUncompressed = 64 << 20

const maxAwdServiceFiles = 2000

func (s *AwdCompetitionService) requireAwdCompetition(ctx context.Context, competitionID uint) (*model.Competition, error) {
	c, err := s.comps.FindByID(ctx, competitionID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if c.Format != model.CompetitionFormatAWD {
		return nil, fmt.Errorf("%w: competition format is %q, expected awd", ErrValidation, c.Format)
	}
	return c, nil
}

// EnsureGame 为 AWD 竞赛创建（幂等）比赛配置，默认 team / 300s / {}。
func (s *AwdCompetitionService) EnsureGame(ctx context.Context, competitionID uint) (*model.AwdGame, error) {
	if _, err := s.requireAwdCompetition(ctx, competitionID); err != nil {
		return nil, err
	}
	game, err := s.awd.FindGameByCompetition(ctx, competitionID)
	if err == nil {
		return game, nil
	}
	if !isNotFound(err) {
		return nil, err
	}
	game = &model.AwdGame{
		CompetitionID: competitionID, Status: model.AwdGamePending,
		CompetitorType: model.AwdCompetitorTeam, RoundSeconds: 300,
		ScoringConfig: "{}",
	}
	if err := s.awd.CreateGame(ctx, game); err != nil {
		// 并发创建：唯一索引冲突时回读。
		if again, findErr := s.awd.FindGameByCompetition(ctx, competitionID); findErr == nil {
			return again, nil
		}
		return nil, err
	}
	return game, nil
}

// Game 管理端读取 AWD 配置（不存在时自动创建）。
func (s *AwdCompetitionService) Game(ctx context.Context, competitionID uint) (*AwdGameView, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	services, err := s.awd.ListServices(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if services == nil {
		services = []model.AwdService{}
	}
	return &AwdGameView{Game: *game, Services: services}, nil
}

// UpdateGame 修改比赛配置：仅 pending；回合秒数 30..86400；计分参数 JSON 对象。
func (s *AwdCompetitionService) UpdateGame(ctx context.Context, competitionID uint, in AwdGameUpdate) (*AwdGameView, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGamePending {
		return nil, fmt.Errorf("%w: AWD game is not pending", ErrValidation)
	}
	if in.CompetitorType == nil && in.RoundSeconds == nil && in.ScoringConfig == nil {
		return nil, fmt.Errorf("%w: empty AWD game update", ErrValidation)
	}
	fields := map[string]any{}
	if in.CompetitorType != nil {
		t := model.AwdCompetitorType(strings.TrimSpace(*in.CompetitorType))
		if !t.Valid() {
			return nil, fmt.Errorf("%w: invalid competitor_type", ErrValidation)
		}
		fields["competitor_type"] = t
	}
	if in.RoundSeconds != nil {
		if *in.RoundSeconds < 30 || *in.RoundSeconds > 86400 {
			return nil, fmt.Errorf("%w: round_seconds must be in 30..86400", ErrValidation)
		}
		fields["round_seconds"] = *in.RoundSeconds
	}
	if in.ScoringConfig != nil {
		normalized, err := normalizeJSONObject(in.ScoringConfig, "scoring_config")
		if err != nil {
			return nil, err
		}
		fields["scoring_config"] = normalized
	}
	if err := s.awd.UpdateGameSelective(ctx, game.ID, fields); err != nil {
		return nil, err
	}
	return s.Game(ctx, competitionID)
}

// CreateService 新增服务定义（slug 竞赛内唯一；仅 pending）。
func (s *AwdCompetitionService) CreateService(ctx context.Context, competitionID uint, in AwdServiceInput) (*model.AwdService, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGamePending {
		return nil, fmt.Errorf("%w: AWD services can only be created before the game starts", ErrValidation)
	}
	slug := strings.TrimSpace(in.Slug)
	if !awdSlugRe.MatchString(slug) {
		return nil, fmt.Errorf("%w: invalid service slug (lowercase letters/digits/hyphen, 1-64)", ErrValidation)
	}
	if existing, err := s.awd.FindServiceBySlug(ctx, competitionID, slug); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: service slug already exists", ErrValidation)
	} else if err != nil && !isNotFound(err) {
		return nil, err
	}
	svc, err := buildAwdService(competitionID, slug, in)
	if err != nil {
		return nil, err
	}
	if err := s.awd.CreateService(ctx, svc); err != nil {
		return nil, err
	}
	return svc, nil
}

// UploadServicePackage 上传 AWD 服务包（zip）：
//
//	awd-service.json   服务定义（slug/name/ports/checker_config/计分参数）
//	Dockerfile         镜像构建文件（与 json 同目录）
//	checker/...        可选 checker 二进制/脚本，随镜像一起构建
//
// 解包到临时目录 → docker build -t yak-awd-<competition>-<slug> → 按 slug
// upsert 服务定义。仅 pending 可上传；构建失败不动 DB。
func (s *AwdCompetitionService) UploadServicePackage(ctx context.Context, competitionID uint, r io.Reader) (*model.AwdService, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGamePending {
		return nil, fmt.Errorf("%w: AWD services can only be uploaded before the game starts", ErrValidation)
	}
	if s.builder == nil {
		return nil, fmt.Errorf("%w: image builder is not configured", ErrValidation)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxAwdServicePackageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read package", ErrValidation)
	}
	if len(data) > MaxAwdServicePackageBytes {
		return nil, fmt.Errorf("%w: package exceeds %d bytes", ErrValidation, MaxAwdServicePackageBytes)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid zip package", ErrValidation)
	}
	if len(zr.File) == 0 || len(zr.File) > maxAwdServiceFiles {
		return nil, fmt.Errorf("%w: invalid package file count", ErrValidation)
	}
	var specEntry *zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, err := pathutil.CleanEntry(f.Name)
		if err != nil {
			return nil, fmt.Errorf("%w: unsafe package entry %q", ErrValidation, f.Name)
		}
		if pathutil.Base(name) == "awd-service.json" {
			if specEntry != nil {
				return nil, fmt.Errorf("%w: multiple awd-service.json", ErrValidation)
			}
			specEntry = f
		}
	}
	if specEntry == nil {
		return nil, fmt.Errorf("%w: awd-service.json is required", ErrValidation)
	}
	specName, _ := pathutil.CleanEntry(specEntry.Name)
	contextRel := pathutil.Dir(specName)
	specReader, err := specEntry.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open awd-service.json", ErrValidation)
	}
	specBytes, err := io.ReadAll(io.LimitReader(specReader, 1<<20))
	specReader.Close()
	if err != nil {
		return nil, fmt.Errorf("%w: read awd-service.json", ErrValidation)
	}
	var in AwdServiceInput
	if err := json.Unmarshal(specBytes, &in); err != nil {
		return nil, fmt.Errorf("%w: parse awd-service.json: %v", ErrValidation, err)
	}
	in.Slug = strings.TrimSpace(in.Slug)
	if !awdSlugRe.MatchString(in.Slug) {
		return nil, fmt.Errorf("%w: awd-service.json slug is invalid", ErrValidation)
	}
	hasDockerfile := false
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, err := pathutil.CleanEntry(f.Name)
		if err != nil {
			continue
		}
		if pathutil.Dir(name) == contextRel && pathutil.Base(name) == "Dockerfile" {
			hasDockerfile = true
			break
		}
	}
	if !hasDockerfile {
		return nil, fmt.Errorf("%w: Dockerfile is required next to awd-service.json", ErrValidation)
	}
	tmp, err := os.MkdirTemp("", "academy-awd-package-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := extractAwdPackage(zr, tmp); err != nil {
		return nil, err
	}
	contextDir := pathutil.Join(tmp, contextRel)
	tag := fmt.Sprintf("yak-awd-%d-%s", competitionID, in.Slug)
	if err := s.builder.Build(ctx, tag, contextDir); err != nil {
		return nil, err
	}
	in.Image = tag
	existing, err := s.awd.FindServiceBySlug(ctx, competitionID, in.Slug)
	if err == nil && existing != nil {
		return s.UpdateService(ctx, competitionID, existing.ID, in)
	}
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	return s.CreateService(ctx, competitionID, in)
}

func extractAwdPackage(zr *zip.Reader, dest string) error {
	var total uint64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		target, err := pathutil.SafeJoin(dest, f.Name)
		if err != nil {
			return fmt.Errorf("%w: unsafe package entry %q", ErrValidation, f.Name)
		}
		total += f.UncompressedSize64
		if total > maxAwdServiceUncompressed {
			return fmt.Errorf("%w: package uncompressed size exceeds limit", ErrValidation)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			return fmt.Errorf("%w: open package entry", ErrValidation)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(src, int64(f.UncompressedSize64)+1))
		closeErr := out.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
func (s *AwdCompetitionService) UpdateService(ctx context.Context, competitionID, serviceID uint, in AwdServiceInput) (*model.AwdService, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGamePending {
		return nil, fmt.Errorf("%w: AWD services can only be changed before the game starts", ErrValidation)
	}
	svc, err := s.awd.FindService(ctx, serviceID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if svc.CompetitionID != competitionID {
		return nil, ErrNotFound
	}
	if strings.TrimSpace(in.Slug) != "" && strings.TrimSpace(in.Slug) != svc.Slug {
		return nil, fmt.Errorf("%w: service slug is immutable", ErrValidation)
	}
	fields := map[string]any{}
	if strings.TrimSpace(in.Name) != "" {
		fields["name"] = strings.TrimSpace(in.Name)
	}
	if strings.TrimSpace(in.Image) != "" {
		fields["image"] = strings.TrimSpace(in.Image)
	}
	if in.Ports != nil {
		portsJSON, err := normalizePorts(in.Ports)
		if err != nil {
			return nil, err
		}
		fields["ports_json"] = portsJSON
	}
	if in.CheckerConfig != nil {
		checker, err := normalizeChecker(in.CheckerConfig)
		if err != nil {
			return nil, err
		}
		fields["checker_config"] = checker
	}
	scoreFields, err := scoringFields(in)
	if err != nil {
		return nil, err
	}
	for key, value := range scoreFields {
		fields[key] = value
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: empty AWD service update", ErrValidation)
	}
	if err := s.awd.UpdateServiceSelective(ctx, serviceID, fields); err != nil {
		return nil, err
	}
	return s.awd.FindService(ctx, serviceID)
}

// DeleteService 删除服务定义：仅 pending 且尚无靶机实例。
func (s *AwdCompetitionService) DeleteService(ctx context.Context, competitionID, serviceID uint) error {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return err
	}
	if game.Status != model.AwdGamePending {
		return fmt.Errorf("%w: AWD services can only be deleted before the game starts", ErrValidation)
	}
	svc, err := s.awd.FindService(ctx, serviceID)
	if err != nil {
		return mapRecordNotFound(err)
	}
	if svc.CompetitionID != competitionID {
		return ErrNotFound
	}
	n, err := s.awd.CountTeamServices(ctx, serviceID)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: service has deployed targets", ErrValidation)
	}
	return s.awd.DeleteService(ctx, serviceID)
}

// AwdDeployResult 靶机部署结果（部分失败时 Failed 非空）。
type AwdDeployResult struct {
	Network string   `json:"network"`
	Created int      `json:"created"`
	Reused  int      `json:"reused"`
	Failed  []string `json:"failed"`
}

// DeployTargets 为每个竞域单位 × 每个服务部署靶机（幂等：running 且已有容器
// 的跳过）。团队赛要求所有参赛者已编队；个人赛每个参赛者一个 competitor。
func (s *AwdCompetitionService) DeployTargets(ctx context.Context, competitionID uint) (*AwdDeployResult, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGameRunning {
		return nil, fmt.Errorf("%w: AWD game is not running", ErrValidation)
	}
	if s.runtime == nil {
		return nil, fmt.Errorf("%w: AWD runtime is not configured", ErrValidation)
	}
	services, err := s.awd.ListServices(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("%w: at least one AWD service is required", ErrValidation)
	}
	participants, _, err := s.comps.ListParticipants(ctx, competitionID, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("%w: at least one participant is required", ErrValidation)
	}
	competitors := make([]uint, 0, len(participants))
	seen := map[uint]bool{}
	for _, p := range participants {
		id := p.UserID
		if game.CompetitorType == model.AwdCompetitorTeam {
			if p.TeamID == 0 {
				return nil, fmt.Errorf("%w: team AWD requires every participant to join a team", ErrValidation)
			}
			id = p.TeamID
		}
		if !seen[id] {
			seen[id] = true
			competitors = append(competitors, id)
		}
	}
	network, err := s.runtime.EnsureNetwork(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	result := &AwdDeployResult{Network: network, Failed: []string{}}
	for _, svc := range services {
		for _, competitorID := range competitors {
			ts, err := s.awd.FindTeamServiceByKey(ctx, game.ID, svc.ID, game.CompetitorType, competitorID)
			if err != nil && !isNotFound(err) {
				return nil, err
			}
			if err == nil && ts.Status == "running" && ts.ContainerID != "" {
				result.Reused++
				continue
			}
			if ts == nil {
				ts = &model.AwdTeamService{
					GameID: game.ID, ServiceID: svc.ID,
					CompetitorType: game.CompetitorType, CompetitorID: competitorID,
					PortsJSON: svc.PortsJSON, Status: "pending",
				}
				if err := s.awd.CreateTeamService(ctx, ts); err != nil {
					return nil, err
				}
			} else if err := s.awd.UpdateTeamServiceSelective(ctx, ts.ID, map[string]any{
				"status": "pending", "ports_json": svc.PortsJSON,
			}); err != nil {
				return nil, err
			}
			target, err := s.runtime.CreateTarget(ctx, AwdTargetSpec{
				CompetitionID: competitionID, GameID: game.ID,
				ServiceID: svc.ID, ServiceSlug: svc.Slug, Image: svc.Image,
				CompetitorType: game.CompetitorType, CompetitorID: competitorID,
			})
			if err != nil {
				_ = s.awd.UpdateTeamServiceSelective(ctx, ts.ID, map[string]any{"status": "failed"})
				result.Failed = append(result.Failed, fmt.Sprintf("%s/%d: %v", svc.Slug, competitorID, err))
				continue
			}
			if err := s.awd.UpdateTeamServiceSelective(ctx, ts.ID, map[string]any{
				"container_id": target.ContainerID, "internal_host": target.InternalHost,
				"status": "running", "ports_json": svc.PortsJSON,
			}); err != nil {
				return nil, err
			}
			result.Created++
		}
	}
	if len(result.Failed) > 0 {
		return result, fmt.Errorf("awd deploy: %d target(s) failed: %s", len(result.Failed), strings.Join(result.Failed, "; "))
	}
	return result, nil
}

// AwdAttackValidation 攻击提交的校验结论（accepted=false 为业务拒绝，不返回 error）。
type AwdAttackValidation struct {
	Accepted            bool
	Reason              string
	FlagID              uint
	RoundID             uint
	VictimTeamServiceID uint
	VictimType          model.AwdCompetitorType
	VictimID            uint
}

// GenerateFlags 为一回合的每个竞域单位服务生成确定性 flag：
// value = HMAC-SHA256(round.Seed, "<teamServiceID>:<roundNumber>")，
// 同一回合重复播种得到相同值（repo 唯一索引 + OnConflict 保证幂等）。
func (s *AwdCompetitionService) GenerateFlags(ctx context.Context, roundID uint) ([]model.AwdFlag, error) {
	round, err := s.awd.FindRound(ctx, roundID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if strings.TrimSpace(round.Seed) == "" {
		return nil, fmt.Errorf("%w: round seed is empty", ErrValidation)
	}
	game, err := s.awd.FindGame(ctx, round.GameID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	teamServices, err := s.awd.ListTeamServicesByGame(ctx, game.ID)
	if err != nil {
		return nil, err
	}
	services, err := s.awd.ListServices(ctx, game.CompetitionID)
	if err != nil {
		return nil, err
	}
	lifetime := map[uint]int{}
	for _, svc := range services {
		lifetime[svc.ID] = svc.FlagLifetimeRounds
	}
	placedAt := round.StartsAt
	if placedAt.IsZero() {
		placedAt = s.now()
	}
	flags := make([]model.AwdFlag, 0, len(teamServices))
	for _, ts := range teamServices {
		value := deterministicAwdFlag(round.Seed, ts.ID, round.Number)
		life := lifetime[ts.ServiceID]
		if life < 1 {
			life = 1
		}
		flags = append(flags, model.AwdFlag{
			RoundID: round.ID, TeamServiceID: ts.ID,
			Value: value, ValueHash: awdFlagHash(value),
			ValidUntilRound: round.Number + life - 1, PlacedAt: placedAt,
		})
	}
	if err := s.awd.CreateFlags(ctx, flags); err != nil {
		return nil, err
	}
	return flags, nil
}

// ValidateAttack 校验攻击提交：flag 存在、未过期、非自攻、未被他人首杀。
// 业务拒绝返回 Accepted=false + Reason；数据库错误才返回 error。
func (s *AwdCompetitionService) ValidateAttack(ctx context.Context, gameID uint, attackerType model.AwdCompetitorType, attackerID uint, flagValue string) (*AwdAttackValidation, error) {
	value := strings.TrimSpace(flagValue)
	if value == "" {
		return &AwdAttackValidation{Reason: "empty flag"}, nil
	}
	if !attackerType.Valid() || attackerID == 0 {
		return &AwdAttackValidation{Reason: "invalid attacker"}, nil
	}
	flag, err := s.awd.FindFlagByHash(ctx, awdFlagHash(value))
	if err != nil {
		if isNotFound(err) {
			return &AwdAttackValidation{Reason: "flag not found"}, nil
		}
		return nil, err
	}
	ts, err := s.awd.FindTeamService(ctx, flag.TeamServiceID)
	if err != nil {
		if isNotFound(err) {
			return &AwdAttackValidation{Reason: "victim service not found"}, nil
		}
		return nil, err
	}
	game, err := s.awd.FindGame(ctx, gameID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if game.Status != model.AwdGameRunning {
		return &AwdAttackValidation{Reason: "game is not running"}, nil
	}
	round, err := s.awd.FindRound(ctx, flag.RoundID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	current := game.CurrentRound
	if current == 0 {
		current = round.Number
	}
	if round.Number > current {
		return &AwdAttackValidation{Reason: "flag is not active yet"}, nil
	}
	if flag.ValidUntilRound < current {
		return &AwdAttackValidation{Reason: "flag expired"}, nil
	}
	if ts.CompetitorType == attackerType && ts.CompetitorID == attackerID {
		return &AwdAttackValidation{Reason: "cannot attack your own service"}, nil
	}
	stolen, err := s.awd.HasAcceptedAttack(ctx, flag.ID)
	if err != nil {
		return nil, err
	}
	if stolen {
		return &AwdAttackValidation{Reason: "flag already captured"}, nil
	}
	return &AwdAttackValidation{
		Accepted: true, FlagID: flag.ID, RoundID: round.ID,
		VictimTeamServiceID: ts.ID, VictimType: ts.CompetitorType, VictimID: ts.CompetitorID,
	}, nil
}

func deterministicAwdFlag(seed string, teamServiceID uint, roundNumber int) string {
	mac := hmac.New(sha256.New, []byte(seed))
	fmt.Fprintf(mac, "%d:%d", teamServiceID, roundNumber)
	return "flag{" + hex.EncodeToString(mac.Sum(nil)[:16]) + "}"
}

func awdFlagHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func buildAwdService(competitionID uint, slug string, in AwdServiceInput) (*model.AwdService, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = slug
	}
	image := strings.TrimSpace(in.Image)
	if image == "" {
		return nil, fmt.Errorf("%w: image is required", ErrValidation)
	}
	portsJSON, err := normalizePorts(in.Ports)
	if err != nil {
		return nil, err
	}
	checker, err := normalizeChecker(in.CheckerConfig)
	if err != nil {
		return nil, err
	}
	svc := &model.AwdService{
		CompetitionID: competitionID, Slug: slug, Name: name, Image: image,
		PortsJSON: portsJSON, CheckerConfig: checker,
		BaseScore: 100, SlaWeight: 100, AttackPoints: 100, DefenseLoss: 50,
		CheckdownPenalty: 50, FlagLifetimeRounds: 1,
	}
	scoreFields, err := scoringFields(in)
	if err != nil {
		return nil, err
	}
	for key, value := range scoreFields {
		switch key {
		case "base_score":
			svc.BaseScore = value.(int)
		case "sla_weight":
			svc.SlaWeight = value.(int)
		case "attack_points":
			svc.AttackPoints = value.(int)
		case "defense_loss":
			svc.DefenseLoss = value.(int)
		case "checkdown_penalty":
			svc.CheckdownPenalty = value.(int)
		case "flag_lifetime_rounds":
			svc.FlagLifetimeRounds = value.(int)
		}
	}
	return svc, nil
}

func scoringFields(in AwdServiceInput) (map[string]any, error) {
	fields := map[string]any{}
	set := func(key string, v *int, min, max int) error {
		if v == nil {
			return nil
		}
		if *v < min || *v > max {
			return fmt.Errorf("%w: %s out of range", ErrValidation, key)
		}
		fields[key] = *v
		return nil
	}
	for _, err := range []error{
		set("base_score", in.BaseScore, 0, 1_000_000),
		set("sla_weight", in.SlaWeight, 0, 100),
		set("attack_points", in.AttackPoints, 0, 1_000_000),
		set("defense_loss", in.DefenseLoss, 0, 1_000_000),
		set("checkdown_penalty", in.CheckdownPenalty, 0, 1_000_000),
		set("flag_lifetime_rounds", in.FlagLifetimeRounds, 1, 20),
	} {
		if err != nil {
			return nil, err
		}
	}
	return fields, nil
}

func normalizePorts(ports []AwdPort) (string, error) {
	if len(ports) == 0 {
		return "", fmt.Errorf("%w: at least one service port is required", ErrValidation)
	}
	out := make([]AwdPort, 0, len(ports))
	seen := map[int]bool{}
	for _, p := range ports {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return "", fmt.Errorf("%w: port name is required", ErrValidation)
		}
		if p.Port < 1 || p.Port > 65535 {
			return "", fmt.Errorf("%w: port must be in 1..65535", ErrValidation)
		}
		if seen[p.Port] {
			return "", fmt.Errorf("%w: duplicate port %d", ErrValidation, p.Port)
		}
		seen[p.Port] = true
		proto := strings.ToLower(strings.TrimSpace(p.Protocol))
		if proto == "" {
			proto = "tcp"
		}
		if proto != "tcp" && proto != "udp" {
			return "", fmt.Errorf("%w: port protocol must be tcp or udp", ErrValidation)
		}
		out = append(out, AwdPort{Name: name, Port: p.Port, Protocol: proto})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func normalizeChecker(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "{}", nil
	}
	return normalizeJSONObject(raw, "checker_config")
}

func normalizeJSONObject(raw json.RawMessage, name string) (string, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return "", fmt.Errorf("%w: %s must be a JSON object", ErrValidation, name)
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
