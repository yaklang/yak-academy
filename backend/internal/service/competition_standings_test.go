package service

import (
	"bytes"
	"context"
	"encoding/csv"
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

	raw, filename, err := e.comps.StandingsCSV(ctx, comp.ID, "individual")
	if err != nil {
		t.Fatalf("individual CSV: %v", err)
	}
	if filename != "competition-"+itoa(comp.ID)+"-standings-individual.csv" {
		t.Fatalf("individual CSV filename = %q", filename)
	}
	if !bytes.HasPrefix(raw, []byte("\xEF\xBB\xBF")) {
		t.Fatal("CSV must start with UTF-8 BOM")
	}
	rows, err := csv.NewReader(bytes.NewReader(raw[3:])).ReadAll()
	if err != nil {
		t.Fatalf("parse individual CSV: %v", err)
	}
	if len(rows) != 3 || rows[0][0] != "排名" || rows[0][2] != "账号编码" {
		t.Fatalf("individual CSV rows: %+v", rows)
	}
	if rows[1][1] != solver.Username || rows[1][4] != "100" || rows[1][5] != "1" {
		t.Fatalf("individual CSV solver row: %+v", rows[1])
	}
	if rows[2][1] != zero.Username || rows[2][4] != "0" || rows[2][5] != "0" {
		t.Fatalf("individual CSV zero row: %+v", rows[2])
	}

	teamRaw, teamFilename, err := e.comps.StandingsCSV(ctx, comp.ID, "team")
	if err != nil || teamFilename != "competition-"+itoa(comp.ID)+"-standings-team.csv" {
		t.Fatalf("team CSV: name=%q err=%v", teamFilename, err)
	}
	teamRows, err := csv.NewReader(bytes.NewReader(teamRaw[3:])).ReadAll()
	if err != nil {
		t.Fatalf("parse team CSV: %v", err)
	}
	if len(teamRows) != 3 || teamRows[0][1] != "队伍" || teamRows[0][2] != "成员" {
		t.Fatalf("team CSV rows: %+v", teamRows)
	}
	if teamRows[2][1] != zero.Username || teamRows[2][3] != "0" {
		t.Fatalf("team CSV zero row: %+v", teamRows[2])
	}
}

func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
