package service

import (
	"archive/zip"
	"bytes"
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

func awdZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// TestAwdServicePackageUpload 服务包：解析 awd-service.json + Dockerfile，
// 构建镜像后按 slug upsert；重复上传更新同一条记录；缺 Dockerfile/坏 zip/
// 已开赛均拒绝。
func TestAwdServicePackageUpload(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	builder := &fakeImageBuilder{}
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	awdSvc.SetImageBuilder(builder)

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 服务包赛", Start: e.now.Add(time.Hour), End: e.now.Add(3 * time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	spec := `{"slug":"web","name":"Web","ports":[{"name":"http","port":80}],"checker_config":{"type":"http","path":"/"},"base_score":150}`
	pkg := awdZip(t, map[string]string{
		"awd-service.json": spec,
		"Dockerfile":       "FROM alpine:3.20\nCMD [\"sleep\",\"infinity\"]\n",
	})
	svc, err := awdSvc.UploadServicePackage(ctx, comp.ID, bytes.NewReader(pkg))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	wantTag := "yak-awd-" + itoa(comp.ID) + "-web"
	if svc.Image != wantTag || svc.BaseScore != 150 || svc.Name != "Web" {
		t.Fatalf("uploaded service: %+v", svc)
	}
	if len(builder.tags) != 1 || builder.tags[0] != wantTag {
		t.Fatalf("builder tags: %+v", builder.tags)
	}
	if len(builder.dirs) != 1 || builder.dirs[0] == "" {
		t.Fatalf("builder context dirs: %+v", builder.dirs)
	}

	spec2 := `{"slug":"web","name":"Web 2","ports":[{"name":"http","port":8080}],"base_score":200}`
	pkg2 := awdZip(t, map[string]string{
		"awd-service.json": spec2,
		"Dockerfile":       "FROM alpine:3.20\n",
	})
	updated, err := awdSvc.UploadServicePackage(ctx, comp.ID, bytes.NewReader(pkg2))
	if err != nil {
		t.Fatalf("re-upload: %v", err)
	}
	if updated.ID != svc.ID || updated.BaseScore != 200 || updated.Name != "Web 2" {
		t.Fatalf("re-upload should update same service: %+v", updated)
	}
	list, err := awdSvc.Game(ctx, comp.ID)
	if err != nil || len(list.Services) != 1 {
		t.Fatalf("service count after upsert: %+v err=%v", list, err)
	}

	if _, err := awdSvc.UploadServicePackage(ctx, comp.ID, bytes.NewReader(awdZip(t, map[string]string{
		"awd-service.json": spec,
	}))); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing dockerfile: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.UploadServicePackage(ctx, comp.ID, bytes.NewReader([]byte("not zip"))); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad zip: want ErrValidation, got %v", err)
	}

	if err := e.gdb.Model(&model.AwdGame{}).Where("competition_id = ?", comp.ID).Update("status", model.AwdGameRunning).Error; err != nil {
		t.Fatalf("start game: %v", err)
	}
	if _, err := awdSvc.UploadServicePackage(ctx, comp.ID, bytes.NewReader(pkg)); !errors.Is(err, ErrValidation) {
		t.Fatalf("running game package: want ErrValidation, got %v", err)
	}
}

