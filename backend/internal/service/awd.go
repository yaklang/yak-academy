package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/pathutil"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/safego"
)

// AWD 独立域服务：只依赖 AwdRepo + CompetitionRepo，不引用 CTF Challenge/
// CompetitionScore/JudgeService（AWD 与 CTF 独立）。
type AwdCompetitionService struct {
	awd     repo.AwdRepo
	comps   repo.CompetitionRepo
	events  repo.EventRepo
	bus     *eventbus.Bus
	builder ImageBuilder
	runtime AwdRuntime
	secret  string
	deriver *AwdScoreDeriver
	now     func() time.Time
}

func NewAwdCompetitionService(awd repo.AwdRepo, comps repo.CompetitionRepo) *AwdCompetitionService {
	return &AwdCompetitionService{awd: awd, comps: comps, deriver: NewAwdScoreDeriver(awd), now: time.Now}
}

// SetImageBuilder 注入服务包镜像构建器（docker / noop）。
func (s *AwdCompetitionService) SetImageBuilder(builder ImageBuilder) {
	s.builder = builder
}

// SetRuntime 注入 AWD 靶机运行时（docker / noop/fake）。
func (s *AwdCompetitionService) SetRuntime(runtime AwdRuntime) {
	s.runtime = runtime
}

// SetSecret 注入攻击盒 SSH 密码加密密钥（主程序用 JWT secret 派生）。
func (s *AwdCompetitionService) SetSecret(secret string) {
	s.secret = secret
}

