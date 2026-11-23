package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 证书签发：赛后按来源竞赛排名/分数筛选接收人，生成唯一验证码与姓名快照。
// 排名口径由各赛制域提供（CertificateRankingSource），证书域不直接依赖
// CTF/AWD/Exam 服务；规则、证书与接收人快照只增不改，重复签发按标题拒绝。

const (
	maxCertificateTitle = 128
	maxCertificateLevel = 64
)

type CertificateService struct {
	comps   repo.CompetitionRepo
	users   repo.UserRepo
	events  repo.EventRepo
	sources map[model.CompetitionFormat]CertificateRankingSource
	now     func() time.Time
}

func NewCertificateService(comps repo.CompetitionRepo, users repo.UserRepo) *CertificateService {
	return &CertificateService{comps: comps, users: users, now: time.Now}
}

// SetRankingSources 注入各赛制榜单来源（main 组合根装配；未注入的赛制不可签发）。
func (s *CertificateService) SetRankingSources(ctf, awd, exam CertificateRankingSource) {
	s.sources = map[model.CompetitionFormat]CertificateRankingSource{
		model.CompetitionFormatCTF:  ctf,
		model.CompetitionFormatAWD:  awd,
		model.CompetitionFormatExam: exam,
	}
}

func (s *CertificateService) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// SetEvents 注入事件表（证书签发审计；未注入时只落证书表）。
func (s *CertificateService) SetEvents(events repo.EventRepo) {
	s.events = events
}

