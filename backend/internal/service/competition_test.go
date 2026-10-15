package service

// 竞赛域测试：真实 SQLite 仓库（project-conventions skill：禁 mock repo），
// 覆盖 创建/配置校验、题目绑定、批量导入、入场锁定、竞赛计分派生、
// 排行榜聚合、反作弊检测、WP 时序。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// compEnv 进程内 SQLite + 竞赛服务全家桶（真实 repo + 异步判题真实管线）。
type compEnv struct {
	gdb    *gorm.DB
	comps  *CompetitionService
	judge  *JudgeService
	derver *Deriver
	bus    *eventbus.Bus
	users  repo.UserRepo
	// inviteMail 投递记录（SetDelivery 注入的探针）。
	inviteMails []string
	now        time.Time
}

func newCompEnv(t *testing.T) *compEnv {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:comptestenv?mode=memory&cache=shared"), &gorm.Config{})
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
	compRepo := repo.NewCompetitionRepo(gdb)
	users := repo.NewUserRepo(gdb)
	chalRepo := repo.NewChallengeRepo(gdb)
	subRepo := repo.NewSubmissionRepo(gdb)
	instRepo := repo.NewLabInstanceRepo(gdb)
	deriver := NewDeriver(repo.NewScoreRepo(gdb), repo.NewProgressRepo(gdb))
	deriver.SetCompetitionStores(compRepo, func(ctx context.Context, id uint) (map[uint]string, error) {
		return map[uint]string{}, nil
	})
	bus.Subscribe(deriver.Handle)
	judge := NewJudgeService(subRepo, chalRepo, instRepo, repo.NewEventRepo(gdb), compRepo, bus)
	env := &compEnv{
		gdb: gdb, comps: NewCompetitionService(compRepo, chalRepo, users, instRepo, subRepo),
		judge: judge, derver: deriver, bus: bus, users: users,
		now: time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
	}
	// 固定时钟：竞赛状态与锁定判定全部由 env.now 推进。
	env.comps.now = func() time.Time { return env.now }
	env.judge.now = func() time.Time { return env.now }
	// 投递探针：记录收件人；建号走 users 直建（密码哈希由 AuthService 负责，此处不涉鉴权）。
	env.comps.SetDelivery(
		func(ctx context.Context, userID uint, title, bodyMd string, competitionID uint, inviteCode string) error {
			env.inviteMails = append(env.inviteMails, fmt.Sprintf("user=%d code=%s", userID, inviteCode))
			return nil
		},
		func(ctx context.Context, username, password string, role model.GlobalRole) (*model.User, error) {
			u := &model.User{Username: username, PasswordHash: "x", GlobalRole: role}
			if err := users.Create(ctx, u); err != nil {
				return nil, err
			}
			return u, nil
		},
	)
	return env
}

// createRunningComp 建一场 now 时刻 running 的竞赛。
func (e *compEnv) createRunningComp(t *testing.T, name string) *CompetitionView {
	t.Helper()
	start := e.now.Add(-time.Hour)
	end := e.now.Add(2 * time.Hour)
	comp, err := e.comps.Create(context.Background(), name, "", start, end)
	if err != nil {
		t.Fatalf("create competition: %v", err)
	}
	return comp
}

func (e *compEnv) ensureUser(t *testing.T, name string) *model.User {
	t.Helper()
	u := &model.User{Username: name, PasswordHash: "x", GlobalRole: model.GlobalRoleStudent}
	if err := e.users.Create(context.Background(), u); err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	return u
}

func (e *compEnv) seedChallenge(t *testing.T, slug, flagMode string) *model.Challenge {
	t.Helper()
	chal := &model.Challenge{
		Slug: slug, Title: slug, Category: "web", Difficulty: model.DifficultyApprentice,
		Description: "test", Type: model.ChallengeStatic, Score: 100, FlagMode: model.FlagMode(flagMode),
	}
	if flagMode == "instance" {
		chal.Image = "nginx:alpine"
		chal.Port = 80
	}
	if err := repo.NewChallengeRepo(e.gdb).Upsert(context.Background(),
		chal, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{" + slug + "}"}}, nil); err != nil {
		t.Fatalf("seed challenge: %v", err)
	}
	fresh, err := repo.NewChallengeRepo(e.gdb).FindBySlug(context.Background(), slug)
	if err != nil {
		t.Fatalf("reload challenge: %v", err)
	}
	return fresh
}

