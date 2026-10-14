package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

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
	builder   ImageBuilder // 题目包 image: . 时构建；可为 NoopDriver
	// ownedTopics/ownedRooms 同名包既有版本的 slug 集合（validate 时视为自有，
	// 不判冲突；仅 Upload 重传路径填充）。
	ownedTopics map[string]bool
	ownedRooms  map[string]bool
}

func (s *ContentService) setOwnedSlugs(topics, rooms map[string]bool) {
	s.ownedTopics, s.ownedRooms = topics, rooms
}

func NewContentService(p repo.ContentPackageRepo, t repo.TopicRepo, r repo.RoomRepo, c *ChallengeService, builder ImageBuilder) *ContentService {
	if builder == nil {
		builder = NoopDriver{}
	}
	return &ContentService{packages: p, topics: t, rooms: r, challenge: c, builder: builder}
}

// parsedPackage 管线 Parse 步骤的内存中间产物，序列化后存 ContentVersion.Payload。
type parsedPackage struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Topics  []model.Topic `json:"topics"`
	// Rooms 用 DTO 而非 model.Room：model.Room.LessonMD 是 json:"-"（详情接口单独返回），
	// 直接序列化会丢正文。
	Rooms []roomPayload `json:"rooms"`
	// Labs 旧版本 Payload 兼容字段：题目独立后新包不再产出，回放旧版本时忽略。
	Labs map[string]labDef `json:"labs,omitempty"`
}

// roomPayload Payload 中的 Room 序列化形态（补回 LessonMD）。
type roomPayload struct {
	Slug       string           `json:"slug"`
	TopicSlug  string           `json:"topic_slug"`
	Title      string           `json:"title"`
	Difficulty model.Difficulty `json:"difficulty"`
	LessonMD   string           `json:"lesson_md"`
	VideoURL   string           `json:"video_url,omitempty"` // 视频教程地址（可选）
}

type labDef struct {
	Title string          `json:"title"`
	Type  model.ChallengeType `json:"type"`
	Flag  string          `json:"flag"`
	Score int             `json:"score"`
	Image string          `json:"image,omitempty"` // lab.yaml env 段（可选）：容器镜像
	Port  int             `json:"port,omitempty"`  // 容器端口
	Hints []model.Hint    `json:"hints"`
}

// manifest 内容包元数据（manifest.yaml）。
type manifest struct {
	Name     string   `yaml:"name"`
	Version  string   `yaml:"version"`
	Language string   `yaml:"language"`
	Topics   []string `yaml:"topics"`
}

// topicFront topic.md 的 front matter。
type topicFront struct {
	Title string `yaml:"title"`
}

// lessonFront lesson.md 的 front matter（difficulty 三级枚举，teaching-integration skill）。
// video 可选：视频教程地址（文字+视频教程）。
type lessonFront struct {
	Title     string           `yaml:"title"`
	Difficulty model.Difficulty `yaml:"difficulty"`
	Video     string           `yaml:"video"`
}

var slugPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Upload 处理上传的 zip：执行 Upload/Parse/Validate 三步并持久化为 draft。
// 解析/校验失败整体拒绝，包与版本均不入库。
func (s *ContentService) Upload(ctx context.Context, data []byte) (*model.ContentPackage, error) {
	parsed, err := s.parseZip(data)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	// 同名包重传时，本包既有版本拥有的 slug 视为自有（重传/升级合法，
	// 发布后按 Upsert 覆盖）；只有他包/裸库内容占用的 slug 才算冲突。
	var ownedTopics, ownedRooms map[string]bool
	if pkg, err := s.packages.FindByName(ctx, parsed.Name); err == nil {
		if v, err := s.packages.FindLatestVersion(ctx, pkg.ID); err == nil {
			var prev parsedPackage
			if json.Unmarshal([]byte(v.Payload), &prev) == nil {
				ownedTopics, ownedRooms = map[string]bool{}, map[string]bool{}
				for _, t := range prev.Topics {
					ownedTopics[t.Slug] = true
				}
				for _, r := range prev.Rooms {
					ownedRooms[r.Slug] = true
				}
			}
		}
	}
	s.setOwnedSlugs(ownedTopics, ownedRooms)
	defer s.setOwnedSlugs(nil, nil) // 复原，避免泄漏到下次调用
	if err := s.validate(ctx, parsed); err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	payload, err := json.Marshal(parsed)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	// 同名包重传：复用既有包记录，追加新 ContentVersion，状态回到 draft（新版本待发布）。
	pkg, err := s.packages.FindByName(ctx, parsed.Name)
	if err != nil {
		pkg = &model.ContentPackage{
			Name:          parsed.Name,
			LatestVersion: parsed.Version,
			Status:        model.PackageDraft,
		}
		if err := s.packages.Create(ctx, pkg); err != nil {
			return nil, err
		}
	} else {
		if err := s.packages.UpdateLatestVersion(ctx, pkg.ID, parsed.Version); err != nil {
			return nil, err
		}
		if err := s.packages.UpdateStatus(ctx, pkg.ID, model.PackageDraft); err != nil {
			return nil, err
		}
	}
	if err := s.packages.AddVersion(ctx, &model.ContentVersion{
		PackageID: pkg.ID, Version: parsed.Version,
		Status: model.PackageDraft, Checksum: hex.EncodeToString(sum[:]),
		Payload: string(payload),
	}); err != nil {
		return nil, err
	}
	return s.packages.FindByID(ctx, pkg.ID)
}

