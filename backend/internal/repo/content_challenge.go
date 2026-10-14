package repo

import (
	"context"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type topicRepo struct{ db *gorm.DB }

func NewTopicRepo(db *gorm.DB) TopicRepo { return &topicRepo{db} }

func (r *topicRepo) Upsert(ctx context.Context, t *model.Topic) error {
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

type roomRepo struct{ db *gorm.DB }

func NewRoomRepo(db *gorm.DB) RoomRepo { return &roomRepo{db} }

func (r *roomRepo) Upsert(ctx context.Context, room *model.Room) error {
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

type challengeRepo struct{ db *gorm.DB }

func NewChallengeRepo(db *gorm.DB) ChallengeRepo { return &challengeRepo{db} }

func (r *challengeRepo) Upsert(ctx context.Context, c *model.Challenge, flags []model.Flag, hints []model.Hint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "slug"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"title", "category", "difficulty", "description", "type", "score", "flag_mode", "image", "port", "links", "tutorial_slug",
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

func (r *challengeRepo) List(ctx context.Context, offset, limit int) ([]model.Challenge, int64, error) {
	var (
		list  []model.Challenge
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.Challenge{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Order("id").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// ListVisible 学生端训练场列表：is_visible 且（无发布时间 或 已到发布时间）。
// release_at 存 TEXT（驱动序列化为带时区偏移的字符串），SQL 比较走字典序：
// 写入与查询必须统一 UTC，否则跨时区字符串比较会把未到期的题判为可见。
func (r *challengeRepo) ListVisible(ctx context.Context, now time.Time, offset, limit int) ([]model.Challenge, int64, error) {
	visible := "is_visible = ? AND (release_at IS NULL OR release_at <= ?)"
	now = now.UTC()
	var (
		list  []model.Challenge
		total int64
	)
	if err := r.db.WithContext(ctx).Model(&model.Challenge{}).Where(visible, true, now).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := r.db.WithContext(ctx).Where(visible, true, now).
		Order("id").Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
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

func (r *submissionRepo) HasCorrect(ctx context.Context, userID, challengeID uint) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Submission{}).
		Where("user_id = ? AND challenge_id = ? AND status = ?", userID, challengeID, model.SubmissionCorrect).
		Count(&n).Error
	return n > 0, err
}

func (r *submissionRepo) UpdateStatus(ctx context.Context, id uint, status model.SubmissionStatus, score int) error {
	return r.db.WithContext(ctx).Model(&model.Submission{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "score": score}).Error
}