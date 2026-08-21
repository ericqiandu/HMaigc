package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTeamInvitationNotPending    = errors.New("team invitation is not pending")
	ErrTeamInvitationEmailMismatch = errors.New("team invitation email does not match")
	ErrTeamMemberAlreadyActive     = errors.New("team member is already active")
	ErrTeamMemberNotActive         = errors.New("team member is not active")
	ErrTeamOwnerImmutable          = errors.New("team owner is immutable")
	ErrTeamSubscriptionRequired    = errors.New("active team subscription is required")
	ErrTeamCreationConflict        = errors.New("team creation idempotency conflict")
	ErrTeamInvitationAlreadyExists = errors.New("team invitation already exists")
	ErrTeamMemberVersionConflict   = errors.New("team member version conflict")
)

type TeamMemberRecord struct {
	model.TeamMember
	Username                string `json:"username"`
	DisplayName             string `json:"displayName"`
	MonthlyUsedMicrocredits int64  `json:"monthlyUsedMicrocredits"`
}

type TeamAuditRecord struct {
	model.TeamAuditEvent
	ActorName  string `json:"actorName"`
	TargetName string `json:"targetName,omitempty"`
}

type IncomingTeamInvitationRecord struct {
	model.TeamInvitation
	TeamName    string `json:"teamName"`
	InviterName string `json:"inviterName"`
}

type TeamSummaryRecord struct {
	ID                     string               `json:"id"`
	OwnerUserID            string               `json:"ownerUserId"`
	Name                   string               `json:"name"`
	Status                 model.TeamStatus     `json:"status"`
	CreatedAt              time.Time            `json:"createdAt"`
	UpdatedAt              time.Time            `json:"updatedAt"`
	CurrentRole            model.TeamMemberRole `json:"currentRole"`
	SeatUsed               int                  `json:"seatUsed"`
	InvitationSeatReserved int                  `json:"invitationSeatReserved"`
	SubscriptionID         string               `json:"subscriptionId"`
	PlanID                 string               `json:"planId"`
	PlanSnapshotJSON       string               `json:"planSnapshotJson"`
	SeatLimit              int                  `json:"seatLimit"`
	SubscriptionEndsAt     *time.Time           `json:"subscriptionEndsAt"`
	AvailableMicrocredits  int64                `json:"availableMicrocredits"`
	ReservedMicrocredits   int64                `json:"reservedMicrocredits"`
	StorageUsedBytes       int64                `json:"storageUsedBytes"`
}

func (r *Repository) TeamSummaryRecordsForUser(userID string, now time.Time) ([]TeamSummaryRecord, error) {
	var records []TeamSummaryRecord
	err := r.db.Raw(`
		SELECT teams.id, teams.owner_user_id, teams.name, teams.status, teams.created_at, teams.updated_at,
		       actor_membership.role AS current_role,
		       (SELECT COUNT(*) FROM team_members active_members
		         WHERE active_members.team_id = teams.id AND active_members.status = ?) AS seat_used,
		       (SELECT COUNT(*) FROM team_invitations pending_invitations
		         WHERE pending_invitations.team_id = teams.id
		           AND pending_invitations.status = ? AND pending_invitations.expires_at > ?) AS invitation_seat_reserved,
		       COALESCE(active_subscription.id, '') AS subscription_id,
		       COALESCE(active_subscription.plan_id, '') AS plan_id,
		       COALESCE(active_subscription.plan_snapshot_json, '') AS plan_snapshot_json,
		       COALESCE(active_subscription.seats, 0) AS seat_limit,
		       active_subscription.ends_at AS subscription_ends_at,
		       COALESCE((SELECT credits.available_microcredits FROM team_credit_accounts credits WHERE credits.team_id = teams.id), 0) AS available_microcredits,
		       COALESCE((SELECT credits.reserved_microcredits FROM team_credit_accounts credits WHERE credits.team_id = teams.id), 0) AS reserved_microcredits,
		       COALESCE((SELECT SUM(resources.size) FROM resources WHERE resources.team_id = teams.id AND resources.status = ?), 0) AS storage_used_bytes
		FROM teams
		JOIN team_members actor_membership
		  ON actor_membership.team_id = teams.id
		 AND actor_membership.user_id = ?
		 AND actor_membership.status = ?
		LEFT JOIN membership_subscriptions active_subscription
		  ON active_subscription.id = (
			SELECT candidate.id
			FROM membership_subscriptions candidate
			WHERE candidate.team_id = teams.id
			  AND candidate.status = ?
			  AND candidate.starts_at <= ?
			  AND (candidate.ends_at IS NULL OR candidate.ends_at > ?)
			ORDER BY candidate.seats DESC, candidate.created_at DESC
			LIMIT 1
		  )
		WHERE teams.status = ?
		ORDER BY teams.created_at DESC
	`,
		model.TeamMemberStatusActive,
		model.TeamInvitationStatusPending,
		now,
		model.ResourceStatusReady,
		userID,
		model.TeamMemberStatusActive,
		model.MembershipSubscriptionActive,
		now,
		now,
		model.TeamStatusActive,
	).Scan(&records).Error
	return records, err
}

