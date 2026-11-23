package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// AWD 独立域服务：只依赖 AwdRepo + CompetitionRepo，不引用 CTF Challenge/
// CompetitionScore/JudgeService（AWD 与 CTF 独立）。
type AwdCompetitionService struct {
	awd   repo.AwdRepo
	comps repo.CompetitionRepo
	now   func() time.Time
}

func NewAwdCompetitionService(awd repo.AwdRepo, comps repo.CompetitionRepo) *AwdCompetitionService {
	return &AwdCompetitionService{awd: awd, comps: comps, now: time.Now}
}

// SetNow 测试注入时钟。
func (s *AwdCompetitionService) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
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

// UpdateService 修改服务定义（slug 不可变；仅 pending）。
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
