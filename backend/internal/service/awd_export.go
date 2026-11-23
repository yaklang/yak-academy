package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// AWD 赛后导出：最终排名、攻击审计、运营告警 CSV。输出带 UTF-8 BOM，
// Excel 可直接打开中文；时间统一 RFC3339，便于二次处理。
// CSV 只包含运营事实，不含 flag 值、攻击盒密码或 checker 原始凭据。

const maxAwdIncidentDetail = 2000

var awdIncidentKindRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// AwdIncidentCreate 管理端手工登记运营告警（如现场判罚、flag 泄漏确认）。
type AwdIncidentCreate struct {
	CompetitorType model.AwdCompetitorType `json:"competitor_type"`
	CompetitorID   uint                    `json:"competitor_id"`
	ServiceID      uint                    `json:"service_id"`
	Kind           string                  `json:"kind"`
	Detail         string                  `json:"detail"`
}

func awdCSVTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func awdCSVTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return awdCSVTime(*t)
}

func awdCSVCompetitorType(t model.AwdCompetitorType) string {
	if t == model.AwdCompetitorTeam {
		return "team"
	}
	return "user"
}

func awdCSVWrite(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FinalRankingCSV 最终排名导出：排名、竞域单位、AWD 分构成、战报分、最终分。
func (s *AwdCompetitionService) FinalRankingCSV(ctx context.Context, competitionID uint) ([]byte, string, error) {
	view, err := s.FinalRanking(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	standings, err := s.Standings(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	standingByKey := make(map[string]AwdStandingView, len(standings))
	for _, row := range standings {
		standingByKey[awdCompetitorKey(row.CompetitorType, row.CompetitorID)] = row
	}
	rows := [][]string{{
		"排名", "竞域单位类型", "竞域单位ID", "竞域单位",
		"AWD分", "攻击分", "防守扣分", "SLA分", "战报分", "最终分", "最后有效攻击",
	}}
	for _, item := range view.Items {
		standing := standingByKey[awdCompetitorKey(item.CompetitorType, item.CompetitorID)]
		rows = append(rows, []string{
			strconv.Itoa(item.Rank), awdCSVCompetitorType(item.CompetitorType),
			strconv.FormatUint(uint64(item.CompetitorID), 10), item.CompetitorName,
			strconv.Itoa(item.AwdScore), strconv.Itoa(standing.AttackScore),
			strconv.Itoa(standing.DefenseLoss), strconv.Itoa(standing.SlaScore),
			strconv.Itoa(item.ReportScore), fmt.Sprintf("%.2f", item.FinalScore),
			awdCSVTimePtr(item.LastAttackAt),
		})
	}
	data, err := awdCSVWrite(rows)
	if err != nil {
		return nil, "", err
	}
	return data, fmt.Sprintf("competition-%d-awd-final.csv", competitionID), nil
}

// AttacksCSV 攻击审计导出：全部提交（含被拒绝）与判定时间、原因。
func (s *AwdCompetitionService) AttacksCSV(ctx context.Context, competitionID uint) ([]byte, string, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	attacks, err := s.awd.ListAllAttacksByGame(ctx, game.ID)
	if err != nil {
		return nil, "", err
	}
	rounds, err := s.awd.ListRoundsByGame(ctx, game.ID)
	if err != nil {
		return nil, "", err
	}
	roundNumber := make(map[uint]int, len(rounds))
	for _, round := range rounds {
		roundNumber[round.ID] = round.Number
	}
	teamServices, err := s.awd.ListTeamServicesByGame(ctx, game.ID)
	if err != nil {
		return nil, "", err
	}
	services, err := s.awd.ListServices(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	serviceByID := make(map[uint]model.AwdService, len(services))
	for _, svc := range services {
		serviceByID[svc.ID] = svc
	}
	tsByID := make(map[uint]model.AwdTeamService, len(teamServices))
	for _, ts := range teamServices {
		tsByID[ts.ID] = ts
	}
	names, err := s.awdCompetitorNames(ctx, competitionID, game.CompetitorType)
	if err != nil {
		return nil, "", err
	}
	rows := [][]string{{
		"提交时间", "判定时间", "回合", "攻击方类型", "攻击方",
		"目标服务", "目标竞域单位", "目标靶机ID", "状态", "原因",
	}}
	for _, attack := range attacks {
		victimTS := tsByID[attack.VictimTeamServiceID]
		targetService := serviceByID[victimTS.ServiceID].Slug
		targetName := awdCompetitorName(names, victimTS.CompetitorID)
		rows = append(rows, []string{
			awdCSVTime(attack.SubmittedAt), awdCSVTimePtr(attack.JudgedAt),
			strconv.Itoa(roundNumber[attack.RoundID]),
			awdCSVCompetitorType(attack.AttackerType),
			awdCompetitorName(names, attack.AttackerID),
			targetService, targetName,
			strconv.FormatUint(uint64(attack.VictimTeamServiceID), 10),
			string(attack.Status), attack.Reason,
		})
	}
	data, err := awdCSVWrite(rows)
	if err != nil {
		return nil, "", err
	}
	return data, fmt.Sprintf("competition-%d-awd-attacks.csv", competitionID), nil
}

// IncidentsCSV 运营告警审计导出。
func (s *AwdCompetitionService) IncidentsCSV(ctx context.Context, competitionID uint) ([]byte, string, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	incidents, err := s.awd.ListAllIncidents(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	services, err := s.awd.ListServices(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	serviceByID := make(map[uint]model.AwdService, len(services))
	for _, svc := range services {
		serviceByID[svc.ID] = svc
	}
	names, err := s.awdCompetitorNames(ctx, competitionID, game.CompetitorType)
	if err != nil {
		return nil, "", err
	}
	rows := [][]string{{"时间", "类型", "竞域单位类型", "竞域单位", "服务", "详情"}}
	for _, incident := range incidents {
		competitor := ""
		if incident.CompetitorID != 0 {
			competitor = awdCompetitorName(names, incident.CompetitorID)
		}
		rows = append(rows, []string{
			awdCSVTime(incident.CreatedAt), incident.Kind,
			awdCSVCompetitorType(incident.CompetitorType),
			competitor,
			serviceByID[incident.ServiceID].Slug,
			incident.Detail,
		})
	}
	data, err := awdCSVWrite(rows)
	if err != nil {
		return nil, "", err
	}
	return data, fmt.Sprintf("competition-%d-awd-incidents.csv", competitionID), nil
}

// CreateIncident 管理端手工登记运营告警（append-only 审计事实）。
func (s *AwdCompetitionService) CreateIncident(ctx context.Context, competitionID uint, in AwdIncidentCreate) (*model.AwdIncident, error) {
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(in.Kind)
	if !awdIncidentKindRe.MatchString(kind) {
		return nil, fmt.Errorf("%w: invalid incident kind (lowercase letters/digits/underscore/hyphen, 1-32)", ErrValidation)
	}
	detail := strings.TrimSpace(in.Detail)
	if detail == "" {
		return nil, fmt.Errorf("%w: incident detail is required", ErrValidation)
	}
	if len(detail) > maxAwdIncidentDetail {
		return nil, fmt.Errorf("%w: incident detail exceeds 2000 characters", ErrValidation)
	}
	competitorType := in.CompetitorType
	if competitorType == "" {
		competitorType = game.CompetitorType
	}
	if !competitorType.Valid() {
		return nil, fmt.Errorf("%w: invalid competitor_type", ErrValidation)
	}
	if in.ServiceID != 0 {
		svc, err := s.awd.FindService(ctx, in.ServiceID)
		if err != nil {
			return nil, mapRecordNotFound(err)
		}
		if svc.CompetitionID != competitionID {
			return nil, ErrNotFound
		}
	}
	incident := &model.AwdIncident{
		CompetitionID: competitionID, CompetitorType: competitorType,
		CompetitorID: in.CompetitorID, ServiceID: in.ServiceID,
		Kind: kind, Detail: detail, CreatedAt: s.now(),
	}
	if err := s.awd.CreateIncident(ctx, incident); err != nil {
		return nil, err
	}
	return incident, nil
}
