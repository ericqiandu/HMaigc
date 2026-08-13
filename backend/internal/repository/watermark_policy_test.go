package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPublishWatermarkPolicyCreatesImmutableMonotonicVersions(t *testing.T) {
	db := openWatermarkPolicySQLite(t)
	repo := New(db)

	first := watermarkPublication("publication-1", "<p>管理规则</p>", "https://example.com/watermark")
	if err := repo.PublishWatermarkPolicy(first, watermarkPublicationAudit("audit-1", first.ID)); err != nil {
		t.Fatal(err)
	}
	second := watermarkPublication("publication-2", "<p>管理规则</p>", "https://example.com/watermark")
	if err := repo.PublishWatermarkPolicy(second, watermarkPublicationAudit("audit-2", second.ID)); err != nil {
		t.Fatal(err)
	}

	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("publication versions = %d, %d; want 1, 2", first.Version, second.Version)
	}
	if first.ContentHash == "" || first.ContentHash != second.ContentHash {
		t.Fatalf("same content hashes = %q, %q", first.ContentHash, second.ContentHash)
	}
	var publications int64
	if err := db.Model(&model.PolicyPublication{}).Count(&publications).Error; err != nil || publications != 2 {
		t.Fatalf("publication count = %d, err=%v", publications, err)
	}
	current, err := repo.CurrentWatermarkPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.ID != second.ID || current.Version != 2 {
		t.Fatalf("current publication = %#v", current)
	}
	var audit model.AdminAuditEvent
	if err := db.First(&audit, "id = ?", "audit-2").Error; err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(audit.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["publicationId"] != second.ID || metadata["version"] != float64(2) || metadata["contentHash"] != second.ContentHash {
		t.Fatalf("audit metadata = %#v", metadata)
	}
	if audit.TargetID != second.ID {
		t.Fatalf("audit target = %q, want %q", audit.TargetID, second.ID)
	}
}

func TestSaveWatermarkPreferenceRejectsStalePublicationWithoutWritingFacts(t *testing.T) {
	db := openWatermarkPolicySQLite(t)
	repo := New(db)
	first := watermarkPublication("publication-1", "<p>规则一</p>", "https://example.com/v1")
	second := watermarkPublication("publication-2", "<p>规则二</p>", "https://example.com/v2")
	if err := repo.PublishWatermarkPolicy(first, watermarkPublicationAudit("audit-1", first.ID)); err != nil {
		t.Fatal(err)
	}
	if err := repo.PublishWatermarkPolicy(second, watermarkPublicationAudit("audit-2", second.ID)); err != nil {
		t.Fatal(err)
	}

	_, _, err := repo.SaveWatermarkPreference("user-1", true, first.ID, watermarkPreferenceEvent("event-stale", "user-1", true, first.ID), time.Now().UTC())
	if !errors.Is(err, ErrWatermarkPolicyVersionConflict) {
		t.Fatalf("stale publication error = %v", err)
	}
	assertWatermarkRowCount(t, db, &model.UserPolicyConsent{}, 0)
	assertWatermarkRowCount(t, db, &model.UserWatermarkPreference{}, 0)
	assertWatermarkRowCount(t, db, &model.UserWatermarkPreferenceEvent{}, 0)
}

func TestSaveWatermarkPreferenceWritesConsentPreferenceAndEventAtomically(t *testing.T) {
	db := openWatermarkPolicySQLite(t)
	repo := New(db)
	publication := watermarkPublication("publication-1", "<p>规则</p>", "https://example.com/v1")
	if err := repo.PublishWatermarkPolicy(publication, watermarkPublicationAudit("audit-1", publication.ID)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)

	preference, current, err := repo.SaveWatermarkPreference("user-1", true, publication.ID, watermarkPreferenceEvent("event-enable", "user-1", true, publication.ID), now)
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.ID != publication.ID || preference == nil || !preference.RemoveWatermark || preference.AcceptedPublicationID != publication.ID || preference.AcceptedAt == nil || !preference.AcceptedAt.Equal(now) {
		t.Fatalf("saved facts: preference=%#v current=%#v", preference, current)
	}
	assertWatermarkRowCount(t, db, &model.UserPolicyConsent{}, 1)
	assertWatermarkRowCount(t, db, &model.UserWatermarkPreferenceEvent{}, 1)

	// Reusing an immutable event ID forces the final insert to fail. The preference update must roll back with it.
	_, _, err = repo.SaveWatermarkPreference("user-2", true, publication.ID, watermarkPreferenceEvent("event-enable", "user-2", true, publication.ID), now.Add(time.Minute))
	if err == nil {
		t.Fatal("duplicate preference event unexpectedly succeeded")
	}
	var missing model.UserWatermarkPreference
	if lookup := db.First(&missing, "user_id = ?", "user-2"); !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
		t.Fatalf("rolled-back preference lookup error = %v, value=%#v", lookup.Error, missing)
	}
	assertWatermarkRowCount(t, db, &model.UserPolicyConsent{}, 1)
}

func TestDisableWatermarkPreferenceDoesNotRequirePublishedPolicy(t *testing.T) {
	db := openWatermarkPolicySQLite(t)
	repo := New(db)
	now := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)

	preference, current, err := repo.SaveWatermarkPreference("user-1", false, "", watermarkPreferenceEvent("event-disable", "user-1", false, ""), now)
	if err != nil {
		t.Fatal(err)
	}
	if current != nil || preference == nil || preference.RemoveWatermark || preference.AcceptedPublicationID != "" || preference.AcceptedAt != nil {
		t.Fatalf("disabled facts: preference=%#v current=%#v", preference, current)
	}
	assertWatermarkRowCount(t, db, &model.UserPolicyConsent{}, 0)
	assertWatermarkRowCount(t, db, &model.UserWatermarkPreferenceEvent{}, 1)
}

func openWatermarkPolicySQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.PolicyPublicationHead{},
		&model.PolicyPublication{},
		&model.UserWatermarkPreference{},
		&model.UserPolicyConsent{},
		&model.UserWatermarkPreferenceEvent{},
		&model.AdminAuditEvent{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func watermarkPublication(id string, richText string, policyURL string) *model.PolicyPublication {
	digest := sha256.Sum256([]byte(richText + "\n" + policyURL))
	return &model.PolicyPublication{ID: id, Kind: model.PolicyKindAIWatermark, ManagementRuleRichText: richText, WatermarkPolicyURL: policyURL, ContentHash: hex.EncodeToString(digest[:]), PublishedBy: "admin-1", PublishedAt: time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC)}
}

func watermarkPublicationAudit(id string, publicationID string) *model.AdminAuditEvent {
	return &model.AdminAuditEvent{ID: id, ActorUserID: "admin-1", Action: "watermark_policy.publish", TargetType: "policy_publication", TargetID: publicationID, Summary: "发布 AI 生成内容水印管理规则", MetadataJSON: `{}`, CreatedAt: time.Date(2026, 8, 13, 7, 0, 0, 0, time.UTC)}
}

func watermarkPreferenceEvent(id string, userID string, remove bool, publicationID string) *model.UserWatermarkPreferenceEvent {
	return &model.UserWatermarkPreferenceEvent{ID: id, UserID: userID, RemoveWatermark: remove, PolicyPublicationID: publicationID, ResultStatus: "succeeded"}
}

func assertWatermarkRowCount(t *testing.T, db *gorm.DB, value any, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(value).Count(&count).Error; err != nil || count != want {
		t.Fatalf("%T row count = %d, want %d, err=%v", value, count, want, err)
	}
}
