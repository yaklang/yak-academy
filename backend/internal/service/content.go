package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// ContentService 内容包五步管线：Upload → Parse → Validate → Persist → Publish。
// 顺序不可跳、不可合并；整体拒绝（禁止部分入库）（content-package-spec skill）。
type ContentService struct {
	packages  repo.ContentPackageRepo
	topics    repo.TopicRepo
	rooms     repo.RoomRepo
	challenge *ChallengeService
}

func NewContentService(p repo.ContentPackageRepo, t repo.TopicRepo, r repo.RoomRepo, c *ChallengeService) *ContentService {
	return &ContentService{packages: p, topics: t, rooms: r, challenge: c}
}

// parsedPackage 管线 Parse 步骤的内存中间产物。
type parsedPackage struct {
	Name    string
	Version string
	Topics  []model.Topic
	Rooms   []model.Room
	// Labs: room slug → 题目定义（教题一体：Lab 必属某 Room）。
	Labs map[string]labDef
}

type labDef struct {
	Title string
	Type  model.ChallengeType
	Flag  string
	Score int
	Hints []model.Hint
}

// Upload 处理上传的 zip：执行 Upload/Parse/Validate 三步并持久化为 draft。
// TODO(Phase2): Parse 与 Validate 的完整实现（front matter 解析、难度枚举、slug 冲突检测）。
func (s *ContentService) Upload(ctx context.Context, data []byte) (*model.ContentPackage, error) {
	parsed, err := s.parseZip(data)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if err := s.validate(ctx, parsed); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	sum := sha256.Sum256(data)
	pkg := &model.ContentPackage{
		Name:          parsed.Name,
		LatestVersion: parsed.Version,
		Status:        model.PackageDraft,
	}
	if err := s.packages.Create(ctx, pkg); err != nil {
		return nil, err
	}
	if err := s.packages.AddVersion(ctx, &model.ContentVersion{
		PackageID: pkg.ID, Version: parsed.Version,
		Status: model.PackageDraft, Checksum: hex.EncodeToString(sum[:]),
	}); err != nil {
		return nil, err
	}
	return pkg, nil
}

// parseZip 管线第 2 步：解压并解析 manifest/topics/rooms/lab.yaml。
// 第一版骨架仅完成解压骨架；front matter/lab schema 解析在 Phase 2 补全。
func (s *ContentService) parseZip(data []byte) (*parsedPackage, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	parsed := &parsedPackage{Labs: map[string]labDef{}}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if _, err := readZipFile(f); err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
	}
	// TODO(Phase2): manifest.yaml 必填校验；lesson.md front matter 解析；lab.yaml schema 解析。
	return parsed, nil
}

// validate 管线第 3 步：字段与冲突校验。
func (s *ContentService) validate(ctx context.Context, p *parsedPackage) error {
	if p.Name == "" {
		return fmt.Errorf("manifest name is required")
	}
	for _, t := range p.Topics {
		if err := s.validateDifficulty(); err != nil {
			return err
		}
		_ = t
	}
	return nil
}

func (s *ContentService) validateDifficulty() error {
	// TODO(Phase2): 难度枚举校验（apprentice/practitioner/expert）。
	return nil
}

// Publish 管线第 5 步：draft → published，发布时才把内容写入 Topic/Room/Challenge。
// 状态机唯一合法迁移见 content-package-spec skill。
func (s *ContentService) Publish(ctx context.Context, packageID uint) (*model.ContentPackage, error) {
	pkg, err := s.packages.FindByID(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if pkg.Status != model.PackageDraft {
		return nil, fmt.Errorf("invalid state transition: %s", pkg.Status)
	}
	if err := s.packages.UpdateStatus(ctx, packageID, model.PackagePublished); err != nil {
		return nil, err
	}
	return s.packages.FindByID(ctx, packageID)
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}