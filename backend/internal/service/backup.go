package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/pathutil"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// BackupService 平台数据快照：SQLite 一致性快照（VACUUM INTO）+ 数据目录
// （attachments/avatars/writeups）打包为单文件 tar.gz 归档，按份数保留自动清理。
// 归档落在 BackupDir（部署时随数据卷持久化）；异地容灾由运维侧把该目录
// rsync / 上传对象存储（平台不内置，见 docs/architecture.md §11）。
//
// PostgreSQL 模式不支持应用内备份（VACUUM INTO 是 SQLite 特性）：Create 返回
// ErrValidation，数据库备份走外部 pg_dump——应用内归档会给出"已备份"的假象，
// 宁可直接拒绝。
type BackupService struct {
	snapshots    repo.BackupRepo
	isPostgres   bool
	backupDir    string
	keep         int
	attachments  string
	avatars      string
	writeups     string
	writeupsKind string // fs / s3（s3 时 WP zip 不入归档，manifest 记录说明）
	now          func() time.Time
}

// NewBackupService 构造。keep < 1 收敛为 1（至少保留最近一份，防止清理全部）。
func NewBackupService(snapshots repo.BackupRepo, isPostgres bool, backupDir string, keep int, attachments, avatars, writeups, writeupsKind string) *BackupService {
	if keep < 1 {
		keep = 1
	}
	return &BackupService{
		snapshots: snapshots, isPostgres: isPostgres, backupDir: backupDir, keep: keep,
		attachments: attachments, avatars: avatars, writeups: writeups, writeupsKind: writeupsKind,
		now: time.Now,
	}
}

