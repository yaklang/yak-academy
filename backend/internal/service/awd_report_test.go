package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func awdReportZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
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

// TestAwdReportLifecycle 战报：仅赛后提交；Markdown 保存 + zip 上传；
// 管理员 rubric 评审落历史与最终分；重新提交重置评审状态。
func TestAwdReportLifecycle(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	awdSvc := NewAwdCompetitionService(repo.NewAwdRepo(e.gdb), repo.NewCompetitionRepo(e.gdb))
	awdSvc.SetNow(func() time.Time { return e.now })
	awdSvc.SetUsers(repo.NewUserRepo(e.gdb))
	awdSvc.SetReportsDir(t.TempDir())

	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 战报赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	game, err := awdSvc.EnsureGame(ctx, comp.ID)
	if err != nil {
		t.Fatalf("ensure game: %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).Updates(map[string]any{
		"competitor_type": model.AwdCompetitorUser, "status": model.AwdGameRunning,
	}).Error; err != nil {
		t.Fatalf("start game: %v", err)
	}
	alice := e.ensureUser(t, "awd-report-alice")
	bob := e.ensureUser(t, "awd-report-bob")
	carol := e.ensureUser(t, "awd-report-carol")
	if _, err := e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Individuals: []string{alice.Username, bob.Username},
	}); err != nil {
		t.Fatalf("import participants: %v", err)
	}
	if err := e.gdb.Model(&model.Participant{}).Where("competition_id = ?", comp.ID).
		Update("entered_at", e.now).Error; err != nil {
		t.Fatalf("mark entered: %v", err)
	}

	if _, err := awdSvc.SaveReport(ctx, comp.ID, alice.ID, "赛前报告"); !errors.Is(err, ErrValidation) {
		t.Fatalf("report before end: want ErrValidation, got %v", err)
	}
	if err := e.gdb.Model(&model.AwdGame{}).Where("id = ?", game.ID).
		Update("status", model.AwdGameEnded).Error; err != nil {
		t.Fatalf("end game: %v", err)
	}
	if _, err := awdSvc.SaveReport(ctx, comp.ID, alice.ID, "  "); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty report: want ErrValidation, got %v", err)
	}
	report, err := awdSvc.SaveReport(ctx, comp.ID, alice.ID, "# 战报\n\n漏洞复盘")
	if err != nil {
		t.Fatalf("save report: %v", err)
	}
	if report.Status != "submitted" || report.Score != 0 || !strings.Contains(report.ContentMD, "漏洞复盘") || report.CompetitorName != alice.Username {
		t.Fatalf("report: %+v", report)
	}
	mine, err := awdSvc.MyReport(ctx, comp.ID, alice.ID)
	if err != nil || mine == nil || mine.ID != report.ID || len(mine.Reviews) != 0 {
		t.Fatalf("my report: %+v err=%v", mine, err)
	}
	if _, err := awdSvc.MyReport(ctx, comp.ID, carol.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non participant report: want ErrNotFound, got %v", err)
	}
	if _, err := awdSvc.ReviewReport(ctx, comp.ID, report.ID, 1, AwdReportReviewInput{Score: 101}); !errors.Is(err, ErrValidation) {
		t.Fatalf("score 101: want ErrValidation, got %v", err)
	}

	if _, err := awdSvc.UploadReportZip(ctx, comp.ID, alice.ID, bytes.NewReader([]byte("not zip"))); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad zip: want ErrValidation, got %v", err)
	}
	if _, err := awdSvc.UploadReportZip(ctx, comp.ID, alice.ID, bytes.NewReader(awdReportZip(t, map[string]string{
		"../evil.txt": "x",
	}))); !errors.Is(err, ErrValidation) {
		t.Fatalf("unsafe zip: want ErrValidation, got %v", err)
	}
	withZip, err := awdSvc.UploadReportZip(ctx, comp.ID, alice.ID, bytes.NewReader(awdReportZip(t, map[string]string{
		"writeup.md": "details",
	})))
	if err != nil {
		t.Fatalf("upload zip: %v", err)
	}
	if !withZip.HasZip || !strings.HasSuffix(withZip.ZipName, ".zip") {
		t.Fatalf("zip metadata: %+v", withZip)
	}
	path, name, err := awdSvc.ReportZipPath(ctx, comp.ID, report.ID)
	if err != nil || name != withZip.ZipName {
		t.Fatalf("zip path: %q %q err=%v", path, name, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("zip file missing: %v", err)
	}

	reviewed, err := awdSvc.ReviewReport(ctx, comp.ID, report.ID, bob.ID, AwdReportReviewInput{
		Score: 88, Rubric: map[string]any{"vuln": 30, "report": 58}, Comment: "结构清晰",
	})
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if reviewed.Status != "reviewed" || reviewed.Score != 88 || len(reviewed.Reviews) != 1 {
		t.Fatalf("reviewed report: %+v", reviewed)
	}
	review := reviewed.Reviews[0]
	if review.Score != 88 || review.Comment != "结构清晰" || review.ReviewerName != bob.Username ||
		review.Rubric["vuln"] != float64(30) {
		t.Fatalf("review view: %+v", review)
	}
	reports, err := awdSvc.ListReports(ctx, comp.ID)
	if err != nil || len(reports) != 1 || reports[0].Score != 88 {
		t.Fatalf("list reports: %+v err=%v", reports, err)
	}

	resubmitted, err := awdSvc.SaveReport(ctx, comp.ID, alice.ID, "# 战报 v2")
	if err != nil || resubmitted.Status != "submitted" || resubmitted.Score != 0 || len(resubmitted.Reviews) != 1 {
		t.Fatalf("resubmit: %+v err=%v", resubmitted, err)
	}
	other, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 另一场战报赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour), Format: "awd",
	})
	if err != nil {
		t.Fatalf("create other comp: %v", err)
	}
	if _, err := awdSvc.ReviewReport(ctx, other.ID, report.ID, bob.ID, AwdReportReviewInput{Score: 10}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign report review: want ErrNotFound, got %v", err)
	}
}