// parseZip 管线第 2 步：解压并解析 manifest/topics/rooms/lab.yaml。
func (s *ContentService) parseZip(data []byte) (*parsedPackage, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	// 全部条目读入内存（先整体解析，失败不入库）。
	rawFiles := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		content, err := readZipFile(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		rawFiles[f.Name] = content
	}
	files, err := normalizeZipEntries(rawFiles)
	if err != nil {
		return nil, err
	}
	raw, ok := files["manifest.yaml"]
	if !ok {
		return nil, errors.New("manifest.yaml is required")
	}
	var m manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest.yaml: %w", err)
	}
	parsed := &parsedPackage{
		Name: m.Name, Version: m.Version,
		Topics: []model.Topic{}, Rooms: []roomPayload{}, Labs: map[string]labDef{},
	}
	for _, topicSlug := range m.Topics {
		topic, err := s.parseTopic(topicSlug, files)
		if err != nil {
			return nil, err
		}
		parsed.Topics = append(parsed.Topics, *topic)
		rooms, err := s.parseRooms(topicSlug, files, parsed)
		if err != nil {
			return nil, err
		}
		parsed.Rooms = append(parsed.Rooms, rooms...)
	}
	return parsed, nil
}

// parseTopic 解析 topics/<slug>/topic.md（front matter 必填）。
func (s *ContentService) parseTopic(topicSlug string, files map[string][]byte) (*model.Topic, error) {
	raw, ok := files[fmt.Sprintf("topics/%s/topic.md", topicSlug)]
	if !ok {
		return nil, fmt.Errorf("topic %s: topic.md is required", topicSlug)
	}
	front, _, err := splitFrontMatter(raw)
	if err != nil {
		return nil, fmt.Errorf("topic %s: %w", topicSlug, err)
	}
	var tf topicFront
	if err := yaml.Unmarshal(front, &tf); err != nil {
		return nil, fmt.Errorf("topic %s: %w", topicSlug, err)
	}
	return &model.Topic{Slug: topicSlug, Title: tf.Title}, nil
}

// parseRooms 解析 topics/<slug>/rooms/<room>/lesson.md 与可选 lab.yaml。
func (s *ContentService) parseRooms(topicSlug string, files map[string][]byte, parsed *parsedPackage) ([]roomPayload, error) {
	prefix := fmt.Sprintf("topics/%s/rooms/", topicSlug)
	// 收集 room slug（lesson.md 所在目录）。
	roomSlugs := map[string]bool{}
	for name := range files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		if strings.HasSuffix(rest, "/lesson.md") {
			roomSlugs[strings.TrimSuffix(rest, "/lesson.md")] = true
		}
	}
	rooms := make([]roomPayload, 0, len(roomSlugs))
	for slug := range roomSlugs {
		raw, ok := files[prefix+slug+"/lesson.md"]
		if !ok {
			return nil, fmt.Errorf("room %s: lesson.md is required", slug)
		}
		front, body, err := splitFrontMatter(raw)
		if err != nil {
			return nil, fmt.Errorf("room %s: %w", slug, err)
		}
		var lf lessonFront
		if err := yaml.Unmarshal(front, &lf); err != nil {
			return nil, fmt.Errorf("room %s: %w", slug, err)
		}
		rooms = append(rooms, roomPayload{
			TopicSlug: topicSlug, Slug: slug,
			Title: lf.Title, Difficulty: lf.Difficulty, LessonMD: string(body),
			VideoURL: lf.Video,
		})
	}
	return rooms, nil
}

