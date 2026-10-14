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

// 删除题目完整性（Item 4）：存在通关记录 409 拒绝；
// 无通关记录时销毁运行中容器并级联删 flags/hints/lab_instances 行。

func newDeleteTestEnv(t *testing.T) (*ChallengeService, *EnvService, *fakeDriver, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:delchaltest?mode=memory&cache=shared"), &gorm.Config{})
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
	ctx := context.Background()
	chalRepo := repo.NewChallengeRepo(gdb)
	subRepo := repo.NewSubmissionRepo(gdb)
	evRepo := repo.NewEventRepo(gdb)
	scoreRepo := repo.NewScoreRepo(gdb)
	instanceRepo := repo.NewLabInstanceRepo(gdb)
	bus := eventbus.New()
	driver := &fakeDriver{}
	chalSvc := NewChallengeService(chalRepo, subRepo, evRepo, scoreRepo, bus)
	envSvc := NewEnvService(instanceRepo, repo.NewInstanceLogRepo(gdb), chalRepo,
		repo.NewUserRepo(gdb), evRepo, bus, driver, "http://host.docker.internal:8080")
	chalSvc.SetEnvService(envSvc)
	// 一道带 env 的题（验证删除前销毁容器）。
	if err := chalRepo.Upsert(ctx, &model.Challenge{
		Slug: "del-chal", Title: "待删题", Category: "web", Difficulty: model.DifficultyApprentice,
		Type: model.ChallengeStatic, Score: 100, Image: "nginx:alpine", Port: 80,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{del}"}},
		[]model.Hint{{Title: "提示", Content: "正文", Cost: 10}}); err != nil {
		t.Fatal(err)
	}
	return chalSvc, envSvc, driver, gdb
}

// 直插一条 running 实例（绕过 driver.Create），模拟用户在线的容器。
func seedRunningInstance(t *testing.T, gdb *gorm.DB, chalID, userID uint) uint {
	t.Helper()
	inst := &model.LabInstance{
		UserID: userID, ChallengeID: chalID, Status: model.InstanceRunning,
		Endpoint: "http://127.0.0.1:1", CheckpointToken: "tok-del",
	}
	if err := gdb.Create(inst).Error; err != nil {
		t.Fatal(err)
	}
	return inst.ID
}

func TestDeleteChallengeRefusedWhenSolved(t *testing.T) {
	chalSvc, _, _, gdb := newDeleteTestEnv(t)
	ctx := context.Background()
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "del-chal").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	// 通关历史：一条 correct 提交（事件溯源下 append-only，不可随题删除）。
	if err := repo.NewSubmissionRepo(gdb).Create(ctx, &model.Submission{
		UserID: 1, ChallengeID: chal.ID, Answer: "flag{del}", Status: model.SubmissionCorrect, Score: 100,
	}); err != nil {
		t.Fatal(err)
	}
	err := chalSvc.DeleteChallenge(ctx, "del-chal")
	if !errors.Is(err, ErrHasSolves) {
		t.Fatalf("err = %v, want ErrHasSolves", err)
	}
	// 题目及关联行原样保留。
	var n int64
	gdb.Model(&model.Challenge{}).Where("slug = ?", "del-chal").Count(&n)
	if n != 1 {
		t.Fatalf("challenge should survive, count = %d", n)
	}
}

func TestDeleteChallengeDestroysInstancesAndCascades(t *testing.T) {
	chalSvc, _, driver, gdb := newDeleteTestEnv(t)
	ctx := context.Background()
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "del-chal").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	instID := seedRunningInstance(t, gdb, chal.ID, 1)

	if err := chalSvc.DeleteChallenge(ctx, "del-chal"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// 运行中容器已销毁。
	if len(driver.destroyed) != 1 || driver.destroyed[0] != instID {
		t.Fatalf("driver.destroyed = %v, want [%d]", driver.destroyed, instID)
	}
	// 级联行全部清理：题目、flags、hints、实例行。
	for name, count := range map[string]int64{
		"challenge": countRows(t, gdb, &model.Challenge{}),
		"flag":      countRows(t, gdb, &model.Flag{}),
		"hint":      countRows(t, gdb, &model.Hint{}),
		"instance":  countRows(t, gdb, &model.LabInstance{}),
	} {
		if count != 0 {
			t.Fatalf("%s rows remain: %d", name, count)
		}
	}
}

func TestDeleteChallengeWithoutEnvServiceStillDeletes(t *testing.T) {
	// env 未注入（nil）时跳过容器销毁，直接级联删（兼容纯库测试/无实例部署）。
	chalSvc, _, _, gdb := newDeleteTestEnv(t)
	ctx := context.Background()
	seedRunningInstance2(t, gdb)
	chalSvc.SetEnvService(nil)
	if err := chalSvc.DeleteChallenge(ctx, "del-chal"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if countRows(t, gdb, &model.Challenge{}) != 0 {
		t.Fatal("challenge should be deleted")
	}
}

// seedRunningInstance2 复用 del-chal 题目 ID 的轻量种子（无返回值版本）。
func seedRunningInstance2(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "del-chal").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	seedRunningInstance(t, gdb, chal.ID, 1)
}

func countRows(t *testing.T, gdb *gorm.DB, m any) int64 {
	t.Helper()
	var n int64
	if err := gdb.Model(m).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDeleteChallengeNotFound(t *testing.T) {
	chalSvc, _, _, _ := newDeleteTestEnv(t)
	if err := chalSvc.DeleteChallenge(context.Background(), "no-such-chal"); !errors.Is(err, ErrNotFound) && err == nil {
		t.Fatalf("err = %v, want non-nil", err)
	}
}