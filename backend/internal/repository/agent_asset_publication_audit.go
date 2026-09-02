package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

type agentAssetPublicationAudit struct {
	ProducerKind     agentAssetPublicationProducerKind       `json:"producerKind"`
	Producer         agentAssetPublicationProducerAudit      `json:"producer"`
	Authorization    agentAssetPublicationAuthorizationAudit `json:"authorization"`
	ArtifactRevision agentAssetPublicationArtifactAudit      `json:"artifactRevision"`
	ModelRequest     agentAssetPublicationModelAudit         `json:"modelRequest"`
	Billing          agentAssetPublicationBillingAudit       `json:"billing"`
	ContentHash      string                                  `json:"contentHash"`
	Approval         agentAssetPublicationApprovalAudit      `json:"approval"`
}

type agentAssetPublicationProducerAudit struct {
	Specialist *agentAssetPublicationSpecialistAudit `json:"specialist,omitempty"`
	MediaTool  *agentAssetPublicationMediaToolAudit  `json:"mediaTool,omitempty"`
}

type agentAssetPublicationSpecialistAudit struct {
	SpecialistRunID   string                     `json:"specialistRunId"`
	SpecialistKey     agentruntime.SpecialistKey `json:"specialistKey"`
	SpecialistVersion int                        `json:"specialistVersion"`
	RunID             string                     `json:"runId"`
}

type agentAssetPublicationMediaToolAudit struct {
	RunID                    string `json:"runId"`
	ToolCallID               string `json:"toolCallId"`
	ActionVersion            int    `json:"actionVersion"`
	AgentRequestIdentity     string `json:"agentRequestIdentity"`
	CandidateRequestIdentity string `json:"candidateRequestIdentity"`
	CandidateOrdinal         int    `json:"candidateOrdinal"`
}

type agentAssetPublicationAuthorizationAudit struct {
	Kind                AgentAssetPublicationAuthorizationKind `json:"kind"`
	ApprovalItemID      string                                 `json:"approvalItemId"`
	StageID             string                                 `json:"stageId"`
	ClientRequestID     string                                 `json:"clientRequestId"`
	ReviewRevisionID    string                                 `json:"reviewRevisionId"`
	SelectionRevisionID string                                 `json:"selectionRevisionId,omitempty"`
}

type agentAssetPublicationArtifactAudit struct {
	ArtifactID          string `json:"artifactId"`
	ArtifactRevisionID  string `json:"artifactRevisionId"`
	ArtifactKey         string `json:"artifactKey"`
	Kind                string `json:"kind"`
	SchemaVersion       int    `json:"schemaVersion"`
	ResourceID          string `json:"resourceId"`
	CreatedByRunID      string `json:"createdByRunId"`
	CreatedBySpecialist string `json:"createdBySpecialist"`
}

type agentAssetPublicationModelAudit struct {
	TaskID                      string `json:"taskId"`
	Provider                    string `json:"provider"`
	Model                       string `json:"model"`
	ProviderRequestID           string `json:"providerRequestId"`
	ProviderEndpointVersionID   string `json:"providerEndpointVersionId"`
	ProviderCredentialVersionID string `json:"providerCredentialVersionId"`
	ParametersJSON              string `json:"parametersJson"`
	Prompt                      string `json:"prompt"`
}

type agentAssetPublicationBillingAudit struct {
	BillingOrderID         string `json:"billingOrderId"`
	Capability             string `json:"capability"`
	Scene                  string `json:"scene"`
	BillingMode            string `json:"billingMode"`
	PriceVersion           int64  `json:"priceVersion"`
	PriceTierID            string `json:"priceTierId"`
	PricingResolution      string `json:"pricingResolution"`
	PricingInputVariant    string `json:"pricingInputVariant"`
	UnitPriceMicrocredits  int64  `json:"unitPriceMicrocredits"`
	MultiplierBasisPoints  int64  `json:"multiplierBasisPoints"`
	Quantity               int64  `json:"quantity"`
	AmountMicrocredits     int64  `json:"amountMicrocredits"`
	ProviderBillingOrderID string `json:"providerBillingOrderId"`
	ProviderBillingAmount  int64  `json:"providerBillingAmount"`
	ProviderBillingStatus  string `json:"providerBillingStatus"`
	ProviderBillingUnit    string `json:"providerBillingUnit"`
}

type agentAssetPublicationApprovalAudit struct {
	StageReviewID      string              `json:"stageReviewId"`
	ApprovedByUserID   string              `json:"approvedByUserId"`
	PublicationPurpose string              `json:"publicationPurpose"`
	TargetCategory     model.AssetCategory `json:"targetCategory"`
	TargetBindingKey   string              `json:"targetBindingKey"`
}

