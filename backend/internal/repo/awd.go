package repo

import (
	"context"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func (r *awdRepo) FindGame(ctx context.Context, id uint) (*model.AwdGame, error) {
	var g model.AwdGame
	if err := r.db.WithContext(ctx).First(&g, id).Error; err != nil {
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

func (r *awdRepo) FindRound(ctx context.Context, id uint) (*model.AwdRound, error) {
	var round model.AwdRound
	if err := r.db.WithContext(ctx).First(&round, id).Error; err != nil {
		return nil, err
	}
	return &round, nil
}

func (r *awdRepo) CreateRound(ctx context.Context, round *model.AwdRound) error {
	return r.db.WithContext(ctx).Create(round).Error
}

func (r *awdRepo) UpdateRoundSelective(ctx context.Context, id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.AwdRound{}).Where("id = ?", id).Updates(fields).Error
}

// FindActiveRound 当前 running 回合（nil + record not found = 无）。
func (r *awdRepo) FindActiveRound(ctx context.Context, gameID uint) (*model.AwdRound, error) {
	var round model.AwdRound
	err := r.db.WithContext(ctx).Where("game_id = ? AND status = ?", gameID, model.AwdRoundRunning).
		Order("number desc").First(&round).Error
	if err != nil {
		return nil, err
	}
	return &round, nil
}

func (r *awdRepo) ListTeamServicesByGame(ctx context.Context, gameID uint) ([]model.AwdTeamService, error) {
	var list []model.AwdTeamService
	err := r.db.WithContext(ctx).Where("game_id = ?", gameID).Order("id asc").Find(&list).Error
	return list, err
}

func (r *awdRepo) FindTeamService(ctx context.Context, id uint) (*model.AwdTeamService, error) {
	var ts model.AwdTeamService
	if err := r.db.WithContext(ctx).First(&ts, id).Error; err != nil {
		return nil, err
	}
	return &ts, nil
}

func (r *awdRepo) FindTeamServiceByKey(ctx context.Context, gameID, serviceID uint, competitorType model.AwdCompetitorType, competitorID uint) (*model.AwdTeamService, error) {
	var ts model.AwdTeamService
	err := r.db.WithContext(ctx).Where(
		"game_id = ? AND service_id = ? AND competitor_type = ? AND competitor_id = ?",
		gameID, serviceID, competitorType, competitorID,
	).First(&ts).Error
	if err != nil {
		return nil, err
	}
	return &ts, nil
}

func (r *awdRepo) CreateTeamService(ctx context.Context, ts *model.AwdTeamService) error {
	return r.db.WithContext(ctx).Create(ts).Error
}

func (r *awdRepo) UpdateTeamServiceSelective(ctx context.Context, id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.AwdTeamService{}).Where("id = ?", id).Updates(fields).Error
}

func (r *awdRepo) FindAttackBoxByKey(ctx context.Context, competitionID uint, competitorType model.AwdCompetitorType, competitorID uint) (*model.AwdAttackBox, error) {
	var box model.AwdAttackBox
	err := r.db.WithContext(ctx).Where(
		"competition_id = ? AND competitor_type = ? AND competitor_id = ?",
		competitionID, competitorType, competitorID,
	).First(&box).Error
	if err != nil {
		return nil, err
	}
	return &box, nil
}

func (r *awdRepo) CreateAttackBox(ctx context.Context, box *model.AwdAttackBox) error {
	return r.db.WithContext(ctx).Create(box).Error
}

func (r *awdRepo) UpdateAttackBoxSelective(ctx context.Context, id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.AwdAttackBox{}).Where("id = ?", id).Updates(fields).Error
}

// CreateFlags 批量写回合 flag；唯一索引冲突（重复播种）静默跳过，保证重放幂等。
func (r *awdRepo) CreateFlags(ctx context.Context, flags []model.AwdFlag) error {
	if len(flags) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&flags).Error
}

func (r *awdRepo) FindFlagByHash(ctx context.Context, valueHash string) (*model.AwdFlag, error) {
	var f model.AwdFlag
	if err := r.db.WithContext(ctx).Where("value_hash = ?", valueHash).First(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *awdRepo) FindFlagByRoundTeamService(ctx context.Context, roundID, teamServiceID uint) (*model.AwdFlag, error) {
	var f model.AwdFlag
	err := r.db.WithContext(ctx).Where("round_id = ? AND team_service_id = ?", roundID, teamServiceID).First(&f).Error
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *awdRepo) FindFlagByID(ctx context.Context, id uint) (*model.AwdFlag, error) {
	var f model.AwdFlag
	if err := r.db.WithContext(ctx).First(&f, id).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *awdRepo) CreateAttack(ctx context.Context, attack *model.AwdAttack) error {
	return r.db.WithContext(ctx).Create(attack).Error
}

func (r *awdRepo) HasAcceptedAttack(ctx context.Context, flagID uint) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.AwdAttack{}).
		Where("flag_id = ? AND status = ?", flagID, model.AwdAttackAccepted).Count(&n).Error
	return n > 0, err
}

func (r *awdRepo) ListPendingAttacks(ctx context.Context, limit int) ([]model.AwdAttack, error) {
	if limit <= 0 {
		limit = 100
	}
	var list []model.AwdAttack
	err := r.db.WithContext(ctx).Where("status = ?", model.AwdAttackPending).
		Order("submitted_at asc, id asc").Limit(limit).Find(&list).Error
	return list, err
}

// UpdateAttackStatus 条件更新（仅 pending → accepted/rejected）：
// 并发/重放时只有一次生效，返回受影响行数。
func (r *awdRepo) UpdateAttackStatus(ctx context.Context, id uint, status model.AwdAttackStatus, reason string, judgedAt time.Time) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.AwdAttack{}).
		Where("id = ? AND status = ?", id, model.AwdAttackPending).
		Updates(map[string]any{"status": status, "reason": reason, "judged_at": judgedAt})
	return res.RowsAffected, res.Error
}

func (r *awdRepo) CreateCheck(ctx context.Context, check *model.AwdCheck) error {
	return r.db.WithContext(ctx).Create(check).Error
}

func (r *awdRepo) CreateIncident(ctx context.Context, incident *model.AwdIncident) error {
	return r.db.WithContext(ctx).Create(incident).Error
}
