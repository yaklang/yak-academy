package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// builtinBuildRecorder 记录 Build 的 tag 与构建上下文文件清单（Build 时目录尚存在）。
type builtinBuildRecorder struct {
	tag      string
	files    []string
	failWith error
	calls    int
}

func (b *builtinBuildRecorder) Build(ctx context.Context, tag, contextDir string) error {
	b.calls++
	b.tag = tag
	_ = filepath.WalkDir(contextDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b.files = append(b.files, filepath.Base(p))
		}
		return nil
	})
	return b.failWith
}

func newBuiltinTestService(t *testing.T, builder ImageBuilder) (*ContentService, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:builtintest?mode=memory&cache=shared"), &gorm.Config{})
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
	)
	return svc, gdb
}

func TestImportBuiltinChallengesFirstImport(t *testing.T) {
	rec := &builtinBuildRecorder{}
	svc, gdb := newBuiltinTestService(t, rec)

	res, err := svc.ImportBuiltinChallenges(context.Background())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !res.ImageBuilt {
		t.Fatal("image_built = false, want true")
	}
	if res.Created != len(res.Slugs) || res.Updated != 0 {
		t.Fatalf("created/updated = %d/%d, slugs = %d", res.Created, res.Updated, len(res.Slugs))
	}
	if res.Version == "" {
		t.Fatal("version is empty")
	}
	if !slices.Contains(res.Slugs, "vulinbox-ssrf-basic") {
		t.Fatalf("slugs missing vulinbox-ssrf-basic: %v", res.Slugs)
	}
	// 镜像构建：tag 来自 manifest，上下文含 Dockerfile 与 NOTICE。
	if rec.calls != 1 {
		t.Fatalf("build calls = %d, want 1", rec.calls)
	}
	if rec.tag != "yak-academy/vulinbox:latest" {
		t.Fatalf("build tag = %q", rec.tag)
	}
	for _, want := range []string{"Dockerfile", "NOTICE"} {
		if !slices.Contains(rec.files, want) {
			t.Fatalf("build context missing %s: %v", want, rec.files)
		}
	}
	// 落库断言：题目数 = slugs 数；抽查一题的镜像与端口字段。
	var count int64
	gdb.Model(&model.Challenge{}).Count(&count)
	if int(count) != len(res.Slugs) {
		t.Fatalf("challenge rows = %d, want %d", count, len(res.Slugs))
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "vulinbox-ssrf-basic").First(&chal).Error; err != nil {
		t.Fatalf("find challenge: %v", err)
	}
	if chal.Image != "yak-academy/vulinbox:latest" || chal.Port != 8787 {
		t.Fatalf("image/port = %q/%d", chal.Image, chal.Port)
	}
	// 内置题集归教程题目 tab 且带来源标记（题库页过滤依据）。
	if chal.Bank != model.BankTutorial || !chal.IsBuiltin {
		t.Fatalf("bank/is_builtin = %q/%v, want tutorial/true", chal.Bank, chal.IsBuiltin)
	}
}

func TestImportBuiltinChallengesReimportUpdates(t *testing.T) {
	svc, gdb := newBuiltinTestService(t, &builtinBuildRecorder{})

	first, err := svc.ImportBuiltinChallenges(context.Background())
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	second, err := svc.ImportBuiltinChallenges(context.Background())
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if second.Created != 0 || second.Updated != first.Created {
		t.Fatalf("second import created/updated = %d/%d, want 0/%d", second.Created, second.Updated, first.Created)
	}
	if len(second.Slugs) != len(first.Slugs) {
		t.Fatalf("second import slugs = %d, want %d", len(second.Slugs), len(first.Slugs))
	}
	var count int64
	gdb.Model(&model.Challenge{}).Count(&count)
	if int(count) != len(first.Slugs) {
		t.Fatalf("challenge rows = %d, want %d (upsert 不应产生重复)", count, len(first.Slugs))
	}
}

func TestImportBuiltinChallengesBuildFailureRollsBack(t *testing.T) {
	svc, gdb := newBuiltinTestService(t, &builtinBuildRecorder{failWith: os.ErrPermission})

	_, err := svc.ImportBuiltinChallenges(context.Background())
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	var count int64
	gdb.Model(&model.Challenge{}).Count(&count)
	if count != 0 {
		t.Fatalf("challenge rows = %d, want 0 (构建失败不入库)", count)
	}
}

func TestImportBuiltinChallengesNoopSkipsImage(t *testing.T) {
	svc, _ := newBuiltinTestService(t, NoopDriver{})

	res, err := svc.ImportBuiltinChallenges(context.Background())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.ImageBuilt {
		t.Fatal("image_built = true under noop driver, want false")
	}
	if res.Created == 0 {
		t.Fatal("noop driver should still import challenge definitions")
	}
}

// 普通上传路径不改写来源标记：管理端编辑/重传内置题后 is_builtin 保持 true。
func TestUploadDoesNotClearBuiltinMark(t *testing.T) {
	svc, gdb := newBuiltinTestService(t, &builtinBuildRecorder{})
	if _, err := svc.ImportBuiltinChallenges(context.Background()); err != nil {
		t.Fatalf("import: %v", err)
	}
	edited := fmt.Sprintf(`[{"slug":"vulinbox-xss-escape","title":"改过的标题","category":"xss",`+
		`"difficulty":"apprentice","score":120,"challenge":{"type":"static","flag":"&lt;script&gt;"}}]`)
	if _, _, err := svc.UploadChallenges(context.Background(), []byte(edited)); err != nil {
		t.Fatalf("re-upload: %v", err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "vulinbox-xss-escape").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if !chal.IsBuiltin {
		t.Fatal("is_builtin cleared by normal upload, want preserved")
	}
	if chal.Title != "改过的标题" || chal.Score != 120 {
		t.Fatalf("re-upload should take effect: %+v", chal)
	}
}
