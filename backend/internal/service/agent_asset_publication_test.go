package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type assetPublicationFixture struct {
	service        *Service
	db             *gorm.DB
	parentRun      model.AgentRun
	stageID        string
	stageVersion   int64
	revisionID     string
	resourceID     string
	taskID         string
	billingOrderID string
}

func TestApprovedMediaRevisionPublishesExactlyOnceToProjectAssets(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	command := assetPublicationReviewCommand(fixture)

	first, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.Publication == nil || first.Publication.Status != model.AgentAssetPublicationSucceeded || first.Publication.Replayed {
		t.Fatalf("first publication = %#v", first.Publication)
	}
	if first.Stage.Status != agentruntime.StageCompleted {
		t.Fatalf("first stage status = %q, want %q", first.Stage.Status, agentruntime.StageCompleted)
	}
	if err := fixture.db.Model(&model.AgentRun{}).Where("id = ?", fixture.parentRun.ID).
		Update("status", agentruntime.RunSucceeded).Error; err != nil {
		t.Fatal(err)
	}

	second, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.Publication == nil || !second.Publication.Replayed || second.Publication.AssetID != first.Publication.AssetID ||
		second.Publication.AssetVersionID != first.Publication.AssetVersionID || second.Publication.ProjectAssetLinkID != first.Publication.ProjectAssetLinkID {
		t.Fatalf("replayed publication = %#v, first = %#v", second.Publication, first.Publication)
	}
	different := command
	different.ClientRequestID = "approve-and-publish-as-style"
	different.PublicationIntent = &agentruntime.AssetPublicationIntent{
		PublicationPurpose: "style-library", TargetCategory: string(model.AssetCategoryStyle), TargetBindingKey: "visual-style",
	}
	_, err = fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, different,
	)
	if !errors.Is(err, repository.ErrProductionStageReviewConflict) {
		t.Fatalf("different terminal publication error = %v, want %v", err, repository.ErrProductionStageReviewConflict)
	}

	assertTableCount(t, fixture.db, "agent_asset_publications", 1)
	assertTableCount(t, fixture.db, "assets", 1)
	assertTableCount(t, fixture.db, "asset_versions", 1)
	assertTableCount(t, fixture.db, "asset_representations", 1)
	assertTableCount(t, fixture.db, "project_asset_links", 1)

	var representation model.AssetRepresentation
	if err := fixture.db.First(&representation).Error; err != nil {
		t.Fatal(err)
	}
	if representation.ResourceID != fixture.resourceID || representation.TaskID != fixture.taskID || representation.MediaType != "image" {
		t.Fatalf("representation = %#v", representation)
	}
	var publication model.AgentAssetPublication
	if err := fixture.db.First(&publication).Error; err != nil {
		t.Fatal(err)
	}
	var audit map[string]json.RawMessage
	if err := json.Unmarshal([]byte(publication.AuditJSON), &audit); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"artifactRevision", "modelRequest", "billing", "contentHash", "approval"} {
		if len(audit[key]) == 0 {
			t.Fatalf("audit is missing %q: %s", key, publication.AuditJSON)
		}
	}
	var facts struct {
		ProducerKind string `json:"producerKind"`
		Producer     struct {
			Specialist *struct {
				SpecialistRunID string `json:"specialistRunId"`
			} `json:"specialist"`
			MediaTool *json.RawMessage `json:"mediaTool"`
		} `json:"producer"`
		ModelRequest struct {
			TaskID         string `json:"taskId"`
			Provider       string `json:"provider"`
			Model          string `json:"model"`
			ParametersJSON string `json:"parametersJson"`
			Prompt         string `json:"prompt"`
		} `json:"modelRequest"`
		Billing struct {
			BillingOrderID     string `json:"billingOrderId"`
			Capability         string `json:"capability"`
			AmountMicrocredits int64  `json:"amountMicrocredits"`
			PriceVersion       int64  `json:"priceVersion"`
		} `json:"billing"`
		ContentHash string `json:"contentHash"`
	}
	if err := json.Unmarshal([]byte(publication.AuditJSON), &facts); err != nil {
		t.Fatal(err)
	}
	if facts.ModelRequest.TaskID != fixture.taskID || facts.ModelRequest.Provider != "kuaizi" ||
		facts.ModelRequest.Model != "image-2.0" || facts.ModelRequest.ParametersJSON != `{"size":"1024x1024","quality":"high"}` ||
		facts.ModelRequest.Prompt != "林夏角色定妆照" || facts.Billing.BillingOrderID != fixture.billingOrderID ||
		facts.Billing.Capability != "image" || facts.Billing.AmountMicrocredits != 1_000_000 ||
		facts.Billing.PriceVersion != 4 || facts.ContentHash == "" ||
		facts.ProducerKind != "specialist" || facts.Producer.Specialist == nil ||
		facts.Producer.Specialist.SpecialistRunID == "" || facts.Producer.MediaTool != nil {
		t.Fatalf("publication audit facts = %#v", facts)
	}
}

