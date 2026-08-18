package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const teamInvitationLifetime = 7 * 24 * time.Hour

type CreateTeamRequest struct {
	Name           string `json:"name"`
	IdempotencyKey string `json:"-"`
}

type RenameTeamRequest struct {
	Name string `json:"name"`
}

type CreateTeamInvitationRequest struct {
	Email string               `json:"email"`
	Role  model.TeamMemberRole `json:"role"`
}

type CreateTeamInvitationResult struct {
	Invitation  model.TeamInvitation `json:"invitation"`
	AcceptToken string               `json:"acceptToken"`
}

type AcceptTeamInvitationRequest struct {
	Token string `json:"token"`
}

type UpdateTeamMemberRequest struct {
	Role                           model.TeamMemberRole `json:"role"`
	MonthlyCreditLimitMicrocredits *int64               `json:"monthlyCreditLimitMicrocredits"`
	ExpectedUpdatedAt              time.Time            `json:"expectedUpdatedAt"`
}

type TeamSubscriptionView struct {
	PlanID                    string     `json:"planId"`
	PlanName                  string     `json:"planName"`
	PlanTier                  string     `json:"planTier"`
	SeatLimit                 int        `json:"seatLimit"`
	EndsAt                    *time.Time `json:"endsAt,omitempty"`
	UnlimitedTaskQueue        bool       `json:"unlimitedTaskQueue"`
	TeamStorageBytes          int64      `json:"teamStorageBytes"`
	SharedAssetsEnabled       bool       `json:"sharedAssetsEnabled"`
	ProjectPermissionsEnabled bool       `json:"projectPermissionsEnabled"`
	InvoicingEnabled          bool       `json:"invoicingEnabled"`
	CommercialUseEnabled      bool       `json:"commercialUseEnabled"`
}

type TeamSummary struct {
	Team                   model.Team            `json:"team"`
	CurrentRole            model.TeamMemberRole  `json:"currentRole"`
	Capabilities           TeamCapabilities      `json:"capabilities"`
	SeatUsed               int                   `json:"seatUsed"`
	InvitationSeatReserved int                   `json:"invitationSeatReserved"`
	Subscription           *TeamSubscriptionView `json:"subscription,omitempty"`
	AvailableMicrocredits  int64                 `json:"availableMicrocredits"`
	ReservedMicrocredits   int64                 `json:"reservedMicrocredits"`
	StorageUsedBytes       int64                 `json:"storageUsedBytes"`
}

type TeamMemberView struct {
	repository.TeamMemberRecord
	CanRemove bool `json:"canRemove"`
}

type TeamDetail struct {
	Summary     TeamSummary                  `json:"summary"`
	Members     []TeamMemberView             `json:"members"`
	Invitations []model.TeamInvitation       `json:"invitations"`
	AuditEvents []repository.TeamAuditRecord `json:"auditEvents"`
}

type TeamWorkspace struct {
	Teams               []TeamSummary                             `json:"teams"`
	IncomingInvitations []repository.IncomingTeamInvitationRecord `json:"incomingInvitations"`
}

