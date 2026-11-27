package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// seedObjective 造一道基础题（走真实 repo，与内容导入落库形态一致）。
func seedObjective(t *testing.T, env *judgeEnv, slug string, typ model.ChallengeType, score int, optionsJSON, answerJSON, matchJSON string) uint {
	t.Helper()
	chalRepo := repo.NewChallengeRepo(env.gdb)
	chal := &model.Challenge{
		Slug: slug, Title: slug, Category: "basics", Bank: model.BankBasic,
		Difficulty: model.DifficultyApprentice, Type: typ, Score: score,
		Description: "基础题测试", IsVisible: true,
	}
	if err := chalRepo.Upsert(context.Background(), chal, nil, nil); err != nil {
		t.Fatalf("seed challenge: %v", err)
	}
	if err := chalRepo.UpsertQuestion(context.Background(), &model.ChallengeQuestion{
		ChallengeID: chal.ID, OptionsJSON: optionsJSON, AnswerJSON: answerJSON, MatchJSON: matchJSON,
	}); err != nil {
		t.Fatalf("seed question: %v", err)
	}
	return chal.ID
}

func submitObjective(t *testing.T, env *judgeEnv, chalID uint, answer string) (model.SubmissionStatus, model.Submission) {
	t.Helper()
	sub, err := env.judge.Submit(context.Background(), 1, chalID, answer)
	if err != nil {
		t.Fatalf("submit %q: %v", answer, err)
	}
	status := env.waitStatus(t, sub.ID)
	return status, env.sub(t, sub.ID)
}

func TestJudgeObjectiveSingle(t *testing.T) {
	env := newJudgeTestEnv(t)
	env.judge.StartWorker(context.Background())
	chalID := seedObjective(t, env, "obj-single", model.ChallengeSingle, 100,
		`[{"key":"A","text":"错误"},{"key":"B","text":"正确"}]`, `"B"`, "")

	status, sub := submitObjective(t, env, chalID, `"A"`)
	if status != model.SubmissionIncorrect || sub.Score != 0 {
		t.Fatalf("wrong single = %s/%d, want incorrect/0", status, sub.Score)
	}
	status, sub = submitObjective(t, env, chalID, `"B"`)
	if status != model.SubmissionCorrect || sub.Score != 100 {
		t.Fatalf("correct single = %s/%d, want correct/100", status, sub.Score)
	}
	// 重复全对：分值只结算一次。
	status, sub = submitObjective(t, env, chalID, `"B"`)
	if status != model.SubmissionCorrect || sub.Score != 0 {
		t.Fatalf("repeat single = %s/%d, want correct/0", status, sub.Score)
	}
}

func TestJudgeObjectiveMultiplePartialCredit(t *testing.T) {
	env := newJudgeTestEnv(t)
	env.judge.StartWorker(context.Background())
	chalID := seedObjective(t, env, "obj-multiple", model.ChallengeMultiple, 100,
		`[{"key":"A","text":"A"},{"key":"B","text":"B"},{"key":"C","text":"C"},{"key":"D","text":"D"}]`,
		`["A","B","C"]`, "")

	cases := []struct {
		name   string
		answer string
		want   model.SubmissionStatus
		score  int
	}{
		{"one correct", `["A"]`, model.SubmissionIncorrect, 33},
		{"two correct", `["A","B"]`, model.SubmissionIncorrect, 67},
		{"one correct one wrong", `["A","D"]`, model.SubmissionIncorrect, 0},
		{"all options", `["A","B","C","D"]`, model.SubmissionIncorrect, 67},
		{"three correct", `["C","B","A"]`, model.SubmissionCorrect, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, sub := submitObjective(t, env, chalID, tc.answer)
			if status != tc.want || sub.Score != tc.score {
				t.Fatalf("%s = %s/%d, want %s/%d", tc.name, status, sub.Score, tc.want, tc.score)
			}
			if tc.want != model.SubmissionCorrect {
				// 部分分只写 submission 反馈，不能改变通关状态。
				var ev model.Event
				if err := env.gdb.Where("type = ?", model.EventSubmissionJudged).Order("id desc").First(&ev).Error; err != nil {
					t.Fatalf("load event: %v", err)
				}
				var payload map[string]any
				if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
					t.Fatalf("event payload: %v", err)
				}
				if payload["correct"] != false {
					t.Fatalf("partial event correct = %v, want false", payload["correct"])
				}
			}
		})
	}
}

