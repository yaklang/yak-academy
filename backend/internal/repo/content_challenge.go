package repo

import (
	"context"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type topicRepo struct{ db *gorm.DB }

func NewTopicRepo(db *gorm.DB) TopicRepo { return &topicRepo{db} }

func (r *topicRepo) Upsert(ctx context.Context, t *model.Topic) error {
	// OnConflict 不回写 is_builtin：普通包 upsert 不触碰既有行的来源标记，
	// 内置标记只由内置导入路径（MarkBuiltinTutorials）显式置位。
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slug"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "summary", "updated_at"}),
	}).Create(t).Error
}

func (r *topicRepo) List(ctx context.Context, offset, limit int) ([]model.Topic, int64, error) {
	var (
		list  []model.Topic
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.Topic{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("id").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

func (r *topicRepo) FindBySlug(ctx context.Context, slug string) (*model.Topic, error) {
	var t model.Topic
	if err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *topicRepo) ExistsBySlug(ctx context.Context, slug string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Topic{}).Where("slug = ?", slug).Count(&n).Error
	return n > 0, err
}

func (r *topicRepo) FindBuiltinBySlugs(ctx context.Context, slugs ...string) (map[string]model.Topic, error) {
	if len(slugs) == 0 {
		return map[string]model.Topic{}, nil
	}
	var list []model.Topic
	if err := r.db.WithContext(ctx).Where("slug IN ? AND is_builtin = ?", slugs, true).Find(&list).Error; err != nil {
		return nil, err
	}
	out := make(map[string]model.Topic, len(list))
	for _, t := range list {
		out[t.Slug] = t
	}
	return out, nil
}

// MarkBuiltin 批量改写 Topic 来源标记（仅内置导入调用；Upsert 不触碰此列）。
func (r *topicRepo) MarkBuiltin(ctx context.Context, slugs []string, builtin bool) error {
	if len(slugs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Topic{}).
		Where("slug IN ?", slugs).Update("is_builtin", builtin).Error
}

func (r *topicRepo) DeleteBySlugs(ctx context.Context, slugs ...string) (int64, error) {
	if len(slugs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Where("slug IN ?", slugs).Delete(&model.Topic{})
	return res.RowsAffected, res.Error
}

// FindBuiltinNotInSlugs 查不在当前内置目录里的内置 Topic 行（导入 prune 用；
// slugs 为空 = 全部内置行——GORM 空 NOT IN 语义错误，显式分支规避）。
func (r *topicRepo) FindBuiltinNotInSlugs(ctx context.Context, slugs ...string) ([]model.Topic, error) {
	q := r.db.WithContext(ctx).Where("is_builtin = ?", true)
	if len(slugs) > 0 {
		q = q.Where("slug NOT IN ?", slugs)
	}
	var list []model.Topic
	return list, q.Find(&list).Error
}

type roomRepo struct{ db *gorm.DB }

func NewRoomRepo(db *gorm.DB) RoomRepo { return &roomRepo{db} }

func (r *roomRepo) Upsert(ctx context.Context, room *model.Room) error {
	// OnConflict 不回写 is_builtin：普通包 upsert 不触碰既有行的来源标记
	//（同 topicRepo.Upsert）。
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "slug"}},
		DoUpdates: clause.AssignmentColumns([]string{"topic_slug", "title", "difficulty", "lesson_md", "video_url", "requirements", "updated_at"}),
	}).Create(room).Error
}

func (r *roomRepo) FindBySlug(ctx context.Context, slug string) (*model.Room, error) {
	var room model.Room
	if err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&room).Error; err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *roomRepo) FindBySlugs(ctx context.Context, slugs ...string) ([]model.Room, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	var list []model.Room
	err := r.db.WithContext(ctx).Where("slug IN ?", slugs).Find(&list).Error
	return list, err
}

func (r *roomRepo) ListByTopic(ctx context.Context, topicSlug string) ([]model.Room, error) {
	var list []model.Room
	err := r.db.WithContext(ctx).Where("topic_slug = ?", topicSlug).Order("id").Find(&list).Error
	return list, err
}

func (r *roomRepo) ExistsBySlug(ctx context.Context, slug string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Room{}).Where("slug = ?", slug).Count(&n).Error
	return n > 0, err
}

func (r *roomRepo) FindBuiltinBySlugs(ctx context.Context, slugs ...string) (map[string]model.Room, error) {
	if len(slugs) == 0 {
		return map[string]model.Room{}, nil
	}
	var list []model.Room
	if err := r.db.WithContext(ctx).Where("slug IN ? AND is_builtin = ?", slugs, true).Find(&list).Error; err != nil {
		return nil, err
	}
	out := make(map[string]model.Room, len(list))
	for _, room := range list {
		out[room.Slug] = room
	}
	return out, nil
}

