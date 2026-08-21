package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func createTeamTestUser(t *testing.T, db *gorm.DB, id string, email string) *model.User {
	t.Helper()
	user := &model.User{
		ID: id, Username: id, DisplayName: id, Email: email,
		Role: model.UserRoleUser, Status: model.UserStatusActive,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	return user
}

func teamCreateRequest(name string) CreateTeamRequest {
	return CreateTeamRequest{Name: name, IdempotencyKey: "test-create-team-" + newID()}
}

func activateTeamTestSubscription(t *testing.T, db *gorm.DB, team *model.Team, owner *model.User, seats int) {
	t.Helper()
	plan := &model.MembershipPlan{
		ID: newID(), Code: "team-plan-" + team.ID, Name: "团队测试套餐",
		Tier: "pro", Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear,
		Currency: "CNY", ImageConcurrency: 6, VideoConcurrency: 4, MinSeats: 2, MaxSeats: 200, Enabled: true,
		UnlimitedTaskQueue: true, TeamStorageBytes: 130 * (1 << 40), SharedAssetsEnabled: true,
		ProjectPermissionsEnabled: true, InvoicingEnabled: true, CommercialUseEnabled: true,
	}
	if err := db.Create(plan).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	end := now.AddDate(1, 0, 0)
	snapshot, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	subscription := &model.MembershipSubscription{
		ID: newID(), UserID: owner.ID, TeamID: team.ID, PlanID: plan.ID,
		Status: model.MembershipSubscriptionActive, Seats: seats, StartsAt: now.Add(-time.Minute),
		EndsAt: &end, PlanSnapshotJSON: string(snapshot), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(subscription).Error; err != nil {
		t.Fatal(err)
	}
}

func TestTeamCollectionsSerializeAsArraysWhenEmpty(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-empty-owner", "team-empty-owner@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("空集合团队"))
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := svc.TeamWorkspace(owner)
	if err != nil {
		t.Fatal(err)
	}
	workspaceJSON, err := json.Marshal(workspace)
	if err != nil {
		t.Fatal(err)
	}
	var workspacePayload struct {
		Teams               []json.RawMessage `json:"teams"`
		IncomingInvitations []json.RawMessage `json:"incomingInvitations"`
	}
	if err := json.Unmarshal(workspaceJSON, &workspacePayload); err != nil {
		t.Fatal(err)
	}
	if workspacePayload.Teams == nil || workspacePayload.IncomingInvitations == nil {
		t.Fatalf("workspace collections must be arrays: %s", workspaceJSON)
	}

	detail, err := svc.TeamDetail(owner, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	var detailPayload struct {
		Members     []json.RawMessage `json:"members"`
		Invitations []json.RawMessage `json:"invitations"`
		AuditEvents []json.RawMessage `json:"auditEvents"`
	}
	if err := json.Unmarshal(detailJSON, &detailPayload); err != nil {
		t.Fatal(err)
	}
	if detailPayload.Members == nil || detailPayload.Invitations == nil || detailPayload.AuditEvents == nil {
		t.Fatalf("detail collections must be arrays: %s", detailJSON)
	}
}

func TestTeamCapabilitiesAreExplicitForOwnerAdminAndMember(t *testing.T) {
	owner := BuildTeamCapabilities(model.TeamMemberRoleOwner, true, true)
	if !owner.CanRenameTeam || !owner.CanManageSubscription || !owner.CanInviteMembers || !owner.CanManageMemberRoles || !owner.CanManageMemberCreditLimits || !owner.CanRemoveMembers || owner.CanLeaveTeam || !owner.CanManageProjects || !owner.CanUploadSharedAssets || !owner.CanViewAudit {
		t.Fatalf("owner capabilities = %#v", owner)
	}
	if len(owner.InviteRoles) != 2 || owner.InviteRoles[0] != model.TeamMemberRoleAdmin || owner.InviteRoles[1] != model.TeamMemberRoleMember {
		t.Fatalf("owner invite roles = %#v", owner.InviteRoles)
	}

	admin := BuildTeamCapabilities(model.TeamMemberRoleAdmin, true, true)
	if admin.CanRenameTeam || admin.CanManageSubscription || !admin.CanInviteMembers || admin.CanManageMemberRoles || admin.CanManageMemberCreditLimits || !admin.CanRemoveMembers || !admin.CanLeaveTeam || !admin.CanManageProjects || !admin.CanUploadSharedAssets || !admin.CanViewAudit {
		t.Fatalf("admin capabilities = %#v", admin)
	}
	if len(admin.InviteRoles) != 1 || admin.InviteRoles[0] != model.TeamMemberRoleMember {
		t.Fatalf("admin invite roles = %#v", admin.InviteRoles)
	}

	member := BuildTeamCapabilities(model.TeamMemberRoleMember, true, true)
	if member.CanRenameTeam || member.CanManageSubscription || member.CanInviteMembers || member.CanManageMemberRoles || member.CanManageMemberCreditLimits || member.CanRemoveMembers || !member.CanLeaveTeam || member.CanManageProjects || member.CanUploadSharedAssets || member.CanViewAudit {
		t.Fatalf("member capabilities = %#v", member)
	}
	if member.InviteRoles == nil || len(member.InviteRoles) != 0 {
		t.Fatalf("member invite roles = %#v", member.InviteRoles)
	}

	withoutEntitlements := BuildTeamCapabilities(model.TeamMemberRoleOwner, false, false)
	if withoutEntitlements.CanManageProjects || withoutEntitlements.CanUploadSharedAssets {
		t.Fatalf("disabled entitlement capabilities = %#v", withoutEntitlements)
	}
	if !canRemoveTeamMember(model.TeamMemberRoleOwner, model.TeamMemberRoleAdmin) || !canRemoveTeamMember(model.TeamMemberRoleAdmin, model.TeamMemberRoleMember) || canRemoveTeamMember(model.TeamMemberRoleAdmin, model.TeamMemberRoleAdmin) || canRemoveTeamMember(model.TeamMemberRoleMember, model.TeamMemberRoleMember) {
		t.Fatal("member-level removal capability matrix is incorrect")
	}
}

func TestTeamCreationIdempotencyReturnsSameFactAndRejectsChangedRequest(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-idempotent-owner", "team-idempotent-owner@example.com")

	first, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "幂等团队", IdempotencyKey: "create-team-request-1"})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "幂等团队", IdempotencyKey: "create-team-request-1"})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID {
		t.Fatalf("replayed team id = %q, want %q", replayed.ID, first.ID)
	}

	_, err = svc.CreateTeam(owner, CreateTeamRequest{Name: "不同团队", IdempotencyKey: "create-team-request-1"})
	requireAuthStatus(t, err, http.StatusConflict)

	var teamCount, ownerCount, auditCount int64
	if err := db.Model(&model.Team{}).Where("owner_user_id = ?", owner.ID).Count(&teamCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TeamMember{}).Where("team_id = ? AND role = ?", first.ID, model.TeamMemberRoleOwner).Count(&ownerCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TeamAuditEvent{}).Where("team_id = ? AND action = ?", first.ID, "team.created").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if teamCount != 1 || ownerCount != 1 || auditCount != 1 {
		t.Fatalf("facts team=%d owner=%d audit=%d", teamCount, ownerCount, auditCount)
	}
}

