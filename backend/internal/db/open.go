package db

import (
	"fmt"
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
		&model.User{}, &model.CourseRole{}, &model.TokenRevocation{}, &model.Mail{},
		&model.ContentPackage{}, &model.ContentVersion{},
		&model.Topic{}, &model.Room{},
		&model.Challenge{}, &model.Flag{}, &model.Hint{}, &model.Submission{},
		&model.ChallengeAttachment{},
		&model.Event{}, &model.ScoreRecord{}, &model.Progress{},
		&model.LabInstance{}, &model.InstanceLog{},
		&model.Competition{}, &model.Team{}, &model.Participant{},
		&model.CompetitionChallenge{}, &model.CheatWarning{}, &model.Writeup{},
		&model.CompetitionScore{}, &model.SmsCode{},
	); err != nil {
		return err
	}
	if err := migrateChallengeSlug(db); err != nil {
		return err
	}
	if err := migrateChallengeBuiltinFlag(db); err != nil {
		return err
	}
	if err := migrateWriteupScope(db); err != nil {
		return err
	}
	return migrateTwoRoleModel(db)
}

// migrateWriteupScope 一次性迁移：WP 模型从"每人一份"改为"题目 × 队伍/个人"后，
// 旧唯一索引 idx_wp_comp_user 与新唯一索引并存会让新 upsert 误撞旧约束；
// 旧模型行（无 challenge_id）在新模型下不可达。幂等：无旧索引/旧行时为空操作。
func migrateWriteupScope(db *gorm.DB) error {
	if db.Migrator().HasIndex("writeups", "idx_wp_comp_user") {
		if err := db.Migrator().DropIndex("writeups", "idx_wp_comp_user"); err != nil {
			return err
		}
		log.Printf("migrate writeups: dropped legacy index idx_wp_comp_user")
	}
	res := db.Exec(`DELETE FROM writeups WHERE challenge_id IS NULL OR challenge_id = 0`)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("migrate writeups: removed %d legacy per-user rows", res.RowsAffected)
	}
	return nil
}

// migrateTwoRoleModel 收敛为普通人(student)/管理员(admin)：
// 历史 teacher 全局角色降为 student；清空课程级角色（不再参与鉴权）。
func migrateTwoRoleModel(db *gorm.DB) error {
	res := db.Exec(`UPDATE users SET global_role = ? WHERE global_role = ?`,
		model.GlobalRoleStudent, model.GlobalRoleTeacher)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("migrate roles: demoted %d teacher → student", res.RowsAffected)
	}
	if err := db.Exec(`DELETE FROM course_roles`).Error; err != nil {
		return err
	}
	return nil
}

// migrateChallengeBuiltinFlag 一次性回填：is_builtin 列曾在无默认值约束下加入
// （列 DDL 为裸 numeric），迁移前已存在的旧行为 NULL；「管理员上传」过滤
// （is_builtin = 0）按 SQL 三值逻辑匹配不到 NULL，旧库会漏题。幂等：无 NULL 行时空操作。
func migrateChallengeBuiltinFlag(db *gorm.DB) error {
	res := db.Exec(`UPDATE challenges SET is_builtin = 0 WHERE is_builtin IS NULL`)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("migrate challenges: backfilled is_builtin for %d legacy rows", res.RowsAffected)
	}
	return nil
}

// migrateChallengeSlug 一次性迁移：教题一体时代的旧行没有 slug，
// 派生 slug=tutorial_slug=room_slug（旧列，已从 model 摘除但物理列仍在）、
// category=topic_slug、difficulty=room.difficulty。幂等：无旧列或无旧行时为空操作。
// room_slug 也为空（早期孤儿数据）的行派生 legacy-challenge-{id}，保主键可达。
func migrateChallengeSlug(db *gorm.DB) error {
	// 全新库从未有过 room_slug 列（AutoMigrate 只加不删），此时无旧行，直接返回。
	hasColumn := db.Migrator().HasColumn("challenges", "room_slug")
	if !hasColumn {
		return nil
	}
	rows := []struct {
		ID       uint
		RoomSlug *string
	}{}
	if err := db.Table("challenges").
		Select("id, room_slug").
		Where("slug IS NULL OR slug = ''").
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		roomSlug := ""
		if r.RoomSlug != nil {
			roomSlug = *r.RoomSlug
		}
		var room model.Room
		category, difficulty, slug := "unknown", model.DifficultyApprentice, roomSlug
		if roomSlug == "" {
			// 孤儿行：无任何可派生源，用 id 合成 slug 保唯一可达。
			slug = fmt.Sprintf("legacy-challenge-%d", r.ID)
		} else if err := db.Where("slug = ?", roomSlug).First(&room).Error; err == nil {
			category, difficulty = room.TopicSlug, room.Difficulty
		} else {
			// 教程已删但题目残留，category 兜底 unknown，只补 slug 保主键。
			log.Printf("migrate challenge %d: room %q missing, category=unknown", r.ID, roomSlug)
		}
		updates := map[string]any{
			"slug":          slug,
			"tutorial_slug": roomSlug,
			"category":      category,
			"difficulty":    difficulty,
		}
		if err := db.Table("challenges").Where("id = ?", r.ID).Updates(updates).Error; err != nil {
			return err
		}
		log.Printf("migrate challenge %d: derived slug %q", r.ID, slug)
	}
	return nil
}
