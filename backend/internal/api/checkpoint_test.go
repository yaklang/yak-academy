package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

// 检测点回调端到端：公开路由（无 JWT）→ token 解析 → 自动判定入队 → worker 判对。

func newCheckpointTestEnv(t *testing.T) (*Router, *gorm.DB, *service.EnvService) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:apicheckpointtest?mode=memory&cache=shared"), &gorm.Config{})
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
	instanceRepo := repo.NewLabInstanceRepo(gdb)
	bus := eventbus.New()

	// 一道静态题（worker 判对后动态计分走 chal.Score）。
	if err := chalRepo.Upsert(ctx, &model.Challenge{
		Slug: "chal-cp", Title: "检测点题", Type: model.ChallengeStatic, Score: 100,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{real}"}}, nil); err != nil {
		t.Fatal(err)
	}
	judge := service.NewJudgeService(subRepo, chalRepo, instanceRepo, evRepo, repo.NewCompetitionRepo(gdb), bus)
	judge.StartWorker(ctx)
	envSvc := service.NewEnvService(
		instanceRepo, repo.NewInstanceLogRepo(gdb), chalRepo,
		repo.NewUserRepo(gdb), evRepo, bus, service.NoopDriver{}, "http://host.docker.internal:8080",
	)
	badges := service.NewBadgeService(repo.NewScoreRepo(gdb), repo.NewProgressRepo(gdb))
	auth := service.NewAuthService(repo.NewUserRepo(gdb), "test-secret")
	router := NewRouter(auth)
	NewHandlers(auth, nil, nil, judge, nil, nil, envSvc, badges, nil, nil, nil, nil, nil, nil, false).Register(router)
	return router, gdb, envSvc
}

// seedRunningInstance 直写一条运行中实例（绕过 driver），返回其 token 与 ID。
func seedRunningInstance(t *testing.T, gdb *gorm.DB, userID uint) (token string, instID uint) {
	t.Helper()
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "chal-cp").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	token = "tok-" + t.Name()
	inst := &model.LabInstance{
		UserID: userID, ChallengeID: chal.ID, Status: model.InstanceRunning,
		Endpoint: "http://127.0.0.1:1", CheckpointToken: token,
	}
	if err := gdb.Create(inst).Error; err != nil {
		t.Fatal(err)
	}
	return token, inst.ID
}

func TestCheckpointCallbackPublicAndQueued(t *testing.T) {
	router, gdb, _ := newCheckpointTestEnv(t)
	token, _ := seedRunningInstance(t, gdb, 7)

	// 无任何 Authorization 头：公开接口必须可达（容器内 checker 无 JWT）。
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checkpoints/"+token, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("checkpoint callback: status = %d, want 202", rec.Code)
	}
	var body struct {
		Status model.SubmissionStatus `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != model.SubmissionPending {
		t.Fatalf("status = %q, want pending (async judging)", body.Status)
	}

	// 异步收敛：判定为 correct 且计分（worker 复用动态计分管线）。
	deadline := waitFor(t, gdb, func() bool {
		var n int64
		gdb.Model(&model.Submission{}).Where("status = ?", model.SubmissionCorrect).Count(&n)
		return n == 1
	})
	if !deadline {
		t.Fatal("auto-solve submission did not converge to correct")
	}
	var sub model.Submission
	if err := gdb.Where("status = ?", model.SubmissionCorrect).First(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if sub.UserID != 7 || sub.Score != 100 {
		t.Fatalf("submission = user %d score %d, want user 7 score 100", sub.UserID, sub.Score)
	}
}

func TestCheckpointUnknownToken404(t *testing.T) {
	router, _, _ := newCheckpointTestEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checkpoints/no-such-token", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown token: status = %d, want 404", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "NOT_FOUND" {
		t.Fatalf("code = %q, want NOT_FOUND", env.Error.Code)
	}
}

func TestCheckpointTokenDiesWithInstance(t *testing.T) {
	router, gdb, _ := newCheckpointTestEnv(t)
	token, instID := seedRunningInstance(t, gdb, 7)
	// 实例销毁后 token 失效（防已回收环境的残留回调继续触发判定）。
	if err := gdb.Model(&model.LabInstance{}).Where("id = ?", instID).
		Update("status", model.InstanceDestroyed).Error; err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/checkpoints/"+token, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("destroyed instance token: status = %d, want 404", rec.Code)
	}
}

// waitFor 轮询条件直至超时（上限 5s），返回是否达成。
func waitFor(t *testing.T, gdb *gorm.DB, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
