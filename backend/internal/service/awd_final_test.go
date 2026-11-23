package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// TestAwdFinalRankingFormula 最终排名：AWD 分按榜首归一化 × 权重 + 战报分 ×
// 权重；默认 70/30；权重可配置；并列比较战报分与最后有效攻击时间。
func TestAwdFinalRankingFormula(t *testing.T) {
	standings := []AwdStandingView{
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 1, CompetitorName: "alice", Score: 100},
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 2, CompetitorName: "bob", Score: 50},
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 3, CompetitorName: "carol", Score: 0},
	}
	reports := []model.AwdReport{
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 1, Score: 80},
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 2, Score: 100},
	}
	game := &model.AwdGame{CompetitionID: 7, Status: model.AwdGameEnded, ScoringConfig: "{}"}

	view := buildAwdFinalRanking(game, standings, reports, nil)
	if !view.Finalized || view.MaxAwdScore != 100 || view.Weights.AwdWeight != 70 || view.Weights.ReportWeight != 30 {
		t.Fatalf("view meta: %+v", view)
	}
	want := map[uint]float64{1: 94, 2: 65, 3: 0}
	for _, item := range view.Items {
		if math.Abs(item.FinalScore-want[item.CompetitorID]) > 0.001 {
			t.Fatalf("final score competitor %d: got %v want %v", item.CompetitorID, item.FinalScore, want[item.CompetitorID])
		}
	}
	if view.Items[0].CompetitorID != 1 || view.Items[1].CompetitorID != 2 || view.Items[2].CompetitorID != 3 {
		t.Fatalf("order: %+v", view.Items)
	}
	if view.Items[0].Rank != 1 || view.Items[1].Rank != 2 || view.Items[2].Rank != 3 {
		t.Fatalf("ranks: %+v", view.Items)
	}

	// 配置权重：50/50。
	game.ScoringConfig = `{"final_awd_weight":50,"final_report_weight":50}`
	view = buildAwdFinalRanking(game, standings, reports, nil)
	if math.Abs(view.Items[0].FinalScore-90) > 0.001 || math.Abs(view.Items[1].FinalScore-75) > 0.001 {
		t.Fatalf("custom weights: %+v", view.Items)
	}

	// 战报 100%：仅按战报分排序。
	game.ScoringConfig = `{"final_awd_weight":0,"final_report_weight":100}`
	view = buildAwdFinalRanking(game, standings, reports, nil)
	if view.Items[0].CompetitorID != 2 || view.Items[0].FinalScore != 100 {
		t.Fatalf("report-only ranking: %+v", view.Items)
	}
}

// TestAwdFinalRankingTieBreak 最终分与战报分相同：最后有效攻击时间更晚者在前，
// 再相同按竞域单位 ID 升序。
func TestAwdFinalRankingTieBreak(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	standings := []AwdStandingView{
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 1, CompetitorName: "alice", Score: 100},
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 2, CompetitorName: "bob", Score: 100},
	}
	reports := []model.AwdReport{
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 1, Score: 80},
		{CompetitorType: model.AwdCompetitorUser, CompetitorID: 2, Score: 80},
	}
	later := now.Add(time.Minute)
	attacks := []model.AwdAttack{
		{
			AttackerType: model.AwdCompetitorUser, AttackerID: 1,
			Status: model.AwdAttackAccepted, SubmittedAt: now, JudgedAt: &now,
		},
		{
			AttackerType: model.AwdCompetitorUser, AttackerID: 2,
			Status: model.AwdAttackAccepted, SubmittedAt: later, JudgedAt: &later,
		},
	}
	game := &model.AwdGame{CompetitionID: 7, Status: model.AwdGameEnded, ScoringConfig: "{}"}
	view := buildAwdFinalRanking(game, standings, reports, attacks)
	if view.Items[0].CompetitorID != 2 || view.Items[1].CompetitorID != 1 {
		t.Fatalf("tie-break by last attack: %+v", view.Items)
	}
	if view.Items[0].LastAttackAt == nil || !view.Items[0].LastAttackAt.Equal(later) {
		t.Fatalf("last attack time: %+v", view.Items[0])
	}

	// 无有效攻击：按 competitor_id 升序。
	reports[1].Score = 80
	view = buildAwdFinalRanking(game, standings, reports, nil)
	if view.Items[0].CompetitorID != 1 || view.Items[1].CompetitorID != 2 {
		t.Fatalf("tie-break by id: %+v", view.Items)
	}
}

// TestAwdScoringConfigFinalWeights 最终排名权重校验：0..1000、不能同时为 0。
func TestAwdScoringConfigFinalWeights(t *testing.T) {
	for _, raw := range []string{
		`{}`,
		`{"final_awd_weight":70,"final_report_weight":30}`,
		`{"final_report_weight":0}`,
	} {
		if err := validateAwdScoringConfig(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"final_awd_weight":0,"final_report_weight":0}`,
		`{"final_awd_weight":-1}`,
		`{"final_awd_weight":1001}`,
		`{"final_report_weight":"high"}`,
	} {
		if err := validateAwdScoringConfig(raw); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: want ErrValidation, got %v", raw, err)
		}
	}
}

// TestAwdFinalRankingLifecycle 管理端可预览（finalized=false），参赛者仅赛后可见；
// 非参赛者 404；自定义权重落库后生效。
func TestAwdFinalRankingLifecycle(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 最终排名", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
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
	alice := e.ensureUser(t, "awd-final-alice")
	bob := e.ensureUser(t, "awd-final-bob")
	carol := e.ensureUser(t, "awd-final-carol")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}

	if _, err := awdSvc.UpdateGame(ctx, comp.ID, AwdGameUpdate{
		ScoringConfig: json.RawMessage(`{"final_awd_weight":0,"final_report_weight":0}`),
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("zero final weights: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.UpdateGame(ctx, comp.ID, AwdGameUpdate{
		ScoringConfig: json.RawMessage(`{"final_awd_weight":60,"final_report_weight":40}`),
	}); err != nil {
		t.Fatalf("update scoring config: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).
		Update("status", model.AwdGameRunning).Error; err != nil {
		t.Fatalf("start game: %v", err)
	}

	preview, err := awdSvc.FinalRanking(ctx, comp.ID)
	if err != nil || preview.Finalized || len(preview.Items) != 2 {
		t.Fatalf("preview: %+v err=%v", preview, err)
	}
	if preview.Weights.AwdWeight != 60 || preview.Weights.ReportWeight != 40 {
		t.Fatalf("preview weights: %+v", preview.Weights)
	}
	if _, err := awdSvc.ParticipantFinalRanking(ctx, comp.ID, alice.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("final before end: want ErrValidation, got %v", err)
	}

	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).
		Update("status", model.AwdGameEnded).Error; err != nil {
		t.Fatalf("end game: %v", err)
	}
	view, err := awdSvc.ParticipantFinalRanking(ctx, comp.ID, alice.ID)
	if err != nil || !view.Finalized || len(view.Items) != 2 {
		t.Fatalf("participant final: %+v err=%v", view, err)
	}
	for i, item := range view.Items {
		if item.Rank != i+1 {
			t.Fatalf("rank: %+v", view.Items)
		}
	}
	if _, err := awdSvc.ParticipantFinalRanking(ctx, comp.ID, carol.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non participant: want ErrNotFound, got %v", err)
	}
}
