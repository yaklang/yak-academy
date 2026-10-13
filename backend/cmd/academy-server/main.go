// academy-server 入口：config → db → repo → service → api 装配（依赖方向唯一，project-conventions skill）。
package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/api"
	"github.com/yaklang/yak-academy/backend/internal/config"
	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/repo"
	"github.com/yaklang/yak-academy/backend/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	gdb, err := db.Open(cfg.Database)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// repo 装配
	userRepo := repo.NewUserRepo(gdb)
	topicRepo := repo.NewTopicRepo(gdb)
	roomRepo := repo.NewRoomRepo(gdb)
	challengeRepo := repo.NewChallengeRepo(gdb)
	submissionRepo := repo.NewSubmissionRepo(gdb)
	contentRepo := repo.NewContentPackageRepo(gdb)
	eventRepo := repo.NewEventRepo(gdb)
	scoreRepo := repo.NewScoreRepo(gdb)
	progressRepo := repo.NewProgressRepo(gdb)

	// 事件总线 + 派生器（积分/进度是事件的物化视图）
	bus := eventbus.New()
	deriver := service.NewDeriver(scoreRepo, progressRepo)
	bus.Subscribe(deriver.Handle)
	// 启动补偿：重放历史事件修复崩溃窗口丢失的派生（幂等，event-sourcing-rules skill）。
	if err := deriver.Replay(ctx, eventRepo); err != nil {
		log.Printf("deriver replay: %v", err)
	}

	// service 装配（横向依赖仅白名单：judge→event、content→challenge）
	authSvc := service.NewAuthService(userRepo, cfg.JWTSecret)
	if err := authSvc.EnsureAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("ensure admin: %v", err)
	}
	challengeSvc := service.NewChallengeService(challengeRepo, submissionRepo)
	contentSvc := service.NewContentService(contentRepo, topicRepo, roomRepo, challengeSvc)
	judgeSvc := service.NewJudgeService(submissionRepo, challengeRepo, eventRepo, bus)
	judgeSvc.StartWorker(ctx)
	progressSvc := service.NewUserProgressService(scoreRepo, progressRepo, userRepo)
	topicSvc := service.NewTopicService(topicRepo, roomRepo, challengeRepo)
	// env 实例生命周期：驱动按配置选择（noop 默认 / docker 需 docker CLI 可用）。
	instanceRepo := repo.NewLabInstanceRepo(gdb)
	var driver service.InstanceDriver = service.NoopDriver{}
	if cfg.EnvDriver == "docker" {
		driver = service.NewDockerDriver()
	}
	envSvc := service.NewEnvService(instanceRepo, challengeRepo, eventRepo, bus, driver)
	envSvc.StartReaper(ctx, time.Minute)

	// api 装配
	router := api.NewRouter(authSvc)
	api.NewHandlers(authSvc, contentSvc, judgeSvc, progressSvc, topicSvc, envSvc).Register(router)

	srv := &http.Server{Addr: cfg.Addr, Handler: router}
	go func() {
		log.Printf("academy-server listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
}