func TestJudgeObjectivePartialAfterSolvedIsZero(t *testing.T) {
	env := newJudgeTestEnv(t)
	env.judge.StartWorker(context.Background())
	chalID := seedObjective(t, env, "obj-solved-partial", model.ChallengeMultiple, 100,
		`[{"key":"A","text":"A"},{"key":"B","text":"B"}]`, `["A","B"]`, "")

	if status, sub := submitObjective(t, env, chalID, `["A","B"]`); status != model.SubmissionCorrect || sub.Score != 100 {
		t.Fatalf("full = %s/%d", status, sub.Score)
	}
	status, sub := submitObjective(t, env, chalID, `["A"]`)
	if status != model.SubmissionIncorrect || sub.Score != 0 {
		t.Fatalf("partial after solved = %s/%d, want incorrect/0", status, sub.Score)
	}
}

func TestJudgeObjectiveTrueFalse(t *testing.T) {
	env := newJudgeTestEnv(t)
	env.judge.StartWorker(context.Background())
	chalID := seedObjective(t, env, "obj-truefalse", model.ChallengeTrueFalse, 50, `[]`, `true`, "")

	if status, sub := submitObjective(t, env, chalID, `false`); status != model.SubmissionIncorrect || sub.Score != 0 {
		t.Fatalf("wrong truefalse = %s/%d", status, sub.Score)
	}
	if status, sub := submitObjective(t, env, chalID, `true`); status != model.SubmissionCorrect || sub.Score != 50 {
		t.Fatalf("correct truefalse = %s/%d", status, sub.Score)
	}
}

func TestJudgeObjectiveFillMatching(t *testing.T) {
	env := newJudgeTestEnv(t)
	env.judge.StartWorker(context.Background())
	// 默认 trim + 忽略大小写；两个可接受答案。
	chalID := seedObjective(t, env, "obj-fill", model.ChallengeFill, 40, `[]`,
		`["Paris","City of Paris"]`, `{"case_sensitive":false,"trim":true,"regex":false}`)

	for i, answer := range []string{`"  paris  "`, `"PARIS"`, `"City of Paris"`} {
		status, sub := submitObjective(t, env, chalID, answer)
		if status != model.SubmissionCorrect {
			t.Fatalf("fill %s = %s/%d, want correct", answer, status, sub.Score)
		}
		if i == 0 && sub.Score != 40 {
			t.Fatalf("first fill score = %d, want 40", sub.Score)
		}
	}
	if status, sub := submitObjective(t, env, chalID, `"Lyon"`); status != model.SubmissionIncorrect || sub.Score != 0 {
		t.Fatalf("wrong fill = %s/%d", status, sub.Score)
	}

	// 正则 + 大小写敏感。
	regexID := seedObjective(t, env, "obj-fill-regex", model.ChallengeFill, 40, `[]`,
		`["^flag\\{[0-9]+\\}$"]`, `{"case_sensitive":true,"trim":true,"regex":true}`)
	if status, sub := submitObjective(t, env, regexID, `"flag{123}"`); status != model.SubmissionCorrect || sub.Score != 40 {
		t.Fatalf("regex fill = %s/%d", status, sub.Score)
	}
	if status, _ := submitObjective(t, env, regexID, `"FLAG{123}"`); status != model.SubmissionIncorrect {
		t.Fatalf("case-sensitive regex fill accepted wrong case")
	}
}

func TestJudgeObjectiveSubmitValidation(t *testing.T) {
	env := newJudgeTestEnv(t)
	env.judge.StartWorker(context.Background())
	chalID := seedObjective(t, env, "obj-invalid", model.ChallengeSingle, 10,
		`[{"key":"A","text":"A"},{"key":"B","text":"B"}]`, `"A"`, "")

	cases := []string{`"C"`, `A`, `["A"]`, `""`}
	for _, answer := range cases {
		if _, err := env.judge.Submit(context.Background(), 1, chalID, answer); !errors.Is(err, ErrValidation) {
			t.Fatalf("submit %q err = %v, want ErrValidation", answer, err)
		}
	}
}