// SetEvents 注入事件表与总线（攻击提交/判定审计）。未注入时只落运营表。
func (s *AwdCompetitionService) SetEvents(events repo.EventRepo, bus *eventbus.Bus) {
	s.events = events
	s.bus = bus
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
	if _, err := NewAwdChecker(svc.CheckerConfig); err != nil {
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
		if _, err := NewAwdChecker(checker); err != nil {
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

func (s *AwdCompetitionService) awdCompetitors(ctx context.Context, competitionID uint, game *model.AwdGame) ([]uint, error) {
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
	return competitors, nil
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
	competitors, err := s.awdCompetitors(ctx, competitionID, game)
	if err != nil {
		return nil, err
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

// AwdAttackBoxDeployResult 攻击盒部署结果。
type AwdAttackBoxDeployResult struct {
	Created int      `json:"created"`
	Reused  int      `json:"reused"`
	Failed  []string `json:"failed"`
}

// DeployAttackBoxes 为每个竞域单位部署 SSH 攻击盒：随机密码加密落库，
// 运行时返回宿主/端口；重复部署对 running 的攻击盒幂等复用。
func (s *AwdCompetitionService) DeployAttackBoxes(ctx context.Context, competitionID uint) (*AwdAttackBoxDeployResult, error) {
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
	if s.secret == "" {
		return nil, fmt.Errorf("%w: AWD secret is not configured", ErrValidation)
	}
	competitors, err := s.awdCompetitors(ctx, competitionID, game)
	if err != nil {
		return nil, err
	}
	if _, err := s.runtime.EnsureNetwork(ctx, competitionID); err != nil {
		return nil, err
	}
	result := &AwdAttackBoxDeployResult{Failed: []string{}}
	for _, competitorID := range competitors {
		box, err := s.awd.FindAttackBoxByKey(ctx, competitionID, game.CompetitorType, competitorID)
		if err != nil && !isNotFound(err) {
			return nil, err
		}
		if err == nil && box.Status == "running" && box.ContainerID != "" && box.SSHPasswordEnc != "" {
			result.Reused++
			continue
		}
		password, err := randomAwdPassword()
		if err != nil {
			return nil, err
		}
		enc, err := encryptAwdSecret(s.secret, password)
		if err != nil {
			return nil, err
		}
		user := fmt.Sprintf("awd%d", competitorID)
		target, err := s.runtime.CreateAttackBox(ctx, AwdAttackBoxSpec{
			CompetitionID: competitionID, GameID: game.ID,
			CompetitorType: game.CompetitorType, CompetitorID: competitorID,
			SSHUser: user, SSHPassword: password,
		})
		if err != nil {
			if box != nil {
				_ = s.awd.UpdateAttackBoxSelective(ctx, box.ID, map[string]any{"status": "failed"})
			}
			result.Failed = append(result.Failed, fmt.Sprintf("%d: %v", competitorID, err))
			continue
		}
		fields := map[string]any{
			"container_id": target.ContainerID, "ssh_host": target.SSHHost,
			"ssh_port": target.SSHPort, "ssh_user": user,
			"ssh_password_enc": enc, "status": "running",
		}
		if box == nil {
			box = &model.AwdAttackBox{
				CompetitionID: competitionID, CompetitorType: game.CompetitorType,
				CompetitorID: competitorID, ContainerID: target.ContainerID,
				SSHHost: target.SSHHost, SSHPort: target.SSHPort, SSHUser: user,
				SSHPasswordEnc: enc, Status: "running",
			}
			if err := s.awd.CreateAttackBox(ctx, box); err != nil {
				return nil, err
			}
		} else if err := s.awd.UpdateAttackBoxSelective(ctx, box.ID, fields); err != nil {
			return nil, err
		}
		result.Created++
	}
	if len(result.Failed) > 0 {
		return result, fmt.Errorf("awd attack boxes: %d failed: %s", len(result.Failed), strings.Join(result.Failed, "; "))
	}
	return result, nil
}

// RunChecker 对单个靶机执行 CHECK/PUT/GET（运营态检查，RoundID=0）。
func (s *AwdCompetitionService) RunChecker(ctx context.Context, teamServiceID uint, action, flag string) (*model.AwdCheck, error) {
	return s.runChecker(ctx, 0, teamServiceID, action, flag)
}

// runChecker 回合执行/运营态共用：落 AwdCheck 明细并推进 last_check_at；
// 非 ok 同步记 AwdIncident。输出中的 flag 会被脱敏。
func (s *AwdCompetitionService) runChecker(ctx context.Context, roundID, teamServiceID uint, action, flag string) (*model.AwdCheck, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "check" && action != "put" && action != "get" {
		return nil, fmt.Errorf("%w: invalid checker action", ErrValidation)
	}
	if action != "check" && strings.TrimSpace(flag) == "" {
		return nil, fmt.Errorf("%w: flag is required for %s", ErrValidation, action)
	}
	ts, err := s.awd.FindTeamService(ctx, teamServiceID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	service, err := s.awd.FindService(ctx, ts.ServiceID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	checker, err := NewAwdChecker(service.CheckerConfig)
	if err != nil {
		return nil, err
	}
	ports, err := parseAwdPorts(ts.PortsJSON)
	if err != nil {
		return nil, err
	}
	result := checker.Run(ctx, action, AwdCheckTarget{
		Host: ts.InternalHost, ServiceSlug: service.Slug, Ports: ports, Flag: flag,
	})
	output := result.Output
	if flag != "" {
		output = strings.ReplaceAll(output, flag, "[flag]")
	}
	now := s.now()
	check := &model.AwdCheck{
		RoundID: roundID, TeamServiceID: ts.ID, Action: action, Status: result.Status,
		LatencyMS: int(result.Latency / time.Millisecond), Output: output, CreatedAt: now,
	}
	if err := s.awd.CreateCheck(ctx, check); err != nil {
		return nil, err
	}
	if err := s.awd.UpdateTeamServiceSelective(ctx, ts.ID, map[string]any{"last_check_at": now}); err != nil {
		return nil, err
	}
	if result.Status != AwdCheckOK {
		_ = s.awd.CreateIncident(ctx, &model.AwdIncident{
			CompetitionID: service.CompetitionID, CompetitorType: ts.CompetitorType,
			CompetitorID: ts.CompetitorID, ServiceID: ts.ServiceID,
			Kind: fmt.Sprintf("%s_%s", action, result.Status), Detail: output, CreatedAt: now,
		})
	}
	return check, nil
}

func parseAwdPorts(raw string) ([]AwdPort, error) {
	var ports []AwdPort
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("%w: service ports are empty", ErrValidation)
	}
	if err := json.Unmarshal([]byte(raw), &ports); err != nil || len(ports) == 0 {
		return nil, fmt.Errorf("%w: invalid service ports JSON", ErrValidation)
	}
	return ports, nil
}

// AwdRoundRunResult 回合创建/检查执行结果。
type AwdRoundRunResult struct {
	Round  model.AwdRound   `json:"round"`
	Flags  int              `json:"flags"`
	Checks []model.AwdCheck `json:"checks"`
	Failed []string         `json:"failed"`
}

// StartRound 开启下一回合：生成确定性 flags，PUT/GET/CHECK 全部靶机，
// 记录检查明细；回合保持 running，等待结算（下一 commit）。
func (s *AwdCompetitionService) StartRound(ctx context.Context, competitionID uint) (*AwdRoundRunResult, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGameRunning {
		return nil, fmt.Errorf("%w: AWD game is not running", ErrValidation)
	}
	if active, err := s.awd.FindActiveRound(ctx, game.ID); err == nil && active != nil {
		return nil, fmt.Errorf("%w: round %d is still active", ErrValidation, active.Number)
	} else if err != nil && !isNotFound(err) {
		return nil, err
	}
	teamServices, err := s.awd.ListTeamServicesByGame(ctx, game.ID)
	if err != nil {
		return nil, err
	}
	if len(teamServices) == 0 {
		return nil, fmt.Errorf("%w: no AWD targets deployed", ErrValidation)
	}
	now := s.now()
	round := &model.AwdRound{
		GameID: game.ID, Number: game.CurrentRound + 1, Status: model.AwdRoundRunning,
		StartsAt: now, EndsAt: now.Add(time.Duration(game.RoundSeconds) * time.Second),
		Seed: randomAwdSeed(), CreatedAt: now,
	}
	if err := s.awd.CreateRound(ctx, round); err != nil {
		return nil, err
	}
	if err := s.awd.UpdateGameSelective(ctx, game.ID, map[string]any{"current_round": round.Number}); err != nil {
		return nil, err
	}
	flags, err := s.GenerateFlags(ctx, round.ID)
	if err != nil {
		return nil, err
	}
	result, err := s.RunRoundChecks(ctx, round.ID)
	if err != nil {
		return nil, err
	}
	result.Round = *round
	result.Flags = len(flags)
	return result, nil
}

// RunRoundChecks 对回合内每个靶机执行 PUT → GET → CHECK，落明细；
// 单个靶机失败进入 Failed，不阻断其它靶机。
func (s *AwdCompetitionService) RunRoundChecks(ctx context.Context, roundID uint) (*AwdRoundRunResult, error) {
	round, err := s.awd.FindRound(ctx, roundID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	teamServices, err := s.awd.ListTeamServicesByGame(ctx, round.GameID)
	if err != nil {
		return nil, err
	}
	result := &AwdRoundRunResult{Round: *round, Checks: []model.AwdCheck{}, Failed: []string{}}
	for _, ts := range teamServices {
		if ts.Status != "running" {
			result.Failed = append(result.Failed, fmt.Sprintf("target %d is %s", ts.ID, ts.Status))
			continue
		}
		flag, err := s.awd.FindFlagByRoundTeamService(ctx, round.ID, ts.ID)
		if err != nil {
			result.Failed = append(result.Failed, fmt.Sprintf("target %d: flag missing", ts.ID))
			continue
		}
		for _, action := range []string{"put", "get", "check"} {
			flagValue := flag.Value
			if action == "check" {
				flagValue = ""
			}
			check, err := s.runChecker(ctx, round.ID, ts.ID, action, flagValue)
			if err != nil {
				result.Failed = append(result.Failed, fmt.Sprintf("target %d %s: %v", ts.ID, action, err))
				continue
			}
			result.Checks = append(result.Checks, *check)
		}
	}
	return result, nil
}

func randomAwdSeed() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("seed-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
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
	return s.validateAttackFlag(ctx, gameID, attackerType, attackerID, flag)
}

// validateAttackFlag 已定位 flag 后的校验（同步提交与异步 worker 共用）：
// 目标服务存在、比赛进行中、回合已激活未过期、非自攻、未被首杀。
func (s *AwdCompetitionService) validateAttackFlag(ctx context.Context, gameID uint, attackerType model.AwdCompetitorType, attackerID uint, flag *model.AwdFlag) (*AwdAttackValidation, error) {
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

// AwdAttackReceipt 攻击提交回执：pending = 已入队等待 worker 判定；
// rejected = 同步预校验拒绝（运营表留痕，便于赛后审计）。
type AwdAttackReceipt struct {
	ID                  uint                  `json:"id"`
	Status              model.AwdAttackStatus `json:"status"`
	Accepted            bool                  `json:"accepted"`
	Reason              string                `json:"reason,omitempty"`
	RoundID             uint                  `json:"round_id"`
	VictimTeamServiceID uint                  `json:"victim_team_service_id"`
	SubmittedAt         time.Time             `json:"submitted_at"`
	JudgedAt            *time.Time            `json:"judged_at,omitempty"`
}

func attackReceipt(attack *model.AwdAttack) *AwdAttackReceipt {
	return &AwdAttackReceipt{
		ID: attack.ID, Status: attack.Status,
		Accepted: attack.Status == model.AwdAttackAccepted,
		Reason:   attack.Reason, RoundID: attack.RoundID,
		VictimTeamServiceID: attack.VictimTeamServiceID,
		SubmittedAt:         attack.SubmittedAt, JudgedAt: attack.JudgedAt,
	}
}

// SubmitAttack 参赛者提交 flag：入场校验 + 竞域单位映射 + 预校验。
// 通过 → 写 pending 攻击（worker 异步判定）；拒绝 → 直接落 rejected 运营记录。
// 每个 flag 只允许首个 accepted，重复/自攻/过期均由 validateAttackFlag 判定。
func (s *AwdCompetitionService) SubmitAttack(ctx context.Context, competitionID, userID uint, flagValue string) (*AwdAttackReceipt, error) {
	value := strings.TrimSpace(flagValue)
	if value == "" {
		return nil, fmt.Errorf("%w: flag is required", ErrValidation)
	}
	if _, err := s.requireAwdCompetition(ctx, competitionID); err != nil {
		return nil, err
	}
	part, err := s.comps.FindParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	if part == nil || part.EnteredAt == nil {
		return nil, ErrNotFound
	}
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGameRunning {
		return nil, fmt.Errorf("%w: AWD game is not running", ErrValidation)
	}
	attackerID := userID
	if game.CompetitorType == model.AwdCompetitorTeam {
		if part.TeamID == 0 {
			return nil, fmt.Errorf("%w: team AWD requires every participant to join a team", ErrValidation)
		}
		attackerID = part.TeamID
	}
	validation, err := s.ValidateAttack(ctx, game.ID, game.CompetitorType, attackerID, value)
	if err != nil {
		return nil, err
	}
	attack := &model.AwdAttack{
		GameID: game.ID, RoundID: validation.RoundID,
		AttackerType: game.CompetitorType, AttackerID: attackerID,
		VictimTeamServiceID: validation.VictimTeamServiceID, FlagID: validation.FlagID,
		Status: model.AwdAttackPending, SubmittedAt: s.now(),
	}
	if !validation.Accepted {
		attack.Status = model.AwdAttackRejected
		attack.Reason = validation.Reason
		judgedAt := s.now()
		attack.JudgedAt = &judgedAt
	}
	if err := s.awd.CreateAttack(ctx, attack); err != nil {
		return nil, err
	}
	s.appendEvent(ctx, userID, model.EventAwdAttackSubmitted, map[string]any{
		"competition_id": competitionID, "attack_id": attack.ID,
		"round_id": attack.RoundID, "attacker_type": attack.AttackerType,
		"attacker_id": attack.AttackerID, "victim_team_service_id": attack.VictimTeamServiceID,
		"status": attack.Status, "reason": attack.Reason,
	})
	if attack.Status == model.AwdAttackRejected {
		s.appendEvent(ctx, userID, model.EventAwdAttackJudged, map[string]any{
			"competition_id": competitionID, "attack_id": attack.ID,
			"status": attack.Status, "reason": attack.Reason,
		})
	}
	return attackReceipt(attack), nil
}

// JudgePendingAttacks 处理 pending 攻击（持久队列 = AwdAttack 表）：逐条重校验，
// 条件更新保证并发/重放下只有一次 accepted。返回本轮判定条数。
func (s *AwdCompetitionService) JudgePendingAttacks(ctx context.Context) (int, error) {
	attacks, err := s.awd.ListPendingAttacks(ctx, 100)
	if err != nil {
		return 0, err
	}
	judged := 0
	for i := range attacks {
		ok, err := s.judgeAttack(ctx, &attacks[i])
		if err != nil {
			return judged, err
		}
		if ok {
			judged++
		}
	}
	return judged, nil
}

func (s *AwdCompetitionService) judgeAttack(ctx context.Context, attack *model.AwdAttack) (bool, error) {
	flag, err := s.awd.FindFlagByID(ctx, attack.FlagID)
	if err != nil {
		if isNotFound(err) {
			return s.rejectAttack(ctx, attack, "flag not found")
		}
		return false, err
	}
	validation, err := s.validateAttackFlag(ctx, attack.GameID, attack.AttackerType, attack.AttackerID, flag)
	if err != nil {
		return false, err
	}
	if !validation.Accepted {
		return s.rejectAttack(ctx, attack, validation.Reason)
	}
	judgedAt := s.now()
	rows, err := s.awd.UpdateAttackStatus(ctx, attack.ID, model.AwdAttackAccepted, "", judgedAt)
	if err != nil {
		return false, err
	}
	if rows == 0 {
		return false, nil // 已被其它 worker/重放判定，跳过。
	}
	attack.Status = model.AwdAttackAccepted
	attack.JudgedAt = &judgedAt
	s.appendEvent(ctx, 0, model.EventAwdAttackJudged, map[string]any{
		"competition_id": attack.GameID, "attack_id": attack.ID,
		"round_id": attack.RoundID, "attacker_type": attack.AttackerType,
		"attacker_id": attack.AttackerID, "victim_team_service_id": attack.VictimTeamServiceID,
		"status": attack.Status,
	})
	return true, nil
}

func (s *AwdCompetitionService) rejectAttack(ctx context.Context, attack *model.AwdAttack, reason string) (bool, error) {
	judgedAt := s.now()
	rows, err := s.awd.UpdateAttackStatus(ctx, attack.ID, model.AwdAttackRejected, reason, judgedAt)
	if err != nil {
		return false, err
	}
	if rows == 0 {
		return false, nil
	}
	attack.Status = model.AwdAttackRejected
	attack.Reason = reason
	attack.JudgedAt = &judgedAt
	s.appendEvent(ctx, 0, model.EventAwdAttackJudged, map[string]any{
		"competition_id": attack.GameID, "attack_id": attack.ID,
		"round_id": attack.RoundID, "attacker_type": attack.AttackerType,
		"attacker_id": attack.AttackerID, "victim_team_service_id": attack.VictimTeamServiceID,
		"status": attack.Status, "reason": reason,
	})
	return true, nil
}

// awdJudgeInterval 攻击判定轮询周期（持久队列兜底：重启滞留的 pending 攻击）。
const awdJudgeInterval = 2 * time.Second

// StartWorker 启动攻击判定 worker：先即时扫描一次（覆盖重启滞留），之后周期轮询。
func (s *AwdCompetitionService) StartWorker(ctx context.Context) {
	safego.Go("awd.judgePending", func() { s.pollPendingAttacks(ctx) })
}

func (s *AwdCompetitionService) pollPendingAttacks(ctx context.Context) {
	safego.Run("awd.judgePending", func() {
		if _, err := s.JudgePendingAttacks(ctx); err != nil {
			log.Printf("awd: judge pending attacks: %v", err)
		}
	})
	ticker := time.NewTicker(awdJudgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			safego.Run("awd.judgePending", func() {
				if _, err := s.JudgePendingAttacks(ctx); err != nil {
					log.Printf("awd: judge pending attacks: %v", err)
				}
			})
		}
	}
}

func (s *AwdCompetitionService) appendEvent(ctx context.Context, userID uint, t model.EventType, payload map[string]any) {
	if s.events == nil {
		return
	}
	raw, _ := json.Marshal(payload)
	ev := model.Event{UserID: userID, Type: t, Payload: string(raw)}
	if err := s.events.Append(ctx, &ev); err != nil {
		log.Printf("awd: append event %s: %v", t, err)
		return
	}
	if s.bus != nil {
		s.bus.Publish(ctx, ev)
	}
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