func (r *Repository) Team(teamID string) (*model.Team, error) {
	var team model.Team
	if err := r.db.First(&team, "id = ? AND status = ?", teamID, model.TeamStatusActive).Error; err != nil {
		return nil, err
	}
	return &team, nil
}

func (r *Repository) TeamMemberForUser(teamID string, userID string) (*model.TeamMember, error) {
	var member model.TeamMember
	if err := r.db.First(&member, "team_id = ? AND user_id = ? AND status = ?", teamID, userID, model.TeamMemberStatusActive).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *Repository) TeamMemberByID(teamID string, memberID string) (*model.TeamMember, error) {
	var member model.TeamMember
	if err := r.db.First(&member, "id = ? AND team_id = ? AND status = ?", memberID, teamID, model.TeamMemberStatusActive).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *Repository) TeamMemberRecords(teamID string) ([]TeamMemberRecord, error) {
	var records []TeamMemberRecord
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)
	err := r.db.Raw(`
		SELECT members.*, users.username, users.display_name,
		       COALESCE((SELECT SUM(orders.amount_microcredits)
		         FROM billing_orders orders
		        WHERE orders.team_id = members.team_id
		          AND orders.user_id = members.user_id
		          AND orders.created_at >= ? AND orders.created_at < ?
		          AND orders.status IN ('reserved', 'running', 'settled', 'uncertain')), 0) AS monthly_used_microcredits
		FROM team_members members
		JOIN users ON users.id = members.user_id
		WHERE members.team_id = ? AND members.status = ?
		ORDER BY
			CASE members.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
			members.created_at ASC
	`, monthStart, monthEnd, teamID, model.TeamMemberStatusActive).Scan(&records).Error
	return records, err
}

func (r *Repository) TeamCreditAccount(teamID string) (*model.TeamCreditAccount, error) {
	account := model.TeamCreditAccount{TeamID: teamID}
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&account).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&account, "team_id = ?", teamID).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func (r *Repository) TeamStoredResourceBytes(teamID string) (int64, error) {
	var bytes int64
	err := r.db.Model(&model.Resource{}).
		Where("team_id = ? AND status = ?", teamID, model.ResourceStatusReady).
		Select("COALESCE(SUM(size), 0)").Scan(&bytes).Error
	return bytes, err
}

func (r *Repository) ActiveTeamSubscription(teamID string, now time.Time) (*model.MembershipSubscription, error) {
	return activeTeamSubscription(r.db, teamID, now)
}

func activeTeamSubscription(db *gorm.DB, teamID string, now time.Time) (*model.MembershipSubscription, error) {
	var subscription model.MembershipSubscription
	err := db.Where(
		"team_id = ? AND status = ? AND starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)",
		teamID,
		model.MembershipSubscriptionActive,
		now,
		now,
	).Order("seats DESC, created_at DESC").First(&subscription).Error
	if err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (r *Repository) PendingTeamInvitations(teamID string, now time.Time) ([]model.TeamInvitation, error) {
	var invitations []model.TeamInvitation
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockTeam(tx, teamID); err != nil {
			return err
		}
		if err := expireTeamInvitations(tx, teamID, now); err != nil {
			return err
		}
		return tx.Where(
			"team_id = ? AND status = ? AND expires_at > ?",
			teamID,
			model.TeamInvitationStatusPending,
			now,
		).Order("created_at DESC").Find(&invitations).Error
	})
	return invitations, err
}