func TestApprovedIndependentAudioPublishesWithoutCopyingResource(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	beforeResources := tableCount(t, fixture.db, "resources")
	if err := fixture.db.Model(&model.Resource{}).Where("id = ?", fixture.resourceID).
		Select("kind", "object_key", "public_url", "mime_type", "width", "height").
		Updates(model.Resource{
			Kind: "audio", ObjectKey: "users/runtime-user/audio/narration.wav",
			PublicURL: "https://assets.example.com/narration.wav", MimeType: "audio/wav",
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.Task{}).Where("id = ?", fixture.taskID).
		Select("capability", "model", "prompt", "input_json").
		Updates(model.Task{
			Capability: "audio", Model: "voice-pro", Prompt: "旁白：雨夜的故事开始了",
			InputJSON: `{"voice":"narrator","format":"wav"}`,
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.BillingOrder{}).Where("id = ?", fixture.billingOrderID).
		Select("capability", "model").
		Updates(model.BillingOrder{Capability: "audio", Model: "voice-pro"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentSpecialistRun{}).Where("task_id = ?", fixture.taskID).
		Update("specialist_key", agentruntime.SpecialistAudio).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentArtifact{}).Where("id = ?", "publication-artifact-image").
		Select("artifact_key", "kind").
		Updates(model.AgentArtifact{ArtifactKey: "narration-track", Kind: "independent_audio"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentArtifactRevision{}).Where("id = ?", fixture.revisionID).
		Select("artifact_key", "kind", "payload_json").
		Updates(model.AgentArtifactRevision{
			ArtifactKey: "narration-track", Kind: "independent_audio",
			PayloadJSON: `{"title":"雨夜旁白","durationSeconds":12}`,
		}).Error; err != nil {
		t.Fatal(err)
	}

	command := assetPublicationReviewCommand(fixture)
	command.ClientRequestID = "approve-and-publish-audio"
	command.PublicationIntent = &agentruntime.AssetPublicationIntent{
		PublicationPurpose: "audio-library", TargetCategory: string(model.AssetCategoryOther), TargetBindingKey: "narration",
	}
	result, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Publication == nil || result.Publication.Status != model.AgentAssetPublicationSucceeded {
		t.Fatalf("audio publication = %#v", result.Publication)
	}
	if got := tableCount(t, fixture.db, "resources"); got != beforeResources {
		t.Fatalf("resource count = %d, want unchanged %d", got, beforeResources)
	}
	var representation model.AssetRepresentation
	if err := fixture.db.First(&representation, "id = ?", result.Publication.RepresentationID).Error; err != nil {
		t.Fatal(err)
	}
	if representation.ResourceID != fixture.resourceID || representation.MediaType != "audio" {
		t.Fatalf("audio representation = %#v", representation)
	}
}

func TestPublishAssetRejectsResourceTaskCapabilityMismatch(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	if err := fixture.db.Model(&model.Task{}).Where("id = ?", fixture.taskID).Update("capability", "audio").Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.BillingOrder{}).Where("id = ?", fixture.billingOrderID).Update("capability", "audio").Error; err != nil {
		t.Fatal(err)
	}
	_, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, assetPublicationReviewCommand(fixture),
	)
	if !errors.Is(err, ErrAssetPublicationBillingMissing) {
		t.Fatalf("ReviewProductionStage() error = %v, want %v", err, ErrAssetPublicationBillingMissing)
	}
	assertTableCount(t, fixture.db, "assets", 0)
	assertTableCount(t, fixture.db, "agent_asset_publications", 0)
}

func TestPublishAssetRequiresInternalTaskAudience(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	if err := fixture.db.Model(&model.Task{}).Where("id = ?", fixture.taskID).
		Update("audience", model.TaskAudienceCustomer).Error; err != nil {
		t.Fatal(err)
	}
	_, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, assetPublicationReviewCommand(fixture),
	)
	if !errors.Is(err, repository.ErrProductionRuntimeSnapshotInvalid) {
		t.Fatalf("ReviewProductionStage() error = %v, want %v", err, repository.ErrProductionRuntimeSnapshotInvalid)
	}
	assertTableCount(t, fixture.db, "assets", 0)
	assertTableCount(t, fixture.db, "agent_asset_publications", 0)
}