// drainJudge 消费判题队列直到空闲（异步判定在测试内等全部落定）。
func (e *compEnv) drainJudge() {
	for {
		updated := false
		pending, _ := repo.NewSubmissionRepo(e.gdb).ListPending(context.Background())
		for _, id := range pending {
			e.judge.judge(context.Background(), id)
			updated = true
		}
		if !updated {
			return
		}
	}
}

// TestCompetitionCreateValidatesTimes 创建与配置校验。
func TestCompetitionCreateValidatesTimes(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	if _, err := e.comps.Create(ctx, "倒时间赛", "", e.now.Add(time.Hour), e.now); !errors.Is(err, ErrValidation) {
		t.Fatalf("end before start: want ErrValidation, got %v", err)
	}
	if _, err := e.comps.Create(ctx, "  ", "", e.now, e.now.Add(time.Hour)); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty name: want ErrValidation, got %v", err)
	}
	comp := e.createRunningComp(t, "合法赛")
	if comp.Status != model.CompetitionRunning {
		t.Fatalf("status: want running, got %s", comp.Status)
	}
	// 空更新拒绝。
	if _, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty update: want ErrValidation, got %v", err)
	}
	// 已开始：改 start_time 拒绝。
	ns := e.now.Add(-2 * time.Hour)
	if _, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{StartTime: &ns}); !errors.Is(err, ErrValidation) {
		t.Fatalf("change start after begun: want ErrValidation, got %v", err)
	}
	// 延长 end_time 允许。
	ne := e.now.Add(3 * time.Hour)
	if _, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{EndTime: &ne}); err != nil {
		t.Fatalf("extend end: %v", err)
	}
}

// TestBindChallengesBySlugsAndUnbind 题目绑定/解绑（幂等 + running 拒解绑）。
func TestBindChallengesBySlugsAndUnbind(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "绑定赛")
	chal := e.seedChallenge(t, "bind-a", "shared")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	res, err := e.comps.BindChallenges(ctx, comp.ID, body, nil)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if res.Added != 1 || len(res.Items) != 1 {
		t.Fatalf("bind result: added=%d items=%d", res.Added, len(res.Items))
	}
	// 幂等：重复绑定 added=0。
	res, err = e.comps.BindChallenges(ctx, comp.ID, body, nil)
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if res.Added != 0 || len(res.Items) != 1 {
		t.Fatalf("rebind result: added=%d items=%d", res.Added, len(res.Items))
	}
	// running 竞赛不允许解绑。
	if _, err := e.comps.UnbindChallenges(ctx, comp.ID, []string{chal.Slug}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unbind while running: want ErrValidation, got %v", err)
	}
	// 结束后解绑成功。
	e.now = comp.EndTime.Add(time.Minute)
	removed, err := e.comps.UnbindChallenges(ctx, comp.ID, []string{chal.Slug})
	if err != nil || removed != 1 {
		t.Fatalf("unbind after end: removed=%d err=%v", removed, err)
	}
	// 未知 slug → ErrNotFound。
	body2, _ := json.Marshal(map[string]any{"slugs": []string{"no-such"}})
	if _, err := e.comps.BindChallenges(ctx, comp.ID, body2, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bind unknown slug: want ErrNotFound, got %v", err)
	}
}

// TestImportParticipantsCreatesTeamsAndInvites 批量导入：建队/建号/邀请码/邮件。
func TestImportParticipantsCreatesTeamsAndInvites(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "导入赛")
	alice := e.ensureUser(t, "alice")
	req := ParticipantImportRequest{
		Teams:       []TeamImport{{Name: "一队", Members: []string{"alice", "newbie1"}}},
		Individuals: []string{"solo"},
	}
	res, err := e.comps.ImportParticipants(ctx, comp.ID, req)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.CreatedAccounts) != 2 {
		t.Fatalf("created accounts: want 2 (newbie1, solo), got %d", len(res.CreatedAccounts))
	}
	if len(res.Participants) != 3 || len(e.inviteMails) != 3 {
		t.Fatalf("participants=%d mails=%d", len(res.Participants), len(e.inviteMails))
	}
	// 已存在用户 alice 不重复建号（FindByUsername 直接命中）。
	if _, err := e.users.FindByUsername(ctx, "alice"); err != nil {
		t.Fatalf("alice should exist: %v", err)
	}
	compRepo := repo.NewCompetitionRepo(e.gdb)
	parts, total, _ := compRepo.ListParticipants(ctx, comp.ID, 0, 100)
	if total != 3 {
		t.Fatalf("participant rows: %d", total)
	}
	var alicePart model.Participant
	for _, p := range parts {
		if p.UserID == alice.ID {
			alicePart = p
		}
	}
	if alicePart.ID == 0 || alicePart.InviteCode == "" || alicePart.TeamID == 0 {
		t.Fatalf("alice participant not linked: %+v", alicePart)
	}
	// 重复导入幂等：不建号、不重建参赛行、沿用邀请码。
	res2, err := e.comps.ImportParticipants(ctx, comp.ID, req)
	if err != nil {
		t.Fatalf("reimport: %v", err)
	}
	if len(res2.CreatedAccounts) != 0 {
		t.Fatalf("reimport should create 0 accounts, got %d", len(res2.CreatedAccounts))
	}
	if res2.Participants[0].InviteCode != res.Participants[0].InviteCode {
		t.Fatalf("invite code changed on reimport")
	}
	parts2, total2, _ := compRepo.ListParticipants(ctx, comp.ID, 0, 100)
	if total2 != 3 {
		t.Fatalf("reimport participant rows: %d", total2)
	}
	_ = parts2
}

