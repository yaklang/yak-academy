package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// newBackupFixture 文件型 SQLite（VACUUM INTO 快照需要真实文件库）+
// 三个数据目录（attachments/avatars 有内容，writeups 缺省不存在）。
func newBackupFixture(t *testing.T) (*BackupService, string, *gorm.DB) {
	t.Helper()
	root := t.TempDir()
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(root, "academy.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := gdb.DB()
		sqlDB.Close()
	})
	if err := gdb.Create(&model.User{Username: "backup-tester", PasswordHash: "x", GlobalRole: model.GlobalRoleStudent}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	for _, rel := range []string{
		filepath.Join("attachments", "web-lfi", "payload.txt"),
		filepath.Join("avatars", "1", "a.png"),
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte("payload-bytes"), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	svc := NewBackupService(repo.NewBackupRepo(gdb), false, filepath.Join(root, "backups"), 7,
		filepath.Join(root, "attachments"), filepath.Join(root, "avatars"),
		filepath.Join(root, "writeups"), "fs")
	return svc, root, gdb
}

func archiveEntryNames(t *testing.T, archivePath string) []string {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			names = append(names, hdr.Name)
		}
	}
	return names
}

func TestBackupCreateListAndArchiveContents(t *testing.T) {
	svc, root, _ := newBackupFixture(t)
	info, err := svc.Create(context.Background())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !ValidBackupName(info.Name) {
		t.Fatalf("archive name %q not in canonical form", info.Name)
	}
	if info.SizeBytes <= 0 || info.Database != "sqlite" {
		t.Fatalf("unexpected info: %+v", info)
	}

	list, err := svc.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v; want 1 entry", list, err)
	}
	names := archiveEntryNames(t, filepath.Join(root, "backups", info.Name))
	joined := strings.Join(names, "\n")
	for _, want := range []string{
		"manifest.json", "academy.db",
		"attachments/web-lfi/payload.txt", "avatars/1/a.png",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("archive missing %s; entries: %s", want, joined)
		}
	}
	// writeups 目录不存在 → 不产生空区块。
	if strings.Contains(joined, "writeups") {
		t.Errorf("archive should skip absent writeups dir; entries: %s", joined)
	}
}

func TestBackupPruneKeepsNewest(t *testing.T) {
	svc, root, _ := newBackupFixture(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.keep = 2
	for i := 0; i < 3; i++ {
		svc.now = func() time.Time { return base.Add(time.Duration(i) * time.Second) }
		if _, err := svc.Create(context.Background()); err != nil {
			t.Fatalf("create #%d: %v", i, err)
		}
	}
	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("after prune want 2 archives, got %d", len(list))
	}
	// 新→旧：保留第二、第三秒的归档，最早的被清。
	if !strings.Contains(list[1].Name, "000001") || !strings.Contains(list[0].Name, "000002") {
		t.Fatalf("prune kept wrong archives: %s, %s", list[0].Name, list[1].Name)
	}
	if _, err := os.Stat(filepath.Join(root, "backups", list[1].Name)); err != nil {
		t.Fatalf("kept archive readable: %v", err)
	}
}

func TestBackupOpenRejectsInvalidNames(t *testing.T) {
	svc, _, _ := newBackupFixture(t)
	if _, _, err := svc.Open(context.Background(), "../academy.db"); !errors.Is(err, ErrValidation) {
		t.Fatalf("traversal name: err = %v, want ErrValidation", err)
	}
	if _, _, err := svc.Open(context.Background(), "academy-20990101-000000.tar.gz"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing archive: err = %v, want ErrRecordNotFound", err)
	}
}

func TestBackupPostgresModeRefused(t *testing.T) {
	svc, _, _ := newBackupFixture(t)
	svc.isPostgres = true
	if _, err := svc.Create(context.Background()); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (pg mode refuses in-app backup)", err)
	}
}
