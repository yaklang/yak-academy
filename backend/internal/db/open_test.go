package db

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// newTestDB 全新内存库。isLegacy=true 时模拟"教题一体时代"的旧库：
// 用 AutoMigrate 建出含 room_slug 列的 challenges 表（model 中该字段已删，
// 全新库不会再有此列，真实旧库是历史迁移留下的）。
func newTestDB(t *testing.T, isLegacy bool) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:dbmigrate?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		dbg, _ := gdb.DB()
		dbg.Close()
	})
	if err := Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if isLegacy {
		if err := gdb.Exec("ALTER TABLE challenges ADD COLUMN room_slug TEXT").Error; err != nil {
			t.Fatalf("add legacy room_slug column: %v", err)
		}
	}
	return gdb
}

// legacyRow 直接落一行"教题一体时代"的旧行：room_slug 列在 model 中已删，
// 用 Raw SQL 构造（AutoMigrate 只加列不删列，物理列保留）。
func legacyRow(t *testing.T, gdb *gorm.DB, roomSlug *string, title string) {
	t.Helper()
	if err := gdb.Exec(
		`INSERT INTO challenges (room_slug, title, type, score, created_at) VALUES (?, ?, ?, ?, datetime('now'))`,
		roomSlug, title, model.ChallengeStatic, 100,
	).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
}

func TestMigrateFreshDBNoLegacyColumn(t *testing.T) {
	gdb := newTestDB(t, false)
	var n int64
	gdb.Model(&model.Challenge{}).Count(&n)
	if n != 0 {
		t.Fatalf("challenges = %d, want 0", n)
	}
}

func TestMigrateDerivesSlugFromRoom(t *testing.T) {
	gdb := newTestDB(t, true)
	// 造教程 Room + 指向它的旧行。
	if err := gdb.Create(&model.Topic{Slug: "web-security", Title: "Web 安全"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Room{
		Slug: "sqli-basics", TopicSlug: "web-security", Title: "SQL 注入基础",
		Difficulty: model.DifficultyExpert,
	}).Error; err != nil {
		t.Fatal(err)
	}
	roomSlug := "sqli-basics"
	legacyRow(t, gdb, &roomSlug, "找 flag")

	if err := migrateChallengeSlug(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var chal model.Challenge
	if err := gdb.First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	// slug=tutorial_slug=room_slug，category=topic_slug，difficulty=room.difficulty。
	if chal.Slug != "sqli-basics" || chal.TutorialSlug != "sqli-basics" {
		t.Fatalf("slug/tutorial_slug = %q/%q, want sqli-basics", chal.Slug, chal.TutorialSlug)
	}
	if chal.Category != "web-security" || chal.Difficulty != model.DifficultyExpert {
		t.Fatalf("category/difficulty = %q/%q", chal.Category, chal.Difficulty)
	}
}

func TestMigrateNullSlugOrphanRow(t *testing.T) {
	gdb := newTestDB(t, true)
	// 早期孤儿数据：slug 与 room_slug 都是 NULL，且无对应 Room。
	legacyRow(t, gdb, nil, "孤儿题")

	if err := migrateChallengeSlug(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var chal model.Challenge
	if err := gdb.First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	// 无可派生源：合成 legacy-challenge-{id} 保主键唯一可达，category 兜底 unknown。
	if chal.Slug == "" || chal.Slug[:16] != "legacy-challenge" {
		t.Fatalf("slug = %q, want legacy-challenge-*", chal.Slug)
	}
	if chal.TutorialSlug != "" || chal.Category != "unknown" {
		t.Fatalf("tutorial_slug/category = %q/%q, want empty/unknown", chal.TutorialSlug, chal.Category)
	}
	if chal.Difficulty != model.DifficultyApprentice {
		t.Fatalf("difficulty = %q, want apprentice default", chal.Difficulty)
	}
}

func TestMigrateMissingRoomFallsBackUnknown(t *testing.T) {
	gdb := newTestDB(t, true)
	// room_slug 指向已删除的教程：category 兜底 unknown，slug 仍取 room_slug。
	roomSlug := "deleted-room"
	legacyRow(t, gdb, &roomSlug, "残留题")

	if err := migrateChallengeSlug(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var chal model.Challenge
	if err := gdb.First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if chal.Slug != "deleted-room" || chal.Category != "unknown" {
		t.Fatalf("slug/category = %q/%q, want deleted-room/unknown", chal.Slug, chal.Category)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	gdb := newTestDB(t, true)
	roomSlug := "sqli-basics"
	legacyRow(t, gdb, &roomSlug, "找 flag")
	if err := migrateChallengeSlug(gdb); err != nil {
		t.Fatal(err)
	}
	// 二次迁移：已派生 slug 的行不再命中，无重复操作、不报错。
	if err := migrateChallengeSlug(gdb); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var n int64
	gdb.Model(&model.Challenge{}).Where("slug = ?", "sqli-basics").Count(&n)
	if n != 1 {
		t.Fatalf("rows with migrated slug = %d, want 1", n)
	}
}