package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/eventbus"
	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ChallengeService 题目域业务：题目是独立资源（challenge.json / challenge.yml 包上传）。
// 浏览视图（训练场/详情）在 TopicService 中；本服务供判题、内容管线与提示解锁使用。
type ChallengeService struct {
	challenges  repo.ChallengeRepo
	submissions repo.SubmissionRepo
	events      repo.EventRepo
	scores      repo.ScoreRepo
	bus         *eventbus.Bus
	// env 延迟注入（main 装配顺序：envSvc 依赖 challengeRepo 构造在后）。
	// 删除题目前销毁运行中容器用；nil 时跳过（仅测试场景）。
	env *EnvService
	// attachments 题目附件元数据表 + 文件落盘根目录。
	attachments repo.ChallengeAttachmentRepo
	attachDir   string
	// now 时钟注入（测试固定时间）。
	now func() time.Time
}

func NewChallengeService(c repo.ChallengeRepo, s repo.SubmissionRepo, ev repo.EventRepo, scores repo.ScoreRepo, bus *eventbus.Bus) *ChallengeService {
	return &ChallengeService{
		challenges: c, submissions: s, events: ev, scores: scores, bus: bus,
		now: time.Now,
	}
}

// SetEnvService 注入实例服务（main 中 envSvc 构造后调用一次）。
func (s *ChallengeService) SetEnvService(env *EnvService) { s.env = env }

// SetAttachmentStore 注入附件存储（main 装配后调用一次；nil = 附件接口不可用）。
func (s *ChallengeService) SetAttachmentStore(repo repo.ChallengeAttachmentRepo, dir string) {
	s.attachments = repo
	s.attachDir = dir
}

// SetVisibility 管理端更新可见性/定时发布。nil = 不改该字段；
// clearRelease=true 时清空定时（立即语义）——需要三态（不改/清空/设值），
// 单 *time.Time 无法区分「清空」与「不改」。
type VisibilityUpdate struct {
	IsVisible     *bool
	ReleaseAt     *time.Time
	ClearRelease  bool
}

func (s *ChallengeService) SetVisibility(ctx context.Context, slug string, u VisibilityUpdate) (*model.Challenge, error) {
	if u.IsVisible == nil && u.ReleaseAt == nil && !u.ClearRelease {
		return nil, fmt.Errorf("%w: empty visibility update", ErrValidation)
	}
	var releaseAt *time.Time
	if u.ClearRelease {
		releaseAt = &time.Time{} // 零值 → SQL NULL（gorm 对指针零值 Updates 需显式赋 NULL）
	}
	if u.ReleaseAt != nil {
		releaseAt = u.ReleaseAt
	}
	rows, err := s.challenges.UpdateVisibility(ctx, slug, u.IsVisible, releaseAt)
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, ErrNotFound
	}
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	return chal, nil
}

// UpsertChallenge 内容发布时写入题目与判定答案/提示（content→challenge 白名单依赖）。
func (s *ChallengeService) UpsertChallenge(ctx context.Context, c *model.Challenge, flags []model.Flag, hints []model.Hint) error {
	return s.challenges.Upsert(ctx, c, flags, hints)
}

// MarkBuiltin 批量改写题目来源标记（内置题集导入置 true；返回受影响行数）。
// 普通上传路径不经此方法，管理端编辑内置题不会丢失来源标记。
func (s *ChallengeService) MarkBuiltin(ctx context.Context, slugs []string, builtin bool) (int64, error) {
	return s.challenges.MarkBuiltin(ctx, slugs, builtin)
}

// compareFlag 恒时比较，防时序侧信道。
// 禁止改为 == 比较（event-sourcing-rules skill）。
func compareFlag(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// 题目附件（Item 2 基础短板）：文件落盘 <attachDir>/<slug>/<filename>，
// 元数据 challenge_attachments 表；同名覆盖更新；删除题目前提下级联清理。
// 文件名规范化：仅保留安全字符，杜绝路径穿越（zip-slip 同理）。

// attachmentNamePattern 非法字符折叠：空白/中文/路径分隔等 → -（. 保留给扩展名）。
var attachmentNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// sanitizeFilename 规范化上传文件名：stem 与扩展名分别处理，
// 仅保留 [a-zA-Z0-9._-]，其余折叠为 -；非 ASCII stem 回退 attachment
// （磁盘/URL/Content-Disposition 三处免转码）；结果禁止 . 开头与穿越段。
func sanitizeFilename(name string) (string, error) {
	name = strings.TrimSpace(filepath.ToSlash(filepath.Base(name)))
	ext := strings.Trim(attachmentNamePattern.ReplaceAllString(filepath.Ext(name), "-"), "-")
	if ext != "" {
		ext = "." + ext // 折叠会吃掉前导点，补回
	}
	stem := attachmentNamePattern.ReplaceAllString(strings.TrimSuffix(name, filepath.Ext(name)), "-")
	stem = strings.Trim(stem, "-.")
	if stem == "" {
		stem = "attachment"
	}
	name = stem + ext
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%w: invalid attachment filename", ErrValidation)
	}
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	return name, nil
}