func (s *Service) CreateTeam(user *model.User, req CreateTeamRequest) (*model.Team, error) {
	name, err := normalizeTeamName(req.Name)
	if err != nil {
		return nil, err
	}
	idempotencyKey, err := normalizeTeamCreationIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	requestHash := sha256.Sum256([]byte(name))
	now := time.Now()
	team := &model.Team{
		ID: newID(), OwnerUserID: user.ID, CreationIdempotencyKey: idempotencyKey,
		CreationRequestHash: hex.EncodeToString(requestHash[:]), Name: name,
		Status: model.TeamStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	owner := &model.TeamMember{
		ID: newID(), TeamID: team.ID, UserID: user.ID,
		Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	metadata, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		return nil, err
	}
	audit := newTeamAuditEvent(team.ID, user.ID, "team.created", string(metadata), now)
	resolved, err := s.repo.CreateTeamIdempotent(team, owner, audit)
	if errors.Is(err, repository.ErrTeamCreationConflict) {
		return nil, Conflict("幂等键已用于不同的团队创建请求")
	}
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

func (s *Service) TeamWorkspace(user *model.User) (*TeamWorkspace, error) {
	now := time.Now()
	records, err := s.repo.TeamSummaryRecordsForUser(user.ID, now)
	if err != nil {
		return nil, err
	}
	summaries := make([]TeamSummary, 0, len(records))
	for index := range records {
		record := records[index]
		summary := TeamSummary{
			Team: model.Team{
				ID: record.ID, OwnerUserID: record.OwnerUserID, Name: record.Name,
				Status: record.Status, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
			},
			CurrentRole:            record.CurrentRole,
			Capabilities:           BuildTeamCapabilities(record.CurrentRole, false, false),
			SeatUsed:               record.SeatUsed,
			InvitationSeatReserved: record.InvitationSeatReserved,
			AvailableMicrocredits:  record.AvailableMicrocredits,
			ReservedMicrocredits:   record.ReservedMicrocredits,
			StorageUsedBytes:       record.StorageUsedBytes,
		}
		if record.SubscriptionID != "" {
			entitlement, entitlementErr := membershipEntitlementFromSubscription(model.MembershipSubscription{
				ID: record.SubscriptionID, TeamID: record.ID, PlanID: record.PlanID,
				Seats: record.SeatLimit, PlanSnapshotJSON: record.PlanSnapshotJSON, EndsAt: record.SubscriptionEndsAt,
			})
			if entitlementErr != nil {
				return nil, entitlementErr
			}
			summary.Subscription = &TeamSubscriptionView{
				PlanID: entitlement.PlanID, PlanName: entitlement.PlanName, PlanTier: entitlement.Tier,
				SeatLimit: record.SeatLimit, EndsAt: record.SubscriptionEndsAt,
				UnlimitedTaskQueue: entitlement.UnlimitedTaskQueue, TeamStorageBytes: entitlement.TeamStorageBytes,
				SharedAssetsEnabled: entitlement.SharedAssetsEnabled, ProjectPermissionsEnabled: entitlement.ProjectPermissionsEnabled,
				InvoicingEnabled: entitlement.InvoicingEnabled, CommercialUseEnabled: entitlement.CommercialUseEnabled,
			}
			summary.Capabilities = BuildTeamCapabilities(record.CurrentRole, entitlement.SharedAssetsEnabled, entitlement.ProjectPermissionsEnabled)
		}
		summaries = append(summaries, summary)
	}
	incoming := []repository.IncomingTeamInvitationRecord{}
	if email := normalizeEmail(user.Email); email != "" {
		incoming, err = s.repo.IncomingTeamInvitations(email, now)
		if err != nil {
			return nil, err
		}
	}
	workspace := &TeamWorkspace{Teams: summaries, IncomingInvitations: incoming}
	normalizeTeamWorkspaceCollections(workspace)
	return workspace, nil
}

func (s *Service) TeamDetail(user *model.User, teamID string) (*TeamDetail, error) {
	team, actor, err := s.teamAccess(user.ID, teamID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	summary, err := s.teamSummary(*team, actor.Role, now)
	if err != nil {
		return nil, err
	}
	members, err := s.repo.TeamMemberRecords(team.ID)
	if err != nil {
		return nil, err
	}
	invitations := []model.TeamInvitation{}
	if canManageTeam(actor.Role) {
		invitations, err = s.repo.PendingTeamInvitations(team.ID, now)
		if err != nil {
			return nil, err
		}
	}
	auditEvents := []repository.TeamAuditRecord{}
	if canManageTeam(actor.Role) {
		auditEvents, err = s.repo.TeamAuditRecords(team.ID, 30)
		if err != nil {
			return nil, err
		}
	}
	memberViews := make([]TeamMemberView, 0, len(members))
	for _, member := range members {
		memberViews = append(memberViews, TeamMemberView{TeamMemberRecord: member, CanRemove: canRemoveTeamMember(actor.Role, member.Role)})
	}
	detail := &TeamDetail{
		Summary: *summary, Members: memberViews,
		Invitations: invitations, AuditEvents: auditEvents,
	}
	normalizeTeamDetailCollections(detail)
	return detail, nil
}

func canRemoveTeamMember(actorRole model.TeamMemberRole, targetRole model.TeamMemberRole) bool {
	if targetRole == model.TeamMemberRoleOwner {
		return false
	}
	return actorRole == model.TeamMemberRoleOwner || (actorRole == model.TeamMemberRoleAdmin && targetRole == model.TeamMemberRoleMember)
}

func (s *Service) RenameTeam(user *model.User, teamID string, req RenameTeamRequest) (*model.Team, error) {
	team, actor, err := s.teamAccess(user.ID, teamID)
	if err != nil {
		return nil, err
	}
	if actor.Role != model.TeamMemberRoleOwner {
		return nil, Forbidden("只有团队所有者可以修改团队名称")
	}
	name, err := normalizeTeamName(req.Name)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	metadata, err := json.Marshal(struct {
		PreviousName string `json:"previousName"`
		Name         string `json:"name"`
	}{PreviousName: team.Name, Name: name})
	if err != nil {
		return nil, err
	}
	audit := newTeamAuditEvent(team.ID, user.ID, "team.renamed", string(metadata), now)
	if err := s.repo.RenameTeam(team.ID, name, audit, now); err != nil {
		return nil, err
	}
	team.Name = name
	team.UpdatedAt = now
	return team, nil
}

func (s *Service) CreateTeamInvitation(user *model.User, teamID string, req CreateTeamInvitationRequest) (*CreateTeamInvitationResult, error) {
	team, actor, err := s.teamAccess(user.ID, teamID)
	if err != nil {
		return nil, err
	}
	if !canManageTeam(actor.Role) {
		return nil, Forbidden("当前角色不能邀请团队成员")
	}
	if req.Role != model.TeamMemberRoleAdmin && req.Role != model.TeamMemberRoleMember {
		return nil, BadAuthRequest("邀请角色无效")
	}
	if actor.Role == model.TeamMemberRoleAdmin && req.Role != model.TeamMemberRoleMember {
		return nil, Forbidden("团队管理员只能邀请普通成员")
	}
	email := normalizeEmail(req.Email)
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	if email == normalizeEmail(user.Email) {
		return nil, BadAuthRequest("不能邀请自己加入当前团队")
	}
	targetUserID := ""
	target, targetErr := s.repo.UserByEmail(email)
	if targetErr == nil {
		targetUserID = target.ID
	} else if !errors.Is(targetErr, gorm.ErrRecordNotFound) {
		return nil, targetErr
	}
	rawToken, tokenHash, err := newTeamInvitationToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	invitation := &model.TeamInvitation{
		ID: newID(), TeamID: team.ID, InviterUserID: user.ID, Email: email,
		Role: req.Role, Status: model.TeamInvitationStatusPending,
		TokenHash: tokenHash, ExpiresAt: now.Add(teamInvitationLifetime),
		CreatedAt: now, UpdatedAt: now,
	}
	metadata, err := json.Marshal(struct {
		Email string               `json:"email"`
		Role  model.TeamMemberRole `json:"role"`
	}{Email: email, Role: req.Role})
	if err != nil {
		return nil, err
	}
	audit := newTeamAuditEvent(team.ID, user.ID, "invitation.created", string(metadata), now)
	if err := s.repo.CreateTeamInvitation(invitation, targetUserID, audit, now); err != nil {
		switch {
		case errors.Is(err, repository.ErrTeamSubscriptionRequired):
			return nil, &AuthError{Status: http.StatusConflict, Message: "团队尚未开通有效团队会员，不能邀请成员"}
		case errors.Is(err, repository.ErrTeamSeatLimitReached):
			return nil, &AuthError{Status: http.StatusConflict, Message: "团队席位已满，请先升级席位数"}
		case errors.Is(err, repository.ErrTeamMemberAlreadyActive):
			return nil, &AuthError{Status: http.StatusConflict, Message: "该用户已经是团队成员"}
		case errors.Is(err, repository.ErrTeamInvitationAlreadyExists):
			return nil, Conflict("该邮箱已有邀请记录，请显式重新生成邀请链接")
		default:
			return nil, err
		}
	}
	return &CreateTeamInvitationResult{Invitation: *invitation, AcceptToken: rawToken}, nil
}

func (s *Service) RegenerateTeamInvitation(user *model.User, teamID string, invitationID string) (*CreateTeamInvitationResult, error) {
	team, actor, err := s.teamAccess(user.ID, teamID)
	if err != nil {
		return nil, err
	}
	if !canManageTeam(actor.Role) {
		return nil, Forbidden("当前角色不能重新生成团队邀请")
	}
	rawToken, tokenHash, err := newTeamInvitationToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	audit := newTeamAuditEvent(team.ID, user.ID, "invitation.regenerated", "{}", now)
	invitation, err := s.repo.RegenerateTeamInvitation(team.ID, strings.TrimSpace(invitationID), user.ID, tokenHash, now.Add(teamInvitationLifetime), audit, now)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrTeamInvitationNotPending):
			return nil, Conflict("只有仍有效的待处理邀请可以重新生成")
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, &AuthError{Status: http.StatusNotFound, Message: "团队邀请不存在"}
		default:
			return nil, err
		}
	}
	return &CreateTeamInvitationResult{Invitation: *invitation, AcceptToken: rawToken}, nil
}

