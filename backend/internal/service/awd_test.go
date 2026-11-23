package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
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

type fakeAwdRuntime struct {
	network     string
	created     []AwdTargetSpec
	destroyed   []string
	failService uint
}

func (f *fakeAwdRuntime) EnsureNetwork(context.Context, uint) (string, error) {
	if f.network == "" {
		f.network = "fake-awd-net"
	}
	return f.network, nil
}
func (f *fakeAwdRuntime) CreateTarget(_ context.Context, spec AwdTargetSpec) (AwdTarget, error) {
	if f.failService != 0 && spec.ServiceID == f.failService {
		return AwdTarget{}, errors.New("fake runtime failure")
	}
	f.created = append(f.created, spec)
	return AwdTarget{
		ContainerID:  fmt.Sprintf("ctr-%d-%d-%d", spec.ServiceID, spec.CompetitorID, len(f.created)),
		InternalHost: fmt.Sprintf("awd-%d-%d", spec.ServiceID, spec.CompetitorID),
	}, nil
}
func (f *fakeAwdRuntime) DestroyTarget(_ context.Context, id string) error {
	f.destroyed = append(f.destroyed, id)
	return nil
}
func (f *fakeAwdRuntime) CreateAttackBox(_ context.Context, spec AwdAttackBoxSpec) (AwdAttackBoxTarget, error) {
	return AwdAttackBoxTarget{
		ContainerID: fmt.Sprintf("box-%d-%d", spec.CompetitorID, len(f.destroyed)+1),
		SSHHost:     "ssh.test",
		SSHPort:     2200 + len(f.created),
	}, nil
}
func (f *fakeAwdRuntime) DestroyAttackBox(_ context.Context, id string) error {
	f.destroyed = append(f.destroyed, id)
	return nil
}
func (f *fakeAwdRuntime) DestroyGame(context.Context, uint) error { return nil }

// TestAwdDeployTargets 运行时部署：每队每服务一个靶机；重复部署幂等复用；
// 失败服务标记 failed；团队赛未编队拒绝。
func TestAwdDeployTargets(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	rt := &fakeAwdRuntime{}
	awdSvc.SetRuntime(rt)

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 部署赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	for _, slug := range []string{"web", "pwn"} {
		if _, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
			Slug: slug, Image: "awd/" + slug + ":latest", Ports: []AwdPort{{Name: "main", Port: 80}},
		}); err != nil {
			t.Fatalf("create service %s: %v", slug, err)
		}
	}
	for _, name := range []string{"deploy-a1", "deploy-a2", "deploy-b1"} {
		e.ensureUser(t, name)
	}
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Teams: []TeamImport{
			{Name: "A 队", Members: []string{"deploy-a1", "deploy-a2"}},
			{Name: "B 队", Members: []string{"deploy-b1"}},
		},
	}); err != nil {
		t.Fatalf("import teams: %v", err)
	}
	game, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	if _, err := awdSvc.Start(ctx, comp.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	result, err := awdSvc.DeployTargets(ctx, comp.ID)
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if result.Network != "fake-awd-net" || result.Created != 4 || result.Reused != 0 || len(result.Failed) != 0 {
		t.Fatalf("deploy result: %+v", result)
	}
	if len(rt.created) != 4 {
		t.Fatalf("runtime targets = %d, want 4", len(rt.created))
	}
	again, err := awdSvc.DeployTargets(ctx, comp.ID)
	if err != nil || again.Created != 0 || again.Reused != 4 {
		t.Fatalf("idempotent deploy: %+v err=%v", again, err)
	}
	teamServices, err := repo.NewAwdRepo(e.gdb).ListTeamServicesByGame(ctx, game.ID)
	if err != nil || len(teamServices) != 4 {
		t.Fatalf("team services = %+v err=%v", teamServices, err)
	}
	hosts := map[string]bool{}
	for _, ts := range teamServices {
		if ts.Status != "running" || ts.ContainerID == "" || ts.InternalHost == "" {
			t.Fatalf("team service not running: %+v", ts)
		}
		if hosts[ts.InternalHost] {
			t.Fatalf("duplicate internal host %q", ts.InternalHost)
		}
		hosts[ts.InternalHost] = true
	}

	// 新增一个服务并注入失败：既有 4 个复用，新服务的 2 个目标失败。
	extra := &model.AwdService{
		CompetitionID: comp.ID, Slug: "misc", Name: "Misc", Image: "awd/misc:latest",
		PortsJSON: `[{"name":"main","port":80,"protocol":"tcp"}]`, CheckerConfig: "{}",
		BaseScore: 100, SlaWeight: 100, AttackPoints: 100, DefenseLoss: 50,
		CheckdownPenalty: 50, FlagLifetimeRounds: 1,
	}
	if err := e.gdb.Create(extra).Error; err != nil {
		t.Fatalf("seed extra service: %v", err)
	}
	rt.failService = extra.ID
	failedResult, err := awdSvc.DeployTargets(ctx, comp.ID)
	if err == nil || len(failedResult.Failed) != 2 || failedResult.Reused != 4 {
		t.Fatalf("failed deploy: %+v err=%v", failedResult, err)
	}

	// 团队赛未编队：新增个人参赛者后拒绝部署。
	teamless := e.ensureUser(t, "deploy-teamless")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{teamless.Username}}); err != nil {
		t.Fatalf("import teamless: %v", err)
	}
	if _, err := awdSvc.DeployTargets(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("teamless deploy: want ErrValidation, got %v", err)
	}
}

