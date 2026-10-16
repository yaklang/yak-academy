package service

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 内置训练集（官方内容，随二进制分发：配套教程 + 题目，一教程对一题）。
// 这是「内容只来源于管理员上传」原则的唯一例外：训练集虽然随后端二进制分发，
// 但只有管理员在管理端显式一键导入才会写入内容库（见 docs/architecture.md 决策 2）。
//
//go:embed builtin/vulinbox
var builtinFS embed.FS

const builtinSetDir = "builtin/vulinbox"
const builtinTutorialsDir = "builtin/vulinbox/tutorials"

// builtinManifest 内置题集清单。
type builtinManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	ImageTag    string `json:"image_tag"`
	Port        int    `json:"port"`
	Source      string `json:"source"`
	License     string `json:"license"`
}

// BuiltinImportResult 一键导入结果（契约见 docs/openapi.yaml /admin/challenges/builtin）。
type BuiltinImportResult struct {
	Version    string   `json:"version"`
	Created    int      `json:"created"`
	Updated    int      `json:"updated"`
	Topics     int      `json:"topics"`
	Rooms      int      `json:"rooms"`
	Slugs      []string `json:"slugs"`
	ImageBuilt bool     `json:"image_built"`
}

// ImportBuiltinChallenges 一键导入内置训练集（教程 + 题目；幂等：重复调用 = 更新）。
// 流程：解析嵌入资产 → 先导入配套教程（Topic/Room，与题目 1:1）→ 预统计新增/更新 →
// 构建基础镜像（Docker 可用时）→ 复用 UploadChallenges 完成校验与逐题 upsert。
// 任一步失败整体拒绝（题目/教程同批生效，不出现有题无教程或反之的半成品）。
// 镜像构建失败则整体拒绝（与题目包「构建失败不入库」语义一致）；
// 未配置 Docker 驱动（noop）时跳过构建仅导入定义，ImageBuilt=false。
func (s *ContentService) ImportBuiltinChallenges(ctx context.Context) (*BuiltinImportResult, error) {
	manifest, err := readBuiltinManifest()
	if err != nil {
		return nil, err
	}
	challengesRaw, err := fs.ReadFile(builtinFS, path.Join(builtinSetDir, "challenges.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: builtin challenges missing: %v", ErrValidation, err)
	}
	challengesRaw, err = markBuiltinBank(challengesRaw)
	if err != nil {
		return nil, err
	}

	created, updated, err := s.countBuiltinDiff(ctx, challengesRaw)
	if err != nil {
		return nil, err
	}

	imageBuilt := false
	if !s.builtinBuildable() {
		// noop 驱动（本地无 Docker）：跳过构建，题目定义照常导入；
		// 配置 Docker 后再次导入即可补齐镜像。
	} else if err := s.buildBuiltinImage(ctx, manifest.ImageTag); err != nil {
		return nil, err
	} else {
		imageBuilt = true
	}

	// 镜像构建不碰内容表，放在最前保证失败时零写入；教程先于题目落库，
	// 题目落库时 tutorial_slug 指向的 Room 已存在，双向入口即刻生效。
	topics, rooms, err := s.importBuiltinTutorials(ctx)
	if err != nil {
		return nil, err
	}

	// 内部路径放行 tutorial：markBuiltinBank 已把全题集注入 tutorial，
	// 走公共 UploadChallenges 会被「上传禁入教程题库」校验拒绝。
	_, slugs, err := s.uploadChallenges(ctx, challengesRaw, true)
	if err != nil {
		return nil, err
	}
	// 普通上传路径不触碰来源标记；内置导入在此显式置 true（题库页「是否内置」过滤依据）。
	if _, err := s.challenge.MarkBuiltin(ctx, slugs, true); err != nil {
		return nil, err
	}
	return &BuiltinImportResult{
		Version:    manifest.Version,
		Created:    created,
		Updated:    updated,
		Topics:     topics,
		Rooms:      rooms,
		Slugs:      slugs,
		ImageBuilt: imageBuilt,
	}, nil
}

// readBuiltinManifest 读取嵌入的训练集清单（manifest.json）。
func readBuiltinManifest() (*builtinManifest, error) {
	manifestRaw, err := fs.ReadFile(builtinFS, path.Join(builtinSetDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: builtin manifest missing: %v", ErrValidation, err)
	}
	var manifest builtinManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, fmt.Errorf("%w: builtin manifest corrupt: %v", ErrValidation, err)
	}
	return &manifest, nil
}

// BuiltinImageStatus 管理端 vulinbox 基础镜像状态
// （GET /admin/challenges/builtin/image；契约见 docs/openapi.yaml）。
type BuiltinImageStatus struct {
	Driver       string `json:"driver"`
	ImageTag     string `json:"image_tag"`
	ImagePresent bool   `json:"image_present"`
	CanBuild     bool   `json:"can_build"`
}

// BuiltinImageStatus 查询基础镜像状态：驱动由 builder 装配类型推断
// （NoopDriver = noop）；镜像存在性只在 Docker 驱动下探测（noop 恒缺失）。
func (s *ContentService) BuiltinImageStatus(ctx context.Context) (*BuiltinImageStatus, error) {
	manifest, err := readBuiltinManifest()
	if err != nil {
		return nil, err
	}
	canBuild := s.builtinBuildable()
	return &BuiltinImageStatus{
		Driver:       map[bool]string{true: "docker", false: "noop"}[canBuild],
		ImageTag:     manifest.ImageTag,
		ImagePresent: canBuild && dockerImageExists(ctx, manifest.ImageTag),
		CanBuild:     canBuild,
	}, nil
}