// splitFrontMatter 拆分 `---` yaml front matter 与 Markdown 正文。
func splitFrontMatter(raw []byte) (front, body []byte, err error) {
	text := string(raw)
	if !strings.HasPrefix(text, "---") {
		return nil, nil, errors.New("front matter is required")
	}
	rest := text[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, nil, errors.New("unterminated front matter")
	}
	return []byte(strings.TrimSpace(rest[:end])), []byte(strings.TrimSpace(rest[end+4:])), nil
}

// validate 管线第 3 步：字段与冲突校验。
func (s *ContentService) validate(ctx context.Context, p *parsedPackage) error {
	if p.Name == "" {
		return errors.New("manifest name is required")
	}
	if p.Version == "" {
		return errors.New("manifest version is required")
	}
	if len(p.Topics) == 0 {
		return errors.New("manifest topics is required")
	}
	seenTopics := map[string]bool{}
	for _, t := range p.Topics {
		if !slugPattern.MatchString(t.Slug) {
			return fmt.Errorf("topic slug %q: must match [a-z0-9-]+", t.Slug)
		}
		if seenTopics[t.Slug] {
			return fmt.Errorf("duplicate topic slug %q", t.Slug)
		}
		seenTopics[t.Slug] = true
		if t.Title == "" {
			return fmt.Errorf("topic %s: title is required", t.Slug)
		}
		exists, err := s.topics.ExistsBySlug(ctx, t.Slug)
		if err != nil {
			return err
		}
		if exists && !s.ownedTopics[t.Slug] {
			return fmt.Errorf("topic slug %q already exists", t.Slug)
		}
	}
	seenRooms := map[string]bool{}
	for _, r := range p.Rooms {
		if !slugPattern.MatchString(r.Slug) {
			return fmt.Errorf("room slug %q: must match [a-z0-9-]+", r.Slug)
		}
		if seenRooms[r.Slug] {
			return fmt.Errorf("duplicate room slug %q", r.Slug)
		}
		seenRooms[r.Slug] = true
		if r.Title == "" {
			return fmt.Errorf("room %s: title is required", r.Slug)
		}
		if err := validateDifficulty(r.Difficulty); err != nil {
			return fmt.Errorf("room %s: %w", r.Slug, err)
		}
		exists, err := s.rooms.ExistsBySlug(ctx, r.Slug)
		if err != nil {
			return err
		}
		if exists && !s.ownedRooms[r.Slug] {
			return fmt.Errorf("room slug %q already exists", r.Slug)
		}
	}
	return nil
}

func validateDifficulty(d model.Difficulty) error {
	switch d {
	case model.DifficultyApprentice, model.DifficultyPractitioner, model.DifficultyExpert:
		return nil
	}
	return fmt.Errorf("invalid difficulty %q (apprentice/practitioner/expert)", d)
}

// ListPackages 包列表（分页）。
func (s *ContentService) ListPackages(ctx context.Context, offset, limit int) ([]model.ContentPackage, int64, error) {
	return s.packages.List(ctx, offset, limit)
}

// GetPackage 包详情（状态/版本/管线错误信息）。
func (s *ContentService) GetPackage(ctx context.Context, id uint) (*model.ContentPackage, error) {
	return s.packages.FindByID(ctx, id)
}

