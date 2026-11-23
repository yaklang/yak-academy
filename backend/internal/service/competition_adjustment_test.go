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

// TestCtfAdjustmentsAndBonusCap 附加分：append-only 计入榜单、正向合计受
// bonus_cap 限制、负数可纠正；仅 CTF、开赛后、且对象必须是本场参赛者。
func TestCtfAdjustmentsAndBonusCap(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "附加分赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), BonusCap: 10,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	chal := e.seedChallenge(t, "adjustment-score", "shared")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	if _, err := e.comps.BindChallenges(ctx, comp.ID, body, nil); err != nil {
		t.Fatalf("bind: %v", err)
	}
	alice := e.ensureUser(t, "adj-alice")
	outsider := e.ensureUser(t, "adj-outsider")
	imported, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{alice.Username}})
	if err != nil || len(imported.Participants) != 1 {
		t.Fatalf("import: %+v err=%v", imported, err)
	}
	if _, err := e.comps.Enter(ctx, alice.ID, comp.ID, imported.Participants[0].InviteCode, model.GlobalRoleStudent); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if _, err := e.comps.SubmitFlag(ctx, e.judge, alice.ID, comp.ID, chal.Slug, "flag{adjustment-score}", "1.1.1.1"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	e.drainJudge()

	item, err := e.comps.AddCtfAdjustment(ctx, comp.ID, 1, CtfAdjustmentCreate{
		UserID: alice.ID, Points: 10, Reason: "AI 辅助附加分",
	})
	if err != nil {
		t.Fatalf("add +10: %v", err)
	}
	if item.Points != 10 || item.Username != alice.Username || item.TeamID != 0 {
		t.Fatalf("adjustment item: %+v", item)
	}
	if _, err := e.comps.AddCtfAdjustment(ctx, comp.ID, 1, CtfAdjustmentCreate{
		UserID: alice.ID, Points: 1, Reason: "超过上限",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("cap exceeded: want ErrValidation, got %v", err)
	}
	if _, err := e.comps.AddCtfAdjustment(ctx, comp.ID, 1, CtfAdjustmentCreate{
		UserID: alice.ID, Points: -5, Reason: "申诉纠正",
	}); err != nil {
		t.Fatalf("negative correction: %v", err)
	}

	board, err := e.comps.Leaderboard(ctx, comp.ID, "individual")
	if err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	entries := board["items"].([]TeamLeaderboardEntry)
	if len(entries) != 1 {
		t.Fatalf("individual entries = %d, want 1: %+v", len(entries), entries)
	}
	var aliceEntry TeamLeaderboardEntry
	for _, entry := range entries {
		if entry.Members[0] == alice.Username {
			aliceEntry = entry
		}
	}
	if aliceEntry.Score != 105 || aliceEntry.Bonus != 5 {
		t.Fatalf("alice entry = %+v, want score 105 bonus 5", aliceEntry)
	}

	// 首通分派生表不变：附加分不写 CompetitionScore。
	scores, err := repo.NewCompetitionRepo(e.gdb).ListScores(ctx, comp.ID)
	if err != nil || len(scores) != 1 || scores[0].Score != 100 {
		t.Fatalf("CompetitionScore mutated: %+v err=%v", scores, err)
	}
	adjustments, err := e.comps.ListCtfAdjustments(ctx, comp.ID)
	if err != nil || len(adjustments) != 2 {
		t.Fatalf("adjustments = %+v err=%v", adjustments, err)
	}

	if _, err := e.comps.AddCtfAdjustment(ctx, comp.ID, 1, CtfAdjustmentCreate{
		UserID: outsider.ID, Points: 1, Reason: "未参赛",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("non-participant: want ErrValidation, got %v", err)
	}
	scheduled, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "未开赛附加分", Start: e.now.Add(time.Hour), End: e.now.Add(2 * time.Hour), BonusCap: 10,
	})
	if err != nil {
		t.Fatalf("create scheduled: %v", err)
	}
	if _, err := e.comps.AddCtfAdjustment(ctx, scheduled.ID, 1, CtfAdjustmentCreate{
		UserID: alice.ID, Points: 1, Reason: "未开赛",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("scheduled adjustment: want ErrValidation, got %v", err)
	}
	awd, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 附加分守卫", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create awd: %v", err)
	}
	if _, err := e.comps.AddCtfAdjustment(ctx, awd.ID, 1, CtfAdjustmentCreate{
		UserID: alice.ID, Points: 1, Reason: "AWD 无附加分",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("awd adjustment: want ErrValidation, got %v", err)
	}
}
