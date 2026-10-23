package service

// 管理端用户列表：过滤（关键字/角色/活跃度/从未完成题目）与条目画像标记。
// 口径：不活跃 = 最近登录（无则注册时间）早于基准时刻 6 个月；
// 从未完成题目 = 不存在 status=correct 的提交行。

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

func newUserListEnv(t *testing.T) (*AuthService, *gorm.DB, time.Time) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:userlist?mode=memory&cache=shared"), &gorm.Config{})
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
	// 固定基准时刻，活跃/不活跃全部相对它构造，杜绝挂钟漂移。
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	return NewAuthService(repo.NewUserRepo(gdb), "userlist-test-secret"), gdb, now
}

// seedUser 建号后按需回写 created_at（repo.Create 会自填当下时刻）与登录基线。
func seedUser(t *testing.T, gdb *gorm.DB, users repo.UserRepo, name string, created time.Time, lastLogin *time.Time) model.User {
	t.Helper()
	u := model.User{Username: name, GlobalRole: model.GlobalRoleStudent}
	if err := users.Create(context.Background(), &u); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	updates := map[string]any{"created_at": created}
	if err := gdb.Model(&model.User{}).Where("id = ?", u.ID).Updates(updates).Error; err != nil {
		t.Fatal(err)
	}
	if lastLogin != nil {
		if err := users.TouchLogin(context.Background(), u.ID, *lastLogin); err != nil {
			t.Fatal(err)
		}
	}
	u.CreatedAt = created
	return u
}

func seedCorrect(t *testing.T, gdb *gorm.DB, userID uint) {
	t.Helper()
	if err := gdb.Create(&model.Submission{
		UserID: userID, ChallengeID: 9001, Status: model.SubmissionCorrect, Score: 100,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestListUsersFiltersAndMarkers(t *testing.T) {
	svc, gdb, now := newUserListEnv(t)
	var users repo.UserRepo = repo.NewUserRepo(gdb)
	ctx := context.Background()

	m7ago := now.AddDate(0, -7, 0)   // 不活跃界外
	m1ago := now.AddDate(0, -1, 0)   // 活跃界内
	old7m := m7ago                    // 注册即 7 个月前（从未登录 → 不活跃）
	recent := now.AddDate(0, 0, -10) // 注册 10 天前（无登录 → 活跃）

	activeSolver := seedUser(t, gdb, users, "active-solver", recent, &m1ago)
	_ = seedUser(t, gdb, users, "active-nosolve", recent, nil)
	staleSolver := seedUser(t, gdb, users, "stale-solver", old7m, &m7ago)
	_ = seedUser(t, gdb, users, "stale-nosolve", old7m, nil)
	seedCorrect(t, gdb, activeSolver.ID)
	seedCorrect(t, gdb, staleSolver.ID)

	list := func(f repo.UserListFilter) map[string]repo.UserItem {
		t.Helper()
		items, total, err := svc.ListUsers(ctx, f, 0, 100)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if int(total) != len(items) {
			t.Fatalf("total = %d, items = %d", total, len(items))
		}
		got := map[string]repo.UserItem{}
		for _, it := range items {
			got[it.Username] = it
		}
		return got
	}

	// 无过滤：画像标记逐人断言。
	all := list(repo.UserListFilter{Now: now})
	if len(all) != 4 {
		t.Fatalf("unfiltered = %d users, want 4", len(all))
	}
	for name, want := range map[string][2]bool{
		"active-solver":  {true, true},
		"active-nosolve": {true, false},
		"stale-solver":   {false, true},
		"stale-nosolve":  {false, false},
	} {
		it, ok := all[name]
		if !ok {
			t.Fatalf("%s missing", name)
		}
		if it.Active != want[0] || it.SolvedAny != want[1] {
			t.Errorf("%s = active:%v solved_any:%v; want %v", name, it.Active, it.SolvedAny, want)
		}
	}
	// 编码随行返回（管理端展示/导入凭据）。
	if all["active-solver"].AccountCode == "" {
		t.Error("account_code missing on list item")
	}

	// 不活跃过滤：stale-solver + stale-nosolve。
	no := false
	inactive := list(repo.UserListFilter{Now: now, Active: &no})
	if len(inactive) != 2 || inactive["stale-solver"].Username == "" || inactive["stale-nosolve"].Username == "" {
		t.Fatalf("active=false = %v", keysOf(inactive))
	}
	// 不活跃 ∩ 从未完成题目：只剩 stale-nosolve。
	yes := true
	inactiveNoSolve := list(repo.UserListFilter{Now: now, Active: &no, NoSolves: &yes})
	if len(inactiveNoSolve) != 1 || inactiveNoSolve["stale-nosolve"].Username == "" {
		t.Fatalf("inactive+no_solves = %v", keysOf(inactiveNoSolve))
	}
	// 从未完成题目（不限活跃）：active-nosolve + stale-nosolve。
	noSolve := list(repo.UserListFilter{Now: now, NoSolves: &yes})
	if len(noSolve) != 2 || noSolve["active-nosolve"].Username == "" {
		t.Fatalf("no_solves=true = %v", keysOf(noSolve))
	}
	// 关键字按账号编码子串命中。
	byCode := list(repo.UserListFilter{Now: now, Q: activeSolver.AccountCode})
	if len(byCode) != 1 || byCode[activeSolver.Username].Username == "" {
		t.Fatalf("q=account_code = %v", keysOf(byCode))
	}

	// 角色过滤：提一个 admin。
	if _, err := svc.CreateUser(ctx, "boss", "pass123", model.GlobalRoleAdmin); err != nil {
		t.Fatal(err)
	}
	admins := list(repo.UserListFilter{Now: now, Role: "admin"})
	if len(admins) != 1 || admins["boss"].Username == "" {
		t.Fatalf("role=admin = %v", keysOf(admins))
	}
}

func keysOf(m map[string]repo.UserItem) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
