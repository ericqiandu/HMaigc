package service

import (
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPublicAuthUserIncludesLinuxDOIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.UserPublicIdentity{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "user-1", Username: "canvas-user", DisplayName: "Canvas User", Role: model.UserRoleUser, Status: model.UserStatusActive}
	identity := model.UserIdentity{ID: "identity-1", UserID: user.ID, Provider: "linuxdo", Subject: "123456", ProviderUsername: "linux-user", AvatarURL: "https://example.com/avatar.png"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	publicIdentity := model.UserPublicIdentity{UserID: user.ID}
	if err := db.Create(&publicIdentity).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&identity).Error; err != nil {
		t.Fatal(err)
	}

	result, err := (&Service{repo: repository.New(db)}).PublicAuthUser(&user)
	if err != nil {
		t.Fatal(err)
	}
	if result.AvatarURL != identity.AvatarURL || result.IdentityProvider != "linuxdo" || result.IdentityID != identity.Subject || result.IdentityUsername != identity.ProviderUsername {
		t.Fatalf("PublicAuthUser() = %#v", result)
	}
	if result.PublicID != publicIdentity.PublicID() {
		t.Fatalf("PublicAuthUser().PublicID = %d, want %d", result.PublicID, publicIdentity.PublicID())
	}
}

func TestPublicAuthUserKeepsLocalUserWithoutIdentity(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.UserPublicIdentity{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "user-1", Username: "local-user", DisplayName: "Local User"}
	publicIdentity := model.UserPublicIdentity{UserID: user.ID}
	if err := db.Create(&publicIdentity).Error; err != nil {
		t.Fatal(err)
	}

	result, err := (&Service{repo: repository.New(db)}).PublicAuthUser(&user)
	if err != nil {
		t.Fatal(err)
	}
	if result.Username != user.Username || result.PublicID != publicIdentity.PublicID() || result.AvatarURL != "" || result.IdentityProvider != "" || result.IdentityID != "" {
		t.Fatalf("PublicAuthUser() = %#v", result)
	}
}

func TestPublicAuthUserFailsWhenPublicIdentityIsMissing(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.UserPublicIdentity{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "missing-public-id", Username: "invalid-user"}

	if _, err := (&Service{repo: repository.New(db)}).PublicAuthUser(&user); err == nil {
		t.Fatal("expected missing public identity to fail explicitly")
	}
}
