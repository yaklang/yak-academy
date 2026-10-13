package service

import (
	"context"
	"crypto/subtle"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ChallengeService 题目域业务：题目是独立资源（challenge.json 上传）。
// 浏览视图（训练场/详情）在 TopicService 中；本服务供判题与内容管线使用。
type ChallengeService struct {
	challenges  repo.ChallengeRepo
	submissions repo.SubmissionRepo
}

func NewChallengeService(c repo.ChallengeRepo, s repo.SubmissionRepo) *ChallengeService {
	return &ChallengeService{challenges: c, submissions: s}
}

// UpsertChallenge 内容发布时写入题目与判定答案/提示（content→challenge 白名单依赖）。
func (s *ChallengeService) UpsertChallenge(ctx context.Context, c *model.Challenge, flags []model.Flag, hints []model.Hint) error {
	return s.challenges.Upsert(ctx, c, flags, hints)
}

// compareFlag 恒时比较，防时序侧信道。
// 禁止改为 == 比较（event-sourcing-rules skill）。
func compareFlag(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}