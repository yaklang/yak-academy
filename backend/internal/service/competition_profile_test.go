package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCompetitionRosterAndProfile(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	comp, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "名册验证赛", Start: e.now.Add(-time.Hour), End: e.now.Add(2 * time.Hour),
		Kind: "invitational", Format: "ctf",
	})
	if err != nil {
		t.Fatalf("create comp: %v", err)
	}
	alice := e.ensureUser(t, "roster-alice")
	bob := e.ensureUser(t, "roster-bob")

	// 非参赛者不能自填名册。
	if _, err := e.comps.UpdateProfile(ctx, comp.ID, alice.ID, CompetitionProfileInput{RealName: "张三"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("profile before participation: want ErrNotFound, got %v", err)
	}

	// 名册导入：命中账号写实名并导入参赛；未命中账号回报 missing 不阻断。
	result, err := e.comps.ImportRoster(ctx, comp.ID, CompetitionRosterImportRequest{
		Entries: []CompetitionRosterEntry{
			{Entry: alice.Username, RealName: "张三", School: "示例大学", StudentNo: "2026001", Email: "alice@example.com", Phone: "13800000000"},
			{Entry: "ghost-user", RealName: "李四", School: "示例大学"},
		},
	})
	if err != nil {
		t.Fatalf("import roster: %v", err)
	}
	if result.Imported != 1 || len(result.Profiles) != 1 || len(result.Participants) != 1 ||
		len(result.Missing) != 1 || result.Missing[0] != "ghost-user" {
		t.Fatalf("import result: %+v", result)
	}
	if result.Profiles[0].Username != alice.Username || result.Profiles[0].RealName != "张三" ||
		!result.Profiles[0].HasProfile {
		t.Fatalf("profile view: %+v", result.Profiles[0])
	}
	participants, total, err := e.comps.comps.ListParticipants(ctx, comp.ID, 0, 10)
	if err != nil || total != 1 || len(participants) != 1 || participants[0].UserID != alice.ID {
		t.Fatalf("participants: %+v total=%d err=%v", participants, total, err)
	}

	// 重复导入幂等：更新实名信息，不新增参赛记录。
	result, err = e.comps.ImportRoster(ctx, comp.ID, CompetitionRosterImportRequest{
		Entries: []CompetitionRosterEntry{
			{Entry: alice.AccountCode, RealName: "张三", School: "示例大学（更新）", Email: "alice@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("reimport roster: %v", err)
	}
	if result.Imported != 1 || len(result.Participants) != 1 {
		t.Fatalf("reimport result: %+v", result)
	}
	if _, total, err := e.comps.comps.ListParticipants(ctx, comp.ID, 0, 10); err != nil || total != 1 {
		t.Fatalf("participants after reimport: total=%d err=%v", total, err)
	}

	// 参赛者本人读取并补填（手机号更新）。
	profile, err := e.comps.Profile(ctx, comp.ID, alice.ID)
	if err != nil || !profile.HasProfile || profile.School != "示例大学（更新）" {
		t.Fatalf("self profile: %+v err=%v", profile, err)
	}
	updated, err := e.comps.UpdateProfile(ctx, comp.ID, alice.ID, CompetitionProfileInput{
		RealName: "张三", School: "示例大学", StudentNo: "2026001", Email: "alice@example.com", Phone: "13900000000",
	})
	if err != nil || updated.Phone != "13900000000" {
		t.Fatalf("update profile: %+v err=%v", updated, err)
	}
	if _, err := e.comps.UpdateProfile(ctx, comp.ID, alice.ID, CompetitionProfileInput{}); !errors.Is(err, ErrValidation) {
		t.Fatalf("empty real name: want ErrValidation, got %v", err)
	}
	if _, err := e.comps.UpdateProfile(ctx, comp.ID, alice.ID, CompetitionProfileInput{
		RealName: "张三", Email: "not-an-email",
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid email: want ErrValidation, got %v", err)
	}
	if _, err := e.comps.UpdateProfile(ctx, comp.ID, bob.ID, CompetitionProfileInput{RealName: "李四"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-participant profile: want ErrNotFound, got %v", err)
	}

	// 管理端名册包含全部参赛者。
	roster, err := e.comps.Roster(ctx, comp.ID)
	if err != nil || len(roster) != 1 || roster[0].Username != alice.Username ||
		roster[0].Phone != "13900000000" || roster[0].TeamName != nil {
		t.Fatalf("roster: %+v err=%v", roster, err)
	}

	// 同一批重复用户与公开赛均拒绝导入。
	if _, err := e.comps.ImportRoster(ctx, comp.ID, CompetitionRosterImportRequest{
		Entries: []CompetitionRosterEntry{
			{Entry: alice.Username, RealName: "张三"},
			{Entry: alice.AccountCode, RealName: "张三"},
		},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate roster entry: want ErrValidation, got %v", err)
	}
	public, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "公开名册赛", Start: e.now.Add(-time.Hour), End: e.now.Add(time.Hour),
		Kind: "public", Format: "ctf",
	})
	if err != nil {
		t.Fatalf("create public comp: %v", err)
	}
	if _, err := e.comps.ImportRoster(ctx, public.ID, CompetitionRosterImportRequest{
		Entries: []CompetitionRosterEntry{{Entry: alice.Username, RealName: "张三"}},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("public roster import: want ErrValidation, got %v", err)
	}

	// 竞赛结束后名册冻结。
	e.now = comp.EndTime.Add(time.Minute)
	if _, err := e.comps.UpdateProfile(ctx, comp.ID, alice.ID, CompetitionProfileInput{RealName: "张三"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("profile after end: want ErrValidation, got %v", err)
	}
	if _, err := e.comps.ImportRoster(ctx, comp.ID, CompetitionRosterImportRequest{
		Entries: []CompetitionRosterEntry{{Entry: alice.Username, RealName: "张三"}},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("roster after end: want ErrValidation, got %v", err)
	}
}
