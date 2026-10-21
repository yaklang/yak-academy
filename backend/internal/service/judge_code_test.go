package service

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ---------- compareOutput / verdictFromError 纯函数 ----------

func TestCompareOutput(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
		ac   bool
	}{
		{"exact match", "3\n5\n", "3\n5\n", true},
		{"trailing spaces tolerated", "3 \n5\t\n", "3\n5\n", true},
		{"multi-space tokens tolerated", "3  5\n", "3 5\n", true},
		{"crlf vs lf", "3\r\n5\r\n", "3\n5\n", true},
		{"missing trailing newline", "3\n5", "3\n5\n", true},
		{"wrong value", "3\n6\n", "3\n5\n", false},
		{"extra line", "3\n5\n7\n", "3\n5\n", false},
		{"missing line", "3\n", "3\n5\n", false},
		{"empty vs nonempty", "", "3\n", false},
		{"both empty", "", "", true},
		{"tab-separated tokens match", "a\tb\n", "a b\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := compareOutput(c.got, c.want); got != c.ac {
				t.Fatalf("compareOutput(%q, %q) = %v, want %v", c.got, c.want, got, c.ac)
			}
		})
	}
}

func TestVerdictFromError(t *testing.T) {
	if v := verdictFromError(nil); v != model.VerdictAC {
		t.Fatalf("nil error verdict = %q, want ac", v)
	}
	if v := verdictFromError(ErrCodeTimeout); v != model.VerdictTLE {
		t.Fatalf("timeout verdict = %q, want tle", v)
	}
	if v := verdictFromError(&ErrCodeRunError{ExitCode: 1, Stderr: "panic"}); v != model.VerdictRE {
		t.Fatalf("run error verdict = %q, want re", v)
	}
}

// ---------- fakeCodeEngine（表驱动注入） ----------

// fakeCodeEngine 预设输出/错误序列：第 i 组数据返回第 i 个结果。
type fakeCodeEngine struct {
	runs    int
	results []fakeRun // 每组数据对应一个结果
}

type fakeRun struct {
	output string
	err    error
}

func (f *fakeCodeEngine) Run(ctx context.Context, source, input string) (string, error) {
	i := f.runs
	f.runs++
	if i >= len(f.results) {
		return "", errors.New("fake engine: no result preset")
	}
	return f.results[i].output, f.results[i].err
}

// ---------- 判题主流程（fake engine 驱动全链路） ----------

func newJudgeTestService(t *testing.T) (*JudgeService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:judgeacmtest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		dbg, _ := gdb.DB()
		dbg.Close()
	})
	chalRepo := repo.NewChallengeRepo(gdb)
	subRepo := repo.NewSubmissionRepo(gdb)
	instRepo := repo.NewLabInstanceRepo(gdb)
	evRepo := repo.NewEventRepo(gdb)
	compRepo := repo.NewCompetitionRepo(gdb)
	bus := eventbus.New()
	svc := NewJudgeService(subRepo, chalRepo, instRepo, evRepo, compRepo, bus)
	return svc, gdb
}

// seedACMChallenge 建一道 code 题（2 组数据：1 样例 1 隐藏），返回 challenge 行。
func seedACMChallenge(t *testing.T, gdb *gorm.DB, slug string, timeLimitS int) *model.Challenge {
	t.Helper()
	chal := &model.Challenge{
		Slug: slug, Title: "A+B", Category: "warmup", Bank: model.BankACM,
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeCode,
		Score: 50,
	}
	if err := gdb.Create(chal).Error; err != nil {
		t.Fatalf("seed challenge: %v", err)
	}
	conf := &model.ACMJudge{ChallengeID: chal.ID, Engine: "yak", TimeLimitS: timeLimitS, MemoryLimitMB: 128}
	data := []model.JudgeTestdata{
		{ChallengeID: chal.ID, Name: "01", Input: "1 2\n", Answer: "3\n", Sample: true},
		{ChallengeID: chal.ID, Name: "02", Input: "10 20\n", Answer: "30\n"},
	}
	if err := repo.NewChallengeRepo(gdb).UpsertACM(context.Background(), conf, data); err != nil {
		t.Fatalf("seed acm: %v", err)
	}
	return chal
}

func submitCode(t *testing.T, s *JudgeService, userID, chalID uint) *model.Submission {
	t.Helper()
	sub, err := s.Submit(context.Background(), userID, chalID, "println(42)")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return sub
}

