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

// validPackage 一个最小合法教程包（topic + 两个 room：文字教程 + 视频教程）。
// 题目已独立（challenge.json 上传），包内不再有 lab.yaml。
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
		"topics/web-security/rooms/xss-intro/lesson.md": `---
title: XSS 简介
difficulty: apprentice
video: https://cdn.example.com/xss-intro.mp4
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
	// 题目 JSON：regex flag 无法编译 → 整体拒绝。
	raw := `{"slug":"re-1","title":"正则题","category":"web","difficulty":"apprentice",
		"score":100,"challenge":{"type":"regex","flag":"[unclosed"}}`
	if _, _, err := svc.UploadChallenges(context.Background(), []byte(raw)); err == nil {
		t.Fatal("expected reject on invalid regex flag")
	}
}

func TestUploadChallengesUpsertAndBatch(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	// 数组批量导入两题（一题带 env/hints/tutorial 关联）。
	batch := `[{
		"slug":"sqli-1","title":"注入题","category":"sql-injection","difficulty":"apprentice",
		"description_md":"# 背景\n目标","score":100,
		"challenge":{"type":"static","flag":"flag{a}"},
		"env":{"image":"nginx:alpine","port":80},
		"hints":[{"title":"提示","content":"看 WHERE","cost":10}],
		"tutorial_slug":"sqli-basics"
	},{
		"slug":"xss-1","title":"XSS题","category":"cross-site-scripting","difficulty":"expert",
		"score":200,"challenge":{"type":"static","flag":"flag{b}"}
	}]`
	imported, slugs, err := svc.UploadChallenges(ctx, []byte(batch))
	if err != nil {
		t.Fatalf("upload challenges: %v", err)
	}
	if imported != 2 || len(slugs) != 2 {
		t.Fatalf("imported=%d slugs=%v", imported, slugs)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "sqli-1").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if chal.Category != "sql-injection" || chal.Image != "nginx:alpine" ||
		chal.TutorialSlug != "sqli-basics" || chal.Description == "" {
		t.Fatalf("challenge fields wrong: %+v", chal)
	}
	var hints int64
	gdb.Model(&model.Hint{}).Where("challenge_id = ?", chal.ID).Count(&hints)
	if hints != 1 {
		t.Fatalf("hints = %d, want 1", hints)
	}
	// 单题失败整体拒绝：数组中第二题 difficulty 非法 → 两题都不入库。
	var before int64
	gdb.Model(&model.Challenge{}).Count(&before)
	badBatch := `[{"slug":"ok-1","title":"ok","category":"c","difficulty":"apprentice",
		"score":1,"challenge":{"type":"static","flag":"f"}},
		{"slug":"bad-1","title":"bad","category":"c","difficulty":"beginner",
		"score":1,"challenge":{"type":"static","flag":"f"}}]`
	if _, _, err := svc.UploadChallenges(ctx, []byte(badBatch)); err == nil {
		t.Fatal("expected reject on invalid difficulty in batch")
	}
	var after int64
	gdb.Model(&model.Challenge{}).Count(&after)
	if after != before {
		t.Fatalf("batch reject must not persist, before=%d after=%d", before, after)
	}
	// 重传同 slug = upsert 更新，不新增行。
	update := `{"slug":"sqli-1","title":"注入题v2","category":"sql-injection","difficulty":"apprentice",
		"score":150,"challenge":{"type":"static","flag":"flag{a}"}}`
	if _, _, err := svc.UploadChallenges(ctx, []byte(update)); err != nil {
		t.Fatalf("re-upload: %v", err)
	}
	gdb.Model(&model.Challenge{}).Count(&after)
	if after != before {
		t.Fatalf("upsert should not add rows, before=%d after=%d", before, after)
	}
	var title string
	gdb.Model(&model.Challenge{}).Where("slug = ?", "sqli-1").Pluck("title", &title)
	if title != "注入题v2" {
		t.Fatalf("upsert title = %q", title)
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
	// 教程包只写 Topic/Room（题目经 challenge.json 独立上传）。
	var (
		topics, rooms, challenges int64
	)
	gdb.Model(&model.Topic{}).Count(&topics)
	gdb.Model(&model.Room{}).Count(&rooms)
	gdb.Model(&model.Challenge{}).Count(&challenges)
	if topics != 1 || rooms != 2 || challenges != 0 {
		t.Fatalf("topics=%d rooms=%d challenges=%d, want 1/2/0", topics, rooms, challenges)
	}
	// 教程正文与视频字段正确写入。
	var room model.Room
	if err := gdb.Where("slug = ?", "sqli-basics").First(&room).Error; err != nil {
		t.Fatal(err)
	}
	if room.LessonMD == "" || room.Title != "SQL 注入基础" {
		t.Fatalf("room content wrong: %+v", room)
	}
	var xssRoom model.Room
	if err := gdb.Where("slug = ?", "xss-intro").First(&xssRoom).Error; err != nil {
		t.Fatal(err)
	}
	if xssRoom.VideoURL != "https://cdn.example.com/xss-intro.mp4" {
		t.Fatalf("video_url = %q", xssRoom.VideoURL)
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

// versionTwoPackage 同名包第二版：改动教程正文（回滚前后可区分）。
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
	// 内容表被 v1 Payload 覆盖：正文回到 v1。
	var room model.Room
	if err := gdb.Where("slug = ?", "sqli-basics").First(&room).Error; err != nil {
		t.Fatal(err)
	}
	if room.LessonMD != "SQL 注入是……" {
		t.Fatalf("lesson_md = %q, want v1 content", room.LessonMD)
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