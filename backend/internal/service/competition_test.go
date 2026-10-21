package service

// 竞赛域测试：真实 SQLite 仓库（project-conventions skill：禁 mock repo），
// 覆盖 创建/配置校验、题目绑定、批量导入、入场锁定、竞赛计分派生、
// 排行榜聚合、反作弊检测、WP 时序。

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	now         time.Time
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

// TestEndedCompetitionReconfigurable 已结束的竞赛可重新配置起止时间
// （回归：曾因"已开始即永久锁死 start_time"导致结束后的竞赛无法重排复活，
// 报错 cannot change start_time after competition started）。
func TestEndedCompetitionReconfigurable(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "重排复活赛")
	// 推进时钟到比赛结束后。
	e.now = comp.EndTime.Add(time.Minute)
	// 1. 整体重排到未来窗口：成功且状态回到 scheduled（重新开赛）。
	ns := e.now.Add(2 * time.Hour)
	ne := e.now.Add(4 * time.Hour)
	rescheduled, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{StartTime: &ns, EndTime: &ne})
	if err != nil {
		t.Fatalf("reconfigure ended competition: %v", err)
	}
	if rescheduled.Status != model.CompetitionScheduled {
		t.Fatalf("status after reschedule: want scheduled, got %s", rescheduled.Status)
	}
	if !rescheduled.StartTime.Equal(ns) || !rescheduled.EndTime.Equal(ne) {
		t.Fatalf("rescheduled times not persisted: start=%v end=%v", rescheduled.StartTime, rescheduled.EndTime)
	}
	// 2. 重排后进入新窗口 running：start_time 再次锁定（保护入场锁定语义）。
	e.now = ns.Add(time.Minute)
	if _, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{StartTime: &ne}); !errors.Is(err, ErrValidation) {
		t.Fatalf("change start while re-running: want ErrValidation, got %v", err)
	}
	// 3. running 窗口内延后 end_time 仍允许。
	ne2 := ne.Add(time.Hour)
	if _, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{EndTime: &ne2}); err != nil {
		t.Fatalf("extend end while re-running: %v", err)
	}
	// 4. 再次结束后缩回 end_time 也属于重新配置（曾报 cannot shorten
	// end_time after competition ended）。
	e.now = ne2.Add(time.Minute)
	shorter := ne2.Add(-time.Hour)
	if _, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{EndTime: &shorter}); err != nil {
		t.Fatalf("shorten end after re-ended: %v", err)
	}
}

