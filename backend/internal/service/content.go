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
	"path"
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
}

func NewContentService(p repo.ContentPackageRepo, t repo.TopicRepo, r repo.RoomRepo, c *ChallengeService) *ContentService {
	return &ContentService{packages: p, topics: t, rooms: r, challenge: c}
}

// parsedPackage 管线 Parse 步骤的内存中间产物，序列化后存 ContentVersion.Payload。
type parsedPackage struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Topics  []model.Topic `json:"topics"`
	// Rooms 用 DTO 而非 model.Room：model.Room.LessonMD 是 json:"-"（详情接口单独返回），
	// 直接序列化会丢正文。
	Rooms []roomPayload `json:"rooms"`
	// Labs: room slug → 题目定义（教题一体：Lab 必属某 Room）。
	Labs map[string]labDef `json:"labs"`
}

// roomPayload Payload 中的 Room 序列化形态（补回 LessonMD）。
type roomPayload struct {
	Slug       string           `json:"slug"`
	TopicSlug  string           `json:"topic_slug"`
	Title      string           `json:"title"`
	Difficulty model.Difficulty `json:"difficulty"`
	LessonMD   string           `json:"lesson_md"`
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
type lessonFront struct {
	Title     string           `yaml:"title"`
	Difficulty model.Difficulty `yaml:"difficulty"`
}

// labYaml lab.yaml schema（content-package-spec skill：title/challenge.type/challenge.flag/score 必填）。
type labYaml struct {
	Title     string `yaml:"title"`
	Challenge struct {
		Type model.ChallengeType `yaml:"type"`
		Flag string             `yaml:"flag"`
	} `yaml:"challenge"`
	Score int `yaml:"score"`
	// Env 可选在线环境（容器驱动用）：image + port。
	Env struct {
		Image string `yaml:"image"`
		Port  int    `yaml:"port"`
	} `yaml:"env"`
	Hints []struct {
		Title   string `yaml:"title"`
		Content string `yaml:"content"`
		Cost    int    `yaml:"cost"`
	} `yaml:"hints"`
}

var slugPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Upload 处理上传的 zip：执行 Upload/Parse/Validate 三步并持久化为 draft。
// 解析/校验失败整体拒绝，包与版本均不入库。
func (s *ContentService) Upload(ctx context.Context, data []byte) (*model.ContentPackage, error) {
	parsed, err := s.parseZip(data)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
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
		})
		// 可选 lab.yaml（至多一个内嵌 Lab，teaching-integration skill）。
		if labRaw, ok := files[prefix+slug+"/lab.yaml"]; ok {
			lab, err := parseLab(labRaw)
			if err != nil {
				return nil, fmt.Errorf("room %s: %w", slug, err)
			}
			def := labDef{
				Title: lab.Title, Type: lab.Challenge.Type, Flag: lab.Challenge.Flag,
				Score: lab.Score, Image: lab.Env.Image, Port: lab.Env.Port,
			}
			for _, h := range lab.Hints {
				def.Hints = append(def.Hints, model.Hint{Title: h.Title, Content: h.Content, Cost: h.Cost})
			}
			parsed.Labs[slug] = def
		}
	}
	return rooms, nil
}

func parseLab(raw []byte) (*labYaml, error) {
	var lab labYaml
	if err := yaml.Unmarshal(raw, &lab); err != nil {
		return nil, err
	}
	return &lab, nil
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
		if exists {
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
		if exists {
			return fmt.Errorf("room slug %q already exists", r.Slug)
		}
	}
	for slug, lab := range p.Labs {
		if lab.Title == "" {
			return fmt.Errorf("lab %s: title is required", slug)
		}
		switch lab.Type {
		case model.ChallengeStatic:
		case model.ChallengeRegex:
			if _, err := regexp.Compile(lab.Flag); err != nil {
				return fmt.Errorf("lab %s: invalid regex flag: %w", slug, err)
			}
		default:
			return fmt.Errorf("lab %s: challenge type must be static or regex", slug)
		}
		if lab.Score < 0 {
			return fmt.Errorf("lab %s: score must be non-negative", slug)
		}
		// env 段可选；出现则 image 必填、port 合法（1-65535）。
		if lab.Image != "" || lab.Port != 0 {
			if lab.Image == "" {
				return fmt.Errorf("lab %s: env.image is required when env is present", slug)
			}
			if lab.Port < 1 || lab.Port > 65535 {
				return fmt.Errorf("lab %s: env.port must be 1-65535", slug)
			}
		}
		for _, h := range lab.Hints {
			if h.Cost < 0 {
				return fmt.Errorf("lab %s: hint cost must be non-negative", slug)
			}
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
	var parsed parsedPackage
	if err := json.Unmarshal([]byte(v.Payload), &parsed); err != nil {
		return nil, fmt.Errorf("payload: %w", err)
	}
	for i := range parsed.Topics {
		if err := s.topics.Upsert(ctx, &parsed.Topics[i]); err != nil {
			return nil, err
		}
	}
	for i := range parsed.Rooms {
		rp := parsed.Rooms[i]
		if err := s.rooms.Upsert(ctx, &model.Room{
			Slug: rp.Slug, TopicSlug: rp.TopicSlug, Title: rp.Title,
			Difficulty: rp.Difficulty, LessonMD: rp.LessonMD,
		}); err != nil {
			return nil, err
		}
	}
	for roomSlug, lab := range parsed.Labs {
		hints := lab.Hints
		if hints == nil {
			hints = []model.Hint{}
		}
		chal := &model.Challenge{
			RoomSlug: roomSlug, Title: lab.Title, Type: lab.Type, Score: lab.Score,
			Image: lab.Image, Port: lab.Port,
		}
		flags := []model.Flag{{Type: lab.Type, Value: lab.Flag}}
		if err := s.challenge.UpsertChallenge(ctx, chal, flags, hints); err != nil {
			return nil, err
		}
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