// TestAwdGameLifecycle 比赛生命周期：pending→running→paused→running→ended；
// 非法迁移拒绝；开赛要求竞赛进行中 + 至少一个服务/参赛者。
func TestAwdGameLifecycle(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 生命周期赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	if _, err := awdSvc.Start(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("start without service: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: 80}},
	}); err != nil {
		t.Fatalf("create service: %v", err)
	}
	if _, err := awdSvc.Start(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("start without participant: want ErrValidation, got %v", err)
	}
	alice := e.ensureUser(t, "awd-lifecycle")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{alice.Username}}); err != nil {
		t.Fatalf("import participant: %v", err)
	}
	if _, err := awdSvc.Start(ctx, comp.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	running, err := awdSvc.Game(ctx, comp.ID)
	if err != nil || running.Game.Status != model.AwdGameRunning || running.Game.StartedAt == nil {
		t.Fatalf("running game: %+v err=%v", running, err)
	}
	if _, err := awdSvc.Start(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("double start: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.Pause(ctx, comp.ID); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := awdSvc.Pause(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("double pause: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.Resume(ctx, comp.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err := awdSvc.Resume(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("double resume: want ErrValidation, got %v", err)
	}
	ended, err := awdSvc.End(ctx, comp.ID)
	if err != nil || ended.Game.Status != model.AwdGameEnded || ended.Game.EndedAt == nil {
		t.Fatalf("end: %+v err=%v", ended, err)
	}
	if _, err := awdSvc.Resume(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("resume ended: want ErrValidation, got %v", err)
	}

	scheduled, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 未开赛", Start: e.now.Add(time.Hour), End: e.now.Add(2 * time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create scheduled: %v", err)
	}
	if _, err := awdSvc.CreateService(ctx, scheduled.ID, AwdServiceInput{
		Slug: "web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: 80}},
	}); err != nil {
		t.Fatalf("create scheduled service: %v", err)
	}
	if _, err := e.comps.ImportParticipants(ctx, scheduled.ID, ParticipantImportRequest{Individuals: []string{alice.Username}}); err != nil {
		t.Fatalf("import scheduled participant: %v", err)
	}
	if _, err := awdSvc.Start(ctx, scheduled.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("start before competition: want ErrValidation, got %v", err)
	}
}

// TestAwdFlagsAndAttackValidation 回合 flag 确定性生成 + 攻击校验：
// 非自攻/未过期/未首杀接受；自攻、过期、重复首杀拒绝。
func TestAwdFlagsAndAttackValidation(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdRepo := repo.NewAwdRepo(e.gdb)
	awdSvc := NewAwdCompetitionService(awdRepo, repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD flag 赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	game, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	svc, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: 80}},
		FlagLifetimeRounds: intPtr(2),
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	teamA := &model.AwdTeamService{GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorTeam, CompetitorID: 101, Status: "running"}
	teamB := &model.AwdTeamService{GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorTeam, CompetitorID: 102, Status: "running"}
	if err := e.gdb.Create(teamA).Error; err != nil {
		t.Fatalf("seed team A: %v", err)
	}
	if err := e.gdb.Create(teamB).Error; err != nil {
		t.Fatalf("seed team B: %v", err)
	}
	round := &model.AwdRound{GameID: game.ID, Number: 1, Status: model.AwdRoundRunning, StartsAt: e.now, EndsAt: e.now.Add(5 * time.Minute), Seed: "seed-1"}
	if err := e.gdb.Create(round).Error; err != nil {
		t.Fatalf("seed round: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).Updates(map[string]any{
		"status": model.AwdGameRunning, "current_round": 1,
	}).Error; err != nil {
		t.Fatalf("start game: %v", err)
	}

	flags, err := awdSvc.GenerateFlags(ctx, round.ID)
	if err != nil || len(flags) != 2 {
		t.Fatalf("generate flags: %+v err=%v", flags, err)
	}
	flagsAgain, err := awdSvc.GenerateFlags(ctx, round.ID)
	if err != nil || len(flagsAgain) != 2 {
		t.Fatalf("regenerate flags: %+v err=%v", flagsAgain, err)
	}
	if flags[0].Value != flagsAgain[0].Value || flags[1].Value != flagsAgain[1].Value {
		t.Fatalf("flags are not deterministic: %+v vs %+v", flags, flagsAgain)
	}
	var flagA, flagB model.AwdFlag
	for _, f := range flags {
		switch f.TeamServiceID {
		case teamA.ID:
			flagA = f
		case teamB.ID:
			flagB = f
		}
	}
	if flagA.Value == "" || flagA.ValueHash == "" || flagA.ValidUntilRound != 2 {
		t.Fatalf("flag A: %+v", flagA)
	}

	got, err := awdSvc.ValidateAttack(ctx, game.ID, model.AwdCompetitorTeam, 102, flagA.Value)
	if err != nil || !got.Accepted || got.VictimID != 101 || got.RoundID != round.ID {
		t.Fatalf("valid attack: %+v err=%v", got, err)
	}
	self, err := awdSvc.ValidateAttack(ctx, game.ID, model.AwdCompetitorTeam, 101, flagA.Value)
	if err != nil || self.Accepted || self.Reason != "cannot attack your own service" {
		t.Fatalf("self attack: %+v err=%v", self, err)
	}
	unknown, err := awdSvc.ValidateAttack(ctx, game.ID, model.AwdCompetitorTeam, 102, "flag{nope}")
	if err != nil || unknown.Accepted || unknown.Reason != "flag not found" {
		t.Fatalf("unknown flag: %+v err=%v", unknown, err)
	}

	now := e.now
	if err := awdRepo.CreateAttack(ctx, &model.AwdAttack{
		GameID: game.ID, RoundID: round.ID, AttackerType: model.AwdCompetitorTeam, AttackerID: 102,
		VictimTeamServiceID: teamA.ID, FlagID: flagA.ID, Status: model.AwdAttackAccepted,
		SubmittedAt: now, JudgedAt: &now,
	}); err != nil {
		t.Fatalf("seed accepted attack: %v", err)
	}
	captured, err := awdSvc.ValidateAttack(ctx, game.ID, model.AwdCompetitorTeam, 102, flagA.Value)
	if err != nil || captured.Accepted || captured.Reason != "flag already captured" {
		t.Fatalf("captured flag: %+v err=%v", captured, err)
	}

	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).Update("current_round", 3).Error; err != nil {
		t.Fatalf("advance round: %v", err)
	}
	expired, err := awdSvc.ValidateAttack(ctx, game.ID, model.AwdCompetitorTeam, 101, flagB.Value)
	if err != nil || expired.Accepted || expired.Reason != "flag expired" {
		t.Fatalf("expired flag: %+v err=%v", expired, err)
	}
}