func (s *CertificateService) appendEvent(ctx context.Context, userID uint, payload map[string]any) {
	if s.events == nil {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	event := &model.Event{UserID: userID, Type: model.EventCompetitionCertificateIssued, Payload: string(raw)}
	if err := s.events.Append(ctx, event); err != nil {
		log.Printf("certificate: append event: %v", err)
	}
}

type CertificateRuleInput struct {
	Title     string                  `json:"title"`
	Level     string                  `json:"level"`
	Mode      model.QualificationMode `json:"mode"`
	Threshold int                     `json:"threshold"`
}

type CertificateCandidateView struct {
	RecipientKind string `json:"recipient_kind"`
	UserID        uint   `json:"user_id"`
	TeamID        uint   `json:"team_id"`
	Username      string `json:"username"`
	RecipientName string `json:"recipient_name"`
	Rank          int    `json:"rank"`
	Score         int    `json:"score"`
}

type CertificatePreview struct {
	CompetitionID   uint                       `json:"competition_id"`
	CompetitionName string                     `json:"competition_name"`
	Title           string                     `json:"title"`
	Level           string                     `json:"level"`
	Mode            model.QualificationMode    `json:"mode"`
	Threshold       int                        `json:"threshold"`
	TotalRanked     int                        `json:"total_ranked"`
	Candidates      []CertificateCandidateView `json:"candidates"`
}

type CertificateRuleSummary struct {
	model.CertificateRule
	IssuedCount int `json:"issued_count"`
}

type CertificateAdminView struct {
	CompetitionID uint                     `json:"competition_id"`
	Rules         []CertificateRuleSummary `json:"rules"`
	Certificates  []model.Certificate      `json:"certificates"`
	Total         int                      `json:"total"`
}

type CertificateIssueResult struct {
	Rule         model.CertificateRule `json:"rule"`
	Certificates []model.Certificate   `json:"certificates"`
	Preview      *CertificatePreview   `json:"preview"`
}

func (s *CertificateService) requireEndedCompetition(ctx context.Context, competitionID uint) (*model.Competition, error) {
	comp, err := s.comps.FindByID(ctx, competitionID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if comp.Status(s.now()) != model.CompetitionEnded {
		return nil, fmt.Errorf("%w: certificates can only be issued after the competition ends", ErrValidation)
	}
	return comp, nil
}

func validateCertificateRuleInput(in CertificateRuleInput) (CertificateRuleInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Level = strings.TrimSpace(in.Level)
	if in.Title == "" || len(in.Title) > maxCertificateTitle {
		return CertificateRuleInput{}, fmt.Errorf("%w: title is required and must be <= %d characters", ErrValidation, maxCertificateTitle)
	}
	if in.Level == "" || len(in.Level) > maxCertificateLevel {
		return CertificateRuleInput{}, fmt.Errorf("%w: level is required and must be <= %d characters", ErrValidation, maxCertificateLevel)
	}
	if !in.Mode.Valid() {
		return CertificateRuleInput{}, fmt.Errorf("%w: invalid certificate mode", ErrValidation)
	}
	switch in.Mode {
	case model.QualificationMinScore:
		if in.Threshold < 0 {
			return CertificateRuleInput{}, fmt.Errorf("%w: min_score cannot be negative", ErrValidation)
		}
	case model.QualificationTopN:
		if in.Threshold <= 0 {
			return CertificateRuleInput{}, fmt.Errorf("%w: top_n must be positive", ErrValidation)
		}
	case model.QualificationTopPercent:
		if in.Threshold <= 0 || in.Threshold > 100 {
			return CertificateRuleInput{}, fmt.Errorf("%w: top_percent must be in 1..100", ErrValidation)
		}
	}
	return in, nil
}

func (s *CertificateService) sourceFor(format model.CompetitionFormat) (CertificateRankingSource, error) {
	source := s.sources[format]
	if source == nil {
		return nil, fmt.Errorf("%w: certificate ranking source is not configured for %s", ErrValidation, format)
	}
	return source, nil
}

func (s *CertificateService) CertificatePreview(ctx context.Context, competitionID uint, in CertificateRuleInput) (*CertificatePreview, error) {
	input, err := validateCertificateRuleInput(in)
	if err != nil {
		return nil, err
	}
	comp, err := s.requireEndedCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	source, err := s.sourceFor(comp.Format)
	if err != nil {
		return nil, err
	}
	candidates, err := source.CertificateCandidates(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	profiles, err := s.comps.ListProfiles(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	profileByUser := make(map[uint]string, len(profiles))
	for _, profile := range profiles {
		if profile.RealName != "" {
			profileByUser[profile.UserID] = profile.RealName
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Rank != candidates[j].Rank {
			return candidates[i].Rank < candidates[j].Rank
		}
		return candidates[i].Name < candidates[j].Name
	})
	total := len(candidates)
	quota := 0
	if input.Mode == model.QualificationTopPercent {
		quota = (total*input.Threshold + 99) / 100
	}
	selected := make([]CertificateCandidateView, 0, total)
	for _, candidate := range candidates {
		if candidate.Rank <= 0 {
			continue
		}
		eligible := false
		switch input.Mode {
		case model.QualificationMinScore:
			eligible = candidate.Score >= input.Threshold
		case model.QualificationTopN:
			eligible = candidate.Score > 0 && candidate.Rank <= input.Threshold
		case model.QualificationTopPercent:
			eligible = candidate.Score > 0 && candidate.Rank <= quota
		}
		if !eligible {
			continue
		}
		name := candidate.Name
		if candidate.RecipientKind == "user" {
			if realName := profileByUser[candidate.UserID]; realName != "" {
				name = realName
			}
		}
		if name == "" && candidate.Username != "" {
			name = candidate.Username
		}
		selected = append(selected, CertificateCandidateView{
			RecipientKind: candidate.RecipientKind, UserID: candidate.UserID, TeamID: candidate.TeamID,
			Username: candidate.Username, RecipientName: name, Rank: candidate.Rank, Score: candidate.Score,
		})
	}
	return &CertificatePreview{
		CompetitionID: comp.ID, CompetitionName: comp.Name,
		Title: input.Title, Level: input.Level, Mode: input.Mode, Threshold: input.Threshold,
		TotalRanked: total, Candidates: selected,
	}, nil
}

// IssueCertificates 签发一批证书：规则 + 证书 + 唯一验证码单事务落库。
func (s *CertificateService) IssueCertificates(ctx context.Context, actorID, competitionID uint, in CertificateRuleInput) (*CertificateIssueResult, error) {
	preview, err := s.CertificatePreview(ctx, competitionID, in)
	if err != nil {
		return nil, err
	}
	if len(preview.Candidates) == 0 {
		return nil, fmt.Errorf("%w: no recipient matches the certificate rule", ErrValidation)
	}
	existing, err := s.comps.FindCertificateRuleByTitle(ctx, competitionID, preview.Title)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: a certificate rule with this title already exists", ErrValidation)
	}
	now := s.now().UTC()
	rule := &model.CertificateRule{
		CompetitionID: competitionID, Title: preview.Title, Level: preview.Level,
		Mode: preview.Mode, Threshold: preview.Threshold, CreatedBy: actorID,
		CreatedAt: now, IssuedAt: &now,
	}
	certificates := make([]model.Certificate, 0, len(preview.Candidates))
	seen := map[string]struct{}{}
	for _, candidate := range preview.Candidates {
		key := fmt.Sprintf("user:%d", candidate.UserID)
		if candidate.RecipientKind == "team" {
			key = fmt.Sprintf("team:%d", candidate.TeamID)
		}
		if _, done := seen[key]; done {
			continue
		}
		seen[key] = struct{}{}
		code, err := randomInviteCode()
		if err != nil {
			return nil, err
		}
		certificates = append(certificates, model.Certificate{
			RecipientKey: key, RecipientKind: candidate.RecipientKind,
			UserID: candidate.UserID, TeamID: candidate.TeamID,
			Username: candidate.Username, RecipientName: candidate.RecipientName,
			Rank: candidate.Rank, Score: candidate.Score, Level: preview.Level,
			Code: code, CreatedAt: now,
		})
	}
	if err := s.comps.CreateCertificateRuleWithCertificates(ctx, rule, certificates); err != nil {
		return nil, err
	}
	s.appendEvent(ctx, actorID, map[string]any{
		"competition_id": competitionID, "rule_id": rule.ID, "title": rule.Title,
		"level": rule.Level, "mode": rule.Mode, "threshold": rule.Threshold,
		"issued": len(certificates),
	})
	return &CertificateIssueResult{Rule: *rule, Certificates: certificates, Preview: preview}, nil
}

// AdminCertificates 管理端证书总览：已签发规则（含数量）与全部证书快照。
func (s *CertificateService) AdminCertificates(ctx context.Context, competitionID uint) (*CertificateAdminView, error) {
	if _, err := s.comps.FindByID(ctx, competitionID); err != nil {
		return nil, mapRecordNotFound(err)
	}
	rules, err := s.comps.ListCertificateRules(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	certificates, err := s.comps.ListCertificates(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	counts := make(map[uint]int, len(rules))
	for _, cert := range certificates {
		counts[cert.RuleID]++
	}
	ruleViews := make([]CertificateRuleSummary, 0, len(rules))
	for _, rule := range rules {
		ruleViews = append(ruleViews, CertificateRuleSummary{
			CertificateRule: rule, IssuedCount: counts[rule.ID],
		})
	}
	return &CertificateAdminView{
		CompetitionID: competitionID, Rules: ruleViews,
		Certificates: certificates, Total: len(certificates),
	}, nil
}
