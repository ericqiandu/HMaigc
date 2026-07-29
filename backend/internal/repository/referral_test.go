package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateUserRegistrationPersistsBindingAndConsumesVerificationAtomically(t *testing.T) {
	repo, db := newReferralRepositoryTest(t)
	now := time.Now()
	inviter := model.User{ID: "registration-inviter", Username: "registration-inviter", Status: model.UserStatusActive}
	verification := model.EmailVerificationCode{
		ID: "registration-code", Email: "invitee@example.com", Purpose: "register",
		CodeHash: "hash", ExpiresAt: now.Add(time.Minute), CreatedAt: now,
	}
	if err := db.Create(&inviter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&verification).Error; err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "registration-invitee", Username: "registration-invitee", Email: verification.Email, Status: model.UserStatusActive}
	profile := model.ReferralProfile{UserID: user.ID, Code: "PROFILE234", CreatedAt: now, UpdatedAt: now}
	relationship := model.ReferralRelationship{
		ID: "registration-relationship", InviterUserID: inviter.ID, InviteeUserID: user.ID,
		ReferralCode: "INVITER234", Status: model.ReferralRelationshipEligible,
		BoundAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateUserRegistration(UserRegistration{
		User: &user, ReferralProfile: &profile, ReferralRelationship: &relationship,
		VerificationCodeID: verification.ID, VerificationCodeUsedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	var storedVerification model.EmailVerificationCode
	if err := db.First(&storedVerification, "id = ?", verification.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedVerification.UsedAt == nil {
		t.Fatal("verification code was not consumed")
	}
	if _, err := repo.ReferralRelationshipForInvitee(user.ID); err != nil {
		t.Fatal(err)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
}

func TestCreateUserRegistrationRollsBackVerificationWhenProfileCodeConflicts(t *testing.T) {
	repo, db := newReferralRepositoryTest(t)
	now := time.Now()
	verification := model.EmailVerificationCode{
		ID: "rollback-code", Email: "rollback@example.com", Purpose: "register",
		CodeHash: "hash", ExpiresAt: now.Add(time.Minute), CreatedAt: now,
	}
	existingProfile := model.ReferralProfile{UserID: "existing-profile-user", Code: "DUPLICATE2", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&verification).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&existingProfile).Error; err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "rollback-user", Username: "rollback-user", Email: verification.Email, Status: model.UserStatusActive}
	conflictingProfile := model.ReferralProfile{UserID: user.ID, Code: existingProfile.Code, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateUserRegistration(UserRegistration{
		User: &user, ReferralProfile: &conflictingProfile,
		VerificationCodeID: verification.ID, VerificationCodeUsedAt: now,
	}); err == nil {
		t.Fatal("expected duplicate referral code to fail registration")
	}
	var storedVerification model.EmailVerificationCode
	if err := db.First(&storedVerification, "id = ?", verification.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedVerification.UsedAt != nil {
		t.Fatal("verification code remained consumed after registration rollback")
	}
	var userCount int64
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Count(&userCount).Error; err != nil {
		t.Fatal(err)
	}
	if userCount != 0 {
		t.Fatal("user remained after registration rollback")
	}
}

func newReferralRepositoryTest(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.UserIdentity{}, &model.EmailVerificationCode{},
		&model.ReferralProfile{}, &model.ReferralRelationship{},
		&model.CreditAccount{},
	); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}
