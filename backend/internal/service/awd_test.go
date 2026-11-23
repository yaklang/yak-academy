package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func intPtr(v int) *int { return &v }

// TestAwdGameAndServiceDefinitions AWD 管理端：比赛配置幂等创建/修改、服务定义
// 校验与 CRUD、非 AWD 竞赛拒绝、已部署服务不可删。
func TestAwdGameAndServiceDefinitions(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 配置赛", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create awd comp: %v", err)
	}
	ctf, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "CTF 对照赛", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create ctf comp: %v", err)
	}
	if _, err := awdSvc.Game(ctx, ctf.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("ctf game: want ErrValidation, got %v", err)
	}

	view, err := awdSvc.Game(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	if view.Game.Status != model.AwdGamePending || view.Game.CompetitorType != model.AwdCompetitorTeam || view.Game.RoundSeconds != 300 {
		t.Fatalf("default game: %+v", view.Game)
	}
	again, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil || again.ID != view.Game.ID {
		t.Fatalf("ensure idempotent: %+v err=%v", again, err)
	}

	updated, err := awdSvc.UpdateGame(ctx, comp.ID, AwdGameUpdate{
		CompetitorType: strPtr("user"), RoundSeconds: intPtr(60),
		ScoringConfig: json.RawMessage(`{"attack_weight": 1}`),
	})
	if err != nil {
		t.Fatalf("update game: %v", err)
	}
	if updated.Game.CompetitorType != model.AwdCompetitorUser || updated.Game.RoundSeconds != 60 || updated.Game.ScoringConfig != `{"attack_weight":1}` {
		t.Fatalf("updated game: %+v", updated.Game)
	}
	if _, err := awdSvc.UpdateGame(ctx, comp.ID, AwdGameUpdate{RoundSeconds: intPtr(5)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("short round: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.UpdateGame(ctx, comp.ID, AwdGameUpdate{ScoringConfig: json.RawMessage(`[]`)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("array scoring: want ErrValidation, got %v", err)
	}

	svc, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Name: "Web", Image: "awd/web:latest",
		Ports: []AwdPort{{Name: "http", Port: 80}},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if svc.BaseScore != 100 || svc.SlaWeight != 100 || svc.AttackPoints != 100 || svc.DefenseLoss != 50 || svc.CheckdownPenalty != 50 || svc.FlagLifetimeRounds != 1 {
		t.Fatalf("service defaults: %+v", svc)
	}
	if svc.PortsJSON != `[{"name":"http","port":80,"protocol":"tcp"}]` {
		t.Fatalf("ports json: %s", svc.PortsJSON)
	}
	if _, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Image: "awd/web:2", Ports: []AwdPort{{Name: "http", Port: 80}},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate slug: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "Bad Slug", Image: "x", Ports: []AwdPort{{Name: "http", Port: 80}},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad slug: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "noports", Image: "x",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("no ports: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "badscore", Image: "x", Ports: []AwdPort{{Name: "http", Port: 80}}, SlaWeight: intPtr(101),
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad score: want ErrValidation, got %v", err)
	}

	updatedSvc, err := awdSvc.UpdateService(ctx, comp.ID, svc.ID, AwdServiceInput{
		Name: "Web 2", BaseScore: intPtr(200), Ports: []AwdPort{{Name: "http", Port: 8080, Protocol: "tcp"}},
	})
	if err != nil {
		t.Fatalf("update service: %v", err)
	}
	if updatedSvc.Name != "Web 2" || updatedSvc.BaseScore != 200 || updatedSvc.PortsJSON != `[{"name":"http","port":8080,"protocol":"tcp"}]` {
		t.Fatalf("updated service: %+v", updatedSvc)
	}
	if _, err := awdSvc.UpdateService(ctx, comp.ID, svc.ID, AwdServiceInput{BaseScore: intPtr(-1)}); !errors.Is(err, ErrValidation) {
		t.Fatalf("negative score: want ErrValidation, got %v", err)
	}

	if err := e.gdb.Create(&model.AwdTeamService{
		GameID: updated.Game.ID, ServiceID: svc.ID,
		CompetitorType: model.AwdCompetitorUser, CompetitorID: 1, Status: "running",
	}).Error; err != nil {
		t.Fatalf("seed team service: %v", err)
	}
	if err := awdSvc.DeleteService(ctx, comp.ID, svc.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("delete deployed service: want ErrValidation, got %v", err)
	}
	if err := e.gdb.Where("service_id = ?", svc.ID).Delete(&model.AwdTeamService{}).Error; err != nil {
		t.Fatalf("cleanup team service: %v", err)
	}
	if err := awdSvc.DeleteService(ctx, comp.ID, svc.ID); err != nil {
		t.Fatalf("delete service: %v", err)
	}
}

func strPtr(v string) *string { return &v }
