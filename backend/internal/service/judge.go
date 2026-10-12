package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 动态计分（event-sourcing-rules skill：value = f(solve_count)，linear 衰减）。
// Phase 2 默认值：decay=10，下限 minimum = max(initial*20%, 10)。
// lab.yaml 无此字段，不扩 spec；如需按包配置在 Phase 3 扩展。
const (
	scoreDecay    = 10
	scoreMinRatio = 20 // minimum = initial 的 20%
	scoreMinFloor = 10
)

// JudgeService 异步判题。提交只入队并返回 pending；判定在 worker 内完成，
// HTTP 请求路径禁止同步判定（event-sourcing-rules skill）。
type JudgeService struct {
	submissions repo.SubmissionRepo
	challenges  repo.ChallengeRepo
	events      repo.EventRepo
	bus         *eventbus.Bus
	queue       chan uint // submission ID 队列（进程内）
}

func NewJudgeService(sub repo.SubmissionRepo, chal repo.ChallengeRepo, ev repo.EventRepo, bus *eventbus.Bus) *JudgeService {
	return &JudgeService{
		submissions: sub,
		challenges:  chal,
		events:      ev,
		bus:         bus,
		// 已知限制：进程内队列，容量 256；满时 Submit 报错但 submission 已落库
		// （永远 pending）。换持久队列为 Phase 4 事项（架构文档演进路线）。
		queue: make(chan uint, 256),
	}
}

// Submit 提交答案：写 submission(pending) + submission.created 事件，立即返回。
func (s *JudgeService) Submit(ctx context.Context, userID uint, challengeID uint, answer string) (*model.Submission, error) {
	sub := &model.Submission{
		UserID:      userID,
		ChallengeID: challengeID,
		Answer:      answer,
		Status:      model.SubmissionPending,
	}
	if err := s.submissions.Create(ctx, sub); err != nil {
		return nil, err
	}
	s.appendEvent(ctx, userID, model.EventSubmissionCreated, map[string]any{
		"submission_id": sub.ID, "challenge_id": challengeID,
	})
	select {
	case s.queue <- sub.ID:
		return sub, nil
	default:
		return nil, fmt.Errorf("judge queue is full")
	}
}

// GetSubmission 查询判定结果（属主校验：仅本人可查，他人 404）。
func (s *JudgeService) GetSubmission(ctx context.Context, userID, submissionID uint) (*model.Submission, error) {
	sub, err := s.submissions.FindByID(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	if sub.UserID != userID {
		return nil, ErrNotFound
	}
	return sub, nil
}

// IsSolved 当前用户该题是否已通关（Room 详情 Lab.solved 字段用）。
func (s *JudgeService) IsSolved(ctx context.Context, userID, challengeID uint) bool {
	solved, err := s.submissions.HasCorrect(ctx, userID, challengeID)
	return err == nil && solved
}

// StartWorker 启动异步判题 worker。判定结果只通过 submission.judged 事件进入派生表。
func (s *JudgeService) StartWorker(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case id := <-s.queue:
				s.judge(ctx, id)
			}
		}
	}()
}

// judge 判定流程：
//  1. 读 submission 与 challenge flags
//  2. static 恒时比较（compareFlag）；regex 用 MatchString（恒时要求仅适用 static）
//  3. 动态计分 value = max(initial - decay*n, minimum)
//  4. UpdateStatus + appendEvent(submission.judged)
func (s *JudgeService) judge(ctx context.Context, submissionID uint) {
	sub, err := s.submissions.FindByID(ctx, submissionID)
	if err != nil {
		log.Printf("judge: load submission %d: %v", submissionID, err)
		return
	}
	flags, err := s.challenges.FindFlags(ctx, sub.ChallengeID)
	if err != nil {
		log.Printf("judge: load flags: %v", err)
		return
	}
	correct := matchAnyFlag(flags, sub.Answer)
	// 同一用户同一题已 correct → 不重复计分（score=0），派生器跳过零分。
	if correct {
		if solved, err := s.submissions.HasCorrect(ctx, sub.UserID, sub.ChallengeID); err == nil && solved {
			s.finish(ctx, sub, model.SubmissionCorrect, 0)
			return
		}
	}
	if !correct {
		s.finish(ctx, sub, model.SubmissionIncorrect, 0)
		return
	}
	score, err := s.dynamicScore(ctx, sub.ChallengeID)
	if err != nil {
		log.Printf("judge: dynamic score: %v", err)
		s.finish(ctx, sub, model.SubmissionCorrect, 0)
		return
	}
	s.finish(ctx, sub, model.SubmissionCorrect, score)
	// 首通：Room 通关事件（教题一体，teaching-integration skill）。
	if chal, err := s.challenges.FindByID(ctx, sub.ChallengeID); err == nil {
		s.AppendRoomCompleted(ctx, sub.UserID, chal.RoomSlug)
	}
}

// finish 写回判定结果 + submission.judged 事件。
func (s *JudgeService) finish(ctx context.Context, sub *model.Submission, status model.SubmissionStatus, score int) {
	if err := s.submissions.UpdateStatus(ctx, sub.ID, status, score); err != nil {
		log.Printf("judge: update status: %v", err)
		return
	}
	s.appendEvent(ctx, sub.UserID, model.EventSubmissionJudged, map[string]any{
		"submission_id": sub.ID, "challenge_id": sub.ChallengeID,
		"correct": status == model.SubmissionCorrect, "score": score,
	})
}

// AppendRoomCompleted 判定首通（correct 且计分）后写 room.completed 事件
// （教题一体：Room 通关即题目通关，teaching-integration skill）。
func (s *JudgeService) AppendRoomCompleted(ctx context.Context, userID uint, roomSlug string) {
	s.appendEvent(ctx, userID, model.EventRoomCompleted, map[string]any{"room_slug": roomSlug})
}

// matchAnyFlag 任一 flag 命中即通过。
func matchAnyFlag(flags []model.Flag, answer string) bool {
	for _, f := range flags {
		if f.Type == model.ChallengeRegex {
			re, err := regexp.Compile(f.Value)
			if err != nil {
				continue // 校验阶段已保证合法，防御性跳过
			}
			if re.MatchString(answer) {
				return true
			}
			continue
		}
		if compareFlag(answer, f.Value) {
			return true
		}
	}
	return false
}

// dynamicScore linear 衰减：value = max(initial - decay*n, minimum)，
// n 为该题已通过人数（solve_count）。
func (s *JudgeService) dynamicScore(ctx context.Context, challengeID uint) (int, error) {
	chal, err := s.challenges.FindByID(ctx, challengeID)
	if err != nil {
		return 0, err
	}
	n, err := s.challenges.CountSolves(ctx, challengeID)
	if err != nil {
		return 0, err
	}
	return linearDecay(chal.Score, int(n)), nil
}

// linearDecay 计分公式（纯函数，便于测试）。
func linearDecay(initial, solveCount int) int {
	minimum := initial * scoreMinRatio / 100
	if minimum < scoreMinFloor {
		minimum = scoreMinFloor
	}
	value := initial - scoreDecay*solveCount
	if value < minimum {
		return minimum
	}
	return value
}

// appendEvent 写事件表（append-only）并广播给派生器。
func (s *JudgeService) appendEvent(ctx context.Context, userID uint, t model.EventType, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	ev := model.Event{UserID: userID, Type: t, Payload: string(raw)}
	if err := s.events.Append(ctx, &ev); err != nil {
		log.Printf("judge: append event %s: %v", t, err)
		return
	}
	s.bus.Publish(ctx, ev)
}