// TestEndCompetitionManually 手动结束：仅 running 可结束，end_time 置为
// 当前时刻；结束后可重排复活（与 TestEndedCompetitionReconfigurable 衔接）。
func TestEndCompetitionManually(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	// scheduled：未开赛不存在"结束"，422。
	futureStart := e.now.Add(time.Hour)
	futureEnd := e.now.Add(3 * time.Hour)
	scheduled, err := e.comps.Create(ctx, "未开赛赛", "", futureStart, futureEnd)
	if err != nil {
		t.Fatalf("create scheduled: %v", err)
	}
	if _, err := e.comps.End(ctx, scheduled.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("end scheduled: want ErrValidation, got %v", err)
	}
	// running：手动结束成功，end_time = now，状态派生 ended。
	comp := e.createRunningComp(t, "手动结束赛")
	ended, err := e.comps.End(ctx, comp.ID)
	if err != nil {
		t.Fatalf("end running: %v", err)
	}
	if ended.Status != model.CompetitionEnded {
		t.Fatalf("status after end: want ended, got %s", ended.Status)
	}
	if !ended.EndTime.Equal(e.now) {
		t.Fatalf("end_time after end: want %v, got %v", e.now, ended.EndTime)
	}
	// 已结束的竞赛再调 End：422（幂等保护，避免误触改写历史窗口）。
	if _, err := e.comps.End(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("end already-ended: want ErrValidation, got %v", err)
	}
	// 手动结束 → 重排到未来复活（用户主路径：进行中竞赛先结束再重新配置）。
	ns := e.now.Add(time.Hour)
	ne := e.now.Add(2 * time.Hour)
	revived, err := e.comps.Update(ctx, comp.ID, CompetitionUpdate{StartTime: &ns, EndTime: &ne})
	if err != nil {
		t.Fatalf("revive after manual end: %v", err)
	}
	if revived.Status != model.CompetitionScheduled {
		t.Fatalf("status after revive: want scheduled, got %s", revived.Status)
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
	chal := e.seedChallenge(t, "wp-chal", "static")
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{"writer"}})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	part, _ := compRepo.FindParticipant(ctx, comp.ID, user.ID)
	e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode)
	if _, err := e.comps.BindChallenges(ctx, comp.ID, []byte(`{"slugs":["`+chal.Slug+`"]}`), nil); err != nil {
		t.Fatalf("bind: %v", err)
	}
	// running：拒绝。
	if err := e.comps.SaveWriteup(ctx, user.ID, comp.ID, chal.ID, "# wp v1"); !errors.Is(err, ErrValidation) {
		t.Fatalf("writeup while running: want ErrValidation, got %v", err)
	}
	// 未绑定题目：404。
	e.now = comp.EndTime.Add(time.Minute)
	if err := e.comps.SaveWriteup(ctx, user.ID, comp.ID, chal.ID+999, "# x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unbound challenge: want ErrNotFound, got %v", err)
	}
	// 结束：提交成功，再次提交覆盖；空文字且无 zip → 422。
	if err := e.comps.SaveWriteup(ctx, user.ID, comp.ID, chal.ID, "# wp v1"); err != nil {
		t.Fatalf("writeup after end: %v", err)
	}
	if err := e.comps.SaveWriteup(ctx, user.ID, comp.ID, chal.ID, ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty md without zip: want ErrValidation, got %v", err)
	}
	if err := e.comps.SaveWriteup(ctx, user.ID, comp.ID, chal.ID, "# wp v2 更新版"); err != nil {
		t.Fatalf("writeup update: %v", err)
	}
	wps, _ := compRepo.ListWriteups(ctx, comp.ID)
	if len(wps) != 1 || wps[0].Content != "# wp v2 更新版" {
		t.Fatalf("writeups: %+v", wps)
	}
	// 管理端 WP 情况（单位 = 题目 × 队伍/个人）。
	items, err := e.comps.AdminWriteups(ctx, comp.ID)
	if err != nil {
		t.Fatalf("admin writeups: %v", err)
	}
	if len(items) != 1 || items[0].Content != "# wp v2 更新版" || items[0].TeamName != nil || items[0].ChallengeID != chal.ID {
		t.Fatalf("admin writeup items: %+v", items)
	}
}

