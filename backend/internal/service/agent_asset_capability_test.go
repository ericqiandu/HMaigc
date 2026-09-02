package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func TestAgentAssetCapabilityPublishesOwnedResourceExactlyOnce(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentCapabilityResource(t, db, "publish-resource-1", "runtime-user", "")
	call := agentAssetPublishCapabilityCall("asset-publish-1", "publish-resource-1", "publish-mutation-1")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}

	first, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	second, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Output) != string(second.Output) {
		t.Fatalf("asset replay changed receipt: first=%s second=%s", first.Output, second.Output)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolAssetsPublish, first.Output)
	if err != nil {
		t.Fatal(err)
	}
	receipt := decoded.(agentruntime.AssetsPublishResult)
	if receipt.AssetID == "" || receipt.ResourceID != "publish-resource-1" || receipt.DomainProjectID != "runtime-project" || receipt.ClientMutationID != "publish-mutation-1" {
		t.Fatalf("asset receipt = %#v", receipt)
	}
	var assetCount, linkCount, representationCount int64
	if err := db.Model(&model.Asset{}).Where("id = ?", receipt.AssetID).Count(&assetCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProjectAssetLink{}).Where("project_id = ? AND asset_id = ?", "runtime-project", receipt.AssetID).Count(&linkCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AssetRepresentation{}).Where("resource_id = ?", "publish-resource-1").Count(&representationCount).Error; err != nil {
		t.Fatal(err)
	}
	if assetCount != 1 || linkCount != 1 || representationCount != 1 {
		t.Fatalf("published facts assets=%d links=%d representations=%d", assetCount, linkCount, representationCount)
	}
	var representation model.AssetRepresentation
	if err := db.Where("resource_id = ?", "publish-resource-1").Take(&representation).Error; err != nil {
		t.Fatal(err)
	}
	if representation.TaskID != "" {
		t.Fatalf("asset publication invented task identity %q", representation.TaskID)
	}
}

func TestAgentAssetCapabilityUsesExecutionTimeForPublicationAuthorizationAndAudit(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentCapabilityResource(t, db, "publish-resource-execution-time", "runtime-user", "")
	call := agentAssetPublishCapabilityCall("asset-publish-execution-time", "publish-resource-execution-time", "publish-mutation-execution-time")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	approvedAt := time.Now().UTC().Add(-2 * time.Hour)
	if result := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", "runtime-run", call.ToolCallID, call.ActionVersion).
		Update("approval_decided_at", approvedAt); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("backdate approval: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	executedAfter := time.Now().UTC().Add(-time.Second)
	result, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolAssetsPublish, result.Output)
	if err != nil {
		t.Fatal(err)
	}
	receipt := decoded.(agentruntime.AssetsPublishResult)
	var asset model.Asset
	if err := db.First(&asset, "id = ?", receipt.AssetID).Error; err != nil {
		t.Fatal(err)
	}
	if asset.CreatedAt.Before(executedAfter) {
		t.Fatalf("asset publication used approval time %s instead of execution time after %s", asset.CreatedAt, executedAfter)
	}
}

func TestAgentAssetCapabilityContributesPersistedPublicationDeliveryEvidence(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentCapabilityResource(t, db, "publish-resource-evidence", "runtime-user", "")
	call := agentAssetPublishCapabilityCall("asset-publish-evidence", "publish-resource-evidence", "publish-mutation-evidence")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	executed, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	if result := db.Model(&model.AgentToolCall{}).
		Where("run_id = ? AND tool_call_id = ? AND action_version = ?", "runtime-run", call.ToolCallID, call.ActionVersion).
		Updates(model.AgentToolCall{Status: agentruntime.ToolCallSucceeded, OutputJSON: string(executed.Output), UpdatedAt: time.Now().UTC()}); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("persist tool receipt: rows=%d err=%v", result.RowsAffected, result.Error)
	}

	evidence, err := svc.agentRuntimeDeliveryEvidence(agentRuntimeServiceScope(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Artifacts) != 1 {
		t.Fatalf("publication evidence = %#v", evidence.Artifacts)
	}
	artifact := evidence.Artifacts[0]
	if artifact.Kind != agentruntime.ArtifactImage || artifact.ResourceID != "publish-resource-evidence" ||
		artifact.URL != "/api/resources/publish-resource-evidence/file" || !artifact.ResourceReady ||
		artifact.PublicationID == "" || !artifact.Approved {
		t.Fatalf("publication artifact = %#v", artifact)
	}
	expected := agentruntime.ExpectedDelivery{
		Kind:              agentruntime.DeliveryGeneratedAsset,
		RequiredArtifacts: []agentruntime.ArtifactKind{agentruntime.ArtifactImage},
		CompletionCriteria: []agentruntime.DeliveryCriterion{
			{Fact: agentruntime.DeliveryFactPublication, Artifact: agentruntime.ArtifactImage},
		},
	}
	if verification := agentruntime.VerifyDelivery(expected, evidence); verification.Status != agentruntime.VerificationSatisfied {
		t.Fatalf("publication verification = %#v", verification)
	}
}

