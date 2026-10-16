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

// fakeImageBuilder 记录 Build 调用；可注入 buildErr。
type fakeImageBuilder struct {
	tags []string
	dirs []string
	err  error
}

func (f *fakeImageBuilder) Build(ctx context.Context, tag, contextDir string) error {
	f.tags = append(f.tags, tag)
	f.dirs = append(f.dirs, contextDir)
	return f.err
}

// newContentTestService 进程内 SQLite + 真实 repo，构造被测 ContentService。
// 测试 zip 在测试代码中构造，不进生产路径（无内置内容原则）。
func newContentTestService(t *testing.T) (*ContentService, *gorm.DB) {
	return newContentTestServiceWithBuilder(t, &fakeImageBuilder{})
}

func newContentTestServiceWithBuilder(t *testing.T, builder ImageBuilder) (*ContentService, *gorm.DB) {
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
		NewChallengeService(repo.NewChallengeRepo(gdb), repo.NewSubmissionRepo(gdb), repo.NewEventRepo(gdb), repo.NewScoreRepo(gdb), nil),
		builder,
		repo.NewProgressRepo(gdb),
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

// 教程题目 tab 专属内置题集，上传路径的题库归属规则：
// 新建缺省 → ctf；重传缺省 → 保留原值（不漂移不重置）；显式仅接受 ctf/acm，
// tutorial 一律 422（内置导入在 builtin.go 注入，绕过上传校验）。
func TestUploadChallengesBankRules(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()

	bankOf := func(slug string) model.ChallengeBank {
		t.Helper()
		var chal model.Challenge
		if err := gdb.Where("slug = ?", slug).First(&chal).Error; err != nil {
			t.Fatal(err)
		}
		return chal.Bank
	}
	upload := func(bank string) error {
		payload := `{"slug":"bank-1","title":"题库题","category":"web","difficulty":"apprentice",
			"score":100,"challenge":{"type":"static","flag":"flag{a}"}}`
		if bank != "" {
			payload = `{"slug":"bank-1","title":"题库题","category":"web","difficulty":"apprentice",
			"score":100,"challenge":{"type":"static","flag":"flag{a}"},"bank":"` + bank + `"}`
		}
		_, _, err := svc.UploadChallenges(ctx, []byte(payload))
		return err
	}

	// 新建缺省 → ctf。
	if err := upload(""); err != nil {
		t.Fatalf("create default: %v", err)
	}
	if got := bankOf("bank-1"); got != model.BankCTF {
		t.Fatalf("create default bank = %q, want ctf", got)
	}
	// 重传缺省 → 保留 ctf。
	if err := upload(""); err != nil {
		t.Fatalf("re-upload default: %v", err)
	}
	if got := bankOf("bank-1"); got != model.BankCTF {
		t.Fatalf("re-upload default bank = %q, want ctf preserved", got)
	}
	// 显式 acm → 切换；再缺省重传 → 保留 acm（保留原值而非重置 ctf）。
	if err := upload("acm"); err != nil {
		t.Fatalf("switch acm: %v", err)
	}
	if got := bankOf("bank-1"); got != model.BankACM {
		t.Fatalf("explicit acm bank = %q, want acm", got)
	}
	if err := upload(""); err != nil {
		t.Fatalf("re-upload after acm: %v", err)
	}
	if got := bankOf("bank-1"); got != model.BankACM {
		t.Fatalf("re-upload after acm bank = %q, want acm preserved", got)
	}
	// 显式 tutorial → 拒绝且不入库。
	var before int64
	gdb.Model(&model.Challenge{}).Count(&before)
	if err := upload("tutorial"); !errors.Is(err, ErrValidation) {
		t.Fatalf("tutorial bank err = %v, want ErrValidation", err)
	}
	if got := bankOf("bank-1"); got != model.BankACM {
		t.Fatalf("rejected upload must not touch existing rows, bank = %q", got)
	}
	var after int64
	gdb.Model(&model.Challenge{}).Count(&after)
	if after != before {
		t.Fatalf("tutorial upload must not persist, before=%d after=%d", before, after)
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

// vulinboxChallengePackageFiles 内存构造题目包（对齐 examples/challenges/vulinbox-xss-reflected，
// 测试不读仓库文件，避免无内置内容原则与样例耦合）。
func vulinboxChallengePackageFiles() map[string]string {
	return map[string]string{
		"vulinbox-xss-reflected/challenge.yml": `slug: vulinbox-xss-reflected
name: Vulinbox 反射型 XSS
category: xss
difficulty: apprentice
description: |
  参考 Yaklang Vulinbox（默认 :8787）的 xss/echo 场景。
  向 name 参数注入脚本，拿到页面中的 flag。
value: 100
type: static
flag_mode: instance
flags:
  - "flag{lab-{token}}"
image: .
port: 8787
protocol: http
hints:
  - title: 参数入口
    content: 关注 /xss/echo?name=
    cost: 10
  - title: 载荷
    content: 试试 <script>alert(1)</script>
    cost: 20
`,
		"vulinbox-xss-reflected/Dockerfile": `FROM python:3.12-alpine
WORKDIR /app
COPY src/ /app/
EXPOSE 8787
CMD ["python", "server.py"]
`,
		"vulinbox-xss-reflected/docker-compose.yml": `services:
  challenge:
    build: .
    ports:
      - "8787:8787"
`,
		"vulinbox-xss-reflected/src/server.py": `import os
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

FLAG = os.environ.get("FLAG", "flag{unset}")

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        q = parse_qs(urlparse(self.path).query)
        name = q.get("name", [""])[0]
        body = f"<html><body><p>Hello {name}</p><!-- {FLAG} --></body></html>".encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *args):
        pass

HTTPServer(("0.0.0.0", 8787), H).serve_forever()
`,
	}
}

func TestUploadChallengePackageVulinboxSample(t *testing.T) {
	builder := &fakeImageBuilder{}
	svc, gdb := newContentTestServiceWithBuilder(t, builder)
	ctx := context.Background()
	imported, slugs, err := svc.UploadChallengePackage(ctx, buildZip(t, vulinboxChallengePackageFiles()))
	if err != nil {
		t.Fatalf("upload package: %v", err)
	}
	if imported != 1 || len(slugs) != 1 || slugs[0] != "vulinbox-xss-reflected" {
		t.Fatalf("imported=%d slugs=%v", imported, slugs)
	}
	if len(builder.tags) != 1 || builder.tags[0] != "yak-academy/vulinbox-xss-reflected:latest" {
		t.Fatalf("builder tags = %v", builder.tags)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "vulinbox-xss-reflected").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if chal.Image != "yak-academy/vulinbox-xss-reflected:latest" || chal.Port != 8787 {
		t.Fatalf("env = %s:%d", chal.Image, chal.Port)
	}
	if chal.FlagMode != model.FlagModeInstance {
		t.Fatalf("flag_mode = %s, want instance", chal.FlagMode)
	}
	if chal.Category != "xss" || chal.Difficulty != model.DifficultyApprentice || chal.Score != 100 {
		t.Fatalf("meta = %+v", chal)
	}
	var hints int64
	gdb.Model(&model.Hint{}).Where("challenge_id = ?", chal.ID).Count(&hints)
	if hints != 2 {
		t.Fatalf("hints=%d, want 2", hints)
	}
	var flag model.Flag
	if err := gdb.Where("challenge_id = ?", chal.ID).First(&flag).Error; err != nil {
		t.Fatal(err)
	}
	if flag.Value != "flag{lab-{token}}" {
		t.Fatalf("flag template = %q", flag.Value)
	}
}

func TestUploadChallengePackageBuildRequired(t *testing.T) {
	svc, _ := newContentTestServiceWithBuilder(t, NoopDriver{})
	_, _, err := svc.UploadChallengePackage(context.Background(), buildZip(t, vulinboxChallengePackageFiles()))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("noop builder should get ErrValidation, got %v", err)
	}
}

func TestUploadChallengePackageRequiresYMLAndContainerFiles(t *testing.T) {
	svc, _ := newContentTestService(t)
	ctx := context.Background()
	// 缺 challenge.yml
	if _, _, err := svc.UploadChallengePackage(ctx, buildZip(t, map[string]string{
		"Dockerfile": "FROM scratch\n",
	})); err == nil {
		t.Fatal("expected reject without challenge.yml")
	}
	// 有 image 但缺 Dockerfile
	if _, _, err := svc.UploadChallengePackage(ctx, buildZip(t, map[string]string{
		"challenge.yml": "slug: a\nname: A\ncategory: web\ndifficulty: apprentice\nvalue: 10\ntype: static\nflags: [flag{a}]\nimage: nginx:alpine\nport: 80\n",
		"docker-compose.yml": "services: {a: {image: nginx:alpine}}\n",
	})); err == nil {
		t.Fatal("expected reject without Dockerfile")
	}
	// 有 image 但缺 compose
	if _, _, err := svc.UploadChallengePackage(ctx, buildZip(t, map[string]string{
		"challenge.yml": "slug: b\nname: B\ncategory: web\ndifficulty: apprentice\nvalue: 10\ntype: static\nflags: [flag{b}]\nimage: nginx:alpine\nport: 80\n",
		"Dockerfile":     "FROM nginx:alpine\n",
	})); err == nil {
		t.Fatal("expected reject without docker-compose.yml")
	}
	// 无容器：仅 challenge.yml 即可
	imported, slugs, err := svc.UploadChallengePackage(ctx, buildZip(t, map[string]string{
		"challenge.yml": "slug: static-only\nname: 静态题\ncategory: misc\ndifficulty: apprentice\nvalue: 50\ntype: static\nflags:\n  - flag{static}\n",
	}))
	if err != nil {
		t.Fatalf("static-only package: %v", err)
	}
	if imported != 1 || slugs[0] != "static-only" {
		t.Fatalf("imported=%d slugs=%v", imported, slugs)
	}
}

// ---------- 内容包删除（DELETE /content/packages/{id}） ----------

// otherPackage 另一作者的包（不同 name/topic/room slug），删除隔离性用。
func otherPackage() map[string]string {
	return map[string]string{
		"manifest.yaml": `
name: other-package
version: 1.0.0
language: zh
topics:
  - mobile-security
`,
		"topics/mobile-security/topic.md": `---
title: 移动安全
---
## Topic 首页`,
		"topics/mobile-security/rooms/api-basics/lesson.md": `---
title: API 基础
difficulty: practitioner
---
API 是……`,
	}
}

// versionWithoutXssPackage v2 删掉 xss-intro 教程：applyPayload 只 upsert 不删行，
// xss-intro 残留在内容表，删除时须靠历代版本 union 清扫。
func versionWithoutXssPackage() map[string]string {
	files := validPackage()
	files["manifest.yaml"] = `
name: test-package
version: 2.0.0
language: zh
topics:
  - web-security
`
	delete(files, "topics/web-security/rooms/xss-intro/lesson.md")
	return files
}

func TestDeletePackageRemovesContentAndReferences(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	pkg, err := svc.Upload(ctx, buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := svc.Publish(ctx, pkg.ID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	// 关联题目（tutorial_slug 软引用）与无关题目各一题。
	if _, _, err := svc.UploadChallenges(ctx, []byte(`[{
		"slug":"linked-1","title":"关联题","category":"web","difficulty":"apprentice",
		"score":100,"challenge":{"type":"static","flag":"flag{a}"},
		"tutorial_slug":"sqli-basics"
	},{
		"slug":"plain-1","title":"无关题","category":"misc","difficulty":"apprentice",
		"score":100,"challenge":{"type":"static","flag":"flag{b}"}
	}]`)); err != nil {
		t.Fatalf("upload challenges: %v", err)
	}
	// 完成进度走真实事件路径产生（不在测试里直改派生表）。
	events := repo.NewEventRepo(gdb)
	deriver := NewDeriver(repo.NewScoreRepo(gdb), repo.NewProgressRepo(gdb))
	ev := model.Event{UserID: 7, Type: model.EventRoomCompleted, Payload: `{"room_slug":"sqli-basics"}`}
	if err := events.Append(ctx, &ev); err != nil {
		t.Fatal(err)
	}
	deriver.Handle(ctx, ev)
	var progressRows int64
	gdb.Model(&model.Progress{}).Where("user_id = ?", 7).Count(&progressRows)
	if progressRows != 1 {
		t.Fatalf("progress rows before delete = %d, want 1", progressRows)
	}

	res, err := svc.DeletePackage(ctx, pkg.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !res.Deleted || res.Topics != 1 || res.Rooms != 2 {
		t.Fatalf("res = %+v, want deleted/1 topic/2 rooms", res)
	}
	for _, tc := range []struct {
		model any
		want  int64
		name  string
	}{
		{&model.ContentPackage{}, 0, "packages"},
		{&model.ContentVersion{}, 0, "versions"},
		{&model.Topic{}, 0, "topics"},
		{&model.Room{}, 0, "rooms"},
		{&model.Progress{}, 0, "progress"},
		{&model.Event{}, 1, "events"},
		{&model.Challenge{}, 2, "challenges"},
	} {
		var n int64
		gdb.Model(tc.model).Count(&n)
		if n != tc.want {
			t.Fatalf("%s = %d, want %d", tc.name, n, tc.want)
		}
	}
	// 事件 append-only 保留、题目本体保留但软引用清空。
	var linked model.Challenge
	if err := gdb.Where("slug = ?", "linked-1").First(&linked).Error; err != nil {
		t.Fatal(err)
	}
	if linked.TutorialSlug != "" {
		t.Fatalf("tutorial_slug = %q, want cleared", linked.TutorialSlug)
	}
	// 二次删除：包已不存在 → 记录缺失。
	if _, err := svc.DeletePackage(ctx, pkg.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("second delete err = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestDeleteDraftPackageOnlyRemovesRecord(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	// draft 包从未发布：内容表无行，删除只清包与版本记录。
	pkg, err := svc.Upload(ctx, buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	res, err := svc.DeletePackage(ctx, pkg.ID)
	if err != nil {
		t.Fatalf("delete draft: %v", err)
	}
	if !res.Deleted || res.Topics != 0 || res.Rooms != 0 {
		t.Fatalf("res = %+v, want deleted/0/0", res)
	}
	for _, tc := range []struct {
		model any
		want  int64
		name  string
	}{
		{&model.ContentPackage{}, 0, "packages"},
		{&model.ContentVersion{}, 0, "versions"},
		{&model.Topic{}, 0, "topics"},
		{&model.Room{}, 0, "rooms"},
	} {
		var n int64
		gdb.Model(tc.model).Count(&n)
		if n != tc.want {
			t.Fatalf("%s = %d, want %d", tc.name, n, tc.want)
		}
	}
}

func TestDeletePackageSweepsRoomsFromOldVersions(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	pkg, err := svc.Upload(ctx, buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload v1: %v", err)
	}
	if _, err := svc.Publish(ctx, pkg.ID); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	pkg, err = svc.Upload(ctx, buildZip(t, versionWithoutXssPackage()))
	if err != nil {
		t.Fatalf("upload v2: %v", err)
	}
	if _, err := svc.Publish(ctx, pkg.ID); err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	// v2 已不含 xss-intro，但 applyPayload 不删行 → 内容表仍是 2 个 Room。
	var rooms int64
	gdb.Model(&model.Room{}).Count(&rooms)
	if rooms != 2 {
		t.Fatalf("rooms after v2 publish = %d, want 2", rooms)
	}
	res, err := svc.DeletePackage(ctx, pkg.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if res.Rooms != 2 || res.Topics != 1 {
		t.Fatalf("res = %+v, want 1 topic/2 rooms（union 清扫）", res)
	}
	gdb.Model(&model.Room{}).Count(&rooms)
	if rooms != 0 {
		t.Fatalf("rooms after delete = %d, want 0（v1 独有的 xss-intro 一并移除）", rooms)
	}
}

func TestDeletePackageKeepsOtherPackageContent(t *testing.T) {
	svc, gdb := newContentTestService(t)
	ctx := context.Background()
	pkgA, err := svc.Upload(ctx, buildZip(t, validPackage()))
	if err != nil {
		t.Fatalf("upload A: %v", err)
	}
	if _, err := svc.Publish(ctx, pkgA.ID); err != nil {
		t.Fatalf("publish A: %v", err)
	}
	pkgB, err := svc.Upload(ctx, buildZip(t, otherPackage()))
	if err != nil {
		t.Fatalf("upload B: %v", err)
	}
	if _, err := svc.Publish(ctx, pkgB.ID); err != nil {
		t.Fatalf("publish B: %v", err)
	}
	// 题目软引用 B 包的教程，删除 A 不得清空。
	if _, _, err := svc.UploadChallenges(ctx, []byte(`{
		"slug":"other-linked","title":"B包关联题","category":"mobile","difficulty":"practitioner",
		"score":100,"challenge":{"type":"static","flag":"flag{c}"},
		"tutorial_slug":"api-basics"
	}`)); err != nil {
		t.Fatalf("upload challenges: %v", err)
	}
	if _, err := svc.DeletePackage(ctx, pkgA.ID); err != nil {
		t.Fatalf("delete A: %v", err)
	}
	var (
		packages, topics, rooms int64
	)
	gdb.Model(&model.ContentPackage{}).Count(&packages)
	gdb.Model(&model.Topic{}).Count(&topics)
	gdb.Model(&model.Room{}).Count(&rooms)
	if packages != 1 || topics != 1 || rooms != 1 {
		t.Fatalf("packages=%d topics=%d rooms=%d, want 1/1/1（B 包完整保留）", packages, topics, rooms)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "other-linked").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if chal.TutorialSlug != "api-basics" {
		t.Fatalf("tutorial_slug = %q, want api-basics（他包引用不动）", chal.TutorialSlug)
	}
	if _, err := svc.DeletePackage(ctx, pkgB.ID); err != nil {
		t.Fatalf("delete B after A: %v", err)
	}
}