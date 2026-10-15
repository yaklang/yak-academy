package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/db"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// newProfileTestService 构造 ProfileService + 内存库（含一名学生与派生积分行）。
func newProfileTestService(t *testing.T) (*ProfileService, *gorm.DB, uint) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:profiletest?mode=memory&cache=shared"), &gorm.Config{})
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
	userRepo := repo.NewUserRepo(gdb)
	u := &model.User{Username: "stu-profile", GlobalRole: model.GlobalRoleStudent}
	if err := userRepo.Create(context.Background(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	// 派生积分行（正常由事件派生器写，测试直接落一行读模型数据）。
	if err := gdb.Create(&model.ScoreRecord{UserID: u.ID, Score: 120, SolveCount: 3}).Error; err != nil {
		t.Fatalf("create score: %v", err)
	}
	dir := t.TempDir()
	svc := NewProfileService(userRepo, repo.NewScoreRepo(gdb), repo.NewMailRepo(gdb), dir)
	return svc, gdb, u.ID
}

func TestProfileReadsDerivedScoreAndZeroState(t *testing.T) {
	svc, _, uid := newProfileTestService(t)
	ctx := context.Background()

	// 有派生行：面板呈现总得分/解题数。
	p, err := svc.Profile(ctx, uid)
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	if p.Score != 120 || p.SolveCount != 3 {
		t.Fatalf("score projection: got score=%d solve=%d, want 120/3", p.Score, p.SolveCount)
	}
	if p.Avatar != nil {
		t.Fatalf("avatar should be nil before upload")
	}

	// 无派生行的用户（admin）按 0 分呈现，不报错。
	admin := &model.User{Username: "admin-profile", GlobalRole: model.GlobalRoleAdmin}
	if err := svc.users.Create(ctx, admin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	p2, err := svc.Profile(ctx, admin.ID)
	if err != nil {
		t.Fatalf("profile admin: %v", err)
	}
	if p2.Score != 0 || p2.SolveCount != 0 {
		t.Fatalf("admin score should be 0, got %d/%d", p2.Score, p2.SolveCount)
	}
}

func TestUpdateBioValidatesAndPersists(t *testing.T) {
	svc, gdb, uid := newProfileTestService(t)
	ctx := context.Background()

	// 超长简介（>500 字）拒绝。
	if _, err := svc.UpdateBio(ctx, uid, strings.Repeat("长", 501)); !errors.Is(err, ErrValidation) {
		t.Fatalf("oversized bio: want ErrValidation, got %v", err)
	}
	// 正常更新：面板返回 + 落库断言。
	if _, err := svc.UpdateBio(ctx, uid, "安全研究员"); err != nil {
		t.Fatalf("update bio: %v", err)
	}
	var u model.User
	if err := gdb.First(&u, uid).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.Bio != "安全研究员" {
		t.Fatalf("bio not persisted: %q", u.Bio)
	}
}

func TestUploadAvatarValidatesTypeSizeAndServes(t *testing.T) {
	svc, _, uid := newProfileTestService(t)
	ctx := context.Background()

	// 非图片扩展拒绝。
	if _, err := svc.UploadAvatar(ctx, uid, "shell.php", strings.NewReader("x")); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad ext: want ErrValidation, got %v", err)
	}
	// 超限（>2MB）拒绝。
	if _, err := svc.UploadAvatar(ctx, uid, "big.png", strings.NewReader(strings.Repeat("a", MaxAvatarSize+1))); !errors.Is(err, ErrValidation) {
		t.Fatalf("oversized: want ErrValidation, got %v", err)
	}
	// 正常上传：返回 URL + 文件可打开 + 记录回写。
	if _, err := svc.UploadAvatar(ctx, uid, "me.png", strings.NewReader("pngdata")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	f, ext, err := svc.OpenAvatar(ctx, uid)
	if err != nil {
		t.Fatalf("open avatar: %v", err)
	}
	defer f.Close()
	if ext != ".png" || AvatarContentType(ext) != "image/png" {
		t.Fatalf("ext/content-type: %q", ext)
	}
	// 重复上传（换扩展）：覆盖旧文件，记录指向新名。
	if _, err := svc.UploadAvatar(ctx, uid, "me.jpg", strings.NewReader("jpgdata")); err != nil {
		t.Fatalf("re-upload: %v", err)
	}
	f2, _, err := svc.OpenAvatar(ctx, uid)
	if err != nil {
		t.Fatalf("open after overwrite: %v", err)
	}
	f2.Close()
	if _, err := os.Stat(filepath.Join(svc.avatars, "shell.php")); err == nil {
		t.Fatal("php file must never be written")
	}
}

func TestMailboxDeliverListUnreadMarkRead(t *testing.T) {
	svc, _, uid := newProfileTestService(t)
	ctx := context.Background()

	// 投递两封竞赛邀请：先投已读，后投未读（id 更大 → 列表更靠前）。
	for i, read := range []bool{true, false} {
		m := &model.Mail{UserID: uid, Title: "邀请", Kind: "invite", CompetitionID: 7, InviteCode: "CODE7"}
		if read {
			m.Read = true
		}
		m.Title = strings.Repeat("t", i+1)
		if err := svc.mails.Deliver(ctx, m); err != nil {
			t.Fatalf("deliver: %v", err)
		}
	}
	// 未读数。
	n, err := svc.UnreadCount(ctx, uid)
	if err != nil || n != 1 {
		t.Fatalf("unread: n=%d err=%v, want 1", n, err)
	}
	// 分页列表：新邮件在前，未读数一并返回。
	mp, err := svc.ListMails(ctx, uid, 1, 20)
	if err != nil {
		t.Fatalf("list mails: %v", err)
	}
	if mp.Total != 2 || mp.Unread != 1 || len(mp.Items) != 2 {
		t.Fatalf("mail page: total=%d unread=%d items=%d", mp.Total, mp.Unread, len(mp.Items))
	}
	// 投递顺序：先投已读、后投未读 → id DESC 下未读在前。
	if mp.Items[0].Read || !mp.Items[1].Read {
		t.Fatal("order: newest (unread) mail must be first")
	}
	if mp.Items[0].InviteCode != "CODE7" || mp.Items[0].CompetitionID != 7 {
		t.Fatalf("invite fields lost: %+v", mp.Items[0])
	}
	// 标记已读：未读数归零；重复标记幂等。
	if err := svc.MarkRead(ctx, uid, mp.Items[0].ID); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	n, _ = svc.UnreadCount(ctx, uid)
	if n != 0 {
		t.Fatalf("unread after read: %d, want 0", n)
	}
	if err := svc.MarkRead(ctx, uid, mp.Items[0].ID); err != nil {
		t.Fatalf("idempotent mark read: %v", err)
	}
	// 他人邮件不可标记（属主校验 → NotFound）。
	if err := svc.MarkRead(ctx, uid+999, mp.Items[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign mail: want ErrNotFound, got %v", err)
	}
	// DeliverInvite 供竞赛域投递（不经 HTTP）。
	if err := svc.DeliverInvite(ctx, uid, "竞赛邀请", "欢迎参加", 9, "CODE9"); err != nil {
		t.Fatalf("deliver invite: %v", err)
	}
	n, _ = svc.UnreadCount(ctx, uid)
	if n != 1 {
		t.Fatalf("unread after invite: %d, want 1", n)
	}
}