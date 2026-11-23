package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 管理端考试成绩：按参赛者汇总作答状态与分数，并列时先看总分，再看用时，
// 最后看交卷时间与用户 ID。成绩只读，派生规则由 ExamDeriver 维护；
// CSV 带 UTF-8 BOM，时间统一 RFC3339，便于赛后归档与二次处理。

type ExamResultStatus string

const (
	ExamResultNotStarted ExamResultStatus = "not_started"
	ExamResultInProgress ExamResultStatus = "in_progress"
	ExamResultSubmitted  ExamResultStatus = "submitted"
	ExamResultExpired    ExamResultStatus = "expired"
)

func examResultStatusOf(status model.ExamAttemptStatus) ExamResultStatus {
	switch status {
	case model.ExamAttemptInProgress:
		return ExamResultInProgress
	case model.ExamAttemptSubmitted:
		return ExamResultSubmitted
	case model.ExamAttemptExpired:
		return ExamResultExpired
	default:
		return ExamResultNotStarted
	}
}

func (s ExamResultStatus) Terminal() bool {
	return s == ExamResultSubmitted || s == ExamResultExpired
}

type ExamResultRow struct {
	Rank            int              `json:"rank"`
	UserID          uint             `json:"user_id"`
	Username        string           `json:"username"`
	Entered         bool             `json:"entered"`
	Status          ExamResultStatus `json:"status"`
	AttemptID       uint             `json:"attempt_id"`
	StartedAt       *time.Time       `json:"started_at"`
	SubmittedAt     *time.Time       `json:"submitted_at"`
	DurationSeconds int              `json:"duration_seconds"`
	AutoSubmitted   bool             `json:"auto_submitted"`
	ObjectiveScore  int              `json:"objective_score"`
	CodeScore       int              `json:"code_score"`
	TotalScore      int              `json:"total_score"`
	Passed          bool             `json:"passed"`
}

type ExamResultsSummary struct {
	Participants int     `json:"participants"`
	Entered      int     `json:"entered"`
	Started      int     `json:"started"`
	InProgress   int     `json:"in_progress"`
	Submitted    int     `json:"submitted"`
	Expired      int     `json:"expired"`
	Passed       int     `json:"passed"`
	AverageScore float64 `json:"average_score"`
	HighestScore int     `json:"highest_score"`
}

type ExamResultsView struct {
	CompetitionID uint               `json:"competition_id"`
	Paper         ExamPaperPublic    `json:"paper"`
	Summary       ExamResultsSummary `json:"summary"`
	Results       []ExamResultRow    `json:"results"`
	GeneratedAt   time.Time          `json:"generated_at"`
}

// AdminResults 管理端成绩单：包含全部参赛者（未开始 = not_started）。
func (s *ExamService) AdminResults(ctx context.Context, competitionID uint) (*ExamResultsView, error) {
	if _, err := s.requireExamCompetition(ctx, competitionID); err != nil {
		return nil, err
	}
	paper, err := s.EnsurePaper(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	participants, _, err := s.comps.ListParticipants(ctx, competitionID, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	attempts, err := s.exam.ListAttemptsByCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]uint, 0, len(participants))
	for _, participant := range participants {
		userIDs = append(userIDs, participant.UserID)
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, err
	}
	usernames := make(map[uint]string, len(users))
	for _, user := range users {
		usernames[user.ID] = user.Username
	}
	attemptByUser := make(map[uint]model.ExamAttempt, len(attempts))
	for _, attempt := range attempts {
		attemptByUser[attempt.UserID] = attempt
	}

	rows := make([]ExamResultRow, 0, len(participants))
	for _, participant := range participants {
		row := ExamResultRow{
			UserID: participant.UserID, Username: usernames[participant.UserID],
			Entered: participant.EnteredAt != nil, Status: ExamResultNotStarted,
		}
		if attempt, ok := attemptByUser[participant.UserID]; ok {
			started := attempt.StartedAt
			row.AttemptID = attempt.ID
			row.Status = examResultStatusOf(attempt.Status)
			row.StartedAt = &started
			row.ObjectiveScore = attempt.ObjectiveScore
			row.CodeScore = attempt.CodeScore
			row.TotalScore = attempt.TotalScore
			row.Passed = attempt.Passed
			row.AutoSubmitted = attempt.AutoSubmitted
			row.SubmittedAt = attempt.SubmittedAt
			if attempt.SubmittedAt != nil {
				duration := int(attempt.SubmittedAt.Sub(attempt.StartedAt).Seconds())
				if duration < 0 {
					duration = 0
				}
				row.DurationSeconds = duration
			}
		}
		rows = append(rows, row)
	}
	sortExamResults(rows)
	view := &ExamResultsView{
		CompetitionID: competitionID,
		Paper: ExamPaperPublic{
			ID: paper.ID, Title: paper.Title, Description: paper.Description,
			DurationSeconds: paper.DurationSeconds, PassScore: paper.PassScore,
			TotalScore: paper.TotalScore,
		},
		Summary:     summarizeExamResults(rows),
		Results:     rows,
		GeneratedAt: s.nowUTC(),
	}
	return view, nil
}

