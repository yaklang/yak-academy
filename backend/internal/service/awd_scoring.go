package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// AWD 回合结算：SLA + 攻击 + 防守 + checkdown 四类得分事件，写入
// AwdScoreEvent（append-only 账本），AwdStanding 只由 AwdScoreDeriver 派生。
// 公式（权重来自 game.scoring_config，缺省 1）：
//   sla_delta   = round(base_score * sla_weight/100 * 成功检查/总检查 * sla_weight_cfg)
//   attack      = round(service.attack_points / 本轮该服务不同攻击者数 * attack_weight_cfg)
//   defense     = -round(service.defense_loss * defense_weight_cfg)（每服务每回合一次）
//   checkdown   = -round(service.checkdown_penalty * checkdown_weight_cfg)（每服务每回合一次）
// 总分下限 0；并列按 attack_score、sla_score、competitor_id 排序。

type awdScoringWeights struct {
	SLAWeight       *float64 `json:"sla_weight"`
	AttackWeight    *float64 `json:"attack_weight"`
	DefenseWeight   *float64 `json:"defense_weight"`
	CheckdownWeight *float64 `json:"checkdown_weight"`
}

func (w awdScoringWeights) sla() float64       { return awdWeightOrDefault(w.SLAWeight) }
func (w awdScoringWeights) attack() float64    { return awdWeightOrDefault(w.AttackWeight) }
func (w awdScoringWeights) defense() float64   { return awdWeightOrDefault(w.DefenseWeight) }
func (w awdScoringWeights) checkdown() float64 { return awdWeightOrDefault(w.CheckdownWeight) }

func awdWeightOrDefault(v *float64) float64 {
	if v == nil {
		return 1
	}
	if *v < 0 {
		return 0
	}
	return *v
}

func parseAwdScoringWeights(raw string) awdScoringWeights {
	var w awdScoringWeights
	if strings.TrimSpace(raw) == "" {
		return w
	}
	// 解析失败回退默认权重（配置在写入时已校验为对象）。
	_ = json.Unmarshal([]byte(raw), &w)
	return w
}

// AwdStandingView 排行榜条目（rank 由服务端计算；同分并列）。
type AwdStandingView struct {
	Rank           int                     `json:"rank"`
	CompetitorType model.AwdCompetitorType `json:"competitor_type"`
	CompetitorID   uint                    `json:"competitor_id"`
	Score          int                     `json:"score"`
	AttackScore    int                     `json:"attack_score"`
	DefenseLoss    int                     `json:"defense_loss"`
	SlaScore       int                     `json:"sla_score"`
	LastEventAt    *time.Time              `json:"last_event_at,omitempty"`
}

// AwdSettlementView 回合结算结果（events = 本次新增账本条数，重复结算为 0）。
type AwdSettlementView struct {
	RoundID     uint              `json:"round_id"`
	RoundNumber int               `json:"round_number"`
	Events      int64             `json:"events"`
	Standings   []AwdStandingView `json:"standings"`
}

// AwdScoreDeriver 账本派生器：唯一允许写 AwdStanding 的组件。
type AwdScoreDeriver struct {
	awd repo.AwdRepo
}

func NewAwdScoreDeriver(awd repo.AwdRepo) *AwdScoreDeriver {
	return &AwdScoreDeriver{awd: awd}
}