// MarkBuiltin 批量改写 Room 来源标记（仅内置导入调用；Upsert 不触碰此列）。
func (r *roomRepo) MarkBuiltin(ctx context.Context, slugs []string, builtin bool) error {
	if len(slugs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Room{}).
		Where("slug IN ?", slugs).Update("is_builtin", builtin).Error
}

func (r *roomRepo) DeleteBySlugs(ctx context.Context, slugs ...string) (int64, error) {
	if len(slugs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Where("slug IN ?", slugs).Delete(&model.Room{})
	return res.RowsAffected, res.Error
}

// FindBuiltinNotInSlugs 查不在当前内置目录里的内置 Room 行（导入 prune 用；
// slugs 为空 = 全部内置行——GORM 空 NOT IN 语义错误，显式分支规避）。
func (r *roomRepo) FindBuiltinNotInSlugs(ctx context.Context, slugs ...string) ([]model.Room, error) {
	q := r.db.WithContext(ctx).Where("is_builtin = ?", true)
	if len(slugs) > 0 {
		q = q.Where("slug NOT IN ?", slugs)
	}
	var list []model.Room
	return list, q.Find(&list).Error
}

type challengeRepo struct{ db *gorm.DB }

func NewChallengeRepo(db *gorm.DB) ChallengeRepo { return &challengeRepo{db} }

func (r *challengeRepo) Upsert(ctx context.Context, c *model.Challenge, flags []model.Flag, hints []model.Hint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "slug"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"title", "category", "bank", "difficulty", "description", "type", "score", "flag_mode", "image", "port", "links", "tutorial_slug",
			}),
		}).Create(c).Error; err != nil {
			return err
		}
		// 重建 flags/hints（版本化内容覆盖）。先回填 challenge_id（调用方不感知自增 ID）。
		for i := range flags {
			flags[i].ChallengeID = c.ID
		}
		for i := range hints {
			hints[i].ChallengeID = c.ID
		}
		if err := tx.Where("challenge_id = ?", c.ID).Delete(&model.Flag{}).Error; err != nil {
			return err
		}
		if len(flags) > 0 {
			if err := tx.Create(&flags).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("challenge_id = ?", c.ID).Delete(&model.Hint{}).Error; err != nil {
			return err
		}
		if len(hints) > 0 {
			return tx.Create(&hints).Error
		}
		return nil
	})
}

func (r *challengeRepo) FindBySlug(ctx context.Context, slug string) (*model.Challenge, error) {
	var c model.Challenge
	if err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *challengeRepo) FindByTutorialSlug(ctx context.Context, tutorialSlug string) (*model.Challenge, error) {
	var c model.Challenge
	if err := r.db.WithContext(ctx).Where("tutorial_slug = ?", tutorialSlug).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *challengeRepo) FindByID(ctx context.Context, id uint) (*model.Challenge, error) {
	var c model.Challenge
	if err := r.db.WithContext(ctx).First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *challengeRepo) FindByIDs(ctx context.Context, ids ...uint) ([]model.Challenge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []model.Challenge
	err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&list).Error
	return list, err
}

func (r *challengeRepo) List(ctx context.Context, filter ChallengeListFilter, offset, limit int) ([]model.Challenge, int64, error) {
	var (
		list  []model.Challenge
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.Challenge{}).Scopes(challengeFilterScope(filter)).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Scopes(challengeFilterScope(filter)).Order("id").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// ListVisible 学生端题库列表：is_visible 且（无发布时间 或 已到发布时间）。
// release_at 存 TEXT（驱动序列化为带时区偏移的字符串），SQL 比较走字典序：
// 写入与查询必须统一 UTC，否则跨时区字符串比较会把未到期的题判为可见。
func (r *challengeRepo) ListVisible(ctx context.Context, now time.Time, filter ChallengeListFilter, offset, limit int) ([]model.Challenge, int64, error) {
	visible := "is_visible = ? AND (release_at IS NULL OR release_at <= ?)"
	now = now.UTC()
	var (
		list  []model.Challenge
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.Challenge{}).Where(visible, true, now).
		Scopes(challengeFilterScope(filter)).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Where(visible, true, now).
		Scopes(challengeFilterScope(filter)).
		Order("id").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// challengeFilterScope 题库过滤条件（零值字段不参与 WHERE）。
// Search 对 slug/title/category 做前缀 LIKE（管理端表格搜索）；值经 gorm
// 占位符绑定，搜索串里的 %/_ 只影响自身命中范围，无注入面。
func challengeFilterScope(f ChallengeListFilter) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if f.Bank != "" {
			db = db.Where("bank = ?", f.Bank)
		}
		if f.IsBuiltin != nil {
			db = db.Where("is_builtin = ?", *f.IsBuiltin)
		}
		if f.Category != "" {
			db = db.Where("category = ?", f.Category)
		}
		if f.Difficulty != "" {
			db = db.Where("difficulty = ?", f.Difficulty)
		}
		if f.Search != "" {
			like := strings.ToLower(f.Search) + "%"
			db = db.Where(
				"LOWER(slug) LIKE ? OR LOWER(title) LIKE ? OR LOWER(category) LIKE ?",
				like, like, like,
			)
		}
		return db
	}
}

