package service

import (
	"context"
	"fmt"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// requireCompetitionFormat 校验入口的竞赛形式。共享核心（报名/入场/名册/
// 锁定/赛程）不限制形式；CTF 专用操作必须经过本守卫，避免 AWD/考试赛被
// 既有 CTF 路由或服务误用。
func requireCompetitionFormat(c *model.Competition, want model.CompetitionFormat) error {
	if c == nil {
		return ErrNotFound
	}
	if c.Format != want {
		return fmt.Errorf("%w: competition format %q does not support this operation (requires %q)",
			ErrValidation, c.Format, want)
	}
	return nil
}

// requireFormat 读取竞赛并校验形式，供只需要存在性 + 形式的 CTF 管理端操作使用。
func (s *CompetitionService) requireFormat(ctx context.Context, id uint, format model.CompetitionFormat) (*model.Competition, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := requireCompetitionFormat(c, format); err != nil {
		return nil, err
	}
	return c, nil
}
