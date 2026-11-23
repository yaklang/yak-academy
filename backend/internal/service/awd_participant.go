package service

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 参赛者视图：比赛概览、我的服务、目标目录、攻击盒凭据、排名、运营事件与
// 我的攻击记录。flag 值/checker 配置不返回；攻击盒密码仅解密给所属竞域单位。

type AwdParticipantCompetition struct {
	ID          uint                    `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Kind        model.CompetitionKind   `json:"kind"`
	Format      model.CompetitionFormat `json:"format"`
	Status      model.CompetitionStatus `json:"status"`
	StartTime   time.Time               `json:"start_time"`
	EndTime     time.Time               `json:"end_time"`
}

type AwdParticipantGame struct {
	ID             uint                    `json:"id"`
	Status         model.AwdGameStatus     `json:"status"`
	CompetitorType model.AwdCompetitorType `json:"competitor_type"`
	RoundSeconds   int                     `json:"round_seconds"`
	CurrentRound   int                     `json:"current_round"`
	StartedAt      *time.Time              `json:"started_at"`
	EndedAt        *time.Time              `json:"ended_at"`
}

type AwdRoundView struct {
	ID               uint                 `json:"id"`
	Number           int                  `json:"number"`
	Status           model.AwdRoundStatus `json:"status"`
	StartsAt         time.Time            `json:"starts_at"`
	EndsAt           time.Time            `json:"ends_at"`
	RemainingSeconds int                  `json:"remaining_seconds"`
}

type AwdCheckView struct {
	Action    string    `json:"action"`
	Status    string    `json:"status"`
	LatencyMS int       `json:"latency_ms"`
	Output    string    `json:"output"`
	CreatedAt time.Time `json:"created_at"`
}

type AwdParticipantService struct {
	TeamServiceID uint          `json:"team_service_id"`
	ServiceID     uint          `json:"service_id"`
	Slug          string        `json:"slug"`
	Name          string        `json:"name"`
	InternalHost  string        `json:"internal_host"`
	PortsJSON     string        `json:"ports_json"`
	Status        string        `json:"status"`
	LastCheckAt   *time.Time    `json:"last_check_at"`
	LastCheck     *AwdCheckView `json:"last_check,omitempty"`
}

type AwdParticipantTarget struct {
	TeamServiceID  uint                    `json:"team_service_id"`
	ServiceID      uint                    `json:"service_id"`
	ServiceSlug    string                  `json:"service_slug"`
	ServiceName    string                  `json:"service_name"`
	CompetitorType model.AwdCompetitorType `json:"competitor_type"`
	CompetitorID   uint                    `json:"competitor_id"`
	CompetitorName string                  `json:"competitor_name"`
	InternalHost   string                  `json:"internal_host"`
	PortsJSON      string                  `json:"ports_json"`
	Status         string                  `json:"status"`
}

type AwdAttackBoxView struct {
	SSHHost     string `json:"ssh_host"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user"`
	SSHPassword string `json:"ssh_password"`
	SSHCommand  string `json:"ssh_command"`
	Status      string `json:"status"`
}

type AwdParticipantAttack struct {
	ID                  uint                  `json:"id"`
	RoundID             uint                  `json:"round_id"`
	Status              model.AwdAttackStatus `json:"status"`
	Reason              string                `json:"reason,omitempty"`
	VictimTeamServiceID uint                  `json:"victim_team_service_id"`
	SubmittedAt         time.Time             `json:"submitted_at"`
	JudgedAt            *time.Time            `json:"judged_at,omitempty"`
}

type AwdParticipantView struct {
	Competition    AwdParticipantCompetition `json:"competition"`
	Game           AwdParticipantGame        `json:"game"`
	CompetitorType model.AwdCompetitorType   `json:"competitor_type"`
	CompetitorID   uint                      `json:"competitor_id"`
	CompetitorName string                    `json:"competitor_name"`
	TeamName       *string                   `json:"team_name"`
	Round          *AwdRoundView             `json:"round,omitempty"`
	MyServices     []AwdParticipantService   `json:"my_services"`
	Targets        []AwdParticipantTarget    `json:"targets"`
	AttackBox      *AwdAttackBoxView         `json:"attack_box,omitempty"`
	Standings      []AwdStandingView         `json:"standings"`
	Incidents      []model.AwdIncident       `json:"incidents"`
	Attacks        []AwdParticipantAttack    `json:"attacks"`
}