func TestJudgeCodeAcceptedScoresOnce(t *testing.T) {
	s, gdb := newJudgeTestService(t)
	chal := seedACMChallenge(t, gdb, "acm-ab", 5)
	s.SetCodeEngine(&fakeCodeEngine{results: []fakeRun{
		{output: "3\n"}, {output: "30\n"},
	}})
	sub := submitCode(t, s, 1, chal.ID)
	s.judge(context.Background(), sub.ID)

	var after model.Submission
	if err := gdb.First(&after, sub.ID).Error; err != nil {
		t.Fatalf("reload submission: %v", err)
	}
	if after.Status != model.SubmissionCorrect || after.Verdict != model.VerdictAC {
		t.Fatalf("status/verdict = %s/%s, want correct/ac", after.Status, after.Verdict)
	}
	if after.Score != 50 {
		t.Fatalf("first AC score = %d, want 50 (dynamic decay initial)", after.Score)
	}

	// 重复提交 AC：不重复计分（score=0，verdict 仍 ac）。
	s.SetCodeEngine(&fakeCodeEngine{results: []fakeRun{
		{output: "3\n"}, {output: "30\n"},
	}})
	sub2 := submitCode(t, s, 2, chal.ID)
	s.judge(context.Background(), sub2.ID)
	var after2 model.Submission
	if err := gdb.First(&after2, sub2.ID).Error; err != nil {
		t.Fatalf("reload submission 2: %v", err)
	}
	if after2.Score != 50-10 { // 两人通过：动态计分 n=1 → 40
		t.Fatalf("second AC score = %d, want 40 (linear decay)", after2.Score)
	}
}

func TestJudgeCodeWrongAnswerStopsAtFirstFailure(t *testing.T) {
	s, gdb := newJudgeTestService(t)
	chal := seedACMChallenge(t, gdb, "acm-wa", 5)
	s.SetCodeEngine(&fakeCodeEngine{results: []fakeRun{
		{output: "3\n"},        // 样例组通过
		{output: "999\n"},      // 隐藏组 WA
	}})
	sub := submitCode(t, s, 1, chal.ID)
	s.judge(context.Background(), sub.ID)

	var after model.Submission
	if err := gdb.First(&after, sub.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Status != model.SubmissionIncorrect || after.Verdict != model.VerdictWA {
		t.Fatalf("status/verdict = %s/%s, want incorrect/wa", after.Status, after.Verdict)
	}
	if after.Score != 0 {
		t.Fatalf("WA score = %d, want 0", after.Score)
	}
}

func TestJudgeCodeTLEAndREVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result fakeRun
		want   model.SubmissionVerdict
	}{
		{"timeout on first case", fakeRun{err: ErrCodeTimeout}, model.VerdictTLE},
		{"runtime error on first case", fakeRun{err: &ErrCodeRunError{ExitCode: 2, Stderr: "panic"}}, model.VerdictRE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, gdb := newJudgeTestService(t)
			chal := seedACMChallenge(t, gdb, "acm-verdict", 5)
			s.SetCodeEngine(&fakeCodeEngine{results: []fakeRun{tc.result}})
			sub := submitCode(t, s, 1, chal.ID)
			s.judge(context.Background(), sub.ID)

			var after model.Submission
			if err := gdb.First(&after, sub.ID).Error; err != nil {
				t.Fatalf("reload: %v", err)
			}
			if after.Status != model.SubmissionIncorrect || after.Verdict != tc.want {
				t.Fatalf("status/verdict = %s/%s, want incorrect/%s", after.Status, after.Verdict, tc.want)
			}
		})
	}
}

