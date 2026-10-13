package db

import (
	"log"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// Open 打开 SQLite 并自动迁移全部实体（纯 Go 驱动，无 CGO 依赖）。
func Open(path string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open(path), &gorm.Config{})
}

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&model.User{}, &model.CourseRole{},
		&model.ContentPackage{}, &model.ContentVersion{},
		&model.Topic{}, &model.Room{},
		&model.Challenge{}, &model.Flag{}, &model.Hint{}, &model.Submission{},
		&model.LearningPath{}, &model.PathItem{},
		&model.Event{}, &model.ScoreRecord{}, &model.Progress{},
		&model.LabInstance{},
	); err != nil {
		return err
	}
	return migrateChallengeSlug(db)
}

// migrateChallengeSlug 一次性迁移：教题一体时代的旧行没有 slug，
// 派生 slug=tutorial_slug=room_slug（旧列，已从 model 摘除但物理列仍在）、
// category=topic_slug、difficulty=room.difficulty。幂等：无旧列或无旧行时为空操作。
func migrateChallengeSlug(db *gorm.DB) error {
	// 全新库从未有过 room_slug 列（AutoMigrate 只加不删），此时无旧行，直接返回。
	hasColumn := db.Migrator().HasColumn("challenges", "room_slug")
	if !hasColumn {
		return nil
	}
	rows := []struct {
		ID       uint
		RoomSlug string
	}{}
	if err := db.Table("challenges").Select("id, room_slug").Where("slug = ''").Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		var room model.Room
		roomTopic, roomDifficulty := "", model.DifficultyApprentice
		if err := db.Where("slug = ?", r.RoomSlug).First(&room).Error; err == nil {
			roomTopic, roomDifficulty = room.TopicSlug, room.Difficulty
		} else {
			// 教程已删但题目残留，category 兜底 unknown，只补 slug 保主键。
			log.Printf("migrate challenge %d: room %q missing, category=unknown", r.ID, r.RoomSlug)
			roomTopic = "unknown"
		}
		updates := map[string]any{
			"slug":          r.RoomSlug,
			"tutorial_slug": r.RoomSlug,
			"category":      roomTopic,
			"difficulty":    roomDifficulty,
		}
		if err := db.Table("challenges").Where("id = ?", r.ID).Updates(updates).Error; err != nil {
			return err
		}
		log.Printf("migrate challenge %d: derived slug %q", r.ID, r.RoomSlug)
	}
	return nil
}
