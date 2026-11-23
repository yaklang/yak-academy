package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/pathutil"
)

// AWD 赛后代战报：参赛者提交 Markdown + 可选 zip，管理员按 rubric 评审；
// 每次评审保留历史，最终分取最后一条（report.score）。zip 只落盘，不返回
// 服务端路径；下载必须经管理端鉴权的专用接口。

const (
	maxAwdReportMarkdown = 512 << 10
	maxAwdReportZip      = 32 << 20
)

type AwdReportReviewView struct {
	ID           uint           `json:"id"`
	ReviewerID   uint           `json:"reviewer_id"`
	ReviewerName string         `json:"reviewer_name,omitempty"`
	Rubric       map[string]any `json:"rubric"`
	Score        int            `json:"score"`
	Comment      string         `json:"comment"`
	CreatedAt    time.Time      `json:"created_at"`
}

type AwdReportView struct {
	ID             uint                    `json:"id"`
	CompetitionID  uint                    `json:"competition_id"`
	CompetitorType model.AwdCompetitorType `json:"competitor_type"`
	CompetitorID   uint                    `json:"competitor_id"`
	CompetitorName string                  `json:"competitor_name,omitempty"`
	ContentMD      string                  `json:"content_md"`
	ZipName        string                  `json:"zip_name"`
	HasZip         bool                    `json:"has_zip"`
	Status         string                  `json:"status"`
	Score          int                     `json:"score"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
	Reviews        []AwdReportReviewView   `json:"reviews"`
}

type AwdReportReviewInput struct {
	Score   int            `json:"score"`
	Rubric  map[string]any `json:"rubric"`
	Comment string         `json:"comment"`
}

// MyReport 当前竞域单位的战报（nil = 尚未提交）。
func (s *AwdCompetitionService) MyReport(ctx context.Context, competitionID, userID uint) (*AwdReportView, error) {
	_, game, competitorID, err := s.requireAwdParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	report, err := s.awd.FindReportByKey(ctx, competitionID, game.CompetitorType, competitorID)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, nil
	}
	return s.reportView(ctx, competitionID, game.CompetitorType, report)
}

// SaveReport 保存 Markdown 战报（赛后）；重新提交会把评审状态重置为 submitted。
func (s *AwdCompetitionService) SaveReport(ctx context.Context, competitionID, userID uint, contentMD string) (*AwdReportView, error) {
	content := strings.TrimSpace(contentMD)
	if content == "" {
		return nil, fmt.Errorf("%w: report content is required", ErrValidation)
	}
	if len(content) > maxAwdReportMarkdown {
		return nil, fmt.Errorf("%w: report content exceeds 512KiB", ErrValidation)
	}
	_, game, competitorID, err := s.requireAwdParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGameEnded {
		return nil, fmt.Errorf("%w: report can only be submitted after the game ends", ErrValidation)
	}
	now := s.now()
	report, err := s.awd.FindReportByKey(ctx, competitionID, game.CompetitorType, competitorID)
	if err != nil {
		return nil, err
	}
	if report == nil {
		report = &model.AwdReport{
			CompetitionID: competitionID, CompetitorType: game.CompetitorType,
			CompetitorID: competitorID, ContentMD: content,
			Status: "submitted", CreatedAt: now, UpdatedAt: now,
		}
		if err := s.awd.CreateReport(ctx, report); err != nil {
			// 并发提交：唯一索引冲突时回读并更新。
			existing, findErr := s.awd.FindReportByKey(ctx, competitionID, game.CompetitorType, competitorID)
			if findErr != nil || existing == nil {
				return nil, err
			}
			report = existing
			if err := s.awd.UpdateReportSelective(ctx, report.ID, map[string]any{
				"content_md": content, "status": "submitted", "score": 0,
			}); err != nil {
				return nil, err
			}
			report.ContentMD = content
			report.Status = "submitted"
			report.Score = 0
		}
	} else {
		if err := s.awd.UpdateReportSelective(ctx, report.ID, map[string]any{
			"content_md": content, "status": "submitted", "score": 0,
		}); err != nil {
			return nil, err
		}
		report.ContentMD = content
		report.Status = "submitted"
		report.Score = 0
	}
	s.appendEvent(ctx, userID, model.EventAwdReportSubmitted, map[string]any{
		"competition_id": competitionID, "report_id": report.ID,
		"competitor_type": game.CompetitorType, "competitor_id": competitorID,
	})
	return s.reportView(ctx, competitionID, game.CompetitorType, report)
}

// UploadReportZip 上传战报 zip（赛后；先保存 Markdown 再上传附件）。
func (s *AwdCompetitionService) UploadReportZip(ctx context.Context, competitionID, userID uint, r io.Reader) (*AwdReportView, error) {
	if s.reportsDir == "" {
		return nil, fmt.Errorf("%w: report storage is not configured", ErrValidation)
	}
	_, game, competitorID, err := s.requireAwdParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	if game.Status != model.AwdGameEnded {
		return nil, fmt.Errorf("%w: report can only be submitted after the game ends", ErrValidation)
	}
	report, err := s.awd.FindReportByKey(ctx, competitionID, game.CompetitorType, competitorID)
	if err != nil {
		return nil, err
	}
	if report == nil {
		return nil, fmt.Errorf("%w: submit report content before uploading a zip", ErrValidation)
	}
	data, err := io.ReadAll(io.LimitReader(r, maxAwdReportZip+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: report zip is empty", ErrValidation)
	}
	if len(data) > maxAwdReportZip {
		return nil, fmt.Errorf("%w: report zip exceeds 32MiB", ErrValidation)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid zip archive", ErrValidation)
	}
	for _, entry := range zr.File {
		if _, err := pathutil.CleanEntry(entry.Name); err != nil {
			return nil, fmt.Errorf("%w: unsafe zip entry %q", ErrValidation, entry.Name)
		}
	}
	dir := pathutil.Join(s.reportsDir, strconv.FormatUint(uint64(competitionID), 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("report-%s-%d-%d.zip", game.CompetitorType, competitorID, s.now().UnixNano())
	target := pathutil.Join(dir, name)
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return nil, err
	}
	if report.ZipFile != "" {
		_ = os.Remove(report.ZipFile)
	}
	if err := s.awd.UpdateReportSelective(ctx, report.ID, map[string]any{
		"zip_file": target, "zip_name": name, "status": "submitted", "score": 0,
	}); err != nil {
		return nil, err
	}
	report.ZipFile = target
	report.ZipName = name
	report.Status = "submitted"
	report.Score = 0
	s.appendEvent(ctx, userID, model.EventAwdReportSubmitted, map[string]any{
		"competition_id": competitionID, "report_id": report.ID,
		"competitor_type": game.CompetitorType, "competitor_id": competitorID,
		"zip_name": name,
	})
	return s.reportView(ctx, competitionID, game.CompetitorType, report)
}

// ReportZipPath 管理端下载：返回磁盘路径与展示文件名（不暴露路径给前端）。
func (s *AwdCompetitionService) ReportZipPath(ctx context.Context, competitionID, reportID uint) (string, string, error) {
	report, err := s.awd.FindReport(ctx, reportID)
	if err != nil {
		return "", "", mapRecordNotFound(err)
	}
	if report.CompetitionID != competitionID || report.ZipFile == "" {
		return "", "", ErrNotFound
	}
	return report.ZipFile, report.ZipName, nil
}

func (s *AwdCompetitionService) ListReports(ctx context.Context, competitionID uint) ([]AwdReportView, error) {
	if _, err := s.requireAwdCompetition(ctx, competitionID); err != nil {
		return nil, err
	}
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	reports, err := s.awd.ListReports(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	views := make([]AwdReportView, 0, len(reports))
	for i := range reports {
		view, err := s.reportView(ctx, competitionID, game.CompetitorType, &reports[i])
		if err != nil {
			return nil, err
		}
		views = append(views, *view)
	}
	return views, nil
}

// ReviewReport 管理员 rubric 评审：保留每次评审，最终分取最后一条。
func (s *AwdCompetitionService) ReviewReport(ctx context.Context, competitionID, reportID, reviewerID uint, in AwdReportReviewInput) (*AwdReportView, error) {
	if _, err := s.requireAwdCompetition(ctx, competitionID); err != nil {
		return nil, err
	}
	report, err := s.awd.FindReport(ctx, reportID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if report.CompetitionID != competitionID {
		return nil, ErrNotFound
	}
	if in.Score < 0 || in.Score > 100 {
		return nil, fmt.Errorf("%w: score must be between 0 and 100", ErrValidation)
	}
	comment := strings.TrimSpace(in.Comment)
	if len(comment) > 2000 {
		return nil, fmt.Errorf("%w: comment exceeds 2000 characters", ErrValidation)
	}
	rubric := in.Rubric
	if rubric == nil {
		rubric = map[string]any{}
	}
	raw, err := json.Marshal(rubric)
	if err != nil {
		return nil, fmt.Errorf("%w: rubric must be a JSON object", ErrValidation)
	}
	review := &model.AwdReportReview{
		ReportID: report.ID, ReviewerID: reviewerID, RubricJSON: string(raw),
		Score: in.Score, Comment: comment, CreatedAt: s.now(),
	}
	if err := s.awd.CreateReportReview(ctx, review); err != nil {
		return nil, err
	}
	if err := s.awd.UpdateReportSelective(ctx, report.ID, map[string]any{
		"status": "reviewed", "score": in.Score,
	}); err != nil {
		return nil, err
	}
	report.Status = "reviewed"
	report.Score = in.Score
	game, err := s.EnsureGame(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	s.appendEvent(ctx, 0, model.EventAwdReportReviewed, map[string]any{
		"competition_id": competitionID, "report_id": report.ID,
		"review_id": review.ID, "reviewer_id": reviewerID, "score": in.Score,
	})
	return s.reportView(ctx, competitionID, game.CompetitorType, report)
}

func (s *AwdCompetitionService) reportView(ctx context.Context, competitionID uint, competitorType model.AwdCompetitorType, report *model.AwdReport) (*AwdReportView, error) {
	names, err := s.awdCompetitorNames(ctx, competitionID, competitorType)
	if err != nil {
		return nil, err
	}
	reviews, err := s.awd.ListReportReviews(ctx, report.ID)
	if err != nil {
		return nil, err
	}
	reviewerNames := map[uint]string{}
	if s.users != nil && len(reviews) > 0 {
		ids := make([]uint, 0, len(reviews))
		for _, review := range reviews {
			ids = append(ids, review.ReviewerID)
		}
		users, err := s.users.FindByIDs(ctx, ids...)
		if err != nil {
			return nil, err
		}
		for _, user := range users {
			reviewerNames[user.ID] = user.Username
		}
	}
	reviewViews := make([]AwdReportReviewView, 0, len(reviews))
	for _, review := range reviews {
		rubric := map[string]any{}
		_ = json.Unmarshal([]byte(review.RubricJSON), &rubric)
		reviewViews = append(reviewViews, AwdReportReviewView{
			ID: review.ID, ReviewerID: review.ReviewerID,
			ReviewerName: awdCompetitorName(reviewerNames, review.ReviewerID),
			Rubric:       rubric, Score: review.Score, Comment: review.Comment,
			CreatedAt: review.CreatedAt,
		})
	}
	return &AwdReportView{
		ID: report.ID, CompetitionID: report.CompetitionID,
		CompetitorType: report.CompetitorType, CompetitorID: report.CompetitorID,
		CompetitorName: awdCompetitorName(names, report.CompetitorID),
		ContentMD:      report.ContentMD, ZipName: report.ZipName, HasZip: report.ZipFile != "",
		Status: report.Status, Score: report.Score,
		CreatedAt: report.CreatedAt, UpdatedAt: report.UpdatedAt,
		Reviews: reviewViews,
	}, nil
}