func TestJudgeCodeEngineMissingInternalError(t *testing.T) {
	s, gdb := newJudgeTestService(t)
	chal := seedACMChallenge(t, gdb, "acm-noengine", 5)
	// 不注入引擎：平台侧失败 → internal_error。
	sub := submitCode(t, s, 1, chal.ID)
	s.judge(context.Background(), sub.ID)

	var after model.Submission
	if err := gdb.First(&after, sub.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Verdict != model.VerdictInternalError {
		t.Fatalf("verdict = %s, want internal_error", after.Verdict)
	}
	if after.Status != model.SubmissionIncorrect {
		t.Fatalf("status = %s, want incorrect (verdict 体现平台侧失败语义)", after.Status)
	}
}

func TestSubmitCodeRejectsPendingDuplicateAndSizeLimit(t *testing.T) {
	s, gdb := newJudgeTestService(t)
	chal := seedACMChallenge(t, gdb, "acm-dup", 5)

	// flag 题不受源码大小限制：先造一道 static 题验证直通语义。
	staticChal := &model.Challenge{
		Slug: "flag-chal", Title: "F", Category: "ctf", Bank: model.BankCTF,
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeStatic, Score: 10,
	}
	if err := gdb.Create(staticChal).Error; err != nil {
		t.Fatalf("seed static: %v", err)
	}
	if _, err := s.Submit(context.Background(), 1, staticChal.ID, bigSource(maxCodeSource+1)); err != nil {
		t.Fatalf("flag challenge should not enforce source size limit: %v", err)
	}

	// 超限源码：422 校验失败。
	if _, err := s.Submit(context.Background(), 1, chal.ID, bigSource(maxCodeSource+1)); err == nil {
		t.Fatal("oversized source must be rejected")
	} else if !errors.Is(err, ErrValidation) {
		t.Fatalf("oversized source err = %v, want ErrValidation", err)
	}

	// pending 期间重复提交：422（防并发刷引擎）。
	if _, err := s.Submit(context.Background(), 1, chal.ID, "println(1)"); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if _, err := s.Submit(context.Background(), 1, chal.ID, "println(2)"); err == nil {
		t.Fatal("pending duplicate must be rejected")
	} else if !errors.Is(err, ErrValidation) {
		t.Fatalf("pending duplicate err = %v, want ErrValidation", err)
	}
}

func bigSource(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

// ---------- 上传校验（judge 段互斥/必填/bank 门控） ----------

func TestValidateChallengeJSONJudgeRules(t *testing.T) {
	svc := &ContentService{} // validateChallengeJSON 不触碰 service 字段，零依赖即可调用
	flagSection := func() (c challengeJSON) {
		c.Slug = "s-1"
		c.Title = "T"
		c.Category = "cat"
		c.Difficulty = model.DifficultyApprentice
		c.Score = 10
		c.Challenge.Type = model.ChallengeStatic
		c.Challenge.Flag = "flag{x}"
		return c
	}
	judgeSection := func() (c challengeJSON) {
		c = flagSection()
		c.Challenge = struct {
			Type model.ChallengeType `json:"type"`
			Flag string              `json:"flag"`
		}{} // 无 flag 段
		c.Bank = model.BankACM
		c.Judge = &judgeJSON{Engine: "yak", TimeLimitS: 5, Testdata: []judgeTestData{
			{Name: "01", Input: "1\n", Output: "2\n"},
		}}
		return c
	}

	if err := svc.validateChallengeJSON(0, flagSection(), map[string]bool{}, false); err != nil {
		t.Fatalf("pure flag challenge should pass: %v", err)
	}
	if err := svc.validateChallengeJSON(0, judgeSection(), map[string]bool{}, false); err != nil {
		t.Fatalf("acm judge challenge should pass: %v", err)
	}

	// judge + challenge 互斥。
	both := judgeSection()
	both.Challenge.Type = model.ChallengeStatic
	both.Challenge.Flag = "flag{x}"
	if err := svc.validateChallengeJSON(0, both, map[string]bool{}, false); err == nil {
		t.Fatal("judge + challenge sections must be mutually exclusive")
	}

	// judge 段在 ctf 题 → 422。
	ctfJudge := judgeSection()
	ctfJudge.Bank = model.BankCTF
	if err := svc.validateChallengeJSON(0, ctfJudge, map[string]bool{}, false); err == nil {
		t.Fatal("judge section requires bank=acm")
	}

	// engine 枚举 / 时限范围。
	badEngine := judgeSection()
	badEngine.Judge.Engine = "python"
	if err := svc.validateChallengeJSON(0, badEngine, map[string]bool{}, false); err == nil {
		t.Fatal("engine must be yak")
	}
	badLimit := judgeSection()
	badLimit.Judge.TimeLimitS = 0
	if err := svc.validateChallengeJSON(0, badLimit, map[string]bool{}, false); err == nil {
		t.Fatal("time_limit_s must be >= 1")
	}

	// bank=acm 但没有 judge 段（也没 flag 段）→ 422。
	acmNoJudge := judgeSection()
	acmNoJudge.Judge = nil
	acmNoJudge.Challenge = struct {
		Type model.ChallengeType `json:"type"`
		Flag string              `json:"flag"`
	}{}
	if err := svc.validateChallengeJSON(0, acmNoJudge, map[string]bool{}, false); err == nil {
		t.Fatal("bank=acm requires judge section")
	}
}

func TestAssembleTestdataPairs(t *testing.T) {
	files := map[string][]byte{
		"challenge.yml":      []byte("x"),
		"testdata/01.in":     []byte("1 2\n"),
		"testdata/01.answer": []byte("3\n"),
		"testdata/02.in":     []byte("5 6\n"),
		"testdata/02.answer": []byte("11\n"),
	}
	j := &judgeJSON{Engine: "yak", TimeLimitS: 5}
	if err := assembleTestdata(j, files); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if len(j.Testdata) != 2 {
		t.Fatalf("assembled groups = %d, want 2", len(j.Testdata))
	}
	byName := map[string]judgeTestData{}
	for _, d := range j.Testdata {
		byName[d.Name] = d
	}
	if byName["01"].Input != "1 2\n" || byName["01"].Output != "3\n" {
		t.Fatalf("group 01 content mismatch: %+v", byName["01"])
	}

	// 缺 .answer 配对 → 422。
	broken := map[string][]byte{"testdata/03.in": []byte("x\n")}
	if err := assembleTestdata(j, broken); err == nil {
		t.Fatal("missing .answer pair must fail")
	}
	// 孤儿 .answer → 422。
	orphan := map[string][]byte{"testdata/04.answer": []byte("x\n")}
	if err := assembleTestdata(j, orphan); err == nil {
		t.Fatal("orphan .answer must fail")
	}
	// yml 声明组但包内无文件 → 422。
	declared := &judgeJSON{Engine: "yak", TimeLimitS: 5, Testdata: []judgeTestData{{Name: "99"}}}
	if err := assembleTestdata(declared, map[string][]byte{}); err == nil {
		t.Fatal("declared group without files must fail")
	}
}