// MarkBuiltin 批量改写来源标记（内置题集导入置 true）。slugs 为空直接返回。
func (r *challengeRepo) MarkBuiltin(ctx context.Context, slugs []string, builtin bool) (int64, error) {
	if len(slugs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.Challenge{}).
		Where("slug IN ?", slugs).Update("is_builtin", builtin)
	return res.RowsAffected, res.Error
}

// UpdateVisibility 更新可见性/发布时间。三态：isVisible/releaseAt 为 nil = 不改；
// releaseAt 为非 nil 零值 = 清空定时（写 NULL，service.VisibilityUpdate.ClearRelease 语义）；
// 非 nil 非零 = 设新发布时间。map 值用 gorm.Expr/gorm.Valuer 兼容的 nil 写 NULL。
func (r *challengeRepo) UpdateVisibility(ctx context.Context, slug string, isVisible *bool, releaseAt *time.Time) (int64, error) {
	updates := map[string]any{}
	if isVisible != nil {
		updates["is_visible"] = *isVisible
	}
	if releaseAt != nil {
		if releaseAt.IsZero() {
			updates["release_at"] = gorm.Expr("NULL")
		} else {
			// 与 ListVisible 对齐：统一 UTC 落库，保证 TEXT 字典序比较语义正确。
			updates["release_at"] = releaseAt.UTC()
		}
	}
	if len(updates) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.Challenge{}).Where("slug = ?", slug).
		Updates(updates)
	return res.RowsAffected, res.Error
}

func (r *challengeRepo) FindFlags(ctx context.Context, challengeID uint) ([]model.Flag, error) {
	var list []model.Flag
	err := r.db.WithContext(ctx).Where("challenge_id = ?", challengeID).Find(&list).Error
	return list, err
}

func (r *challengeRepo) FindHints(ctx context.Context, challengeID uint) ([]model.Hint, error) {
	var list []model.Hint
	err := r.db.WithContext(ctx).Where("challenge_id = ?", challengeID).Order("id").Find(&list).Error
	return list, err
}

func (r *challengeRepo) FindHint(ctx context.Context, hintID uint) (*model.Hint, error) {
	var h model.Hint
	if err := r.db.WithContext(ctx).First(&h, hintID).Error; err != nil {
		return nil, err
	}
	return &h, nil
}

func (r *challengeRepo) CountSolves(ctx context.Context, challengeID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Submission{}).
		Where("challenge_id = ? AND status = ?", challengeID, model.SubmissionCorrect).
		Count(&n).Error
	return n, err
}

// ClearTutorialSlugs 教程 Room 删除后清空指向它的 tutorial_slug（软引用置空，题目本体保留）。
func (r *challengeRepo) ClearTutorialSlugs(ctx context.Context, roomSlugs []string) (int64, error) {
	if len(roomSlugs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Model(&model.Challenge{}).
		Where("tutorial_slug IN ?", roomSlugs).
		Update("tutorial_slug", "")
	return res.RowsAffected, res.Error
}

func (r *challengeRepo) Delete(ctx context.Context, slug string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c model.Challenge
		if err := tx.Where("slug = ?", slug).First(&c).Error; err != nil {
			return err
		}
		if err := tx.Where("challenge_id = ?", c.ID).Delete(&model.Flag{}).Error; err != nil {
			return err
		}
		if err := tx.Where("challenge_id = ?", c.ID).Delete(&model.Hint{}).Error; err != nil {
			return err
		}
		// 题目删除后实例行失去引用：一并级联（容器本体由 service 层先销毁）。
		if err := tx.Where("challenge_id = ?", c.ID).Delete(&model.LabInstance{}).Error; err != nil {
			return err
		}
		return tx.Delete(&c).Error
	})
}