// TestWriteupTeamSharedAndZip 队伍一份（任一队员可改，不重复建行）+
// zip 上传/替换/纯 zip 保留（文字与 zip 至少一项）。
func TestWriteupTeamSharedAndZip(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	dir := t.TempDir()
	e.comps.SetWriteupDir(dir)
	comp := e.createRunningComp(t, "组队 WP 赛")
	chalA := e.seedChallenge(t, "team-wp-a", "static")
	chalB := e.seedChallenge(t, "team-wp-b", "static")
	alice := e.ensureUser(t, "alice")
	bob := e.ensureUser(t, "bob")
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{
		Teams: []TeamImport{{Name: "Blue", Members: []string{"alice", "bob"}}},
	})
	// rival 走导入建号（ImportParticipants 内部 createAccount）→ 按用户名取回。
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{"rival"}})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	parts, _, _ := compRepo.ListParticipants(ctx, comp.ID, 0, 100)
	codes := map[uint]string{}
	for _, p := range parts {
		codes[p.UserID] = p.InviteCode
	}
	rival, err := e.users.FindByUsername(context.Background(), "rival")
	if err != nil {
		t.Fatalf("load rival: %v", err)
	}
	for _, u := range []*model.User{alice, bob, rival} {
		if _, err := e.comps.Enter(ctx, u.ID, comp.ID, codes[u.ID]); err != nil {
			t.Fatalf("enter %s: %v", u.Username, err)
		}
	}
	if _, err := e.comps.BindChallenges(ctx, comp.ID, []byte(`{"slugs":["`+chalA.Slug+`","`+chalB.Slug+`"]}`), nil); err != nil {
		t.Fatalf("bind: %v", err)
	}
	e.now = comp.EndTime.Add(time.Minute)

	// alice 提交 A 题，bob（同队）修改 → 同一份（一行）。
	if err := e.comps.SaveWriteup(ctx, alice.ID, comp.ID, chalA.ID, "# A by alice"); err != nil {
		t.Fatalf("alice save A: %v", err)
	}
	if err := e.comps.SaveWriteup(ctx, bob.ID, comp.ID, chalA.ID, "# A by bob 覆盖"); err != nil {
		t.Fatalf("bob save A: %v", err)
	}
	// rival 与 Blue 不是同一单位：A 题个人份互不干扰。
	if err := e.comps.SaveWriteup(ctx, rival.ID, comp.ID, chalA.ID, "# A by rival"); err != nil {
		t.Fatalf("rival save A: %v", err)
	}
	wps, _ := compRepo.ListWriteups(ctx, comp.ID)
	if len(wps) != 2 {
		t.Fatalf("want 2 writeups (Blue A + rival A), got %d: %+v", len(wps), wps)
	}

	// alice 上传 A 题 zip；bob 重传替换；文字保留。
	zipV1 := makeZip(t, "solve_v1.py")
	name, err := e.comps.UploadWriteupZip(ctx, alice.ID, comp.ID, chalA.ID, "攻击脚本 v1.zip", bytes.NewReader(zipV1))
	if err != nil {
		t.Fatalf("upload zip: %v", err)
	}
	if name != "攻击脚本-v1.zip" {
		t.Fatalf("zip name sanitize: %q", name)
	}
	zipV2 := makeZip(t, "solve_v2.py")
	if _, err := e.comps.UploadWriteupZip(ctx, bob.ID, comp.ID, chalA.ID, "attack_v2.zip", bytes.NewReader(zipV2)); err != nil {
		t.Fatalf("reupload zip: %v", err)
	}
	// 非 zip 内容 → 422。
	if _, err := e.comps.UploadWriteupZip(ctx, alice.ID, comp.ID, chalA.ID, "fake.zip", strings.NewReader("not a zip")); !errors.Is(err, ErrValidation) {
		t.Fatalf("non-zip: want ErrValidation, got %v", err)
	}

	// Blue A 份：最新内容 + 最新 zip + 修改人 bob；B 题独立无份。
	mine, err := e.comps.MyWriteups(ctx, alice.ID, comp.ID)
	if err != nil {
		t.Fatalf("my writeups: %v", err)
	}
	if len(mine) != 1 || mine[0].ChallengeID != chalA.ID {
		t.Fatalf("alice my writeups: %+v", mine)
	}
	if mine[0].ContentMd != "# A by bob 覆盖" || mine[0].ZipName != "attack_v2.zip" || mine[0].UpdatedBy != "bob" {
		t.Fatalf("blue A writeup: %+v", mine[0])
	}
	// rival 看到的只有个人份。
	rivalMine, _ := e.comps.MyWriteups(ctx, rival.ID, comp.ID)
	if len(rivalMine) != 1 || rivalMine[0].UpdatedBy != "rival" || rivalMine[0].ZipName != "" {
		t.Fatalf("rival my writeups: %+v", rivalMine)
	}

	// 落盘只保留最新包（同路径覆盖）。
	entries, _ := os.ReadDir(filepath.Join(dir, strconv.FormatUint(uint64(comp.ID), 10)))
	if len(entries) != 1 {
		t.Fatalf("zip files on disk: %d", len(entries))
	}
	// 纯 zip 份：清空文字允许（zip 还在）。
	if err := e.comps.SaveWriteup(ctx, bob.ID, comp.ID, chalA.ID, ""); err != nil {
		t.Fatalf("clear md with zip kept: %v", err)
	}
	// 管理端下载 zip。
	items, err := e.comps.AdminWriteups(ctx, comp.ID)
	if err != nil {
		t.Fatalf("admin writeups: %v", err)
	}
	var blueA *AdminWriteupItem
	for i := range items {
		if items[i].ChallengeID == chalA.ID && items[i].TeamName != nil && *items[i].TeamName == "Blue" {
			blueA = &items[i]
		}
	}
	if blueA == nil || blueA.ZipName != "attack_v2.zip" || blueA.Content != "" {
		t.Fatalf("admin blue A: %+v", blueA)
	}
	f, fname, err := e.comps.OpenWriteupZip(ctx, comp.ID, blueA.ID)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer f.Close()
	if fname != "attack_v2.zip" {
		t.Fatalf("download name: %q", fname)
	}
	got, _ := io.ReadAll(f)
	if !bytes.Equal(got, zipV2) {
		t.Fatalf("zip content mismatch: %d bytes", len(got))
	}
}