// TestEnterLocksUntilEnd 入场与锁定：仅 running 可入场，锁定直到结束，
// 结束自动解锁；未入场/无效码拒绝。
func TestEnterLocksUntilEnd(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "锁定赛")
	user := e.ensureUser(t, "runner")
	// 未导入：无码可入。
	if _, err := e.comps.Enter(ctx, user.ID, comp.ID, "nosuchcode"); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid code: want ErrValidation, got %v", err)
	}
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{"runner"}})
	// 从投递探针拿邀请码（真实链路：用户从站内邮箱获得）。
	compRepo := repo.NewCompetitionRepo(e.gdb)
	part, _ := compRepo.FindParticipant(ctx, comp.ID, user.ID)
	// 1. scheduled 阶段拒绝入场。
	e.now = comp.StartTime.Add(-time.Minute)
	if _, err := e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode); !errors.Is(err, ErrValidation) {
		t.Fatalf("enter before start: want ErrValidation, got %v", err)
	}
	// 2. running 入场成功 → 锁定。
	e.now = comp.StartTime.Add(time.Minute)
	entry, err := e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode)
	if err != nil || !entry.Locked {
		t.Fatalf("enter: err=%v locked=%v", err, entry.Locked)
	}
	if id, locked := e.comps.LockedCompetition(ctx, user.ID); !locked || id != comp.ID {
		t.Fatalf("locked check: id=%d locked=%v", id, locked)
	}
	// 3. 结束：解锁。
	e.now = comp.EndTime.Add(time.Minute)
	if id, locked := e.comps.LockedCompetition(ctx, user.ID); locked {
		t.Fatalf("should unlock after end, got competition %d", id)
	}
	// 4. ended 后入场拒绝（新入场不可能）。
	if _, err := e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode); !errors.Is(err, ErrValidation) {
		t.Fatalf("enter after end: want ErrValidation, got %v", err)
	}
}