func TestTeamCreationRequiresAVisibleASCIIIdempotencyKey(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-idempotency-validation-owner", "team-idempotency-validation@example.com")
	for _, key := range []string{"", "  ", strings.Repeat("a", 129), "包含中文"} {
		_, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "无效幂等键", IdempotencyKey: key})
		requireAuthStatus(t, err, http.StatusBadRequest)
	}
}

func TestTeamCreationIsAtomicAndAudited(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-a", "owner-a@example.com")

	team, err := svc.CreateTeam(owner, teamCreateRequest("  弘梦制作组  "))
	if err != nil {
		t.Fatal(err)
	}
	if team.Name != "弘梦制作组" {
		t.Fatalf("team name = %q", team.Name)
	}
	var member model.TeamMember
	if err := db.First(&member, "team_id = ? AND user_id = ?", team.ID, owner.ID).Error; err != nil {
		t.Fatal(err)
	}
	if member.Role != model.TeamMemberRoleOwner || member.Status != model.TeamMemberStatusActive {
		t.Fatalf("unexpected owner member: %#v", member)
	}
	var audit model.TeamAuditEvent
	if err := db.First(&audit, "team_id = ? AND action = ?", team.ID, "team.created").Error; err != nil {
		t.Fatal(err)
	}
	if audit.ActorUserID != owner.ID || !strings.Contains(audit.MetadataJSON, "弘梦制作组") {
		t.Fatalf("unexpected audit: %#v", audit)
	}
}