// Publish 管线第 5 步：draft → published，发布时才把内容写入 Topic/Room/Challenge。
// 顺序：先写内容表，成功后才改状态——失败保持 draft 可重试。
// 版本级状态机：目标版本 → published，包内其余版本 → archived。
func (s *ContentService) Publish(ctx context.Context, packageID uint) (*model.ContentPackage, error) {
	pkg, err := s.packages.FindByID(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if pkg.Status != model.PackageDraft {
		return nil, fmt.Errorf("invalid state transition: %s", pkg.Status)
	}
	v, err := s.packages.FindLatestVersion(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if err := s.applyPayload(ctx, v.Payload); err != nil {
		return nil, err
	}
	if err := s.packages.UpdateVersionStatus(ctx, v.ID, model.PackagePublished); err != nil {
		return nil, err
	}
	if err := s.packages.ArchiveOtherVersions(ctx, packageID, v.ID); err != nil {
		return nil, err
	}
	if err := s.packages.UpdateStatus(ctx, packageID, model.PackagePublished); err != nil {
		return nil, err
	}
	return s.packages.FindByID(ctx, packageID)
}

// Archive 归档：published → archived（包级冻结，内容表不动；学生侧继续可见）。
func (s *ContentService) Archive(ctx context.Context, packageID uint) (*model.ContentPackage, error) {
	pkg, err := s.packages.FindByID(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if pkg.Status != model.PackagePublished {
		return nil, fmt.Errorf("%w: archive requires published package, got %s", ErrValidation, pkg.Status)
	}
	if err := s.packages.UpdateStatus(ctx, packageID, model.PackageArchived); err != nil {
		return nil, err
	}
	return s.packages.FindByID(ctx, packageID)
}

// Rollback 回滚：published 包回退到包内某个 archived 版本——目标版本 Payload
// 重写内容表（覆盖式，复用发布写入逻辑），目标 → published、当前 → archived，
// LatestVersion 更新。禁止回滚到当前版本；draft 新版本应走发布而非回滚。
func (s *ContentService) Rollback(ctx context.Context, packageID, versionID uint) (*model.ContentPackage, error) {
	pkg, err := s.packages.FindByID(ctx, packageID)
	if err != nil {
		return nil, err
	}
	if pkg.Status != model.PackagePublished {
		return nil, fmt.Errorf("%w: rollback requires published package, got %s", ErrValidation, pkg.Status)
	}
	target, err := s.packages.FindVersionByID(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if target.PackageID != packageID {
		return nil, fmt.Errorf("%w: version %d does not belong to package %d", ErrNotFound, versionID, packageID)
	}
	if target.Status != model.PackageArchived {
		return nil, fmt.Errorf("%w: rollback target must be an archived version, got %s", ErrValidation, target.Status)
	}
	if err := s.applyPayload(ctx, target.Payload); err != nil {
		return nil, err
	}
	if err := s.packages.UpdateVersionStatus(ctx, target.ID, model.PackagePublished); err != nil {
		return nil, err
	}
	if err := s.packages.ArchiveOtherVersions(ctx, packageID, target.ID); err != nil {
		return nil, err
	}
	if err := s.packages.UpdateLatestVersion(ctx, packageID, target.Version); err != nil {
		return nil, err
	}
	return s.packages.FindByID(ctx, packageID)
}

// ListVersions 包的版本列表（含各版本状态，回滚选择用）。
func (s *ContentService) ListVersions(ctx context.Context, packageID uint) ([]model.ContentVersion, error) {
	if _, err := s.packages.FindByID(ctx, packageID); err != nil {
		return nil, err
	}
	return s.packages.ListVersions(ctx, packageID)
}

// applyPayload 把版本 Payload 写入内容表（Publish 与 Rollback 共用的覆盖式写入）。
// 题目已是独立资源：旧版本 Payload 中的 Labs 段忽略不写（兼容历史包回放）。
func (s *ContentService) applyPayload(ctx context.Context, payload string) error {
	var parsed parsedPackage
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return fmt.Errorf("payload: %w", err)
	}
	for i := range parsed.Topics {
		if err := s.topics.Upsert(ctx, &parsed.Topics[i]); err != nil {
			return err
		}
	}
	for i := range parsed.Rooms {
		rp := parsed.Rooms[i]
		if err := s.rooms.Upsert(ctx, &model.Room{
			Slug: rp.Slug, TopicSlug: rp.TopicSlug, Title: rp.Title,
			Difficulty: rp.Difficulty, LessonMD: rp.LessonMD, VideoURL: rp.VideoURL,
		}); err != nil {
			return err
		}
	}
	return nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// challengeJSON 管理员上传的题目定义固定格式（单对象或对象数组）。
type challengeJSON struct {
	Slug          string           `json:"slug"`
	Title         string           `json:"title"`
	Category      string           `json:"category"`
	Difficulty    model.Difficulty `json:"difficulty"`
	DescriptionMD string           `json:"description_md"`
	Score         int              `json:"score"`
	FlagMode      model.FlagMode   `json:"flag_mode"`
	Challenge     struct {
		Type model.ChallengeType `json:"type"`
		Flag string             `json:"flag"`
	} `json:"challenge"`
	Env struct {
		Image string `json:"image"`
		Port  int    `json:"port"`
	} `json:"env"`
	Hints []struct {
		Title   string `json:"title"`
		Content string `json:"content"`
		Cost    int    `json:"cost"`
	} `json:"hints"`
	// Links 题目外链（配套工具/参考资料），存 JSON 列。
	Links []model.ChallengeLink `json:"links"`
	TutorialSlug string `json:"tutorial_slug"`
}

// UploadChallenges 解析并导入题目定义 JSON（单对象或数组）。
// 逐题校验、整体拒绝（单题失败全部不入库）；slug 冲突视为 upsert（重传更新）。
func (s *ContentService) UploadChallenges(ctx context.Context, raw []byte) (imported int, slugs []string, err error) {
	var single challengeJSON
	var batch []challengeJSON
	if json.Unmarshal(raw, &single) == nil && single.Slug != "" {
		batch = []challengeJSON{single}
	} else if err := json.Unmarshal(raw, &batch); err != nil {
		return 0, nil, fmt.Errorf("%w: body must be a challenge object or array", ErrValidation)
	}
	if len(batch) == 0 {
		return 0, nil, fmt.Errorf("%w: empty challenge list", ErrValidation)
	}
	// 先整体校验，再逐题 upsert（失败前置，避免部分入库）。
	seen := map[string]bool{}
	for i, c := range batch {
		if err := s.validateChallengeJSON(i, c, seen); err != nil {
			return 0, nil, err
		}
	}
	for _, c := range batch {
		hints := make([]model.Hint, 0, len(c.Hints))
		for _, h := range c.Hints {
			hints = append(hints, model.Hint{Title: h.Title, Content: h.Content, Cost: h.Cost})
		}
		chal := &model.Challenge{
			Slug: c.Slug, Title: c.Title, Category: c.Category, Difficulty: c.Difficulty,
			Description: c.DescriptionMD, Type: c.Challenge.Type, Score: c.Score,
			FlagMode: resolveFlagMode(c.FlagMode), Image: c.Env.Image, Port: c.Env.Port,
			Links: linksJSON(c.Links), TutorialSlug: c.TutorialSlug,
		}
		flags := []model.Flag{{Type: c.Challenge.Type, Value: c.Challenge.Flag}}
		if err := s.challenge.UpsertChallenge(ctx, chal, flags, hints); err != nil {
			return 0, nil, err
		}
		slugs = append(slugs, c.Slug)
	}
	return len(batch), slugs, nil
}

// validateChallengeJSON 单题字段校验（枚举/slug/flag/env 成对/hint cost）。
func (s *ContentService) validateChallengeJSON(i int, c challengeJSON, seen map[string]bool) error {
	where := fmt.Sprintf("challenge[%d]", i)
	if c.Slug == "" {
		return fmt.Errorf("%w: %s: slug is required", ErrValidation, where)
	}
	if !slugPattern.MatchString(c.Slug) {
		return fmt.Errorf("%w: %s: slug must match [a-z0-9-]+", ErrValidation, where)
	}
	if seen[c.Slug] {
		return fmt.Errorf("%w: %s: duplicate slug %q", ErrValidation, where, c.Slug)
	}
	seen[c.Slug] = true
	if c.Title == "" {
		return fmt.Errorf("%w: %s: title is required", ErrValidation, where)
	}
	if c.Category == "" {
		return fmt.Errorf("%w: %s: category is required", ErrValidation, where)
	}
	if !slugPattern.MatchString(c.Category) {
		return fmt.Errorf("%w: %s: category must match [a-z0-9-]+", ErrValidation, where)
	}
	if err := validateDifficulty(c.Difficulty); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrValidation, where, err)
	}
	if c.Score < 0 {
		return fmt.Errorf("%w: %s: score must be non-negative", ErrValidation, where)
	}
	switch c.Challenge.Type {
	case model.ChallengeStatic:
	case model.ChallengeRegex:
		if _, err := regexp.Compile(c.Challenge.Flag); err != nil {
			return fmt.Errorf("%w: %s: invalid regex flag: %v", ErrValidation, where, err)
		}
	default:
		return fmt.Errorf("%w: %s: challenge.type must be static or regex", ErrValidation, where)
	}
	if c.Challenge.Flag == "" {
		return fmt.Errorf("%w: %s: challenge.flag is required", ErrValidation, where)
	}
	mode := resolveFlagMode(c.FlagMode)
	if c.FlagMode != "" && mode != model.FlagModeShared && mode != model.FlagModeInstance {
		return fmt.Errorf("%w: %s: flag_mode must be shared or instance", ErrValidation, where)
	}
	// env 段可选；出现则 image 必填、port 合法（1-65535）——与教程包 env 规则一致。
	if c.Env.Image != "" || c.Env.Port != 0 {
		if c.Env.Image == "" {
			return fmt.Errorf("%w: %s: env.image is required when env is present", ErrValidation, where)
		}
		if c.Env.Port < 1 || c.Env.Port > 65535 {
			return fmt.Errorf("%w: %s: env.port must be 1-65535", ErrValidation, where)
		}
	}
	if mode == model.FlagModeInstance {
		if c.Env.Image == "" || c.Env.Port < 1 {
			return fmt.Errorf("%w: %s: flag_mode=instance requires env.image and env.port", ErrValidation, where)
		}
		if !strings.Contains(c.Challenge.Flag, "{token}") {
			return fmt.Errorf("%w: %s: instance flag template must contain {token}", ErrValidation, where)
		}
	}
	for _, h := range c.Hints {
		if h.Cost < 0 {
			return fmt.Errorf("%w: %s: hint cost must be non-negative", ErrValidation, where)
		}
	}
	for i, l := range c.Links {
		if l.Name == "" {
			return fmt.Errorf("%w: %s: links[%d].name is required", ErrValidation, where, i)
		}
		if !strings.HasPrefix(l.URL, "http://") && !strings.HasPrefix(l.URL, "https://") {
			return fmt.Errorf("%w: %s: links[%d].url must be http(s)", ErrValidation, where, i)
		}
	}
	return nil
}

// linksJSON 外链列表序列化为 Challenge.Links JSON 字符串列；空列表存空串。
func linksJSON(links []model.ChallengeLink) string {
	if len(links) == 0 {
		return ""
	}
	b, err := json.Marshal(links)
	if err != nil {
		return ""
	}
	return string(b)
}

func resolveFlagMode(m model.FlagMode) model.FlagMode {
	if m == "" {
		return model.FlagModeShared
	}
	return m
}

// DeleteChallenge 删除题目：存在通关记录拒绝（409）；
// 否则先销毁全部运行中容器，再级联删 flags/hints/lab_instances 行。
// 提交事件历史 append-only 保留（event-sourcing-rules skill），题目行删除后
// 历史 submission.challenge_id 悬挂属预期——重放派生只按事件流进行，不再读题。
func (s *ChallengeService) DeleteChallenge(ctx context.Context, slug string) error {
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return err
	}
	if n, err := s.challenges.CountSolves(ctx, chal.ID); err != nil {
		return err
	} else if n > 0 {
		return ErrHasSolves
	}
	if s.env != nil {
		instances, err := s.env.ListActiveByChallenge(ctx, chal.ID)
		if err != nil {
			return err
		}
		for _, inst := range instances {
			if err := s.env.AdminDestroyInstance(ctx, inst.ID); err != nil {
				return fmt.Errorf("destroy instance %d: %w", inst.ID, err)
			}
		}
	}
	// 级联删附件：先清元数据行，再删磁盘目录（目录不存在不算错）。
	if err := s.DeleteChallengeAttachments(ctx, slug); err != nil {
		return err
	}
	return s.challenges.Delete(ctx, slug)
}

// challengeYML 实训常用 challenge.yml（扩展 difficulty/port/tutorial_slug/slug/flag_mode）。
// 容器题必须同时提供 Dockerfile 与 docker-compose.yml（或 compose.yml）。
type challengeYML struct {
	Slug          string           `yaml:"slug"`
	Name          string           `yaml:"name"`
	Title         string           `yaml:"title"` // 兼容 name/title
	Category      string           `yaml:"category"`
	Difficulty    model.Difficulty `yaml:"difficulty"`
	Description   string           `yaml:"description"`
	DescriptionMD string           `yaml:"description_md"`
	Value         int              `yaml:"value"`
	Score         int              `yaml:"score"`
	Type          string           `yaml:"type"` // static | regex（flag 类型）
	FlagMode      model.FlagMode   `yaml:"flag_mode"`
	Image         string           `yaml:"image"`
	Port          int              `yaml:"port"`
	Protocol      string           `yaml:"protocol"`
	Links         []model.ChallengeLink `yaml:"links"`
	Flags         []challengeYMLFlag `yaml:"flags"`
	Hints         []struct {
		Title   string `yaml:"title"`
		Content string `yaml:"content"`
		Cost    int    `yaml:"cost"`
	} `yaml:"hints"`
	TutorialSlug string `yaml:"tutorial_slug"`
}

type challengeYMLFlag struct {
	Type    string // 解析时填充
	Content string
}

func (f *challengeYMLFlag) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err == nil {
		f.Type = string(model.ChallengeStatic)
		f.Content = s
		return nil
	}
	var obj struct {
		Type    string `yaml:"type"`
		Content string `yaml:"content"`
	}
	if err := n.Decode(&obj); err != nil {
		return err
	}
	f.Type = obj.Type
	if f.Type == "" {
		f.Type = string(model.ChallengeStatic)
	}
	f.Content = obj.Content
	return nil
}

