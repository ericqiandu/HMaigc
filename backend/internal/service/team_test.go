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

func TestTeamCreationIsAtomicAndAudited(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-a", "owner-a@example.com")

	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "  弘梦制作组  "})
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
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "席位团队"})
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
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "邀请团队"})
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

func TestTeamRolePermissionsAndOwnerInvariants(t *testing.T) {
	svc, db := newMembershipTestService(t)
	owner := createTeamTestUser(t, db, "team-owner-d", "owner-d@example.com")
	admin := createTeamTestUser(t, db, "team-admin-d", "admin-d@example.com")
	member := createTeamTestUser(t, db, "team-member-d", "member-d@example.com")
	team, err := svc.CreateTeam(owner, CreateTeamRequest{Name: "权限团队"})
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
