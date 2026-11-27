package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

func uploadQuestionJSON(t *testing.T, svc *ContentService, raw string) (int, []string) {
	t.Helper()
	imported, slugs, err := svc.UploadChallenges(context.Background(), []byte(raw))
	if err != nil {
		t.Fatalf("UploadChallenges: %v", err)
	}
	return imported, slugs
}

func TestUploadBasicQuestionsJSON(t *testing.T) {
	svc, gdb := newContentTestService(t)
	raw := `[
	  {"slug":"basic-single","title":"单选","category":"basics","difficulty":"apprentice",
	   "description_md":"选一个","score":10,
	   "question":{"type":"single","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":"B","explanation":"乙正确"}},
	  {"slug":"basic-multiple","title":"多选","category":"basics","difficulty":"practitioner",
	   "description_md":"选多个","score":20,
	   "question":{"type":"multiple","options":[{"key":"C","text":"丙"},{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":["C","A"]}},
	  {"slug":"basic-truefalse","title":"判断","category":"basics","difficulty":"apprentice",
	   "description_md":"判断","score":5,
	   "question":{"type":"truefalse","answer":true}},
	  {"slug":"basic-fill","title":"填空","category":"basics","difficulty":"expert",
	   "description_md":"填空","score":30,
	   "question":{"type":"fill","answer":["Paris","paris"]}}
	]`
	imported, slugs := uploadQuestionJSON(t, svc, raw)
	if imported != 4 || len(slugs) != 4 {
		t.Fatalf("imported=%d slugs=%v", imported, slugs)
	}

	wantTypes := map[string]model.ChallengeType{
		"basic-single": model.ChallengeSingle, "basic-multiple": model.ChallengeMultiple,
		"basic-truefalse": model.ChallengeTrueFalse, "basic-fill": model.ChallengeFill,
	}
	for slug, wantType := range wantTypes {
		var chal model.Challenge
		if err := gdb.Where("slug = ?", slug).First(&chal).Error; err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		if chal.Bank != model.BankBasic || chal.Type != wantType {
			t.Fatalf("%s bank/type = %s/%s, want basic/%s", slug, chal.Bank, chal.Type, wantType)
		}
		var flags int64
		gdb.Model(&model.Flag{}).Where("challenge_id = ?", chal.ID).Count(&flags)
		if flags != 0 {
			t.Fatalf("%s should not have flag rows, got %d", slug, flags)
		}
		var q model.ChallengeQuestion
		if err := gdb.Where("challenge_id = ?", chal.ID).First(&q).Error; err != nil {
			t.Fatalf("%s question: %v", slug, err)
		}
	}

	var multiple model.ChallengeQuestion
	{
		var chal model.Challenge
		gdb.Where("slug = ?", "basic-multiple").First(&chal)
		gdb.Where("challenge_id = ?", chal.ID).First(&multiple)
	}
	if multiple.AnswerJSON != `["A","C"]` {
		t.Fatalf("multiple answer = %s, want sorted canonical", multiple.AnswerJSON)
	}
	var fill model.ChallengeQuestion
	{
		var chal model.Challenge
		gdb.Where("slug = ?", "basic-fill").First(&chal)
		gdb.Where("challenge_id = ?", chal.ID).First(&fill)
	}
	var match QuestionMatch
	if err := json.Unmarshal([]byte(fill.MatchJSON), &match); err != nil {
		t.Fatalf("fill match: %v", err)
	}
	if match.CaseSensitive || !match.Trim || match.Regex {
		t.Fatalf("fill default match = %+v", match)
	}

	// 管理端完整定义回填：含答案，供编辑弹窗使用。
	full, err := svc.challenge.GetFullJSON(context.Background(), "basic-single")
	if err != nil {
		t.Fatalf("GetFullJSON: %v", err)
	}
	if full.Question == nil || full.Question.Type != model.ChallengeSingle || string(full.Question.Answer) != `"B"` {
		t.Fatalf("full question = %+v", full.Question)
	}
	if full.Question.Explanation != "乙正确" {
		t.Fatalf("explanation = %q", full.Question.Explanation)
	}
}