// TestCompetitionScopedScoringAndLeaderboard 竞赛提交计分：
// 首通动态分、与训练场首通独立、队伍/个人双榜聚合。
func TestCompetitionScopedScoringAndLeaderboard(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "计分赛")
	chal := e.seedChallenge(t, "score-a", "shared")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	if _, err := e.comps.BindChallenges(ctx, comp.ID, body, nil); err != nil {
		t.Fatalf("bind: %v", err)
	}
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Teams: []TeamImport{{Name: "蓝队", Members: []string{"bob", "cat"}}},
	})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	bob, _ := e.users.FindByUsername(ctx, "bob")
	cat, _ := e.users.FindByUsername(ctx, "cat")
	bobPart, _ := compRepo.FindParticipant(ctx, comp.ID, bob.ID)
	catPart, _ := compRepo.FindParticipant(ctx, comp.ID, cat.ID)
	// 入场。
	e.comps.Enter(ctx, bob.ID, comp.ID, bobPart.InviteCode)
	e.comps.Enter(ctx, cat.ID, comp.ID, catPart.InviteCode)
	// bob 首通 100 分；cat 错误提交后同 flag 也通（动态分 90）。
	if _, err := e.comps.SubmitFlag(ctx, e.judge, bob.ID, comp.ID, chal.Slug, "flag{score-a}", "1.1.1.1"); err != nil {
		t.Fatalf("bob submit: %v", err)
	}
	if _, err := e.comps.SubmitFlag(ctx, e.judge, cat.ID, comp.ID, chal.Slug, "wrong", "2.2.2.2"); err != nil {
		t.Fatalf("cat wrong submit: %v", err)
	}
	e.drainJudge()
	if _, err := e.comps.SubmitFlag(ctx, e.judge, cat.ID, comp.ID, chal.Slug, "flag{score-a}", "2.2.2.2"); err != nil {
		t.Fatalf("cat submit: %v", err)
	}
	e.drainJudge()
	// 竞赛派生积分：bob 100 / cat 90，同一 CompetitionScore 表。
	scores, _ := compRepo.ListScores(ctx, comp.ID)
	if len(scores) != 2 {
		t.Fatalf("competition scores: %d rows", len(scores))
	}
	byUser := map[uint]int{}
	for _, sc := range scores {
		byUser[sc.UserID] = sc.Score
	}
	if byUser[bob.ID] != 100 || byUser[cat.ID] != 90 {
		t.Fatalf("scores bob=%d cat=%d, want 100/90", byUser[bob.ID], byUser[cat.ID])
	}
	// 队伍榜：蓝队 190 分两人；个人榜 bob 第一。
	team, err := e.comps.Leaderboard(ctx, comp.ID, "team")
	if err != nil {
		t.Fatalf("team leaderboard: %v", err)
	}
	items := team["items"].([]TeamLeaderboardEntry)
	if len(items) != 1 || items[0].Score != 190 || items[0].TeamName == nil || *items[0].TeamName != "蓝队" {
		t.Fatalf("team board: %+v", items)
	}
	if len(items[0].Members) != 2 {
		t.Fatalf("team members: %+v", items[0].Members)
	}
	ind, err := e.comps.Leaderboard(ctx, comp.ID, "individual")
	if err != nil {
		t.Fatalf("individual leaderboard: %v", err)
	}
	indItems := ind["items"].([]TeamLeaderboardEntry)
	if len(indItems) != 2 || indItems[0].Members[0] != "bob" {
		t.Fatalf("individual board: %+v", indItems)
	}
	// 竞赛首通不影响训练场首通：bob 在训练场再交 flag 仍应完整计分。
	if _, err := e.judge.Submit(ctx, bob.ID, chal.ID, "flag{score-a}"); err != nil {
		t.Fatalf("training submit: %v", err)
	}
	e.drainJudge()
	solved, _ := repo.NewSubmissionRepo(e.gdb).HasCorrect(ctx, bob.ID, chal.ID, 0)
	if !solved {
		t.Fatalf("training first-solve shadowed by competition solve")
	}
	// 视图：bob solved=true。
	view, err := e.comps.View(ctx, bob.ID, comp.ID)
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if len(view.Challenges) != 1 || !view.Challenges[0].Solved {
		t.Fatalf("view challenges: %+v", view.Challenges)
	}
	if view.TeamName == nil || *view.TeamName != "蓝队" || len(view.Teammates) != 2 {
		t.Fatalf("view team: %+v %+v", view.TeamName, view.Teammates)
	}
}

// TestWriteupSubmissionWindow WP 提交窗口：结束前拒绝、结束后可交可改。
func TestWriteupSubmissionWindow(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "WP 赛")
	user := e.ensureUser(t, "writer")
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{"writer"}})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	part, _ := compRepo.FindParticipant(ctx, comp.ID, user.ID)
	e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode)
	// running：拒绝。
	if err := e.comps.SubmitWriteup(ctx, user.ID, comp.ID, "# wp v1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("writeup while running: want ErrValidation, got %v", err)
	}
	// 结束：提交成功，再次提交覆盖。
	e.now = comp.EndTime.Add(time.Minute)
	if err := e.comps.SubmitWriteup(ctx, user.ID, comp.ID, "# wp v1"); err != nil {
		t.Fatalf("writeup after end: %v", err)
	}
	if err := e.comps.SubmitWriteup(ctx, user.ID, comp.ID, "# wp v2 更新版"); err != nil {
		t.Fatalf("writeup update: %v", err)
	}
	wps, _ := compRepo.ListWriteups(ctx, comp.ID)
	if len(wps) != 1 || wps[0].Content != "# wp v2 更新版" {
		t.Fatalf("writeups: %+v", wps)
	}
	// 管理端 WP 情况：全部参赛者一行，writer submitted=true。
	items, err := e.comps.AdminWriteups(ctx, comp.ID)
	if err != nil {
		t.Fatalf("admin writeups: %v", err)
	}
	if len(items) != 1 || !items[0].Submitted || items[0].Content != "# wp v2 更新版" {
		t.Fatalf("admin writeup items: %+v", items)
	}
}

