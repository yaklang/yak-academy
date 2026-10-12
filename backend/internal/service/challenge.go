package service

import (
	"crypto/subtle"

	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ChallengeService 题目域业务。教题一体：Challenge 必属某 Room（teaching-integration skill）。
// 浏览视图（Room 详情）在 TopicService 中；本服务供判题与内容管线使用。
type ChallengeService struct {
	challenges  repo.ChallengeRepo
	submissions repo.SubmissionRepo
}

func NewChallengeService(c repo.ChallengeRepo, s repo.SubmissionRepo) *ChallengeService {
	return &ChallengeService{challenges: c, submissions: s}
}

// compareFlag 恒时比较，防时序侧信道。
// 禁止改为 == 比较（event-sourcing-rules skill）。
func compareFlag(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}