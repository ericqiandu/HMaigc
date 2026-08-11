package repository

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTeamsForUserReturnsJSONEmptyArrayWhenUserHasNoTeams(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Team{}, &model.TeamMember{}); err != nil {
		t.Fatal(err)
	}

	teams, err := New(db).TeamsForUser("user-without-teams")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(teams)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != "[]" {
		t.Fatalf("empty teams JSON = %s, want []", payload)
	}
}