func (s *Service) AcceptTeamInvitationByID(user *model.User, invitationID string) (*model.TeamMember, error) {
	return s.acceptTeamInvitation(user, strings.TrimSpace(invitationID), "")
}

func (s *Service) AcceptTeamInvitationByToken(user *model.User, req AcceptTeamInvitationRequest) (*model.TeamMember, error) {
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return nil, BadAuthRequest("邀请凭证不能为空")
	}
	hash := sha256.Sum256([]byte(token))
	return s.acceptTeamInvitation(user, "", hex.EncodeToString(hash[:]))
}

func (s *Service) acceptTeamInvitation(user *model.User, invitationID string, tokenHash string) (*model.TeamMember, error) {
	email := normalizeEmail(user.Email)
	if email == "" {
		return nil, Forbidden("当前账号未绑定邮箱，不能接受团队邀请")
	}
	now := time.Now()
	audit := newTeamAuditEvent("", user.ID, "invitation.accepted", "{}", now)
	member, err := s.repo.AcceptTeamInvitation(invitationID, tokenHash, newID(), user.ID, email, audit, now)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrTeamInvitationEmailMismatch):
			return nil, Forbidden("该邀请不属于当前账号邮箱")
		case errors.Is(err, repository.ErrTeamInvitationNotPending):
			return nil, &AuthError{Status: http.StatusConflict, Message: "邀请已失效、已撤销或已处理"}
		case errors.Is(err, repository.ErrTeamSubscriptionRequired):
			return nil, &AuthError{Status: http.StatusConflict, Message: "团队会员已失效，暂时不能加入"}
		case errors.Is(err, repository.ErrTeamSeatLimitReached):
			return nil, &AuthError{Status: http.StatusConflict, Message: "团队席位已满，暂时不能加入"}
		case errors.Is(err, repository.ErrTeamMemberAlreadyActive):
			return nil, &AuthError{Status: http.StatusConflict, Message: "当前账号已经是团队成员"}
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, &AuthError{Status: http.StatusNotFound, Message: "团队邀请不存在"}
		default:
			return nil, err
		}
	}
	return member, nil
}

