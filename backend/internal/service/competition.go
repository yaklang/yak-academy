package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yaklang/yak-academy/backend/internal/model"
	"github.com/yaklang/yak-academy/backend/internal/repo"
)

// CompetitionService 竞赛域业务：竞赛 CRUD、题目绑定、参赛者/队伍批量导入、
// 邀请邮件投递、入场与锁定、排行榜聚合、WP 提交。
// 判定与竞赛积分派生走 judge/deriver（event-sourcing-rules skill），
// 本服务不触碰派生表写入。
type CompetitionService struct {
	comps        repo.CompetitionRepo
	challenges   repo.ChallengeRepo
	users        repo.UserRepo
	instances    repo.LabInstanceRepo
	submissions  repo.SubmissionRepo
	deliverInvite func(ctx context.Context, userID uint, title, bodyMd string, competitionID uint, inviteCode string) error
	createAccount func(ctx context.Context, username, password string, role model.GlobalRole) (*model.User, error)
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
	c := &model.Competition{Name: name, Description: description, StartTime: start, EndTime: end}
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
	Teams       int64 `json:"teams"`
	Participants int64 `json:"participants"`
	Entered     int64 `json:"entered"`
	Challenges  int   `json:"challenges"`
	Instances   int64 `json:"instances"`
	Warnings    int64 `json:"warnings"`
	Writeups    int64 `json:"writeups"`
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
			Writeups:     int64(len(writeups)),
		},
	}, nil
}

// Update 修改配置：nil 字段不动。运行中的竞赛只允许延后 end_time 与改描述
// （改起止时间会影响已入场的锁定语义，收紧必须先确认）。
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
	// 已开始的竞赛：不允许把 start/finish 改回过去影响锁定语义——
	// end 只能延后，start 不动。
	if now.After(c.StartTime) {
		if u.StartTime != nil && !u.StartTime.Equal(c.StartTime) {
			return nil, fmt.Errorf("%w: cannot change start_time after competition started", ErrValidation)
		}
		if u.EndTime != nil && u.EndTime.Before(c.EndTime) && now.After(c.EndTime) {
			return nil, fmt.Errorf("%w: cannot shorten end_time after competition ended", ErrValidation)
		}
	}
	if u.StartTime != nil {
		fields["start_time"] = start
	}
	if u.EndTime != nil {
		fields["end_time"] = end
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

// ---------- 管理端：题目绑定与批量导入 ----------

// BindChallenges 三种入参统一入口：
//  1. 题目 JSON（对象或数组）→ 复用 content 管线导入后绑定；
//  2. {"slugs": [...]} → 绑定既有题目。
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
	Added int64         `json:"added"`
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
	CreatedAccounts []CreatedAccount `json:"created_accounts"`
	Participants    []InvitedParticipant `json:"participants"`
}

type CreatedAccount struct {
	Username        string `json:"username"`
	InitialPassword string `json:"initial_password"`
}

type InvitedParticipant struct {
	Username      string `json:"username"`
	TeamName      *string `json:"team_name"`
	InviteCode    string `json:"invite_code"`
	MailDelivered bool   `json:"mail_delivered"`
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
	ID           uint   `json:"id"`
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Category     string `json:"category"`
	Difficulty   string `json:"difficulty"`
	Score        int    `json:"score"`
	HasEnv       bool   `json:"has_env"`
	Description  string `json:"description_md"`
	Solved       bool   `json:"solved"`
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
	Competition      CompetitionView         `json:"competition"`
	TeamName         *string                 `json:"team_name"`
	Teammates        []string                `json:"teammates"`
	Challenges       []CompetitionChallengeView `json:"challenges"`
	MySubmissions    []MySubmissionView     `json:"my_submissions"`
	WriteupSubmitted bool                    `json:"writeup_submitted"`
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
	writeupSubmitted := false
	if c.Status(s.now()) == model.CompetitionEnded {
		writeups, err := s.comps.ListWriteups(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, w := range writeups {
			if w.UserID == userID {
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
	Rank       int      `json:"rank"`
	TeamID     uint     `json:"team_id"`
	TeamName   *string  `json:"team_name"`
	Score      int      `json:"score"`
	SolvedCount int     `json:"solved_count"`
	Members    []string `json:"members"`
	LastSolveAt *string `json:"last_solve_at"`
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

// AdminWriteups WP 提交情况：全部参赛者一行，未提交 submitted=false。
type AdminWriteupItem struct {
	UserID      uint    `json:"user_id"`
	Username    string  `json:"username"`
	TeamName    *string `json:"team_name"`
	Submitted   bool    `json:"submitted"`
	SubmittedAt *string `json:"submitted_at"`
	Content     string  `json:"content"`
}

func (s *CompetitionService) AdminWriteups(ctx context.Context, id uint) ([]AdminWriteupItem, error) {
	if _, err := s.find(ctx, id); err != nil {
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
	teamNames := map[uint]string{}
	for _, t := range teams {
		teamNames[t.ID] = t.Name
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
	writeups, err := s.comps.ListWriteups(ctx, id)
	if err != nil {
		return nil, err
	}
	wpByUser := map[uint]model.Writeup{}
	for _, w := range writeups {
		wpByUser[w.UserID] = w
	}
	items := make([]AdminWriteupItem, len(parts))
	for i, p := range parts {
		item := AdminWriteupItem{UserID: p.UserID, Username: userNames[p.UserID]}
		if p.TeamID > 0 {
			if n, ok := teamNames[p.TeamID]; ok {
				item.TeamName = &n
			}
		}
		if w, ok := wpByUser[p.UserID]; ok {
			item.Submitted = true
			ts := w.UpdatedAt.Format(time.RFC3339)
			item.SubmittedAt = &ts
			item.Content = w.Content
		}
		items[i] = item
	}
	return items, nil
}

// SubmitWriteup 参赛者提交 WP：仅竞赛结束后允许（未结束 422），可反复修改。
func (s *CompetitionService) SubmitWriteup(ctx context.Context, userID, id uint, contentMd string) error {
	c, _, err := s.requireEntered(ctx, userID, id)
	if err != nil {
		return err
	}
	if c.Status(s.now()) != model.CompetitionEnded {
		return fmt.Errorf("%w: writeup is accepted only after competition ends", ErrValidation)
	}
	if strings.TrimSpace(contentMd) == "" {
		return fmt.Errorf("%w: content_md is required", ErrValidation)
	}
	return s.comps.UpsertWriteup(ctx, &model.Writeup{
		CompetitionID: id, UserID: userID, Content: contentMd,
	})
}

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