// TestAwdAttackBoxes 攻击盒：每队一个；密码加密落库且可解密；重复部署复用。
func TestAwdAttackBoxes(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdRepo := repo.NewAwdRepo(e.gdb)
	awdSvc := NewAwdCompetitionService(awdRepo, repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	awdSvc.SetSecret("test-awd-secret")
	awdSvc.SetRuntime(&fakeAwdRuntime{})

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 攻击盒赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	if _, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: 80}},
	}); err != nil {
		t.Fatalf("create service: %v", err)
	}
	for _, name := range []string{"box-a1", "box-a2", "box-b1"} {
		e.ensureUser(t, name)
	}
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Teams: []TeamImport{
			{Name: "A 队", Members: []string{"box-a1", "box-a2"}},
			{Name: "B 队", Members: []string{"box-b1"}},
		},
	}); err != nil {
		t.Fatalf("import: %v", err)
	}
	game, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	if _, err := awdSvc.Start(ctx, comp.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	result, err := awdSvc.DeployAttackBoxes(ctx, comp.ID)
	if err != nil || result.Created != 2 || result.Reused != 0 || len(result.Failed) != 0 {
		t.Fatalf("deploy boxes: %+v err=%v", result, err)
	}
	boxes, err := awdRepo.ListTeamServicesByGame(ctx, game.ID)
	_ = boxes // 仅确认接口可用；攻击盒单独查
	if err != nil {
		t.Fatalf("list team services: %v", err)
	}
	teams, err := repo.NewCompetitionRepo(e.gdb).ListTeams(ctx, comp.ID)
	if err != nil || len(teams) != 2 {
		t.Fatalf("teams: %+v err=%v", teams, err)
	}
	for _, team := range teams {
		box, err := awdRepo.FindAttackBoxByKey(ctx, comp.ID, model.AwdCompetitorTeam, team.ID)
		if err != nil {
			t.Fatalf("find attack box for team %d: %v", team.ID, err)
		}
		if box.Status != "running" || box.ContainerID == "" || box.SSHHost != "ssh.test" || box.SSHPort <= 0 {
			t.Fatalf("attack box: %+v", box)
		}
		if box.SSHPasswordEnc == "" || box.SSHPasswordEnc == "password" {
			t.Fatalf("password not encrypted: %+v", box)
		}
		plain, err := decryptAwdSecret("test-awd-secret", box.SSHPasswordEnc)
		if err != nil || plain == "" {
			t.Fatalf("decrypt password: %q err=%v", plain, err)
		}
	}
	again, err := awdSvc.DeployAttackBoxes(ctx, comp.ID)
	if err != nil || again.Created != 0 || again.Reused != 2 {
		t.Fatalf("idempotent attack boxes: %+v err=%v", again, err)
	}
}

func httpAwdTestServer(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var stored string
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/flag", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			var body map[string]string
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			stored = body["flag"]
			w.WriteHeader(http.StatusOK)
			return
		}
		if stored == "" {
			http.Error(w, "no flag", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("current=" + stored))
	})
	return httptest.NewServer(mux), &stored
}