func (s *Service) UpdateTeamMember(user *model.User, teamID string, memberID string, req UpdateTeamMemberRequest) error {
	team, actor, err := s.teamAccess(user.ID, teamID)
	if err != nil {
		return err
	}
	if actor.Role != model.TeamMemberRoleOwner {
		return Forbidden("只有团队所有者可以调整成员角色")
	}
	if req.Role != model.TeamMemberRoleAdmin && req.Role != model.TeamMemberRoleMember {
		return BadAuthRequest("成员角色无效")
	}
	if req.ExpectedUpdatedAt.IsZero() {
		return BadAuthRequest("成员更新版本不能为空")
	}
	monthlyLimit := int64(0)
	if req.MonthlyCreditLimitMicrocredits != nil {
		monthlyLimit = *req.MonthlyCreditLimitMicrocredits
		if monthlyLimit < 0 {
			return BadAuthRequest("成员月度积分额度不能为负数")
		}
	}
	target, err := s.repo.TeamMemberByID(team.ID, memberID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &AuthError{Status: http.StatusNotFound, Message: "团队成员不存在"}
	}
	if err != nil {
		return err
	}
	if target.Role == model.TeamMemberRoleOwner {
		return Forbidden("团队所有者角色不可修改")
	}
	if req.MonthlyCreditLimitMicrocredits == nil {
		monthlyLimit = target.MonthlyCreditLimitMicrocredits
	}
	if target.Role == req.Role && target.MonthlyCreditLimitMicrocredits == monthlyLimit {
		return BadAuthRequest("成员角色和月度积分额度均未变化")
	}
	now := nextTeamMutationTime(target.UpdatedAt)
	metadata, err := json.Marshal(struct {
		PreviousRole                           model.TeamMemberRole `json:"previousRole"`
		Role                                   model.TeamMemberRole `json:"role"`
		PreviousMonthlyCreditLimitMicrocredits int64                `json:"previousMonthlyCreditLimitMicrocredits"`
		MonthlyCreditLimitMicrocredits         int64                `json:"monthlyCreditLimitMicrocredits"`
	}{PreviousRole: target.Role, Role: req.Role, PreviousMonthlyCreditLimitMicrocredits: target.MonthlyCreditLimitMicrocredits, MonthlyCreditLimitMicrocredits: monthlyLimit})
	if err != nil {
		return err
	}
	audit := newTeamAuditEvent(team.ID, user.ID, "member.policy_updated", string(metadata), now)
	audit.TargetUserID = target.UserID
	err = s.repo.UpdateTeamMemberPolicy(team.ID, memberID, req.Role, monthlyLimit, req.ExpectedUpdatedAt, audit, now)
	if errors.Is(err, repository.ErrTeamMemberVersionConflict) {
		return Conflict("团队成员信息已被其他操作更新，请刷新后重试")
	}
	return err
}

