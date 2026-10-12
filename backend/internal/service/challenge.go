package service

import (
	"context"
	"crypto/subtle"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ChallengeService 题目域业务。教题一体：Challenge 必属某 Room（teaching-integration skill）。
type ChallengeService struct {
	challenges repo.ChallengeRepo
	submissions repo.SubmissionRepo
}

func NewChallengeService(c repo.ChallengeRepo, s repo.SubmissionRepo) *ChallengeService {
	return &ChallengeService{challenges: c, submissions: s}
}

// compareFlag 恒时比较（逐字节 XOR），防时序侧信道。
// 禁止改为 == 比较（event-sourcing-rules skill）。
func compareFlag(got, want string) bool {
	if len(got) != len(want) {
		// 长度不同的场景也需要恒定时间，先消耗一次比较
		subtle.ConstantTimeCompare([]byte(got), []byte(got))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// RoomDetail Room 视图：教程正文 + 内嵌 Lab（教题一体单元）。
type RoomDetail struct {
	Room       *model.Room
	Challenge  *model.Challenge // 可为 nil（纯教程 Room）
}

func (s *ChallengeService) GetRoomDetail(ctx context.Context, topicSlug, roomSlug string) (*RoomDetail, error) {
	room, err := s.findRoom(ctx, roomSlug)
	if err != nil {
		return nil, err
	}
	detail := &RoomDetail{Room: room}
	if chal, err := s.challenges.FindByRoomSlug(ctx, roomSlug); err == nil {
		detail.Challenge = chal
	}
	return detail, nil
}

// findRoom 通过 repo 查 Room（此处占位依赖 RoomRepo 由 content 服务提供场景）。
func (s *ChallengeService) findRoom(ctx context.Context, roomSlug string) (*model.Room, error) {
	// TODO(Phase2): 由构造函数注入 RoomRepo 统一查询；第一版先返回占位错误。
	return nil, errNotImplemented("findRoom")
}