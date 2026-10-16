package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/pathutil"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ProfileService 账号信息面板 + 站内邮箱。
//   - 面板：内置账号 ID、账号名称、简介、总得分（事件派生只读）、头像。
//   - 邮箱：只收不发——投递入口 DeliverInvite 仅供竞赛域调用（横向依赖白名单：
//     competition → profile），用户侧仅读 + 标记已读。
type ProfileService struct {
	users   repo.UserRepo
	scores  repo.ScoreRepo
	mails   repo.MailRepo
	avatars string // 头像落盘根目录（<dir>/<userID>/<file>）
}

func NewProfileService(users repo.UserRepo, scores repo.ScoreRepo, mails repo.MailRepo, avatarDir string) *ProfileService {
	return &ProfileService{users: users, scores: scores, mails: mails, avatars: avatarDir}
}

// UserProfile 账号信息面板读模型（契约 UserProfile schema）。
type UserProfile struct {
	ID         uint             `json:"id"`
	Username   string           `json:"username"`
	GlobalRole model.GlobalRole `json:"global_role"`
	Bio        string           `json:"bio"`
	Score      int              `json:"score"`
	SolveCount int              `json:"solve_count"`
	Avatar     *string          `json:"avatar"`
	CreatedAt  time.Time        `json:"created_at"`
}

const maxBioLength = 500

func (s *ProfileService) Profile(ctx context.Context, userID uint) (*UserProfile, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	p := &UserProfile{
		ID: u.ID, Username: u.Username, GlobalRole: u.GlobalRole.Normalize(),
		Bio: u.Bio, CreatedAt: u.CreatedAt,
	}
	// 总得分是事件派生数据（event-sourcing-rules skill）：零值行视为 0 分。
	if sr, err := s.scores.Get(ctx, userID); err == nil {
		p.Score, p.SolveCount = sr.Score, sr.SolveCount
	} else if !errorsIsNotFound(err) {
		return nil, err
	}
	if u.AvatarFile != "" {
		avatar := "/api/v1/me/avatar"
		p.Avatar = &avatar
	}
	return p, nil
}

func (s *ProfileService) UpdateBio(ctx context.Context, userID uint, bio string) (*UserProfile, error) {
	// 简介按 rune 截断校验（中文场景按字数而非字节）。
	if len([]rune(bio)) > maxBioLength {
		return nil, fmt.Errorf("%w: bio exceeds 500 characters", ErrValidation)
	}
	if err := s.users.UpdateBio(ctx, userID, bio); err != nil {
		return nil, err
	}
	return s.Profile(ctx, userID)
}

// MaxAvatarSize 头像上限 2MB（参考项目同限；题目附件 50MB 不适用于头像）。
// 导出供 api 层设置 MaxBytesReader（multipart 头部余量在 api 层另加）。
const MaxAvatarSize = 2 << 20

// avatarExts 允许的图片扩展（MIME 由扩展映射，Content-Type 用嗅探兜底）。
var avatarExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

// UploadAvatar 上传头像：校验类型/大小 → 原子写盘（tmp+rename）→ 回写用户记录。
// 文件名固定为 avatar<ext>（每用户一个，重复上传覆盖），旧文件自然被替换。
func (s *ProfileService) UploadAvatar(ctx context.Context, userID uint, origName string, r io.Reader) (*UserProfile, error) {
	if s.avatars == "" {
		return nil, errNotImplemented("avatar store")
	}
	ext := strings.ToLower(filepath.Ext(origName))
	if !avatarExts[ext] {
		return nil, fmt.Errorf("%w: avatar must be png/jpg/gif/webp", ErrValidation)
	}
	dir := pathutil.Join(s.avatars, fmt.Sprint(userID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	name := "avatar" + ext
	path := pathutil.Join(dir, name)
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	size, err := io.Copy(f, io.LimitReader(r, MaxAvatarSize+1))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if size > MaxAvatarSize {
		os.Remove(tmp)
		return nil, fmt.Errorf("%w: avatar exceeds 2MB limit", ErrValidation)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := s.users.UpdateAvatar(ctx, userID, name); err != nil {
		return nil, err
	}
	return s.Profile(ctx, userID)
}

// OpenAvatar 打开当前用户头像文件（供 HTTP 下载）。
func (s *ProfileService) OpenAvatar(ctx context.Context, userID uint) (*os.File, string, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, "", mapRecordNotFound(err)
	}
	if u.AvatarFile == "" || s.avatars == "" {
		return nil, "", ErrNotFound
	}
	ext := strings.ToLower(filepath.Ext(u.AvatarFile))
	if !avatarExts[ext] {
		return nil, "", ErrNotFound
	}
	f, err := os.Open(pathutil.Join(s.avatars, fmt.Sprint(userID), u.AvatarFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	return f, ext, nil
}

// AvatarContentType 按扩展映射响应 Content-Type（下载接口用）。
func AvatarContentType(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// --- 站内邮箱 ---

// MailItem 邮件读模型（契约 MailItem schema；InviteCode 仅本人可见）。
type MailItem struct {
	ID            uint      `json:"id"`
	Title         string    `json:"title"`
	BodyMd        string    `json:"body_md"`
	Kind          string    `json:"kind"`
	CompetitionID uint      `json:"competition_id"`
	InviteCode    string    `json:"invite_code"`
	Read          bool      `json:"read"`
	CreatedAt     time.Time `json:"created_at"`
}

// MailPage 分页读模型（unread 为全量未读数，供角标）。
type MailPage struct {
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
	Unread   int64      `json:"unread"`
	Items    []MailItem `json:"items"`
}

func toMailItem(m model.Mail) MailItem {
	return MailItem{
		ID: m.ID, Title: m.Title, BodyMd: m.BodyMd, Kind: m.Kind,
		CompetitionID: m.CompetitionID, InviteCode: m.InviteCode,
		Read: m.Read, CreatedAt: m.CreatedAt,
	}
}

// ListMails 本人收件箱（分页 + 未读数）。
func (s *ProfileService) ListMails(ctx context.Context, userID uint, page, pageSize int) (*MailPage, error) {
	items, total, err := s.mails.List(ctx, userID, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}
	unread, err := s.mails.UnreadCount(ctx, userID)
	if err != nil {
		return nil, err
	}
	mp := &MailPage{Total: total, Page: page, PageSize: pageSize, Unread: unread, Items: []MailItem{}}
	for _, m := range items {
		mp.Items = append(mp.Items, toMailItem(m))
	}
	return mp, nil
}

// UnreadCount 未读数（头部角标轮询）。
func (s *ProfileService) UnreadCount(ctx context.Context, userID uint) (int64, error) {
	return s.mails.UnreadCount(ctx, userID)
}

// MarkRead 标记已读（仅本人邮件；不存在/非本人 → ErrNotFound）。
func (s *ProfileService) MarkRead(ctx context.Context, userID, mailID uint) error {
	n, err := s.mails.MarkRead(ctx, userID, mailID)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeliverInvite 竞赛邀请投递（竞赛域调用，不经 HTTP）。kind=invite 附邀请码。
func (s *ProfileService) DeliverInvite(ctx context.Context, userID uint, title, bodyMd string, competitionID uint, inviteCode string) error {
	return s.mails.Deliver(ctx, &model.Mail{
		UserID: userID, Title: title, BodyMd: bodyMd,
		Kind: "invite", CompetitionID: competitionID, InviteCode: inviteCode,
	})
}

// errorsIsNotFound gorm not found 判定（scores.Get 零值行场景）。
func errorsIsNotFound(err error) bool { return err == gorm.ErrRecordNotFound }