// TestAwdHttpChecker HTTP checker：CHECK/PUT/GET 成功与 flag 不匹配失败。
func TestAwdHttpChecker(t *testing.T) {
	server, _ := httpAwdTestServer(t)
	defer server.Close()
	u, _ := url.Parse(server.URL)
	host, portRaw, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(portRaw)
	target := AwdCheckTarget{Host: host, Ports: []AwdPort{{Name: "http", Port: port}}, Flag: "flag{check}"}
	checker, err := NewAwdChecker(`{"type":"http","check_path":"/health","flag_path":"/flag"}`)
	if err != nil {
		t.Fatalf("new checker: %v", err)
	}
	check := checker.Run(context.Background(), "check", target)
	if check.Status != AwdCheckOK {
		t.Fatalf("check: %+v", check)
	}
	put := checker.Run(context.Background(), "put", target)
	if put.Status != AwdCheckOK {
		t.Fatalf("put: %+v", put)
	}
	get := checker.Run(context.Background(), "get", target)
	if get.Status != AwdCheckOK {
		t.Fatalf("get: %+v", get)
	}
	bad := checker.Run(context.Background(), "get", AwdCheckTarget{Host: host, Ports: target.Ports, Flag: "flag{other}"})
	if bad.Status != AwdCheckFail {
		t.Fatalf("bad flag: %+v", bad)
	}
	if _, err := NewAwdChecker(`{"type":"nope"}`); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad checker type: want ErrValidation, got %v", err)
	}
}

// TestAwdRunChecker RunChecker 落检查明细、推进 last_check_at、失败记 incident，
// 并把输出中的 flag 脱敏。
func TestAwdRunChecker(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdRepo := repo.NewAwdRepo(e.gdb)
	server, _ := httpAwdTestServer(t)
	defer server.Close()
	u, _ := url.Parse(server.URL)
	host, portRaw, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(portRaw)

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD checker 赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	awdSvc := NewAwdCompetitionService(awdRepo, repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	checkerCfg := json.RawMessage(`{"type":"http","check_path":"/health","flag_path":"/flag"}`)
	svc, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: port}},
		CheckerConfig: checkerCfg,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	game, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	ts := &model.AwdTeamService{
		GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorUser,
		CompetitorID: 1, InternalHost: host, PortsJSON: svc.PortsJSON, Status: "running",
	}
	if err := e.gdb.Create(ts).Error; err != nil {
		t.Fatalf("seed team service: %v", err)
	}
	if _, err := awdSvc.RunChecker(ctx, ts.ID, "put", "flag{secret}"); err != nil {
		t.Fatalf("put: %v", err)
	}
	check, err := awdSvc.RunChecker(ctx, ts.ID, "get", "flag{secret}")
	if err != nil || check.Status != AwdCheckOK {
		t.Fatalf("get: %+v err=%v", check, err)
	}
	if strings.Contains(check.Output, "flag{secret}") {
		t.Fatalf("flag leaked in check output: %q", check.Output)
	}
	var stored model.AwdTeamService
	if err := e.gdb.First(&stored, ts.ID).Error; err != nil || stored.LastCheckAt == nil {
		t.Fatalf("last_check_at not updated: %+v err=%v", stored, err)
	}
	var checkCount int64
	e.gdb.Model(&model.AwdCheck{}).Where("team_service_id = ?", ts.ID).Count(&checkCount)
	if checkCount != 2 {
		t.Fatalf("check rows = %d, want 2", checkCount)
	}

	badTS := &model.AwdTeamService{
		GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorUser,
		CompetitorID: 2, InternalHost: "127.0.0.1",
		PortsJSON: `[{"name":"http","port":1,"protocol":"tcp"}]`, Status: "running",
	}
	if err := e.gdb.Create(badTS).Error; err != nil {
		t.Fatalf("seed bad team service: %v", err)
	}
	failed, err := awdSvc.RunChecker(ctx, badTS.ID, "check", "")
	if err != nil || failed.Status == AwdCheckOK {
		t.Fatalf("unreachable check: %+v err=%v", failed, err)
	}
	var incidents int64
	e.gdb.Model(&model.AwdIncident{}).Where("competitor_id = ?", 2).Count(&incidents)
	if incidents != 1 {
		t.Fatalf("incidents = %d, want 1", incidents)
	}
}
