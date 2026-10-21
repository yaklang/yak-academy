package service

// 部分得分制（ACM 逐组计分）单测：逐组全跑（不再首败即停）、通过组按权重
// 折算得分、未配置分值退化为全有全无、submission.judged 事件 payload 携带
// 逐组 groups 明细、scores.json 装配与校验。

import (
	"encoding/json"
	"testing"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// seedScoredACMChallenge 建 code 题（题面 100 分）+ 带分值数据组（scores 长度 = 组数）。
// 返回 service、challenge 与底层 gorm.DB（断言直查）。
func seedScoredACMChallenge(t *testing.T, slug string, scores []int) (*JudgeService, *model.Challenge, *gorm.DB) {
	t.Helper()
	s, gdb := newJudgeTestService(t)
	chal := &model.Challenge{
		Slug: slug, Title: "A+B", Category: "warmup", Bank: model.BankACM,
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeCode, Score: 100,
	}
	if err := gdb.Create(chal).Error; err != nil {
		t.Fatalf("seed challenge: %v", err)
	}
	conf := &model.ACMJudge{ChallengeID: chal.ID, Engine: "yak", TimeLimitS: 5, MemoryLimitMB: 128}
	data := make([]model.JudgeTestdata, 0, len(scores))
	for i, sc := range scores {
		data = append(data, model.JudgeTestdata{
			ChallengeID: chal.ID, Name: "0" + string(rune('1'+i)),
			Input: "1 2\n", Answer: "3\n", Sample: i == 0, Score: sc,
		})
	}
	if err := repo.NewChallengeRepo(gdb).UpsertACM(t.Context(), conf, data); err != nil {
		t.Fatalf("seed acm: %v", err)
	}
	return s, chal, gdb
}

func reloadSubmission(t *testing.T, gdb *gorm.DB, id uint) *model.Submission {
	t.Helper()
	var after model.Submission
	if err := gdb.First(&after, id).Error; err != nil {
		t.Fatalf("reload submission: %v", err)
	}
	return &after
}

// 矩阵：各组通过/失败组合 → 期望 score（题面 100）。
func TestJudgeCodePartialScoringMatrix(t *testing.T) {
	cases := []struct {
		name      string
		scores    []int
		outputs   []fakeRun
		wantAC    bool
		wantScore int
	}{
		{
			name: "全部通过全额", scores: []int{30, 70},
			outputs: []fakeRun{{output: "3\n"}, {output: "3\n"}},
			wantAC: true, wantScore: 100,
		},
		{
			name: "小权重组通过", scores: []int{30, 70},
			outputs: []fakeRun{{output: "3\n"}, {output: "999\n"}},
			wantAC: false, wantScore: 30,
		},
		{
			name: "大权重组通过", scores: []int{30, 70},
			outputs: []fakeRun{{output: "999\n"}, {output: "3\n"}},
			wantAC: false, wantScore: 70,
		},
		{
			name: "三组过一（权重 33/99）", scores: []int{33, 33, 33},
			outputs: []fakeRun{{output: "3\n"}, {output: "999\n"}, {output: "999\n"}},
			wantAC: false, wantScore: 33,
		},
		{
			name: "未配置分值：部分通过 = 0（全有全无）", scores: []int{0, 0},
			outputs: []fakeRun{{output: "3\n"}, {output: "999\n"}},
			wantAC: false, wantScore: 0,
		},
		{
			name: "未配置分值：全过 = 全额", scores: []int{0, 0},
			outputs: []fakeRun{{output: "3\n"}, {output: "3\n"}},
			wantAC: true, wantScore: 100,
		},
		{
			name: "TLE 组不给分", scores: []int{50, 50},
			outputs: []fakeRun{{err: ErrCodeTimeout}, {output: "3\n"}},
			wantAC: false, wantScore: 50,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, chal, gdb := seedScoredACMChallenge(t, "acm-scored", tc.scores)
			s.SetCodeEngine(&fakeCodeEngine{results: tc.outputs})
			sub := submitCode(t, s, 1, chal.ID)
			s.judge(t.Context(), sub.ID)
			after := reloadSubmission(t, gdb, sub.ID)
			if tc.wantAC {
				if after.Verdict != model.VerdictAC || after.Status != model.SubmissionCorrect {
					t.Fatalf("verdict/status = %s/%s, want ac/correct", after.Verdict, after.Status)
				}
			} else if after.Status != model.SubmissionIncorrect {
				t.Fatalf("status = %s, want incorrect", after.Status)
			}
			if after.Score != tc.wantScore {
				t.Fatalf("partial score = %d, want %d", after.Score, tc.wantScore)
			}
		})
	}
}

// partialScore 纯函数表驱动：折算、四舍五入与退化语义。
func TestPartialScoreMath(t *testing.T) {
	cases := []struct {
		full, passed, total int
		allPassed           bool
		want                int
	}{
		{100, 0, 100, true, 100},  // 全过 = 全额（权重未用到）
		{100, 30, 100, false, 30}, // 按权重
		{100, 1, 3, false, 33},    // (100+1)/3 = 33
		{100, 2, 3, false, 67},    // (200+1)/3 = 67
		{100, 0, 0, false, 0},     // 权重全 0 且部分过 = 0
		{100, 0, 0, true, 100},    // 权重全 0 但全过 = 全额
		{100, 50, 0, false, 0},     // total=0 除零守卫
		{55, 11, 20, false, 30},   // (605+10)/20 = 30.75 → 30
	}
	for _, c := range cases {
		if got := partialScore(c.full, c.passed, c.total, c.allPassed); got != c.want {
			t.Errorf("partialScore(%d,%d,%d,%v) = %d, want %d", c.full, c.passed, c.total, c.allPassed, got, c.want)
		}
	}
}

