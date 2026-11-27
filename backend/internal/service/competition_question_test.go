package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 基础题只服务训练场：CTF 竞赛绑定入口必须拒绝，避免误走 flag 判定链路。
func TestCompetitionRejectsBasicQuestionBinding(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "基础题赛")
	chalRepo := repo.NewChallengeRepo(e.gdb)
	chal := &model.Challenge{
		Slug: "basic-comp", Title: "基础题", Category: "basics", Bank: model.BankBasic,
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeSingle,
		Score: 10, Description: "题干", IsVisible: true,
	}
	if err := chalRepo.Upsert(ctx, chal, nil, nil); err != nil {
		t.Fatalf("seed challenge: %v", err)
	}
	if err := chalRepo.UpsertQuestion(ctx, &model.ChallengeQuestion{
		ChallengeID: chal.ID,
		OptionsJSON: `[{"key":"A","text":"甲"},{"key":"B","text":"乙"}]`,
		AnswerJSON:  `"A"`,
	}); err != nil {
		t.Fatalf("seed question: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	if _, err := e.comps.BindChallenges(ctx, comp.ID, body, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("bind basic question err = %v, want ErrValidation", err)
	}
}
