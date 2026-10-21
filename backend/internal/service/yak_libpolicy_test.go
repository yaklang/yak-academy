package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ---------- 纯函数：字面量剥除 / 库标识符扫描 ----------

func TestStripYakLiterals(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // 剥除后是否仍含该标识符
		keep bool
	}{
		{"double-quoted string", `println("exec.Command")`, "exec", false},
		{"single-quoted char", `c := 'x'`, "x", false},
		{"raw backtick", "println(`exec`)", "exec", false},
		{"f-string", `println(f"{$exec}")`, "exec", false},
		{"line comment", "// exec is bad\nprintln(1)", "exec", false},
		{"block comment", "/* exec */ println(1)", "exec", false},
		{"escaped quote", `println("a\"b") ; exec.Command("id")`, "exec", true},
		{"member access kept", `println(exec.Command("id"))`, "exec", true},
		{"bare identifier kept", `a := exec`, "exec", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Contains(stripYakLiterals(c.src), c.want)
			if got != c.keep {
				t.Fatalf("stripYakLiterals(%q) = %q, contains %q = %v, want %v", c.src, stripYakLiterals(c.src), c.want, got, c.keep)
			}
		})
	}
}

func TestUsedYakLibs(t *testing.T) {
	src := `
println("codec is a lib") // codec in comment
res := io.ReadAll(os.Stdin)
alias := exec
println(str.SplitAndTrim("a b", " "))
notALib.Variable
`
	got := usedYakLibs(src)
	want := []string{"exec", "io", "os", "str"}
	if len(got) != len(want) {
		t.Fatalf("usedYakLibs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("usedYakLibs = %v, want %v", got, want)
		}
	}
}

func TestUsedYakLibsSorted(t *testing.T) {
	got := usedYakLibs("str.Join() codec.EncodeToHex() poc.Get()")
	if !sort.StringsAreSorted(got) {
		t.Fatalf("usedYakLibs not sorted: %v", got)
	}
}

// ---------- 纯函数：策略校验 / 序列化 ----------

func TestCheckYakLibPolicy(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		allowed []string
		wantErr bool
		substr  string
	}{
		{"clean source", `println(1 + 2)`, nil, false, ""},
		{"builtin only", `println(str.ParseStringToLines("x"))`, nil, false, ""},
		{"blacklisted exec", `exec.Command("id")`, nil, true, "blacklisted"},
		{"blacklisted alias form", `a := exec`, nil, true, "blacklisted"},
		{"blacklisted file", `file.IsDir("/etc")`, nil, true, "blacklisted"},
		{"blacklisted inside string ok", `println("exec.Command")`, nil, false, ""},
		{"outside whitelist", `codec.EncodeToHex("a")`, []string{"str", "io", "os"}, true, "whitelist"},
		{"inside whitelist", `str.SplitAndTrim("a b", " ")`, []string{"str", "io", "os"}, false, ""},
		{"whitelist empty means all allowed", `codec.EncodeToHex("a")`, nil, false, ""},
		{"whitelist set empty slice = unset", `codec.EncodeToHex("a")`, []string{}, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := checkYakLibPolicy(c.src, c.allowed)
			if (err != nil) != c.wantErr {
				t.Fatalf("checkYakLibPolicy err = %v, wantErr %v", err, c.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), c.substr) {
				t.Fatalf("error %q should contain %q", err.Error(), c.substr)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("error should wrap ErrValidation, got %v", err)
			}
		})
	}
}

func TestCheckYakLibPolicyBlacklistEnvOverride(t *testing.T) {
	t.Setenv(envYakLibBlacklist, "str,codec")
	if err := checkYakLibPolicy(`str.Join([]string{"a"})`, nil); err == nil {
		t.Fatal("str should be blocked via env override")
	}
	// 默认黑名单成员在覆盖后不再拦截（覆盖语义 = 替换）。
	if err := checkYakLibPolicy(`exec.Command("id")`, nil); err != nil {
		t.Fatalf("exec should pass after blacklist override removed it: %v", err)
	}
}

