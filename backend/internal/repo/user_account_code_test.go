package repo

// 用户账号编码：格式约定、Create 统一分配（同年 MAX+1）、迁移回填、TouchLogin。

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
)

func TestAccountCodeFormat(t *testing.T) {
	if got := model.AccountCodePrefix(2026); got != "YA26-" {
		t.Fatalf("prefix = %q, want YA26-", got)
	}
	cases := []struct {
		year, seq int
		want      string
	}{
		{2026, 1, "YA26-00001"},
		{2026, 42, "YA26-00042"},
		{2025, 12345, "YA25-12345"},
		{2026, 0, ""},
		{2026, 100000, ""},
	}
	for _, c := range cases {
		if got := model.FormatAccountCode(c.year, c.seq); got != c.want {
			t.Errorf("model.FormatAccountCode(%d,%d) = %q, want %q", c.year, c.seq, got, c.want)
		}
	}
	if seq, ok := model.AccountCodeSeq("YA26-00042"); !ok || seq != 42 {
		t.Errorf("model.AccountCodeSeq(YA26-00042) = %d,%v; want 42,true", seq, ok)
	}
	for _, bad := range []string{"YA26-", "YA26-abc", "", "00042", "YA26-0"} {
		if _, ok := model.AccountCodeSeq(bad); ok {
			t.Errorf("model.AccountCodeSeq(%q) ok, want reject", bad)
		}
	}
}

func newAccountCodeTestDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	return gdb
}

func TestUserCreateAssignsAccountCode(t *testing.T) {
	gdb := newAccountCodeTestDB(t, "accountcode")
	users := NewUserRepo(gdb)
	ctx := context.Background()

	y2026 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	y2025 := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	mk := func(name string, created time.Time) *model.User {
		return &model.User{Username: name, GlobalRole: model.GlobalRoleStudent, CreatedAt: created}
	}
	for i, want := range []struct {
		name string
		year time.Time
		code string
	}{
		{"u1", y2026, "YA26-00001"},
		{"u2", y2026, "YA26-00002"},
		// 年份按建号年分组，互不挤占序号。
		{"u3", y2025, "YA25-00001"},
	} {
		u := mk(want.name, want.year)
		if err := users.Create(ctx, u); err != nil {
			t.Fatalf("create %s: %v", want.name, err)
		}
		if u.AccountCode != want.code {
			t.Fatalf("user %d code = %q, want %q", i, u.AccountCode, want.code)
		}
	}
	// 已带编码（迁移回填语义）原样保留；跳号后新分配从已有最大值 +1。
	preset := mk("u4", y2026)
	preset.AccountCode = "YA26-90000"
	if err := users.Create(ctx, preset); err != nil {
		t.Fatalf("create preset: %v", err)
	}
	if preset.AccountCode != "YA26-90000" {
		t.Fatalf("preset code rewritten to %q", preset.AccountCode)
	}
	u5 := mk("u5", y2026)
	if err := users.Create(ctx, u5); err != nil {
		t.Fatal(err)
	}
	if u5.AccountCode != "YA26-90001" {
		t.Fatalf("post-preset code = %q, want YA26-90001", u5.AccountCode)
	}

	// FindByAccountCode 命中。
	got, err := users.FindByAccountCode(ctx, "YA26-00002")
	if err != nil || got.Username != "u2" {
		t.Fatalf("FindByAccountCode = %v,%v; want u2", got, err)
	}

	// TouchLogin 写入基线。
	ts := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := users.TouchLogin(ctx, got.ID, ts); err != nil {
		t.Fatalf("touch login: %v", err)
	}
	reloaded, err := users.FindByID(ctx, got.ID)
	if err != nil || reloaded.LastLoginAt == nil || !reloaded.LastLoginAt.Equal(ts) {
		t.Fatalf("last_login_at = %v,%v; want %v", reloaded.LastLoginAt, err, ts)
	}
}

func TestMigrateBackfillsAccountCodes(t *testing.T) {
	// 模拟旧库：只建表插入无编码历史行，再跑完整 Migrate 触发回填。
	gdb, err := gorm.Open(sqlite.Open("file:accountcodemigrate?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	if err := gdb.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	legacy := []model.User{
		{Username: "old-a", CreatedAt: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)},
		{Username: "old-b", CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)},
		{Username: "old-c", CreatedAt: time.Date(2025, 8, 1, 0, 0, 0, 0, time.UTC)},
	}
	for i := range legacy {
		if err := gdb.Create(&legacy[i]).Error; err != nil {
			t.Fatalf("seed legacy: %v", err)
		}
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	want := map[string]string{
		"old-a": "YA25-00001",
		"old-c": "YA25-00002", // 同年按 id 升序连续编号
		"old-b": "YA26-00001",
	}
	var reloaded []model.User
	if err := gdb.Order("id").Find(&reloaded).Error; err != nil {
		t.Fatal(err)
	}
	for _, u := range reloaded {
		if u.AccountCode != want[u.Username] {
			t.Fatalf("%s account_code = %q, want %q", u.Username, u.AccountCode, want[u.Username])
		}
	}
	// 幂等：二次 Migrate 不改号。
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var again []model.User
	if err := gdb.Order("id").Find(&again).Error; err != nil {
		t.Fatal(err)
	}
	for i := range again {
		if again[i].AccountCode != reloaded[i].AccountCode {
			t.Fatalf("idempotency broken for %s", again[i].Username)
		}
	}
	// 回填后运行时分配从该年最大序号 +1。
	users := NewUserRepo(gdb)
	u := &model.User{Username: "new-d", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if u.AccountCode != "YA26-00002" {
		t.Fatalf("post-backfill code = %q, want YA26-00002", u.AccountCode)
	}
}
