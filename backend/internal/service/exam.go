package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 考试（Exam）独立域服务：只依赖 ExamRepo/CompetitionRepo/ChallengeRepo，
// 不引用 AWD 或 CTF CompetitionScore。试卷与试题管理在 exam_admin.go；
// 作答、交卷、评分与成绩导出在后续文件（均以 exam_* 前缀保持独立）。
type ExamService struct {
	exam       repo.ExamRepo
	comps      repo.CompetitionRepo
	challenges repo.ChallengeRepo
	now        func() time.Time
}

func NewExamService(exam repo.ExamRepo, comps repo.CompetitionRepo, challenges repo.ChallengeRepo) *ExamService {
	return &ExamService{exam: exam, comps: comps, challenges: challenges, now: time.Now}
}

// SetNow 测试注入时钟。
func (s *ExamService) SetNow(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *ExamService) requireExamCompetition(ctx context.Context, competitionID uint) (*model.Competition, error) {
	comp, err := s.comps.FindByID(ctx, competitionID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	if comp.Format != model.CompetitionFormatExam {
		return nil, fmt.Errorf("%w: competition format is %q, expected exam", ErrValidation, comp.Format)
	}
	return comp, nil
}

// EnsurePaper 为考试竞赛创建（幂等）试卷配置，默认时长 90 分钟、标题取竞赛名。
func (s *ExamService) EnsurePaper(ctx context.Context, competitionID uint) (*model.ExamPaper, error) {
	comp, err := s.requireExamCompetition(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	paper, err := s.exam.FindPaperByCompetition(ctx, competitionID)
	if err == nil {
		return paper, nil
	}
	if !isNotFound(err) {
		return nil, err
	}
	paper = &model.ExamPaper{
		CompetitionID: competitionID, Title: comp.Name,
		DurationSeconds: 5400, PassScore: 60,
	}
	if err := s.exam.CreatePaper(ctx, paper); err != nil {
		// 并发创建：唯一索引冲突时回读。
		if again, findErr := s.exam.FindPaperByCompetition(ctx, competitionID); findErr == nil {
			return again, nil
		}
		return nil, err
	}
	return paper, nil
}

// ExamQuestionAdminView 管理端试题视图：含选项与标准答案（仅供管理端编辑）。
type ExamQuestionAdminView struct {
	ID          uint                   `json:"id"`
	PaperID     uint                   `json:"paper_id"`
	Seq         int                    `json:"seq"`
	Type        model.ExamQuestionType `json:"type"`
	Title       string                 `json:"title"`
	Content     string                 `json:"content"`
	Options     []ExamOption           `json:"options"`
	Answer      any                    `json:"answer"`
	Explanation string                 `json:"explanation"`
	Score       int                    `json:"score"`
	ChallengeID uint                   `json:"challenge_id"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// ExamOption 客观题选项：key 为答案标识（A/B/C…），text 为展示文本。
type ExamOption struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// ExamPaperView 管理端试卷视图：试卷配置 + 全部试题（按 seq 升序）。
type ExamPaperView struct {
	Paper     model.ExamPaper         `json:"paper"`
	Questions []ExamQuestionAdminView `json:"questions"`
}

func newExamQuestionAdminView(question *model.ExamQuestion) ExamQuestionAdminView {
	options := []ExamOption{}
	if question.OptionsJSON != "" {
		_ = json.Unmarshal([]byte(question.OptionsJSON), &options)
	}
	var answer any
	if question.AnswerJSON != "" {
		_ = json.Unmarshal([]byte(question.AnswerJSON), &answer)
	}
	return ExamQuestionAdminView{
		ID: question.ID, PaperID: question.PaperID, Seq: question.Seq,
		Type: question.Type, Title: question.Title, Content: question.Content,
		Options: options, Answer: answer, Explanation: question.Explanation,
		Score: question.Score, ChallengeID: question.ChallengeID,
		CreatedAt: question.CreatedAt, UpdatedAt: question.UpdatedAt,
	}
}