// maxAttachmentSize 单附件上限 50MB（与教程包/题目包一致）。
const maxAttachmentSize = 50 << 20

// UploadAttachment 管理端上传题目附件：写文件 + upsert 元数据。
// 题目必须已存在（附件挂在既有 slug 上）。
func (s *ChallengeService) UploadAttachment(ctx context.Context, slug, origName string, r io.Reader) (*model.ChallengeAttachment, error) {
	if s.attachments == nil {
		return nil, errNotImplemented("attachment store")
	}
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	filename, err := sanitizeFilename(origName)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.attachDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, filename)
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	size, err := io.Copy(f, io.LimitReader(r, maxAttachmentSize+1))
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if size > maxAttachmentSize {
		os.Remove(tmp)
		return nil, fmt.Errorf("%w: attachment exceeds 50MB limit", ErrValidation)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return nil, err
	}
	att := &model.ChallengeAttachment{
		ChallengeID: chal.ID, Filename: filename, Size: size, CreatedAt: s.now(),
	}
	if err := s.attachments.Upsert(ctx, att); err != nil {
		os.Remove(path)
		return nil, err
	}
	return att, nil
}

// ListAttachments 管理端：题目附件元数据列表。
func (s *ChallengeService) ListAttachments(ctx context.Context, slug string) ([]model.ChallengeAttachment, error) {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	return s.attachments.ListByChallenge(ctx, chal.ID)
}

// AttachmentView 学生端附件条目（下载 URL 相对 /api/v1）。
type AttachmentView struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

// ListStudentAttachments 学生端：题目附件列表（详情页渲染下载入口）。
func (s *ChallengeService) ListStudentAttachments(ctx context.Context, slug string) ([]AttachmentView, error) {
	list, err := s.ListAttachments(ctx, slug)
	if err != nil {
		return nil, err
	}
	out := make([]AttachmentView, 0, len(list))
	for _, a := range list {
		out = append(out, AttachmentView{Filename: a.Filename, Size: a.Size})
	}
	return out, nil
}

// OpenAttachment 学生端下载：校验元数据命中后打开文件（属主校验在路由层 = 登录即可）。
// 返回文件与规范化文件名；未命中返回 ErrNotFound。
func (s *ChallengeService) OpenAttachment(ctx context.Context, slug, filename string) (*os.File, string, error) {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, "", mapRecordNotFound(err)
	}
	safe, err := sanitizeFilename(filename)
	if err != nil || safe != filename {
		return nil, "", ErrNotFound // 路径穿越/非法名：对外统一 404 语义
	}
	att, err := s.attachments.Find(ctx, chal.ID, filename)
	if err != nil {
		return nil, "", err
	}
	if att == nil {
		return nil, "", ErrNotFound
	}
	f, err := os.Open(filepath.Join(s.attachDir, slug, filename))
	if err != nil {
		return nil, "", ErrNotFound
	}
	return f, filename, nil
}

// DeleteAttachment 管理端删除附件：元数据行 + 磁盘文件。
func (s *ChallengeService) DeleteAttachment(ctx context.Context, slug, filename string) error {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return mapRecordNotFound(err)
	}
	att, err := s.attachments.Find(ctx, chal.ID, filename)
	if err != nil {
		return err
	}
	if att == nil {
		return ErrNotFound
	}
	if err := s.attachments.Delete(ctx, att.ID); err != nil {
		return err
	}
	os.Remove(filepath.Join(s.attachDir, slug, filename))
	return nil
}

// DeleteChallengeAttachments 删除题目时级联清理附件（元数据 + 目录）。
// 附件库未注入（测试场景）时跳过——题目行本身照常删除。
func (s *ChallengeService) DeleteChallengeAttachments(ctx context.Context, slug string) error {
	if s.attachments == nil {
		return nil
	}
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return mapRecordNotFound(err)
	}
	if err := s.attachments.DeleteByChallenge(ctx, chal.ID); err != nil {
		return err
	}
	os.RemoveAll(filepath.Join(s.attachDir, slug))
	return nil
}

// mapRecordNotFound repo 层 gorm.ErrRecordNotFound → service.ErrNotFound（404 语义统一）。
func mapRecordNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
