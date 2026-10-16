package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// 题目附件与外链（基础短板 Item 2）：
//   - links：challenge.json / challenge.yml 上传 → JSON 列 → 训练场/详情视图输出；
//   - 附件：上传落盘 + 元数据 upsert、学生下载校验、同名覆盖、删除级联清理。

func newAttachmentTestEnv(t *testing.T) (*ChallengeService, *ContentService, *gorm.DB, string) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:attachtest?mode=memory&cache=shared"), &gorm.Config{})
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
	ctx := context.Background()
	_ = ctx
	chalRepo := repo.NewChallengeRepo(gdb)
	chalSvc := NewChallengeService(chalRepo, repo.NewSubmissionRepo(gdb),
		repo.NewEventRepo(gdb), repo.NewScoreRepo(gdb), eventbus.New())
	dir := t.TempDir()
	chalSvc.SetAttachmentStore(repo.NewChallengeAttachmentRepo(gdb), dir)
	contentSvc := NewContentService(repo.NewContentPackageRepo(gdb), repo.NewTopicRepo(gdb),
		repo.NewRoomRepo(gdb), chalSvc, NoopDriver{}, repo.NewProgressRepo(gdb))
	_ = ctx
	return chalSvc, contentSvc, gdb, dir
}

func attachmentSeedChallenge(t *testing.T, cs *ChallengeService) {
	t.Helper()
	if err := cs.UpsertChallenge(context.Background(), &model.Challenge{
		Slug: "attach-chal", Title: "附件题", Category: "misc",
		Difficulty: model.DifficultyApprentice, Type: model.ChallengeStatic, Score: 100,
	}, []model.Flag{{Type: model.ChallengeStatic, Value: "flag{a}"}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestUploadAndListAttachment(t *testing.T) {
	chalSvc, _, _, _ := newAttachmentTestEnv(t)
	ctx := context.Background()
	attachmentSeedChallenge(t, chalSvc)

	att, err := chalSvc.UploadAttachment(ctx, "attach-chal", "流量分析.pcapng", strings.NewReader("pcap-bytes"))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if att.Filename != "attachment.pcapng" {
		t.Fatalf("filename not sanitized: %q", att.Filename)
	}
	views, err := chalSvc.ListStudentAttachments(ctx, "attach-chal")
	if err != nil || len(views) != 1 || views[0].Filename != "attachment.pcapng" || views[0].Size != int64(len("pcap-bytes")) {
		t.Fatalf("list after upload: %v %+v", err, views)
	}
}

func TestUploadAttachmentSameNameOverwrites(t *testing.T) {
	chalSvc, _, _, _ := newAttachmentTestEnv(t)
	ctx := context.Background()
	attachmentSeedChallenge(t, chalSvc)

	if _, err := chalSvc.UploadAttachment(ctx, "attach-chal", "data.zip", strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	att, err := chalSvc.UploadAttachment(ctx, "attach-chal", "data.zip", strings.NewReader("longer-v2"))
	if err != nil {
		t.Fatal(err)
	}
	if att.Size != int64(len("longer-v2")) {
		t.Fatalf("overwrite size = %d", att.Size)
	}
	list, err := chalSvc.ListAttachments(ctx, "attach-chal")
	if err != nil || len(list) != 1 {
		t.Fatalf("duplicate row after overwrite: %v %+v", err, list)
	}
}

func TestUploadAttachmentUnknownSlug(t *testing.T) {
	chalSvc, _, _, _ := newAttachmentTestEnv(t)
	if _, err := chalSvc.UploadAttachment(context.Background(), "no-such-chal", "a.bin", strings.NewReader("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUploadAttachmentTooLarge(t *testing.T) {
	chalSvc, _, _, _ := newAttachmentTestEnv(t)
	ctx := context.Background()
	attachmentSeedChallenge(t, chalSvc)
	// maxAttachmentSize+1 字节：LimitReader 截断后超限报 422。
	big := strings.Repeat("x", maxAttachmentSize+1)
	if _, err := chalSvc.UploadAttachment(ctx, "attach-chal", "big.bin", strings.NewReader(big)); !errors.Is(err, ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

func TestOpenAttachmentRejectsTraversal(t *testing.T) {
	chalSvc, _, _, _ := newAttachmentTestEnv(t)
	ctx := context.Background()
	attachmentSeedChallenge(t, chalSvc)
	if _, err := chalSvc.UploadAttachment(ctx, "attach-chal", "ok.txt", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := chalSvc.OpenAttachment(ctx, "attach-chal", "../ok.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for traversal, got %v", err)
	}
	if _, _, err := chalSvc.OpenAttachment(ctx, "attach-chal", "missing.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for missing, got %v", err)
	}
	f, name, err := chalSvc.OpenAttachment(ctx, "attach-chal", "ok.txt")
	if err != nil || name != "ok.txt" || f == nil {
		t.Fatalf("open ok: %v %v", name, err)
	}
	f.Close()
}

func TestDeleteAttachmentAndChallengeCascade(t *testing.T) {
	chalSvc, _, gdb, dir := newAttachmentTestEnv(t)
	ctx := context.Background()
	attachmentSeedChallenge(t, chalSvc)
	if _, err := chalSvc.UploadAttachment(ctx, "attach-chal", "one.txt", strings.NewReader("1")); err != nil {
		t.Fatal(err)
	}
	if _, err := chalSvc.UploadAttachment(ctx, "attach-chal", "two.txt", strings.NewReader("2")); err != nil {
		t.Fatal(err)
	}
	if err := chalSvc.DeleteAttachment(ctx, "attach-chal", "one.txt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := chalSvc.OpenAttachment(ctx, "attach-chal", "one.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted attachment still openable: %v", err)
	}
	// 删除题目级联：元数据行 + 磁盘目录一起清。
	if err := chalSvc.DeleteChallenge(ctx, "attach-chal"); err != nil {
		t.Fatalf("delete challenge: %v", err)
	}
	var n int64
	gdb.Model(&model.ChallengeAttachment{}).Count(&n)
	if n != 0 {
		t.Fatalf("attachment rows survive challenge delete: %d", n)
	}
	if _, err := chalSvc.ListAttachments(ctx, "attach-chal"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("challenge row still present: %v", err)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		t.Fatalf("attachment dir not cleaned: %v", entries)
	}
}

func TestLinksRoundTripViaChallengeJSON(t *testing.T) {
	_, contentSvc, gdb, _ := newAttachmentTestEnv(t)
	ctx := context.Background()
	raw := []byte(`{
		"slug": "link-chal", "title": "外链题", "category": "web",
		"difficulty": "apprentice", "score": 100,
		"challenge": {"type": "static", "flag": "flag{link}"},
		"links": [
			{"name": "Wireshark 下载", "url": "https://www.wireshark.org/"},
			{"name": "参考资料", "url": "http://example.com/doc"}
		]
	}`)
	if _, _, err := contentSvc.UploadChallenges(ctx, raw); err != nil {
		t.Fatalf("upload: %v", err)
	}
	db := gdb
	var chal model.Challenge
	if err := db.Where("slug = ?", "link-chal").First(&chal).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(chal.Links, "wireshark.org") {
		t.Fatalf("links column not stored: %q", chal.Links)
	}
	// 学生详情视图解析 JSON 列输出。
	topicSvc := NewTopicService(repo.NewTopicRepo(db), repo.NewRoomRepo(db),
		repo.NewChallengeRepo(db), repo.NewSubmissionRepo(db), repo.NewEventRepo(db))
	detail, err := topicSvc.GetChallengeDetail(ctx, 1, "link-chal")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Links) != 2 || detail.Links[0].Name != "Wireshark 下载" {
		t.Fatalf("detail links: %+v", detail.Links)
	}
	// 重传不带 links 的 JSON：upsert 清空外链。
	if _, _, err := contentSvc.UploadChallenges(ctx, []byte(`{
		"slug": "link-chal", "title": "外链题", "category": "web",
		"difficulty": "apprentice", "score": 100,
		"challenge": {"type": "static", "flag": "flag{link}"}
	}`)); err != nil {
		t.Fatal(err)
	}
	detail, err = topicSvc.GetChallengeDetail(ctx, 1, "link-chal")
	if err != nil || len(detail.Links) != 0 {
		t.Fatalf("links not cleared on re-upload: %v %+v", err, detail.Links)
	}
}

func TestLinksValidation(t *testing.T) {
	_, contentSvc, _, _ := newAttachmentTestEnv(t)
	ctx := context.Background()
	base := `{"slug": "link-bad", "title": "t", "category": "web", "difficulty": "apprentice",
		"score": 10, "challenge": {"type": "static", "flag": "flag{x}"}, "links": %s}`
	for _, links := range []string{
		`[{"name": "", "url": "https://a.b"}]`,
		`[{"name": "n", "url": "ftp://a.b"}]`,
		`[{"name": "n"}]`,
	} {
		if _, _, err := contentSvc.UploadChallenges(ctx, []byte(strings.Replace(base, "%s", links, 1))); !errors.Is(err, ErrValidation) {
			t.Fatalf("want ErrValidation for %s, got %v", links, err)
		}
	}
}