func TestAgentAssetPublicationFactUsesTeamTenantOwnershipInsteadOfCreatorIdentity(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentCapabilityResource(t, db, "team-publication-resource", "runtime-user", "")
	call := agentAssetPublishCapabilityCall("team-publication", "team-publication-resource", "team-publication-mutation")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	published, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolAssetsPublish, published.Output)
	if err != nil {
		t.Fatal(err)
	}
	receipt := decoded.(agentruntime.AssetsPublishResult)
	if err := db.Model(&model.Resource{}).Where("id = ?", receipt.ResourceID).
		Select("user_id", "team_id").
		Updates(model.Resource{UserID: "", TeamID: "runtime-team"}).Error; err != nil {
		t.Fatal(err)
	}

	teamScope := agentRuntimeServiceScope()
	teamScope.TenantKind = agentruntime.TenantTeam
	teamScope.TenantID = "runtime-team"
	teamScope.ActorUserID = "runtime-collaborator"
	fact, err := svc.repo.AgentCapabilityAssetPublicationForScope(teamScope, receipt.AssetID, receipt.ResourceID)
	if err != nil {
		t.Fatalf("same-team publication fact error = %v", err)
	}
	if fact.Asset.ID != receipt.AssetID || fact.Resource.TeamID != "runtime-team" {
		t.Fatalf("same-team publication fact = %#v", fact)
	}

	foreignScope := teamScope
	foreignScope.TenantID = "other-team"
	if _, err := svc.repo.AgentCapabilityAssetPublicationForScope(foreignScope, receipt.AssetID, receipt.ResourceID); err == nil ||
		errors.Is(err, repository.ErrAgentCapabilityAssetInvalid) {
		t.Fatalf("cross-team publication fact error = %v", err)
	}
}

func TestAgentAssetCapabilityRejectsConflictingRepublishWithoutPartialMutation(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentCapabilityResource(t, db, "publish-resource-conflict", "runtime-user", "")
	firstCall := agentAssetPublishCapabilityCallWithName(
		"asset-publish-conflict-1", "publish-resource-conflict", "角色参考图", "publish-mutation-conflict-1",
	)
	seedApprovedAgentCapabilityProposal(t, svc, db, firstCall)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	firstResult, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), firstCall)
	if err != nil {
		t.Fatal(err)
	}
	resolveSuccessfulAgentCapabilityForTest(t, svc, firstCall, firstResult)

	conflictingCall := agentAssetPublishCapabilityCallWithName(
		"asset-publish-conflict-2", "publish-resource-conflict", "冲突名称", "publish-mutation-conflict-2",
	)
	seedApprovedAgentCapabilityProposal(t, svc, db, conflictingCall)
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), conflictingCall); !agentCapabilityErrorCode(err, "asset_publication_conflict") {
		t.Fatalf("conflicting publication error = %v", err)
	}

	var assets []model.Asset
	if err := db.Find(&assets).Error; err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Title != "角色参考图" {
		t.Fatalf("conflicting publication changed assets: %#v", assets)
	}
	var representationCount int64
	if err := db.Model(&model.AssetRepresentation{}).
		Where("resource_id = ?", "publish-resource-conflict").Count(&representationCount).Error; err != nil {
		t.Fatal(err)
	}
	if representationCount != 1 {
		t.Fatalf("conflicting publication created %d representations", representationCount)
	}
	var retainedResource model.Resource
	if err := db.First(&retainedResource, "id = ?", "publish-resource-conflict").Error; err != nil {
		t.Fatal(err)
	}
	if retainedResource.Status != model.ResourceStatusReady || retainedResource.ObjectKey == "" {
		t.Fatalf("publication conflict discarded the generated resource: %#v", retainedResource)
	}
}

