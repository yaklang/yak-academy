package service

import (
	"bytes"
	"context"
	"crypto/rand"
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

func (s *CompetitionService) Create(ctx context.Context, name, description string, start, end time.Time) (*CompetitionView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if !end.After(start) {
		return nil, fmt.Errorf("%w: end_time must be after start_time", ErrValidation)
	}
	// 起止时间统一以 UTC 落库：SQLite 按文本比较时间，混用时区偏移会导致
	// ActiveCompetitions 的区间判定失真（锁定/入场窗口错判）。
	c := &model.Competition{Name: name, Description: description, StartTime: start.UTC(), EndTime: end.UTC()}
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
	Name        *string
	Description *string
	StartTime   *time.Time
	EndTime     *time.Time
}

func (s *CompetitionService) Update(ctx context.Context, id uint, u CompetitionUpdate) (*CompetitionDetail, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.Name == nil && u.Description == nil && u.StartTime == nil && u.EndTime == nil {
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
	if _, err := s.find(ctx, id); err != nil {
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
	if _, err := s.find(ctx, id); err != nil {
		return nil, err
	}
	return s.listBoundItems(ctx, id)
}

// UnbindChallenges 解绑题目：running 竞赛不允许（榜单已产生，撤题破坏现场）。
func (s *CompetitionService) UnbindChallenges(ctx context.Context, id uint, slugs []string) (int64, error) {
	c, err := s.find(ctx, id)
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

// ---------- 管理端：参赛者与队伍批量导入 ----------

type ParticipantImportRequest struct {
	Teams       []TeamImport `json:"teams"`
	Individuals []string     `json:"individuals"`
}

type TeamImport struct {
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

type ParticipantImportResult struct {
	CreatedAccounts []CreatedAccount     `json:"created_accounts"`
	Participants    []InvitedParticipant `json:"participants"`
}

type CreatedAccount struct {
	Username        string `json:"username"`
	InitialPassword string `json:"initial_password"`
}

type InvitedParticipant struct {
	Username      string  `json:"username"`
	TeamName      *string `json:"team_name"`
	InviteCode    string  `json:"invite_code"`
	MailDelivered bool    `json:"mail_delivered"`
}

// ImportParticipants 批量导入：队伍/个人按用户名引用，不存在自动创建
// （随机初始密码，仅本次响应返回）。每人生成唯一邀请码并投递站内邀请邮件。
// 竞赛已结束拒绝导入；已参赛用户幂等跳过并沿用原邀请码（队伍按新归属更新不覆盖，
// 以首次导入为准——重复导入以补投邮件为主要用途）。
func (s *CompetitionService) ImportParticipants(ctx context.Context, id uint, req ParticipantImportRequest) (*ParticipantImportResult, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.Status(s.now()) == model.CompetitionEnded {
		return nil, fmt.Errorf("%w: competition has ended", ErrValidation)
	}
	// 1. 请求级校验（队伍名/成员名非空、请求内用户名不重复占位由建参赛记录唯一索引兜底）。
	for _, t := range req.Teams {
		if strings.TrimSpace(t.Name) == "" || len(t.Members) == 0 {
			return nil, fmt.Errorf("%w: team name and members are required", ErrValidation)
		}
		for _, m := range t.Members {
			if strings.TrimSpace(m) == "" {
				return nil, fmt.Errorf("%w: empty username", ErrValidation)
			}
		}
	}
	for _, m := range req.Individuals {
		if strings.TrimSpace(m) == "" {
			return nil, fmt.Errorf("%w: empty username", ErrValidation)
		}
	}
	// 2. 用户解析：存在即用，缺失建号（随机初始密码，仅本次响应返回）。
	userCache := map[string]*model.User{}
	created := []CreatedAccount{}
	allNames := []string{}
	for _, t := range req.Teams {
		allNames = append(allNames, t.Members...)
	}
	allNames = append(allNames, req.Individuals...)
	for _, name := range allNames {
		if _, ok := userCache[name]; ok {
			continue
		}
		u, err := s.users.FindByUsername(ctx, name)
		if err == nil {
			userCache[name] = u
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if s.createAccount == nil {
			return nil, fmt.Errorf("%w: account creation unavailable", ErrValidation)
		}
		pwd, err := randomPassword()
		if err != nil {
			return nil, err
		}
		acc, err := s.createAccount(ctx, name, pwd, model.GlobalRoleStudent)
		if err != nil {
			return nil, err
		}
		userCache[name] = acc
		created = append(created, CreatedAccount{Username: name, InitialPassword: pwd})
	}
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
	result := &ParticipantImportResult{
		CreatedAccounts: created,
		Participants:    []InvitedParticipant{},
	}
	for _, t := range req.Teams {
		for _, m := range t.Members {
			if err := s.importOne(ctx, c, userCache[m], teamID[strings.TrimSpace(t.Name)], result); err != nil {
				return nil, err
			}
		}
	}
	for _, m := range req.Individuals {
		if err := s.importOne(ctx, c, userCache[m], 0, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// importOne 单人导入：参赛记录存在 → 沿用邀请码；不存在 → 生成邀请码、
// 落库、投递站内邀请邮件（失败只标记 mail_delivered=false，不回滚——
// 管理端可从参赛者列表拿到邀请码重发）。
func (s *CompetitionService) importOne(ctx context.Context, c *model.Competition, u *model.User, teamID uint, result *ParticipantImportResult) error {
	if u == nil {
		return fmt.Errorf("%w: unknown user", ErrValidation)
	}
	part, err := s.comps.FindParticipant(ctx, c.ID, u.ID)
	if err != nil {
		return err
	}
	if part == nil {
		code, err := randomInviteCode()
		if err != nil {
			return err
		}
		part = &model.Participant{
			CompetitionID: c.ID,
			UserID:        u.ID,
			TeamID:        teamID,
			InviteCode:    code,
		}
		if err := s.comps.CreateParticipant(ctx, part); err != nil {
			return err
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
	result.Participants = append(result.Participants, InvitedParticipant{
		Username:      u.Username,
		TeamName:      teamName,
		InviteCode:    part.InviteCode,
		MailDelivered: delivered,
	})
	return nil
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
		out = append(out, TeamLeaderboardEntry{TeamID: t.ID, TeamName: &name, Members: members[t.ID]})
	}
	return out, nil
}

// LockedCompetition 入场锁检查（api 层中间件调用）：已入场且竞赛 running
// → (竞赛ID, true)。ended 后自动解锁。管理员不查此接口（中间件短路）。
func (s *CompetitionService) LockedCompetition(ctx context.Context, userID uint) (uint, bool) {
	active, err := s.comps.ActiveCompetitions(ctx, s.now())
	if err != nil {
		return 0, false // 查询失败不锁（可用性优先）
	}
	for _, c := range active {
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

// MyEntry 当前用户参赛状态：已入场且竞赛 running → locked=true
// （前端据此锁定平台入口；服务端所有竞赛接口同样校验）。
func (s *CompetitionService) MyEntry(ctx context.Context, userID uint) (*MyCompetitionEntry, error) {
	active, err := s.comps.ActiveCompetitions(ctx, s.now())
	if err != nil {
		return nil, err
	}
	for _, c := range active {
		part, err := s.comps.FindParticipant(ctx, c.ID, userID)
		if err != nil {
			return nil, err
		}
		if part == nil || part.EnteredAt == nil {
			continue
		}
		view := toView(&c, s.now())
		teamName, err := s.participantTeamName(ctx, part)
		if err != nil {
			return nil, err
		}
		return &MyCompetitionEntry{Competition: &view, Locked: true, Entered: true, TeamName: teamName}, nil
	}
	return &MyCompetitionEntry{Competition: nil, Locked: false, Entered: false}, nil
}

type MyCompetitionEntry struct {
	Competition *CompetitionView `json:"competition"`
	Locked      bool             `json:"locked"`
	Entered     bool             `json:"entered"`
	TeamName    *string          `json:"team_name"`
}

// Enter 凭邀请码入场：仅 running 竞赛有效（scheduled 未开赛、ended 已结束 → 422）。
// 入场后不可退出：锁定状态派生自 (entered 且 running)，直到比赛结束。
func (s *CompetitionService) Enter(ctx context.Context, userID, id uint, inviteCode string) (*MyCompetitionEntry, error) {
	c, err := s.find(ctx, id)
	if err != nil {
		return nil, err
	}
	if inviteCode == "" {
		return nil, fmt.Errorf("%w: invite_code is required", ErrValidation)
	}
	part, err := s.comps.FindParticipantByCode(ctx, inviteCode)
	if err != nil {
		return nil, err
	}
	if part == nil || part.CompetitionID != id || part.UserID != userID {
		return nil, fmt.Errorf("%w: invalid invite code", ErrValidation)
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
	return &MyCompetitionEntry{Competition: &view, Locked: true, Entered: true, TeamName: teamName}, nil
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
	WriteupSubmitted bool                       `json:"writeup_submitted"`
}

func (s *CompetitionService) View(ctx context.Context, userID, id uint) (*CompetitionHome, error) {
	c, part, err := s.requireEntered(ctx, userID, id)
	if err != nil {
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
	challenges := make([]CompetitionChallengeView, 0, len(chals))
	for _, chal := range chals {
		solved, err := s.submissions.HasCorrect(ctx, userID, chal.ID, id)
		if err != nil {
			return nil, err
		}
		challenges = append(challenges, CompetitionChallengeView{
			ID: chal.ID, Slug: chal.Slug, Title: chal.Title, Category: chal.Category,
			Difficulty: string(chal.Difficulty), Score: chal.Score,
			HasEnv: chal.Image != "", Description: chal.Description, Solved: solved,
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
		WriteupSubmitted: writeupSubmitted,
	}, nil
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
	SolvedCount int      `json:"solved_count"`
	Members     []string `json:"members"`
	LastSolveAt *string  `json:"last_solve_at"`
}

// CompLeaderboardEntry 个人榜条目（同分并列，先达者靠前）。
type CompLeaderboardEntry struct {
	Rank        int     `json:"rank"`
	UserID      uint    `json:"user_id"`
	Username    string  `json:"username"`
	TeamName    *string `json:"team_name"`
	Score       int     `json:"score"`
	SolvedCount int     `json:"solved_count"`
	LastSolveAt *string `json:"last_solve_at"`
}

// Leaderboard 竞赛排行榜：team 按战队聚合（个人参赛者各成一"独行侠"行），
// individual 按人。排序：score DESC → last_solve_at 早者靠前 → solved_count DESC。
// 数据源 CompetitionScore（派生表，只读聚合）。
func (s *CompetitionService) Leaderboard(ctx context.Context, id uint, mode string) (map[string]any, error) {
	if _, err := s.find(ctx, id); err != nil {
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
	for _, u := range users {
		userNames[u.ID] = u.Username
	}
	teamNames := map[uint]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
	}
	// 聚合到个人。
	type personAgg struct {
		userID, teamID uint
		score, solved  int
		last           time.Time
	}
	agg := map[uint]*personAgg{}
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
				UserID: a.userID, Username: userNames[a.userID], TeamName: teamName,
				Score: a.score, SolvedCount: a.solved, LastSolveAt: last,
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
				Score: e.Score, SolvedCount: e.SolvedCount,
				Members: []string{e.Username}, LastSolveAt: e.LastSolveAt,
			}
		}
		return map[string]any{"mode": "individual", "items": items}, nil
	}
	// team 模式：TeamID=0 的个人参赛者各成一行（队名=用户名）。
	type teamAgg struct {
		teamID  uint
		name    *string
		score   int
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
			TeamID: t.teamID, TeamName: t.name, Score: t.score,
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

// ---------- 管理端：警告 / WP / 容器 ----------

// AdminWarnings 反作弊警告视图（原因/人员/队伍）。
type AdminWarningItem struct {
	model.CheatWarning
	Username string  `json:"username"`
	TeamID   *uint   `json:"team_id"`
	TeamName *string `json:"team_name"`
}

func (s *CompetitionService) AdminWarnings(ctx context.Context, id uint) ([]AdminWarningItem, error) {
	if _, err := s.find(ctx, id); err != nil {
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
	if _, err := s.find(ctx, id); err != nil {
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
	_, part, err := s.requireEntered(ctx, userID, id)
	if err != nil {
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

// randomPassword 自动建号的随机初始密码（仅导入响应返回一次，管理员分发）。
func randomPassword() (string, error) {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
