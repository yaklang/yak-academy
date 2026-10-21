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
		repo.NewProgressRepo(gdb),
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
	// 全题集 instance 模式（镜像内 flaggateway 层消费平台注入的 FLAG）+ 模板 flag。
	if chal.FlagMode != model.FlagModeInstance {
		t.Fatalf("flag_mode = %q, want instance", chal.FlagMode)
	}
	var flagRow model.Flag
	if err := gdb.Where("challenge_id = ?", chal.ID).First(&flagRow).Error; err != nil {
		t.Fatal(err)
	}
	if flagRow.Value != "flag{lab-{token}}" {
		t.Fatalf("flag template = %q, want flag{lab-{token}}", flagRow.Value)
	}
	// 内置题集归教程题目 tab 且带来源标记（题库页过滤依据）。
	if chal.Bank != model.BankTutorial || !chal.IsBuiltin {
		t.Fatalf("bank/is_builtin = %q/%v, want tutorial/true", chal.Bank, chal.IsBuiltin)
	}
	// 配套教程同批导入：5 Topic / 12 Room，与题目 1:1。
	if res.Topics != 5 || res.Rooms != 12 {
		t.Fatalf("topics/rooms = %d/%d, want 5/12", res.Topics, res.Rooms)
	}
	var room model.Room
	if err := gdb.Where("slug = ?", "yakit-jwt-forge").First(&room).Error; err != nil {
		t.Fatalf("find builtin room: %v", err)
	}
	if room.TopicSlug != "yakit-session-authz" || room.LessonMD == "" ||
		room.Difficulty != model.DifficultyPractitioner || room.Title == "" {
		t.Fatalf("builtin room fields wrong: %+v", room)
	}
	// 1:1 关联：每道内置题的 tutorial_slug 都能反查到配套 Room。
	for _, slug := range res.Slugs {
		var chal model.Challenge
		if err := gdb.Where("slug = ?", slug).First(&chal).Error; err != nil {
			t.Fatal(err)
		}
		if chal.TutorialSlug == "" {
			t.Fatalf("builtin challenge %s: tutorial_slug is empty", slug)
		}
		var n int64
		gdb.Model(&model.Room{}).Where("slug = ?", chal.TutorialSlug).Count(&n)
		if n != 1 {
			t.Fatalf("builtin challenge %s: tutorial room %q not found", slug, chal.TutorialSlug)
		}
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
	// 教程同样幂等：重导入不新增 Topic/Room 行。
	var topics, rooms int64
	gdb.Model(&model.Topic{}).Count(&topics)
	gdb.Model(&model.Room{}).Count(&rooms)
	if topics != 5 || rooms != 12 {
		t.Fatalf("topics/rooms after reimport = %d/%d, want 5/12 (upsert 不应产生重复)", topics, rooms)
	}
	if second.Removed != 0 {
		t.Fatalf("second import removed = %d, want 0 (目录一致时无清理)", second.Removed)
	}
}