func nextTeamMutationTime(previous time.Time) time.Time {
	now := time.Now()
	minimum := previous.Add(time.Microsecond)
	if now.Before(minimum) {
		return minimum
	}
	return now
}

func (s *Service) RemoveTeamMember(user *model.User, teamID string, memberID string) error {
	team, actor, err := s.teamAccess(user.ID, teamID)
	if err != nil {
		return err
	}
	target, err := s.repo.TeamMemberByID(team.ID, memberID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &AuthError{Status: http.StatusNotFound, Message: "团队成员不存在"}
	}
	if err != nil {
		return err
	}
	if target.Role == model.TeamMemberRoleOwner {
		return Forbidden("团队所有者不能被移除")
	}
	if actor.Role == model.TeamMemberRoleMember {
		return Forbidden("当前角色不能移除团队成员")
	}
	if actor.Role == model.TeamMemberRoleAdmin && (target.Role != model.TeamMemberRoleMember || target.UserID == user.ID) {
		return Forbidden("团队管理员只能移除普通成员")
	}
	now := time.Now()
	audit := newTeamAuditEvent(team.ID, user.ID, "member.removed", "{}", now)
	return s.repo.RemoveTeamMember(team.ID, memberID, audit, now)
}

func (s *Service) LeaveTeam(user *model.User, teamID string) error {
	team, actor, err := s.teamAccess(user.ID, teamID)
	if err != nil {
		return err
	}
	if actor.Role == model.TeamMemberRoleOwner {
		return Forbidden("团队所有者不能退出团队")
	}
	now := time.Now()
	audit := newTeamAuditEvent(team.ID, user.ID, "member.left", "{}", now)
	return s.repo.RemoveTeamMember(team.ID, actor.ID, audit, now)
}

func (s *Service) RevokeTeamInvitation(user *model.User, teamID string, invitationID string) error {
	team, actor, err := s.teamAccess(user.ID, teamID)
	if err != nil {
		return err
	}
	if !canManageTeam(actor.Role) {
		return Forbidden("当前角色不能撤销团队邀请")
	}
	now := time.Now()
	audit := newTeamAuditEvent(team.ID, user.ID, "invitation.revoked", "{}", now)
	err = s.repo.RevokeTeamInvitation(team.ID, invitationID, audit, now)
	if errors.Is(err, repository.ErrTeamInvitationNotPending) {
		return &AuthError{Status: http.StatusConflict, Message: "邀请已失效或已处理"}
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &AuthError{Status: http.StatusNotFound, Message: "团队邀请不存在"}
	}
	return err
}

