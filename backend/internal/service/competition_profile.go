package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yaklang/yak-academy/backend/internal/model"
)

// 竞赛身份名册：外部报名渠道（学习强国/小程序/扫码）的实名信息按 CSV 导入，
// 选手也可在赛前/赛中补填本人信息。名册只服务赛务（签到、证书、晋级），
// 不改变平台账号资料；管理端可见全部字段，参赛者只能读写自己的记录。

const (
	maxRosterEntries     = 1000
	maxProfileRealName   = 64
	maxProfileSchool     = 128
	maxProfileStudentNo  = 64
	maxProfileEmail      = 128
	maxProfilePhone      = 32
	maxRosterEntryLength = 128
)

type CompetitionProfileInput struct {
	RealName  string `json:"real_name"`
	School    string `json:"school"`
	StudentNo string `json:"student_no"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

type CompetitionProfileView struct {
	CompetitionID uint       `json:"competition_id"`
	UserID        uint       `json:"user_id"`
	Username      string     `json:"username"`
	AccountCode   string     `json:"account_code"`
	HasProfile    bool       `json:"has_profile"`
	RealName      string     `json:"real_name"`
	School        string     `json:"school"`
	StudentNo     string     `json:"student_no"`
	Email         string     `json:"email"`
	Phone         string     `json:"phone"`
	UpdatedAt     *time.Time `json:"updated_at"`
}

type CompetitionRosterRow struct {
	CompetitionProfileView
	TeamName *string `json:"team_name"`
	Entered  bool    `json:"entered"`
}

type CompetitionRosterEntry struct {
	Entry     string `json:"entry"` // 账号编码（YA26-00001）或用户名
	RealName  string `json:"real_name"`
	School    string `json:"school"`
	StudentNo string `json:"student_no"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

type CompetitionRosterImportRequest struct {
	Entries []CompetitionRosterEntry `json:"entries"`
}

type CompetitionRosterImportResult struct {
	Imported     int                      `json:"imported"`
	Profiles     []CompetitionProfileView `json:"profiles"`
	Participants []InvitedParticipant     `json:"participants"`
	Missing      []string                 `json:"missing"`
}

func normalizeCompetitionProfile(in CompetitionProfileInput) (CompetitionProfileInput, error) {
	profile := CompetitionProfileInput{
		RealName:  strings.TrimSpace(in.RealName),
		School:    strings.TrimSpace(in.School),
		StudentNo: strings.TrimSpace(in.StudentNo),
		Email:     strings.TrimSpace(in.Email),
		Phone:     strings.TrimSpace(in.Phone),
	}
	if profile.RealName == "" {
		return CompetitionProfileInput{}, fmt.Errorf("%w: real_name is required", ErrValidation)
	}
	if len(profile.RealName) > maxProfileRealName {
		return CompetitionProfileInput{}, fmt.Errorf("%w: real_name must be <= %d characters", ErrValidation, maxProfileRealName)
	}
	if len(profile.School) > maxProfileSchool {
		return CompetitionProfileInput{}, fmt.Errorf("%w: school must be <= %d characters", ErrValidation, maxProfileSchool)
	}
	if len(profile.StudentNo) > maxProfileStudentNo {
		return CompetitionProfileInput{}, fmt.Errorf("%w: student_no must be <= %d characters", ErrValidation, maxProfileStudentNo)
	}
	if len(profile.Email) > maxProfileEmail {
		return CompetitionProfileInput{}, fmt.Errorf("%w: email must be <= %d characters", ErrValidation, maxProfileEmail)
	}
	if profile.Email != "" && (strings.ContainsAny(profile.Email, " \t\r\n") || !strings.Contains(profile.Email, "@")) {
		return CompetitionProfileInput{}, fmt.Errorf("%w: invalid email", ErrValidation)
	}
	if len(profile.Phone) > maxProfilePhone {
		return CompetitionProfileInput{}, fmt.Errorf("%w: phone must be <= %d characters", ErrValidation, maxProfilePhone)
	}
	return profile, nil
}

func (s *CompetitionService) profileView(profile *model.CompetitionProfile, user *model.User) CompetitionProfileView {
	view := CompetitionProfileView{}
	if user != nil {
		view.Username = user.Username
		view.AccountCode = user.AccountCode
		view.UserID = user.ID
	}
	if profile == nil {
		return view
	}
	updated := profile.UpdatedAt
	return CompetitionProfileView{
		CompetitionID: profile.CompetitionID,
		UserID:        profile.UserID,
		Username:      view.Username,
		AccountCode:   view.AccountCode,
		HasProfile:    true,
		RealName:      profile.RealName,
		School:        profile.School,
		StudentNo:     profile.StudentNo,
		Email:         profile.Email,
		Phone:         profile.Phone,
		UpdatedAt:     &updated,
	}
}

// Profile 参赛者本人名册（未填写时 has_profile=false，仍返回账号信息）。
func (s *CompetitionService) Profile(ctx context.Context, competitionID, userID uint) (*CompetitionProfileView, error) {
	if _, err := s.find(ctx, competitionID); err != nil {
		return nil, err
	}
	participant, err := s.comps.FindParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	if participant == nil {
		return nil, ErrNotFound
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	profile, err := s.comps.FindProfile(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	view := s.profileView(profile, user)
	view.CompetitionID = competitionID
	return &view, nil
}

// UpdateProfile 参赛者补填/修改本人名册（竞赛结束后冻结）。
func (s *CompetitionService) UpdateProfile(ctx context.Context, competitionID, userID uint, in CompetitionProfileInput) (*CompetitionProfileView, error) {
	c, err := s.find(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if c.Status(s.now()) == model.CompetitionEnded {
		return nil, fmt.Errorf("%w: competition has ended", ErrValidation)
	}
	participant, err := s.comps.FindParticipant(ctx, competitionID, userID)
	if err != nil {
		return nil, err
	}
	if participant == nil {
		return nil, ErrNotFound
	}
	profile, err := normalizeCompetitionProfile(in)
	if err != nil {
		return nil, err
	}
	now := s.now()
	record := &model.CompetitionProfile{
		CompetitionID: competitionID, UserID: userID,
		RealName: profile.RealName, School: profile.School, StudentNo: profile.StudentNo,
		Email: profile.Email, Phone: profile.Phone,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.comps.UpsertProfile(ctx, record); err != nil {
		return nil, err
	}
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, mapRecordNotFound(err)
	}
	view := s.profileView(record, user)
	return &view, nil
}

// Roster 管理端名册：全部参赛者（未填写名册的也返回，便于发现缺口）。
func (s *CompetitionService) Roster(ctx context.Context, competitionID uint) ([]CompetitionRosterRow, error) {
	if _, err := s.find(ctx, competitionID); err != nil {
		return nil, err
	}
	participants, _, err := s.comps.ListParticipants(ctx, competitionID, 0, 1<<30)
	if err != nil {
		return nil, err
	}
	profiles, err := s.comps.ListProfiles(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	profileByUser := make(map[uint]*model.CompetitionProfile, len(profiles))
	for i := range profiles {
		profileByUser[profiles[i].UserID] = &profiles[i]
	}
	teams, err := s.comps.ListTeams(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	teamNames := make(map[uint]string, len(teams))
	for _, team := range teams {
		teamNames[team.ID] = team.Name
	}
	userIDs := make([]uint, 0, len(participants))
	for _, participant := range participants {
		userIDs = append(userIDs, participant.UserID)
	}
	users, err := s.users.FindByIDs(ctx, userIDs...)
	if err != nil {
		return nil, err
	}
	userByID := make(map[uint]*model.User, len(users))
	for i := range users {
		userByID[users[i].ID] = &users[i]
	}
	rows := make([]CompetitionRosterRow, 0, len(participants))
	for _, participant := range participants {
		view := s.profileView(profileByUser[participant.UserID], userByID[participant.UserID])
		row := CompetitionRosterRow{
			CompetitionProfileView: view,
			Entered:                participant.EnteredAt != nil,
		}
		row.CompetitionID = competitionID
		row.UserID = participant.UserID
		if participant.TeamID > 0 {
			name := teamNames[participant.TeamID]
			row.TeamName = &name
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// ImportRoster 管理端 CSV 名册导入（仅邀请赛）：条目引用既有账号，缺失条目
// 不阻断并在 missing 回报；命中者写入实名信息并导入为参赛者（幂等）。
func (s *CompetitionService) ImportRoster(ctx context.Context, competitionID uint, req CompetitionRosterImportRequest) (*CompetitionRosterImportResult, error) {
	c, err := s.find(ctx, competitionID)
	if err != nil {
		return nil, err
	}
	if c.Kind != model.CompetitionInvitational {
		return nil, fmt.Errorf("%w: roster import is only available for invitational competitions", ErrValidation)
	}
	if c.Status(s.now()) == model.CompetitionEnded {
		return nil, fmt.Errorf("%w: competition has ended", ErrValidation)
	}
	if len(req.Entries) == 0 || len(req.Entries) > maxRosterEntries {
		return nil, fmt.Errorf("%w: entries must contain 1..%d rows", ErrValidation, maxRosterEntries)
	}

	type resolvedEntry struct {
		user    *model.User
		profile CompetitionProfileInput
	}
	resolved := make([]resolvedEntry, 0, len(req.Entries))
	seenUsers := map[uint]string{}
	seenMissing := map[string]struct{}{}
	missing := []string{}
	for _, entry := range req.Entries {
		ref := strings.TrimSpace(entry.Entry)
		if ref == "" || len(ref) > maxRosterEntryLength {
			return nil, fmt.Errorf("%w: roster entry is required and must be <= %d characters", ErrValidation, maxRosterEntryLength)
		}
		profile, err := normalizeCompetitionProfile(CompetitionProfileInput{
			RealName: entry.RealName, School: entry.School, StudentNo: entry.StudentNo,
			Email: entry.Email, Phone: entry.Phone,
		})
		if err != nil {
			return nil, fmt.Errorf("%w (entry %s)", err, ref)
		}
		user, err := s.resolveUser(ctx, ref)
		if err != nil {
			return nil, err
		}
		if user == nil {
			if _, done := seenMissing[ref]; !done {
				seenMissing[ref] = struct{}{}
				missing = append(missing, ref)
			}
			continue
		}
		if previous, done := seenUsers[user.ID]; done {
			return nil, fmt.Errorf("%w: duplicate roster entry for user %s (also %s)", ErrValidation, user.Username, previous)
		}
		seenUsers[user.ID] = ref
		resolved = append(resolved, resolvedEntry{user: user, profile: profile})
	}
	sort.Strings(missing)

	result := &CompetitionRosterImportResult{
		Profiles:     make([]CompetitionProfileView, 0, len(resolved)),
		Participants: make([]InvitedParticipant, 0, len(resolved)),
		Missing:      missing,
	}
	now := s.now()
	for _, item := range resolved {
		record := &model.CompetitionProfile{
			CompetitionID: competitionID, UserID: item.user.ID,
			RealName: item.profile.RealName, School: item.profile.School,
			StudentNo: item.profile.StudentNo, Email: item.profile.Email,
			Phone: item.profile.Phone, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.comps.UpsertProfile(ctx, record); err != nil {
			return nil, err
		}
		invited, err := s.importOne(ctx, c, item.user, 0)
		if err != nil {
			return nil, err
		}
		result.Profiles = append(result.Profiles, s.profileView(record, item.user))
		result.Participants = append(result.Participants, invited)
	}
	result.Imported = len(result.Profiles)
	return result, nil
}