func TestPublishAssetRejectsApprovalWithoutDeclaredPublicationIntent(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	approved, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID,
		agentruntime.StageReviewCommand{
			StageVersion: fixture.stageVersion, RevisionID: fixture.revisionID,
			Decision: agentruntime.StageReviewApprove, ClientRequestID: "approve-without-publication",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.service.PublishAsset(context.Background(), specialistRuntimeScope(), PublishAssetCommand{
		AuthorizationKind:  repository.AgentAssetPublicationDirectReview,
		ArtifactRevisionID: fixture.revisionID, ReviewRevisionID: fixture.revisionID, PublicationPurpose: "character-library",
		TargetCategory: model.AssetCategoryCharacter, TargetBindingKey: "hero",
		ApprovedByUserID: specialistRuntimeScope().ActorUserID, StageReviewID: approved.ReviewID,
	})
	if !errors.Is(err, ErrAssetPublicationApprovalRequired) {
		t.Fatalf("PublishAsset() error = %v, want %v", err, ErrAssetPublicationApprovalRequired)
	}
	assertTableCount(t, fixture.db, "agent_asset_publications", 0)
	assertTableCount(t, fixture.db, "assets", 0)
}

func TestPublishAssetRequiresCurrentProjectEditPermission(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	if err := fixture.db.Model(&model.Project{}).Where("id = ?", specialistRuntimeScope().DomainProjectID).
		Update("user_id", "different-project-owner").Error; err != nil {
		t.Fatal(err)
	}
	_, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, assetPublicationReviewCommand(fixture),
	)
	if !errors.Is(err, ErrAssetPublicationApprovalRequired) {
		t.Fatalf("ReviewProductionStage() error = %v, want %v", err, ErrAssetPublicationApprovalRequired)
	}
	assertTableCount(t, fixture.db, "agent_asset_publications", 0)
	assertTableCount(t, fixture.db, "assets", 0)
}