// TestImportBuiltinPrunesRemovedEntries 重导入清理目录外残留：历史版本导入的
// 内置题/Room 已不在当前目录时，重导入按删除级联清理（模块专精 3 题缩成 1 道
// 综合题后，旧库重导入不应残留旧题）。学生做过的旧内置题同样清理——目录是
// 内置内容的唯一事实源；提交事件 append-only 保留（event-sourcing）。
func TestImportBuiltinPrunesRemovedEntries(t *testing.T) {
	ctx := context.Background()
	svc, gdb := newBuiltinTestService(t, &builtinBuildRecorder{})

	if _, err := svc.ImportBuiltinChallenges(ctx); err != nil {
		t.Fatalf("first import: %v", err)
	}

	// 伪造旧版本（v1.6.0）残留：目录外的内置题 + 配套内置 Room，题上带通关记录。
	stale := model.Challenge{
		Slug: "vulinbox-fuzzer-headers", Title: "旧模块题", Category: "fuzz",
		Bank: model.BankTutorial, Difficulty: model.DifficultyPractitioner,
		Type: model.ChallengeStatic, Score: 150, FlagMode: model.FlagModeInstance,
		TutorialSlug: "yakit-fuzzer-replay", IsBuiltin: true,
	}
	if err := gdb.Create(&stale).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Flag{ChallengeID: stale.ID, Value: "flag{lab-{token}}"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Room{
		Slug: "yakit-fuzzer-replay", TopicSlug: "yakit-module-mastery", Title: "旧模块教程",
		Difficulty: model.DifficultyPractitioner, LessonMD: "正文", IsBuiltin: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.NewSubmissionRepo(gdb).Create(ctx, &model.Submission{
		UserID: 1, ChallengeID: stale.ID, Answer: "flag{lab-x}", Status: model.SubmissionCorrect, Score: 150,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ImportBuiltinChallenges(ctx)
	if err != nil {
		t.Fatalf("reimport: %v", err)
	}
	if res.Removed != 1 {
		t.Fatalf("removed = %d, want 1", res.Removed)
	}

	// 残留行全部消失；目录内题目不受影响。
	var errNF = gorm.ErrRecordNotFound
	if err := gdb.Where("slug = ?", "vulinbox-fuzzer-headers").First(&model.Challenge{}).Error; !errors.Is(err, errNF) {
		t.Fatalf("stale challenge still present: %v", err)
	}
	if err := gdb.Where("slug = ?", "yakit-fuzzer-replay").First(&model.Room{}).Error; !errors.Is(err, errNF) {
		t.Fatalf("stale room still present: %v", err)
	}
	var flagN int64
	gdb.Model(&model.Flag{}).Where("challenge_id = ?", stale.ID).Count(&flagN)
	if flagN != 0 {
		t.Fatalf("stale flags = %d, want 0 (级联清理)", flagN)
	}
	var kept model.Challenge
	if err := gdb.Where("slug = ?", "vulinbox-mall-chain").First(&kept).Error; err != nil {
		t.Fatalf("catalog challenge missing after prune: %v", err)
	}
	var total int64
	gdb.Model(&model.Challenge{}).Count(&total)
	if total != 12 {
		t.Fatalf("challenge rows = %d, want 12 (残留清理后只剩目录题)", total)
	}

	// 通关记录 append-only 保留（重放派生只按事件流，不随目录演进丢失）。
	var subN int64
	gdb.Model(&model.Submission{}).Where("challenge_id = ?", stale.ID).Count(&subN)
	if subN != 1 {
		t.Fatalf("submission rows = %d, want 1 (append-only 保留)", subN)
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

// 内置题集防写护栏：普通上传重传内置题一律 422（目录是内置内容唯一事实源，
// 单题编辑出路会破坏「一键导入 = 整体更新」语义）。拒绝后库内行原样保留。
func TestUploadDoesNotClearBuiltinMark(t *testing.T) {
	svc, gdb := newBuiltinTestService(t, &builtinBuildRecorder{})
	if _, err := svc.ImportBuiltinChallenges(context.Background()); err != nil {
		t.Fatalf("import: %v", err)
	}
	edited := fmt.Sprintf(`[{"slug":"vulinbox-xss-escape","title":"改过的标题","category":"xss",`+
		`"difficulty":"apprentice","score":120,"challenge":{"type":"static","flag":"&lt;script&gt;"}}]`)
	_, _, err := svc.UploadChallenges(context.Background(), []byte(edited))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("re-upload builtin challenge: err = %v, want ErrValidation (422)", err)
	}
	var chal model.Challenge
	if err := gdb.Where("slug = ?", "vulinbox-xss-escape").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if !chal.IsBuiltin {
		t.Fatal("is_builtin cleared by rejected upload, want preserved")
	}
	if chal.Title == "改过的标题" || chal.Score == 120 {
		t.Fatalf("rejected re-upload must not take effect: %+v", chal)
	}
}

// noop 驱动下镜像状态：driver=noop、不可构建、恒缺失（不触碰 docker CLI）。
func TestBuiltinImageStatusNoopDriver(t *testing.T) {
	svc, _ := newBuiltinTestService(t, NoopDriver{})
	st, err := svc.BuiltinImageStatus(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Driver != "noop" || st.CanBuild || st.ImagePresent {
		t.Fatalf("noop status = %+v, want driver=noop can_build=false present=false", st)
	}
	if st.ImageTag != "yak-academy/vulinbox:latest" {
		t.Fatalf("image_tag = %q, want manifest tag", st.ImageTag)
	}
}

// Docker 驱动下镜像存在性走 dockerImageExists 探测（seam 替换为受控实现）。
func TestBuiltinImageStatusDockerProbe(t *testing.T) {
	orig := dockerImageExists
	t.Cleanup(func() { dockerImageExists = orig })
	svc, _ := newBuiltinTestService(t, &builtinBuildRecorder{})

	dockerImageExists = func(ctx context.Context, tag string) bool { return true }
	st, err := svc.BuiltinImageStatus(context.Background())
	if err != nil {
		t.Fatalf("status(present): %v", err)
	}
	if st.Driver != "docker" || !st.CanBuild || !st.ImagePresent {
		t.Fatalf("status = %+v, want docker/present", st)
	}

	dockerImageExists = func(ctx context.Context, tag string) bool { return false }
	st, err = svc.BuiltinImageStatus(context.Background())
	if err != nil {
		t.Fatalf("status(missing): %v", err)
	}
	if st.ImagePresent {
		t.Fatalf("status = %+v, want present=false when probe misses", st)
	}
}

// 仅构建镜像：复用一键导入的构建上下文，不写内容表。
func TestBuildBuiltinImageOnlyBuilds(t *testing.T) {
	rec := &builtinBuildRecorder{}
	svc, gdb := newBuiltinTestService(t, rec)
	st, err := svc.BuildBuiltinImageOnly(context.Background())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if st.Driver != "docker" || !st.ImagePresent {
		t.Fatalf("build status = %+v, want docker/present", st)
	}
	if rec.calls != 1 || rec.tag != "yak-academy/vulinbox:latest" {
		t.Fatalf("recorder = calls %d tag %q", rec.calls, rec.tag)
	}
	if !slices.Contains(rec.files, "Dockerfile") {
		t.Fatalf("build context files = %v, want Dockerfile", rec.files)
	}
	var n int64
	gdb.Model(&model.Challenge{}).Count(&n)
	if n != 0 {
		t.Fatalf("build-only wrote %d challenges, want 0", n)
	}
}

// noop 驱动拒绝构建（对外 422）。
func TestBuildBuiltinImageOnlyNoopRefused(t *testing.T) {
	svc, _ := newBuiltinTestService(t, NoopDriver{})
	_, err := svc.BuildBuiltinImageOnly(context.Background())
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// 内置教程目录：未导入时也能列出全量帐面（imported 全 false），
// 导入后逐篇 imported=true 且计数与目录总数一致（教程管理页数据源）。
func TestBuiltinTutorialCatalog(t *testing.T) {
	svc, _ := newBuiltinTestService(t, NoopDriver{})
	ctx := context.Background()

	catalog, err := svc.BuiltinTutorialCatalog(ctx)
	if err != nil {
		t.Fatalf("catalog before import: %v", err)
	}
	if len(catalog.Topics) != 5 {
		t.Fatalf("catalog topics = %d, want 5", len(catalog.Topics))
	}
	totalRooms := 0
	for _, topic := range catalog.Topics {
		for _, room := range topic.Rooms {
			totalRooms++
			if room.Imported {
				t.Fatalf("room %s imported before first import", room.Slug)
			}
		}
	}
	if totalRooms != 12 || catalog.ImportedRooms != 0 || catalog.ImportedTopics != 0 {
		t.Fatalf("catalog rooms/imported = %d/%d/%d, want 12/0/0", totalRooms, catalog.ImportedRooms, catalog.ImportedTopics)
	}

	if _, err := svc.ImportBuiltinChallenges(ctx); err != nil {
		t.Fatalf("import: %v", err)
	}
	catalog, err = svc.BuiltinTutorialCatalog(ctx)
	if err != nil {
		t.Fatalf("catalog after import: %v", err)
	}
	if catalog.ImportedTopics != len(catalog.Topics) || catalog.ImportedRooms != 12 {
		t.Fatalf("imported topics/rooms = %d/%d, want %d/12", catalog.ImportedTopics, catalog.ImportedRooms, len(catalog.Topics))
	}
	for _, topic := range catalog.Topics {
		for _, room := range topic.Rooms {
			if !room.Imported {
				t.Fatalf("room %s not imported after builtin import", room.Slug)
			}
			if err := validateDifficulty(room.Difficulty); err != nil {
				t.Fatalf("room %s difficulty %q: %v", room.Slug, room.Difficulty, err)
			}
		}
	}
}

// 内置教程禁编辑：内置导入占据 slug 后，历史同 slug 包的重传（owned 豁免
// 跳过通用 exists 检查）被 builtin 专项拦截（ErrValidation），内容不被覆盖。
func TestUploadRejectsBuiltinTutorialSlugs(t *testing.T) {
	svc, gdb := newBuiltinTestService(t, NoopDriver{})
	ctx := context.Background()
	// 1. 内置导入前上传历史包：yakit-jwt-forge 尚未被占用，合法落库。
	legacy := map[string]string{
		"manifest.yaml": `
name: legacy-package
version: 1.0.0
language: zh
topics:
  - legacy-topic
`,
		"topics/legacy-topic/topic.md": "---\ntitle: 历史专题\n---\n正文",
		"topics/legacy-topic/rooms/yakit-jwt-forge/lesson.md": "---\ntitle: 历史教程\ndifficulty: apprentice\n---\n正文",
	}
	if _, err := svc.Upload(ctx, buildZip(t, legacy)); err != nil {
		t.Fatalf("legacy upload: %v", err)
	}
	// 2. 内置导入 upsert 同 slug 行并置来源标记（导入内容覆盖历史行）。
	if _, err := svc.ImportBuiltinChallenges(ctx); err != nil {
		t.Fatalf("builtin import: %v", err)
	}
	// 3. 历史包重传新版本：owned 豁免跳过通用冲突检查，必须被 builtin 拦截。
	reupload := map[string]string{}
	for k, v := range legacy {
		reupload[k] = v
	}
	reupload["manifest.yaml"] = `
name: legacy-package
version: 2.0.0
language: zh
topics:
  - legacy-topic
`
	reupload["topics/legacy-topic/rooms/yakit-jwt-forge/lesson.md"] = "---\ntitle: 冒名覆盖\ndifficulty: apprentice\n---\n正文"
	_, err := svc.Upload(ctx, buildZip(t, reupload))
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	var room model.Room
	if err := gdb.Where("slug = ?", "yakit-jwt-forge").First(&room).Error; err != nil {
		t.Fatal(err)
	}
	if room.Title == "冒名覆盖" || !room.IsBuiltin {
		t.Fatalf("builtin room overwritten: title=%q is_builtin=%v", room.Title, room.IsBuiltin)
	}
}

// 内置教程禁删（历史兜底）：内置导入前上传的同 slug 包（合法落库），在内置
// 导入占据该 slug 后删除——DeletePackage 必须整体拒绝，内置行保持原样。
func TestDeletePackageRejectsBuiltinTutorials(t *testing.T) {
	svc, gdb := newBuiltinTestService(t, NoopDriver{})
	ctx := context.Background()
	// 1. 先上传普通包：此时 yakit-jwt-forge 尚未被内置占用，上传合法。
	files := map[string]string{
		"manifest.yaml": `
name: legacy-package
version: 1.0.0
language: zh
topics:
  - legacy-topic
`,
		"topics/legacy-topic/topic.md":      "---\ntitle: 历史专题\n---\n正文",
		"topics/legacy-topic/rooms/yakit-jwt-forge/lesson.md": "---\ntitle: 历史教程\ndifficulty: apprentice\n---\n正文",
	}
	pkg, err := svc.Upload(ctx, buildZip(t, files))
	if err != nil {
		t.Fatalf("legacy upload: %v", err)
	}
	// 2. 内置导入 upsert 同 slug 行并置来源标记（导入内容覆盖历史行）。
	if _, err := svc.ImportBuiltinChallenges(ctx); err != nil {
		t.Fatalf("builtin import: %v", err)
	}
	// 3. 删除历史包：payload 含内置 slug，整体拒绝。
	_, err = svc.DeletePackage(ctx, pkg.ID)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	var room model.Room
	if err := gdb.Where("slug = ?", "yakit-jwt-forge").First(&room).Error; err != nil {
		t.Fatalf("builtin room deleted: %v", err)
	}
	if !room.IsBuiltin || room.Title == "历史教程" {
		t.Fatalf("builtin room altered: title=%q is_builtin=%v", room.Title, room.IsBuiltin)
	}
	var n int64
	gdb.Model(&model.ContentPackage{}).Count(&n)
	if n != 1 {
		t.Fatalf("package rows = %d, want 1 (rejected delete keeps package)", n)
	}
}