// FindBuiltinNotInSlugs 查不在当前内置目录里的内置题目行（导入 prune 用；
// slugs 为空 = 全部内置行——GORM 空 NOT IN 语义错误，显式分支规避）。
func (r *challengeRepo) FindBuiltinNotInSlugs(ctx context.Context, slugs ...string) ([]model.Challenge, error) {
	q := r.db.WithContext(ctx).Where("is_builtin = ?", true)
	if len(slugs) > 0 {
		q = q.Where("slug NOT IN ?", slugs)
	}
	var list []model.Challenge
	return list, q.Find(&list).Error
}

type challengeAttachmentRepo struct{ db *gorm.DB }

func NewChallengeAttachmentRepo(db *gorm.DB) ChallengeAttachmentRepo {
	return &challengeAttachmentRepo{db: db}
}

func (r *challengeAttachmentRepo) Upsert(ctx context.Context, a *model.ChallengeAttachment) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "challenge_id"}, {Name: "filename"}},
		DoUpdates: clause.AssignmentColumns([]string{"size", "created_at"}),
	}).Create(a).Error
}

func (r *challengeAttachmentRepo) ListByChallenge(ctx context.Context, challengeID uint) ([]model.ChallengeAttachment, error) {
	var list []model.ChallengeAttachment
	err := r.db.WithContext(ctx).Where("challenge_id = ?", challengeID).Order("filename").Find(&list).Error
	return list, err
}

func (r *challengeAttachmentRepo) Find(ctx context.Context, challengeID uint, filename string) (*model.ChallengeAttachment, error) {
	var a model.ChallengeAttachment
	err := r.db.WithContext(ctx).
		Where("challenge_id = ? AND filename = ?", challengeID, filename).
		First(&a).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *challengeAttachmentRepo) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.ChallengeAttachment{}, id).Error
}

func (r *challengeAttachmentRepo) DeleteByChallenge(ctx context.Context, challengeID uint) error {
	return r.db.WithContext(ctx).Where("challenge_id = ?", challengeID).
		Delete(&model.ChallengeAttachment{}).Error
}

type submissionRepo struct{ db *gorm.DB }

func NewSubmissionRepo(db *gorm.DB) SubmissionRepo { return &submissionRepo{db} }

func (r *submissionRepo) Create(ctx context.Context, s *model.Submission) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *submissionRepo) FindByID(ctx context.Context, id uint) (*model.Submission, error) {
	var s model.Submission
	if err := r.db.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *submissionRepo) ListPending(ctx context.Context) ([]uint, error) {
	var ids []uint
	err := r.db.WithContext(ctx).Model(&model.Submission{}).
		Where("status = ?", model.SubmissionPending).
		Order("id").Pluck("id", &ids).Error
	return ids, err
}

// HasCorrect 防重复计分：作用域 = competition_id（0 = 训练场）。
// 竞赛内首通与训练场首通相互独立，互不挤占计分。
func (r *submissionRepo) HasCorrect(ctx context.Context, userID, challengeID, competitionID uint) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Submission{}).
		Where("user_id = ? AND challenge_id = ? AND competition_id = ? AND status = ?",
			userID, challengeID, competitionID, model.SubmissionCorrect).
		Count(&n).Error
	return n > 0, err
}

// CountCorrectSolves 指定作用域内该题的通过人数（动态计分 solve_count）。
func (r *submissionRepo) CountCorrectSolves(ctx context.Context, challengeID, competitionID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Submission{}).
		Where("challenge_id = ? AND competition_id = ? AND status = ?",
			challengeID, competitionID, model.SubmissionCorrect).
		Count(&n).Error
	return n, err
}

// ListByCompetitionUser 参赛者本人的竞赛提交（最新在前，竞赛首页展示）。
func (r *submissionRepo) ListByCompetitionUser(ctx context.Context, competitionID, userID uint, limit int) ([]model.Submission, error) {
	var list []model.Submission
	err := r.db.WithContext(ctx).
		Where("competition_id = ? AND user_id = ?", competitionID, userID).
		Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

// DeleteByCompetition 删除该竞赛全部提交行（竞赛删除级联，事务由调用方管理）。
func (r *submissionRepo) DeleteByCompetition(ctx context.Context, competitionID uint) error {
	return r.db.WithContext(ctx).
		Where("competition_id = ?", competitionID).
		Delete(&model.Submission{}).Error
}

func (r *submissionRepo) UpdateStatus(ctx context.Context, id uint, status model.SubmissionStatus, score int) error {
	return r.db.WithContext(ctx).Model(&model.Submission{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "score": score}).Error
}