// makeZip 构造最小合法 zip（内容为一个入口）。
func makeZip(t *testing.T, entry string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(entry)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	w.Write([]byte("print('solved')\n"))
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
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

// TestActiveCompetitionNonUTCZone 服务时区非 UTC 时锁定判定仍正确：
// 起止时间 UTC 落库 + ActiveCompetitions 查询参数归一 UTC（SQLite 文本比较）。
func TestActiveCompetitionNonUTCZone(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	// 服务器时钟在 +08:00 时区：同一时刻，墙上时间与 UTC 差 8 小时。
	zone := time.FixedZone("CST", 8*3600)
	localNow := e.now.In(zone)
	e.comps.now = func() time.Time { return localNow }
	// 竞赛用 UTC 时间创建（API 层 RFC3339 常带 Z）。
	comp := e.createRunningComp(t, "跨时区赛")
	user := e.ensureUser(t, "zoner")
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{"zoner"}})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	part, _ := compRepo.FindParticipant(ctx, comp.ID, user.ID)
	if _, err := e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if id, locked := e.comps.LockedCompetition(ctx, user.ID); !locked || id != comp.ID {
		t.Fatalf("locked in non-UTC zone: id=%d locked=%v", id, locked)
	}
	entry, err := e.comps.MyEntry(ctx, user.ID)
	if err != nil || !entry.Locked {
		t.Fatalf("my entry in non-UTC zone: err=%v locked=%v", err, entry.Locked)
	}
	// 结束（UTC 判定）后解锁。
	e.comps.now = func() time.Time { return comp.EndTime.Add(time.Minute).In(zone) }
	if _, locked := e.comps.LockedCompetition(ctx, user.ID); locked {
		t.Fatalf("should unlock after end in non-UTC zone")
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

// TestCompetitionDeleteLifecycle 删除竞赛：running 拒绝；ended 后删除并级联
// 清空参赛者/绑定/提交/积分行；已入场者在删除后解除锁定（服务端派生）。
func TestCompetitionDeleteLifecycle(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "删除赛")
	chal := e.seedChallenge(t, "c-del", "shared")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	e.comps.BindChallenges(ctx, comp.ID, body, nil)
	user := e.ensureUser(t, "delver")
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{"delver"}})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	part, _ := compRepo.FindParticipant(ctx, comp.ID, user.ID)
	e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode)
	if id, locked := e.comps.LockedCompetition(ctx, user.ID); !locked || id != comp.ID {
		t.Fatalf("locked before delete: id=%d locked=%v", id, locked)
	}

	// running 期间拒绝删除（与 running 期间禁改 start_time 同一保护语义）。
	if err := e.comps.Delete(ctx, comp.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("delete running: want ErrValidation, got %v", err)
	}

	// 结束后可删；级联清空关联行。
	e.comps.now = func() time.Time { return comp.EndTime.Add(time.Minute) }
	if err := e.comps.Delete(ctx, comp.ID); err != nil {
		t.Fatalf("delete ended: %v", err)
	}
	if _, err := compRepo.FindByID(ctx, comp.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("competition row should be gone, got err=%v", err)
	}
	var n int64
	for _, m := range []any{&model.Participant{}, &model.CompetitionChallenge{}, &model.CompetitionScore{}} {
		if err := e.gdb.Model(m).Where("competition_id = ?", comp.ID).Count(&n).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Fatalf("cascade left %d rows in %T", n, m)
		}
	}
	// 已入场者随行删除解除锁定。
	if _, locked := e.comps.LockedCompetition(ctx, user.ID); locked {
		t.Fatalf("lock should be gone after delete")
	}
	// 重复删除：404。
	if err := e.comps.Delete(ctx, comp.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: want ErrNotFound, got %v", err)
	}
}