// UploadChallengePackage 解析实训常用题目包 zip：必含 challenge.yml；
// 声明容器（image 非空）时必含 Dockerfile + docker-compose.yml（或 compose.yml）；
// image: . 时先构建镜像再 upsert（构建失败 422，禁止悬空 tag）。
func (s *ContentService) UploadChallengePackage(ctx context.Context, zipBytes []byte) (imported int, slugs []string, err error) {
	files, err := unzipToMap(zipBytes)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	files, err = normalizeZipEntries(files)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	ymlRaw, ok := files["challenge.yml"]
	if !ok {
		ymlRaw, ok = files["challenge.yaml"]
	}
	if !ok {
		return 0, nil, fmt.Errorf("%w: challenge.yml is required in package", ErrValidation)
	}
	var yml challengeYML
	if err := yaml.Unmarshal(ymlRaw, &yml); err != nil {
		return 0, nil, fmt.Errorf("%w: invalid challenge.yml: %v", ErrValidation, err)
	}
	cj, err := challengeYMLToJSON(yml)
	if err != nil {
		return 0, nil, err
	}
	buildLocal := false
	// 容器题：要求 Dockerfile + compose。
	if cj.Env.Image != "" {
		if _, hasDocker := files["Dockerfile"]; !hasDocker {
			return 0, nil, fmt.Errorf("%w: Dockerfile is required when challenge declares a container image", ErrValidation)
		}
		_, hasCompose := files["docker-compose.yml"]
		if !hasCompose {
			_, hasCompose = files["compose.yml"]
		}
		if !hasCompose {
			return 0, nil, fmt.Errorf("%w: docker-compose.yml (or compose.yml) is required when challenge declares a container image", ErrValidation)
		}
		// image: . → 本地构建约定，落库为 yak-academy/<slug>:latest（env 驱动按 tag 拉起）。
		if cj.Env.Image == "." {
			buildLocal = true
			cj.Env.Image = "yak-academy/" + cj.Slug + ":latest"
		}
	}
	seen := map[string]bool{}
	if err := s.validateChallengeJSON(0, cj, seen); err != nil {
		return 0, nil, err
	}
	if buildLocal {
		if err := s.buildChallengeImage(ctx, cj.Env.Image, files); err != nil {
			return 0, nil, err
		}
	}
	hints := make([]model.Hint, 0, len(cj.Hints))
	for _, h := range cj.Hints {
		hints = append(hints, model.Hint{Title: h.Title, Content: h.Content, Cost: h.Cost})
	}
	chal := &model.Challenge{
		Slug: cj.Slug, Title: cj.Title, Category: cj.Category, Difficulty: cj.Difficulty,
		Description: cj.DescriptionMD, Type: cj.Challenge.Type, Score: cj.Score,
		FlagMode: resolveFlagMode(cj.FlagMode), Image: cj.Env.Image, Port: cj.Env.Port,
		Links: linksJSON(cj.Links), TutorialSlug: cj.TutorialSlug,
	}
	flags := []model.Flag{{Type: cj.Challenge.Type, Value: cj.Challenge.Flag}}
	if err := s.challenge.UpsertChallenge(ctx, chal, flags, hints); err != nil {
		return 0, nil, err
	}
	return 1, []string{cj.Slug}, nil
}

