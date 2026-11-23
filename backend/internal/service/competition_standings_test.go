package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// TestIndividualStandingsIncludeZeroScoreParticipants 个人榜必须包含已参赛但
// 零得分的选手；队伍榜中个人参赛者同样各成一行（包括零分）。
func TestIndividualStandingsIncludeZeroScoreParticipants(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "零分个人榜")
	chal := e.seedChallenge(t, "zero-board", "shared")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	if _, err := e.comps.BindChallenges(ctx, comp.ID, body, nil); err != nil {
		t.Fatalf("bind: %v", err)
	}
	solver := e.ensureUser(t, "board-solver")
	zero := e.ensureUser(t, "board-zero")
	imported, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{solver.Username, zero.Username},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	codes := map[string]string{}
	for _, p := range imported.Participants {
		codes[p.Username] = p.InviteCode
	}
	for _, u := range []*model.User{solver, zero} {
		if _, err := e.comps.Enter(ctx, u.ID, comp.ID, codes[u.Username], model.GlobalRoleStudent); err != nil {
			t.Fatalf("enter %s: %v", u.Username, err)
		}
	}
	if _, err := e.comps.SubmitFlag(ctx, e.judge, solver.ID, comp.ID, chal.Slug, "flag{zero-board}", "1.1.1.1"); err != nil {
		t.Fatalf("submit: %v", err)
	}
	e.drainJudge()

	board, err := e.comps.Leaderboard(ctx, comp.ID, "individual")
	if err != nil {
		t.Fatalf("individual leaderboard: %v", err)
	}
	items := board["items"].([]TeamLeaderboardEntry)
	if len(items) != 2 {
		t.Fatalf("individual board size = %d, want 2: %+v", len(items), items)
	}
	byName := map[string]TeamLeaderboardEntry{}
	for _, item := range items {
		if len(item.Members) != 1 {
			t.Fatalf("individual entry members: %+v", item)
		}
		byName[item.Members[0]] = item
	}
	solved := byName[solver.Username]
	if solved.Rank != 1 || solved.Score != 100 || solved.SolvedCount != 1 || solved.LastSolveAt == nil {
		t.Fatalf("solver entry: %+v", solved)
	}
	empty := byName[zero.Username]
	if empty.Rank != 2 || empty.Score != 0 || empty.SolvedCount != 0 || empty.LastSolveAt != nil {
		t.Fatalf("zero-score entry: %+v", empty)
	}

	teamBoard, err := e.comps.Leaderboard(ctx, comp.ID, "team")
	if err != nil {
		t.Fatalf("team leaderboard: %v", err)
	}
	teamItems := teamBoard["items"].([]TeamLeaderboardEntry)
	if len(teamItems) != 2 {
		t.Fatalf("team board size = %d, want 2 individual rows: %+v", len(teamItems), teamItems)
	}
	foundZero := false
	for _, item := range teamItems {
		if item.TeamName != nil && *item.TeamName == zero.Username {
			foundZero = item.Score == 0 && item.SolvedCount == 0
		}
	}
	if !foundZero {
		t.Fatalf("team board missing zero-score individual: %+v", teamItems)
	}
}