func TestAgentAssetCapabilityRepublishesSameResourceAsOneAsset(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentCapabilityResource(t, db, "publish-resource-replay", "runtime-user", "")
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}

	assetIDs := make([]string, 0, 2)
	for index, mutationID := range []string{"publish-resource-replay-1", "publish-resource-replay-2"} {
		call := agentAssetPublishCapabilityCall(
			"asset-publish-resource-replay-"+mutationID, "publish-resource-replay", mutationID,
		)
		seedApprovedAgentCapabilityProposal(t, svc, db, call)
		result, executeErr := registry.Execute(context.Background(), agentRuntimeServiceScope(), call)
		if executeErr != nil {
			t.Fatal(executeErr)
		}
		resolveSuccessfulAgentCapabilityForTest(t, svc, call, result)
		decoded, decodeErr := agentruntime.DecodeCapabilityResult(agentruntime.ToolAssetsPublish, result.Output)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		receipt := decoded.(agentruntime.AssetsPublishResult)
		if receipt.ClientMutationID != mutationID {
			t.Fatalf("publication %d mutation = %q", index, receipt.ClientMutationID)
		}
		assetIDs = append(assetIDs, receipt.AssetID)
	}
	if assetIDs[0] != assetIDs[1] {
		t.Fatalf("same resource published as multiple assets: %v", assetIDs)
	}
	var representationCount int64
	if err := db.Model(&model.AssetRepresentation{}).
		Where("resource_id = ?", "publish-resource-replay").Count(&representationCount).Error; err != nil {
		t.Fatal(err)
	}
	if representationCount != 1 {
		t.Fatalf("same resource created %d representations", representationCount)
	}
}

func TestAgentAssetCapabilityRejectsMutationReuseForDifferentResource(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentCapabilityResource(t, db, "publish-resource-mutation-a", "runtime-user", "")
	seedAgentCapabilityResource(t, db, "publish-resource-mutation-b", "runtime-user", "")
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	firstCall := agentAssetPublishCapabilityCall("asset-publish-mutation-a", "publish-resource-mutation-a", "reused-mutation")
	seedApprovedAgentCapabilityProposal(t, svc, db, firstCall)
	firstResult, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), firstCall)
	if err != nil {
		t.Fatal(err)
	}
	resolveSuccessfulAgentCapabilityForTest(t, svc, firstCall, firstResult)
	secondCall := agentAssetPublishCapabilityCall("asset-publish-mutation-b", "publish-resource-mutation-b", "reused-mutation")
	seedApprovedAgentCapabilityProposal(t, svc, db, secondCall)
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), secondCall); !agentCapabilityErrorCode(err, "asset_publication_conflict") {
		t.Fatalf("reused mutation error = %v", err)
	}
	var secondRepresentationCount int64
	if err := db.Model(&model.AssetRepresentation{}).
		Where("resource_id = ?", "publish-resource-mutation-b").Count(&secondRepresentationCount).Error; err != nil {
		t.Fatal(err)
	}
	if secondRepresentationCount != 0 {
		t.Fatalf("reused mutation created %d representations for the second resource", secondRepresentationCount)
	}
}

func TestAgentAssetCapabilityRejectsOtherTenantResource(t *testing.T) {
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://example.com")
	seedAgentCapabilityResource(t, db, "foreign-resource", "other-user", "")
	call := agentAssetPublishCapabilityCall("asset-publish-foreign", "foreign-resource", "publish-foreign")
	seedApprovedAgentCapabilityProposal(t, svc, db, call)
	registry, err := newAgentCapabilityRegistry(svc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), agentRuntimeServiceScope(), call); !agentCapabilityErrorCode(err, "asset_ownership_forbidden") {
		t.Fatalf("foreign resource error = %v", err)
	}
	var assetCount int64
	if err := db.Model(&model.AssetRepresentation{}).Where("resource_id = ?", "foreign-resource").Count(&assetCount).Error; err != nil {
		t.Fatal(err)
	}
	if assetCount != 0 {
		t.Fatalf("foreign resource created %d representations", assetCount)
	}
}

func agentAssetPublishCapabilityCall(toolCallID string, resourceID string, mutationID string) agentruntime.ToolCallDecision {
	return agentAssetPublishCapabilityCallWithName(toolCallID, resourceID, "角色参考图", mutationID)
}

func agentAssetPublishCapabilityCallWithName(toolCallID string, resourceID string, displayName string, mutationID string) agentruntime.ToolCallDecision {
	arguments, err := json.Marshal(agentruntime.AssetsPublishArguments{
		ResourceID: resourceID, DomainProjectID: "runtime-project", DisplayName: displayName, ClientMutationID: mutationID,
	})
	if err != nil {
		panic(err)
	}
	return agentruntime.ToolCallDecision{ToolCallID: toolCallID, ToolName: agentruntime.ToolAssetsPublish, ActionVersion: 1, Arguments: arguments}
}

func seedAgentCapabilityResource(t *testing.T, db *gorm.DB, resourceID string, userID string, teamID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&model.Resource{
		ID: resourceID, UserID: userID, TeamID: teamID, Kind: "image", Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: "agent/" + resourceID + ".png", MimeType: "image/png", Width: 1024, Height: 1024,
		CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
}
