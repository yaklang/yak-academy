package service

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// newTrainingTestService 内存库 + 两个 room（各带一题：一题有 env、一题无）+ 已解标记。
func newTrainingTestService(t *testing.T) (*TopicService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:trainingtest?mode=memory&cache=shared"), &gorm.Config{})
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
	roomRepo := repo.NewRoomRepo(gdb)
	if err := roomRepo.Upsert(context.Background(), &model.Room{
		Slug: "room-sqli", TopicSlug: "web-security", Title: "SQL 注入", Difficulty: model.DifficultyApprentice,
	}); err != nil {
		t.Fatal(err)
	}
	if err := roomRepo.Upsert(context.Background(), &model.Room{
		Slug: "room-crypto", TopicSlug: "crypto-basics", Title: "密码学入门", Difficulty: model.DifficultyExpert,
	}); err != nil {
		t.Fatal(err)
	}
	// 有 env 的题（挂在 web-security）与无 env 的题（挂在 crypto-basics）。
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		RoomSlug: "room-sqli", Title: "注入题", Type: model.ChallengeStatic, Score: 100,
		Image: "nginx:alpine", Port: 80,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{a}"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := chalRepo.Upsert(context.Background(), &model.Challenge{
		RoomSlug: "room-crypto", Title: "编码题", Type: model.ChallengeStatic, Score: 200,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{b}"}}, nil); err != nil {
		t.Fatal(err)
	}
	svc := NewTopicService(
		repo.NewTopicRepo(gdb), roomRepo, chalRepo, repo.NewSubmissionRepo(gdb),
	)
	return svc, gdb
}

func TestListTrainingEnrichesRoomAndEnv(t *testing.T) {
	svc, _ := newTrainingTestService(t)
	items, total, err := svc.ListTraining(context.Background(), 1, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d len=%d, want 2/2", total, len(items))
	}
	sqli, crypto := items[0], items[1]
	// 房间元数据与 env 标记拼装正确。
	if sqli.RoomSlug != "room-sqli" || sqli.RoomTitle != "SQL 注入" ||
		sqli.TopicSlug != "web-security" || sqli.Difficulty != model.DifficultyApprentice {
		t.Fatalf("sqli item = %+v", sqli)
	}
	if !sqli.HasEnv || sqli.Score != 100 {
		t.Fatalf("sqli has_env/score = %v/%d, want true/100", sqli.HasEnv, sqli.Score)
	}
	if crypto.HasEnv || crypto.TopicSlug != "crypto-basics" || crypto.Difficulty != model.DifficultyExpert {
		t.Fatalf("crypto item = %+v", crypto)
	}
	// 陌生用户：无解题记录 → solved 全 false。
	if sqli.Solved || crypto.Solved {
		t.Fatal("fresh user should have no solved items")
	}
}

func TestListTrainingSolvedForUser(t *testing.T) {
	svc, gdb := newTrainingTestService(t)
	ctx := context.Background()
	// 用户 5 直接落一条 correct 提交（绕过 judge，制造已解状态）。
	var chal model.Challenge
	if err := gdb.Where("room_slug = ?", "room-sqli").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Submission{
		UserID: 5, ChallengeID: chal.ID, Answer: "flag{a}",
		Status: model.SubmissionCorrect,
	}).Error; err != nil {
		t.Fatal(err)
	}
	items, _, err := svc.ListTraining(ctx, 5, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !items[0].Solved || items[1].Solved {
		t.Fatalf("solved flags = [%v, %v], want [true, false]", items[0].Solved, items[1].Solved)
	}
	// 其他用户视角不受影响。
	other, _, err := svc.ListTraining(ctx, 6, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if other[0].Solved {
		t.Fatal("other user must not see solved")
	}
}

func TestListTrainingPagination(t *testing.T) {
	svc, _ := newTrainingTestService(t)
	// page_size=1 的第二页应只有第二题。
	items, total, err := svc.ListTraining(context.Background(), 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 1 || items[0].RoomSlug != "room-crypto" {
		t.Fatalf("page 2 = %+v total=%d", items, total)
	}
}