// ParticipantView 参赛者 AWD 首页：只返回本人可见的服务与攻击盒凭据，
// 目标目录只包含其他竞域单位的服务（内部地址用于攻击盒内访问）。
func (s *AwdCompetitionService) ParticipantView(ctx context.Context, competitionID, userID uint) (*AwdParticipantView, error) {
	comp, game, competitorID, err := s.requireAwdParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	teamServices, err := s.awd.ListTeamServicesByGame(ctx, game.ID)
	if err != nil {
		return nil, err
	}
	services, err := s.awd.ListServices(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	serviceByID := make(map[uint]model.AwdService, len(services))
	for _, svc := range services {
		serviceByID[svc.ID] = svc
	}
	names, err := s.awdCompetitorNames(ctx, competitionID, game.CompetitorType)
	if err != nil {
		return nil, err
	}
	myServices := make([]AwdParticipantService, 0, len(services))
	targets := make([]AwdParticipantTarget, 0, len(teamServices))
	for _, ts := range teamServices {
		svc := serviceByID[ts.ServiceID]
		if ts.CompetitorType == game.CompetitorType && ts.CompetitorID == competitorID {
			var lastCheck *AwdCheckView
			check, err := s.awd.FindLastCheckByTeamService(ctx, ts.ID)
			if err != nil {
				return nil, err
			}
			if check != nil {
				lastCheck = &AwdCheckView{
					Action: check.Action, Status: check.Status,
					LatencyMS: check.LatencyMS, Output: check.Output, CreatedAt: check.CreatedAt,
				}
			}
			myServices = append(myServices, AwdParticipantService{
				TeamServiceID: ts.ID, ServiceID: ts.ServiceID, Slug: svc.Slug, Name: svc.Name,
				InternalHost: ts.InternalHost, PortsJSON: ts.PortsJSON, Status: ts.Status,
				LastCheckAt: ts.LastCheckAt, LastCheck: lastCheck,
			})
			continue
		}
		targets = append(targets, AwdParticipantTarget{
			TeamServiceID: ts.ID, ServiceID: ts.ServiceID, ServiceSlug: svc.Slug,
			ServiceName: svc.Name, CompetitorType: ts.CompetitorType,
			CompetitorID: ts.CompetitorID, CompetitorName: awdCompetitorName(names, ts.CompetitorID),
			InternalHost: ts.InternalHost, PortsJSON: ts.PortsJSON, Status: ts.Status,
		})
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].ServiceSlug != targets[j].ServiceSlug {
			return targets[i].ServiceSlug < targets[j].ServiceSlug
		}
		if targets[i].CompetitorID != targets[j].CompetitorID {
			return targets[i].CompetitorID < targets[j].CompetitorID
		}
		return targets[i].TeamServiceID < targets[j].TeamServiceID
	})

	now := s.now()
	roundView, err := s.awdRoundView(ctx, game.ID, now)
	if err != nil {
		return nil, err
	}
	boxView, err := s.awdAttackBoxView(ctx, competitionID, game.CompetitorType, competitorID)
	if err != nil {
		return nil, err
	}
	standings, err := s.Standings(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	incidents, err := s.awd.ListIncidentsByCompetitor(ctx, competitionID, game.CompetitorType, competitorID, 50)
	if err != nil {
		return nil, err
	}
	if incidents == nil {
		incidents = []model.AwdIncident{}
	}
	attacks, err := s.awd.ListAttacksByAttacker(ctx, game.ID, game.CompetitorType, competitorID, 100)
	if err != nil {
		return nil, err
	}
	attackViews := make([]AwdParticipantAttack, 0, len(attacks))
	for _, a := range attacks {
		attackViews = append(attackViews, AwdParticipantAttack{
			ID: a.ID, RoundID: a.RoundID, Status: a.Status, Reason: a.Reason,
			VictimTeamServiceID: a.VictimTeamServiceID,
			SubmittedAt:         a.SubmittedAt, JudgedAt: a.JudgedAt,
		})
	}
	var teamName *string
	if game.CompetitorType == model.AwdCompetitorTeam {
		if name := names[competitorID]; name != "" {
			teamName = &name
		}
	}
	return &AwdParticipantView{
		Competition: AwdParticipantCompetition{
			ID: comp.ID, Name: comp.Name, Description: comp.Description,
			Kind: comp.Kind, Format: comp.Format, Status: comp.Status(now),
			StartTime: comp.StartTime, EndTime: comp.EndTime,
		},
		Game: AwdParticipantGame{
			ID: game.ID, Status: game.Status, CompetitorType: game.CompetitorType,
			RoundSeconds: game.RoundSeconds, CurrentRound: game.CurrentRound,
			StartedAt: game.StartedAt, EndedAt: game.EndedAt,
		},
		CompetitorType: game.CompetitorType, CompetitorID: competitorID,
		CompetitorName: awdCompetitorName(names, competitorID),
		TeamName:       teamName,
		Round:          roundView,
		MyServices:     myServices,
		Targets:        targets,
		AttackBox:      boxView,
		Standings:      standings,
		Incidents:      incidents,
		Attacks:        attackViews,
	}, nil
}

