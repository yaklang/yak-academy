package service

import (
	"context"
	"crypto/subtle"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ChallengeService 题目域业务：题目是独立资源（challenge.json / challenge.yml 包上传）。
// 浏览视图（训练场/详情）在 TopicService 中；本服务供判题、内容管线与提示解锁使用。
type ChallengeService struct {
	challenges  repo.ChallengeRepo
	submissions repo.SubmissionRepo
	events      repo.EventRepo
	scores      repo.ScoreRepo
	bus         *eventbus.Bus
	// env 延迟注入（main 装配顺序：envSvc 依赖 challengeRepo 构造在后）。
	// 删除题目前销毁运行中容器用；nil 时跳过（仅测试场景）。
	env *EnvService
}

func NewChallengeService(c repo.ChallengeRepo, s repo.SubmissionRepo, ev repo.EventRepo, scores repo.ScoreRepo, bus *eventbus.Bus) *ChallengeService {
	return &ChallengeService{challenges: c, submissions: s, events: ev, scores: scores, bus: bus}
}

// SetEnvService 注入实例服务（main 中 envSvc 构造后调用一次）。
func (s *ChallengeService) SetEnvService(env *EnvService) { s.env = env }

// UpsertChallenge 内容发布时写入题目与判定答案/提示（content→challenge 白名单依赖）。
func (s *ChallengeService) UpsertChallenge(ctx context.Context, c *model.Challenge, flags []model.Flag, hints []model.Hint) error {
	return s.challenges.Upsert(ctx, c, flags, hints)
}

// compareFlag 恒时比较，防时序侧信道。
// 禁止改为 == 比较（event-sourcing-rules skill）。
func compareFlag(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
