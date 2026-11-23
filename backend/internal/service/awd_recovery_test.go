package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

type awdRecoveryFixture struct {
	compID  uint
	game    *model.AwdGame
	targets []model.AwdTeamService
}

// seedAwdRecoveryGame 造一场 running 的 AWD 比赛 + 若干 running 靶机（未开回合）。
func seedAwdRecoveryGame(t *testing.T, e *compEnv, awdSvc *AwdCompetitionService, name, host string, port, targets int) *awdRecoveryFixture {
	t.Helper()
	ctx := context.Background()
	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: name, Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	svc, err := awdSvc.CreateService(ctx, comp.ID, AwdServiceInput{
		Slug: "web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: port}},
		CheckerConfig: json.RawMessage(`{"type":"http","check_path":"/health","flag_path":"/flag"}`),
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	game, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	fixture := &awdRecoveryFixture{compID: comp.ID, game: game}
	for i := 1; i <= targets; i++ {
		ts := &model.AwdTeamService{
			GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorUser,
			CompetitorID: uint(i), InternalHost: host, PortsJSON: svc.PortsJSON, Status: "running",
		}
		if err := e.gdb.Create(ts).Error; err != nil {
			t.Fatalf("seed target %d: %v", i, err)
		}
		fixture.targets = append(fixture.targets, *ts)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).Updates(map[string]any{
		"status": model.AwdGameRunning,
	}).Error; err != nil {
		t.Fatalf("start game: %v", err)
	}
	fixture.game.Status = model.AwdGameRunning
	return fixture
}

func awdRecoveryHostPort(t *testing.T, server *httptest.Server) (string, int) {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host, portRaw, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return host, port
}

func containsRound(ids []uint, want uint) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestAwdRecoverPendingRound 崩溃窗口：pending 回合已落库、检查未完成。
// 恢复只补 flag 与缺失检查（已落库的 put 不重跑），置 running 并按当前时刻
// 重设回合窗口；再次恢复是空操作。
func TestAwdRecoverPendingRound(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	server, _ := httpAwdTestServer(t)
	defer server.Close()
	host, port := awdRecoveryHostPort(t, server)

	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	fixture := seedAwdRecoveryGame(t, e, awdSvc, "AWD 恢复 pending 回合", host, port, 1)

	round := &model.AwdRound{
		GameID: fixture.game.ID, Number: 1, Status: model.AwdRoundPending,
		StartsAt: e.now.Add(-time.Hour), EndsAt: e.now.Add(-time.Minute),
		Seed: "seed-recover", CreatedAt: e.now,
	}
	if err := e.gdb.Create(round).Error; err != nil {
		t.Fatalf("seed pending round: %v", err)
	}
	// 模拟崩溃前已完成 PUT：恢复必须跳过它，避免重复检查拉大 SLA 分母。
	if err := e.gdb.Create(&model.AwdCheck{
		RoundID: round.ID, TeamServiceID: fixture.targets[0].ID,
		Action: "put", Status: AwdCheckOK, CreatedAt: e.now,
	}).Error; err != nil {
		t.Fatalf("seed completed check: %v", err)
	}

	recovered, err := awdSvc.Recover(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if !containsRound(recovered.ResumedRounds, round.ID) {
		t.Fatalf("resumed rounds = %v, want round %d", recovered.ResumedRounds, round.ID)
	}
	var stored model.AwdRound
	if err := e.gdb.First(&stored, round.ID).Error; err != nil {
		t.Fatalf("load round: %v", err)
	}
	if stored.Status != model.AwdRoundRunning {
		t.Fatalf("round status = %s, want running", stored.Status)
	}
	if !stored.EndsAt.After(e.now) {
		t.Fatalf("round window not refreshed: ends_at=%v now=%v", stored.EndsAt, e.now)
	}
	var game model.AwdGame
	if err := e.gdb.First(&game, fixture.game.ID).Error; err != nil || game.CurrentRound != 1 {
		t.Fatalf("current round = %d err=%v, want 1", game.CurrentRound, err)
	}
	var flags int64
	e.gdb.Model(&model.AwdFlag{}).Where("round_id = ?", round.ID).Count(&flags)
	if flags != 1 {
		t.Fatalf("flags = %d, want 1", flags)
	}
	var checks int64
	e.gdb.Model(&model.AwdCheck{}).Where("round_id = ?", round.ID).Count(&checks)
	if checks != 3 {
		t.Fatalf("checks = %d, want 3 (resume skips existing put)", checks)
	}

	again, err := awdSvc.Recover(ctx)
	if err != nil {
		t.Fatalf("second recover: %v", err)
	}
	if containsRound(again.ResumedRounds, round.ID) {
		t.Fatalf("running round resumed twice: %v", again.ResumedRounds)
	}
	e.gdb.Model(&model.AwdCheck{}).Where("round_id = ?", round.ID).Count(&checks)
	if checks != 3 {
		t.Fatalf("checks after second recover = %d, want 3", checks)
	}
}

// TestAwdRecoverJudgesPendingAttacks 重启后滞留 pending 攻击由恢复流程判定，
// 不需要等 worker 的下一次轮询。
func TestAwdRecoverJudgesPendingAttacks(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	server, _ := httpAwdTestServer(t)
	defer server.Close()
	host, port := awdRecoveryHostPort(t, server)

	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	fixture := seedAwdRecoveryGame(t, e, awdSvc, "AWD 恢复 pending 攻击", host, port, 2)

	round := &model.AwdRound{
		GameID: fixture.game.ID, Number: 1, Status: model.AwdRoundRunning,
		StartsAt: e.now, EndsAt: e.now.Add(5 * time.Minute), Seed: "seed-attack-recover",
	}
	if err := e.gdb.Create(round).Error; err != nil {
		t.Fatalf("seed round: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", fixture.game.ID).
		Update("current_round", 1).Error; err != nil {
		t.Fatalf("advance current round: %v", err)
	}
	flags, err := awdSvc.GenerateFlags(ctx, round.ID)
	if err != nil || len(flags) != 2 {
		t.Fatalf("generate flags: %+v err=%v", flags, err)
	}
	var victimFlag model.AwdFlag
	for _, flag := range flags {
		if flag.TeamServiceID == fixture.targets[0].ID {
			victimFlag = flag
		}
	}
	if victimFlag.ID == 0 {
		t.Fatal("victim flag not generated")
	}
	attack := &model.AwdAttack{
		GameID: fixture.game.ID, RoundID: round.ID,
		AttackerType: model.AwdCompetitorUser, AttackerID: fixture.targets[1].CompetitorID,
		VictimTeamServiceID: fixture.targets[0].ID, FlagID: victimFlag.ID,
		Status: model.AwdAttackPending, SubmittedAt: e.now,
	}
	if err := e.gdb.Create(attack).Error; err != nil {
		t.Fatalf("seed pending attack: %v", err)
	}

	recovered, err := awdSvc.Recover(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered.JudgedAttacks < 1 {
		t.Fatalf("judged attacks = %d, want >= 1", recovered.JudgedAttacks)
	}
	var stored model.AwdAttack
	if err := e.gdb.First(&stored, attack.ID).Error; err != nil {
		t.Fatalf("load attack: %v", err)
	}
	if stored.Status != model.AwdAttackAccepted || stored.JudgedAt == nil {
		t.Fatalf("attack = %+v, want accepted with judged_at", stored)
	}
}

// TestAwdStartRoundResumesPendingRound 开回合检查阶段失败后管理员重试：
// 不新建回合，直接续跑同一 pending 回合并置 running。
func TestAwdStartRoundResumesPendingRound(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	server, _ := httpAwdTestServer(t)
	defer server.Close()
	host, port := awdRecoveryHostPort(t, server)

	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	fixture := seedAwdRecoveryGame(t, e, awdSvc, "AWD 重试 pending 回合", host, port, 1)

	round := &model.AwdRound{
		GameID: fixture.game.ID, Number: 1, Status: model.AwdRoundPending,
		StartsAt: e.now.Add(-time.Minute), EndsAt: e.now.Add(4 * time.Minute),
		Seed: "seed-retry", CreatedAt: e.now.Add(-time.Minute),
	}
	if err := e.gdb.Create(round).Error; err != nil {
		t.Fatalf("seed pending round: %v", err)
	}
	result, err := awdSvc.StartRound(ctx, fixture.compID)
	if err != nil {
		t.Fatalf("retry start round: %v", err)
	}
	if result.Round.ID != round.ID || result.Round.Status != model.AwdRoundRunning {
		t.Fatalf("resumed round = %+v, want existing round %d running", result.Round, round.ID)
	}
	var rounds int64
	e.gdb.Model(&model.AwdRound{}).Where("game_id = ?", fixture.game.ID).Count(&rounds)
	if rounds != 1 {
		t.Fatalf("rounds = %d, want 1", rounds)
	}
	var checks int64
	e.gdb.Model(&model.AwdCheck{}).Where("round_id = ?", round.ID).Count(&checks)
	if checks != 3 {
		t.Fatalf("checks = %d, want 3", checks)
	}
}

// TestAwdStartRoundSerializesConcurrentRequests 两个并发的开回合请求由竞赛级
// 锁串行：一个成功、一个收到 “回合仍活跃” 校验错误，不会生成两个回合。
func TestAwdStartRoundSerializesConcurrentRequests(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	server, _ := httpAwdTestServer(t)
	defer server.Close()
	host, port := awdRecoveryHostPort(t, server)

	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	fixture := seedAwdRecoveryGame(t, e, awdSvc, "AWD 并发开回合", host, port, 1)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := awdSvc.StartRound(ctx, fixture.compID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	var succeeded, rejected int
	for err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrValidation):
			rejected++
		default:
			t.Fatalf("unexpected start round error: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("start results succeeded=%d rejected=%d, want 1/1", succeeded, rejected)
	}
	var rounds int64
	e.gdb.Model(&model.AwdRound{}).Where("game_id = ?", fixture.game.ID).Count(&rounds)
	if rounds != 1 {
		t.Fatalf("rounds = %d, want 1", rounds)
	}
	var checks int64
	e.gdb.Model(&model.AwdCheck{}).Where("round_id = (SELECT id FROM awd_rounds WHERE game_id = ?)", fixture.game.ID).Count(&checks)
	if checks != 3 {
		t.Fatalf("checks = %d, want 3", checks)
	}
}
