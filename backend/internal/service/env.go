package service

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// EnvService 题目环境生命周期。第一版仅 InstanceDriver 接口 + noop 实现
// （架构文档 §3.5：Phase 3 实现 Docker 驱动）。
type InstanceDriver interface {
	Create(ctx context.Context, challengeID uint, userID uint) (*model.LabInstance, error)
	Destroy(ctx context.Context, instanceID uint) error
	Health(ctx context.Context, instanceID uint) (model.InstanceStatus, error)
}

// NoopDriver 第一版占位驱动。
type NoopDriver struct{}

func (NoopDriver) Create(ctx context.Context, challengeID, userID uint) (*model.LabInstance, error) {
	return nil, errNotImplemented("env driver create")
}
func (NoopDriver) Destroy(ctx context.Context, instanceID uint) error {
	return errNotImplemented("env driver destroy")
}
func (NoopDriver) Health(ctx context.Context, instanceID uint) (model.InstanceStatus, error) {
	return "", errNotImplemented("env driver health")
}

type EnvService struct{ Driver InstanceDriver }

func NewEnvService(d InstanceDriver) *EnvService { return &EnvService{Driver: d} }