// BuildBuiltinImageOnly 仅构建基础镜像、不写任何内容表
// （POST /admin/challenges/builtin/image；与一键导入的镜像构建步骤同一实现：
// 解包嵌入 image/ 上下文 → docker build 内从官方 OSS 下载 vulinbox 二进制）。
// noop 驱动（无 Docker）返回 ErrValidation（对外 422）。
func (s *ContentService) BuildBuiltinImageOnly(ctx context.Context) (*BuiltinImageStatus, error) {
	manifest, err := readBuiltinManifest()
	if err != nil {
		return nil, err
	}
	if !s.builtinBuildable() {
		return nil, fmt.Errorf("%w: docker driver unavailable; cannot build builtin image", ErrValidation)
	}
	if err := s.buildBuiltinImage(ctx, manifest.ImageTag); err != nil {
		return nil, err
	}
	return &BuiltinImageStatus{
		Driver: "docker", ImageTag: manifest.ImageTag, ImagePresent: true, CanBuild: true,
	}, nil
}

// importBuiltinTutorials 导入内置训练集的配套教程（Topic/Room）。
// 复用教程包解析与静态校验（parseFiles/validateParsed）；绕过包状态机
// 直接 Upsert——与题目同语义的幂等写入，重复导入 = 更新。
// Room slug 统一 yakit- 前缀命名空间；challenges.json 每题 tutorial_slug
// 指向配套 Room（1:1），详见 docs/architecture.md 决策 2 例外。
func (s *ContentService) importBuiltinTutorials(ctx context.Context) (topics, rooms int, err error) {
	files := map[string][]byte{}
	if err := fs.WalkDir(builtinFS, builtinTutorialsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := builtinFS.ReadFile(p)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(p, builtinTutorialsDir+"/")] = data
		return nil
	}); err != nil {
		return 0, 0, fmt.Errorf("%w: builtin tutorials unreadable: %v", ErrValidation, err)
	}
	parsed, err := s.parseFiles(files)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: builtin tutorials corrupt: %v", ErrValidation, err)
	}
	if err := validateParsed(parsed); err != nil {
		return 0, 0, fmt.Errorf("%w: builtin tutorials invalid: %v", ErrValidation, err)
	}
	for i := range parsed.Topics {
		if err := s.topics.Upsert(ctx, &parsed.Topics[i]); err != nil {
			return 0, 0, err
		}
	}
	for _, rp := range parsed.Rooms {
		if err := s.rooms.Upsert(ctx, &model.Room{
			Slug: rp.Slug, TopicSlug: rp.TopicSlug, Title: rp.Title,
			Difficulty: rp.Difficulty, LessonMD: rp.LessonMD, VideoURL: rp.VideoURL,
		}); err != nil {
			return 0, 0, err
		}
	}
	return len(parsed.Topics), len(parsed.Rooms), nil
}

// markBuiltinBank 内置题集统一归入教程题目 tab：上传路径已禁用 tutorial，
// 这里显式注入，保证「教程题目 = 内置题集」不被普通上传稀释。
func markBuiltinBank(raw []byte) ([]byte, error) {
	var batch []challengeJSON
	if err := json.Unmarshal(raw, &batch); err != nil {
		return nil, fmt.Errorf("%w: builtin challenges corrupt: %v", ErrValidation, err)
	}
	for i := range batch {
		batch[i].Bank = model.BankTutorial
	}
	out, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// countBuiltinDiff 预统计本次导入的新增/更新题数（按 slug 存在性判定）。
func (s *ContentService) countBuiltinDiff(ctx context.Context, raw []byte) (created, updated int, err error) {
	var batch []challengeJSON
	if err := json.Unmarshal(raw, &batch); err != nil {
		return 0, 0, fmt.Errorf("%w: builtin challenges corrupt: %v", ErrValidation, err)
	}
	for _, c := range batch {
		_, findErr := s.challenge.challenges.FindBySlug(ctx, c.Slug)
		switch {
		case findErr == nil:
			updated++
		case errors.Is(findErr, gorm.ErrRecordNotFound):
			created++
		default:
			return 0, 0, findErr
		}
	}
	return created, updated, nil
}

// buildBuiltinImage 解包嵌入的 image/ 目录到临时目录并构建基础镜像。
func (s *ContentService) buildBuiltinImage(ctx context.Context, tag string) error {
	dir, err := os.MkdirTemp("", "yak-builtin-build-*")
	if err != nil {
		return fmt.Errorf("%w: create build dir: %v", ErrValidation, err)
	}
	defer os.RemoveAll(dir)
	if err := extractBuiltinDir(dir); err != nil {
		return err
	}
	if err := s.builder.Build(ctx, tag, dir); err != nil {
		return fmt.Errorf("%w: builtin image build failed: %v", ErrValidation, err)
	}
	return nil
}

// extractBuiltinDir 把嵌入的 image/ 目录展开到本地临时目录（docker build 上下文）。
func extractBuiltinDir(dst string) error {
	root := path.Join(builtinSetDir, "image")
	return fs.WalkDir(builtinFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil // 根目录本身即 dst
		}
		rel := strings.TrimPrefix(p, root+"/")
		target := path.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := builtinFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// builtinBuildable 当前装配是否具备镜像构建能力（nil / NoopDriver 均视为不可用）。
func (s *ContentService) builtinBuildable() bool {
	if s.builder == nil {
		return false
	}
	_, isNoop := s.builder.(NoopDriver)
	return !isNoop
}