func TestUploadBasicQuestionValidation(t *testing.T) {
	svc, gdb := newContentTestService(t)
	base := `"slug":"bad","title":"坏题","category":"basics","difficulty":"apprentice","description_md":"题干","score":10`
	cases := []struct {
		name string
		raw  string
	}{
		{"question and challenge", `{` + base + `,"question":{"type":"single","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":"A"},"challenge":{"type":"static","flag":"flag{x}"}}`},
		{"question and judge", `{` + base + `,"question":{"type":"single","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":"A"},"judge":{"engine":"yak","time_limit_s":1,"testdata":[]}}`},
		{"bank ctf", `{` + base + `,"bank":"ctf","question":{"type":"single","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":"A"}}`},
		{"one option", `{` + base + `,"question":{"type":"single","options":[{"key":"A","text":"甲"}],"answer":"A"}}`},
		{"duplicate key", `{` + base + `,"question":{"type":"single","options":[{"key":"A","text":"甲"},{"key":"A","text":"乙"}],"answer":"A"}}`},
		{"answer not option", `{` + base + `,"question":{"type":"single","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":"C"}}`},
		{"multiple empty", `{` + base + `,"question":{"type":"multiple","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":[]}}`},
		{"truefalse options", `{` + base + `,"question":{"type":"truefalse","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":true}}`},
		{"fill empty", `{` + base + `,"question":{"type":"fill","answer":""}}`},
		{"fill bad regex", `{` + base + `,"question":{"type":"fill","answer":["["],"match":{"regex":true}}}`},
		{"missing description", `{"slug":"bad","title":"坏题","category":"basics","difficulty":"apprentice","score":10,"question":{"type":"truefalse","answer":true}}`},
		{"env", `{` + base + `,"env":{"image":"x","port":80},"question":{"type":"truefalse","answer":true}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.UploadChallenges(context.Background(), []byte(tc.raw))
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
	var count int64
	gdb.Model(&model.Challenge{}).Count(&count)
	if count != 0 {
		t.Fatalf("invalid uploads wrote %d challenges", count)
	}
}

func TestUploadBasicQuestionUpdateAndTypeSwitch(t *testing.T) {
	svc, gdb := newContentTestService(t)
	uploadQuestionJSON(t, svc, `{"slug":"switch","title":"切换","category":"basics","difficulty":"apprentice","description_md":"题干","score":10,"question":{"type":"single","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":"A"}}`)

	// 更新基础题：答案与解析覆盖，question 行仍为 1。
	uploadQuestionJSON(t, svc, `{"slug":"switch","title":"切换2","category":"basics","difficulty":"apprentice","description_md":"新题干","score":20,"question":{"type":"truefalse","answer":false,"explanation":"解析"}}`)
	var chal model.Challenge
	gdb.Where("slug = ?", "switch").First(&chal)
	if chal.Type != model.ChallengeTrueFalse || chal.Score != 20 || chal.Title != "切换2" {
		t.Fatalf("updated challenge = %+v", chal)
	}
	var q model.ChallengeQuestion
	if err := gdb.Where("challenge_id = ?", chal.ID).First(&q).Error; err != nil {
		t.Fatalf("question after update: %v", err)
	}
	if q.AnswerJSON != "false" || q.Explanation != "解析" {
		t.Fatalf("updated question = %+v", q)
	}

	// 改回 flag 题：question 行必须清理，flag 行重建。
	uploadQuestionJSON(t, svc, `{"slug":"switch","title":"切换3","category":"basics","difficulty":"apprentice","description_md":"flag","score":30,"challenge":{"type":"static","flag":"flag{x}"}}`)
	var count int64
	gdb.Model(&model.ChallengeQuestion{}).Where("challenge_id = ?", chal.ID).Count(&count)
	if count != 0 {
		t.Fatalf("question row should be deleted, got %d", count)
	}
	var flags int64
	gdb.Model(&model.Flag{}).Where("challenge_id = ?", chal.ID).Count(&flags)
	if flags != 1 {
		t.Fatalf("flags = %d, want 1", flags)
	}
}

func TestUploadBasicQuestionPackageYAML(t *testing.T) {
	svc, gdb := newContentTestService(t)
	yml := `
slug: basic-zip
title: 包里单选
category: basics
difficulty: apprentice
description: 选一个
score: 10
question:
  type: single
  options:
    - key: A
      text: 甲
    - key: B
      text: 乙
  answer: B
  explanation: 乙正确
hints:
  - title: 提示
    content: 看乙
    cost: 1
`
	imported, slugs, err := svc.UploadChallengePackage(context.Background(), buildZip(t, map[string]string{"challenge.yml": yml}))
	if err != nil {
		t.Fatalf("UploadChallengePackage: %v", err)
	}
	if imported != 1 || len(slugs) != 1 || slugs[0] != "basic-zip" {
		t.Fatalf("imported=%d slugs=%v", imported, slugs)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "basic-zip").First(&chal).Error; err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if chal.Bank != model.BankBasic || chal.Type != model.ChallengeSingle {
		t.Fatalf("challenge = %+v", chal)
	}
	var q model.ChallengeQuestion
	if err := gdb.Where("challenge_id = ?", chal.ID).First(&q).Error; err != nil {
		t.Fatalf("question: %v", err)
	}
	if q.AnswerJSON != `"B"` {
		t.Fatalf("answer = %s", q.AnswerJSON)
	}
	var hints int64
	gdb.Model(&model.Hint{}).Where("challenge_id = ?", chal.ID).Count(&hints)
	if hints != 1 {
		t.Fatalf("hints = %d, want 1", hints)
	}
}

func TestUploadACMQuestionKeepsHints(t *testing.T) {
	svc, gdb := newContentTestService(t)
	yml := `
slug: acm-hints
title: ACM 提示
category: basics
difficulty: apprentice
description: 求和
score: 10
bank: acm
judge:
  engine: yak
  time_limit_s: 1
  testdata:
    - name: sample
      sample: true
hints:
  - title: 提示
    content: 用加法
    cost: 1
`
	files := map[string]string{
		"challenge.yml":          yml,
		"testdata/sample.in":     "1 2\n",
		"testdata/sample.answer": "3\n",
	}
	_, _, err := svc.UploadChallengePackage(context.Background(), buildZip(t, files))
	if err != nil {
		t.Fatalf("UploadChallengePackage: %v", err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "acm-hints").First(&chal).Error; err != nil {
		t.Fatalf("challenge: %v", err)
	}
	var hints int64
	gdb.Model(&model.Hint{}).Where("challenge_id = ?", chal.ID).Count(&hints)
	if hints != 1 {
		t.Fatalf("ACM YAML hints = %d, want 1", hints)
	}
}

func TestUploadBasicQuestionRejectsBuiltinLikeUpload(t *testing.T) {
	// 非内置上传带 question 时 bank 必须 basic；显式 tutorial 仍拒绝。
	svc, _ := newContentTestService(t)
	raw := `{"slug":"basic-tutorial","title":"题","category":"basics","difficulty":"apprentice","description_md":"题干","score":1,"bank":"tutorial","question":{"type":"truefalse","answer":true}}`
	if _, _, err := svc.UploadChallenges(context.Background(), []byte(raw)); !errors.Is(err, ErrValidation) {
		t.Fatalf("tutorial bank question err = %v", err)
	}
}

func TestUploadBasicQuestionFlagRowsNotWrittenForCodeJSON(t *testing.T) {
	// JSON judge 路径历史上会写空 Flag 行；基础题/编程题都不应再写。
	svc, gdb := newContentTestService(t)
	raw := `{"slug":"code-flaghygiene","title":"代码题","category":"basics","difficulty":"apprentice","description_md":"求和","score":10,"bank":"acm","judge":{"engine":"yak","time_limit_s":1,"testdata":[{"name":"s","input":"1 2","output":"3","sample":true}]}}`
	if _, _, err := svc.UploadChallenges(context.Background(), []byte(raw)); err != nil {
		t.Fatalf("upload code: %v", err)
	}
	var chal model.Challenge
	gdb.Where("slug = ?", "code-flaghygiene").First(&chal)
	var flags int64
	gdb.Model(&model.Flag{}).Where("challenge_id = ?", chal.ID).Count(&flags)
	if flags != 0 {
		t.Fatalf("code challenge flags = %d, want 0", flags)
	}
	if !strings.Contains(chal.Slug, "code-") {
		t.Fatalf("unexpected slug")
	}
}
