package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// JudgeService 异步判题。提交只入队并返回 pending；判定在 worker 内完成，
// HTTP 请求路径禁止同步判定（event-sourcing-rules skill）。
type JudgeService struct {
	submissions repo.SubmissionRepo
	challenges repo.ChallengeRepo
	events     repo.EventRepo
	bus        *eventbus.Bus
	queue      chan uint // submission ID 队列（进程内；Phase2 换持久队列）
}

func NewJudgeService(sub repo.SubmissionRepo, chal repo.ChallengeRepo, ev repo.EventRepo, bus *eventbus.Bus) *JudgeService {
	return &JudgeService{
		submissions: sub,
		challenges: chal,
		events:     ev,
		bus:        bus,
		queue:      make(chan uint, 256),
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

func (s *JudgeService) judge(ctx context.Context, submissionID uint) {
	// TODO(Phase2): 完整判定流程。骨架保留事件路径以固化数据流契约。
	//  1. 读 submission 与 challenge flags
	//  2. compareFlag 恒时比较（禁止 ==）
	//  3. 动态计分 value = f(solve_count)
	//  4. UpdateStatus + appendEvent(submission.judged)
	_ = submissionID
}

func (s *JudgeService) appendEvent(ctx context.Context, userID uint, t model.EventType, payload map[string]any) {
	raw, _ := json.Marshal(payload)
	ev := model.Event{UserID: userID, Type: t, Payload: string(raw)}
	if err := s.events.Append(ctx, &ev); err != nil {
		return
	}
	s.bus.Publish(ctx, ev)
}