package repo

import (
	"context"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
)

type awdRepo struct {
	db *gorm.DB
}

// NewAwdRepo AWD 独立域仓库（repo 层唯一允许直接持有 gorm 的位置）。
func NewAwdRepo(db *gorm.DB) AwdRepo {
	return &awdRepo{db: db}
}

func (r *awdRepo) FindGameByCompetition(ctx context.Context, competitionID uint) (*model.AwdGame, error) {
	var g model.AwdGame
	if err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).First(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *awdRepo) CreateGame(ctx context.Context, g *model.AwdGame) error {
	return r.db.WithContext(ctx).Create(g).Error
}

func (r *awdRepo) UpdateGameSelective(ctx context.Context, gameID uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.AwdGame{}).Where("id = ?", gameID).Updates(fields).Error
}

func (r *awdRepo) ListServices(ctx context.Context, competitionID uint) ([]model.AwdService, error) {
	var list []model.AwdService
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).Order("id asc").Find(&list).Error
	return list, err
}

func (r *awdRepo) FindService(ctx context.Context, id uint) (*model.AwdService, error) {
	var s model.AwdService
	if err := r.db.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *awdRepo) FindServiceBySlug(ctx context.Context, competitionID uint, slug string) (*model.AwdService, error) {
	var s model.AwdService
	err := r.db.WithContext(ctx).Where("competition_id = ? AND slug = ?", competitionID, slug).First(&s).Error
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *awdRepo) CreateService(ctx context.Context, s *model.AwdService) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *awdRepo) UpdateServiceSelective(ctx context.Context, id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.AwdService{}).Where("id = ?", id).Updates(fields).Error
}

func (r *awdRepo) DeleteService(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.AwdService{}, id).Error
}

func (r *awdRepo) CountTeamServices(ctx context.Context, serviceID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.AwdTeamService{}).Where("service_id = ?", serviceID).Count(&n).Error
	return n, err
}
