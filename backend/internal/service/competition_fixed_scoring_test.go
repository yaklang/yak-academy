package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// TestFixedScoringKeepsNominalValueForEverySolver 固定计分模式下同一题不随
// 通过人数衰减：两人先后通过都拿题面分值（动态模式第二人应为 90）。
func TestFixedScoringKeepsNominalValueForEverySolver(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "固定计分赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour),
		ScoringMode: "fixed",
	})
	if err != nil {
		t.Fatalf("create fixed competition: %v", err)
	}
	chal := e.seedChallenge(t, "fixed-score", "shared")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	if _, err := e.comps.BindChallenges(ctx, comp.ID, body, nil); err != nil {
		t.Fatalf("bind: %v", err)
	}
	alice := e.ensureUser(t, "fixed-alice")
	bob := e.ensureUser(t, "fixed-bob")
	imported, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	codes := map[string]string{}
	for _, p := range imported.Participants {
		codes[p.Username] = p.InviteCode
	}
	for _, u := range []*model.User{alice, bob} {
		if _, err := e.comps.Enter(ctx, u.ID, comp.ID, codes[u.Username], model.GlobalRoleStudent); err != nil {
			t.Fatalf("enter %s: %v", u.Username, err)
		}
	}
	for _, u := range []*model.User{alice, bob} {
		if _, err := e.comps.SubmitFlag(ctx, e.judge, u.ID, comp.ID, chal.Slug, "flag{fixed-score}", "1.1.1.1"); err != nil {
			t.Fatalf("submit %s: %v", u.Username, err)
		}
		e.drainJudge()
	}

	scores, err := repo.NewCompetitionRepo(e.gdb).ListScores(ctx, comp.ID)
	if err != nil {
		t.Fatalf("list scores: %v", err)
	}
	byUser := map[uint]int{}
	for _, sc := range scores {
		byUser[sc.UserID] = sc.Score
	}
	if len(scores) != 2 || byUser[alice.ID] != 100 || byUser[bob.ID] != 100 {
		t.Fatalf("fixed scores = %+v, want alice=100 bob=100", byUser)
	}
}
