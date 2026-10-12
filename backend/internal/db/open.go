package db

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// Open 打开 SQLite 并自动迁移全部实体（纯 Go 驱动，无 CGO 依赖）。
func Open(path string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open(path), &gorm.Config{})
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.User{}, &model.CourseRole{},
		&model.ContentPackage{}, &model.ContentVersion{},
		&model.Topic{}, &model.Room{},
		&model.Challenge{}, &model.Flag{}, &model.Hint{}, &model.Submission{},
		&model.LearningPath{}, &model.PathItem{},
		&model.Event{}, &model.ScoreRecord{}, &model.Progress{},
		&model.LabInstance{},
	)
}