func (r *Repository) IncomingTeamInvitations(email string, now time.Time) ([]IncomingTeamInvitationRecord, error) {
	var invitations []IncomingTeamInvitationRecord
	err := r.db.Raw(`
		SELECT invitations.*, teams.name AS team_name,
		       COALESCE(NULLIF(users.display_name, ''), users.username) AS inviter_name
		FROM team_invitations invitations
		JOIN teams ON teams.id = invitations.team_id AND teams.status = ?
		JOIN users ON users.id = invitations.inviter_user_id
		WHERE lower(invitations.email) = lower(?)
		  AND invitations.status = ?
		  AND invitations.expires_at > ?
		ORDER BY invitations.created_at DESC
	`, model.TeamStatusActive, email, model.TeamInvitationStatusPending, now).Scan(&invitations).Error
	return invitations, err
}

func (r *Repository) TeamAuditRecords(teamID string, limit int) ([]TeamAuditRecord, error) {
	var records []TeamAuditRecord
	err := r.db.Raw(`
		SELECT events.*,
		       COALESCE(NULLIF(actor.display_name, ''), actor.username) AS actor_name,
		       COALESCE(NULLIF(target.display_name, ''), target.username) AS target_name
		FROM team_audit_events events
		JOIN users actor ON actor.id = events.actor_user_id
		LEFT JOIN users target ON target.id = events.target_user_id
		WHERE events.team_id = ?
		ORDER BY events.created_at DESC
		LIMIT ?
	`, teamID, limit).Scan(&records).Error
	return records, err
}

func (r *Repository) CreateTeamIdempotent(team *model.Team, owner *model.TeamMember, audit *model.TeamAuditEvent) (*model.Team, error) {
	var resolved model.Team
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var existing model.Team
		err := tx.First(&existing, "owner_user_id = ? AND creation_idempotency_key = ?", team.OwnerUserID, team.CreationIdempotencyKey).Error
		if err == nil {
			if existing.CreationRequestHash != team.CreationRequestHash {
				return ErrTeamCreationConflict
			}
			resolved = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(team)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := tx.First(&existing, "owner_user_id = ? AND creation_idempotency_key = ?", team.OwnerUserID, team.CreationIdempotencyKey).Error; err != nil {
				return err
			}
			if existing.CreationRequestHash != team.CreationRequestHash {
				return ErrTeamCreationConflict
			}
			resolved = existing
			return nil
		}
		if err := tx.Create(owner).Error; err != nil {
			return err
		}
		if err := tx.Create(audit).Error; err != nil {
			return err
		}
		resolved = *team
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

func (r *Repository) RenameTeam(teamID string, name string, audit *model.TeamAuditEvent, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Team{}).
			Where("id = ? AND status = ?", teamID, model.TeamStatusActive).
			Updates(map[string]interface{}{"name": name, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) UpdateTeamMemberPolicy(teamID string, memberID string, role model.TeamMemberRole, monthlyLimit int64, expectedUpdatedAt time.Time, audit *model.TeamAuditEvent, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TeamMember{}).
			Where("id = ? AND team_id = ? AND status = ? AND role <> ? AND updated_at = ?", memberID, teamID, model.TeamMemberStatusActive, model.TeamMemberRoleOwner, expectedUpdatedAt).
			Updates(map[string]interface{}{"role": role, "monthly_credit_limit_microcredits": monthlyLimit, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var current model.TeamMember
			if err := tx.First(&current, "id = ? AND team_id = ? AND status = ?", memberID, teamID, model.TeamMemberStatusActive).Error; err != nil {
				return err
			}
			if current.Role == model.TeamMemberRoleOwner {
				return ErrTeamOwnerImmutable
			}
			return ErrTeamMemberVersionConflict
		}
		return tx.Create(audit).Error
	})
}

