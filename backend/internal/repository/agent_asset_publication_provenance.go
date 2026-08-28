package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

type agentAssetPublicationProducerKind string

const (
	agentAssetPublicationProducerSpecialist agentAssetPublicationProducerKind = "specialist"
	agentAssetPublicationProducerMediaTool  agentAssetPublicationProducerKind = "media_tool"
)

type agentAssetPublicationProvenance struct {
	ProducerKind agentAssetPublicationProducerKind
	Task         model.Task
	BillingOrder model.BillingOrder
	Specialist   *model.AgentSpecialistRun
	MediaTool    *agentAssetPublicationMediaToolProvenance
}

type agentAssetPublicationMediaToolProvenance struct {
	ToolCall         model.AgentToolCall
	Candidate        agentruntime.MediaCandidateContent
	RequestIdentity  string
	CandidateOrdinal int
}

type storedMediaGenerationInput struct {
	GenerationModel         agentruntime.GenerationModelSelection `json:"generationModel"`
	GenerationModelRecordID string                                `json:"generationModelRecordId"`
	Capability              string                                `json:"capability"`
	Parameters              json.RawMessage                       `json:"parameters"`
	RequestIdentity         string                                `json:"requestIdentity"`
	Commercial              storedMediaGenerationCommercial       `json:"commercial"`
}

type storedMediaGenerationCommercial struct {
	TaskID                  string `json:"taskId"`
	BillingIdempotencyKey   string `json:"billingIdempotencyKey"`
	TaskType                string `json:"taskType"`
	Operation               string `json:"operation"`
	Prompt                  string `json:"prompt"`
	Capability              string `json:"capability"`
	ChannelID               string `json:"channelId"`
	ChannelModelID          string `json:"channelModelId"`
	ModelKey                string `json:"modelKey"`
	ParametersJSON          string `json:"parametersJson"`
	Quantity                int64  `json:"quantity"`
	AmountMicrocredits      int64  `json:"amountMicrocredits"`
	PerTaskMicrocredits     int64  `json:"perTaskAmountMicrocredits"`
	PriceVersion            int64  `json:"priceVersion"`
	BillingMode             string `json:"billingMode"`
	PricingResolution       string `json:"pricingResolution"`
	PricingInputVariant     string `json:"pricingInputVariant"`
	BillingQuoteFingerprint string `json:"billingQuoteFingerprint"`
	QuoteID                 string `json:"quoteId"`
	ApprovalFingerprint     string `json:"approvalFingerprint"`
}