func TestPublicationFailureRollsBackAssetRowsAndCanRetryExactApproval(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	if err := fixture.db.Exec(`CREATE TRIGGER fail_agent_representation BEFORE INSERT ON asset_representations BEGIN SELECT RAISE(ABORT, 'forced representation failure'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	command := assetPublicationReviewCommand(fixture)

	_, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if !errors.Is(err, ErrAssetPublicationFailed) {
		t.Fatalf("ReviewProductionStage() error = %v, want %v", err, ErrAssetPublicationFailed)
	}
	assertTableCount(t, fixture.db, "assets", 0)
	assertTableCount(t, fixture.db, "asset_versions", 0)
	assertTableCount(t, fixture.db, "asset_representations", 0)
	assertTableCount(t, fixture.db, "project_asset_links", 0)
	assertTableCount(t, fixture.db, "agent_asset_publications", 1)
	var failed model.AgentAssetPublication
	if err := fixture.db.First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.AgentAssetPublicationFailed || failed.LastErrorCode == "" {
		t.Fatalf("failed publication = %#v", failed)
	}
	if got := assetPublicationTimelineCount(t, fixture.db, agentruntime.AssetPublicationFailedType); got != 1 {
		t.Fatalf("failure timeline count = %d, want 1", got)
	}
	var revision model.AgentArtifactRevision
	if err := fixture.db.First(&revision, "id = ?", fixture.revisionID).Error; err != nil {
		t.Fatal(err)
	}
	if revision.ResourceID != fixture.resourceID || revision.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
		t.Fatalf("artifact revision changed after failed publication: %#v", revision)
	}

	if err := fixture.db.Exec(`DROP TRIGGER fail_agent_representation`).Error; err != nil {
		t.Fatal(err)
	}
	retried, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Publication == nil || retried.Publication.Status != model.AgentAssetPublicationSucceeded {
		t.Fatalf("retried publication = %#v", retried.Publication)
	}
	assertTableCount(t, fixture.db, "agent_asset_publications", 1)
	assertTableCount(t, fixture.db, "assets", 1)
	assertTableCount(t, fixture.db, "asset_representations", 1)
	if got := assetPublicationTimelineCount(t, fixture.db, agentruntime.AssetPublicationFailedType); got != 1 {
		t.Fatalf("failure timeline count after retry = %d, want 1", got)
	}
	if got := assetPublicationTimelineCount(t, fixture.db, agentruntime.AssetPublicationContentType); got != 1 {
		t.Fatalf("success timeline count after retry = %d, want 1", got)
	}
}

func TestFailedPublicationRetryAfterRunTerminatesRecoversExactAuthorization(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	if err := fixture.db.Exec(`CREATE TRIGGER fail_agent_representation BEFORE INSERT ON asset_representations BEGIN SELECT RAISE(ABORT, 'forced representation failure'); END;`).Error; err != nil {
		t.Fatal(err)
	}
	command := assetPublicationReviewCommand(fixture)
	_, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if !errors.Is(err, ErrAssetPublicationFailed) {
		t.Fatalf("first ReviewProductionStage() error = %v, want %v", err, ErrAssetPublicationFailed)
	}
	var failed model.AgentAssetPublication
	if err := fixture.db.First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Model(&model.AgentRun{}).Where("id = ?", fixture.parentRun.ID).
		Update("status", agentruntime.RunSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.Exec(`DROP TRIGGER fail_agent_representation`).Error; err != nil {
		t.Fatal(err)
	}

	recovered, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Publication == nil || recovered.Publication.ID != failed.ID ||
		recovered.Publication.Status != model.AgentAssetPublicationSucceeded {
		t.Fatalf("terminal retry publication = %#v, failed = %#v", recovered.Publication, failed)
	}
	var stored model.AgentAssetPublication
	if err := fixture.db.First(&stored, "id = ?", failed.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Version != failed.Version+1 || stored.Status != model.AgentAssetPublicationSucceeded {
		t.Fatalf("terminal retry did not recover failure fact: before=%#v after=%#v", failed, stored)
	}
	assertTableCount(t, fixture.db, "agent_asset_publications", 1)
	assertTableCount(t, fixture.db, "assets", 1)
	assertTableCount(t, fixture.db, "asset_versions", 1)
	assertTableCount(t, fixture.db, "asset_representations", 1)
	if got := assetPublicationTimelineCount(t, fixture.db, agentruntime.AssetPublicationFailedType); got != 1 {
		t.Fatalf("failure timeline count = %d, want 1", got)
	}
	if got := assetPublicationTimelineCount(t, fixture.db, agentruntime.AssetPublicationContentType); got != 1 {
		t.Fatalf("success timeline count = %d, want 1", got)
	}
}

func TestPublishedAssetReplayRejectsCorruptedAssetRelationships(t *testing.T) {
	fixture := newAssetPublicationFixture(t)
	command := assetPublicationReviewCommand(fixture)
	first, err := fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.Publication == nil {
		t.Fatal("first publication is nil")
	}
	if err := fixture.db.Model(&model.ProjectAssetLink{}).
		Where("id = ?", first.Publication.ProjectAssetLinkID).
		Update("project_id", "different-project").Error; err != nil {
		t.Fatal(err)
	}

	_, err = fixture.service.ReviewProductionStage(
		context.Background(), specialistRuntimeScope(), fixture.parentRun, fixture.stageID, command,
	)
	if !errors.Is(err, ErrAssetPublicationConflict) {
		t.Fatalf("corrupted replay error = %v, want %v", err, ErrAssetPublicationConflict)
	}
}

func newAssetPublicationFixture(t *testing.T) assetPublicationFixture {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("asset publication fixture must not call the provider")
	}))
	t.Cleanup(server.Close)
	request := specialistRuntimeRequestFixture("runtime-token-agent-model", "deepseek-v4-flash")
	svc, db, runtimeFixture := newSpecialistRuntimeFixture(t, server.URL, request)
	parentRun := specialistParentRun(t, svc, db, runtimeFixture.channelModel, request)
	now := time.Now().UTC()
	scope := specialistRuntimeScope()
	resourceID := "publication-resource-image"
	taskID := "publication-task-image"
	billingOrderID := "publication-billing-image"
	revisionID := "publication-revision-image"
	artifactID := "publication-artifact-image"
	specialistRunID := "publication-specialist-image"
	providerRequestID := "publication-provider-request-image"

	resource := model.Resource{
		ID: resourceID, UserID: scope.ActorUserID, Kind: "image", Status: model.ResourceStatusReady,
		Provider: "oss", ObjectKey: "users/runtime-user/image/character.png", PublicURL: "https://assets.example.com/character.png",
		MimeType: "image/png", Size: 4096, Width: 1024, Height: 1024, ETag: "publication-etag", CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: billingOrderID, UserID: scope.ActorUserID, IdempotencyKey: "publication-billing-idempotency",
		TaskID: taskID, ChannelID: runtimeFixture.channel.ID, ChannelModelID: runtimeFixture.channelModel.ID,
		Model: "image-2.0", Capability: "image", Scene: "agent_character_asset", BillingMode: "fixed_request",
		PriceVersion: 4, PriceTierID: "image-high-1k", PricingResolution: "1K", PricingInputVariant: "standard",
		UnitPriceMicrocredits: 1_000_000, MultiplierBasisPoints: 10_000, Quantity: 1, AmountMicrocredits: 1_000_000,
		Status: model.BillingStatusSettled, ProviderRequestID: providerRequestID, ProviderBillingOrderID: "supplier-order-1",
		ProviderBillingAmount: 1_000_000, ProviderBillingStatus: "settled", ProviderBillingUnit: "CNY_MICRO",
		SettledAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	task := model.Task{
		ID: taskID, UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal, ProjectID: scope.CanvasID,
		Type: "agent_runtime_media", Capability: "image", Status: model.TaskStatusSucceeded, Stage: "已完成", Progress: 100,
		Prompt: "林夏角色定妆照", Provider: "kuaizi", Model: "image-2.0", BillingOrderID: billingOrderID,
		ProviderEndpointVersionID: "endpoint-version-1", ProviderCredentialVersionID: "credential-version-1",
		ProviderRequestID: providerRequestID, InputJSON: `{"size":"1024x1024","quality":"high"}`,
		ResultJSON: `{"resourceId":"publication-resource-image"}`, StartedAt: &now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	specialistRun := model.AgentSpecialistRun{
		ID: specialistRunID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		StageID: request.StageID, SpecialistKey: agentruntime.SpecialistVisual, SpecialistVersion: 3,
		Objective: "生成角色定妆图", ModelRecordID: parentRun.ModelRecordID, ModelKey: parentRun.ModelKey,
		ToolSchemaVersion: agentruntime.ProductionToolSchemaVersion, InputRevisionsJSON: `[]`, SkillVersionsJSON: `[]`,
		ToolAllowlistJSON: `[]`, ExpectedOutputSchema: "character_image.v1", ExpectedDeliveryJSON: `{"kind":"answer","completionCriteria":[{"fact":"final_message"}]}`,
		TaskID: taskID, BillingOrderID: billingOrderID, Attempt: 1, ProviderRequestID: providerRequestID,
		Status: model.AgentSpecialistRunSucceeded, Version: 2, ResultSummary: "角色定妆图已生成", ResultJSON: `{"resourceId":"publication-resource-image"}`,
		CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}
	artifact := model.AgentArtifact{
		ID: artifactID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		ArtifactKey: "hero-character", Kind: "character_image", HeadRevision: 1,
		LifecycleStatus: model.AgentArtifactLifecycleActive, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	revision := model.AgentArtifactRevision{
		ID: revisionID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		ArtifactID: artifactID, ArtifactKey: artifact.ArtifactKey, Revision: 1, Kind: artifact.Kind, SchemaVersion: 1,
		PayloadJSON: `{"title":"林夏角色定妆","description":"冷静敏锐的女记者"}`, ResourceID: resourceID,
		UpstreamRevisionsJSON: `[]`, ModelRequestIdentity: providerRequestID, SkillVersionsJSON: `[]`,
		CreatedByRunID: scope.RunID, CreatedBySpecialistID: specialistRunID,
		LifecycleStatus: model.AgentArtifactRevisionAwaitingReview, CreatedAt: now,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&specialistRun).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&artifact).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionStage{}).Where("id = ?", request.StageID).
		Select("status", "version", "review_revision_id", "updated_at").
		Updates(model.AgentProductionStage{
			Status: agentruntime.StageAwaitingReview, Version: 2, ReviewRevisionID: revisionID, UpdatedAt: now,
		}).Error; err != nil {
		t.Fatal(err)
	}
	return assetPublicationFixture{
		service: svc, db: db, parentRun: parentRun, stageID: request.StageID, stageVersion: 2,
		revisionID: revisionID, resourceID: resourceID, taskID: taskID, billingOrderID: billingOrderID,
	}
}

func assetPublicationReviewCommand(fixture assetPublicationFixture) agentruntime.StageReviewCommand {
	return agentruntime.StageReviewCommand{
		StageVersion: fixture.stageVersion, RevisionID: fixture.revisionID,
		Decision: agentruntime.StageReviewApprove, ClientRequestID: "approve-and-publish-character",
		PublicationIntent: &agentruntime.AssetPublicationIntent{
			PublicationPurpose: "character-library", TargetCategory: string(model.AssetCategoryCharacter), TargetBindingKey: "hero",
		},
	}
}

func assertTableCount(t *testing.T, db *gorm.DB, table string, want int64) {
	t.Helper()
	count := tableCount(t, db, table)
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}

func tableCount(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

func assetPublicationTimelineCount(t *testing.T, db *gorm.DB, contentType string) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&model.AgentTimelineItem{}).
		Where("content_json LIKE ?", `%"contentType":"`+contentType+`"%`).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}