func (r *Repository) CreateTeamInvitation(invitation *model.TeamInvitation, targetUserID string, audit *model.TeamAuditEvent, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockTeam(tx, invitation.TeamID); err != nil {
			return err
		}
		if err := expireTeamInvitations(tx, invitation.TeamID, now); err != nil {
			return err
		}
		subscription, err := activeTeamSubscription(tx, invitation.TeamID, now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTeamSubscriptionRequired
		}
		if err != nil {
			return err
		}
		if targetUserID != "" {
			var activeCount int64
			if err := tx.Model(&model.TeamMember{}).
				Where("team_id = ? AND user_id = ? AND status = ?", invitation.TeamID, targetUserID, model.TeamMemberStatusActive).
				Count(&activeCount).Error; err != nil {
				return err
			}
			if activeCount > 0 {
				return ErrTeamMemberAlreadyActive
			}
		}
		if err := tx.Model(&model.TeamInvitation{}).
			Where("team_id = ? AND status = ? AND expires_at <= ?", invitation.TeamID, model.TeamInvitationStatusPending, now).
			Updates(map[string]interface{}{"status": model.TeamInvitationStatusExpired, "updated_at": now}).Error; err != nil {
			return err
		}

		var existing model.TeamInvitation
		existingErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("team_id = ? AND lower(email) = lower(?) AND status = ?", invitation.TeamID, invitation.Email, model.TeamInvitationStatusPending).
			First(&existing).Error
		if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}

		if existingErr == nil {
			return ErrTeamInvitationAlreadyExists
		}

		var activeMembers int64
		if err := tx.Model(&model.TeamMember{}).
			Where("team_id = ? AND status = ?", invitation.TeamID, model.TeamMemberStatusActive).
			Count(&activeMembers).Error; err != nil {
			return err
		}
		var reservedInvitations int64
		query := tx.Model(&model.TeamInvitation{}).
			Where("team_id = ? AND status = ? AND expires_at > ?", invitation.TeamID, model.TeamInvitationStatusPending, now)
		if err := query.Count(&reservedInvitations).Error; err != nil {
			return err
		}
		if activeMembers+reservedInvitations >= int64(subscription.Seats) {
			return ErrTeamSeatLimitReached
		}

		if err := tx.Create(invitation).Error; err != nil {
			return err
		}
		audit.TargetInvitationID = invitation.ID
		return tx.Create(audit).Error
	})
}

func (r *Repository) RegenerateTeamInvitation(teamID string, invitationID string, inviterUserID string, tokenHash string, expiresAt time.Time, audit *model.TeamAuditEvent, now time.Time) (*model.TeamInvitation, error) {
	var regenerated model.TeamInvitation
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockTeam(tx, teamID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&regenerated, "id = ? AND team_id = ?", invitationID, teamID).Error; err != nil {
			return err
		}
		if regenerated.Status != model.TeamInvitationStatusPending || !regenerated.ExpiresAt.After(now) {
			return ErrTeamInvitationNotPending
		}
		result := tx.Model(&model.TeamInvitation{}).Where("id = ? AND status = ?", regenerated.ID, model.TeamInvitationStatusPending).Updates(map[string]interface{}{
			"inviter_user_id": inviterUserID,
			"token_hash":      tokenHash,
			"expires_at":      expiresAt,
			"updated_at":      now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTeamInvitationNotPending
		}
		regenerated.InviterUserID = inviterUserID
		regenerated.TokenHash = tokenHash
		regenerated.ExpiresAt = expiresAt
		regenerated.UpdatedAt = now
		audit.TargetInvitationID = regenerated.ID
		return tx.Create(audit).Error
	})
	if err != nil {
		return nil, err
	}
	return &regenerated, nil
}

func (r *Repository) AcceptTeamInvitation(invitationID string, tokenHash string, memberID string, userID string, email string, audit *model.TeamAuditEvent, now time.Time) (*model.TeamMember, error) {
	var accepted model.TeamMember
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var invitationReference model.TeamInvitation
		query := tx
		if invitationID != "" {
			query = query.Where("id = ?", invitationID)
		} else {
			query = query.Where("token_hash = ?", tokenHash)
		}
		if err := query.First(&invitationReference).Error; err != nil {
			return err
		}
		if err := lockTeam(tx, invitationReference.TeamID); err != nil {
			return err
		}
		var invitation model.TeamInvitation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&invitation, "id = ?", invitationReference.ID).Error; err != nil {
			return err
		}
		if invitation.Status != model.TeamInvitationStatusPending || !invitation.ExpiresAt.After(now) {
			return ErrTeamInvitationNotPending
		}
		if invitation.Email != email {
			return ErrTeamInvitationEmailMismatch
		}
		subscription, err := activeTeamSubscription(tx, invitation.TeamID, now)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTeamSubscriptionRequired
		}
		if err != nil {
			return err
		}
		var existing model.TeamMember
		existingErr := tx.Where("team_id = ? AND user_id = ?", invitation.TeamID, userID).First(&existing).Error
		if existingErr == nil && existing.Status == model.TeamMemberStatusActive {
			return ErrTeamMemberAlreadyActive
		}
		if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}
		var activeMembers int64
		if err := tx.Model(&model.TeamMember{}).
			Where("team_id = ? AND status = ?", invitation.TeamID, model.TeamMemberStatusActive).
			Count(&activeMembers).Error; err != nil {
			return err
		}
		if activeMembers >= int64(subscription.Seats) {
			return ErrTeamSeatLimitReached
		}
		accepted = model.TeamMember{
			ID:        existing.ID,
			TeamID:    invitation.TeamID,
			UserID:    userID,
			Role:      invitation.Role,
			Status:    model.TeamMemberStatusActive,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if accepted.ID == "" {
			accepted.ID = memberID
			if err := tx.Create(&accepted).Error; err != nil {
				return err
			}
		} else {
			accepted.CreatedAt = existing.CreatedAt
			if err := tx.Model(&model.TeamMember{}).Where("id = ?", accepted.ID).Updates(map[string]interface{}{
				"role":       accepted.Role,
				"status":     accepted.Status,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
		}
		updatedInvitation := tx.Model(&model.TeamInvitation{}).Where("id = ? AND status = ?", invitation.ID, model.TeamInvitationStatusPending).Updates(map[string]interface{}{
			"status":              model.TeamInvitationStatusAccepted,
			"accepted_by_user_id": userID,
			"accepted_at":         now,
			"updated_at":          now,
		})
		if updatedInvitation.Error != nil {
			return updatedInvitation.Error
		}
		if updatedInvitation.RowsAffected != 1 {
			return ErrTeamInvitationNotPending
		}
		audit.TeamID = invitation.TeamID
		audit.TargetUserID = userID
		audit.TargetInvitationID = invitation.ID
		return tx.Create(audit).Error
	})
	if err != nil {
		return nil, err
	}
	return &accepted, nil
}

