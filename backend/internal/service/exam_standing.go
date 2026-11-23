package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ExamDeriver 考试成绩派生器：订阅 exam.attempt.graded，按作答事实重建
// ExamStanding（先删后插，幂等）。Standing 是派生表，业务代码不得直接写。
type ExamDeriver struct {
	exam repo.ExamRepo
}

func NewExamDeriver(exam repo.ExamRepo) *ExamDeriver {
	return &ExamDeriver{exam: exam}
}

// Handle 只消费考试评分事件；payload 的 competition_id 决定重建范围。
func (d *ExamDeriver) Handle(ctx context.Context, ev model.Event) {
	if ev.Type != model.EventExamAttemptGraded {
		return
	}
	var payload struct {
		CompetitionID uint `json:"competition_id"`
	}
	if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil || payload.CompetitionID == 0 {
		return
	}
	if err := d.Derive(ctx, payload.CompetitionID); err != nil {
		log.Printf("exam deriver: competition=%d: %v", payload.CompetitionID, err)
	}
}

// Derive 全量重建某考试成绩（只包含已交卷/已到期且有作答记录的参赛者）。
func (d *ExamDeriver) Derive(ctx context.Context, competitionID uint) error {
	attempts, err := d.exam.ListAttemptsByCompetition(ctx, competitionID)
	if err != nil {
		return err
	}
	now := time.Now()
	rows := make([]model.ExamStanding, 0, len(attempts))
	for i := range attempts {
		attempt := &attempts[i]
		if !attempt.Status.Terminal() {
			continue
		}
		duration := 0
		if attempt.SubmittedAt != nil {
			duration = int(attempt.SubmittedAt.Sub(attempt.StartedAt).Seconds())
			if duration < 0 {
				duration = 0
			}
		}
		rows = append(rows, model.ExamStanding{
			CompetitionID: competitionID, UserID: attempt.UserID,
			Score: attempt.TotalScore, ObjectiveScore: attempt.ObjectiveScore,
			CodeScore: attempt.CodeScore, Passed: attempt.Passed,
			DurationSeconds: duration, SubmittedAt: attempt.SubmittedAt,
			UpdatedAt: now,
		})
	}
	return d.exam.ReplaceStandings(ctx, competitionID, rows)
}

// Replay 启动补偿：从头重放事件，恢复崩溃窗口内丢失的成绩派生。
func (d *ExamDeriver) Replay(ctx context.Context, events repo.EventRepo) error {
	var afterID uint
	for {
		batch, err := events.ListAfter(ctx, afterID, 500)
		if err != nil {
			return fmt.Errorf("exam replay: %w", err)
		}
		if len(batch) == 0 {
			return nil
		}
		for _, ev := range batch {
			d.Handle(ctx, ev)
		}
		afterID = batch[len(batch)-1].ID
	}
}
