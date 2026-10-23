package db

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// Open 打开数据库并自动迁移全部实体。目标由入参形态决定：
// postgres:// / postgresql:// 开头 → PostgreSQL（Phase 4 多人规模路线，
// ACADEMY_DB_DSN）；否则视为 SQLite 文件路径（纯 Go 驱动，本地开发默认）。
func Open(pathOrDSN string) (*gorm.DB, error) {
	if IsPostgresDSN(pathOrDSN) {
		gdb, err := gorm.Open(postgres.Open(pathOrDSN), &gorm.Config{})
		if err != nil {
			return nil, err
		}
		log.Printf("db: postgresql (dsn host resolved by driver)")
		return gdb, nil
	}
	return gorm.Open(sqlite.Open(pathOrDSN), &gorm.Config{})
}

// IsPostgresDSN 入参是否为 PostgreSQL 连接串（postgres:// / postgresql:// 前缀）。
func IsPostgresDSN(s string) bool {
	return strings.HasPrefix(s, "postgres://") || strings.HasPrefix(s, "postgresql://")
}

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&model.User{}, &model.CourseRole{}, &model.TokenRevocation{}, &model.Mail{},
		&model.ContentPackage{}, &model.ContentVersion{},
		&model.Topic{}, &model.Room{},
		&model.Challenge{}, &model.Flag{}, &model.Hint{}, &model.Submission{},
		&model.ChallengeAttachment{}, &model.ACMJudge{}, &model.JudgeTestdata{},
		&model.Event{}, &model.ScoreRecord{}, &model.Progress{},
		&model.LabInstance{}, &model.InstanceLog{},
		&model.Competition{}, &model.Team{}, &model.Participant{},
		&model.CompetitionChallenge{}, &model.CheatWarning{}, &model.Writeup{},
		&model.CompetitionScore{}, &model.SmsCode{},
		&model.AdminAuditLog{},
	); err != nil {
		return err
	}
	if err := migrateChallengeSlug(db); err != nil {
		return err
	}
	if err := migrateChallengeBuiltinFlag(db); err != nil {
		return err
	}
	if err := migrateChallengeBankOwnership(db); err != nil {
		return err
	}
	if err := migrateTutorialBuiltinFlag(db); err != nil {
		return err
	}
	if err := migrateWriteupScope(db); err != nil {
		return err
	}
	if err := migrateTwoRoleModel(db); err != nil {
		return err
	}
	return migrateUserAccountCode(db)
}

// migrateUserAccountCode 一次性回填：账号编码列加入前已存在的用户按
// created_at 年份分组、id 升序补编码（与运行时 Create 的同年 MAX+1 规则一致，
// 起始序号取该年已有编码最大值 +1，部分回填后重跑不撞号）。幂等：无空编码
// 行时为空操作。
func migrateUserAccountCode(db *gorm.DB) error {
	var users []model.User
	if err := db.Where("account_code IS NULL OR account_code = ''").Order("id").Find(&users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}
	next := map[int]int{}
	for _, u := range users {
		year := u.CreatedAt.Year()
		if u.CreatedAt.IsZero() {
			year = time.Now().Year()
		}
		seq, seeded := next[year]
		if !seeded {
			var codes []string
			if err := db.Model(&model.User{}).
				Where("account_code LIKE ?", model.AccountCodePrefix(year)+"%").
				Order("account_code DESC").Limit(1).
				Pluck("account_code", &codes).Error; err != nil {
				return err
			}
			seq = 1
			if len(codes) > 0 {
				if prev, ok := model.AccountCodeSeq(codes[0]); ok {
					seq = prev + 1
				}
			}
		}
		code := model.FormatAccountCode(year, seq)
		if code == "" {
			return fmt.Errorf("migrate users: account code seq %d overflow for year %d", seq, year)
		}
		if err := db.Model(&model.User{}).Where("id = ?", u.ID).Update("account_code", code).Error; err != nil {
			return err
		}
		next[year] = seq + 1
	}
	log.Printf("migrate users: backfilled account codes for %d users", len(users))
	return nil
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
// 布尔值一律走绑定参数（SQLite 存 0/1、PostgreSQL 存 boolean，字面量 0/1 在 PG 报错）。
func migrateChallengeBuiltinFlag(db *gorm.DB) error {
	res := db.Exec(`UPDATE challenges SET is_builtin = ? WHERE is_builtin IS NULL`, false)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("migrate challenges: backfilled is_builtin for %d legacy rows", res.RowsAffected)
	}
	return nil
}

// migrateTutorialBuiltinFlag 一次性迁移：topics/rooms 的 is_builtin 列为后加，
// 历史内置导入（列加入前落库）的行按 yakit- 内置命名空间回填 true，其余行
// （含 NULL 裸列）回填 false——内置教程禁删禁编辑按此标记判定，漏标会让
// 旧库内置教程失去保护。幂等：无匹配行为空操作。布尔值走绑定参数（方言可移植）。
func migrateTutorialBuiltinFlag(db *gorm.DB) error {
	if err := db.Exec(`UPDATE topics SET is_builtin = ? WHERE slug LIKE 'yakit-%' AND (is_builtin IS NULL OR is_builtin = ?)`, true, false).Error; err != nil {
		return err
	}
	if err := db.Exec(`UPDATE topics SET is_builtin = ? WHERE is_builtin IS NULL`, false).Error; err != nil {
		return err
	}
	if err := db.Exec(`UPDATE rooms SET is_builtin = ? WHERE slug LIKE 'yakit-%' AND (is_builtin IS NULL OR is_builtin = ?)`, true, false).Error; err != nil {
		return err
	}
	return db.Exec(`UPDATE rooms SET is_builtin = ? WHERE is_builtin IS NULL`, false).Error
}

// migrateChallengeBankOwnership 一次性迁移：教程题目 tab 收敛为专属内置题集，
// 迁移前已上传到 tutorial 题库的普通题整体改判 ctf（独立夺旗式题目）。
// 幂等：无匹配行为空操作。依赖 is_builtin 已物化（COALESCE 兜底 NULL）。
func migrateChallengeBankOwnership(db *gorm.DB) error {
	res := db.Exec(`UPDATE challenges SET bank = 'ctf' WHERE bank = 'tutorial' AND COALESCE(is_builtin, ?) = ?`, false, false)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		log.Printf("migrate challenges: moved %d uploaded challenges from tutorial bank to ctf", res.RowsAffected)
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