func (s *Service) teamAccess(userID string, teamID string) (*model.Team, *model.TeamMember, error) {
	team, err := s.repo.Team(strings.TrimSpace(teamID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, &AuthError{Status: http.StatusNotFound, Message: "团队不存在"}
	}
	if err != nil {
		return nil, nil, err
	}
	member, err := s.repo.TeamMemberForUser(team.ID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, Forbidden("当前用户不是团队成员")
	}
	if err != nil {
		return nil, nil, err
	}
	return team, member, nil
}

func (s *Service) teamSummary(team model.Team, role model.TeamMemberRole, now time.Time) (*TeamSummary, error) {
	members, err := s.repo.TeamMembers(team.ID)
	if err != nil {
		return nil, err
	}
	invitations, err := s.repo.PendingTeamInvitations(team.ID, now)
	if err != nil {
		return nil, err
	}
	summary := &TeamSummary{
		Team: team, CurrentRole: role,
		Capabilities: BuildTeamCapabilities(role, false, false),
		SeatUsed:     len(members), InvitationSeatReserved: len(invitations),
	}
	subscription, err := s.repo.ActiveTeamSubscription(team.ID, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return summary, nil
	}
	if err != nil {
		return nil, err
	}
	entitlement, err := membershipEntitlementFromSubscription(*subscription)
	if err != nil {
		return nil, err
	}
	summary.Subscription = &TeamSubscriptionView{
		PlanID: entitlement.PlanID, PlanName: entitlement.PlanName, PlanTier: entitlement.Tier,
		SeatLimit: subscription.Seats, EndsAt: subscription.EndsAt,
		UnlimitedTaskQueue: entitlement.UnlimitedTaskQueue, TeamStorageBytes: entitlement.TeamStorageBytes,
		SharedAssetsEnabled: entitlement.SharedAssetsEnabled, ProjectPermissionsEnabled: entitlement.ProjectPermissionsEnabled,
		InvoicingEnabled: entitlement.InvoicingEnabled, CommercialUseEnabled: entitlement.CommercialUseEnabled,
	}
	summary.Capabilities = BuildTeamCapabilities(role, entitlement.SharedAssetsEnabled, entitlement.ProjectPermissionsEnabled)
	account, err := s.repo.TeamCreditAccount(team.ID)
	if err != nil {
		return nil, err
	}
	summary.AvailableMicrocredits = account.AvailableMicrocredits
	summary.ReservedMicrocredits = account.ReservedMicrocredits
	summary.StorageUsedBytes, err = s.repo.TeamStoredResourceBytes(team.ID)
	if err != nil {
		return nil, err
	}
	return summary, nil
}

func normalizeTeamName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len([]rune(name)) > 80 {
		return "", BadAuthRequest("团队名称不能为空且最多 80 个字符")
	}
	return name, nil
}

func normalizeTeamCreationIdempotencyKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if key == "" || len(key) > 128 {
		return "", BadAuthRequest("创建团队需要 1 到 128 字节的幂等键")
	}
	for _, character := range key {
		if character < 0x21 || character > 0x7e {
			return "", BadAuthRequest("团队创建幂等键只能包含可见 ASCII 字符")
		}
	}
	return key, nil
}

func canManageTeam(role model.TeamMemberRole) bool {
	return role == model.TeamMemberRoleOwner || role == model.TeamMemberRoleAdmin
}

func newTeamAuditEvent(teamID string, actorUserID string, action string, metadataJSON string, now time.Time) *model.TeamAuditEvent {
	return &model.TeamAuditEvent{
		ID: newID(), TeamID: teamID, ActorUserID: actorUserID,
		Action: action, MetadataJSON: metadataJSON, CreatedAt: now,
	}
}

func newTeamInvitationToken() (string, string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes[:])
	hash := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(hash[:]), nil
}