func loadAgentAssetPublicationProvenanceTx(
	tx *gorm.DB,
	input PublishAgentAssetInput,
	authorization agentAssetPublicationAuthorization,
	revision model.AgentArtifactRevision,
	resource model.Resource,
) (agentAssetPublicationProvenance, error) {
	if revision.CreatedByRunID != input.Scope.RunID {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	if revision.CreatedBySpecialistID != "" {
		return loadSpecialistPublicationProvenanceTx(tx, input, authorization, revision, resource)
	}
	return loadMediaToolPublicationProvenanceTx(tx, input, revision, resource)
}

func loadSpecialistPublicationProvenanceTx(
	tx *gorm.DB,
	input PublishAgentAssetInput,
	authorization agentAssetPublicationAuthorization,
	revision model.AgentArtifactRevision,
	resource model.Resource,
) (agentAssetPublicationProvenance, error) {
	var specialist model.AgentSpecialistRun
	if err := agentSpecialistScopeQuery(tx, input.Scope).Where("id = ?", revision.CreatedBySpecialistID).First(&specialist).Error; err != nil ||
		specialist.Status != model.AgentSpecialistRunSucceeded || specialist.ProviderRequestID != revision.ModelRequestIdentity ||
		specialist.TaskID == "" || specialist.BillingOrderID == "" || specialist.StageID != authorization.Approval.StageID {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	var tasks []model.Task
	if err := tx.Where("id = ? AND user_id = ? AND audience = ? AND status = ? AND provider_request_id = ? AND billing_order_id = ? AND project_id IN ?",
		specialist.TaskID, input.Scope.ActorUserID, model.TaskAudienceInternal, model.TaskStatusSucceeded, revision.ModelRequestIdentity,
		specialist.BillingOrderID, []string{input.Scope.CanvasID, input.Scope.DomainProjectID}).Find(&tasks).Error; err != nil || len(tasks) != 1 {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	task := tasks[0]
	if task.Capability != resource.Kind {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	order, err := loadSettledPublicationBillingOrderTx(tx, input, task, specialist.BillingOrderID)
	if err != nil {
		return agentAssetPublicationProvenance{}, err
	}
	return agentAssetPublicationProvenance{
		ProducerKind: agentAssetPublicationProducerSpecialist,
		Task:         task, BillingOrder: order, Specialist: &specialist,
	}, nil
}

func loadMediaToolPublicationProvenanceTx(
	tx *gorm.DB,
	input PublishAgentAssetInput,
	revision model.AgentArtifactRevision,
	resource model.Resource,
) (agentAssetPublicationProvenance, error) {
	if revision.Kind != mediaCandidateArtifactKind || revision.SchemaVersion != 1 ||
		(resource.Kind != string(agentruntime.ArtifactImage) && resource.Kind != string(agentruntime.ArtifactAudio)) {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationResourceMissing
	}
	candidate, err := agentruntime.DecodeMediaCandidateContent([]byte(revision.PayloadJSON))
	if err != nil || candidate.CandidateKey != revision.ArtifactKey || candidate.ResourceID != revision.ResourceID ||
		candidate.ProviderRequestIdentity != revision.ModelRequestIdentity || string(candidate.MediaKind) != resource.Kind {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationResourceMissing
	}

	var calls []model.AgentToolCall
	if err := tx.Where("run_id = ? AND tool_name = ? AND status = ?", input.Scope.RunID,
		string(agentruntime.ToolMediaGenerate), agentruntime.ToolCallSucceeded).
		Order("created_at ASC, id ASC").Find(&calls).Error; err != nil {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	var matchedCall *model.AgentToolCall
	var matchedResult agentruntime.MediaGenerationToolResult
	matchedOrdinal := 0
	for index := range calls {
		result, decodeErr := agentruntime.DecodeMediaGenerationToolResult([]byte(calls[index].OutputJSON))
		if decodeErr != nil {
			return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
		}
		for candidateIndex, reference := range result.Candidates {
			if reference.ArtifactID != revision.ArtifactID || reference.RevisionID != revision.ID {
				continue
			}
			if result.TaskID != candidate.SourceTaskID || matchedCall != nil {
				return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
			}
			matchedCall = &calls[index]
			matchedResult = result
			matchedOrdinal = candidateIndex + 1
		}
	}
	if matchedCall == nil {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	toolInput, err := decodeStoredMediaGenerationInput(matchedCall.InputJSON)
	if err != nil || !candidateRequestIdentityMatches(candidate.ProviderRequestIdentity, toolInput.RequestIdentity, matchedOrdinal) {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}

	operation, err := agentruntime.MediaGenerationOperationForRun(input.Scope.RunID)
	if err != nil {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	var tasks []model.Task
	if err := tx.Where("id = ? AND user_id = ? AND audience = ? AND project_id = ? AND status = ? AND operation = ?",
		candidate.SourceTaskID, input.Scope.ActorUserID, model.TaskAudienceInternal, input.Scope.CanvasID,
		model.TaskStatusSucceeded, operation).Find(&tasks).Error; err != nil || len(tasks) != 1 {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	task := tasks[0]
	if task.BillingOrderID != matchedResult.BillingOrderID || task.Capability != resource.Kind ||
		!taskResultContainsExactResource(task.ResultJSON, candidate.MediaKind, candidate.ResourceID) {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	order, err := loadSettledPublicationBillingOrderTx(tx, input, task, matchedResult.BillingOrderID)
	if err != nil || !storedMediaGenerationInputMatches(toolInput, task, order) {
		return agentAssetPublicationProvenance{}, ErrAgentAssetPublicationBillingMissing
	}
	return agentAssetPublicationProvenance{
		ProducerKind: agentAssetPublicationProducerMediaTool,
		Task:         task, BillingOrder: order,
		MediaTool: &agentAssetPublicationMediaToolProvenance{
			ToolCall: *matchedCall, Candidate: candidate, RequestIdentity: toolInput.RequestIdentity,
			CandidateOrdinal: matchedOrdinal,
		},
	}, nil
}

func loadSettledPublicationBillingOrderTx(
	tx *gorm.DB,
	input PublishAgentAssetInput,
	task model.Task,
	orderID string,
) (model.BillingOrder, error) {
	var order model.BillingOrder
	query := tx.Where("id = ? AND user_id = ? AND task_id = ? AND provider_request_id = ? AND status = ?",
		orderID, input.Scope.ActorUserID, task.ID, task.ProviderRequestID, model.BillingStatusSettled)
	if input.Scope.TenantKind == agentruntime.TenantTeam {
		query = query.Where("team_id = ?", input.Scope.TenantID)
	} else {
		query = query.Where("team_id = ''")
	}
	if err := query.First(&order).Error; err != nil || task.ProviderRequestID == "" ||
		order.Capability != task.Capability || order.Model != task.Model || order.AmountMicrocredits < 0 {
		return order, ErrAgentAssetPublicationBillingMissing
	}
	return order, nil
}

func decodeStoredMediaGenerationInput(raw string) (storedMediaGenerationInput, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	var input storedMediaGenerationInput
	if err := decoder.Decode(&input); err != nil {
		return storedMediaGenerationInput{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) ||
		input.GenerationModel.ChannelID == "" || input.GenerationModel.Model == "" ||
		input.GenerationModelRecordID == "" || input.Capability == "" ||
		len(bytes.TrimSpace(input.Parameters)) == 0 || input.RequestIdentity == "" {
		return storedMediaGenerationInput{}, ErrAgentAssetPublicationBillingMissing
	}
	commercial := input.Commercial
	if commercial.TaskID == "" || commercial.BillingIdempotencyKey == "" || commercial.TaskType == "" ||
		commercial.Operation == "" || commercial.Prompt == "" || commercial.Capability == "" || commercial.ChannelID == "" ||
		commercial.ChannelModelID == "" || commercial.ModelKey == "" || commercial.ParametersJSON == "" ||
		commercial.Quantity <= 0 || commercial.AmountMicrocredits < 0 || commercial.PerTaskMicrocredits < 0 ||
		commercial.PriceVersion <= 0 || commercial.BillingMode == "" || commercial.BillingQuoteFingerprint == "" ||
		commercial.QuoteID == "" || commercial.ApprovalFingerprint == "" {
		return storedMediaGenerationInput{}, ErrAgentAssetPublicationBillingMissing
	}
	return input, nil
}

func storedMediaGenerationInputMatches(
	input storedMediaGenerationInput,
	task model.Task,
	order model.BillingOrder,
) bool {
	commercial := input.Commercial
	return input.Capability == task.Capability && input.GenerationModel.ChannelID == order.ChannelID &&
		input.GenerationModel.Model == task.Model && input.GenerationModelRecordID == order.ChannelModelID &&
		commercial.TaskID == task.ID && commercial.BillingIdempotencyKey == order.IdempotencyKey &&
		commercial.TaskType == task.Type && commercial.Operation == task.Operation && commercial.Prompt == task.Prompt &&
		commercial.Capability == task.Capability && commercial.ChannelID == order.ChannelID &&
		commercial.ChannelModelID == order.ChannelModelID && commercial.ModelKey == task.Model &&
		commercial.ParametersJSON == task.InputJSON && commercial.Quantity == order.Quantity &&
		commercial.AmountMicrocredits == order.AmountMicrocredits && commercial.PerTaskMicrocredits == order.AmountMicrocredits &&
		commercial.PriceVersion == order.PriceVersion && commercial.BillingMode == order.BillingMode &&
		commercial.PricingResolution == order.PricingResolution &&
		commercial.PricingInputVariant == order.PricingInputVariant
}

func taskResultContainsExactResource(raw string, kind agentruntime.ArtifactKind, resourceID string) bool {
	resources, err := agentruntime.DecodeMediaTaskResultResources([]byte(raw))
	if err != nil {
		return false
	}
	count := 0
	for _, resource := range resources {
		if resource.Kind == kind && resource.ResourceID == resourceID {
			count++
		}
	}
	return count == 1
}

func candidateRequestIdentityMatches(candidateIdentity string, requestIdentity string, ordinal int) bool {
	return ordinal > 0 && candidateIdentity == fmt.Sprintf("%s:%02d", requestIdentity, ordinal)
}
