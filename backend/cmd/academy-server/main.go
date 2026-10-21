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
	// 默认凭据告警：本地开发默认值一旦带进生产即等于弱口令/可伪造 token。
	if cfg.JWTSecret == config.DefaultJWTSecret {
		log.Println("warning: ACADEMY_JWT_SECRET unset — using dev-only default; set a strong secret in production")
	}
	if cfg.AdminPassword == config.DefaultAdminPassword {
		log.Println("warning: ACADEMY_ADMIN_PASSWORD unset — initial admin will use the built-in default password")
	}

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
	competitionRepo := repo.NewCompetitionRepo(gdb)
	mailRepo := repo.NewMailRepo(gdb)

	// 事件总线 + 派生器（积分/进度/竞赛积分是事件的物化视图）
	bus := eventbus.New()
	deriver := service.NewDeriver(scoreRepo, progressRepo)
	deriver.SetCompetitionStores(competitionRepo, func(ctx context.Context, competitionID uint) (map[uint]string, error) {
		teams, err := competitionRepo.ListTeams(ctx, competitionID)
		if err != nil {
			return nil, err
		}
		out := make(map[uint]string, len(teams))
		for _, t := range teams {
			out[t.ID] = t.Name
		}
		return out, nil
	})
	// 内容包删除守卫：被删 Room 的 room.completed 事件重放时跳过（不复活已清进度）。
	deriver.SetRoomLookup(func(ctx context.Context, roomSlug string) bool {
		exists, err := roomRepo.ExistsBySlug(ctx, roomSlug)
		return err == nil && exists
	})
	bus.Subscribe(deriver.Handle)
	// 启动补偿：重放历史事件修复崩溃窗口丢失的派生（幂等，event-sourcing-rules skill）。
	if err := deriver.Replay(ctx, eventRepo); err != nil {
		log.Printf("deriver replay: %v", err)
	}

	// service 装配（横向依赖仅白名单：judge→event、content→challenge）
	authSvc := service.NewAuthService(userRepo, cfg.JWTSecret)
	authSvc.SetRevocationRepo(repo.NewTokenRevocationRepo(gdb))
	if err := authSvc.EnsureAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("ensure admin: %v", err)
	}
	challengeSvc := service.NewChallengeService(challengeRepo, submissionRepo, eventRepo, scoreRepo, bus)
	challengeSvc.SetAttachmentStore(repo.NewChallengeAttachmentRepo(gdb), cfg.AttachmentsDir)
	// env 实例生命周期：驱动按配置选择——auto（默认）探测 docker CLI，
	// 可用即 docker 否则 noop；docker/noop 显式值按字面生效。
	instanceRepo := repo.NewLabInstanceRepo(gdb)
	instanceLogRepo := repo.NewInstanceLogRepo(gdb)
	useDocker := false
	switch cfg.EnvDriver {
	case "docker":
		useDocker = true
	case "noop":
	default: // auto
		useDocker = service.DockerAvailable(ctx)
		if !useDocker {
			log.Println("env driver: docker unavailable, falling back to noop (lab APIs return 501); set ACADEMY_ENV_DRIVER=docker to force")
		}
	}
	var driver service.InstanceDriver = service.NoopDriver{}
	var builder service.ImageBuilder = service.NoopDriver{}
	if useDocker {
		dd := service.NewDockerDriver(cfg.InstanceMemory, cfg.InstanceCPUs, cfg.InstancePublicHost)
		driver = dd
		builder = dd
		log.Printf("env driver: docker (memory=%s cpus=%s public_host=%s)", cfg.InstanceMemory, cfg.InstanceCPUs, cfg.InstancePublicHost)
	}
	contentSvc := service.NewContentService(contentRepo, topicRepo, roomRepo, challengeSvc, builder, progressRepo)
	judgeSvc := service.NewJudgeService(submissionRepo, challengeRepo, instanceRepo, eventRepo, competitionRepo, bus)
	// ACM 判题引擎装配：ACADEMY_YAK_BIN 显式路径优先；否则探测下载目录
	//（ACADEMY_YAK_DIR，默认数据库目录）里管理端 OSS 下载的 yak 二进制。
	// 运行期可经题目管理 ACM tab 下载换装（无需重启）。
	judgeSvc.ConfigureYakEngine(cfg.YakBin, cfg.YakDownloadDir)
	if st := judgeSvc.YakEngineStatus(); st.Configured {
		log.Printf("code judge engine: yak (%s)", st.Path)
	} else {
		log.Printf("code judge engine: not configured (set ACADEMY_YAK_BIN or download via admin ACM tab; code submissions judge internal_error)")
	}
	judgeSvc.StartWorker(ctx)
	progressSvc := service.NewUserProgressService(scoreRepo, progressRepo, userRepo)
	topicSvc := service.NewTopicService(topicRepo, roomRepo, challengeRepo, submissionRepo, eventRepo)
	envSvc := service.NewEnvService(instanceRepo, instanceLogRepo, challengeRepo, userRepo, eventRepo, bus, driver, cfg.PublicURL)
	challengeSvc.SetEnvService(envSvc)
	envSvc.StartReaper(ctx, time.Minute)
	badgeSvc := service.NewBadgeService(scoreRepo, progressRepo)
	profileSvc := service.NewProfileService(userRepo, scoreRepo, mailRepo, cfg.AvatarsDir)
	competitionSvc := service.NewCompetitionService(competitionRepo, challengeRepo, userRepo, instanceRepo, submissionRepo)
	competitionSvc.SetDelivery(profileSvc.DeliverInvite, authSvc.CreateUser)
	competitionSvc.SetWriteupDir(cfg.WriteupsDir)
	competitionSvc.SetInstanceDestroyer(envSvc.DestroyCompetitionInstances)

	// api 装配（竞赛入场锁定：已入场未结束的参赛者只能访问竞赛域接口）
	router := api.NewRouter(authSvc)
	router.SetEntryLock(competitionSvc)
	smsCodeRepo := repo.NewSmsCodeRepo(gdb)
	registerSvc := service.NewRegisterService(userRepo, smsCodeRepo, nil)
	api.NewHandlers(authSvc, contentSvc, challengeSvc, judgeSvc, progressSvc, topicSvc, envSvc, badgeSvc, profileSvc, competitionSvc, registerSvc, cfg.SmsShowCode).Register(router)

	srv := &http.Server{Addr: cfg.Addr, Handler: router}
	go func() {
		log.Printf("academy-server listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	// 排水在途请求（判题入队等）后再退出；超时兜底强制关闭。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
}
