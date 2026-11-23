package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// TestAwdScoringWeights 计分权重解析：缺省 1，0 保留为关闭，负值按 0。
func TestAwdScoringWeights(t *testing.T) {
	w := parseAwdScoringWeights("")
	if w.sla() != 1 || w.attack() != 1 || w.defense() != 1 || w.checkdown() != 1 {
		t.Fatalf("default weights: %+v", w)
	}
	w = parseAwdScoringWeights(`{"sla_weight":0.5,"attack_weight":2,"defense_weight":0,"checkdown_weight":-1}`)
	if w.sla() != 0.5 || w.attack() != 2 || w.defense() != 0 || w.checkdown() != 0 {
		t.Fatalf("configured weights: %+v", w)
	}
	if got := parseAwdScoringWeights("not-json"); got.sla() != 1 {
		t.Fatalf("invalid json should fall back to defaults: %+v", got)
	}
}

// TestAwdRoundSettlement 回合结算：SLA 成功率、攻击池按不同攻击者均分、
// 受害服务每回合只扣一次防守、CHECK 失败扣 checkdown；总分下限 0；
// 重复结算幂等且排名一致。
func TestAwdRoundSettlement(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdRepo := repo.NewAwdRepo(e.gdb)
	awdSvc := NewAwdCompetitionService(awdRepo, repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 结算赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
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
		Slug: "web", Image: "nginx:alpine", Ports: []AwdPort{{Name: "http", Port: 80}},
		BaseScore: intPtr(100), SlaWeight: intPtr(100), AttackPoints: intPtr(100),
		DefenseLoss: intPtr(50), CheckdownPenalty: intPtr(30),
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	alice := e.ensureUser(t, "awd-settle-alice")
	bob := e.ensureUser(t, "awd-settle-bob")
	carol := e.ensureUser(t, "awd-settle-carol")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username, carol.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}
	teamService := func(userID uint) *model.AwdTeamService {
		return &model.AwdTeamService{
			GameID: game.ID, ServiceID: svc.ID, CompetitorType: model.AwdCompetitorUser,
			CompetitorID: userID, Status: "running",
		}
	}
	aliceTS, bobTS, carolTS := teamService(alice.ID), teamService(bob.ID), teamService(carol.ID)
	for _, ts := range []*model.AwdTeamService{aliceTS, bobTS, carolTS} {
		if err := e.gdb.Create(ts).Error; err != nil {
			t.Fatalf("seed team service: %v", err)
		}
	}
	round := &model.AwdRound{
		GameID: game.ID, Number: 1, Status: model.AwdRoundRunning,
		StartsAt: e.now, EndsAt: e.now.Add(5 * time.Minute), Seed: "seed-settle",
	}
	if err := e.gdb.Create(round).Error; err != nil {
		t.Fatalf("seed round: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).Updates(map[string]any{
		"status": model.AwdGameRunning, "current_round": 1,
	}).Error; err != nil {
		t.Fatalf("start game: %v", err)
	}

	checks := []model.AwdCheck{
		{RoundID: round.ID, TeamServiceID: aliceTS.ID, Action: "check", Status: "ok"},
		{RoundID: round.ID, TeamServiceID: aliceTS.ID, Action: "check", Status: "ok"},
		{RoundID: round.ID, TeamServiceID: aliceTS.ID, Action: "put", Status: "ok"},
		{RoundID: round.ID, TeamServiceID: aliceTS.ID, Action: "get", Status: "fail"},
		{RoundID: round.ID, TeamServiceID: bobTS.ID, Action: "check", Status: "fail"},
	}
	for i := range checks {
		if err := e.gdb.Create(&checks[i]).Error; err != nil {
			t.Fatalf("seed check: %v", err)
		}
	}
	accepted := func(attackerID, victimTSID uint) *model.AwdAttack {
		return &model.AwdAttack{
			GameID: game.ID, RoundID: round.ID, AttackerType: model.AwdCompetitorUser,
			AttackerID: attackerID, VictimTeamServiceID: victimTSID,
			Status: model.AwdAttackAccepted, SubmittedAt: e.now, JudgedAt: &e.now,
		}
	}
	// alice 服务被 bob、carol 两个不同攻击者攻破；bob 服务被 alice 攻破；
	// bob 对 alice 的第二次攻击不重复计攻击分/防守分。
	for _, attack := range []*model.AwdAttack{
		accepted(bob.ID, aliceTS.ID),
		accepted(carol.ID, aliceTS.ID),
		accepted(alice.ID, bobTS.ID),
		accepted(bob.ID, aliceTS.ID),
	} {
		if err := e.gdb.Create(attack).Error; err != nil {
			t.Fatalf("seed attack: %v", err)
		}
	}

	before, err := awdSvc.Standings(ctx, comp.ID)
	if err != nil || len(before) != 3 {
		t.Fatalf("standings before settlement: %+v err=%v", before, err)
	}
	for _, row := range before {
		if row.Score != 0 || row.Rank == 0 {
			t.Fatalf("zero standing before settlement: %+v", row)
		}
	}

	view, err := awdSvc.SettleRound(ctx, comp.ID, round.ID)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if view.Events != 7 || view.RoundNumber != 1 || len(view.Standings) != 3 {
		t.Fatalf("settlement view: %+v", view)
	}
	byID := map[uint]AwdStandingView{}
	for _, row := range view.Standings {
		byID[row.CompetitorID] = row
	}
	if got := byID[alice.ID]; got.Rank != 1 || got.Score != 125 || got.AttackScore != 100 || got.DefenseLoss != 50 || got.SlaScore != 75 {
		t.Fatalf("alice standing: %+v", got)
	}
	if got := byID[carol.ID]; got.Rank != 2 || got.Score != 50 || got.AttackScore != 50 || got.DefenseLoss != 0 {
		t.Fatalf("carol standing: %+v", got)
	}
	if got := byID[bob.ID]; got.Rank != 3 || got.Score != 0 || got.AttackScore != 50 || got.DefenseLoss != 50 || got.SlaScore != 0 {
		t.Fatalf("bob standing (floor 0): %+v", got)
	}

	var settled model.AwdRound
	if err := e.gdb.First(&settled, round.ID).Error; err != nil || settled.Status != model.AwdRoundSettled {
		t.Fatalf("round should be settled: %+v err=%v", settled, err)
	}
	var eventCount int64
	if err := e.gdb.Model(&model.AwdScoreEvent{}).Where("competition_id = ?", comp.ID).Count(&eventCount).Error; err != nil || eventCount != 7 {
		t.Fatalf("score events: %d err=%v", eventCount, err)
	}

	again, err := awdSvc.SettleRound(ctx, comp.ID, round.ID)
	if err != nil || again.Events != 0 || len(again.Standings) != 3 {
		t.Fatalf("re-settle should be idempotent: %+v err=%v", again, err)
	}
	for i := range view.Standings {
		a, b := view.Standings[i], again.Standings[i]
		if a.Rank != b.Rank || a.CompetitorID != b.CompetitorID || a.Score != b.Score ||
			a.AttackScore != b.AttackScore || a.DefenseLoss != b.DefenseLoss || a.SlaScore != b.SlaScore {
			t.Fatalf("standings changed on re-settle: %+v vs %+v", a, b)
		}
	}
	if err := e.gdb.Model(&model.AwdScoreEvent{}).Where("competition_id = ?", comp.ID).Count(&eventCount).Error; err != nil || eventCount != 7 {
		t.Fatalf("score events after re-settle: %d err=%v", eventCount, err)
	}

	pendingRound := &model.AwdRound{GameID: game.ID, Number: 2, Status: model.AwdRoundPending, Seed: "seed-pending"}
	if err := e.gdb.Create(pendingRound).Error; err != nil {
		t.Fatalf("seed pending round: %v", err)
	}
	if _, err := awdSvc.SettleRound(ctx, comp.ID, pendingRound.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("pending round: want ErrValidation, got %v", err)
	}
	other, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 另一场", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create other comp: %v", err)
	}
	otherGame, err := awdSvc.EnsureGame(ctx, other.ID)
	if err != nil {
		t.Fatalf("ensure other game: %v", err)
	}
	otherRound := &model.AwdRound{GameID: otherGame.ID, Number: 1, Status: model.AwdRoundRunning, Seed: "seed-other"}
	if err := e.gdb.Create(otherRound).Error; err != nil {
		t.Fatalf("seed other round: %v", err)
	}
	if _, err := awdSvc.SettleRound(ctx, comp.ID, otherRound.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign round: want ErrNotFound, got %v", err)
	}
}