// ParticipantScoreboard 参赛者排行榜（需已入场；未结算参赛者 0 分）。
func (s *AwdCompetitionService) ParticipantScoreboard(ctx context.Context, competitionID, userID uint) ([]AwdStandingView, error) {
	if _, _, _, err := s.requireAwdParticipant(ctx, competitionID, userID); err != nil {
		return nil, err
	}
	return s.Standings(ctx, competitionID)
}

func (s *AwdCompetitionService) awdRoundView(ctx context.Context, gameID uint, now time.Time) (*AwdRoundView, error) {
	round, err := s.awd.FindActiveRound(ctx, gameID)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	remaining := 0
	if round.Status == model.AwdRoundRunning {
		remaining = int(round.EndsAt.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	return &AwdRoundView{
		ID: round.ID, Number: round.Number, Status: round.Status,
		StartsAt: round.StartsAt, EndsAt: round.EndsAt, RemainingSeconds: remaining,
	}, nil
}

func (s *AwdCompetitionService) awdAttackBoxView(ctx context.Context, competitionID uint, competitorType model.AwdCompetitorType, competitorID uint) (*AwdAttackBoxView, error) {
	if s.secret == "" {
		return nil, nil
	}
	box, err := s.awd.FindAttackBoxByKey(ctx, competitionID, competitorType, competitorID)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	password := ""
	if box.SSHPasswordEnc != "" {
		password, err = decryptAwdSecret(s.secret, box.SSHPasswordEnc)
		if err != nil {
			// 密钥轮换/密文损坏不应阻断整页：返回无密码视图并记录服务端日志。
			log.Printf("awd: decrypt attack box password (competition=%d competitor=%d): %v",
				competitionID, competitorID, err)
			password = ""
		}
	}
	command := ""
	if box.SSHHost != "" && box.SSHPort > 0 && box.SSHUser != "" {
		command = fmt.Sprintf("ssh -p %d %s@%s", box.SSHPort, box.SSHUser, box.SSHHost)
	}
	return &AwdAttackBoxView{
		SSHHost: box.SSHHost, SSHPort: box.SSHPort, SSHUser: box.SSHUser,
		SSHPassword: password, SSHCommand: command, Status: box.Status,
	}, nil
}

func (s *AwdCompetitionService) awdCompetitorNames(ctx context.Context, competitionID uint, competitorType model.AwdCompetitorType) (map[uint]string, error) {
	names := map[uint]string{}
	if competitorType == model.AwdCompetitorTeam {
		teams, err := s.comps.ListTeams(ctx, competitionID)
		if err != nil {
			return nil, err
		}
		for _, t := range teams {
			names[t.ID] = t.Name
		}
		return names, nil
	}
	if s.users == nil {
		return names, nil
	}
	participants, _, err := s.comps.ListParticipants(ctx, competitionID, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	userIDs := make([]uint, 0, len(participants))
	for _, p := range participants {
		userIDs = append(userIDs, p.UserID)
	}
	if len(userIDs) == 0 {
		return names, nil
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		names[u.ID] = u.Username
	}
	return names, nil
}

func awdCompetitorName(names map[uint]string, id uint) string {
	if name := names[id]; name != "" {
		return name
	}
	return fmt.Sprintf("#%d", id)
}
