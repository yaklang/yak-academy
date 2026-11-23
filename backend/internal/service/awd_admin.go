package service

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// AWD 管理端控制台：配置 + 服务定义 + 部署目标 + 回合/检查/攻击/告警 + 排名。
// 攻击盒只返回连接元数据（密码不出库）；靶机内部地址仅管理端可见。

type AwdAdminAttackBox struct {
	ID             uint                    `json:"id"`
	CompetitorType model.AwdCompetitorType `json:"competitor_type"`
	CompetitorID   uint                    `json:"competitor_id"`
	SSHHost        string                  `json:"ssh_host"`
	SSHPort        int                     `json:"ssh_port"`
	SSHUser        string                  `json:"ssh_user"`
	Status         string                  `json:"status"`
}

type AwdAdminView struct {
	Competition CompetitionView        `json:"competition"`
	Game        model.AwdGame          `json:"game"`
	Services    []model.AwdService     `json:"services"`
	Rounds      []model.AwdRound       `json:"rounds"`
	Targets     []model.AwdTeamService `json:"targets"`
	AttackBoxes []AwdAdminAttackBox    `json:"attack_boxes"`
	Standings   []AwdStandingView      `json:"standings"`
	Incidents   []model.AwdIncident    `json:"incidents"`
	Attacks     []model.AwdAttack      `json:"attacks"`
	Checks      []model.AwdCheck       `json:"checks"`
}

func (s *AwdCompetitionService) AdminConsole(ctx context.Context, competitionID uint) (*AwdAdminView, error) {
	comp, err := s.requireAwdCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	services, err := s.awd.ListServices(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	rounds, err := s.awd.ListRoundsByGame(ctx, game.ID)
	if err != nil {
		return nil, err
	}
	targets, err := s.awd.ListTeamServicesByGame(ctx, game.ID)
	if err != nil {
		return nil, err
	}
	boxes, err := s.awd.ListAttackBoxes(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	standings, err := s.Standings(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	incidents, err := s.awd.ListIncidents(ctx, competitionID, 100)
	if err != nil {
		return nil, err
	}
	attacks, err := s.awd.ListAttacksByGame(ctx, game.ID, 100)
	if err != nil {
		return nil, err
	}
	checks, err := s.awd.ListChecksByGame(ctx, game.ID, 100)
	if err != nil {
		return nil, err
	}
	if services == nil {
		services = []model.AwdService{}
	}
	if rounds == nil {
		rounds = []model.AwdRound{}
	}
	if targets == nil {
		targets = []model.AwdTeamService{}
	}
	if incidents == nil {
		incidents = []model.AwdIncident{}
	}
	if attacks == nil {
		attacks = []model.AwdAttack{}
	}
	if checks == nil {
		checks = []model.AwdCheck{}
	}
	boxViews := make([]AwdAdminAttackBox, 0, len(boxes))
	for _, box := range boxes {
		boxViews = append(boxViews, AwdAdminAttackBox{
			ID: box.ID, CompetitorType: box.CompetitorType, CompetitorID: box.CompetitorID,
			SSHHost: box.SSHHost, SSHPort: box.SSHPort, SSHUser: box.SSHUser, Status: box.Status,
		})
	}
	return &AwdAdminView{
		Competition: toView(comp, s.now()),
		Game:        *game,
		Services:    services,
		Rounds:      rounds,
		Targets:     targets,
		AttackBoxes: boxViews,
		Standings:   standings,
		Incidents:   incidents,
		Attacks:     attacks,
		Checks:      checks,
	}, nil
}
