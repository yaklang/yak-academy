package repo

import (
	"context"

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
		DoUpdates: clause.AssignmentColumns([]string{"topic_slug", "title", "difficulty", "lesson_md", "requirements", "updated_at"}),
	}).Create(room).Error
}

func (r *roomRepo) FindBySlug(ctx context.Context, slug string) (*model.Room, error) {
	var room model.Room
	if err := r.db.WithContext(ctx).Where("slug = ?", slug).First(&room).Error; err != nil {
		return nil, err
	}
	return &room, nil
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
			Columns:   []clause.Column{{Name: "room_slug"}},
			DoUpdates: clause.AssignmentColumns([]string{"title", "type", "score"}),
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

func (r *challengeRepo) FindByRoomSlug(ctx context.Context, roomSlug string) (*model.Challenge, error) {
	var c model.Challenge
	if err := r.db.WithContext(ctx).Where("room_slug = ?", roomSlug).First(&c).Error; err != nil {
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

func (r *challengeRepo) FindFlags(ctx context.Context, challengeID uint) ([]model.Flag, error) {
	var list []model.Flag
	err := r.db.WithContext(ctx).Where("challenge_id = ?", challengeID).Find(&list).Error
	return list, err
}

func (r *challengeRepo) FindHints(ctx context.Context, challengeID uint) ([]model.Hint, error) {
	var list []model.Hint
	err := r.db.WithContext(ctx).Where("challenge_id = ?", challengeID).Find(&list).Error
	return list, err
}

func (r *challengeRepo) CountSolves(ctx context.Context, challengeID uint) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Submission{}).
		Where("challenge_id = ? AND status = ?", challengeID, model.SubmissionCorrect).
		Count(&n).Error
	return n, err
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