package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// newContentTestService 进程内 SQLite + 真实 repo，构造被测 ContentService。
// 测试 zip 在测试代码中构造，不进生产路径（无内置内容原则）。
func newContentTestService(t *testing.T) (*ContentService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:contenttest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Migrate(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		dbg, _ := gdb.DB()
		dbg.Close() // 关闭共享内存库，下个测试重建
	})
	svc := NewContentService(
		repo.NewContentPackageRepo(gdb),
		repo.NewTopicRepo(gdb),
		repo.NewRoomRepo(gdb),
		NewChallengeService(repo.NewChallengeRepo(gdb), repo.NewSubmissionRepo(gdb)),
	)
	return svc, gdb
}

// buildZip 从 map（路径→内容）构造内存 zip。
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// validPackage 一个最小合法内容包（topic + 带 lab 的 room + 纯教程 room）。
func validPackage() map[string]string {
	return map[string]string{
		"manifest.yaml": `
name: test-package
version: 1.0.0
language: zh
topics:
  - web-security
`,
		"topics/web-security/topic.md": `---
title: Web 安全入门
---
## Topic 首页`,
		"topics/web-security/rooms/sqli-basics/lesson.md": `---
title: SQL 注入基础
difficulty: apprentice
---
SQL 注入是……`,
		"topics/web-security/rooms/sqli-basics/lab.yaml": `
title: 找出注入点
challenge:
  type: static
  flag: flag{test-answer}
score: 100
hints:
  - title: 提示一
    content: 看 UNION
    cost: 10
`,
		"topics/web-security/rooms/xss-intro/lesson.md": `---
title: XSS 简介
difficulty: apprentice
---
XSS 是……`,
	}
}

func TestUploadMissingManifest(t *testing.T) {
	svc, gdb := newContentTestService(t)
	files := validPackage()
	delete(files, "manifest.yaml")
	_, err := svc.Upload(context.Background(), buildZip(t, files))
	if err == nil {
		t.Fatal("expected reject when manifest missing")
	}
	var n int64
	gdb.Model(&model.ContentPackage{}).Count(&n)
	if n != 0 {
		t.Fatalf("package should not persist on failure, got %d", n)
	}
}

