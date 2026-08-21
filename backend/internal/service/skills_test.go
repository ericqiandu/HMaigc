package service

import (
	"context"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/skillcatalog"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newFirstPartySkillTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{ID: "skill-user", Email: "skill-user@example.com", Status: model.UserStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), t.TempDir())
}

func TestFirstPartySkillCatalogListsPublishedSkillsWithoutExternalProvider(t *testing.T) {
	svc := newFirstPartySkillTestService(t)
	builtins, err := skillcatalog.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	publishedVersions := make(map[string]int, len(builtins))
	for _, builtin := range builtins {
		publishedVersions[builtin.Dir] = builtin.Version
	}
	capabilities := svc.SkillIntegrationCapabilities()
	if capabilities.Provider != "first_party" || !capabilities.PublicCatalog || capabilities.Upload {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	result, err := svc.SkillsCatalog(context.Background(), "skill-user", SkillListRequest{Page: 1, PageSize: 2, Search: "导演"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total < 3 || len(result.Skills) != 2 || result.Page != 1 || result.PageSize != 2 {
		t.Fatalf("catalog page = %#v", result)
	}
	for _, skill := range result.Skills {
		expectedVersion, exists := publishedVersions[skill.Dir]
		if !exists || skill.Version != expectedVersion || skill.DetailText != "" || skill.UploaderName != "HMaigc" {
			t.Fatalf("invalid first-party skill = %#v", skill)
		}
	}
	detail, err := svc.SkillDetail(context.Background(), "skill-user", result.Skills[0].Dir)
	if err != nil {
		t.Fatal(err)
	}
	if detail.DetailText == "" {
		t.Fatal("skill detail omitted the published instructions")
	}
}

func TestFirstPartySkillCatalogIsPublicWithoutUserState(t *testing.T) {
	svc := newFirstPartySkillTestService(t)
	result, err := svc.SkillsCatalog(context.Background(), "", SkillListRequest{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total < 3 || len(result.Skills) < 3 {
		t.Fatalf("public catalog = %#v", result)
	}
	for _, skill := range result.Skills {
		if skill.Activated || skill.Liked {
			t.Fatalf("anonymous catalog leaked user state: %#v", skill)
		}
	}
}

func TestFirstPartySkillActivationLoadsPublishedVersion(t *testing.T) {
	svc := newFirstPartySkillTestService(t)
	skill, err := svc.SetSkillActivated(context.Background(), "skill-user", "short-drama-director", true)
	if err != nil {
		t.Fatal(err)
	}
	if !skill.Activated || skill.Dir != "short-drama-director" {
		t.Fatalf("activated skill = %#v", skill)
	}
	activated, err := svc.ActivatedSkills(context.Background(), "skill-user")
	if err != nil {
		t.Fatal(err)
	}
	if len(activated) != 1 || activated[0].Dir != skill.Dir || activated[0].Version != skill.Version {
		t.Fatalf("activated skills = %#v", activated)
	}
}

func TestFirstPartySkillStateUpdatesPreserveIndependentFlags(t *testing.T) {
	svc := newFirstPartySkillTestService(t)
	dir := "commercial-film-director"
	if _, err := svc.SetSkillActivated(context.Background(), "skill-user", dir, true); err != nil {
		t.Fatal(err)
	}
	result, err := svc.SetSkillLiked(context.Background(), "skill-user", dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Activated || !result.Liked {
		t.Fatalf("skill state flags were overwritten: %#v", result)
	}
	state, err := svc.repo.UserSkillState("skill-user", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Activated || !state.Liked {
		t.Fatalf("stored skill state flags were overwritten: %#v", state)
	}
}

func TestActivatedSkillsRejectsUnknownLegacyState(t *testing.T) {
	svc := newFirstPartySkillTestService(t)
	if err := svc.repo.Create(&model.UserSkillState{ID: "legacy-state", UserID: "skill-user", SkillDir: "removed-external-skill", Activated: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ActivatedSkills(context.Background(), "skill-user"); err == nil {
		t.Fatal("unknown activated skill must fail explicitly")
	}
}

func TestFirstPartySkillCatalogRejectsUnboundedPublicQueries(t *testing.T) {
	svc := newFirstPartySkillTestService(t)
	requests := []SkillListRequest{
		{Page: 0, PageSize: 12},
		{Page: SkillCatalogMaximumPage + 1, PageSize: 12},
		{Page: 1, PageSize: 61},
		{Page: 1, PageSize: 12, Search: strings.Repeat("x", skillCatalogMaximumSearch+1)},
	}
	for _, request := range requests {
		if _, err := svc.SkillsCatalog(context.Background(), "", request); err == nil {
			t.Fatalf("unbounded catalog request was accepted: %#v", request)
		}
	}
}