// 逐组全跑：第 2 组 WA 后第 3 组仍被执行（partial scoring 需要每组结论），
// verdict = 首个失败组的判定。
func TestJudgeCodeRunsAllGroupsAfterFailure(t *testing.T) {
	s, chal, gdb := seedScoredACMChallenge(t, "acm-runall", []int{10, 10, 80})
	engine := &fakeCodeEngine{results: []fakeRun{
		{output: "3\n"}, {output: "999\n"}, {output: "3\n"},
	}}
	s.SetCodeEngine(engine)
	sub := submitCode(t, s, 1, chal.ID)
	s.judge(t.Context(), sub.ID)

	if engine.runs != 3 {
		t.Fatalf("engine runs = %d, want 3 (all groups executed)", engine.runs)
	}
	after := reloadSubmission(t, gdb, sub.ID)
	if after.Verdict != model.VerdictWA || after.Score != 90 { // 10+80 权重通过
		t.Fatalf("verdict/score = %s/%d, want wa/90", after.Verdict, after.Score)
	}
}

// submission.judged 事件 payload 携带逐组 groups 明细（学生端反馈与审计
// 重放同源；Event 表 append-only 语义不变——只 INSERT 新事件）。
func TestJudgeCodeEventPayloadGroups(t *testing.T) {
	s, chal, gdb := seedScoredACMChallenge(t, "acm-groups", []int{40, 60})
	s.SetCodeEngine(&fakeCodeEngine{results: []fakeRun{
		{output: "3\n"}, {output: "999\n"},
	}})
	sub := submitCode(t, s, 1, chal.ID)
	s.judge(t.Context(), sub.ID)

	var events []model.Event
	if err := gdb.Where("type = ?", model.EventSubmissionJudged).Find(&events).Error; err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("judged events = %d, want 1", len(events))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(events[0].Payload), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	raw, ok := payload["groups"]
	if !ok {
		t.Fatal("event payload missing groups")
	}
	groups, ok := raw.([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("groups = %#v, want 2 entries", raw)
	}
	first, _ := groups[0].(map[string]any)
	if first == nil || first["name"] != "01" || first["passed"] != true {
		t.Fatalf("group[0] = %#v, want 01 passed", groups[0])
	}
	second, _ := groups[1].(map[string]any)
	if second == nil || second["verdict"] != "wa" {
		t.Fatalf("group[1] = %#v, want verdict wa", groups[1])
	}
}

// scores.json 装配：组合并/未知组拒绝/负分拒绝/坏 JSON 拒绝。
func TestAssembleTestdataScores(t *testing.T) {
	files := func(extra map[string][]byte) map[string][]byte {
		f := map[string][]byte{
			"challenge.yml":      []byte("x"),
			"testdata/01.in":     []byte("1 2\n"),
			"testdata/01.answer": []byte("3\n"),
			"testdata/02.in":     []byte("5 6\n"),
			"testdata/02.answer": []byte("11\n"),
		}
		for k, v := range extra {
			f[k] = v
		}
		return f
	}
	scoreOf := func(j *judgeJSON, name string) int {
		for _, d := range j.Testdata {
			if d.Name == name {
				return d.Score
			}
		}
		return -1
	}

	// 正常：组名 → 分值合并。
	j := &judgeJSON{Engine: "yak", TimeLimitS: 5}
	if err := assembleTestdata(j, files(map[string][]byte{
		"testdata/scores.json": []byte(`{"01": 30, "02": 70}`),
	})); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if scoreOf(j, "01") != 30 || scoreOf(j, "02") != 70 {
		t.Fatalf("scores = %d/%d, want 30/70", scoreOf(j, "01"), scoreOf(j, "02"))
	}

	// 未知组名 → 422。
	if err := assembleTestdata(&judgeJSON{}, files(map[string][]byte{
		"testdata/scores.json": []byte(`{"99": 10}`),
	})); err == nil {
		t.Fatal("unknown group in scores.json must be rejected")
	}

	// 负分 → 422。
	if err := assembleTestdata(&judgeJSON{}, files(map[string][]byte{
		"testdata/scores.json": []byte(`{"01": -5}`),
	})); err == nil {
		t.Fatal("negative score must be rejected")
	}

	// 坏 JSON → 422。
	if err := assembleTestdata(&judgeJSON{}, files(map[string][]byte{
		"testdata/scores.json": []byte(`{not json`),
	})); err == nil {
		t.Fatal("malformed scores.json must be rejected")
	}
}

// judge 段负分值校验（内联 JSON 路径与 scores.json 同一约束）。
func TestValidateJudgeJSONNegativeScore(t *testing.T) {
	j := &judgeJSON{Engine: "yak", TimeLimitS: 5, Testdata: []judgeTestData{
		{Name: "01", Input: "1\n", Output: "2\n", Score: -1},
	}}
	if err := validateJudgeJSON("challenge.yml", j); err == nil {
		t.Fatal("negative testdata score must be rejected")
	}
}