func buildAgentAssetPublicationAuditJSON(
	input PublishAgentAssetInput,
	authorization agentAssetPublicationAuthorization,
	revision model.AgentArtifactRevision,
	resource model.Resource,
	provenance agentAssetPublicationProvenance,
) (string, error) {
	if !validAgentAssetPublicationProvenance(provenance) {
		return "", ErrAgentAssetPublicationBillingMissing
	}
	task := provenance.Task
	order := provenance.BillingOrder
	hash := sha256.New()
	selectionRevisionID := ""
	selectionPayload := ""
	selectionUpstream := ""
	if authorization.SelectionRevision != nil {
		selectionRevisionID = authorization.SelectionRevision.ID
		selectionPayload = authorization.SelectionRevision.PayloadJSON
		selectionUpstream = authorization.SelectionRevision.UpstreamRevisionsJSON
	}
	for _, value := range []string{
		string(input.AuthorizationKind), authorization.ApprovalItem.ID, authorization.ApprovalItem.ContentJSON,
		authorization.ReviewRevision.ID, authorization.ReviewRevision.PayloadJSON, authorization.ReviewRevision.UpstreamRevisionsJSON,
		selectionRevisionID, selectionPayload, selectionUpstream,
		revision.ID, revision.PayloadJSON, revision.ResourceID, revision.UpstreamRevisionsJSON,
		revision.SkillVersionsJSON, resource.ID, resource.ETag, resource.ObjectKey,
		input.PublicationPurpose, string(input.TargetCategory), input.TargetBindingKey,
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	audit := agentAssetPublicationAudit{
		ProducerKind: provenance.ProducerKind,
		Authorization: agentAssetPublicationAuthorizationAudit{
			Kind: input.AuthorizationKind, ApprovalItemID: authorization.ApprovalItem.ID,
			StageID: authorization.Approval.StageID, ClientRequestID: authorization.Approval.ClientRequestID,
			ReviewRevisionID: authorization.ReviewRevision.ID, SelectionRevisionID: selectionRevisionID,
		},
		ArtifactRevision: agentAssetPublicationArtifactAudit{
			ArtifactID: revision.ArtifactID, ArtifactRevisionID: revision.ID, ArtifactKey: revision.ArtifactKey,
			Kind: revision.Kind, SchemaVersion: revision.SchemaVersion, ResourceID: resource.ID,
			CreatedByRunID: revision.CreatedByRunID, CreatedBySpecialist: revision.CreatedBySpecialistID,
		},
		ModelRequest: agentAssetPublicationModelAudit{
			TaskID: task.ID, Provider: task.Provider, Model: task.Model, ProviderRequestID: task.ProviderRequestID,
			ProviderEndpointVersionID: task.ProviderEndpointVersionID, ProviderCredentialVersionID: task.ProviderCredentialVersionID,
			ParametersJSON: task.InputJSON, Prompt: task.Prompt,
		},
		Billing: agentAssetPublicationBillingAudit{
			BillingOrderID: order.ID, Capability: order.Capability, Scene: order.Scene, BillingMode: order.BillingMode,
			PriceVersion: order.PriceVersion, PriceTierID: order.PriceTierID, PricingResolution: order.PricingResolution,
			PricingInputVariant: order.PricingInputVariant, UnitPriceMicrocredits: order.UnitPriceMicrocredits,
			MultiplierBasisPoints: order.MultiplierBasisPoints, Quantity: order.Quantity, AmountMicrocredits: order.AmountMicrocredits,
			ProviderBillingOrderID: order.ProviderBillingOrderID, ProviderBillingAmount: order.ProviderBillingAmount,
			ProviderBillingStatus: order.ProviderBillingStatus, ProviderBillingUnit: order.ProviderBillingUnit,
		},
		ContentHash: hex.EncodeToString(hash.Sum(nil)),
		Approval: agentAssetPublicationApprovalAudit{
			StageReviewID: input.StageReviewID, ApprovedByUserID: input.ApprovedByUserID,
			PublicationPurpose: input.PublicationPurpose, TargetCategory: input.TargetCategory, TargetBindingKey: input.TargetBindingKey,
		},
	}
	if provenance.Specialist != nil {
		audit.Producer.Specialist = &agentAssetPublicationSpecialistAudit{
			SpecialistRunID: provenance.Specialist.ID, SpecialistKey: provenance.Specialist.SpecialistKey,
			SpecialistVersion: provenance.Specialist.SpecialistVersion, RunID: provenance.Specialist.RunID,
		}
	}
	if provenance.MediaTool != nil {
		audit.Producer.MediaTool = &agentAssetPublicationMediaToolAudit{
			RunID: input.Scope.RunID, ToolCallID: provenance.MediaTool.ToolCall.ToolCallID,
			ActionVersion:            provenance.MediaTool.ToolCall.ActionVersion,
			AgentRequestIdentity:     provenance.MediaTool.RequestIdentity,
			CandidateRequestIdentity: provenance.MediaTool.Candidate.ProviderRequestIdentity,
			CandidateOrdinal:         provenance.MediaTool.CandidateOrdinal,
		}
	}
	encoded, err := json.Marshal(audit)
	return string(encoded), err
}

func validAgentAssetPublicationProvenance(provenance agentAssetPublicationProvenance) bool {
	switch provenance.ProducerKind {
	case agentAssetPublicationProducerSpecialist:
		return provenance.Specialist != nil && provenance.MediaTool == nil &&
			provenance.Specialist.ID != "" && provenance.Task.ID != "" && provenance.BillingOrder.ID != ""
	case agentAssetPublicationProducerMediaTool:
		return provenance.Specialist == nil && provenance.MediaTool != nil &&
			provenance.MediaTool.ToolCall.ToolCallID != "" && provenance.MediaTool.Candidate.Validate() == nil &&
			provenance.Task.ID != "" && provenance.BillingOrder.ID != ""
	default:
		return false
	}
}

func validateAgentAssetPublicationAudit(audit agentAssetPublicationAudit) error {
	if audit.ContentHash == "" || !audit.Authorization.Kind.Valid() || audit.Authorization.ApprovalItemID == "" ||
		audit.Authorization.StageID == "" || audit.Authorization.ClientRequestID == "" || audit.Authorization.ReviewRevisionID == "" {
		return ErrAgentAssetPublicationConflict
	}
	switch audit.ProducerKind {
	case agentAssetPublicationProducerSpecialist:
		if audit.Producer.Specialist == nil || audit.Producer.MediaTool != nil || audit.Producer.Specialist.SpecialistRunID == "" {
			return ErrAgentAssetPublicationConflict
		}
	case agentAssetPublicationProducerMediaTool:
		if audit.Producer.Specialist != nil || audit.Producer.MediaTool == nil || audit.Producer.MediaTool.ToolCallID == "" ||
			audit.Authorization.Kind != AgentAssetPublicationCandidateSelection || audit.Authorization.SelectionRevisionID == "" {
			return ErrAgentAssetPublicationConflict
		}
	default:
		return ErrAgentAssetPublicationConflict
	}
	return nil
}

func buildPublishedAssetJSON(
	input PublishAgentAssetInput,
	revision model.AgentArtifactRevision,
	resource model.Resource,
	auditJSON string,
) (string, string, string, error) {
	assetPayload, err := json.Marshal(struct {
		Source             string              `json:"source"`
		ArtifactRevisionID string              `json:"artifactRevisionId"`
		TargetBindingKey   string              `json:"targetBindingKey"`
		Category           model.AssetCategory `json:"category"`
	}{Source: "agent_artifact_revision", ArtifactRevisionID: revision.ID, TargetBindingKey: input.TargetBindingKey, Category: input.TargetCategory})
	if err != nil {
		return "", "", "", err
	}
	definition, err := json.Marshal(struct {
		ArtifactPayload json.RawMessage `json:"artifactPayload"`
		Audit           json.RawMessage `json:"audit"`
	}{ArtifactPayload: json.RawMessage(revision.PayloadJSON), Audit: json.RawMessage(auditJSON)})
	if err != nil {
		return "", "", "", err
	}
	metadata, err := json.Marshal(struct {
		Source             string `json:"source"`
		ArtifactRevisionID string `json:"artifactRevisionId"`
		ResourceETag       string `json:"resourceEtag"`
		PublicationPurpose string `json:"publicationPurpose"`
		TargetBindingKey   string `json:"targetBindingKey"`
	}{Source: "agent_artifact_revision", ArtifactRevisionID: revision.ID, ResourceETag: resource.ETag,
		PublicationPurpose: input.PublicationPurpose, TargetBindingKey: input.TargetBindingKey})
	return string(assetPayload), string(definition), string(metadata), err
}

func agentAssetRecordID(namespace string, publicationID string) string {
	digest := sha256.Sum256([]byte(namespace + "\x00" + publicationID))
	value := hex.EncodeToString(digest[:16])
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:]
}
