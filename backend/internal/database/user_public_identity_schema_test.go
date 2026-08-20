package database

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureUserPublicIdentitySchemaBackfillsEveryUserIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}

	createdAt := time.Date(2026, time.August, 20, 10, 0, 0, 0, time.UTC)
	users := []model.User{
		{ID: "user-older", Username: "older", Status: model.UserStatusActive, CreatedAt: createdAt},
		{ID: "user-newer", Username: "newer", Status: model.UserStatusActive, CreatedAt: createdAt.Add(time.Second)},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}

	if err := EnsureUserPublicIdentitySchema(db); err != nil {
		t.Fatal(err)
	}
	var firstPass []model.UserPublicIdentity
	if err := db.Order("number asc").Find(&firstPass).Error; err != nil {
		t.Fatal(err)
	}
	if len(firstPass) != len(users) {
		t.Fatalf("expected %d public identities, got %d", len(users), len(firstPass))
	}
	if firstPass[0].UserID != users[0].ID || firstPass[1].UserID != users[1].ID {
		t.Fatalf("historical users were not assigned in stable creation order: %#v", firstPass)
	}
	for _, identity := range firstPass {
		publicID := identity.PublicID()
		if publicID < 10_000 {
			t.Fatalf("public ID %d does not start from five digits", publicID)
		}
	}
	if firstPass[0].Number == firstPass[1].Number || firstPass[0].PublicID() == firstPass[1].PublicID() {
		t.Fatal("distinct users received the same public ID")
	}

	if err := EnsureUserPublicIdentitySchema(db); err != nil {
		t.Fatal(err)
	}
	var secondPass []model.UserPublicIdentity
	if err := db.Order("number asc").Find(&secondPass).Error; err != nil {
		t.Fatal(err)
	}
	if len(secondPass) != len(firstPass) {
		t.Fatalf("idempotent migration changed identity count from %d to %d", len(firstPass), len(secondPass))
	}
	for index := range firstPass {
		if secondPass[index].Number != firstPass[index].Number || secondPass[index].UserID != firstPass[index].UserID {
			t.Fatalf("idempotent migration changed identity at index %d", index)
		}
	}
}
