package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// TestAwdParticipantView 参赛者视图：只返回本人服务/攻击盒凭据与运营事件，
// 目标目录只含其他竞域单位的服务；未入场/非参赛者/非 AWD 竞赛拒绝。
func TestAwdParticipantView(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	awdSvc.SetSecret("participant-secret")
	awdSvc.SetUsers(repo.NewUserRepo(e.gdb))

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 参赛者视图", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	game, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).Update("competitor_type", model.AwdCompetitorUser).Error; err != nil {
		t.Fatalf("set individual competitor: %v", err)
	}
	svc, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Name: "Web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: 80}},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	alice := e.ensureUser(t, "awd-participant-alice")
	bob := e.ensureUser(t, "awd-participant-bob")
	carol := e.ensureUser(t, "awd-participant-carol")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	aliceTS := &model.AwdTeamService{
		GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorUser,
		CompetitorID: alice.ID, InternalHost: "awd-alice", PortsJSON: svc.PortsJSON, Status: "running",
	}
	bobTS := &model.AwdTeamService{
		GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorUser,
		CompetitorID: bob.ID, InternalHost: "awd-bob", PortsJSON: svc.PortsJSON, Status: "running",
	}
	for _, ts := range []*model.AwdTeamService{aliceTS, bobTS} {
		if err := e.gdb.Create(ts).Error; err != nil {
			t.Fatalf("seed team service: %v", err)
		}
	}
	round := &model.AwdRound{
		GameID: game.ID, Number: 1, Status: model.AwdRoundRunning,
		StartsAt: e.now, EndsAt: e.now.Add(5 * time.Minute), Seed: "seed-participant",
	}
	if err := e.gdb.Create(round).Error; err != nil {
		t.Fatalf("seed round: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).Updates(map[string]any{
		"status": model.AwdGameRunning, "current_round": 1,
	}).Error; err != nil {
		t.Fatalf("start game: %v", err)
	}
	if err := e.gdb.Create(&model.AwdCheck{
		RoundID: round.ID, TeamServiceID: aliceTS.ID, Action: "check", Status: "ok", LatencyMS: 12,
	}).Error; err != nil {
		t.Fatalf("seed check: %v", err)
	}
	for _, incident := range []*model.AwdIncident{
		{CompetitionID: comp.ID, CompetitorType: model.AwdCompetitorUser, CompetitorID: alice.ID, Kind: "check_fail", Detail: "alice incident"},
		{CompetitionID: comp.ID, CompetitorType: model.AwdCompetitorUser, CompetitorID: bob.ID, Kind: "check_fail", Detail: "bob incident"},
	} {
		if err := e.gdb.Create(incident).Error; err != nil {
			t.Fatalf("seed incident: %v", err)
		}
	}
	aliceAttack := &model.AwdAttack{
		GameID: game.ID, RoundID: round.ID, AttackerType: model.AwdCompetitorUser,
		AttackerID: alice.ID, VictimTeamServiceID: bobTS.ID,
		Status: model.AwdAttackAccepted, SubmittedAt: e.now, JudgedAt: &e.now,
	}
	if err := e.gdb.Create(aliceAttack).Error; err != nil {
		t.Fatalf("seed alice attack: %v", err)
	}
	if err := e.gdb.Create(&model.AwdAttack{
		GameID: game.ID, RoundID: round.ID, AttackerType: model.AwdCompetitorUser,
		AttackerID: bob.ID, VictimTeamServiceID: aliceTS.ID,
		Status: model.AwdAttackRejected, Reason: "flag expired", SubmittedAt: e.now,
	}).Error; err != nil {
		t.Fatalf("seed bob attack: %v", err)
	}
	enc, err := encryptAwdSecret("participant-secret", "s3cret")
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	if err := e.gdb.Create(&model.AwdAttackBox{
		CompetitionID: comp.ID, CompetitorType: model.AwdCompetitorUser, CompetitorID: alice.ID,
		ContainerID: "box-alice", SSHHost: "ssh.test", SSHPort: 2222, SSHUser: "awd1",
		SSHPasswordEnc: enc, Status: "running",
	}).Error; err != nil {
		t.Fatalf("seed attack box: %v", err)
	}

	view, err := awdSvc.ParticipantView(ctx, comp.ID, alice.ID)
	if err != nil {
		t.Fatalf("participant view: %v", err)
	}
	if view.Game.Status != model.AwdGameRunning || view.CompetitorID != alice.ID ||
		view.CompetitorName != alice.Username || view.CompetitorType != model.AwdCompetitorUser {
		t.Fatalf("participant identity: %+v", view)
	}
	if view.Round == nil || view.Round.Number != 1 || view.Round.RemainingSeconds <= 0 || view.Round.RemainingSeconds > 300 {
		t.Fatalf("round view: %+v", view.Round)
	}
	if len(view.MyServices) != 1 || view.MyServices[0].Slug != "web" || view.MyServices[0].InternalHost != "awd-alice" {
		t.Fatalf("my services: %+v", view.MyServices)
	}
	if view.MyServices[0].LastCheck == nil || view.MyServices[0].LastCheck.Status != "ok" ||
		view.MyServices[0].LastCheck.LatencyMS != 12 {
		t.Fatalf("last check: %+v", view.MyServices[0].LastCheck)
	}
	if len(view.Targets) != 1 || view.Targets[0].CompetitorID != bob.ID ||
		view.Targets[0].CompetitorName != bob.Username || view.Targets[0].InternalHost != "awd-bob" {
		t.Fatalf("targets: %+v", view.Targets)
	}
	if view.AttackBox == nil || view.AttackBox.SSHHost != "ssh.test" || view.AttackBox.SSHPort != 2222 ||
		view.AttackBox.SSHPassword != "s3cret" || view.AttackBox.SSHCommand != "ssh -p 2222 awd1@ssh.test" {
		t.Fatalf("attack box: %+v", view.AttackBox)
	}
	if len(view.Standings) != 2 {
		t.Fatalf("standings: %+v", view.Standings)
	}
	for _, row := range view.Standings {
		if row.Score != 0 || row.Rank == 0 {
			t.Fatalf("zero standing: %+v", row)
		}
	}
	if len(view.Incidents) != 1 || view.Incidents[0].Detail != "alice incident" {
		t.Fatalf("incidents should be own only: %+v", view.Incidents)
	}
	if len(view.Attacks) != 1 || view.Attacks[0].ID != aliceAttack.ID || view.Attacks[0].Status != model.AwdAttackAccepted {
		t.Fatalf("attacks should be own only: %+v", view.Attacks)
	}

	board, err := awdSvc.ParticipantScoreboard(ctx, comp.ID, alice.ID)
	if err != nil || len(board) != 2 {
		t.Fatalf("scoreboard: %+v err=%v", board, err)
	}
	if _, err := awdSvc.ParticipantView(ctx, comp.ID, carol.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non participant view: want ErrNotFound, got %v", err)
	}
	if _, err := awdSvc.ParticipantScoreboard(ctx, comp.ID, carol.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non participant scoreboard: want ErrNotFound, got %v", err)
	}

	ctf, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "CTF 对照", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create ctf comp: %v", err)
	}
	if _, err := awdSvc.ParticipantView(ctx, ctf.ID, alice.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("ctf participant view: want ErrValidation, got %v", err)
	}
}
