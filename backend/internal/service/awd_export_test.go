package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func parseAwdCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("\xEF\xBB\xBF")) {
		t.Fatalf("csv missing UTF-8 BOM")
	}
	rows, err := csv.NewReader(bytes.NewReader(data[3:])).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	return rows
}

// TestAwdExports 最终排名/攻击/告警 CSV 导出包含完整审计字段与 UTF-8 BOM。
func TestAwdExports(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	awdSvc.SetUsers(repo.NewUserRepo(e.gdb))

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 导出赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	game, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).
		Update("competitor_type", model.AwdCompetitorUser).Error; err != nil {
		t.Fatalf("set individual: %v", err)
	}
	svc, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: 80}},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	alice := e.ensureUser(t, "awd-export-alice")
	bob := e.ensureUser(t, "awd-export-bob")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).
		Update("status", model.AwdGameRunning).Error; err != nil {
		t.Fatalf("start game: %v", err)
	}
	target := func(userID uint) *model.AwdTeamService {
		return &model.AwdTeamService{
			GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorUser,
			CompetitorID: userID, InternalHost: "host", PortsJSON: `[{"name":"http","port":80}]`, Status: "running",
		}
	}
	aliceTS, bobTS := target(alice.ID), target(bob.ID)
	for _, ts := range []*model.AwdTeamService{aliceTS, bobTS} {
		if err := e.gdb.Create(ts).Error; err != nil {
			t.Fatalf("seed team service: %v", err)
		}
	}
	round := &model.AwdRound{
		GameID: game.ID, Number: 3, Status: model.AwdRoundSettled,
		StartsAt: e.now, EndsAt: e.now.Add(5 * time.Minute), Seed: "export",
	}
	if err := e.gdb.Create(round).Error; err != nil {
		t.Fatalf("seed round: %v", err)
	}
	judged := e.now.Add(30 * time.Second)
	if err := e.gdb.Create(&model.AwdAttack{
		GameID: game.ID, RoundID: round.ID, AttackerType: model.AwdCompetitorUser,
		AttackerID: alice.ID, VictimTeamServiceID: bobTS.ID, Status: model.AwdAttackAccepted,
		SubmittedAt: e.now, JudgedAt: &judged,
	}).Error; err != nil {
		t.Fatalf("seed accepted attack: %v", err)
	}
	if err := e.gdb.Create(&model.AwdAttack{
		GameID: game.ID, RoundID: round.ID, AttackerType: model.AwdCompetitorUser,
		AttackerID: bob.ID, VictimTeamServiceID: aliceTS.ID, Status: model.AwdAttackRejected,
		Reason: "flag expired", SubmittedAt: e.now, JudgedAt: &judged,
	}).Error; err != nil {
		t.Fatalf("seed rejected attack: %v", err)
	}

	incident, err := awdSvc.CreateIncident(ctx, comp.ID, AwdIncidentCreate{
		CompetitorID: bob.ID, ServiceID: svc.ID, Kind: "manual", Detail: "现场判罚确认",
	})
	if err != nil {
		t.Fatalf("create incident: %v", err)
	}
	if incident.CompetitorType != model.AwdCompetitorUser || incident.CompetitionID != comp.ID {
		t.Fatalf("incident: %+v", incident)
	}

	final, filename, err := awdSvc.FinalRankingCSV(ctx, comp.ID)
	if err != nil {
		t.Fatalf("final csv: %v", err)
	}
	if filename != "competition-1-awd-final.csv" {
		t.Fatalf("final filename: %s", filename)
	}
	rows := parseAwdCSV(t, final)
	if len(rows) != 3 || rows[0][0] != "排名" || rows[0][9] != "最终分" {
		t.Fatalf("final rows: %+v", rows)
	}
	if rows[1][3] != alice.Username || rows[2][3] != bob.Username {
		t.Fatalf("final names: %+v", rows)
	}

	attacks, filename, err := awdSvc.AttacksCSV(ctx, comp.ID)
	if err != nil {
		t.Fatalf("attacks csv: %v", err)
	}
	if filename != "competition-1-awd-attacks.csv" {
		t.Fatalf("attacks filename: %s", filename)
	}
	rows = parseAwdCSV(t, attacks)
	if len(rows) != 3 || rows[0][0] != "提交时间" {
		t.Fatalf("attack rows: %+v", rows)
	}
	if rows[1][2] != "3" || rows[1][4] != alice.Username || rows[1][5] != "web" ||
		rows[1][6] != bob.Username || rows[1][8] != "accepted" {
		t.Fatalf("accepted attack row: %+v", rows[1])
	}
	if rows[2][8] != "rejected" || rows[2][9] != "flag expired" {
		t.Fatalf("rejected attack row: %+v", rows[2])
	}

	incidents, filename, err := awdSvc.IncidentsCSV(ctx, comp.ID)
	if err != nil {
		t.Fatalf("incidents csv: %v", err)
	}
	if filename != "competition-1-awd-incidents.csv" {
		t.Fatalf("incidents filename: %s", filename)
	}
	rows = parseAwdCSV(t, incidents)
	if len(rows) != 2 || rows[1][1] != "manual" || rows[1][3] != bob.Username ||
		rows[1][4] != "web" || rows[1][5] != "现场判罚确认" {
		t.Fatalf("incident rows: %+v", rows)
	}
}

// TestAwdCreateIncidentValidation 手工告警校验：类型/详情必填，服务必须属于本场。
func TestAwdCreateIncidentValidation(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 告警赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	if _, err := awdSvc.CreateIncident(ctx, comp.ID, AwdIncidentCreate{Kind: "Manual", Detail: "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("uppercase kind: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.CreateIncident(ctx, comp.ID, AwdIncidentCreate{Kind: "manual", Detail: "  "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty detail: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.CreateIncident(ctx, comp.ID, AwdIncidentCreate{
		Kind: "manual", Detail: strings.Repeat("x", 2001),
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("long detail: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.CreateIncident(ctx, comp.ID, AwdIncidentCreate{
		Kind: "manual", Detail: "x", ServiceID: 9999,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown service: want ErrNotFound, got %v", err)
	}
	incident, err := awdSvc.CreateIncident(ctx, comp.ID, AwdIncidentCreate{Kind: "manual", Detail: "竞域级告警"})
	if err != nil {
		t.Fatalf("create incident: %v", err)
	}
	if incident.CompetitorType != model.AwdCompetitorTeam || incident.ServiceID != 0 {
		t.Fatalf("default type: %+v", incident)
	}
}