// Derive 全量重放某竞赛账本并重建排名（先删后插，幂等）。
// competitors 决定零分参赛者是否出现在榜单中。
func (d *AwdScoreDeriver) Derive(ctx context.Context, competitionID uint, competitorType model.AwdCompetitorType, competitors []uint, now time.Time) ([]model.AwdStanding, error) {
	events, err := d.awd.ListScoreEvents(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	rows := aggregateAwdStandings(competitionID, competitorType, competitors, events, now)
	if err := d.awd.ReplaceStandings(ctx, competitionID, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

type awdStandingAgg struct {
	score   int
	attack  int
	defense int
	sla     int
	last    time.Time
}

func aggregateAwdStandings(competitionID uint, competitorType model.AwdCompetitorType, competitors []uint, events []model.AwdScoreEvent, now time.Time) []model.AwdStanding {
	agg := make(map[string]*awdStandingAgg, len(competitors))
	for _, id := range competitors {
		agg[awdCompetitorKey(competitorType, id)] = &awdStandingAgg{}
	}
	for _, ev := range events {
		key := awdCompetitorKey(ev.CompetitorType, ev.CompetitorID)
		a, ok := agg[key]
		if !ok {
			a = &awdStandingAgg{}
			agg[key] = a
		}
		a.score += ev.Delta
		switch ev.Source {
		case "attack":
			a.attack += ev.Delta
		case "defense":
			a.defense += -ev.Delta
		case "sla":
			a.sla += ev.Delta
		}
		if ev.CreatedAt.After(a.last) {
			a.last = ev.CreatedAt
		}
	}
	rows := make([]model.AwdStanding, 0, len(competitors))
	for _, id := range competitors {
		a := agg[awdCompetitorKey(competitorType, id)]
		score := a.score
		if score < 0 {
			score = 0
		}
		var last *time.Time
		if !a.last.IsZero() {
			last = &a.last
		}
		rows = append(rows, model.AwdStanding{
			CompetitionID: competitionID, CompetitorType: competitorType, CompetitorID: id,
			Score: score, AttackScore: a.attack, DefenseLoss: a.defense, SlaScore: a.sla,
			LastEventAt: last, UpdatedAt: now,
		})
	}
	return rows
}

// SettleRound 结算一个回合：幂等（账本 idempotency_key 唯一）并可重复调用。
func (s *AwdCompetitionService) SettleRound(ctx context.Context, competitionID, roundID uint) (*AwdSettlementView, error) {
	if _, err := s.requireAwdCompetition(ctx, competitionID); err != nil {
		return nil, err
	}
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	round, err := s.awd.FindRound(ctx, roundID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if round.GameID != game.ID {
		return nil, ErrNotFound
	}
	if round.Status == model.AwdRoundPending {
		return nil, fmt.Errorf("%w: round has not started", ErrValidation)
	}
	teamServices, err := s.awd.ListTeamServicesByGame(ctx, game.ID)
	if err != nil {
		return nil, err
	}
	services, err := s.awd.ListServices(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	checks, err := s.awd.ListChecksByRound(ctx, roundID)
	if err != nil {
		return nil, err
	}
	attacks, err := s.awd.ListAcceptedAttacksByRound(ctx, roundID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	events := buildAwdScoreEvents(competitionID, game, round, teamServices, services, checks, attacks, now)
	created, err := s.awd.CreateScoreEvents(ctx, events)
	if err != nil {
		return nil, err
	}
	if round.Status != model.AwdRoundSettled {
		if err := s.awd.UpdateRoundSelective(ctx, round.ID, map[string]any{"status": model.AwdRoundSettled}); err != nil {
			return nil, err
		}
	}
	competitors, err := s.awdStandingCompetitors(ctx, competitionID, game)
	if err != nil {
		return nil, err
	}
	rows, err := s.deriver.Derive(ctx, competitionID, game.CompetitorType, competitors, now)
	if err != nil {
		return nil, err
	}
	return &AwdSettlementView{
		RoundID: round.ID, RoundNumber: round.Number, Events: created,
		Standings: buildAwdStandingViews(game.CompetitorType, competitors, rows),
	}, nil
}

// Standings 只读排行榜（未结算的参赛者显示 0 分）。
func (s *AwdCompetitionService) Standings(ctx context.Context, competitionID uint) ([]AwdStandingView, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	competitors, err := s.awdStandingCompetitors(ctx, competitionID, game)
	if err != nil {
		return nil, err
	}
	rows, err := s.awd.ListStandings(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	return buildAwdStandingViews(game.CompetitorType, competitors, rows), nil
}

func (s *AwdCompetitionService) awdStandingCompetitors(ctx context.Context, competitionID uint, game *model.AwdGame) ([]uint, error) {
	participants, _, err := s.comps.ListParticipants(ctx, competitionID, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	seen := make(map[uint]bool, len(participants))
	competitors := make([]uint, 0, len(participants))
	for _, p := range participants {
		id := p.UserID
		if game.CompetitorType == model.AwdCompetitorTeam {
			id = p.TeamID
		}
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		competitors = append(competitors, id)
	}
	return competitors, nil
}

func buildAwdStandingViews(competitorType model.AwdCompetitorType, competitors []uint, rows []model.AwdStanding) []AwdStandingView {
	byKey := make(map[string]model.AwdStanding, len(rows))
	for _, r := range rows {
		byKey[awdCompetitorKey(r.CompetitorType, r.CompetitorID)] = r
	}
	views := make([]AwdStandingView, 0, len(competitors))
	for _, id := range competitors {
		r := byKey[awdCompetitorKey(competitorType, id)]
		views = append(views, AwdStandingView{
			CompetitorType: competitorType, CompetitorID: id,
			Score: r.Score, AttackScore: r.AttackScore, DefenseLoss: r.DefenseLoss,
			SlaScore: r.SlaScore, LastEventAt: r.LastEventAt,
		})
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Score != views[j].Score {
			return views[i].Score > views[j].Score
		}
		if views[i].AttackScore != views[j].AttackScore {
			return views[i].AttackScore > views[j].AttackScore
		}
		if views[i].SlaScore != views[j].SlaScore {
			return views[i].SlaScore > views[j].SlaScore
		}
		return views[i].CompetitorID < views[j].CompetitorID
	})
	rank := 0
	for i := range views {
		if i == 0 || views[i].Score != views[i-1].Score ||
			views[i].AttackScore != views[i-1].AttackScore || views[i].SlaScore != views[i-1].SlaScore {
			rank = i + 1
		}
		views[i].Rank = rank
	}
	return views
}

func buildAwdScoreEvents(
	competitionID uint, game *model.AwdGame, round *model.AwdRound,
	teamServices []model.AwdTeamService, services []model.AwdService,
	checks []model.AwdCheck, attacks []model.AwdAttack, now time.Time,
) []model.AwdScoreEvent {
	weights := parseAwdScoringWeights(game.ScoringConfig)
	serviceByID := make(map[uint]model.AwdService, len(services))
	for _, svc := range services {
		serviceByID[svc.ID] = svc
	}
	events := make([]model.AwdScoreEvent, 0, len(teamServices)*2)

	// SLA：每个竞域单位服务的检查成功率。
	type slaAgg struct{ ok, total int }
	sla := make(map[uint]*slaAgg, len(teamServices))
	for _, c := range checks {
		switch c.Action {
		case "check", "put", "get":
		default:
			continue
		}
		a := sla[c.TeamServiceID]
		if a == nil {
			a = &slaAgg{}
			sla[c.TeamServiceID] = a
		}
		a.total++
		if c.Status == "ok" {
			a.ok++
		}
	}
	for _, ts := range teamServices {
		a := sla[ts.ID]
		svc, ok := serviceByID[ts.ServiceID]
		if a == nil || a.total == 0 || !ok {
			continue
		}
		delta := int(math.Round(float64(svc.BaseScore) * float64(svc.SlaWeight) / 100 *
			float64(a.ok) / float64(a.total) * weights.sla()))
		if delta == 0 {
			continue
		}
		events = append(events, model.AwdScoreEvent{
			CompetitionID: competitionID, GameID: game.ID, RoundID: round.ID,
			CompetitorType: ts.CompetitorType, CompetitorID: ts.CompetitorID,
			Source: "sla", Delta: delta,
			IdempotencyKey: fmt.Sprintf("sla:%d:%d", round.ID, ts.ID),
			Reason:         fmt.Sprintf("service %s sla %d/%d", svc.Slug, a.ok, a.total),
			CreatedAt:      now,
		})
	}

	// checkdown：CHECK 失败每服务每回合扣一次。
	checkdown := make(map[uint]bool, len(teamServices))
	for _, c := range checks {
		if c.Action == "check" && c.Status != "ok" {
			checkdown[c.TeamServiceID] = true
		}
	}
	for _, ts := range teamServices {
		if !checkdown[ts.ID] {
			continue
		}
		svc, ok := serviceByID[ts.ServiceID]
		if !ok {
			continue
		}
		delta := -int(math.Round(float64(svc.CheckdownPenalty) * weights.checkdown()))
		if delta == 0 {
			continue
		}
		events = append(events, model.AwdScoreEvent{
			CompetitionID: competitionID, GameID: game.ID, RoundID: round.ID,
			CompetitorType: ts.CompetitorType, CompetitorID: ts.CompetitorID,
			Source: "checkdown", Delta: delta,
			IdempotencyKey: fmt.Sprintf("checkdown:%d:%d", round.ID, ts.ID),
			Reason:         fmt.Sprintf("service %s checkdown", svc.Slug),
			CreatedAt:      now,
		})
	}

	// 攻击池：每个受害服务按本轮不同攻击者均分；受害方每服务每回合扣一次防守分。
	attackers := make(map[uint]map[string]bool, len(teamServices))
	victims := make(map[uint]bool, len(teamServices))
	for _, a := range attacks {
		key := awdCompetitorKey(a.AttackerType, a.AttackerID)
		if attackers[a.VictimTeamServiceID] == nil {
			attackers[a.VictimTeamServiceID] = map[string]bool{}
		}
		attackers[a.VictimTeamServiceID][key] = true
		victims[a.VictimTeamServiceID] = true
	}
	for _, ts := range teamServices {
		if !victims[ts.ID] {
			continue
		}
		svc, ok := serviceByID[ts.ServiceID]
		if !ok {
			continue
		}
		keys := make([]string, 0, len(attackers[ts.ID]))
		for key := range attackers[ts.ID] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			points := int(math.Round(float64(svc.AttackPoints) * weights.attack() / float64(len(keys))))
			for _, key := range keys {
				ctype, cid := parseAwdCompetitorKey(key)
				if points != 0 {
					events = append(events, model.AwdScoreEvent{
						CompetitionID: competitionID, GameID: game.ID, RoundID: round.ID,
						CompetitorType: ctype, CompetitorID: cid,
						Source: "attack", Delta: points,
						IdempotencyKey: fmt.Sprintf("attack:%d:%d:%s:%d", round.ID, ts.ID, ctype, cid),
						Reason:         fmt.Sprintf("service %s attack pool", svc.Slug),
						CreatedAt:      now,
					})
				}
			}
		}
		loss := -int(math.Round(float64(svc.DefenseLoss) * weights.defense()))
		if loss != 0 {
			events = append(events, model.AwdScoreEvent{
				CompetitionID: competitionID, GameID: game.ID, RoundID: round.ID,
				CompetitorType: ts.CompetitorType, CompetitorID: ts.CompetitorID,
				Source: "defense", Delta: loss,
				IdempotencyKey: fmt.Sprintf("defense:%d:%d", round.ID, ts.ID),
				Reason:         fmt.Sprintf("service %s defense loss", svc.Slug),
				CreatedAt:      now,
			})
		}
	}
	return events
}

func awdCompetitorKey(t model.AwdCompetitorType, id uint) string {
	return fmt.Sprintf("%s:%d", t, id)
}

func parseAwdCompetitorKey(key string) (model.AwdCompetitorType, uint) {
	idx := strings.IndexByte(key, ':')
	if idx < 0 {
		return "", 0
	}
	id, _ := strconv.ParseUint(key[idx+1:], 10, 64)
	return model.AwdCompetitorType(key[:idx]), uint(id)
}
