package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// TestCtfOnlyOperationsRejectOtherFormats CTF 专用操作必须以 format=ctf 为前提。
// AWD/考试竞赛共享报名、入场、名册与赛程，但不得从旧 CTF 路由读取题目、
// 提交 flag、看榜或提交 WP；否则赛制隔离会在服务层失效。
func TestCtfOnlyOperationsRejectOtherFormats(t *testing.T) {
	e := newCompEnv(t)
	ctx := context.Background()
	start := e.now.Add(-time.Hour)
	end := e.now.Add(2 * time.Hour)
	awd, err := e.comps.Create(ctx, CompetitionCreate{
		Name: "AWD 形式守卫", Kind: "public", Format: "awd", Start: start, End: end,
	})
	if err != nil {
		t.Fatalf("create awd: %v", err)
	}
	user := e.ensureUser(t, "awd-format-guard")
	if _, err := e.comps.Register(ctx, user.ID, awd.ID); err != nil {
		t.Fatalf("register awd: %v", err)
	}
	if _, err := e.comps.Enter(ctx, user.ID, awd.ID, "", model.GlobalRoleStudent); err != nil {
		t.Fatalf("enter awd: %v", err)
	}

	cases := []struct {
		name string
		call func() error
	}{
		{"bind challenges", func() error {
			_, err := e.comps.BindChallenges(ctx, awd.ID, []byte(`{"slugs":["x"]}`), nil)
			return err
		}},
		{"admin challenges", func() error { _, err := e.comps.AdminChallenges(ctx, awd.ID); return err }},
		{"unbind challenges", func() error { _, err := e.comps.UnbindChallenges(ctx, awd.ID, []string{"x"}); return err }},
		{"participant view", func() error { _, err := e.comps.View(ctx, user.ID, awd.ID); return err }},
		{"start instance", func() error { _, err := e.comps.StartInstance(ctx, nil, user.ID, awd.ID, "x"); return err }},
		{"submit flag", func() error { _, err := e.comps.SubmitFlag(ctx, nil, user.ID, awd.ID, "x", "flag{x}", ""); return err }},
		{"leaderboard", func() error { _, err := e.comps.Leaderboard(ctx, awd.ID, "individual"); return err }},
		{"admin warnings", func() error { _, err := e.comps.AdminWarnings(ctx, awd.ID); return err }},
		{"admin writeups", func() error { _, err := e.comps.AdminWriteups(ctx, awd.ID); return err }},
		{"open writeup zip", func() error { _, _, _, err := e.comps.OpenWriteupZip(ctx, awd.ID, 1); return err }},
		{"my writeups", func() error { _, err := e.comps.MyWriteups(ctx, user.ID, awd.ID); return err }},
		{"save writeup", func() error { return e.comps.SaveWriteup(ctx, user.ID, awd.ID, 1, "x") }},
		{"upload writeup zip", func() error {
			_, err := e.comps.UploadWriteupZip(ctx, user.ID, awd.ID, 1, "x.zip", nil)
			return err
		}},
	}
	for _, tc := range cases {
		if err := tc.call(); !errors.Is(err, ErrValidation) {
			t.Errorf("%s: want ErrValidation, got %v", tc.name, err)
		}
	}

	// 共享核心仍然可用：公开赛列表、报名状态、入场状态不受形式守卫影响。
	if _, _, err := e.comps.ListPublic(ctx, user.ID, 1, 20); err != nil {
		t.Fatalf("list public: %v", err)
	}
	entry, err := e.comps.MyEntry(ctx, user.ID, model.GlobalRoleStudent)
	if err != nil || !entry.Entered || entry.Competition == nil || entry.Competition.ID != awd.ID {
		t.Fatalf("shared entry state broken: %+v err=%v", entry, err)
	}
}