// buildChallengeImage 将包内文件写到临时目录后调用 ImageBuilder。
func (s *ContentService) buildChallengeImage(ctx context.Context, tag string, files map[string][]byte) error {
	dir, err := os.MkdirTemp("", "yak-chal-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name, content := range files {
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, content, 0o644); err != nil {
			return err
		}
	}
	if err := s.builder.Build(ctx, tag, dir); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

func challengeYMLToJSON(y challengeYML) (challengeJSON, error) {
	var out challengeJSON
	out.Slug = y.Slug
	out.Title = y.Title
	if out.Title == "" {
		out.Title = y.Name
	}
	out.Category = y.Category
	out.Difficulty = y.Difficulty
	out.DescriptionMD = y.DescriptionMD
	if out.DescriptionMD == "" {
		out.DescriptionMD = y.Description
	}
	out.Score = y.Score
	if out.Score == 0 {
		out.Score = y.Value
	}
	out.FlagMode = y.FlagMode
	out.TutorialSlug = y.TutorialSlug
	out.Links = y.Links
	flagType := model.ChallengeType(y.Type)
	if flagType == "" {
		flagType = model.ChallengeStatic
	}
	if len(y.Flags) == 0 {
		return out, fmt.Errorf("%w: flags required in challenge.yml", ErrValidation)
	}
	// 首个 flag 作为判定答案（v1 单 flag）。
	f := y.Flags[0]
	if f.Type != "" {
		flagType = model.ChallengeType(f.Type)
	}
	out.Challenge.Type = flagType
	out.Challenge.Flag = f.Content
	if y.Image != "" && y.Image != "null" {
		out.Env.Image = y.Image
		out.Env.Port = y.Port
		if out.Env.Port == 0 {
			out.Env.Port = 80
		}
	}
	for _, h := range y.Hints {
		content := h.Content
		title := h.Title
		if title == "" {
			title = "提示"
		}
		out.Hints = append(out.Hints, struct {
			Title   string `json:"title"`
			Content string `json:"content"`
			Cost    int    `json:"cost"`
		}{Title: title, Content: content, Cost: h.Cost})
	}
	return out, nil
}

