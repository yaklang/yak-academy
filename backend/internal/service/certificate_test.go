package service

import (
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func TestCertificateIssueRules(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "证书验证赛", Start: e.now.Add(-2 * time.Hour), End: e.now.Add(time.Hour),
		Kind: "invitational", Format: "ctf",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	alice := e.ensureUser(t, "cert-alice")
	bob := e.ensureUser(t, "cert-bob")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	// 参赛者导入完成后再结束竞赛，证书只允许赛后签发。
	ended := e.now.Add(-time.Hour)
	if _, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{EndTime: &ended}); err != nil {
		t.Fatalf("end competition: %v", err)
	}
	if err := e.gdb.Create(&model.CompetitionScore{
		CompetitionID: comp.ID, UserID: alice.ID, ChallengeID: 1, Score: 100, SolvedAt: e.now.Add(-90 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed alice score: %v", err)
	}
	if err := e.gdb.Create(&model.CompetitionScore{
		CompetitionID: comp.ID, UserID: bob.ID, ChallengeID: 2, Score: 20, SolvedAt: e.now.Add(-80 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed bob score: %v", err)
	}
	if err := e.comps.comps.UpsertProfile(ctx, &model.CompetitionProfile{
		CompetitionID: comp.ID, UserID: alice.ID, RealName: "张三", School: "示例大学",
		CreatedAt: e.now, UpdatedAt: e.now,
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	certSvc := NewCertificateService(repo.NewCompetitionRepo(e.gdb), repo.NewUserRepo(e.gdb))
	certSvc.SetNow(func() time.Time { return e.now })
	certSvc.SetRankingSources(e.comps, nil, nil)
	certSvc.SetEvents(repo.NewEventRepo(e.gdb))

	preview, err := certSvc.CertificatePreview(ctx, comp.ID, CertificateRuleInput{
		Title: "一等奖", Level: "一等奖", Mode: model.QualificationTopN, Threshold: 1,
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.TotalRanked != 2 || len(preview.Candidates) != 1 {
		t.Fatalf("preview: %+v", preview)
	}
	if candidate := preview.Candidates[0]; candidate.UserID != alice.ID || candidate.Rank != 1 ||
		candidate.Score != 100 || candidate.RecipientName != "张三" {
		t.Fatalf("candidate: %+v", candidate)
	}

	// 未结束的竞赛不能签发。
	running, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "进行中证书赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "ctf",
	})
	if err != nil {
		t.Fatalf("create running comp: %v", err)
	}
	if _, err := certSvc.CertificatePreview(ctx, running.ID, CertificateRuleInput{
		Title: "进行中", Level: "测试", Mode: model.QualificationTopN, Threshold: 1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("preview running: want ErrValidation, got %v", err)
	}

	result, err := certSvc.IssueCertificates(ctx, 1, comp.ID, CertificateRuleInput{
		Title: "一等奖", Level: "一等奖", Mode: model.QualificationTopN, Threshold: 1,
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if result.Rule.IssuedAt == nil || len(result.Certificates) != 1 {
		t.Fatalf("issue result: %+v", result)
	}
	cert := result.Certificates[0]
	if cert.UserID != alice.ID || cert.RecipientName != "张三" || cert.Level != "一等奖" ||
		cert.Rank != 1 || cert.Score != 100 || len(cert.Code) != 16 ||
		cert.Code != strings.ToUpper(cert.Code) {
		t.Fatalf("certificate: %+v", cert)
	}
	view, err := certSvc.AdminCertificates(ctx, comp.ID)
	if err != nil || view.Total != 1 || len(view.Rules) != 1 || view.Rules[0].IssuedCount != 1 {
		t.Fatalf("admin view: %+v err=%v", view, err)
	}
	verified, err := certSvc.VerifyCertificate(ctx, strings.ToLower(cert.Code))
	if err != nil {
		t.Fatalf("verify certificate: %v", err)
	}
	if !verified.Valid || verified.Code != cert.Code || verified.Title != "一等奖" ||
		verified.Level != "一等奖" || verified.CompetitionName != comp.Name ||
		verified.RecipientName != "张三" || verified.Rank != 1 || verified.Score != 100 ||
		verified.IssuedAt == nil {
		t.Fatalf("verified certificate: %+v", verified)
	}
	if _, err := certSvc.VerifyCertificate(ctx, "FFFFFFFFFFFFFFFF"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown code: want ErrNotFound, got %v", err)
	}

	// 同标题重复签发拒绝。
	if _, err := certSvc.IssueCertificates(ctx, 1, comp.ID, CertificateRuleInput{
		Title: "一等奖", Level: "一等奖", Mode: model.QualificationTopN, Threshold: 1,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate title: want ErrValidation, got %v", err)
	}
	// 前 100% 含两名正分参赛者，验证码互不相同。
	result, err = certSvc.IssueCertificates(ctx, 1, comp.ID, CertificateRuleInput{
		Title: "完赛证书", Level: "完赛", Mode: model.QualificationTopPercent, Threshold: 100,
	})
	if err != nil {
		t.Fatalf("issue percent: %v", err)
	}
	if len(result.Certificates) != 2 || result.Certificates[0].Code == result.Certificates[1].Code {
		t.Fatalf("percent certificates: %+v", result.Certificates)
	}
	var events int64
	if err := e.gdb.Model(&model.Event{}).
		Where("type = ?", model.EventCompetitionCertificateIssued).Count(&events).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 2 {
		t.Fatalf("certificate events = %d, want 2", events)
	}
	// 无符合条件者拒绝签发（阈值高于所有分数）。
	if _, err := certSvc.IssueCertificates(ctx, 1, comp.ID, CertificateRuleInput{
		Title: "无人证书", Level: "测试", Mode: model.QualificationMinScore, Threshold: 1000,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty recipients: want ErrValidation, got %v", err)
	}

	certCSV, filename, err := certSvc.CertificateCSV(ctx, comp.ID)
	if err != nil || filename != "competition-1-certificates.csv" {
		t.Fatalf("certificate csv: %s err=%v", filename, err)
	}
	if !strings.HasPrefix(string(certCSV), "\xEF\xBB\xBF") {
		t.Fatal("certificate CSV must start with UTF-8 BOM")
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(certCSV), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil || len(records) != 4 {
		t.Fatalf("certificate CSV rows: %d err=%v", len(records), err)
	}
	if records[1][3] != "张三" || records[1][7] != cert.Code || records[1][8] != "/certificates/"+cert.Code {
		t.Fatalf("certificate CSV row: %+v", records[1])
	}
	awardCSV, filename, err := certSvc.AwardCSV(ctx, comp.ID)
	if err != nil || filename != "competition-1-awards.csv" {
		t.Fatalf("award csv: %s err=%v", filename, err)
	}
	awards, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(awardCSV), "\xEF\xBB\xBF"))).ReadAll()
	if err != nil || len(awards) != 4 {
		t.Fatalf("award CSV rows: %d err=%v", len(awards), err)
	}
	if awards[1][0] != comp.Name || awards[1][4] != "张三" || awards[1][2] != "一等奖" {
		t.Fatalf("award CSV row: %+v", awards[1])
	}
	if strings.Contains(string(awardCSV), cert.Code) {
		t.Fatal("award CSV must not contain verification codes")
	}
}
