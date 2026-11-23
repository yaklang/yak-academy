package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// MaxWriteupZipBytes WP 附带 zip 上限 20MB（脚本+截图打包；导出供 api 层
// 设置 MaxBytesReader，multipart 头部余量在 api 层另加）。
const MaxWriteupZipBytes = 20 << 20

// CompetitionService 竞赛域业务：竞赛 CRUD、题目绑定、参赛者/队伍批量导入、
// 邀请邮件投递、入场与锁定、排行榜聚合、WP 提交。
// 判定与竞赛积分派生走 judge/deriver（event-sourcing-rules skill），
// 本服务不触碰派生表写入。
type CompetitionService struct {
	comps       repo.CompetitionRepo
	challenges  repo.ChallengeRepo
	users       repo.UserRepo
	instances   repo.LabInstanceRepo
	submissions repo.SubmissionRepo
	// writeups WP zip 附件存储（SetWriteupStore/SetWriteupDir 注入；测试用
	// SetWriteupDir 指向 t.TempDir 的 fs 后端，或 SetWriteupStore 换 fake）。
	writeups      WriteupStore
	deliverInvite func(ctx context.Context, userID uint, title, bodyMd string, competitionID uint, inviteCode string) error
	createAccount func(ctx context.Context, username, password string, role model.GlobalRole) (*model.User, error)
	// destroyInstances 竞赛删除前销毁该赛运行容器（SetInstanceDestroyer 注入）。
	destroyInstances instanceDestroyer
	// now 时钟注入（测试固定时间推进竞赛状态）。
	now func() time.Time
}

func NewCompetitionService(
	comps repo.CompetitionRepo,
	challenges repo.ChallengeRepo,
	users repo.UserRepo,
	instances repo.LabInstanceRepo,
	submissions repo.SubmissionRepo,
) *CompetitionService {
	return &CompetitionService{
		comps:       comps,
		challenges:  challenges,
		users:       users,
		instances:   instances,
		submissions: submissions,
		now:         time.Now,
	}
}

// SetWriteupStore 注入 WP zip 附件存储后端（fs / s3，main 装配后调用）。
func (s *CompetitionService) SetWriteupStore(store WriteupStore) {
	s.writeups = store
}

// SetWriteupDir 注入 fs 后端并指定落盘根目录（测试便捷入口）。
func (s *CompetitionService) SetWriteupDir(dir string) {
	s.writeups = NewFSWriteupStore(dir)
}

// SetDelivery 注入站内信投递与账号创建入口（main 装配后调用一次）。
// 用函数注入避免 service 横向 import（project-conventions 白名单外）。
func (s *CompetitionService) SetDelivery(
	deliverInvite func(ctx context.Context, userID uint, title, bodyMd string, competitionID uint, inviteCode string) error,
	createAccount func(ctx context.Context, username, password string, role model.GlobalRole) (*model.User, error),
) {
	s.deliverInvite = deliverInvite
	s.createAccount = createAccount
}

// ---------- 管理端：竞赛 CRUD ----------

// CompetitionView 竞赛对外视图（含派生 status）。
type CompetitionView struct {
	model.Competition
	Status model.CompetitionStatus `json:"status"`
}

func toView(c *model.Competition, now time.Time) CompetitionView {
	return CompetitionView{Competition: *c, Status: c.Status(now)}
}

// CompetitionCreate 创建竞赛入参：kind/format 创建后不可更改。
type CompetitionCreate struct {
	Name              string
	Description       string
	Start             time.Time
	End               time.Time
	Kind              string
	Format            string
	ScoringMode       string
	BonusCap          int
	RegistrationStart *time.Time
	RegistrationEnd   *time.Time
}

// validateRegistrationWindow 报名窗口必须成对配置、起止有序，且不得晚于
// 竞赛结束（结束后报名没有意义；未配置 = 沿用旧语义）。
// validateBonusCap 附加分上限：非负；仅 CTF 支持（AWD/考试的附加分走各自模型）。
func validateBonusCap(format model.CompetitionFormat, cap int) error {
	if cap < 0 {
		return fmt.Errorf("%w: bonus_cap cannot be negative", ErrValidation)
	}
	if cap > 0 && format != model.CompetitionFormatCTF {
		return fmt.Errorf("%w: bonus_cap is only available for ctf competitions", ErrValidation)
	}
	return nil
}

func validateRegistrationWindow(start, end *time.Time, competitionEnd time.Time) (*time.Time, *time.Time, error) {
	if start == nil && end == nil {
		return nil, nil, nil
	}
	if start == nil || end == nil {
		return nil, nil, fmt.Errorf("%w: registration_start and registration_end must be set together", ErrValidation)
	}
	s, e := start.UTC(), end.UTC()
	if !e.After(s) {
		return nil, nil, fmt.Errorf("%w: registration_end must be after registration_start", ErrValidation)
	}
	if e.After(competitionEnd.UTC()) {
		return nil, nil, fmt.Errorf("%w: registration_end must not be after end_time", ErrValidation)
	}
	return &s, &e, nil
}

func (s *CompetitionService) Create(ctx context.Context, in CompetitionCreate) (*CompetitionView, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if !in.End.After(in.Start) {
		return nil, fmt.Errorf("%w: end_time must be after start_time", ErrValidation)
	}
	k := model.CompetitionKind(strings.TrimSpace(in.Kind))
	if k == "" {
		k = model.CompetitionInvitational
	}
	if !k.Valid() {
		return nil, fmt.Errorf("%w: invalid competition kind", ErrValidation)
	}
	f := model.CompetitionFormat(strings.TrimSpace(in.Format))
	if f == "" {
		f = model.CompetitionFormatCTF
	}
	if !f.Valid() {
		return nil, fmt.Errorf("%w: invalid competition format", ErrValidation)
	}
	m := model.CompetitionScoringMode(strings.TrimSpace(in.ScoringMode))
	if m == "" {
		m = model.CompetitionScoringDynamic
	}
	if !m.Valid() {
		return nil, fmt.Errorf("%w: invalid competition scoring mode", ErrValidation)
	}
	if f != model.CompetitionFormatCTF && m != model.CompetitionScoringDynamic {
		return nil, fmt.Errorf("%w: fixed scoring is only available for ctf competitions", ErrValidation)
	}
	if err := validateBonusCap(f, in.BonusCap); err != nil {
		return nil, err
	}
	regStart, regEnd, err := validateRegistrationWindow(in.RegistrationStart, in.RegistrationEnd, in.End)
	if err != nil {
		return nil, err
	}
	// 起止时间统一以 UTC 落库：SQLite 按文本比较时间，混用时区偏移会导致
	// ActiveCompetitions 的区间判定失真（锁定/入场窗口错判）。
	// kind/format 创建后不可更改：Update 白名单不含二者，两条参赛通路
	// （报名/导入）与赛制实现的语义都以它们为前提，中途切换会让既有参赛者
	// 失去入场依据或让已有成绩按错误赛制解释。
	c := &model.Competition{
		Name: name, Description: in.Description, Kind: k, Format: f, ScoringMode: m,
		BonusCap:          in.BonusCap,
		RegistrationStart: regStart, RegistrationEnd: regEnd,
		StartTime: in.Start.UTC(), EndTime: in.End.UTC(),
	}
	if err := s.comps.Create(ctx, c); err != nil {
		return nil, mapRecordNotFound(err)
	}
	view := toView(c, s.now())
	return &view, nil
}

func (s *CompetitionService) List(ctx context.Context, page, pageSize int) ([]CompetitionView, int64, error) {
	offset := (page - 1) * pageSize
	list, total, err := s.comps.List(ctx, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}
	now := s.now()
	out := make([]CompetitionView, len(list))
	for i := range list {
		out[i] = toView(&list[i], now)
	}
	return out, total, nil
}

func (s *CompetitionService) find(ctx context.Context, id uint) (*model.Competition, error) {
	c, err := s.comps.FindByID(ctx, id)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	return c, nil
}

// CompetitionDetail 管理端详情视图：实体 + 各域计数。
type CompetitionDetail struct {
	CompetitionView
	Stats CompetitionStats `json:"stats"`
}

type CompetitionStats struct {
	Teams        int64 `json:"teams"`
	Participants int64 `json:"participants"`
	Entered      int64 `json:"entered"`
	Challenges   int   `json:"challenges"`
	Instances    int64 `json:"instances"`
	Warnings     int64 `json:"warnings"`
	Writeups     int64 `json:"writeups"`
}

func (s *CompetitionService) Detail(ctx context.Context, id uint) (*CompetitionDetail, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.buildDetail(ctx, c)
}