// unzipToMap 解压 zip 为 path→content（与教程包共用条目规范化）。
func unzipToMap(zipBytes []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 8<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		files[f.Name] = data
	}
	return files, nil
}

// normalizeZipEntries 条目路径规范化：
//   - 分隔符统一 `\` → `/`，剥 `./` 前缀与首部 `/`；
//   - 忽略 `__MACOSX/` 垃圾条目（Mac 打包工具产物）；
//   - 全部条目共享唯一首段时视为顶层包裹目录，整体剥掉
//     （多顶层 = 平铺结构，不剥）；
//   - 结果中禁止 `..` 与绝对路径（zip slip 防护）。
func normalizeZipEntries(raw map[string][]byte) (map[string][]byte, error) {
	cleaned := make(map[string][]byte, len(raw))
	firsts := map[string]bool{}
	for name, content := range raw {
		p := strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/")
		p = strings.TrimPrefix(p, "./")
		if p == "" || strings.HasPrefix(p, "__MACOSX/") || strings.HasPrefix(p, "__MACOSX") {
			continue
		}
		cleaned[p] = content
		if seg := strings.SplitN(p, "/", 2)[0]; seg != "" {
			firsts[seg] = true
		}
	}
	prefix := ""
	if len(firsts) == 1 {
		// 唯一首段且非已知内容根（manifest/topics），判定为包裹目录。
		for seg := range firsts {
			if seg != "topics" {
				prefix = seg + "/"
			}
		}
	}
	files := make(map[string][]byte, len(cleaned))
	for p, content := range cleaned {
		name := p
		if prefix != "" && strings.HasPrefix(p, prefix) {
			name = strings.TrimPrefix(p, prefix)
		}
		if name == "" {
			continue
		}
		// 防路径穿越（zip slip）。
		if strings.HasPrefix(name, "../") || path.IsAbs(name) {
			return nil, fmt.Errorf("illegal entry path: %s", p)
		}
		files[name] = content
	}
	return files, nil
}