// TestCompetitionDeleteDestroysInstancesAndWriteups 删除前回调销毁容器、
// 清理 WP zip 落盘目录；容器销毁失败中止删除（防孤儿容器）。
func TestCompetitionDeleteDestroysInstancesAndWriteups(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp := e.createRunningComp(t, "清理赛")
	chal := e.seedChallenge(t, "c-clean", "shared")
	body, _ := json.Marshal(map[string]any{"slugs": []string{chal.Slug}})
	e.comps.BindChallenges(ctx, comp.ID, body, nil)

	// destroy 回调探针：记录被要求销毁的竞赛 ID。
	destroyed := map[uint]bool{}
	e.comps.SetInstanceDestroyer(func(ctx context.Context, competitionID uint) error {
		destroyed[competitionID] = true
		return nil
	})

	// WP 落盘：<dir>/<compID>/<wpID>.zip。
	dir := t.TempDir()
	e.comps.SetWriteupDir(dir)
	user := e.ensureUser(t, "wper")
	e.comps.ImportParticipants(ctx, comp.ID, ParticipantImportRequest{Individuals: []string{"wper"}})
	compRepo := repo.NewCompetitionRepo(e.gdb)
	part, _ := compRepo.FindParticipant(ctx, comp.ID, user.ID)
	e.comps.Enter(ctx, user.ID, comp.ID, part.InviteCode)
	e.comps.now = func() time.Time { return comp.EndTime.Add(time.Minute) }
	if err := e.comps.SaveWriteup(ctx, user.ID, comp.ID, chal.ID, "# wp"); err != nil {
		t.Fatalf("save writeup: %v", err)
	}
	// zip 上传后落盘路径 <dir>/<compID>/<wpID>.zip（删除时整目录清理的对象）。
	if _, err := e.comps.UploadWriteupZip(ctx, user.ID, comp.ID, chal.ID, "s.zip", bytes.NewReader(makeZip(t, "s.sh"))); err != nil {
		t.Fatalf("upload writeup zip: %v", err)
	}
	wps, err := compRepo.ListWriteups(ctx, comp.ID)
	if err != nil || len(wps) != 1 {
		t.Fatalf("writeups: len=%d err=%v", len(wps), err)
	}
	if wps[0].ZipFile == "" {
		t.Fatalf("writeup should have zip path")
	}

	if err := e.comps.Delete(ctx, comp.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !destroyed[comp.ID] {
		t.Fatalf("instance destroyer not invoked for competition %d", comp.ID)
	}
	if _, err := os.Stat(filepath.Join(dir, strconv.Itoa(int(comp.ID)))); !os.IsNotExist(err) {
		t.Fatalf("writeup dir should be removed, stat err=%v", err)
	}

	// 容器销毁失败 → 删除中止，竞赛行仍在。
	comp2 := e.createRunningComp(t, "失败赛")
	e.comps.now = func() time.Time { return comp2.EndTime.Add(time.Minute) }
	e.comps.SetInstanceDestroyer(func(ctx context.Context, competitionID uint) error {
		return fmt.Errorf("docker down")
	})
	if err := e.comps.Delete(ctx, comp2.ID); err == nil || !strings.Contains(err.Error(), "docker down") {
		t.Fatalf("delete should fail on destroy error, got %v", err)
	}
	if _, err := compRepo.FindByID(ctx, comp2.ID); err != nil {
		t.Fatalf("competition should remain after failed delete: %v", err)
	}
}