func (r *Repository) UpdateTeamMemberRole(teamID string, memberID string, role model.TeamMemberRole, audit *model.TeamAuditEvent, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var member model.TeamMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&member, "id = ? AND team_id = ? AND status = ?", memberID, teamID, model.TeamMemberStatusActive).Error; err != nil {
			return err
		}
		if member.Role == model.TeamMemberRoleOwner {
			return ErrTeamOwnerImmutable
		}
		if err := tx.Model(&model.TeamMember{}).Where("id = ?", member.ID).Updates(map[string]interface{}{"role": role, "updated_at": now}).Error; err != nil {
			return err
		}
		audit.TargetUserID = member.UserID
		return tx.Create(audit).Error
	})
}

func (r *Repository) RemoveTeamMember(teamID string, memberID string, audit *model.TeamAuditEvent, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var member model.TeamMember
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&member, "id = ? AND team_id = ? AND status = ?", memberID, teamID, model.TeamMemberStatusActive).Error; err != nil {
			return err
		}
		if member.Role == model.TeamMemberRoleOwner {
			return ErrTeamOwnerImmutable
		}
		result := tx.Model(&model.TeamMember{}).Where("id = ? AND status = ?", member.ID, model.TeamMemberStatusActive).Updates(map[string]interface{}{
			"status":     model.TeamMemberStatusRemoved,
			"updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTeamMemberNotActive
		}
		audit.TargetUserID = member.UserID
		return tx.Create(audit).Error
	})
}

func (r *Repository) RevokeTeamInvitation(teamID string, invitationID string, audit *model.TeamAuditEvent, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := lockTeam(tx, teamID); err != nil {
			return err
		}
		if err := expireTeamInvitations(tx, teamID, now); err != nil {
			return err
		}
		var invitation model.TeamInvitation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&invitation, "id = ? AND team_id = ?", invitationID, teamID).Error; err != nil {
			return err
		}
		if invitation.Status != model.TeamInvitationStatusPending || !invitation.ExpiresAt.After(now) {
			return ErrTeamInvitationNotPending
		}
		result := tx.Model(&model.TeamInvitation{}).Where("id = ? AND status = ?", invitation.ID, model.TeamInvitationStatusPending).Updates(map[string]interface{}{
			"status":     model.TeamInvitationStatusRevoked,
			"revoked_at": now,
			"updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTeamInvitationNotPending
		}
		audit.TargetInvitationID = invitation.ID
		return tx.Create(audit).Error
	})
}

func expireTeamInvitations(tx *gorm.DB, teamID string, now time.Time) error {
	return tx.Model(&model.TeamInvitation{}).
		Where("team_id = ? AND status = ? AND expires_at <= ?", teamID, model.TeamInvitationStatusPending, now).
		Updates(map[string]interface{}{"status": model.TeamInvitationStatusExpired, "updated_at": now}).Error
}

func lockTeam(tx *gorm.DB, teamID string) error {
	var team model.Team
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&team, "id = ? AND status = ?", teamID, model.TeamStatusActive).Error
}