// BackupInfo 归档元数据（列表/创建响应）。
type BackupInfo struct {
	Name       string    `json:"name"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	Database   string    `json:"database"` // sqlite（归档内含快照）
	StoreKinds string    `json:"store_kinds"`
}

// backupNamePattern 归档文件名唯一合法形态（同时是 Open/下载入参的白名单）。
var backupNamePattern = regexp.MustCompile(`^academy-\d{8}-\d{6}\.tar\.gz$`)

// manifest 归档内自描述清单（恢复时辨认内容与边界）。
type backupManifest struct {
	CreatedAt    time.Time `json:"created_at"`
	Database     string    `json:"database"`
	WriteupsKind string    `json:"writeups_store"`
	// WriteupsNote s3 模式下 WP zip 在对象存储（不在本归档），恢复需单独处理。
	WriteupsNote string `json:"writeups_note,omitempty"`
}

// Create 生成一份完整备份：快照库 → tar.gz（manifest + academy.db + 数据目录）
// → 原子改名落位 → 按份数清理旧归档。同步执行（百 MB 级数据秒级完成）。
func (s *BackupService) Create(ctx context.Context) (*BackupInfo, error) {
	if s.isPostgres {
		return nil, fmt.Errorf("%w: in-app backup supports SQLite only; dump the PostgreSQL database externally with pg_dump and back up data dirs separately", ErrValidation)
	}
	if err := os.MkdirAll(s.backupDir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup dir: %w", err)
	}
	now := s.now().UTC()
	name := fmt.Sprintf("academy-%s.tar.gz", now.Format("20060102-150405"))
	dest := pathutil.Join(s.backupDir, name)
	if _, err := os.Stat(dest); err == nil {
		return nil, fmt.Errorf("%w: backup %s already exists, retry in a second", ErrValidation, name)
	}

	// 库快照（文件名带纳秒防同秒并发冲突；归档后即删）。
	snapshot := pathutil.Join(s.backupDir, fmt.Sprintf("snapshot-%d.db", now.UnixNano()))
	if err := s.snapshots.VacuumInto(ctx, snapshot); err != nil {
		_ = os.Remove(snapshot)
		return nil, fmt.Errorf("snapshot database: %w", err)
	}
	defer os.Remove(snapshot)

	tmp := dest + ".tmp"
	if err := s.writeArchive(tmp, now, snapshot); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return nil, fmt.Errorf("finalize backup: %w", err)
	}
	info, err := s.statBackup(dest)
	if err != nil {
		return nil, err
	}
	if _, err := s.prune(); err != nil {
		// 清理失败不影响新备份本身（下次 Create 重试清理）。
		log.Printf("backup prune: %v", err)
	}
	return info, nil
}

// List 现存归档列表（新→旧）。
func (s *BackupService) List(_ context.Context) ([]BackupInfo, error) {
	entries, err := os.ReadDir(s.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupInfo{}, nil
		}
		return nil, fmt.Errorf("read backup dir: %w", err)
	}
	out := make([]BackupInfo, 0, len(entries))
	for _, e := range entries {
		if !backupNamePattern.MatchString(e.Name()) {
			continue
		}
		info, err := s.statBackup(pathutil.Join(s.backupDir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, *info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out, nil
}

// Open 打开归档供下载。name 必须匹配备份命名白名单（防路径穿越，
// 双保险再过 pathutil.SafeJoin）。
func (s *BackupService) Open(_ context.Context, name string) (*os.File, int64, error) {
	if !backupNamePattern.MatchString(name) {
		return nil, 0, fmt.Errorf("%w: invalid backup name", ErrValidation)
	}
	p, err := pathutil.SafeJoin(s.backupDir, name)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: invalid backup name", ErrValidation)
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("%w: backup %q not found: %w", ErrValidation, name, gorm.ErrRecordNotFound)
		}
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

// RunScheduler 周期备份（ACADEMY_BACKUP_INTERVAL；失败仅告警不退出）。
func (s *BackupService) RunScheduler(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.Create(ctx); err != nil {
				log.Printf("scheduled backup: %v", err)
			}
		}
	}
}

// prune 保留最近 keep 份（按文件名降序），其余删除；返回删除数。
func (s *BackupService) prune() (int, error) {
	items, err := s.List(context.Background())
	if err != nil {
		return 0, err
	}
	if len(items) <= s.keep {
		return 0, nil
	}
	removed := 0
	for _, it := range items[s.keep:] {
		if err := os.Remove(pathutil.Join(s.backupDir, it.Name)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

func (s *BackupService) statBackup(p string) (*BackupInfo, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	return &BackupInfo{
		Name:       filepath.Base(p),
		SizeBytes:  st.Size(),
		CreatedAt:  st.ModTime().UTC(),
		Database:   "sqlite",
		StoreKinds: s.writeupsKind,
	}, nil
}

// writeArchive 打包：manifest.json + academy.db + attachments/ + avatars/ +
// writeups/（存在才入包）。dest 以 .tmp 落盘，由调用方原子改名。
func (s *BackupService) writeArchive(dest string, now time.Time, dbSnapshot string) error {
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	m := backupManifest{CreatedAt: now, Database: "sqlite", WriteupsKind: s.writeupsKind}
	if s.writeupsKind == "s3" {
		m.WriteupsNote = "writeup zips live in the S3-compatible object store and are NOT in this archive; restore them from bucket versioning/replication"
	}
	if err := writeTarJSON(tw, "manifest.json", m); err != nil {
		return err
	}
	if err := writeTarFile(tw, "academy.db", dbSnapshot); err != nil {
		return err
	}
	for prefix, dir := range map[string]string{
		"attachments": s.attachments, "avatars": s.avatars, "writeups": s.writeups,
	} {
		if err := writeTarDir(tw, prefix, dir); err != nil {
			return err
		}
	}
	return nil
}

func writeTarJSON(tw *tar.Writer, name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), ModTime: time.Now().UTC()}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar %s: %w", name, err)
	}
	if _, err := tw.Write(b); err != nil {
		return fmt.Errorf("tar %s: %w", name, err)
	}
	return nil
}

func writeTarFile(tw *tar.Writer, name, src string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: st.Size(), ModTime: st.ModTime()}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("tar %s: %w", name, err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("tar %s: %w", name, err)
	}
	return nil
}

// writeTarDir 递归打包目录为 <prefix>/<相对路径>。目录不存在或为空时
// 静默跳过（tar 非空目录无需单独目录头，成员路径前缀已表达层级）。
func writeTarDir(tw *tar.Writer, prefix, dir string) error {
	if dir == "" {
		return nil
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return nil
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		// 归档内路径是 / 分隔的虚拟路径（pathutil 管磁盘路径，此处标准库 path）。
		return writeTarFile(tw, path.Join(prefix, filepath.ToSlash(rel)), p)
	})
}

// ValidBackupName 暴露命名白名单给测试与 handler 共用。
func ValidBackupName(name string) bool { return backupNamePattern.MatchString(name) }