func TestUploadValidPackageDraft(t *testing.T) {
	svc, _ := newContentTestService(t)
	pkg, err := svc.Upload(context.Background(), buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if pkg.Name != "test-package" || pkg.LatestVersion != "1.0.0" {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if pkg.Status != model.PackageDraft {
		t.Fatalf("status = %s, want draft", pkg.Status)
	}
}

func TestUploadSameNameAppendsVersion(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	if _, err := svc.Upload(ctx, buildZip(t, validPackage())); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	// 第二版仅改 version。
	files := validPackage()
	files["manifest.yaml"] = `
name: test-package
version: 2.0.0
language: zh
topics:
  - web-security
`
	pkg, err := svc.Upload(ctx, buildZip(t, files))
	if err != nil {
		t.Fatalf("second upload: %v", err)
	}
	if pkg.LatestVersion != "2.0.0" {
		t.Fatalf("latest_version = %s, want 2.0.0", pkg.LatestVersion)
	}
	var pkgs, versions int64
	gdb.Model(&model.ContentPackage{}).Count(&pkgs)
	gdb.Model(&model.ContentVersion{}).Count(&versions)
	if pkgs != 1 || versions != 2 {
		t.Fatalf("packages=%d versions=%d, want 1/2", pkgs, versions)
	}
}

func TestUploadInvalidDifficulty(t *testing.T) {
	svc, _ := newContentTestService(t)
	files := validPackage()
	files["topics/web-security/rooms/sqli-basics/lesson.md"] = `---
title: SQL 注入基础
difficulty: beginner
---
正文`
	if _, err := svc.Upload(context.Background(), buildZip(t, files)); err == nil {
		t.Fatal("expected reject on invalid difficulty")
	}
}

func TestUploadSlugConflictWithDB(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	// 先让 DB 已有同名 topic。
	if err := gdb.Create(&model.Topic{Slug: "web-security", Title: "已有"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Upload(ctx, buildZip(t, validPackage())); err == nil {
		t.Fatal("expected reject on slug conflict with DB")
	}
}

func TestUploadInvalidRegexFlag(t *testing.T) {
	svc, _ := newContentTestService(t)
	files := validPackage()
	files["topics/web-security/rooms/sqli-basics/lab.yaml"] = `
title: 找出注入点
challenge:
  type: regex
  flag: "[unclosed"
score: 100
`
	if _, err := svc.Upload(context.Background(), buildZip(t, files)); err == nil {
		t.Fatal("expected reject on invalid regex flag")
	}
}

func TestPublishWritesContentTables(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	pkg, err := svc.Upload(ctx, buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := svc.Publish(ctx, pkg.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	var (
		topics, rooms, challenges, flags, hints int64
	)
	gdb.Model(&model.Topic{}).Count(&topics)
	gdb.Model(&model.Room{}).Count(&rooms)
	gdb.Model(&model.Challenge{}).Count(&challenges)
	gdb.Model(&model.Flag{}).Count(&flags)
	gdb.Model(&model.Hint{}).Count(&hints)
	if topics != 1 || rooms != 2 || challenges != 1 || flags != 1 || hints != 1 {
		t.Fatalf("topics=%d rooms=%d challenges=%d flags=%d hints=%d, want 1/2/1/1/1",
			topics, rooms, challenges, flags, hints)
	}
	// 教程正文与 flag 值正确写入。
	var room model.Room
	if err := gdb.Where("slug = ?", "sqli-basics").First(&room).Error; err != nil {
		t.Fatal(err)
	}
	if room.LessonMD == "" || room.Title != "SQL 注入基础" {
		t.Fatalf("room content wrong: %+v", room)
	}
	var flag model.Flag
	if err := gdb.First(&flag).Error; err != nil {
		t.Fatal(err)
	}
	if flag.Value != "flag{test-answer}" {
		t.Fatalf("flag = %q", flag.Value)
	}
}

func TestPublishRejectsNonDraft(t *testing.T) {
	svc, _ := newContentTestService(t)
	ctx := context.Background()
	pkg, err := svc.Upload(ctx, buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := svc.Publish(ctx, pkg.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := svc.Publish(ctx, pkg.ID); err == nil {
		t.Fatal("expected reject on published → published")
	}
}

// wrappedPackage 把合法包整体包进一层顶层目录（常见打包工具行为），
// 并混入 Mac 打包垃圾条目与反斜杠路径。
func wrappedPackage() map[string]string {
	wrapped := map[string]string{}
	for name, content := range validPackage() {
		wrapped["pkg-root/"+name] = content
	}
	wrapped["__MACOSX/pkg-root/manifest.yaml"] = "garbage"
	wrapped["pkg-root\\topics\\web-security\\extra.txt"] = "win-style path"
	return wrapped
}

func TestUploadStripsWrapperDirectory(t *testing.T) {
	svc, _ := newContentTestService(t)
	// 顶层包裹目录被剥掉 + __MACOSX 忽略 + 反斜杠归一 → 解析成功。
	pkg, err := svc.Upload(context.Background(), buildZip(t, wrappedPackage()))
	if err != nil {
		t.Fatalf("upload wrapped package: %v", err)
	}
	if pkg.Name != "test-package" || pkg.Status != model.PackageDraft {
		t.Fatalf("unexpected package: %+v", pkg)
	}
}

func TestUploadFlatStructureUntouched(t *testing.T) {
	svc, _ := newContentTestService(t)
	// 平铺结构（首段直接是 manifest/topics）：唯一首段是 "topics" 与 "manifest.yaml" 共两个 → 不剥。
	if _, err := svc.Upload(context.Background(), buildZip(t, validPackage())); err != nil {
		t.Fatalf("flat package should parse unchanged: %v", err)
	}
}

// versionTwoPackage 同名包第二版：改动教程正文与 flag（回滚前后可区分）。
func versionTwoPackage() map[string]string {
	files := validPackage()
	files["manifest.yaml"] = `
name: test-package
version: 2.0.0
language: zh
topics:
  - web-security
`
	files["topics/web-security/rooms/sqli-basics/lesson.md"] = `---
title: SQL 注入基础
difficulty: apprentice
---
v2 重写后的正文`
	files["topics/web-security/rooms/sqli-basics/lab.yaml"] = `
title: 找出注入点
challenge:
  type: static
  flag: flag{v2-answer}
score: 100
`
	return files
}

// publishTwoVersions 发布 v1 再上传并发布 v2，返回包与两版 ContentVersion。
func publishTwoVersions(t *testing.T, svc *ContentService) (*model.ContentPackage, []model.ContentVersion) {
	t.Helper()
	ctx := context.Background()
	pkg, err := svc.Upload(ctx, buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload v1: %v", err)
	}
	if _, err := svc.Publish(ctx, pkg.ID); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	pkg, err = svc.Upload(ctx, buildZip(t, versionTwoPackage()))
	if err != nil {
		t.Fatalf("re-upload v2 after publish: %v", err)
	}
	pkg, err = svc.Publish(ctx, pkg.ID)
	if err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	versions, err := svc.ListVersions(ctx, pkg.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	return pkg, versions
}

func TestPublishArchivesOldVersions(t *testing.T) {
	svc, _ := newContentTestService(t)
	pkg, versions := publishTwoVersions(t, svc)
	if len(versions) != 2 {
		t.Fatalf("versions = %d, want 2", len(versions))
	}
	// id DESC：第一条是 v2（published），第二条是 v1（archived）。
	if versions[0].Version != "2.0.0" || versions[0].Status != model.PackagePublished {
		t.Fatalf("v2 = %+v, want published 2.0.0", versions[0])
	}
	if versions[1].Version != "1.0.0" || versions[1].Status != model.PackageArchived {
		t.Fatalf("v1 = %+v, want archived 1.0.0", versions[1])
	}
	if pkg.LatestVersion != "2.0.0" || pkg.Status != model.PackagePublished {
		t.Fatalf("package = %+v", pkg)
	}
}

func TestRollbackRestoresOldContent(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	pkg, versions := publishTwoVersions(t, svc)
	// 回滚到 v1（archived）。
	pkg, err := svc.Rollback(ctx, pkg.ID, versions[1].ID)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if pkg.LatestVersion != "1.0.0" || pkg.Status != model.PackagePublished {
		t.Fatalf("package after rollback = %+v", pkg)
	}
	// 内容表被 v1 Payload 覆盖：正文与 flag 回到 v1。
	var room model.Room
	if err := gdb.Where("slug = ?", "sqli-basics").First(&room).Error; err != nil {
		t.Fatal(err)
	}
	if room.LessonMD != "SQL 注入是……" {
		t.Fatalf("lesson_md = %q, want v1 content", room.LessonMD)
	}
	var flag model.Flag
	if err := gdb.First(&flag).Error; err != nil {
		t.Fatal(err)
	}
	if flag.Value != "flag{test-answer}" {
		t.Fatalf("flag = %q, want v1 flag", flag.Value)
	}
	// 版本状态对调：v1 published，v2 archived。
	after, err := svc.ListVersions(ctx, pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Version != "2.0.0" || after[0].Status != model.PackageArchived {
		t.Fatalf("v2 after rollback = %+v, want archived", after[0])
	}
	if after[1].Version != "1.0.0" || after[1].Status != model.PackagePublished {
		t.Fatalf("v1 after rollback = %+v, want published", after[1])
	}
}

func TestRollbackRejectsCurrentVersion(t *testing.T) {
	svc, _ := newContentTestService(t)
	ctx := context.Background()
	pkg, versions := publishTwoVersions(t, svc)
	// versions[0] 是当前 published 版本。
	if _, err := svc.Rollback(ctx, pkg.ID, versions[0].ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("rollback to current should get ErrValidation, got %v", err)
	}
}

func TestArchiveRejectsDraft(t *testing.T) {
	svc, _ := newContentTestService(t)
	ctx := context.Background()
	pkg, err := svc.Upload(ctx, buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	// 状态机禁止 draft → archived。
	if _, err := svc.Archive(ctx, pkg.ID); !errors.Is(err, ErrValidation) {
		t.Fatalf("archive draft should get ErrValidation, got %v", err)
	}
}

func TestArchiveFreezesPackage(t *testing.T) {
	svc, _ := newContentTestService(t)
	ctx := context.Background()
	pkg, versions := publishTwoVersions(t, svc)
	pkg, err := svc.Archive(ctx, pkg.ID)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if pkg.Status != model.PackageArchived {
		t.Fatalf("status = %s, want archived", pkg.Status)
	}
	// 冻结后：发布与回滚均拒绝。
	if _, err := svc.Publish(ctx, pkg.ID); err == nil {
		t.Fatal("publish after archive should be rejected")
	}
	if _, err := svc.Rollback(ctx, pkg.ID, versions[1].ID); err == nil {
		t.Fatal("rollback after archive should be rejected")
	}
}