func TestValidateAllowedLibs(t *testing.T) {
	if err := validateAllowedLibs("challenge[0]", []string{"str", "io", "os", "json"}); err != nil {
		t.Fatalf("valid libs rejected: %v", err)
	}
	if err := validateAllowedLibs("challenge[0]", []string{"notalib"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown lib should 422, got %v", err)
	}
	if err := validateAllowedLibs("challenge[0]", []string{"exec"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("blacklisted lib should 422, got %v", err)
	}
	if err := validateAllowedLibs("challenge[0]", []string{"str", "str"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate lib should 422, got %v", err)
	}
}

func TestAllowedLibsRoundTrip(t *testing.T) {
	if got := parseAllowedLibs(serializeAllowedLibs([]string{"str", "io"})); len(got) != 2 || got[0] != "str" || got[1] != "io" {
		t.Fatalf("round trip mismatch: %v", got)
	}
	if got := serializeAllowedLibs(nil); got != "" {
		t.Fatalf("nil should serialize to empty, got %q", got)
	}
	if got := parseAllowedLibs(""); got != nil {
		t.Fatalf("empty column should parse to nil, got %v", got)
	}
	if got := parseAllowedLibs("not-json"); got != nil {
		t.Fatalf("bad column should parse to nil, got %v", got)
	}
}

// ---------- 受理拦截（提交 / 自测运行） ----------

// newLibPolicyTestService 本文件专属内存库（project-conventions：测试文件间
// 禁止共用 cache=shared 库名）。
func newLibPolicyTestService(t *testing.T) (*JudgeService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:yaklibpolicy?mode=memory&cache=shared"), &gorm.Config{})
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
	bus := eventbus.New()
	return NewJudgeService(
		repo.NewSubmissionRepo(gdb), repo.NewChallengeRepo(gdb),
		repo.NewLabInstanceRepo(gdb), repo.NewEventRepo(gdb),
		repo.NewCompetitionRepo(gdb), bus,
	), gdb
}

func TestSubmitCodeRejectsBlacklistedLib(t *testing.T) {
	svc, gdb := newLibPolicyTestService(t)
	chal := seedACMChallenge(t, gdb, "acm-exec-block", 5)
	ctx := context.Background()

	_, err := svc.Submit(ctx, 1, chal.ID, `exec.Command("whoami")`)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("blacklisted lib submission should 422, got %v", err)
	}
	// 未落任何 submission 行（拦截发生在入库前）。
	var n int64
	gdb.Model(&model.Submission{}).Count(&n)
	if n != 0 {
		t.Fatalf("rejected submission should not persist, got %d rows", n)
	}
}

func TestSubmitCodeRejectsLibOutsideWhitelist(t *testing.T) {
	svc, gdb := newLibPolicyTestService(t)
	chal := seedACMChallenge(t, gdb, "acm-wl", 5)
	// 题目白名单只放 str/io/os。
	gdb.Model(&model.ACMJudge{}).Where("challenge_id = ?", chal.ID).
		Update("allowed_libs", `["str","io","os"]`)

	ctx := context.Background()
	if _, err := svc.Submit(ctx, 1, chal.ID, `codec.EncodeToHex("a")`); !errors.Is(err, ErrValidation) {
		t.Fatalf("codec outside whitelist should 422, got %v", err)
	}
	// 白名单内 + 内建函数：受理成功（pending 落库）。
	sub, err := svc.Submit(ctx, 1, chal.ID, `println(str.Join(["a"], ","))`)
	if err != nil {
		t.Fatalf("whitelisted submission should be accepted: %v", err)
	}
	if sub.Status != model.SubmissionPending {
		t.Fatalf("accepted submission status = %q, want pending", sub.Status)
	}
}

func TestSubmitCodeUnconfiguredWhitelistAllowsCommonLibs(t *testing.T) {
	svc, gdb := newLibPolicyTestService(t)
	chal := seedACMChallenge(t, gdb, "acm-open", 5)
	// 未配置白名单：非黑名单库全放行。
	if _, err := svc.Submit(context.Background(), 1, chal.ID, `println(codec.EncodeToHex("a"))`); err != nil {
		t.Fatalf("unconfigured whitelist should allow codec: %v", err)
	}
}

func TestCreateCodeRunRejectsBlacklistedLib(t *testing.T) {
	svc, gdb := newLibPolicyTestService(t)
	chal := seedACMChallenge(t, gdb, "acm-run-block", 5)
	svc.SetCodeEngine(&fakeCodeEngine{results: []fakeRun{{output: "ok\n"}}})

	ctx := context.Background()
	_, err := svc.CreateCodeRun(ctx, 1, chal.Slug, `println(exec.Command("id"))`, "01", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("self-test with blacklisted lib should 422, got %v", err)
	}
	// 正常源码仍可自测受理（拦截只挡违规源码，不误伤正常自测路径）。
	id, err := svc.CreateCodeRun(ctx, 1, chal.Slug, `println(1)`, "01", "")
	if err != nil {
		t.Fatalf("clean self-test should be accepted: %v", err)
	}
	if id == "" {
		t.Fatal("run id should not be empty")
	}
}

// ---------- 上传管线：白名单解析校验 / 落库 / 视图回显 ----------

// newLibPolicyContentEnv 同一内存库上装配 content/challenge/topic 三服务
//（上传 → 落库 → 详情/编辑回显全链路）。
func newLibPolicyContentEnv(t *testing.T) (*ContentService, *ChallengeService, *TopicService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:yaklibpolicy2?mode=memory&cache=shared"), &gorm.Config{})
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
	chalSvc := NewChallengeService(chalRepo, repo.NewSubmissionRepo(gdb), repo.NewEventRepo(gdb), repo.NewScoreRepo(gdb), nil)
	contentSvc := NewContentService(
		repo.NewContentPackageRepo(gdb), repo.NewTopicRepo(gdb), repo.NewRoomRepo(gdb),
		chalSvc, &fakeImageBuilder{}, repo.NewProgressRepo(gdb),
	)
	topicSvc := NewTopicService(repo.NewTopicRepo(gdb), repo.NewRoomRepo(gdb), chalRepo,
		repo.NewSubmissionRepo(gdb), repo.NewEventRepo(gdb))
	return contentSvc, chalSvc, topicSvc, gdb
}

// acmChallengeJSON 带 judge 段与白名单的最小 ACM 题 JSON（直传内联数据组）。
func acmChallengeJSON(slug string, allowedLibs string) string {
	return `{
		"slug":"` + slug + `","title":"A+B","category":"warmup","bank":"acm",
		"difficulty":"apprentice","score":50,
		"judge":{
			"engine":"yak","time_limit_s":5,"memory_limit_mb":128,
			"allowed_libs":` + allowedLibs + `,
			"testdata":[{"name":"01","input":"1 2\n","output":"3\n","sample":true}]
		}
	}`
}

func TestUploadChallengesPersistsAllowedLibs(t *testing.T) {
	contentSvc, chalSvc, topicSvc, gdb := newLibPolicyContentEnv(t)
	ctx := context.Background()

	if _, _, err := contentSvc.UploadChallenges(ctx, []byte(
		acmChallengeJSON("acm-wl-persist", `["str","io","os","json"]`),
	)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "acm-wl-persist").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	var conf model.ACMJudge
	if err := gdb.Where("challenge_id = ?", chal.ID).First(&conf).Error; err != nil {
		t.Fatal(err)
	}
	if got := parseAllowedLibs(conf.AllowedLibs); len(got) != 4 || got[0] != "str" || got[3] != "json" {
		t.Fatalf("persisted allowed_libs = %v, want [str io os json]", got)
	}
	// 学生端详情：allowed_libs 原样回显 + blocked_libs 下发。
	detail, err := topicSvc.GetChallengeDetail(ctx, 1, "acm-wl-persist")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Judge == nil {
		t.Fatal("detail.judge missing")
	}
	if len(detail.Judge.AllowedLibs) != 4 || detail.Judge.AllowedLibs[0] != "str" {
		t.Fatalf("detail allowed_libs = %v", detail.Judge.AllowedLibs)
	}
	if !containsStr(detail.Judge.BlockedLibs, "exec") || !containsStr(detail.Judge.BlockedLibs, "file") {
		t.Fatalf("detail blocked_libs missing exec/file: %v", detail.Judge.BlockedLibs)
	}
	// 管理端编辑回填往返。
	full, err := chalSvc.GetFullJSON(ctx, "acm-wl-persist")
	if err != nil {
		t.Fatalf("full json: %v", err)
	}
	if full.Judge == nil || len(full.Judge.AllowedLibs) != 4 || full.Judge.AllowedLibs[2] != "os" {
		t.Fatalf("full json allowed_libs = %+v", full.Judge)
	}
}

func TestUploadChallengesUnconfiguredWhitelistStaysNull(t *testing.T) {
	contentSvc, _, topicSvc, _ := newLibPolicyContentEnv(t)
	ctx := context.Background()
	if _, _, err := contentSvc.UploadChallenges(ctx, []byte(
		acmChallengeJSON("acm-wl-null", `[]`),
	)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	detail, err := topicSvc.GetChallengeDetail(ctx, 1, "acm-wl-null")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Judge.AllowedLibs != nil {
		t.Fatalf("unconfigured whitelist should be nil, got %v", detail.Judge.AllowedLibs)
	}
}

func TestUploadChallengesRejectsBadAllowedLibs(t *testing.T) {
	contentSvc, _, _, _ := newLibPolicyContentEnv(t)
	ctx := context.Background()
	for _, libs := range []string{`["notalib"]`, `["str","exec"]`, `["str","str"]`} {
		if _, _, err := contentSvc.UploadChallenges(ctx, []byte(acmChallengeJSON("acm-wl-bad", libs))); !errors.Is(err, ErrValidation) {
			t.Fatalf("allowed_libs %s should 422, got %v", libs, err)
		}
	}
}

func TestUploadChallengePackageYamlAllowedLibs(t *testing.T) {
	contentSvc, _, _, gdb := newLibPolicyContentEnv(t)
	ctx := context.Background()
	zp := buildZip(t, map[string]string{
		"challenge.yml": `
slug: acm-wl-yml
title: A+B (yml)
category: warmup
bank: acm
difficulty: apprentice
score: 50
judge:
  engine: yak
  time_limit_s: 5
  memory_limit_mb: 128
  allowed_libs: [str, io, os]
  testdata:
    - name: 01
      sample: true
`,
		"testdata/01.in":        "1 2\n",
		"testdata/01.answer":    "3\n",
	})
	if _, _, err := contentSvc.UploadChallengePackage(ctx, zp); err != nil {
		t.Fatalf("upload package: %v", err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "acm-wl-yml").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	var conf model.ACMJudge
	if err := gdb.Where("challenge_id = ?", chal.ID).First(&conf).Error; err != nil {
		t.Fatal(err)
	}
	if got := parseAllowedLibs(conf.AllowedLibs); len(got) != 3 || got[1] != "io" {
		t.Fatalf("yml allowed_libs = %v, want [str io os]", got)
	}
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