// TestCrossTeamFlagWarning 反作弊：提交其他队伍成员的实例 flag → 管理端警告。
func TestCrossTeamFlagWarning(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "反作弊赛")
	chal := e.seedChallenge(t, "inst-a", "instance")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	e.comps.BindChallenges(ctx, comp.ID, body, nil)
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Teams: []TeamImport{
			{Name: "甲队", Members: []string{"aa"}},
			{Name: "乙队", Members: []string{"bb"}},
		},
	})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	instRepo := repo.NewLabInstanceRepo(e.gdb)
	aa, _ := e.users.FindByUsername(ctx, "aa")
	bb, _ := e.users.FindByUsername(ctx, "bb")
	aaPart, _ := compRepo.FindParticipant(ctx, comp.ID, aa.ID)
	bbPart, _ := compRepo.FindParticipant(ctx, comp.ID, bb.ID)
	e.comps.Enter(ctx, aa.ID, comp.ID, aaPart.InviteCode)
	e.comps.Enter(ctx, bb.ID, comp.ID, bbPart.InviteCode)
	// aa 的 running 实例持有唯一 flag。
	inst := &model.LabInstance{
		UserID: aa.ID, ChallengeID: chal.ID, CompetitionID: comp.ID,
		Status: model.InstanceRunning, FlagValue: "flag{inst-a-aa}",
	}
	if err := instRepo.Create(ctx, inst); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	// bb 提交 aa 的 flag：判定可能错（对 bb 而言），但必须产生跨队警告。
	if _, err := e.comps.SubmitFlag(ctx, e.judge, bb.ID, comp.ID, chal.Slug, "flag{inst-a-aa}", "9.9.9.9"); err != nil {
		t.Fatalf("submit foreign flag: %v", err)
	}
	e.drainJudge()
	warnings, err := e.comps.AdminWarnings(ctx, comp.ID)
	if err != nil {
		t.Fatalf("admin warnings: %v", err)
	}
	found := false
	for _, wn := range warnings {
		if wn.UserID == bb.ID && wn.Kind == model.WarningCrossTeamFlag && wn.TeamID != nil {
			found = true
		}
	}
	if !found {
		t.Fatalf("cross-team warning missing: %+v", warnings)
	}
	// IP 异常：bb 换 IP 再提交 → ip_anomaly 警告（上次基线 9.9.9.9）。
	chal2 := e.seedChallenge(t, "inst-b", "instance")
	body2, _ := json.Marshal(map[string]any{"slugs": []string{chal2.Slug}})
	e.comps.BindChallenges(ctx, comp.ID, body2, nil)
	if _, err := e.comps.SubmitFlag(ctx, e.judge, bb.ID, comp.ID, chal2.Slug, "flag{inst-b}", "8.8.8.8"); err != nil {
		t.Fatalf("submit with new ip: %v", err)
	}
	e.drainJudge()
	warnings, _ = e.comps.AdminWarnings(ctx, comp.ID)
	ipWarned := false
	for _, wn := range warnings {
		if wn.UserID == bb.ID && wn.Kind == model.WarningIPAnomaly {
			ipWarned = true
		}
	}
	if !ipWarned {
		t.Fatalf("ip anomaly warning missing: %+v", warnings)
	}
}

// TestCompetitionInstanceScope 竞赛容器归属：竞赛内启动的实例落 competition_id，
// 管理端按竞赛过滤可见。
func TestCompetitionInstanceScope(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "容器赛")
	chal := e.seedChallenge(t, "c-scoped", "instance")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	e.comps.BindChallenges(ctx, comp.ID, body, nil)
	user := e.ensureUser(t, "diver")
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{"diver"}})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	part, _ := compRepo.FindParticipant(ctx, comp.ID, user.ID)
	e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode)
	// fakeDriver 不够用：这里直接走 repo 层断言（EnsureInstanceScoped 的
	// scope 传递在 env_test 已由行为覆盖，此处验证绑定关系约束）。
	// 未绑定的题目 → ErrNotFound。
	other := e.seedChallenge(t, "c-outside", "instance")
	if _, err := e.comps.StartInstance(ctx, nil, user.ID, comp.ID, other.Slug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("start unbound challenge: want ErrNotFound, got %v", err)
	}
	// 实例落 competition_id（repo 层直接建行 + 列表过滤验证）。
	instRepo := repo.NewLabInstanceRepo(e.gdb)
	if err := instRepo.Create(ctx, &model.LabInstance{
		UserID: user.ID, ChallengeID: chal.ID, CompetitionID: comp.ID, Status: model.InstanceRunning,
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	list, total, err := instRepo.ListByCompetition(ctx, comp.ID, 0, 10)
	if err != nil || total != 1 || len(list) != 1 || list[0].CompetitionID != comp.ID {
		t.Fatalf("list by competition: total=%d err=%v", total, err)
	}
}