// ResultsCSV 成绩单导出（管理端）。
func (s *ExamService) ResultsCSV(ctx context.Context, competitionID uint) ([]byte, string, error) {
	view, err := s.AdminResults(ctx, competitionID)
	if err != nil {
		return nil, "", err
	}
	rows := [][]string{{
		"排名", "用户名", "是否入场", "状态", "总分", "客观题", "编程题",
		"是否通过", "用时(秒)", "开始时间", "交卷时间", "自动交卷",
	}}
	for _, item := range view.Results {
		rows = append(rows, []string{
			examResultRank(item.Rank), item.Username, yesNo(item.Entered),
			examResultStatusLabel(item.Status), strconv.Itoa(item.TotalScore),
			strconv.Itoa(item.ObjectiveScore), strconv.Itoa(item.CodeScore),
			yesNo(item.Passed), strconv.Itoa(item.DurationSeconds),
			examResultTime(item.StartedAt), examResultTime(item.SubmittedAt),
			yesNo(item.AutoSubmitted),
		})
	}
	data, err := examCSVWrite(rows)
	if err != nil {
		return nil, "", err
	}
	return data, fmt.Sprintf("competition-%d-exam-results.csv", competitionID), nil
}

func sortExamResults(rows []ExamResultRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		leftTerminal, rightTerminal := left.Status.Terminal(), right.Status.Terminal()
		if leftTerminal != rightTerminal {
			return leftTerminal
		}
		if leftTerminal {
			if left.TotalScore != right.TotalScore {
				return left.TotalScore > right.TotalScore
			}
			if left.DurationSeconds != right.DurationSeconds {
				return left.DurationSeconds < right.DurationSeconds
			}
			if left.SubmittedAt != nil && right.SubmittedAt != nil &&
				!left.SubmittedAt.Equal(*right.SubmittedAt) {
				return left.SubmittedAt.Before(*right.SubmittedAt)
			}
			if (left.SubmittedAt == nil) != (right.SubmittedAt == nil) {
				return left.SubmittedAt != nil
			}
			return left.UserID < right.UserID
		}
		leftStarted := left.Status == ExamResultInProgress
		rightStarted := right.Status == ExamResultInProgress
		if leftStarted != rightStarted {
			return leftStarted
		}
		if left.Entered != right.Entered {
			return left.Entered
		}
		return left.UserID < right.UserID
	})
	rank := 0
	for i := range rows {
		if rows[i].Status.Terminal() {
			rank++
			rows[i].Rank = rank
		} else {
			rows[i].Rank = 0
		}
	}
}

func summarizeExamResults(rows []ExamResultRow) ExamResultsSummary {
	summary := ExamResultsSummary{Participants: len(rows)}
	total := 0
	for _, row := range rows {
		if row.Entered {
			summary.Entered++
		}
		if row.Status != ExamResultNotStarted {
			summary.Started++
		}
		switch row.Status {
		case ExamResultInProgress:
			summary.InProgress++
		case ExamResultSubmitted:
			summary.Submitted++
		case ExamResultExpired:
			summary.Expired++
		}
		if !row.Status.Terminal() {
			continue
		}
		total += row.TotalScore
		if row.Passed {
			summary.Passed++
		}
		if row.TotalScore > summary.HighestScore {
			summary.HighestScore = row.TotalScore
		}
	}
	graded := summary.Submitted + summary.Expired
	if graded > 0 {
		summary.AverageScore = math.Round(float64(total)/float64(graded)*100) / 100
	}
	return summary
}

func examResultTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func examResultStatusLabel(status ExamResultStatus) string {
	switch status {
	case ExamResultInProgress:
		return "作答中"
	case ExamResultSubmitted:
		return "已交卷"
	case ExamResultExpired:
		return "超时交卷"
	default:
		return "未开始"
	}
}

func examResultRank(rank int) string {
	if rank <= 0 {
		return ""
	}
	return strconv.Itoa(rank)
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func examCSVWrite(rows [][]string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
