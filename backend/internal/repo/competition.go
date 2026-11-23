package repo

import (
	"context"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// ListPublic 竞赛广场分页：仅公开赛（kind=public；邀请赛定向，不对外展示），
// 开始时间倒序（最近开赛的排前面），同刻按 id 倒序稳定排序。
func (r *competitionRepo) ListPublic(ctx context.Context, offset, limit int) ([]model.Competition, int64, error) {
	var (
		list  []model.Competition
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Competition{}).Where("kind = ?", model.CompetitionPublic)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("start_time DESC").Order("id DESC").Offset(offset).Limit(limit).Find(&list).Error
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
			&model.Participant{}, &model.CompetitionProfile{}, &model.Team{}, &model.CompetitionChallenge{},
			&model.CheatWarning{}, &model.Writeup{}, &model.CompetitionScore{},
			&model.Certificate{}, &model.CertificateRule{},
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
// UpsertProfile 竞赛+用户唯一键覆盖写：重复导入更新实名信息，不产生新行。
func (r *competitionRepo) UpsertProfile(ctx context.Context, profile *model.CompetitionProfile) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "competition_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"real_name", "school", "student_no", "email", "phone", "updated_at",
		}),
	}).Create(profile).Error
}

// FindProfile 单人身份名册（nil = 尚未填写）。
func (r *competitionRepo) FindProfile(ctx context.Context, competitionID, userID uint) (*model.CompetitionProfile, error) {
	var profile model.CompetitionProfile
	err := r.db.WithContext(ctx).
		First(&profile, "competition_id = ? AND user_id = ?", competitionID, userID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// ListProfiles 竞赛全部身份名册（user_id 升序）。
func (r *competitionRepo) ListProfiles(ctx context.Context, competitionID uint) ([]model.CompetitionProfile, error) {
	var list []model.CompetitionProfile
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).
		Order("user_id").Find(&list).Error
	return list, err
}

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

// CreateCtfAdjustment 追加一条附加分（只 INSERT；修正走新记录，禁止 UPDATE）。
func (r *competitionRepo) CreateCtfAdjustment(ctx context.Context, a *model.CtfAdjustment) error {
	return r.db.WithContext(ctx).Create(a).Error
}

// ListCtfAdjustments 该竞赛全部附加分明细（按创建时间正序）。
func (r *competitionRepo) ListCtfAdjustments(ctx context.Context, competitionID uint) ([]model.CtfAdjustment, error) {
	var list []model.CtfAdjustment
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).
		Order("created_at asc, id asc").Find(&list).Error
	return list, err
}

// SumPositiveCtfAdjustments 单人在该竞赛的正向附加分合计。
func (r *competitionRepo) SumPositiveCtfAdjustments(ctx context.Context, competitionID, userID uint) (int, error) {
	var total int
	err := r.db.WithContext(ctx).Model(&model.CtfAdjustment{}).
		Where("competition_id = ? AND user_id = ? AND points > 0", competitionID, userID).
		Select("COALESCE(SUM(points), 0)").Scan(&total).Error
	return total, err
}

// CreateQualificationRule 落一条已应用的晋级规则。
func (r *competitionRepo) CreateQualificationRule(ctx context.Context, rule *model.QualificationRule) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

// CreateQualificationEntries 批量落中选名单（单事务）。
func (r *competitionRepo) CreateQualificationEntries(ctx context.Context, entries []model.QualificationEntry) error {
	if len(entries) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&entries).Error
}

// ListQualificationRules 目标竞赛历史晋级规则（新→旧）。
func (r *competitionRepo) ListQualificationRules(ctx context.Context, targetCompetitionID uint) ([]model.QualificationRule, error) {
	var list []model.QualificationRule
	err := r.db.WithContext(ctx).Where("target_competition_id = ?", targetCompetitionID).
		Order("created_at desc, id desc").Find(&list).Error
	return list, err
}

// ListQualificationEntries 某条规则的中选名单（按名次）。
func (r *competitionRepo) ListQualificationEntries(ctx context.Context, ruleID uint) ([]model.QualificationEntry, error) {
	var list []model.QualificationEntry
	err := r.db.WithContext(ctx).Where("rule_id = ?", ruleID).Order("rank asc, user_id asc").Find(&list).Error
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

// FindCertificateRuleByTitle 竞赛内证书标题查重（nil = 不存在）。
func (r *competitionRepo) FindCertificateRuleByTitle(ctx context.Context, competitionID uint, title string) (*model.CertificateRule, error) {
	var rule model.CertificateRule
	err := r.db.WithContext(ctx).
		First(&rule, "competition_id = ? AND title = ?", competitionID, title).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// ListCertificateRules 竞赛已签发规则（新→旧）。
func (r *competitionRepo) ListCertificateRules(ctx context.Context, competitionID uint) ([]model.CertificateRule, error) {
	var rules []model.CertificateRule
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).
		Order("id DESC").Find(&rules).Error
	return rules, err
}

// CreateCertificateRuleWithCertificates 单事务落规则、证书与签发时间。
func (r *competitionRepo) CreateCertificateRuleWithCertificates(ctx context.Context, rule *model.CertificateRule, certs []model.Certificate) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rule).Error; err != nil {
			return err
		}
		for i := range certs {
			certs[i].RuleID = rule.ID
			certs[i].CompetitionID = rule.CompetitionID
			certs[i].Level = rule.Level
		}
		if len(certs) > 0 {
			if err := tx.Create(&certs).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.CertificateRule{}).Where("id = ?", rule.ID).
			Update("issued_at", rule.IssuedAt).Error
	})
}

// ListCertificates 竞赛全部证书（id 升序）。
func (r *competitionRepo) ListCertificates(ctx context.Context, competitionID uint) ([]model.Certificate, error) {
	var list []model.Certificate
	err := r.db.WithContext(ctx).Where("competition_id = ?", competitionID).
		Order("id").Find(&list).Error
	return list, err
}

// FindCertificateByCode 验证码解析（nil = 不存在）。
func (r *competitionRepo) FindCertificateByCode(ctx context.Context, code string) (*model.Certificate, error) {
	var cert model.Certificate
	err := r.db.WithContext(ctx).First(&cert, "code = ?", code).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// FindCertificateRule 单条签发规则（公开验证页取证书名称）。
func (r *competitionRepo) FindCertificateRule(ctx context.Context, id uint) (*model.CertificateRule, error) {
	var rule model.CertificateRule
	if err := r.db.WithContext(ctx).First(&rule, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}