func TestTeamInvitationRequiresSubscriptionAndReservesSeat(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-b", "owner-b@example.com")
	first := createTeamTestUser(t, db, "team-member-b1", "member-b1@example.com")
	second := createTeamTestUser(t, db, "team-member-b2", "member-b2@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("席位团队"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreateTeamInvitation(owner, team.ID, CreateTeamInvitationRequest{Email: first.Email, Role: model.TeamMemberRoleMember})
	requireAuthStatus(t, err, http.StatusConflict)

	activateTeamTestSubscription(t, db, team, owner, 2)
	firstInvitation, err := svc.CreateTeamInvitation(owner, team.ID, CreateTeamInvitationRequest{Email: first.Email, Role: model.TeamMemberRoleMember})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateTeamInvitation(owner, team.ID, CreateTeamInvitationRequest{Email: second.Email, Role: model.TeamMemberRoleMember})
	requireAuthStatus(t, err, http.StatusConflict)
	if err := svc.RevokeTeamInvitation(owner, team.ID, firstInvitation.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTeamInvitation(owner, team.ID, CreateTeamInvitationRequest{Email: second.Email, Role: model.TeamMemberRoleMember}); err != nil {
		t.Fatal(err)
	}

	detail, err := svc.TeamDetail(owner, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.SeatUsed != 1 || detail.Summary.InvitationSeatReserved != 1 || detail.Summary.Subscription == nil || detail.Summary.Subscription.SeatLimit != 2 {
		t.Fatalf("unexpected seat summary: %#v", detail.Summary)
	}
	workspace, err := svc.TeamWorkspace(owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Teams) != 1 || workspace.Teams[0].SeatUsed != 1 || workspace.Teams[0].InvitationSeatReserved != 1 || workspace.Teams[0].Subscription == nil {
		t.Fatalf("unexpected workspace summary: %#v", workspace.Teams)
	}
}

func TestTeamInvitationAcceptanceChecksEmailAndCannotRepeat(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-c", "owner-c@example.com")
	invitee := createTeamTestUser(t, db, "team-member-c", "member-c@example.com")
	other := createTeamTestUser(t, db, "team-other-c", "other-c@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("邀请团队"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 3)
	result, err := svc.CreateTeamInvitation(owner, team.ID, CreateTeamInvitationRequest{Email: invitee.Email, Role: model.TeamMemberRoleMember})
	if err != nil {
		t.Fatal(err)
	}
	var storedBeforeAccept model.TeamInvitation
	if err := db.First(&storedBeforeAccept, "id = ?", result.Invitation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedBeforeAccept.TokenHash == result.AcceptToken || len(storedBeforeAccept.TokenHash) != 64 {
		t.Fatal("invitation token was not stored as a SHA-256 hash")
	}

	_, err = svc.AcceptTeamInvitationByToken(other, AcceptTeamInvitationRequest{Token: result.AcceptToken})
	requireAuthStatus(t, err, http.StatusForbidden)
	member, err := svc.AcceptTeamInvitationByToken(invitee, AcceptTeamInvitationRequest{Token: result.AcceptToken})
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != model.TeamMemberRoleMember || member.UserID != invitee.ID {
		t.Fatalf("unexpected accepted member: %#v", member)
	}
	_, err = svc.AcceptTeamInvitationByToken(invitee, AcceptTeamInvitationRequest{Token: result.AcceptToken})
	requireAuthStatus(t, err, http.StatusConflict)

	var invitation model.TeamInvitation
	if err := db.First(&invitation, "id = ?", result.Invitation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if invitation.Status != model.TeamInvitationStatusAccepted || invitation.AcceptedByUserID != invitee.ID || invitation.AcceptedAt == nil {
		t.Fatalf("unexpected invitation state: %#v", invitation)
	}
	var auditCount int64
	if err := db.Model(&model.TeamAuditEvent{}).Where("team_id = ? AND action = ?", team.ID, "invitation.accepted").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("accepted audit count = %d, want 1", auditCount)
	}
	memberDetail, err := svc.TeamDetail(invitee, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(memberDetail.AuditEvents) != 0 || len(memberDetail.Invitations) != 0 {
		t.Fatalf("ordinary member received manager-only data: %#v", memberDetail)
	}
}

func TestPendingInvitationRequiresExplicitRegeneration(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-regenerate", "owner-regenerate@example.com")
	invitee := createTeamTestUser(t, db, "team-member-regenerate", "member-regenerate@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("邀请轮换团队"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 3)
	request := CreateTeamInvitationRequest{Email: invitee.Email, Role: model.TeamMemberRoleMember}
	first, err := svc.CreateTeamInvitation(owner, team.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTeamInvitation(owner, team.ID, request); err == nil {
		t.Fatal("duplicate pending invitation unexpectedly rotated its token")
	} else {
		requireAuthStatus(t, err, http.StatusConflict)
	}
	rotated, err := svc.RegenerateTeamInvitation(owner, team.ID, first.Invitation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Invitation.ID != first.Invitation.ID || rotated.AcceptToken == first.AcceptToken {
		t.Fatalf("rotated invitation = %#v", rotated)
	}
	_, err = svc.AcceptTeamInvitationByToken(invitee, AcceptTeamInvitationRequest{Token: first.AcceptToken})
	requireAuthStatus(t, err, http.StatusNotFound)
	if _, err := svc.AcceptTeamInvitationByToken(invitee, AcceptTeamInvitationRequest{Token: rotated.AcceptToken}); err != nil {
		t.Fatal(err)
	}
	var invitationCount int64
	if err := db.Model(&model.TeamInvitation{}).Where("team_id = ?", team.ID).Count(&invitationCount).Error; err != nil {
		t.Fatal(err)
	}
	if invitationCount != 1 {
		t.Fatalf("invitation count = %d, want 1", invitationCount)
	}
}

func TestTerminalInvitationCanBeCreatedAgainWithoutOverwritingHistory(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-reinvite", "owner-reinvite@example.com")
	invitee := createTeamTestUser(t, db, "team-member-reinvite", "member-reinvite@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("重新邀请团队"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 3)
	request := CreateTeamInvitationRequest{Email: invitee.Email, Role: model.TeamMemberRoleMember}
	first, err := svc.CreateTeamInvitation(owner, team.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeTeamInvitation(owner, team.ID, first.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateTeamInvitation(owner, team.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Invitation.ID == first.Invitation.ID {
		t.Fatal("terminal invitation history was overwritten")
	}
	if err := db.Model(&model.TeamInvitation{}).Where("id = ?", second.Invitation.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	third, err := svc.CreateTeamInvitation(owner, team.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	if third.Invitation.ID == second.Invitation.ID {
		t.Fatal("expired invitation history was overwritten")
	}
	var invitationCount int64
	if err := db.Model(&model.TeamInvitation{}).Where("team_id = ? AND email = ?", team.ID, invitee.Email).Count(&invitationCount).Error; err != nil {
		t.Fatal(err)
	}
	if invitationCount != 3 {
		t.Fatalf("invitation count = %d, want 3", invitationCount)
	}
	var expired model.TeamInvitation
	if err := db.First(&expired, "id = ?", second.Invitation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if expired.Status != model.TeamInvitationStatusExpired {
		t.Fatalf("expired invitation status = %q", expired.Status)
	}
}

func TestTeamMemberPolicyRejectsStaleVersion(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-version", "owner-version@example.com")
	memberUser := createTeamTestUser(t, db, "team-member-version", "member-version@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("成员并发团队"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 3)
	invitation, err := svc.CreateTeamInvitation(owner, team.ID, CreateTeamInvitationRequest{Email: memberUser.Email, Role: model.TeamMemberRoleMember})
	if err != nil {
		t.Fatal(err)
	}
	member, err := svc.AcceptTeamInvitationByToken(memberUser, AcceptTeamInvitationRequest{Token: invitation.AcceptToken})
	if err != nil {
		t.Fatal(err)
	}
	staleVersion := member.UpdatedAt
	winnerLimit := int64(2_000_000)
	if err := svc.UpdateTeamMember(owner, team.ID, member.ID, UpdateTeamMemberRequest{Role: model.TeamMemberRoleAdmin, MonthlyCreditLimitMicrocredits: &winnerLimit, ExpectedUpdatedAt: staleVersion}); err != nil {
		t.Fatal(err)
	}
	loserLimit := int64(3_000_000)
	err = svc.UpdateTeamMember(owner, team.ID, member.ID, UpdateTeamMemberRequest{Role: model.TeamMemberRoleMember, MonthlyCreditLimitMicrocredits: &loserLimit, ExpectedUpdatedAt: staleVersion})
	requireAuthStatus(t, err, http.StatusConflict)
	stored, err := svc.repo.TeamMemberByID(team.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Role != model.TeamMemberRoleAdmin || stored.MonthlyCreditLimitMicrocredits != winnerLimit {
		t.Fatalf("winner fact was overwritten: %#v", stored)
	}
}

func TestTeamRolePermissionsAndOwnerInvariants(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-d", "owner-d@example.com")
	admin := createTeamTestUser(t, db, "team-admin-d", "admin-d@example.com")
	member := createTeamTestUser(t, db, "team-member-d", "member-d@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("权限团队"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 4)
	for _, item := range []struct {
		user *model.User
		role model.TeamMemberRole
	}{
		{user: admin, role: model.TeamMemberRoleAdmin},
		{user: member, role: model.TeamMemberRoleMember},
	} {
		invitation, inviteErr := svc.CreateTeamInvitation(owner, team.ID, CreateTeamInvitationRequest{Email: item.user.Email, Role: item.role})
		if inviteErr != nil {
			t.Fatal(inviteErr)
		}
		if _, acceptErr := svc.AcceptTeamInvitationByToken(item.user, AcceptTeamInvitationRequest{Token: invitation.AcceptToken}); acceptErr != nil {
			t.Fatal(acceptErr)
		}
	}

	ownerMember, err := svc.repo.TeamMemberForUser(team.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveTeamMember(owner, team.ID, ownerMember.ID); err == nil {
		t.Fatal("owner was unexpectedly removable")
	}
	_, err = svc.RenameTeam(member, team.ID, RenameTeamRequest{Name: "越权改名"})
	requireAuthStatus(t, err, http.StatusForbidden)
	_, err = svc.CreateTeamInvitation(member, team.ID, CreateTeamInvitationRequest{Email: "member-target@example.com", Role: model.TeamMemberRoleMember})
	requireAuthStatus(t, err, http.StatusForbidden)
	_, err = svc.CreateTeamInvitation(admin, team.ID, CreateTeamInvitationRequest{Email: "admin-target@example.com", Role: model.TeamMemberRoleAdmin})
	requireAuthStatus(t, err, http.StatusForbidden)

	memberRecord, err := svc.repo.TeamMemberForUser(team.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RemoveTeamMember(admin, team.ID, memberRecord.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.TeamMemberForUser(team.ID, member.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("removed member query error = %v", err)
	}
}