func (s *CompetitionService) buildDetail(ctx context.Context, c *model.Competition) (*CompetitionDetail, error) {
	participants, _, err := s.comps.ListParticipants(ctx, c.ID, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	teams, err := s.comps.ListTeams(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	var entered int64
	for _, p := range participants {
		if p.EnteredAt != nil {
			entered++
		}
	}
	bound, err := s.comps.ListBoundChallengeIDs(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	_, instTotal, err := s.instances.ListByCompetition(ctx, c.ID, 0, 1)
	if err != nil {
		return nil, err
	}
	warnings, err := s.comps.ListWarnings(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	writeups, err := s.comps.ListWriteups(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	return &CompetitionDetail{
		CompetitionView: toView(c, s.now()),
		Stats: CompetitionStats{
			Teams:        int64(len(teams)),
			Participants: int64(len(participants)),
			Entered:      entered,
			Challenges:   len(bound),
			Instances:    instTotal,
			Warnings:     int64(len(warnings)),
			Writeups:     int64(distinctWpUnits(writeups)),
		},
	}, nil
}

// Update 修改配置：nil 字段不动。running 期间 start_time 不可改（入场锁定
// 语义依赖开赛时刻）；已结束的竞赛起止时间均可重新配置（新窗口落回未来
// 即重新开赛，锁定派生自"已入场 ∧ running"，随新窗口自动恢复）。
type CompetitionUpdate struct {
	Name              *string
	Description       *string
	StartTime         *time.Time
	EndTime           *time.Time
	ScoringMode       *string
	BonusCap          *int
	RegistrationStart *time.Time
	RegistrationEnd   *time.Time
	ClearRegistration *bool
}

func (s *CompetitionService) Update(ctx context.Context, id uint, u CompetitionUpdate) (*CompetitionDetail, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.Name == nil && u.Description == nil && u.StartTime == nil && u.EndTime == nil &&
		u.ScoringMode == nil && u.BonusCap == nil && u.RegistrationStart == nil && u.RegistrationEnd == nil &&
		u.ClearRegistration == nil {
		return nil, fmt.Errorf("%w: empty competition update", ErrValidation)
	}
	fields := map[string]any{}
	if u.Name != nil {
		name := strings.TrimSpace(*u.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: name cannot be empty", ErrValidation)
		}
		fields["name"] = name
	}
	if u.Description != nil {
		fields["description"] = *u.Description
	}
	now := s.now()
	// running 期间 start_time 不动：入场锁定语义依赖开赛时刻
	// （改回过去 = 事后追溯入场资格，改到未来 = 已入场者被锁进尚未开赛的窗口）。
	if c.Status(now) == model.CompetitionRunning {
		if u.StartTime != nil && !u.StartTime.Equal(c.StartTime) {
			return nil, fmt.Errorf("%w: cannot change start_time while competition is running", ErrValidation)
		}
	}
	// 计分模式只属于 CTF，且只在未开赛时允许切换：开赛后再切换会让已有
	// 首通分按另一套规则解释，破坏榜单可审计性。
	if u.ScoringMode != nil {
		if c.Format != model.CompetitionFormatCTF {
			return nil, fmt.Errorf("%w: scoring_mode is only configurable for ctf competitions", ErrValidation)
		}
		m := model.CompetitionScoringMode(strings.TrimSpace(*u.ScoringMode))
		if !m.Valid() {
			return nil, fmt.Errorf("%w: invalid competition scoring mode", ErrValidation)
		}
		if c.Status(now) != model.CompetitionScheduled {
			return nil, fmt.Errorf("%w: cannot change scoring_mode after competition starts", ErrValidation)
		}
		fields["scoring_mode"] = m
	}
	if u.BonusCap != nil {
		if err := validateBonusCap(c.Format, *u.BonusCap); err != nil {
			return nil, err
		}
		fields["bonus_cap"] = *u.BonusCap
	}
	// 合成新的起止时间再整体校验区间。
	start, end := c.StartTime, c.EndTime
	if u.StartTime != nil {
		start = *u.StartTime
	}
	if u.EndTime != nil {
		end = *u.EndTime
	}
	if !end.After(start) {
		return nil, fmt.Errorf("%w: end_time must be after start_time", ErrValidation)
	}
	if u.StartTime != nil {
		fields["start_time"] = start.UTC()
	}
	if u.EndTime != nil {
		fields["end_time"] = end.UTC()
	}
	// 报名窗口：clear_registration=true 清空；否则请求里只给一侧时沿用库内
	// 另一侧，整体校验后再落库。
	regStart, regEnd := c.RegistrationStart, c.RegistrationEnd
	if u.ClearRegistration != nil && *u.ClearRegistration {
		if u.RegistrationStart != nil || u.RegistrationEnd != nil {
			return nil, fmt.Errorf("%w: cannot clear and set registration window together", ErrValidation)
		}
		fields["registration_start"] = nil
		fields["registration_end"] = nil
	} else if u.RegistrationStart != nil || u.RegistrationEnd != nil {
		if u.RegistrationStart != nil {
			regStart = u.RegistrationStart
		}
		if u.RegistrationEnd != nil {
			regEnd = u.RegistrationEnd
		}
		validStart, validEnd, err := validateRegistrationWindow(regStart, regEnd, end)
		if err != nil {
			return nil, err
		}
		fields["registration_start"] = validStart
		fields["registration_end"] = validEnd
	} else if c.RegistrationStart != nil || c.RegistrationEnd != nil {
		// 改了 end_time 时，既有报名窗口仍须落在新赛程内。
		if _, _, err := validateRegistrationWindow(c.RegistrationStart, c.RegistrationEnd, end); err != nil {
			return nil, err
		}
	}
	if err := s.comps.UpdateSelective(ctx, id, fields); err != nil {
		return nil, err
	}
	fresh, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.buildDetail(ctx, fresh)
}

// End 手动结束竞赛（仅 running）：end_time 置为当前时刻。状态仍由时间派生，
// 不引入落库字段；后续如需重新开赛，经 Update 把窗口改回未来即可。
func (s *CompetitionService) End(ctx context.Context, id uint) (*CompetitionDetail, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	switch c.Status(s.now()) {
	case model.CompetitionRunning:
	case model.CompetitionScheduled, model.CompetitionEnded:
		return nil, fmt.Errorf("%w: only a running competition can be ended", ErrValidation)
	}
	if err := s.comps.UpdateSelective(ctx, id, map[string]any{"end_time": s.now().UTC()}); err != nil {
		return nil, err
	}
	fresh, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.buildDetail(ctx, fresh)
}

// destroyInstance 竞赛删除时销毁运行容器的能力（main 注入 EnvService 适配器，
// 避免 service 横向 import；nil = 未装配（早期测试），跳过容器销毁）。
type instanceDestroyer func(ctx context.Context, competitionID uint) error

// SetInstanceDestroyer 注入竞赛容器销毁入口（main 装配后调用一次）。
func (s *CompetitionService) SetInstanceDestroyer(destroy instanceDestroyer) {
	s.destroyInstances = destroy
}

// Delete 删除竞赛（仅 scheduled/ended；running 422 拒绝——与 running 期间
// 禁改 start_time/禁解绑题目同一保护语义，先 End 手动结束）。容器/落盘 zip
// 是非事务资源，先于行删除清理：容器销毁失败中止（防孤儿容器），zip 失败仅记录继续删。
// 提交事件历史 append-only 保留（event-sourcing-rules）。
func (s *CompetitionService) Delete(ctx context.Context, id uint) error {
	c, err := s.find(ctx, id)
	if err != nil {
		return err
	}
	if c.Status(s.now()) == model.CompetitionRunning {
		return fmt.Errorf("%w: cannot delete a running competition; end it manually first", ErrValidation)
	}
	if s.destroyInstances != nil {
		if err := s.destroyInstances(ctx, id); err != nil {
			return fmt.Errorf("destroy competition instances: %w", err)
		}
	}
	// WP zip 附件整前缀删除（<id>/ 下全部对象；不存在不算错）。
	if s.writeups != nil {
		if err := s.writeups.RemovePrefix(ctx, strconv.Itoa(int(id))); err != nil {
			log.Printf("competition: remove writeup objects for %d failed: %v", id, err)
		}
	}
	return s.comps.DeleteCascade(ctx, id)
}

// ---------- 管理端：题目绑定与批量导入 ----------

// BindChallenges 三种入参统一入口：
//  1. 题目 JSON（对象或数组）→ 复用 content 管线导入后绑定；
//  2. {"slugs": [...]} → 绑定既有题目。
//
// 单题失败整体拒绝；已绑定跳过（幂等）。
func (s *CompetitionService) BindChallenges(ctx context.Context, id uint, raw []byte, importJSON func(ctx context.Context, raw []byte) (imported int, slugs []string, err error)) (*BindResult, error) {
	if _, err := s.requireFormat(ctx, id, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	var slugsReq struct {
		Slugs []string `json:"slugs"`
	}
	if err := json.Unmarshal(raw, &slugsReq); err == nil && slugsReq.Slugs != nil {
		return s.bindBySlugs(ctx, id, slugsReq.Slugs)
	}
	// 题目 JSON：走 content 管线（校验/upsert），再按返回 slug 绑定。
	if importJSON == nil {
		return nil, fmt.Errorf("%w: challenge import unavailable", ErrValidation)
	}
	_, slugs, err := importJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	return s.bindBySlugs(ctx, id, slugs)
}

// BindResult 绑定结果（契约 added + items）。
type BindResult struct {
	Added int64          `json:"added"`
	Items []TrainingItem `json:"items"`
}

func (s *CompetitionService) bindBySlugs(ctx context.Context, id uint, slugs []string) (*BindResult, error) {
	if len(slugs) == 0 {
		return nil, fmt.Errorf("%w: empty slugs", ErrValidation)
	}
	ids := make([]uint, 0, len(slugs))
	for _, slug := range slugs {
		chal, err := s.challenges.FindBySlug(ctx, slug)
		if err != nil {
			return nil, mapRecordNotFound(err)
		}
		ids = append(ids, chal.ID)
	}
	added, err := s.comps.BindChallenges(ctx, id, ids)
	if err != nil {
		return nil, err
	}
	items, err := s.listBoundItems(ctx, id)
	if err != nil {
		return nil, err
	}
	return &BindResult{Added: added, Items: items}, nil
}

func (s *CompetitionService) listBoundItems(ctx context.Context, id uint) ([]TrainingItem, error) {
	ids, err := s.comps.ListBoundChallengeIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []TrainingItem{}, nil
	}
	chals, err := s.challenges.FindByIDs(ctx, ids...)
	if err != nil {
		return nil, err
	}
	out := make([]TrainingItem, len(chals))
	for i, chal := range chals {
		out[i] = toTrainingItem(chal)
	}
	return out, nil
}

// AdminChallenges 管理端竞赛题目列表。
func (s *CompetitionService) AdminChallenges(ctx context.Context, id uint) ([]TrainingItem, error) {
	if _, err := s.requireFormat(ctx, id, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	return s.listBoundItems(ctx, id)
}

// UnbindChallenges 解绑题目：running 竞赛不允许（榜单已产生，撤题破坏现场）。
func (s *CompetitionService) UnbindChallenges(ctx context.Context, id uint, slugs []string) (int64, error) {
	c, err := s.requireFormat(ctx, id, model.CompetitionFormatCTF)
	if err != nil {
		return 0, err
	}
	if c.Status(s.now()) == model.CompetitionRunning {
		return 0, fmt.Errorf("%w: cannot unbind challenges while competition is running", ErrValidation)
	}
	if len(slugs) == 0 {
		return 0, fmt.Errorf("%w: empty slugs", ErrValidation)
	}
	ids := make([]uint, 0, len(slugs))
	for _, slug := range slugs {
		chal, err := s.challenges.FindBySlug(ctx, slug)
		if err != nil {
			return 0, mapRecordNotFound(err)
		}
		ids = append(ids, chal.ID)
	}
	return s.comps.UnbindChallenges(ctx, id, ids)
}

// ---------- 管理端：参赛者与队伍批量导入 / 一键生成 ----------

type ParticipantImportRequest struct {
	Teams       []TeamImport `json:"teams"`
	Individuals []string     `json:"individuals"`
}

type TeamImport struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

type ParticipantImportResult struct {
	Participants []InvitedParticipant `json:"participants"`
	// Missing 未匹配到账号的条目原文（账号编码与用户名均未命中）。
	// 导入只引用既有账号——新建账号走一键生成（GenerateUsers）。
	Missing []string `json:"missing"`
}

type InvitedParticipant struct {
	Username      string  `json:"username"`
	AccountCode   string  `json:"account_code"`
	TeamName      *string `json:"team_name"`
	InviteCode    string  `json:"invite_code"`
	MailDelivered bool    `json:"mail_delivered"`
}

// resolveUser 条目解析：形如账号编码（YA26-00042）的条目先按编码精确匹配、
// 未命中再按用户名；其余条目直接按用户名。返回 nil = 两种引用都未命中。
func (s *CompetitionService) resolveUser(ctx context.Context, entry string) (*model.User, error) {
	if _, ok := model.AccountCodeSeq(entry); ok {
		if u, err := s.users.FindByAccountCode(ctx, entry); err == nil {
			return u, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	u, err := s.users.FindByUsername(ctx, entry)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ImportParticipants 批量导入（仅邀请赛）：条目按「账号编码或用户名」引用
// 既有账号，不自动建号；缺失条目不阻断，在响应 missing 回报（配合一键生成
// 先补号再重导）。每人生成唯一邀请码并投递站内邀请邮件。
// 竞赛已结束拒绝导入；已参赛用户幂等跳过并沿用原邀请码（队伍按新归属更新
// 不覆盖，以首次导入为准——重复导入以补投邮件为主要用途）。
func (s *CompetitionService) ImportParticipants(ctx context.Context, id uint, req ParticipantImportRequest) (*ParticipantImportResult, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Kind != model.CompetitionInvitational {
		return nil, fmt.Errorf("%w: participants can only be imported into an invitational competition", ErrValidation)
	}
	if c.Status(s.now()) == model.CompetitionEnded {
		return nil, fmt.Errorf("%w: competition has ended", ErrValidation)
	}
	// 1. 请求级校验（队伍名/成员条目非空）。
	for _, t := range req.Teams {
		if strings.TrimSpace(t.Name) == "" || len(t.Members) == 0 {
			return nil, fmt.Errorf("%w: team name and members are required", ErrValidation)
		}
		for _, m := range t.Members {
			if strings.TrimSpace(m) == "" {
				return nil, fmt.Errorf("%w: empty entry", ErrValidation)
			}
		}
	}
	for _, m := range req.Individuals {
		if strings.TrimSpace(m) == "" {
			return nil, fmt.Errorf("%w: empty entry", ErrValidation)
		}
	}
	// 2. 用户解析（账号编码或用户名；缺失记入 missing，不建号）。
	userCache := map[string]*model.User{}
	missing := []string{}
	resolve := func(raw string) error {
		entry := strings.TrimSpace(raw)
		if _, done := userCache[entry]; done {
			return nil
		}
		u, err := s.resolveUser(ctx, entry)
		if err != nil {
			return err
		}
		userCache[entry] = u // nil 也缓存：同一条目重复出现只报一次 missing
		return nil
	}
	for _, t := range req.Teams {
		for _, m := range t.Members {
			if err := resolve(m); err != nil {
				return nil, err
			}
		}
	}
	for _, m := range req.Individuals {
		if err := resolve(m); err != nil {
			return nil, err
		}
	}
	for entry, u := range userCache {
		if u == nil {
			missing = append(missing, entry)
		}
	}
	sort.Strings(missing)
	// 3. 建队伍（竞赛内重名沿用既有——重复导入幂等）。
	teamID := map[string]uint{}
	for _, t := range req.Teams {
		name := strings.TrimSpace(t.Name)
		existing, err := s.comps.FindTeamByName(ctx, id, name)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			team := &model.Team{CompetitionID: id, Name: name}
			if err := s.comps.CreateTeam(ctx, team); err != nil {
				return nil, err
			}
			teamID[name] = team.ID
		} else {
			teamID[name] = existing.ID
		}
	}
	// 4. 逐人建参赛记录（幂等：已参赛沿用原记录）+ 投递邀请邮件。
	result := &ParticipantImportResult{Participants: []InvitedParticipant{}, Missing: missing}
	importEntry := func(raw string, team uint) error {
		u := userCache[strings.TrimSpace(raw)]
		if u == nil {
			return nil // 缺失条目已在 missing 回报
		}
		item, err := s.importOne(ctx, c, u, team)
		if err != nil {
			return err
		}
		result.Participants = append(result.Participants, item)
		return nil
	}
	for _, t := range req.Teams {
		for _, m := range t.Members {
			if err := importEntry(m, teamID[strings.TrimSpace(t.Name)]); err != nil {
				return nil, err
			}
		}
	}
	for _, m := range req.Individuals {
		if err := importEntry(m, 0); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// importOne 单人导入：参赛记录存在 → 沿用邀请码；不存在 → 生成邀请码、
// 落库、投递站内邀请邮件（失败只标记 mail_delivered=false，不回滚——
// 管理端可从参赛者列表拿到邀请码重发）。
func (s *CompetitionService) importOne(ctx context.Context, c *model.Competition, u *model.User, teamID uint) (InvitedParticipant, error) {
	part, err := s.comps.FindParticipant(ctx, c.ID, u.ID)
	if err != nil {
		return InvitedParticipant{}, err
	}
	if part == nil {
		code, err := randomInviteCode()
		if err != nil {
			return InvitedParticipant{}, err
		}
		part = &model.Participant{
			CompetitionID: c.ID,
			UserID:        u.ID,
			TeamID:        teamID,
			InviteCode:    code,
		}
		if err := s.comps.CreateParticipant(ctx, part); err != nil {
			return InvitedParticipant{}, err
		}
	}
	var teamName *string
	if part.TeamID > 0 {
		if name, err := s.participantTeamName(ctx, part); err == nil && name != nil {
			teamName = name
		}
	}
	delivered := false
	if s.deliverInvite != nil {
		title := fmt.Sprintf("竞赛邀请：%s", c.Name)
		body := fmt.Sprintf("你已被邀请参加「%s」。\n\n起止时间：%s ~ %s。\n\n凭下方邀请码入场（入场后至比赛结束不可退出）：",
			c.Name, c.StartTime.Format("2006-01-02 15:04"), c.EndTime.Format("2006-01-02 15:04"))
		delivered = s.deliverInvite(ctx, u.ID, title, body, c.ID, part.InviteCode) == nil
	}
	return InvitedParticipant{
		Username:      u.Username,
		AccountCode:   u.AccountCode,
		TeamName:      teamName,
		InviteCode:    part.InviteCode,
		MailDelivered: delivered,
	}, nil
}

// GenerateUsersRequest 一键生成参赛账号（仅邀请赛；默认密码 123456）。
type GenerateUsersRequest struct {
	Prefix   string `json:"prefix"`
	Count    int    `json:"count"`
	TeamSize int    `json:"team_size"`
	Password string `json:"password"`
}

// GeneratedAccount 生成账号条目（initial_password 仅新建账号返回，
// 供管理端导出分发；复用既有账号为 null，沿用原密码）。
type GeneratedAccount struct {
	Username        string  `json:"username"`
	AccountCode     string  `json:"account_code"`
	InitialPassword *string `json:"initial_password"`
	Created         bool    `json:"created"`
}

type GenerateUsersResult struct {
	Accounts     []GeneratedAccount   `json:"accounts"`
	Teams        []string             `json:"teams"`
	Participants []InvitedParticipant `json:"participants"`
}

var generatePrefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,15}$`)

// GenerateUsers 一键生成 count 个参赛账号（用户名 = 前缀-零填充序号）并全部
// 导入为本场参赛者：可配置自动组队（team_size > 0 按序编入「第 n 队」，余数
// 进最后一队）与初始密码（默认 123456）。已存在的同名账号复用（沿用原密码，
// created=false）。账号编码由建号入口统一分配。
func (s *CompetitionService) GenerateUsers(ctx context.Context, id uint, req GenerateUsersRequest) (*GenerateUsersResult, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Kind != model.CompetitionInvitational {
		return nil, fmt.Errorf("%w: user generation is only available for invitational competitions", ErrValidation)
	}
	if c.Status(s.now()) == model.CompetitionEnded {
		return nil, fmt.Errorf("%w: competition has ended", ErrValidation)
	}
	prefix := strings.TrimSpace(req.Prefix)
	if !generatePrefixPattern.MatchString(prefix) {
		return nil, fmt.Errorf("%w: prefix must be 1-16 lowercase letters, digits or hyphens", ErrValidation)
	}
	if req.Count < 1 || req.Count > 500 {
		return nil, fmt.Errorf("%w: count must be between 1 and 500", ErrValidation)
	}
	if req.TeamSize < 0 || req.TeamSize > 10 {
		return nil, fmt.Errorf("%w: team_size must be between 0 and 10", ErrValidation)
	}
	password := req.Password
	if password == "" {
		password = "123456"
	}
	if len(password) < 6 {
		return nil, fmt.Errorf("%w: password must be at least 6 characters", ErrValidation)
	}
	if s.createAccount == nil {
		return nil, fmt.Errorf("%w: account creation unavailable", ErrValidation)
	}
	// 用户名序号零填充：至少两位、按 count 位数（class26-01 … class26-06）。
	width := len(strconv.Itoa(req.Count))
	if width < 2 {
		width = 2
	}
	result := &GenerateUsersResult{Accounts: []GeneratedAccount{}, Teams: []string{}, Participants: []InvitedParticipant{}}
	users := make([]*model.User, 0, req.Count)
	for i := 1; i <= req.Count; i++ {
		username := fmt.Sprintf("%s-%0*d", prefix, width, i)
		existing, err := s.users.FindByUsername(ctx, username)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if existing != nil {
			result.Accounts = append(result.Accounts, GeneratedAccount{
				Username: existing.Username, AccountCode: existing.AccountCode, Created: false,
			})
			users = append(users, existing)
			continue
		}
		acc, err := s.createAccount(ctx, username, password, model.GlobalRoleStudent)
		if err != nil {
			return nil, err
		}
		pwd := password
		result.Accounts = append(result.Accounts, GeneratedAccount{
			Username: acc.Username, AccountCode: acc.AccountCode, InitialPassword: &pwd, Created: true,
		})
		users = append(users, acc)
	}
	// 自动组队：按生成顺序每 team_size 人一队，余数进最后一队。
	teamID := make([]uint, req.Count)
	if req.TeamSize > 0 {
		teamCount := (req.Count + req.TeamSize - 1) / req.TeamSize
		for t := 1; t <= teamCount; t++ {
			name := fmt.Sprintf("第%d队", t)
			existing, err := s.comps.FindTeamByName(ctx, id, name)
			if err != nil {
				return nil, err
			}
			if existing == nil {
				team := &model.Team{CompetitionID: id, Name: name}
				if err := s.comps.CreateTeam(ctx, team); err != nil {
					return nil, err
				}
				result.Teams = append(result.Teams, name)
				for i := (t - 1) * req.TeamSize; i < t*req.TeamSize && i < req.Count; i++ {
					teamID[i] = team.ID
				}
			} else {
				// 重复生成同名队伍沿用（与导入幂等语义一致）。
				for i := (t - 1) * req.TeamSize; i < t*req.TeamSize && i < req.Count; i++ {
					teamID[i] = existing.ID
				}
			}
		}
	}
	for i, u := range users {
		item, err := s.importOne(ctx, c, u, teamID[i])
		if err != nil {
			return nil, err
		}
		result.Participants = append(result.Participants, item)
	}
	return result, nil
}

// AdminParticipants 管理端参赛者分页（含队伍富化）。
type AdminParticipantItem struct {
	model.Participant
	Username  string  `json:"username"`
	TeamID    *uint   `json:"team_id"`
	TeamName  *string `json:"team_name"`
	Entered   bool    `json:"entered"`
	EnteredAt *string `json:"entered_at"`
}

func (s *CompetitionService) AdminParticipants(ctx context.Context, id uint, page, pageSize int) ([]AdminParticipantItem, []TeamLeaderboardEntry, int64, error) {
	if _, err := s.find(ctx, id); err != nil {
		return nil, nil, 0, err
	}
	list, total, err := s.comps.ListParticipants(ctx, id, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, nil, 0, err
	}
	teams, err := s.comps.ListTeams(ctx, id)
	if err != nil {
		return nil, nil, 0, err
	}
	teamNames := map[uint]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}
	userIDs := make([]uint, len(list))
	for i, p := range list {
		userIDs[i] = p.UserID
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, nil, 0, err
	}
	userNames := map[uint]string{}
	for _, u := range users {
		userNames[u.ID] = u.Username
	}
	items := make([]AdminParticipantItem, len(list))
	for i, p := range list {
		item := AdminParticipantItem{
			Participant: p,
			Username:    userNames[p.UserID],
			Entered:     p.EnteredAt != nil,
		}
		if p.TeamID > 0 {
			tid := p.TeamID
			item.TeamID = &tid
			if name, ok := teamNames[p.TeamID]; ok {
				item.TeamName = &name
			}
		}
		if p.EnteredAt != nil {
			ts := p.EnteredAt.Format(time.RFC3339)
			item.EnteredAt = &ts
		}
		items[i] = item
	}
	// 队伍总览（复用排行榜聚合，成员按导入顺序）。
	teamOverview, err := s.teamOverview(ctx, id, teams)
	if err != nil {
		return nil, nil, 0, err
	}
	return items, teamOverview, total, nil
}

// teamOverview 队伍总览（成员名，不含积分——导入阶段榜单无意义）。
func (s *CompetitionService) teamOverview(ctx context.Context, id uint, teams []model.Team) ([]TeamLeaderboardEntry, error) {
	parts, _, err := s.comps.ListParticipants(ctx, id, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	userIDs := make([]uint, len(parts))
	for i, p := range parts {
		userIDs[i] = p.UserID
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, err
	}
	userNames := map[uint]string{}
	for _, u := range users {
		userNames[u.ID] = u.Username
	}
	members := map[uint][]string{}
	for _, p := range parts {
		members[p.TeamID] = append(members[p.TeamID], userNames[p.UserID])
	}
	out := make([]TeamLeaderboardEntry, 0, len(teams))
	for _, t := range teams {
		name := t.Name
		memberNames := members[t.ID]
		if memberNames == nil {
			memberNames = []string{}
		}
		out = append(out, TeamLeaderboardEntry{TeamID: t.ID, TeamName: &name, Members: memberNames})
	}
	return out, nil
}

// LockedCompetition 入场锁检查（api 层中间件调用）：已入场且竞赛 running
// 的邀请赛 → (竞赛ID, true)。公开赛报名即可入场、可随时返回平台，不锁定；
// ended 后自动解锁。管理员不查此接口（中间件短路）。
func (s *CompetitionService) LockedCompetition(ctx context.Context, userID uint) (uint, bool) {
	active, err := s.comps.ActiveCompetitions(ctx, s.now())
	if err != nil {
		return 0, false // 查询失败不锁（可用性优先）
	}
	for i := range active {
		c := &active[i]
		// 锁定只属于邀请赛：公开赛是开放报名的自由进出模式。
		if c.Kind != model.CompetitionInvitational {
			continue
		}
		part, err := s.comps.FindParticipant(ctx, c.ID, userID)
		if err != nil {
			continue
		}
		if part != nil && part.EnteredAt != nil {
			return c.ID, true
		}
	}
	return 0, false
}

// ---------- 参赛者：入场与锁定 ----------

// ---------- 参赛者：竞赛广场 / 报名 / 入场与锁定 ----------

// PublicCompetitionItem 竞赛广场条目（仅公开赛；邀请赛定向，不对外展示）。
type PublicCompetitionItem struct {
	ID                uint                         `json:"id"`
	Name              string                       `json:"name"`
	Description       string                       `json:"description"`
	Format            model.CompetitionFormat      `json:"format"`
	ScoringMode       model.CompetitionScoringMode `json:"scoring_mode"`
	RegistrationStart *time.Time                   `json:"registration_start"`
	RegistrationEnd   *time.Time                   `json:"registration_end"`
	StartTime         time.Time                    `json:"start_time"`
	EndTime           time.Time                    `json:"end_time"`
	Status            model.CompetitionStatus      `json:"status"`
	Registered        bool                         `json:"registered"`
	Participants      int64                        `json:"participants"`
}

// ListPublic 竞赛广场（学生侧）：分页列出公开赛，标注当前用户报名状态与
// 报名人数。邀请赛不出现在广场。
func (s *CompetitionService) ListPublic(ctx context.Context, userID uint, page, pageSize int) ([]PublicCompetitionItem, int64, error) {
	list, total, err := s.comps.ListPublic(ctx, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, err
	}
	now := s.now()
	items := make([]PublicCompetitionItem, len(list))
	for i := range list {
		c := &list[i]
		_, pTotal, err := s.comps.ListParticipants(ctx, c.ID, 0, 1)
		if err != nil {
			return nil, 0, err
		}
		registered := false
		if part, err := s.comps.FindParticipant(ctx, c.ID, userID); err != nil {
			return nil, 0, err
		} else if part != nil {
			registered = true
		}
		items[i] = PublicCompetitionItem{
			ID: c.ID, Name: c.Name, Description: c.Description,
			Format: c.Format, ScoringMode: c.ScoringMode,
			RegistrationStart: c.RegistrationStart, RegistrationEnd: c.RegistrationEnd,
			StartTime: c.StartTime, EndTime: c.EndTime, Status: c.Status(now),
			Registered: registered, Participants: pTotal,
		}
	}
	return items, total, nil
}

// Register 公开赛自助报名（幂等）：个人参赛（不编队），仍生成邀请码作备用
// 入场凭据但不投递邮件（报名即入场资格）。邀请赛拒绝（参赛者由管理端定向
// 导入/生成）；已结束拒绝。已报名重复调用返回当前条目。
func (s *CompetitionService) Register(ctx context.Context, userID, id uint) (*PublicCompetitionItem, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Kind != model.CompetitionPublic {
		return nil, fmt.Errorf("%w: cannot register for an invitational competition", ErrValidation)
	}
	now := s.now()
	if c.Status(now) == model.CompetitionEnded {
		return nil, fmt.Errorf("%w: competition has ended", ErrValidation)
	}
	if !c.RegistrationOpen(now) {
		return nil, fmt.Errorf("%w: competition registration is not open", ErrValidation)
	}
	part, err := s.comps.FindParticipant(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if part == nil {
		code, err := randomInviteCode()
		if err != nil {
			return nil, err
		}
		if err := s.comps.CreateParticipant(ctx, &model.Participant{
			CompetitionID: id, UserID: userID, InviteCode: code,
		}); err != nil {
			return nil, err
		}
	}
	return s.publicItem(ctx, c, userID)
}

func (s *CompetitionService) publicItem(ctx context.Context, c *model.Competition, userID uint) (*PublicCompetitionItem, error) {
	_, pTotal, err := s.comps.ListParticipants(ctx, c.ID, 0, 1)
	if err != nil {
		return nil, err
	}
	registered := false
	if part, err := s.comps.FindParticipant(ctx, c.ID, userID); err != nil {
		return nil, err
	} else if part != nil {
		registered = true
	}
	return &PublicCompetitionItem{
		ID: c.ID, Name: c.Name, Description: c.Description,
		Format: c.Format, ScoringMode: c.ScoringMode,
		RegistrationStart: c.RegistrationStart, RegistrationEnd: c.RegistrationEnd,
		StartTime: c.StartTime, EndTime: c.EndTime, Status: c.Status(s.now()),
		Registered: registered, Participants: pTotal,
	}, nil
}

// MyEntry 当前用户参赛状态：优先返回已入场的邀请赛（locked=true）；
// 否则返回已入场的公开赛（entered=true, locked=false，可自由进出）。
// 管理员恒不锁（锁是参赛者隔离，不是权限；与路由中间件豁免同语义）。
// 前端据 locked 锁定平台入口，据 entered 判断是否可回到竞赛首页。
func (s *CompetitionService) MyEntry(ctx context.Context, userID uint, role model.GlobalRole) (*MyCompetitionEntry, error) {
	active, err := s.comps.ActiveCompetitions(ctx, s.now())
	if err != nil {
		return nil, err
	}
	var enteredPublic *MyCompetitionEntry
	for i := range active {
		c := &active[i]
		part, err := s.comps.FindParticipant(ctx, c.ID, userID)
		if err != nil {
			return nil, err
		}
		if part == nil || part.EnteredAt == nil {
			continue
		}
		view := toView(c, s.now())
		teamName, err := s.participantTeamName(ctx, part)
		if err != nil {
			return nil, err
		}
		entry := &MyCompetitionEntry{
			Competition: &view, Locked: locksPlatform(c, role),
			Entered: true, TeamName: teamName,
		}
		// 邀请赛锁定优先：同时参加公开赛与邀请赛时以邀请赛为准。
		if entry.Locked {
			return entry, nil
		}
		if enteredPublic == nil {
			enteredPublic = entry
		}
	}
	if enteredPublic != nil {
		return enteredPublic, nil
	}
	return &MyCompetitionEntry{Competition: nil, Locked: false, Entered: false}, nil
}

type MyCompetitionEntry struct {
	Competition *CompetitionView `json:"competition"`
	Locked      bool             `json:"locked"`
	Entered     bool             `json:"entered"`
	TeamName    *string          `json:"team_name"`
}

// Enter 入场：仅 running 竞赛有效（scheduled 未开赛、ended 已结束 → 422）。
// 凭据两种：邀请码（邀请赛/公开赛通用——参赛者列表可查码），或公开赛已报名
// 者免码（报名即入场资格）。邀请赛入场后不可退出：锁定状态派生自
// (entered 且 running 且邀请赛)，直到比赛结束；公开赛入场不锁定，可自由进出。
func (s *CompetitionService) Enter(ctx context.Context, userID, id uint, inviteCode string, role model.GlobalRole) (*MyCompetitionEntry, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	var part *model.Participant
	if inviteCode != "" {
		part, err = s.comps.FindParticipantByCode(ctx, inviteCode)
		if err != nil {
			return nil, err
		}
		if part == nil || part.CompetitionID != id || part.UserID != userID {
			return nil, fmt.Errorf("%w: invalid invite code", ErrValidation)
		}
	} else if c.Kind == model.CompetitionPublic {
		// 公开赛已报名者免码入场；未报名先去广场报名。
		part, err = s.comps.FindParticipant(ctx, id, userID)
		if err != nil {
			return nil, err
		}
		if part == nil {
			return nil, fmt.Errorf("%w: register before entering a public competition", ErrValidation)
		}
	} else {
		return nil, fmt.Errorf("%w: invite_code is required", ErrValidation)
	}
	switch c.Status(s.now()) {
	case model.CompetitionScheduled:
		return nil, fmt.Errorf("%w: competition has not started", ErrValidation)
	case model.CompetitionEnded:
		return nil, fmt.Errorf("%w: competition has ended", ErrValidation)
	}
	if _, err := s.comps.MarkEntered(ctx, part.ID); err != nil {
		return nil, err
	}
	view := toView(c, s.now())
	teamName, err := s.participantTeamName(ctx, part)
	if err != nil {
		return nil, err
	}
	return &MyCompetitionEntry{
		Competition: &view, Locked: locksPlatform(c, role),
		Entered: true, TeamName: teamName,
	}, nil
}

// locksPlatform 该竞赛是否把已入场者锁定在竞赛域：仅邀请赛，且管理员恒不锁。
// 与 router.go 的中间件豁免同语义；集中在这里避免入口页/守卫各自判断。
func locksPlatform(c *model.Competition, role model.GlobalRole) bool {
	return c.Kind == model.CompetitionInvitational && role.Normalize() != model.GlobalRoleAdmin
}

func (s *CompetitionService) participantTeamName(ctx context.Context, part *model.Participant) (*string, error) {
	if part.TeamID == 0 {
		return nil, nil
	}
	teams, err := s.comps.ListTeams(ctx, part.CompetitionID)
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		if t.ID == part.TeamID {
			name := t.Name
			return &name, nil
		}
	}
	return nil, nil
}

// distinctWpUnits WP 份数统计口径 = 题目 × 队伍/个人（与提交单位一致）。
func distinctWpUnits(list []model.Writeup) int {
	seen := map[[2]uint]bool{}
	for _, w := range list {
		seen[[2]uint{w.ChallengeID, w.TeamID}] = true
	}
	return len(seen)
}

// requireEntered 校验"已入场参赛者"（参赛者视角全部接口的前置条件）。
// 结束后仍允许访问（看榜单、提交 WP），running 之外不允许开容器/提交 flag。
func (s *CompetitionService) requireEntered(ctx context.Context, userID, id uint) (*model.Competition, *model.Participant, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	part, err := s.comps.FindParticipant(ctx, id, userID)
	if err != nil {
		return nil, nil, err
	}
	if part == nil || part.EnteredAt == nil {
		return nil, nil, ErrNotFound
	}
	return c, part, nil
}

// ---------- 参赛者：竞赛首页 / 容器 / 提交 ----------

// CompetitionChallengeView 参赛者视角题目条目。
type CompetitionChallengeView struct {
	ID          uint   `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Category    string `json:"category"`
	Difficulty  string `json:"difficulty"`
	Score       int    `json:"score"`
	HasEnv      bool   `json:"has_env"`
	Description string `json:"description_md"`
	Solved      bool   `json:"solved"`
	// SolveCount 该题在竞赛内的通过人数（按 CompetitionScore 去重统计），
	// 前端用于展示通过率；无通过记录时为 0。
	SolveCount int `json:"solve_count"`
}

// CompetitionScorePoint 得分曲线点：按通关时间累计得分。
type CompetitionScorePoint struct {
	At          string `json:"at"`
	ChallengeID uint   `json:"challenge_id"`
	Delta       int    `json:"delta"`
	Score       int    `json:"score"`
}

// CompetitionCategoryStat 分类进度：题目数、已通关数、满分与已得分。
type CompetitionCategoryStat struct {
	Category    string `json:"category"`
	Total       int    `json:"total"`
	Solved      int    `json:"solved"`
	ScoreTotal  int    `json:"score_total"`
	ScoreEarned int    `json:"score_earned"`
}

// CompetitionDifficultyStat 难度分布：该难度题目数与已通关数。
type CompetitionDifficultyStat struct {
	Difficulty string `json:"difficulty"`
	Total      int    `json:"total"`
	Solved     int    `json:"solved"`
}

// CompetitionSubmissionStats 本人提交结果计数（全量，不只最近 20 条）。
type CompetitionSubmissionStats struct {
	Correct   int64 `json:"correct"`
	Incorrect int64 `json:"incorrect"`
	Pending   int64 `json:"pending"`
}

// CompetitionAnalytics 竞赛首页看板聚合（只读派生数据，与排行榜同源）。
type CompetitionAnalytics struct {
	Score            int                         `json:"score"`
	SolvedCount      int                         `json:"solved_count"`
	TotalChallenges  int                         `json:"total_challenges"`
	TotalScore       int                         `json:"total_score"`
	ParticipantCount int64                       `json:"participant_count"`
	ScoreTimeline    []CompetitionScorePoint     `json:"score_timeline"`
	Categories       []CompetitionCategoryStat   `json:"categories"`
	Difficulties     []CompetitionDifficultyStat `json:"difficulties"`
	SubmissionStats  CompetitionSubmissionStats  `json:"submission_stats"`
}

// MySubmissionView 参赛者本人的提交条目（竞赛首页"我的提交"）。
type MySubmissionView struct {
	ID        uint   `json:"id"`
	Status    string `json:"status"`
	Score     int    `json:"score"`
	CreatedAt string `json:"created_at"`
}

// CompetitionHome 竞赛首页聚合视图。
type CompetitionHome struct {
	Competition      CompetitionView            `json:"competition"`
	TeamName         *string                    `json:"team_name"`
	Teammates        []string                   `json:"teammates"`
	Challenges       []CompetitionChallengeView `json:"challenges"`
	MySubmissions    []MySubmissionView         `json:"my_submissions"`
	Analytics        CompetitionAnalytics       `json:"analytics"`
	WriteupSubmitted bool                       `json:"writeup_submitted"`
}

func (s *CompetitionService) View(ctx context.Context, userID, id uint) (*CompetitionHome, error) {
	c, part, err := s.requireEntered(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if err := requireCompetitionFormat(c, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	bound, err := s.comps.ListBoundChallengeIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	chals, err := s.challenges.FindByIDs(ctx, bound...)
	if err != nil {
		return nil, err
	}
	// 通关状态与通过人数统一从 CompetitionScore 派生表读取：与排行榜同源，
	// 同时避免逐题 HasCorrect 的 N+1 查询。
	scores, err := s.comps.ListScores(ctx, id)
	if err != nil {
		return nil, err
	}
	solveUsers := make(map[uint]map[uint]struct{}, len(bound))
	myScores := make([]model.CompetitionScore, 0, len(bound))
	for _, sc := range scores {
		set := solveUsers[sc.ChallengeID]
		if set == nil {
			set = make(map[uint]struct{})
			solveUsers[sc.ChallengeID] = set
		}
		set[sc.UserID] = struct{}{}
		if sc.UserID == userID {
			myScores = append(myScores, sc)
		}
	}
	myScoreByChallenge := make(map[uint]int, len(myScores))
	for _, sc := range myScores {
		myScoreByChallenge[sc.ChallengeID] = sc.Score
	}
	challenges := make([]CompetitionChallengeView, 0, len(chals))
	for _, chal := range chals {
		_, solved := myScoreByChallenge[chal.ID]
		challenges = append(challenges, CompetitionChallengeView{
			ID: chal.ID, Slug: chal.Slug, Title: chal.Title, Category: chal.Category,
			Difficulty: string(chal.Difficulty), Score: chal.Score,
			HasEnv: chal.Image != "", Description: chal.Description, Solved: solved,
			SolveCount: len(solveUsers[chal.ID]),
		})
	}
	subs, err := s.submissions.ListByCompetitionUser(ctx, id, userID, 20)
	if err != nil {
		return nil, err
	}
	mySubs := make([]MySubmissionView, len(subs))
	for i, sub := range subs {
		mySubs[i] = MySubmissionView{
			ID: sub.ID, Status: string(sub.Status), Score: sub.Score,
			CreatedAt: sub.CreatedAt.Format(time.RFC3339),
		}
	}
	teamName, teammates, err := s.teamInfo(ctx, part)
	if err != nil {
		return nil, err
	}
	participantCount, err := s.comps.CountEntered(ctx, id)
	if err != nil {
		return nil, err
	}
	submissionCounts, err := s.submissions.CountByCompetitionUserStatus(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	analytics := buildCompetitionAnalytics(chals, myScores, participantCount, submissionCounts)
	// 队伍模式任一队员提交过任一题即视为已提交。
	writeupSubmitted := false
	if c.Status(s.now()) == model.CompetitionEnded {
		writeups, err := s.comps.ListWriteups(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, w := range writeups {
			if w.TeamID == part.TeamID {
				writeupSubmitted = true
				break
			}
		}
	}
	return &CompetitionHome{
		Competition:      toView(c, s.now()),
		TeamName:         teamName,
		Teammates:        teammates,
		Challenges:       challenges,
		MySubmissions:    mySubs,
		Analytics:        analytics,
		WriteupSubmitted: writeupSubmitted,
	}, nil
}

// buildCompetitionAnalytics 从绑定题目与本人积分派生行聚合看板数据：
// 分数/通关数/得分曲线取本人，分类与难度覆盖率按绑定题目为分母，
// 提交计数取全量状态分布。所有输入都是只读派生数据，不写事件流。
func buildCompetitionAnalytics(
	chals []model.Challenge,
	myScores []model.CompetitionScore,
	participantCount int64,
	submissionCounts map[model.SubmissionStatus]int64,
) CompetitionAnalytics {
	out := CompetitionAnalytics{
		TotalChallenges:  len(chals),
		ParticipantCount: participantCount,
		ScoreTimeline:    make([]CompetitionScorePoint, 0, len(myScores)),
		Categories:       make([]CompetitionCategoryStat, 0),
		Difficulties:     make([]CompetitionDifficultyStat, 0),
		SubmissionStats: CompetitionSubmissionStats{
			Correct:   submissionCounts[model.SubmissionCorrect],
			Incorrect: submissionCounts[model.SubmissionIncorrect],
			Pending:   submissionCounts[model.SubmissionPending],
		},
	}
	scoreByChallenge := make(map[uint]int, len(myScores))
	for _, sc := range myScores {
		scoreByChallenge[sc.ChallengeID] = sc.Score
		out.Score += sc.Score
		out.SolvedCount++
	}
	categoryIndex := make(map[string]int)
	difficultyTotal := make(map[string][2]int)
	for _, chal := range chals {
		out.TotalScore += chal.Score
		earned, solved := scoreByChallenge[chal.ID]
		idx, ok := categoryIndex[chal.Category]
		if !ok {
			idx = len(out.Categories)
			categoryIndex[chal.Category] = idx
			out.Categories = append(out.Categories, CompetitionCategoryStat{Category: chal.Category})
		}
		cat := &out.Categories[idx]
		cat.Total++
		cat.ScoreTotal += chal.Score
		cat.ScoreEarned += earned
		if solved {
			cat.Solved++
		}
		diff := string(chal.Difficulty)
		agg := difficultyTotal[diff]
		agg[0]++
		if solved {
			agg[1]++
		}
		difficultyTotal[diff] = agg
	}
	for _, d := range []model.Difficulty{
		model.DifficultyApprentice, model.DifficultyPractitioner, model.DifficultyExpert,
	} {
		agg, ok := difficultyTotal[string(d)]
		if !ok {
			continue
		}
		out.Difficulties = append(out.Difficulties, CompetitionDifficultyStat{
			Difficulty: string(d), Total: agg[0], Solved: agg[1],
		})
	}
	sort.SliceStable(myScores, func(i, j int) bool {
		if myScores[i].SolvedAt.Equal(myScores[j].SolvedAt) {
			return myScores[i].ID < myScores[j].ID
		}
		return myScores[i].SolvedAt.Before(myScores[j].SolvedAt)
	})
	cumulative := 0
	for _, sc := range myScores {
		cumulative += sc.Score
		out.ScoreTimeline = append(out.ScoreTimeline, CompetitionScorePoint{
			At:          sc.SolvedAt.UTC().Format(time.RFC3339),
			ChallengeID: sc.ChallengeID,
			Delta:       sc.Score,
			Score:       cumulative,
		})
	}
	return out
}

func (s *CompetitionService) teamInfo(ctx context.Context, part *model.Participant) (*string, []string, error) {
	if part.TeamID == 0 {
		return nil, []string{}, nil
	}
	teams, err := s.comps.ListTeams(ctx, part.CompetitionID)
	if err != nil {
		return nil, nil, err
	}
	var name *string
	for _, t := range teams {
		if t.ID == part.TeamID {
			n := t.Name
			name = &n
			break
		}
	}
	parts, _, err := s.comps.ListParticipants(ctx, part.CompetitionID, 0, 1<<30)
	if err != nil {
		return nil, nil, err
	}
	var members []uint
	for _, p := range parts {
		if p.TeamID == part.TeamID {
			members = append(members, p.UserID)
		}
	}
	users, err := s.users.FindByIDs(ctx, members...)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, u.Username)
	}
	return name, names, nil
}

// StartInstance 竞赛内启动题目容器（已入场 + running 竞赛 + 题目属于本场）。
// 容器创建本身委托 EnvService（EnsureInstanceScoped 落 competition_id）。
func (s *CompetitionService) StartInstance(ctx context.Context, env *EnvService, userID, id uint, slug string) (*model.LabInstance, error) {
	c, _, err := s.requireEntered(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if err := requireCompetitionFormat(c, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	if c.Status(s.now()) != model.CompetitionRunning {
		return nil, fmt.Errorf("%w: competition is not running", ErrValidation)
	}
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	bound, err := s.comps.ListBoundChallengeIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	if !containsUint(bound, chal.ID) {
		return nil, ErrNotFound
	}
	return env.EnsureInstanceScoped(ctx, userID, chal.ID, id)
}

// SubmitFlag 竞赛内提交：已入场 + running + 题目属于本场。
// 判定/计分/反作弊全部走 judge 异步管线（competition_id 作用域）。
func (s *CompetitionService) SubmitFlag(ctx context.Context, judge *JudgeService, userID, id uint, slug, answer, sourceIP string) (*model.Submission, error) {
	c, _, err := s.requireEntered(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if err := requireCompetitionFormat(c, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	if c.Status(s.now()) != model.CompetitionRunning {
		return nil, fmt.Errorf("%w: competition is not running", ErrValidation)
	}
	chal, err := s.challenges.FindBySlug(ctx, slug)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	bound, err := s.comps.ListBoundChallengeIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	if !containsUint(bound, chal.ID) {
		return nil, ErrNotFound
	}
	return judge.SubmitScoped(ctx, userID, chal.ID, id, answer, sourceIP)
}

// ---------- 排行榜 ----------

// TeamLeaderboardEntry 战队榜条目；individual 模式下复用（TeamID=0，
// TeamName=nil，按成员聚合）。同分并列同名次；先达者靠前。
type TeamLeaderboardEntry struct {
	Rank        int      `json:"rank"`
	TeamID      uint     `json:"team_id"`
	TeamName    *string  `json:"team_name"`
	Score       int      `json:"score"`
	Bonus       int      `json:"bonus"`
	SolvedCount int      `json:"solved_count"`
	Members     []string `json:"members"`
	LastSolveAt *string  `json:"last_solve_at"`
	// AccountCode 仅 individual 模式填充（榜单导出/证书定位账号）；team 模式为空。
	AccountCode string `json:"account_code,omitempty"`
	// UserID 仅 individual 模式填充（晋级/证书按人落库）；team 模式为空。
	UserID uint `json:"user_id,omitempty"`
}

// CompLeaderboardEntry 个人榜条目（同分并列，先达者靠前）。
type CompLeaderboardEntry struct {
	Rank        int     `json:"rank"`
	UserID      uint    `json:"user_id"`
	Username    string  `json:"username"`
	AccountCode string  `json:"account_code"`
	TeamName    *string `json:"team_name"`
	Score       int     `json:"score"`
	Bonus       int     `json:"bonus"`
	SolvedCount int     `json:"solved_count"`
	LastSolveAt *string `json:"last_solve_at"`
}

// Leaderboard 竞赛排行榜：team 按战队聚合（个人参赛者各成一"独行侠"行），
// individual 按人。排序：score DESC → last_solve_at 早者靠前 → solved_count DESC。
// 数据源 CompetitionScore（派生表，只读聚合）。
func (s *CompetitionService) Leaderboard(ctx context.Context, id uint, mode string) (map[string]any, error) {
	if _, err := s.requireFormat(ctx, id, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	scores, err := s.comps.ListScores(ctx, id)
	if err != nil {
		return nil, err
	}
	parts, _, err := s.comps.ListParticipants(ctx, id, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	teams, err := s.comps.ListTeams(ctx, id)
	if err != nil {
		return nil, err
	}
	userIDs := make([]uint, len(parts))
	for i, p := range parts {
		userIDs[i] = p.UserID
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, err
	}
	userNames := map[uint]string{}
	userCodes := map[uint]string{}
	for _, u := range users {
		userNames[u.ID] = u.Username
		userCodes[u.ID] = u.AccountCode
	}
	teamNames := map[uint]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}
	// 聚合到个人。
	type personAgg struct {
		userID, teamID uint
		score, bonus   int
		solved         int
		last           time.Time
	}
	agg := map[uint]*personAgg{}
	// 先按参赛名册铺设零分行：个人榜必须展示未解出任何题的参赛者，
	// 否则「未得分」与「未参赛」在榜单上无法区分，也无法用于晋级/证书。
	for _, p := range parts {
		agg[p.UserID] = &personAgg{userID: p.UserID, teamID: p.TeamID}
	}
	for _, sc := range scores {
		a := agg[sc.UserID]
		if a == nil {
			a = &personAgg{userID: sc.UserID, teamID: sc.TeamID}
			agg[sc.UserID] = a
		}
		a.score += sc.Score
		a.solved++
		if sc.SolvedAt.After(a.last) {
			a.last = sc.SolvedAt
		}
	}
	// 附加分是 append-only 源事实，不和首通分混写 CompetitionScore；
	// 与首通分同源聚合进个人/队伍总分。
	adjustments, err := s.comps.ListCtfAdjustments(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, adj := range adjustments {
		a := agg[adj.UserID]
		if a == nil {
			a = &personAgg{userID: adj.UserID, teamID: adj.TeamID}
			agg[adj.UserID] = a
		}
		a.score += adj.Points
		a.bonus += adj.Points
	}
	if mode == "individual" {
		entries := make([]CompLeaderboardEntry, 0, len(agg))
		for _, a := range agg {
			var teamName *string
			if a.teamID > 0 {
				if n, ok := teamNames[a.teamID]; ok {
					teamName = &n
				}
			}
			var last *string
			if !a.last.IsZero() {
				ls := a.last.Format(time.RFC3339)
				last = &ls
			}
			entries = append(entries, CompLeaderboardEntry{
				UserID: a.userID, Username: userNames[a.userID], AccountCode: userCodes[a.userID],
				TeamName: teamName, Score: a.score, Bonus: a.bonus, SolvedCount: a.solved, LastSolveAt: last,
			})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Score != entries[j].Score {
				return entries[i].Score > entries[j].Score
			}
			li, lj := entries[i].LastSolveAt, entries[j].LastSolveAt
			if (li == nil) != (lj == nil) {
				return li != nil
			}
			if li != nil && *li != *lj {
				return *li < *lj
			}
			return entries[i].SolvedCount > entries[j].SolvedCount
		})
		rankIndividualEntries(entries)
		items := make([]TeamLeaderboardEntry, len(entries))
		for i, e := range entries {
			items[i] = TeamLeaderboardEntry{
				Rank: e.Rank, TeamName: e.TeamName,
				Score: e.Score, Bonus: e.Bonus, SolvedCount: e.SolvedCount,
				Members: []string{e.Username}, LastSolveAt: e.LastSolveAt,
				AccountCode: e.AccountCode, UserID: e.UserID,
			}
		}
		return map[string]any{"mode": "individual", "items": items}, nil
	}
	// team 模式：TeamID=0 的个人参赛者各成一行（队名=用户名）。
	type teamAgg struct {
		teamID  uint
		name    *string
		score   int
		bonus   int
		solved  int
		members []string
		last    time.Time
	}
	teamAggs := map[uint]*teamAgg{}
	for _, p := range parts {
		var key uint
		var name *string
		if p.TeamID > 0 {
			key = p.TeamID
			if n, ok := teamNames[p.TeamID]; ok {
				name = &n
			}
		} else {
			key = 1<<31 - p.UserID // 个人行独立键（避开真实 team id 区间）
			n := userNames[p.UserID]
			name = &n
		}
		t := teamAggs[key]
		if t == nil {
			t = &teamAgg{teamID: p.TeamID, name: name}
			teamAggs[key] = t
		}
		t.members = append(t.members, userNames[p.UserID])
		if a := agg[p.UserID]; a != nil {
			t.score += a.score
			t.bonus += a.bonus
			t.solved += a.solved
			if a.last.After(t.last) {
				t.last = a.last
			}
		}
	}
	entries := make([]TeamLeaderboardEntry, 0, len(teamAggs))
	for _, t := range teamAggs {
		var last *string
		if !t.last.IsZero() {
			ls := t.last.Format(time.RFC3339)
			last = &ls
		}
		entries = append(entries, TeamLeaderboardEntry{
			TeamID: t.teamID, TeamName: t.name, Score: t.score, Bonus: t.bonus,
			SolvedCount: t.solved, Members: t.members, LastSolveAt: last,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score
		}
		if entries[i].LastSolveAt != entries[j].LastSolveAt {
			li, lj := entries[i].LastSolveAt, entries[j].LastSolveAt
			if li == nil {
				return false
			}
			if lj == nil {
				return true
			}
			return *li < *lj
		}
		return entries[i].SolvedCount > entries[j].SolvedCount
	})
	rankTeamEntries(entries)
	return map[string]any{"mode": "team", "items": entries}, nil
}

// rankIndividualEntries 个人榜并列同名次（与全局榜同语义：排序后相邻键不同则名次推进）。
func rankIndividualEntries(entries []CompLeaderboardEntry) {
	rank := 1
	for i := range entries {
		if i > 0 && (entries[i].Score != entries[i-1].Score ||
			(entries[i].LastSolveAt == nil) != (entries[i-1].LastSolveAt == nil) ||
			(entries[i].LastSolveAt != nil && entries[i-1].LastSolveAt != nil &&
				*entries[i].LastSolveAt != *entries[i-1].LastSolveAt)) {
			rank = i + 1
		}
		entries[i].Rank = rank
	}
}

func rankTeamEntries(entries []TeamLeaderboardEntry) {
	rank := 1
	for i := range entries {
		if i > 0 && (entries[i].Score != entries[i-1].Score ||
			(entries[i].LastSolveAt == nil) != (entries[i-1].LastSolveAt == nil) ||
			(entries[i].LastSolveAt != nil && entries[i-1].LastSolveAt != nil &&
				*entries[i].LastSolveAt != *entries[i-1].LastSolveAt)) {
			rank = i + 1
		}
		entries[i].Rank = rank
	}
}

// StandingsCSV 导出竞赛榜单 CSV（管理端）。individual 列：排名/用户名/账号编码/
// 队伍/得分/解题数/最近通关；team 列：排名/队伍/成员/得分/解题数/最近通关。
// 输出带 UTF-8 BOM，兼容 Excel 直接打开中文。
func (s *CompetitionService) StandingsCSV(ctx context.Context, id uint, mode string) ([]byte, string, error) {
	board, err := s.Leaderboard(ctx, id, mode)
	if err != nil {
		return nil, "", err
	}
	items, _ := board["items"].([]TeamLeaderboardEntry)
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	w := csv.NewWriter(&buf)
	lastSolve := func(item TeamLeaderboardEntry) string {
		if item.LastSolveAt == nil {
			return ""
		}
		return *item.LastSolveAt
	}
	if mode == "individual" {
		_ = w.Write([]string{"排名", "用户名", "账号编码", "队伍", "得分", "解题数", "最近通关"})
		for _, item := range items {
			team := ""
			if item.TeamName != nil {
				team = *item.TeamName
			}
			username := ""
			if len(item.Members) > 0 {
				username = item.Members[0]
			}
			_ = w.Write([]string{
				strconv.Itoa(item.Rank), username, item.AccountCode, team,
				strconv.Itoa(item.Score), strconv.Itoa(item.SolvedCount), lastSolve(item),
			})
		}
	} else {
		_ = w.Write([]string{"排名", "队伍", "成员", "得分", "解题数", "最近通关"})
		for _, item := range items {
			team := ""
			if item.TeamName != nil {
				team = *item.TeamName
			}
			_ = w.Write([]string{
				strconv.Itoa(item.Rank), team, strings.Join(item.Members, "、"),
				strconv.Itoa(item.Score), strconv.Itoa(item.SolvedCount), lastSolve(item),
			})
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), fmt.Sprintf("competition-%d-standings-%s.csv", id, mode), nil
}

// CtfAdjustmentCreate 管理端追加附加分入参（用户必须已在本场参赛）。
type CtfAdjustmentCreate struct {
	UserID uint   `json:"user_id"`
	Points int    `json:"points"`
	Reason string `json:"reason"`
}

// CtfAdjustmentItem 附加分明细（附用户名/队伍名，便于管理端直接展示）。
type CtfAdjustmentItem struct {
	model.CtfAdjustment
	Username string  `json:"username"`
	TeamName *string `json:"team_name"`
}

const maxCtfAdjustmentAbs = 1_000_000

// AddCtfAdjustment 追加一条附加分（append-only；不修改 CompetitionScore）。
// 仅 CTF；开赛后可用；用户必须已参赛；正向合计受 Competition.BonusCap 限制。
func (s *CompetitionService) AddCtfAdjustment(ctx context.Context, competitionID, actorID uint, in CtfAdjustmentCreate) (*CtfAdjustmentItem, error) {
	c, err := s.requireFormat(ctx, competitionID, model.CompetitionFormatCTF)
	if err != nil {
		return nil, err
	}
	if c.Status(s.now()) == model.CompetitionScheduled {
		return nil, fmt.Errorf("%w: adjustments are available after competition starts", ErrValidation)
	}
	if in.UserID == 0 {
		return nil, fmt.Errorf("%w: user_id is required", ErrValidation)
	}
	if in.Points == 0 {
		return nil, fmt.Errorf("%w: points cannot be zero", ErrValidation)
	}
	if in.Points > maxCtfAdjustmentAbs || in.Points < -maxCtfAdjustmentAbs {
		return nil, fmt.Errorf("%w: points out of range", ErrValidation)
	}
	reason := strings.TrimSpace(in.Reason)
	if len(reason) < 2 {
		return nil, fmt.Errorf("%w: reason is required", ErrValidation)
	}
	if len(reason) > 512 {
		return nil, fmt.Errorf("%w: reason is too long", ErrValidation)
	}
	part, err := s.comps.FindParticipant(ctx, competitionID, in.UserID)
	if err != nil {
		return nil, err
	}
	if part == nil {
		return nil, fmt.Errorf("%w: user is not a participant of this competition", ErrValidation)
	}
	if in.Points > 0 && c.BonusCap > 0 {
		used, err := s.comps.SumPositiveCtfAdjustments(ctx, competitionID, in.UserID)
		if err != nil {
			return nil, err
		}
		if used+in.Points > c.BonusCap {
			return nil, fmt.Errorf("%w: bonus cap %d exceeded (already %d)", ErrValidation, c.BonusCap, used)
		}
	}
	adj := &model.CtfAdjustment{
		CompetitionID: competitionID, UserID: in.UserID, TeamID: part.TeamID,
		Points: in.Points, Reason: reason, CreatedBy: actorID, CreatedAt: s.now(),
	}
	if err := s.comps.CreateCtfAdjustment(ctx, adj); err != nil {
		return nil, err
	}
	item := &CtfAdjustmentItem{CtfAdjustment: *adj}
	if u, err := s.users.FindByID(ctx, in.UserID); err == nil && u != nil {
		item.Username = u.Username
	}
	if part.TeamID > 0 {
		if teams, err := s.comps.ListTeams(ctx, competitionID); err == nil {
			for _, t := range teams {
				if t.ID == part.TeamID {
					name := t.Name
					item.TeamName = &name
					break
				}
			}
		}
	}
	return item, nil
}

// ListCtfAdjustments 管理端附加分明细（含用户名/队伍名）。
func (s *CompetitionService) ListCtfAdjustments(ctx context.Context, competitionID uint) ([]CtfAdjustmentItem, error) {
	if _, err := s.requireFormat(ctx, competitionID, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	adjustments, err := s.comps.ListCtfAdjustments(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	userIDs := make([]uint, 0, len(adjustments))
	seen := map[uint]bool{}
	for _, a := range adjustments {
		if !seen[a.UserID] {
			seen[a.UserID] = true
			userIDs = append(userIDs, a.UserID)
		}
	}
	userNames := map[uint]string{}
	if len(userIDs) > 0 {
		users, err := s.users.FindByIDs(ctx, userIDs...)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			userNames[u.ID] = u.Username
		}
	}
	teams, err := s.comps.ListTeams(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	teamNames := map[uint]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}
	items := make([]CtfAdjustmentItem, len(adjustments))
	for i, a := range adjustments {
		item := CtfAdjustmentItem{CtfAdjustment: a, Username: userNames[a.UserID]}
		if a.TeamID > 0 {
			if name, ok := teamNames[a.TeamID]; ok {
				item.TeamName = &name
			}
		}
		items[i] = item
	}
	return items, nil
}

// ---------- 晋级（来源榜单 → 邀请赛参赛名单） ----------

// QualificationCandidate 晋级预览中的一名中选者（按个人榜）。
type QualificationCandidate struct {
	UserID      uint    `json:"user_id"`
	Username    string  `json:"username"`
	AccountCode string  `json:"account_code"`
	TeamName    *string `json:"team_name"`
	Rank        int     `json:"rank"`
	Score       int     `json:"score"`
}

// QualificationPreview 晋级预览（不落库）。Threshold 语义随 Mode 变化：
// min_score=最低总分（含）；top_n=前 N 名；top_percent=前 N%。
type QualificationPreview struct {
	SourceCompetitionID   uint                     `json:"source_competition_id"`
	SourceCompetitionName string                   `json:"source_competition_name"`
	TargetCompetitionID   uint                     `json:"target_competition_id"`
	TargetCompetitionName string                   `json:"target_competition_name"`
	Mode                  model.QualificationMode  `json:"mode"`
	Threshold             int                      `json:"threshold"`
	TotalParticipants     int                      `json:"total_participants"`
	Candidates            []QualificationCandidate `json:"candidates"`
}

// QualificationApplyResult 应用结果。
type QualificationApplyResult struct {
	RuleID   uint                  `json:"rule_id"`
	Imported int                   `json:"imported"`
	Missing  []string              `json:"missing"`
	Preview  *QualificationPreview `json:"preview"`
}

// QualificationRuleItem 历史晋级规则（管理端展示）。
type QualificationRuleItem struct {
	model.QualificationRule
	SourceName string   `json:"source_name"`
	TargetName string   `json:"target_name"`
	Usernames  []string `json:"usernames"`
}

func (s *CompetitionService) qualificationPreview(ctx context.Context, sourceID, targetID uint, mode string, threshold int) (*QualificationPreview, error) {
	if sourceID == 0 || targetID == 0 {
		return nil, fmt.Errorf("%w: source_competition_id and target_competition_id are required", ErrValidation)
	}
	if sourceID == targetID {
		return nil, fmt.Errorf("%w: source and target competition must differ", ErrValidation)
	}
	source, err := s.requireFormat(ctx, sourceID, model.CompetitionFormatCTF)
	if err != nil {
		return nil, err
	}
	if source.Status(s.now()) == model.CompetitionScheduled {
		return nil, fmt.Errorf("%w: source competition has not started", ErrValidation)
	}
	target, err := s.find(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if target.Kind != model.CompetitionInvitational {
		return nil, fmt.Errorf("%w: qualification can only target an invitational competition", ErrValidation)
	}
	if target.Status(s.now()) != model.CompetitionScheduled {
		return nil, fmt.Errorf("%w: qualification target must be scheduled", ErrValidation)
	}
	m := model.QualificationMode(strings.TrimSpace(mode))
	if !m.Valid() {
		return nil, fmt.Errorf("%w: invalid qualification mode", ErrValidation)
	}
	switch m {
	case model.QualificationMinScore:
		if threshold < 0 {
			return nil, fmt.Errorf("%w: min_score cannot be negative", ErrValidation)
		}
	case model.QualificationTopN:
		if threshold <= 0 {
			return nil, fmt.Errorf("%w: top_n must be positive", ErrValidation)
		}
	case model.QualificationTopPercent:
		if threshold <= 0 || threshold > 100 {
			return nil, fmt.Errorf("%w: top_percent must be in 1..100", ErrValidation)
		}
	}
	board, err := s.Leaderboard(ctx, sourceID, "individual")
	if err != nil {
		return nil, err
	}
	items, _ := board["items"].([]TeamLeaderboardEntry)
	total := len(items)
	quota := 0
	if m == model.QualificationTopPercent {
		quota = (total*threshold + 99) / 100
	}
	candidates := make([]QualificationCandidate, 0, total)
	for _, item := range items {
		eligible := false
		switch m {
		case model.QualificationMinScore:
			eligible = item.Score >= threshold
		case model.QualificationTopN:
			eligible = item.Score > 0 && item.Rank <= threshold
		case model.QualificationTopPercent:
			eligible = item.Score > 0 && item.Rank <= quota
		}
		if !eligible {
			continue
		}
		username := ""
		if len(item.Members) > 0 {
			username = item.Members[0]
		}
		candidates = append(candidates, QualificationCandidate{
			UserID: item.UserID, Username: username, AccountCode: item.AccountCode,
			TeamName: item.TeamName, Rank: item.Rank, Score: item.Score,
		})
	}
	return &QualificationPreview{
		SourceCompetitionID: source.ID, SourceCompetitionName: source.Name,
		TargetCompetitionID: target.ID, TargetCompetitionName: target.Name,
		Mode: m, Threshold: threshold, TotalParticipants: total, Candidates: candidates,
	}, nil
}

// PreviewQualification 按来源竞赛个人榜预览晋级名单。
func (s *CompetitionService) PreviewQualification(ctx context.Context, sourceID, targetID uint, mode string, threshold int) (*QualificationPreview, error) {
	return s.qualificationPreview(ctx, sourceID, targetID, mode, threshold)
}

// ApplyQualification 应用晋级：把中选者导入目标邀请赛（生成邀请码并投递邀请），
// 落晋级规则 + 名单快照。同一来源→目标只允许应用一次（重复应用 422）。
func (s *CompetitionService) ApplyQualification(ctx context.Context, actorID, sourceID, targetID uint, mode string, threshold int) (*QualificationApplyResult, error) {
	preview, err := s.qualificationPreview(ctx, sourceID, targetID, mode, threshold)
	if err != nil {
		return nil, err
	}
	if len(preview.Candidates) == 0 {
		return nil, fmt.Errorf("%w: no participant matches the qualification rule", ErrValidation)
	}
	rules, err := s.comps.ListQualificationRules(ctx, targetID)
	if err != nil {
		return nil, err
	}
	for _, r := range rules {
		if r.SourceCompetitionID == sourceID {
			return nil, fmt.Errorf("%w: qualification from this source has already been applied", ErrValidation)
		}
	}
	usernames := make([]string, 0, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		usernames = append(usernames, candidate.Username)
	}
	imported, err := s.ImportParticipants(ctx, targetID, ParticipantImportRequest{Individuals: usernames})
	if err != nil {
		return nil, err
	}
	now := s.now()
	rule := &model.QualificationRule{
		SourceCompetitionID: sourceID, TargetCompetitionID: targetID,
		Mode: preview.Mode, Threshold: threshold, CreatedBy: actorID,
		CreatedAt: now, AppliedAt: &now,
	}
	if err := s.comps.CreateQualificationRule(ctx, rule); err != nil {
		return nil, err
	}
	entries := make([]model.QualificationEntry, 0, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		entries = append(entries, model.QualificationEntry{
			RuleID: rule.ID, SourceCompetitionID: sourceID, TargetCompetitionID: targetID,
			UserID: candidate.UserID, Rank: candidate.Rank, Score: candidate.Score,
			CreatedAt: now,
		})
	}
	if err := s.comps.CreateQualificationEntries(ctx, entries); err != nil {
		return nil, err
	}
	return &QualificationApplyResult{
		RuleID: rule.ID, Imported: len(imported.Participants), Missing: imported.Missing,
		Preview: preview,
	}, nil
}

// ListQualifications 目标竞赛历史晋级规则（含中选用户名）。
func (s *CompetitionService) ListQualifications(ctx context.Context, targetID uint) ([]QualificationRuleItem, error) {
	target, err := s.find(ctx, targetID)
	if err != nil {
		return nil, err
	}
	rules, err := s.comps.ListQualificationRules(ctx, targetID)
	if err != nil {
		return nil, err
	}
	out := make([]QualificationRuleItem, 0, len(rules))
	for _, rule := range rules {
		item := QualificationRuleItem{QualificationRule: rule, TargetName: target.Name}
		if source, err := s.comps.FindByID(ctx, rule.SourceCompetitionID); err == nil {
			item.SourceName = source.Name
		}
		entries, err := s.comps.ListQualificationEntries(ctx, rule.ID)
		if err != nil {
			return nil, err
		}
		userIDs := make([]uint, 0, len(entries))
		for _, entry := range entries {
			userIDs = append(userIDs, entry.UserID)
		}
		if len(userIDs) > 0 {
			users, err := s.users.FindByIDs(ctx, userIDs...)
			if err != nil {
				return nil, err
			}
			for _, u := range users {
				item.Usernames = append(item.Usernames, u.Username)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// ---------- 管理端：警告 / WP / 容器 ----------

// AdminWarnings 反作弊警告视图（原因/人员/队伍）。
type AdminWarningItem struct {
	model.CheatWarning
	Username string  `json:"username"`
	TeamID   *uint   `json:"team_id"`
	TeamName *string `json:"team_name"`
}

func (s *CompetitionService) AdminWarnings(ctx context.Context, id uint) ([]AdminWarningItem, error) {
	if _, err := s.requireFormat(ctx, id, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	warnings, err := s.comps.ListWarnings(ctx, id)
	if err != nil {
		return nil, err
	}
	teams, err := s.comps.ListTeams(ctx, id)
	if err != nil {
		return nil, err
	}
	teamNames := map[uint]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}
	userIDs := make([]uint, len(warnings))
	for i, w := range warnings {
		userIDs[i] = w.UserID
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, err
	}
	userNames := map[uint]string{}
	for _, u := range users {
		userNames[u.ID] = u.Username
	}
	items := make([]AdminWarningItem, len(warnings))
	for i, w := range warnings {
		item := AdminWarningItem{CheatWarning: w, Username: userNames[w.UserID]}
		if w.TeamID > 0 {
			tid := w.TeamID
			item.TeamID = &tid
			if n, ok := teamNames[w.TeamID]; ok {
				item.TeamName = &n
			}
		}
		items[i] = item
	}
	return items, nil
}

// AdminWriteups WP 提交情况（单位 = 题目 × 队伍/个人）：仅返回已提交条目，
// 前端按题目分组、与参赛名单对差集得未提交名单。
type AdminWriteupItem struct {
	ID             uint    `json:"id"`
	ChallengeID    uint    `json:"challenge_id"`
	ChallengeTitle string  `json:"challenge_title"`
	UserID         uint    `json:"user_id"`
	Username       string  `json:"username"`
	TeamName       *string `json:"team_name"`
	SubmittedAt    string  `json:"submitted_at"`
	Content        string  `json:"content"`
	ZipName        string  `json:"zip_name"`
}

func (s *CompetitionService) AdminWriteups(ctx context.Context, id uint) ([]AdminWriteupItem, error) {
	if _, err := s.requireFormat(ctx, id, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	teams, err := s.comps.ListTeams(ctx, id)
	if err != nil {
		return nil, err
	}
	teamNames := map[uint]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}
	writeups, err := s.comps.ListWriteups(ctx, id)
	if err != nil {
		return nil, err
	}
	// 题目标题与修改人用户名批量补全。
	chalIDs := make([]uint, 0, len(writeups))
	userIDs := make([]uint, 0, len(writeups))
	for _, w := range writeups {
		chalIDs = append(chalIDs, w.ChallengeID)
		userIDs = append(userIDs, w.UserID)
	}
	chalTitles := map[uint]string{}
	if len(chalIDs) > 0 {
		chals, err := s.challenges.FindByIDs(ctx, chalIDs...)
		if err != nil {
			return nil, err
		}
		for _, c := range chals {
			chalTitles[c.ID] = c.Title
		}
	}
	userNames := map[uint]string{}
	if len(userIDs) > 0 {
		users, err := s.users.FindByIDs(ctx, userIDs...)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			userNames[u.ID] = u.Username
		}
	}
	items := make([]AdminWriteupItem, 0, len(writeups))
	for _, w := range writeups {
		item := AdminWriteupItem{
			ID: w.ID, ChallengeID: w.ChallengeID, ChallengeTitle: chalTitles[w.ChallengeID],
			UserID: w.UserID, Username: userNames[w.UserID],
			SubmittedAt: w.UpdatedAt.Format(time.RFC3339),
			Content:     w.Content, ZipName: w.ZipName,
		}
		if w.TeamID > 0 {
			if n, ok := teamNames[w.TeamID]; ok {
				item.TeamName = &n
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// OpenWriteupZip 管理端下载 WP 附带 zip（打开存储对象；未上传 404）。
// 返回 (流, 下载展示名, 字节数)；流由调用方关闭。
func (s *CompetitionService) OpenWriteupZip(ctx context.Context, competitionID, writeupID uint) (io.ReadCloser, string, int64, error) {
	if _, err := s.requireFormat(ctx, competitionID, model.CompetitionFormatCTF); err != nil {
		return nil, "", 0, err
	}
	w, err := s.comps.FindWriteup(ctx, writeupID)
	if err != nil {
		return nil, "", 0, err
	}
	if w == nil || w.CompetitionID != competitionID || w.ZipFile == "" {
		return nil, "", 0, ErrNotFound
	}
	r, size, err := s.writeups.Open(ctx, w.ZipFile)
	if err != nil {
		return nil, "", 0, ErrNotFound
	}
	return r, w.ZipName, size, nil
}

// MyWriteups 本队/本人全部 WP（结束后的 WP 标签页数据源）。
type MyWriteupItem struct {
	ChallengeID uint   `json:"challenge_id"`
	ContentMd   string `json:"content_md"`
	ZipName     string `json:"zip_name"`
	UpdatedAt   string `json:"updated_at"`
	UpdatedBy   string `json:"updated_by"`
}

func (s *CompetitionService) MyWriteups(ctx context.Context, userID, id uint) ([]MyWriteupItem, error) {
	c, part, err := s.requireEntered(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if err := requireCompetitionFormat(c, model.CompetitionFormatCTF); err != nil {
		return nil, err
	}
	writeups, err := s.comps.ListWriteups(ctx, id)
	if err != nil {
		return nil, err
	}
	userIDs := make([]uint, 0, len(writeups))
	for _, w := range writeups {
		if w.TeamID == part.TeamID {
			userIDs = append(userIDs, w.UserID)
		}
	}
	userNames := map[uint]string{}
	if len(userIDs) > 0 {
		users, err := s.users.FindByIDs(ctx, userIDs...)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			userNames[u.ID] = u.Username
		}
	}
	items := make([]MyWriteupItem, 0, len(writeups))
	for _, w := range writeups {
		if w.TeamID != part.TeamID {
			continue
		}
		items = append(items, MyWriteupItem{
			ChallengeID: w.ChallengeID, ContentMd: w.Content, ZipName: w.ZipName,
			UpdatedAt: w.UpdatedAt.Format(time.RFC3339), UpdatedBy: userNames[w.UserID],
		})
	}
	return items, nil
}

// SaveWriteup 提交/修改某题 WP 文字（比赛结束后；队伍模式全队一份，
// 任一队员可改；文字与 zip 至少保留一项，全空 422）。
func (s *CompetitionService) SaveWriteup(ctx context.Context, userID, id, challengeID uint, contentMd string) error {
	c, part, err := s.requireEntered(ctx, userID, id)
	if err != nil {
		return err
	}
	if err := requireCompetitionFormat(c, model.CompetitionFormatCTF); err != nil {
		return err
	}
	if c.Status(s.now()) != model.CompetitionEnded {
		return fmt.Errorf("%w: writeup is accepted only after competition ends", ErrValidation)
	}
	if err := s.requireBoundChallenge(ctx, id, challengeID); err != nil {
		return err
	}
	// 既有 zip 保留判定：文字清空且原份无 zip → 拒绝。
	existing, err := s.findWriteup(ctx, id, challengeID, part.TeamID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(contentMd) == "" && (existing == nil || existing.ZipFile == "") {
		return fmt.Errorf("%w: content_md is required when no zip uploaded", ErrValidation)
	}
	return s.comps.UpsertWriteup(ctx, &model.Writeup{
		CompetitionID: id, ChallengeID: challengeID, TeamID: part.TeamID, UserID: userID,
		Content: contentMd,
		ZipFile: existingZip(existing), ZipName: existingZipName(existing),
	})
}

// UploadWriteupZip 上传/替换某题 WP 附带 zip（≤MaxWriteupZipBytes、需为合法 zip；
// 覆盖旧包；不影响已提交文字）。
func (s *CompetitionService) UploadWriteupZip(ctx context.Context, userID, id, challengeID uint, filename string, r io.Reader) (string, error) {
	c, part, err := s.requireEntered(ctx, userID, id)
	if err != nil {
		return "", err
	}
	if err := requireCompetitionFormat(c, model.CompetitionFormatCTF); err != nil {
		return "", err
	}
	if c.Status(s.now()) != model.CompetitionEnded {
		return "", fmt.Errorf("%w: writeup is accepted only after competition ends", ErrValidation)
	}
	if err := s.requireBoundChallenge(ctx, id, challengeID); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxWriteupZipBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: read zip", ErrValidation)
	}
	if len(data) > MaxWriteupZipBytes {
		return "", fmt.Errorf("%w: zip exceeds %d bytes", ErrValidation, MaxWriteupZipBytes)
	}
	if len(data) < 4 || !bytes.Equal(data[:4], []byte("PK\x03\x04")) {
		return "", fmt.Errorf("%w: not a valid zip archive", ErrValidation)
	}
	existing, err := s.findWriteup(ctx, id, challengeID, part.TeamID)
	if err != nil {
		return "", err
	}
	name := sanitizeZipName(filename)
	// 对象键 = <compID>/<wpID>.zip：同一份 WP 永远同一路径，覆盖即替换。
	rel := fmt.Sprintf("%d/%d.zip", id, existingID(existing))
	if err := s.writeups.Save(ctx, rel, data); err != nil {
		return "", err
	}
	// 旧包在其他路径（历史上传）时清理，避免孤儿对象。
	if existing != nil && existing.ZipFile != "" && existing.ZipFile != rel {
		if err := s.writeups.Remove(ctx, existing.ZipFile); err != nil {
			log.Printf("competition: remove stale writeup %q failed: %v", existing.ZipFile, err)
		}
	}
	if err := s.comps.UpsertWriteup(ctx, &model.Writeup{
		CompetitionID: id, ChallengeID: challengeID, TeamID: part.TeamID, UserID: userID,
		Content: existingContent(existing), ZipFile: rel, ZipName: name,
	}); err != nil {
		return "", err
	}
	return name, nil
}

// requireBoundChallenge 校验题目属于该竞赛（防跨竞赛提交 WP）。
func (s *CompetitionService) requireBoundChallenge(ctx context.Context, id, challengeID uint) error {
	ids, err := s.comps.ListBoundChallengeIDs(ctx, id)
	if err != nil {
		return err
	}
	for _, cid := range ids {
		if cid == challengeID {
			return nil
		}
	}
	return ErrNotFound
}

// findWriteup 定位本单位（队伍/个人）某题的 WP（nil = 无）。
func (s *CompetitionService) findWriteup(ctx context.Context, id, challengeID, teamID uint) (*model.Writeup, error) {
	writeups, err := s.comps.ListWriteups(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, w := range writeups {
		if w.ChallengeID == challengeID && w.TeamID == teamID {
			return &w, nil
		}
	}
	return nil, nil
}

func existingID(w *model.Writeup) uint {
	if w == nil {
		return 0
	}
	return w.ID
}

func existingZip(w *model.Writeup) string {
	if w == nil {
		return ""
	}
	return w.ZipFile
}

func existingZipName(w *model.Writeup) string {
	if w == nil {
		return ""
	}
	return w.ZipName
}

func existingContent(w *model.Writeup) string {
	if w == nil {
		return ""
	}
	return w.Content
}

// sanitizeZipName 保留原文件名供下载展示：仅白名单字符折叠为 -，
// 杜绝路径穿越；空名回退 writeup.zip。
func sanitizeZipName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	out := zipNamePattern.ReplaceAllString(base, "-")
	out = strings.Trim(out, "-.")
	if out == "" {
		out = "writeup.zip"
	}
	return out
}

var zipNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_.\x{4e00}-\x{9fa5}]+`)

// ---------- 工具 ----------

// randomInviteCode 邀请码：16 位大写十六进制（邀请码即能力凭证，仅本人邮箱可见）。
func randomInviteCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}
