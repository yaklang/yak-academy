package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// HintView 题目详情中的提示视图（未解锁 content 为空）。
type HintView struct {
	ID       uint   `json:"id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Cost     int    `json:"cost"`
	Unlocked bool   `json:"unlocked"`
}

// HintUnlockResult 解锁响应（含扣分后积分）。
type HintUnlockResult struct {
	HintView
	Score int `json:"score"`
}

// UnlockHint 扣分解锁提示：校验属主题目 → 幂等已解锁 → 积分够扣 → 写 hint.unlocked → 广播派生。
// 派生器消费事件扣减 ScoreRecord；禁止本方法直接改写积分表（event-sourcing-rules skill）。
func (s *ChallengeService) UnlockHint(ctx context.Context, userID uint, challengeSlug string, hintID uint) (*HintUnlockResult, error) {
	if s.events == nil || s.bus == nil || s.scores == nil {
		return nil, fmt.Errorf("%w: hint unlock not wired", ErrValidation)
	}
	chal, err := s.challenges.FindBySlug(ctx, challengeSlug)
	if err != nil {
		return nil, err
	}
	if !chal.Published(time.Now()) {
		return nil, ErrNotFound
	}
	hint, err := s.challenges.FindHint(ctx, hintID)
	if err != nil {
		return nil, err
	}
	if hint.ChallengeID != chal.ID {
		return nil, ErrNotFound
	}

	unlocked, err := s.events.HasHintUnlocked(ctx, userID, hint.ID)
	if err != nil {
		return nil, err
	}
	if unlocked {
		score := currentScore(ctx, s.scores, userID)
		return &HintUnlockResult{
			HintView: HintView{ID: hint.ID, Title: hint.Title, Content: hint.Content, Cost: hint.Cost, Unlocked: true},
			Score:    score,
		}, nil
	}

	if hint.Cost > 0 {
		rec, err := s.scores.Get(ctx, userID)
		if err != nil || rec.Score < hint.Cost {
			return nil, fmt.Errorf("%w: insufficient score to unlock hint", ErrValidation)
		}
	}

	payload, _ := json.Marshal(map[string]any{
		"hint_id": hint.ID, "challenge_id": chal.ID, "cost": hint.Cost, "challenge_slug": chal.Slug,
	})
	ev := &model.Event{UserID: userID, Type: model.EventHintUnlocked, Payload: string(payload)}
	if err := s.events.Append(ctx, ev); err != nil {
		return nil, err
	}
	s.bus.Publish(ctx, *ev)

	score := currentScore(ctx, s.scores, userID)
	return &HintUnlockResult{
		HintView: HintView{ID: hint.ID, Title: hint.Title, Content: hint.Content, Cost: hint.Cost, Unlocked: true},
		Score:    score,
	}, nil
}

func currentScore(ctx context.Context, scores repo.ScoreRepo, userID uint) int {
	if rec, err := scores.Get(ctx, userID); err == nil {
		return rec.Score
	}
	return 0
}

// toHintViews 题目详情：未解锁提示正文置空。
func toHintViews(hints []model.Hint, unlocked map[uint]bool) []HintView {
	out := make([]HintView, 0, len(hints))
	for _, h := range hints {
		v := HintView{ID: h.ID, Title: h.Title, Cost: h.Cost, Unlocked: unlocked[h.ID]}
		if v.Unlocked {
			v.Content = h.Content
		}
		out = append(out, v)
	}
	return out
}
