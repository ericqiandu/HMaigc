package repository

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/gorm"
)

func TestPostgresConcurrentTeamCreationCreatesOneCompleteFact(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureTeamIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	owner := &model.User{ID: "team-owner", Username: "team-owner", DisplayName: "Team Owner", Email: "team-owner@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create(owner).Error; err != nil {
		t.Fatal(err)
	}

	const contenders = 2
	start := make(chan struct{})
	results := make(chan *model.Team, contenders)
	errors := make(chan error, contenders)
	var wait sync.WaitGroup
	wait.Add(contenders)
	for index := 0; index < contenders; index++ {
		index := index
		go func() {
			defer wait.Done()
			<-start
			now := time.Now().UTC()
			teamID := "team-contender-" + string(rune('a'+index))
			team := &model.Team{ID: teamID, OwnerUserID: owner.ID, CreationIdempotencyKey: "team-create-pg-1", CreationRequestHash: "same-request-hash", Name: "并发团队", Status: model.TeamStatusActive, CreatedAt: now, UpdatedAt: now}
			member := &model.TeamMember{ID: "member-" + teamID, TeamID: teamID, UserID: owner.ID, Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
			audit := &model.TeamAuditEvent{ID: "audit-" + teamID, TeamID: teamID, ActorUserID: owner.ID, Action: "team.created", MetadataJSON: `{"name":"并发团队"}`, CreatedAt: now}
			resolved, err := New(db.Session(&gorm.Session{NewDB: true})).CreateTeamIdempotent(team, member, audit)
			if err != nil {
				errors <- err
				return
			}
			results <- resolved
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent team creation: %v", err)
	}
	var resolvedID string
	for team := range results {
		if resolvedID == "" {
			resolvedID = team.ID
		}
		if team.ID != resolvedID {
			t.Fatalf("concurrent creations resolved to %q and %q", resolvedID, team.ID)
		}
	}
	for table, query := range map[string]interface{}{
		"teams":             &model.Team{},
		"team_members":      &model.TeamMember{},
		"team_audit_events": &model.TeamAuditEvent{},
	} {
		var count int64
		if err := db.Model(query).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s count = %d, want 1", table, count)
		}
	}
}

func TestPostgresConcurrentTeamInvitationsReserveOnlyAvailableSeat(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureTeamIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	owner := model.User{ID: "seat-owner", Username: "seat-owner", Email: "seat-owner@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	team := model.Team{ID: "seat-team", OwnerUserID: owner.ID, Name: "席位并发团队", Status: model.TeamStatusActive, CreatedAt: now, UpdatedAt: now}
	member := model.TeamMember{ID: "seat-owner-member", TeamID: team.ID, UserID: owner.ID, Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
	subscription := model.MembershipSubscription{ID: "seat-subscription", UserID: owner.ID, TeamID: team.ID, PlanID: "seat-plan", Status: model.MembershipSubscriptionActive, Seats: 2, StartsAt: now.Add(-time.Hour), CreatedAt: now, UpdatedAt: now}
	for _, value := range []interface{}{&owner, &team, &member, &subscription} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	const contenders = 2
	start := make(chan struct{})
	errorsByContender := make(chan error, contenders)
	var wait sync.WaitGroup
	wait.Add(contenders)
	for index := 0; index < contenders; index++ {
		index := index
		go func() {
			defer wait.Done()
			<-start
			invitation := &model.TeamInvitation{
				ID:            fmt.Sprintf("seat-invitation-%d", index),
				TeamID:        team.ID,
				InviterUserID: owner.ID,
				Email:         fmt.Sprintf("candidate-%d@example.com", index),
				Role:          model.TeamMemberRoleMember,
				Status:        model.TeamInvitationStatusPending,
				TokenHash:     fmt.Sprintf("seat-token-%d", index),
				ExpiresAt:     now.Add(24 * time.Hour),
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			audit := &model.TeamAuditEvent{ID: fmt.Sprintf("seat-audit-%d", index), TeamID: team.ID, ActorUserID: owner.ID, Action: "invitation.created", MetadataJSON: "{}", CreatedAt: now}
			errorsByContender <- New(db.Session(&gorm.Session{NewDB: true})).CreateTeamInvitation(invitation, "", audit, now)
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByContender)

	successes := 0
	seatConflicts := 0
	for err := range errorsByContender {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrTeamSeatLimitReached):
			seatConflicts++
		default:
			t.Fatalf("concurrent invitation returned unexpected error: %v", err)
		}
	}
	if successes != 1 || seatConflicts != 1 {
		t.Fatalf("concurrent invitations successes=%d seatConflicts=%d, want 1/1", successes, seatConflicts)
	}
	var pendingCount int64
	if err := db.Model(&model.TeamInvitation{}).Where("team_id = ? AND status = ?", team.ID, model.TeamInvitationStatusPending).Count(&pendingCount).Error; err != nil {
		t.Fatal(err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending invitation count = %d, want 1", pendingCount)
	}
	var auditCount int64
	if err := db.Model(&model.TeamAuditEvent{}).Where("team_id = ? AND action = ?", team.ID, "invitation.created").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("invitation audit count = %d, want 1", auditCount)
	}
}

func TestPostgresConcurrentMemberPolicyUpdatesUseVersionCAS(t *testing.T) {
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureTeamIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	owner := model.User{ID: "policy-owner", Username: "policy-owner", Email: "policy-owner@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	target := model.User{ID: "policy-target", Username: "policy-target", Email: "policy-target@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	team := model.Team{ID: "policy-team", OwnerUserID: owner.ID, Name: "成员并发团队", Status: model.TeamStatusActive, CreatedAt: now, UpdatedAt: now}
	ownerMember := model.TeamMember{ID: "policy-owner-member", TeamID: team.ID, UserID: owner.ID, Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
	targetMember := model.TeamMember{ID: "policy-target-member", TeamID: team.ID, UserID: target.ID, Role: model.TeamMemberRoleMember, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
	for _, value := range []interface{}{&owner, &target, &team, &ownerMember, &targetMember} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	var persisted model.TeamMember
	if err := db.First(&persisted, "id = ?", targetMember.ID).Error; err != nil {
		t.Fatal(err)
	}

	const contenders = 2
	start := make(chan struct{})
	errorsByContender := make(chan error, contenders)
	var wait sync.WaitGroup
	wait.Add(contenders)
	for index := 0; index < contenders; index++ {
		index := index
		go func() {
			defer wait.Done()
			<-start
			audit := &model.TeamAuditEvent{ID: fmt.Sprintf("policy-audit-%d", index), TeamID: team.ID, ActorUserID: owner.ID, Action: "member.policy_updated", MetadataJSON: "{}", CreatedAt: now.Add(time.Duration(index+1) * time.Second)}
			errorsByContender <- New(db.Session(&gorm.Session{NewDB: true})).UpdateTeamMemberPolicy(team.ID, targetMember.ID, model.TeamMemberRoleAdmin, int64(index+1)*1000, persisted.UpdatedAt, audit, now.Add(time.Duration(index+1)*time.Second))
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByContender)

	successes := 0
	versionConflicts := 0
	for err := range errorsByContender {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrTeamMemberVersionConflict):
			versionConflicts++
		default:
			t.Fatalf("concurrent policy update returned unexpected error: %v", err)
		}
	}
	if successes != 1 || versionConflicts != 1 {
		t.Fatalf("concurrent policy updates successes=%d versionConflicts=%d, want 1/1", successes, versionConflicts)
	}
	var auditCount int64
	if err := db.Model(&model.TeamAuditEvent{}).Where("team_id = ? AND action = ?", team.ID, "member.policy_updated").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("member policy audit count = %d, want 1", auditCount)
	}
}
