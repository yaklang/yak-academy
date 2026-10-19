package repo

import (
	"context"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
)

// CompetitionRepo 竞赛域 sqlite 实现（competition/team/participant/
// competition_challenge/cheat_warning/writeup/competition_score 七表）。

type competitionRepo struct{ db *gorm.DB }

func NewCompetitionRepo(db *gorm.DB) CompetitionRepo { return &competitionRepo{db} }

func (r *competitionRepo) Create(ctx context.Context, c *model.Competition) error {
	return r.db.WithContext(ctx).Create(c).Error
}

func (r *competitionRepo) FindByID(ctx context.Context, id uint) (*model.Competition, error) {
	var c model.Competition
	if err := r.db.WithContext(ctx).First(&c, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *competitionRepo) List(ctx context.Context, offset, limit int) ([]model.Competition, int64, error) {
	var (
		list  []model.Competition
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.Competition{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// UpdateSelective 只更新请求里出现的字段（map 形式，零值可更新）。
func (r *competitionRepo) UpdateSelective(ctx context.Context, id uint, fields map[string]any) error {
	return r.db.WithContext(ctx).Model(&model.Competition{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteCascade 删除竞赛及本域全部关联行（participants/teams/bindings/
// warnings/writeups/scores + 竞赛本体 + 竞赛内提交与容器记录行），单事务。
// 派生行（CompetitionScore）随聚合删除——事件流 append-only 保留，但
// 竞赛已删，重放不会复活（deriver 的首通幂等插入无竞赛行可挂靠）。
// 落盘 WP zip 与运行容器由 service 层在事务外清理（文件/驱动非事务资源）。
func (r *competitionRepo) DeleteCascade(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{
			&model.Participant{}, &model.Team{}, &model.CompetitionChallenge{},
			&model.CheatWarning{}, &model.Writeup{}, &model.CompetitionScore{},
			&model.Submission{}, &model.LabInstance{},
		} {
			if err := tx.Where("competition_id = ?", id).Delete(m).Error; err != nil {
				return err
			}
		}
		return tx.Where("id = ?", id).Delete(&model.Competition{}).Error
	})
}

func (r *competitionRepo) CreateTeam(ctx context.Context, t *model.Team) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// FindTeamByName 竞赛内战队名查重（nil = 不存在）。
func (r *competitionRepo) FindTeamByName(ctx context.Context, competitionID uint, name string) (*model.Team, error) {
	var t model.Team
	err := r.db.WithContext(ctx).First(&t, "competition_id = ? AND name = ?", competitionID, name).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTeams 竞赛全部战队（id 升序，导入顺序稳定）。
func (r *competitionRepo) ListTeams(ctx context.Context, competitionID uint) ([]model.Team, error) {
	var teams []model.Team
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).Order("id").Find(&teams).Error
	return teams, err
}

func (r *competitionRepo) CreateParticipant(ctx context.Context, p *model.Participant) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// FindParticipant 该竞赛该用户的参赛记录（nil = 未参赛）。
func (r *competitionRepo) FindParticipant(ctx context.Context, competitionID, userID uint) (*model.Participant, error) {
	var p model.Participant
	err := r.db.WithContext(ctx).First(&p, "competition_id = ? AND user_id = ?", competitionID, userID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FindParticipantByCode 邀请码解析（nil = 无效邀请码）。
func (r *competitionRepo) FindParticipantByCode(ctx context.Context, code string) (*model.Participant, error) {
	var p model.Participant
	err := r.db.WithContext(ctx).First(&p, "invite_code = ?", code).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListParticipants 竞赛参赛者分页（id 升序，与导入顺序一致）。
func (r *competitionRepo) ListParticipants(ctx context.Context, competitionID uint, offset, limit int) ([]model.Participant, int64, error) {
	var (
		list  []model.Participant
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.Participant{}).
		Where("competition_id = ?", competitionID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).
		Order("id").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// CountParticipants 已入场的参赛人数（管理端统计）。
func (r *competitionRepo) CountEntered(ctx context.Context, competitionID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Participant{}).
		Where("competition_id = ? AND entered_at IS NOT NULL", competitionID).Count(&n).Error
	return n, err
}

// MarkEntered 入场（幂等：已入场再标记返回 0）。
func (r *competitionRepo) MarkEntered(ctx context.Context, id uint) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.Participant{}).
		Where("id = ? AND entered_at IS NULL", id).
		Update("entered_at", time.Now().UTC())
	return res.RowsAffected, res.Error
}

// UpdateParticipantIP 记录最近提交来源 IP（覆盖语义，反作弊基线）。
func (r *competitionRepo) UpdateParticipantIP(ctx context.Context, competitionID, userID uint, ip string) error {
	return r.db.WithContext(ctx).Model(&model.Participant{}).
		Where("competition_id = ? AND user_id = ?", competitionID, userID).
		Update("last_ip", ip).Error
}

// BindChallenges 幂等绑定题目：已绑定的跳过，返回新增数。事务内。
func (r *competitionRepo) BindChallenges(ctx context.Context, competitionID uint, challengeIDs []uint) (int64, error) {
	added := int64(0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, cid := range challengeIDs {
			bind := model.CompetitionChallenge{CompetitionID: competitionID, ChallengeID: cid}
			res := tx.Where(bind).FirstOrCreate(&bind)
			if res.Error != nil {
				return res.Error
			}
			added += res.RowsAffected
		}
		return nil
	})
	return added, err
}

// UnbindChallenges 解绑（返回移除数）。
func (r *competitionRepo) UnbindChallenges(ctx context.Context, competitionID uint, challengeIDs []uint) (int64, error) {
	if len(challengeIDs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Where("competition_id = ? AND challenge_id IN ?", competitionID, challengeIDs).
		Delete(&model.CompetitionChallenge{})
	return res.RowsAffected, res.Error
}

// ListBoundChallengeIDs 该竞赛绑定的题目 ID（id 升序 = 绑定顺序）。
func (r *competitionRepo) ListBoundChallengeIDs(ctx context.Context, competitionID uint) ([]uint, error) {
	var rows []model.CompetitionChallenge
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).Order("id").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]uint, len(rows))
	for i, b := range rows {
		ids[i] = b.ChallengeID
	}
	return ids, nil
}

func (r *competitionRepo) CreateWarning(ctx context.Context, w *model.CheatWarning) error {
	return r.db.WithContext(ctx).Create(w).Error
}

// ListWarnings 竞赛警告（时间正序：异常发生顺序）。
func (r *competitionRepo) ListWarnings(ctx context.Context, competitionID uint) ([]model.CheatWarning, error) {
	var list []model.CheatWarning
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).Order("id").Find(&list).Error
	return list, err
}

// UpsertWriteup WP upsert（单位 = 题目 × 队伍/个人）：重复提交覆盖同一份，
// w 的 UserID 记录最近修改人。
func (r *competitionRepo) UpsertWriteup(ctx context.Context, w *model.Writeup) error {
	// map 条件：TeamID=0（个人）是有效值，struct 条件会丢零值字段（GORM 陷阱）。
	return r.db.WithContext(ctx).
		Where(map[string]any{
			"competition_id": w.CompetitionID, "challenge_id": w.ChallengeID, "team_id": w.TeamID,
		}).
		// Assign 用 map：Content 清空（零值）也要能更新（GORM struct 零值陷阱）。
		Assign(map[string]any{
			"content": w.Content, "zip_file": w.ZipFile, "zip_name": w.ZipName, "user_id": w.UserID,
		}).
		FirstOrCreate(w).Error
}

// ListWriteups 竞赛全部 WP（管理端列表、视图中已提交判定走这里）。
func (r *competitionRepo) ListWriteups(ctx context.Context, competitionID uint) ([]model.Writeup, error) {
	var list []model.Writeup
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).Order("challenge_id, id").Find(&list).Error
	return list, err
}

// FindWriteup 单份 WP（zip 下载按 id 直取）。
func (r *competitionRepo) FindWriteup(ctx context.Context, id uint) (*model.Writeup, error) {
	var w model.Writeup
	err := r.db.WithContext(ctx).First(&w, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// AddScore 竞赛派生积分：新首通插入，已存在（该用户该题）返回 0 行。
// 仅事件派生器调用（event-sourcing-rules skill）。
func (r *competitionRepo) AddScore(ctx context.Context, rec *model.CompetitionScore) (int64, error) {
	res := r.db.WithContext(ctx).Where(model.CompetitionScore{
		CompetitionID: rec.CompetitionID, UserID: rec.UserID, ChallengeID: rec.ChallengeID,
	}).FirstOrCreate(rec)
	return res.RowsAffected, res.Error
}

// ListScores 该竞赛全部派生积分行（排行榜聚合用）。
func (r *competitionRepo) ListScores(ctx context.Context, competitionID uint) ([]model.CompetitionScore, error) {
	var list []model.CompetitionScore
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).Find(&list).Error
	return list, err
}

// ActiveCompetitions 当前处于 running 状态的竞赛（锁定判定用）。
// 时间比较在 SQL 侧完成，避免时钟注入差异。入参统一转 UTC：
// SQLite 以文本比较时间，与落库的 UTC 值保持同一基准才可靠。
func (r *competitionRepo) ActiveCompetitions(ctx context.Context, now time.Time) ([]model.Competition, error) {
	var list []model.Competition
	err := r.db.WithContext(ctx).Where("start_time <= ? AND end_time > ?", now.UTC(), now.UTC()).Find(&list).Error
	return list, err
}