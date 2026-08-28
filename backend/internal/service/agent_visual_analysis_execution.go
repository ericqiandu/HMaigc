package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func (s *Service) processAgentVisualAnalysisTask(ctx context.Context, task model.Task) (map[string]interface{}, error) {
	input, err := decodeAgentVisualAnalysisTaskInput(task.InputJSON)
	if err != nil {
		return nil, err
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return nil, err
	}
	if err := validateAgentVisualAnalysisExecutionTask(input.Scope, task, *order, input); err != nil {
		return nil, err
	}
	if existing, found, err := s.existingAgentVisualEvidence(input.Scope, input.Analysis); err != nil {
		return nil, err
	} else if found {
		return agentVisualAnalysisResultMap(*existing, ""), nil
	}
	resource, err := s.currentAgentVisualAnalysisSource(input.Scope, input.Analysis)
	if err != nil {
		return nil, err
	}
	setting, err := s.ossSettingForResource(resource.UserID, resource)
	if err != nil {
		return nil, err
	}
	signedURL, err := signedOSSObjectURL(setting, resource.ObjectKey, time.Now().UTC().Add(providerResourceURLTTL))
	if err != nil {
		return nil, err
	}
	runtime, err := s.repo.FrozenProviderRuntime(task)
	if err != nil {
		return nil, errors.New("读取视觉分析冻结运行配置失败")
	}
	apiKey, err := NewProviderSecretCipher(s.dataDir).Decrypt(
		runtime.ProviderAccountID, runtime.ProviderCredentialID, runtime.CredentialVersion, runtime.KeyCipher,
	)
	if err != nil {
		return nil, errors.New("解密视觉分析冻结账号 Key 失败")
	}
	prompt, err := visualAnalysisProviderPrompt(input.Analysis)
	if err != nil {
		return nil, err
	}
	result, err := runKuaiziChatCompletion(ctx, canvasGenerationInput{
		Mode: "vision", Prompt: prompt,
		Config: providerConfig{
			ChannelID: input.Analysis.VisionModel.ChannelID, Model: input.Analysis.VisionModel.Model,
			BaseURL: kuaiziChatCompletionsBaseURL(runtime.BaseURL), APIKey: apiKey,
			InterfaceType: string(model.ChannelInterfaceChatCompletion), SystemPrompt: agentVisualAnalysisSystemPrompt, JSONOutput: true,
		},
		ReferenceImages: []providerMedia{{
			ID: resource.ID, Name: resource.ID, Type: "image", URL: signedURL,
			StorageKey: "resource:" + resource.ID, MimeType: resource.MimeType,
		}},
	})
	if err != nil {
		return nil, err
	}
	evidence, err := agentruntime.DecodeVisualEvidence([]byte(result.Text))
	if err != nil {
		return nil, fmt.Errorf("visual_evidence.v1 output invalid: %w", err)
	}
	if evidence.SourceRevision != input.Analysis.SourceRevision || evidence.VisionModelRecordID != input.Analysis.VisionModelRecordID ||
		evidence.RequestIdentity != input.Analysis.RequestIdentity {
		return nil, errors.New("visual_evidence.v1 audit facts conflict")
	}
	encodedEvidence, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	revision, err := s.appendAgentVisualEvidence(input.Scope, input.Analysis, encodedEvidence)
	if err != nil {
		return nil, err
	}
	return agentVisualAnalysisResultMap(*revision, result.ProviderRequestID), nil
}

func validateAgentVisualAnalysisExecutionTask(
	scope agentruntime.Scope,
	task model.Task,
	order model.BillingOrder,
	input agentVisualAnalysisTaskInput,
) error {
	if input.Mode != "vision" || input.Prompt != agentVisualAnalysisPrompt || input.Scope != scope ||
		validateAgentVisualAnalysisExecution(input.Analysis) != nil ||
		input.Config.ChannelID != input.Analysis.VisionModel.ChannelID || input.Config.Model != input.Analysis.VisionModel.Model ||
		task.UserID != scope.ActorUserID || task.ProjectID != scope.CanvasID || task.Audience != model.TaskAudienceInternal ||
		task.Type != agentVisualAnalysisTaskType || task.Operation != agentVisualAnalysisOperationForRun(scope.RunID) ||
		task.Prompt != agentVisualAnalysisPrompt || task.Model != input.Analysis.VisionModel.Model || task.Capability != "vision" ||
		task.BillingOrderID == "" || task.ProviderAccountID == "" || task.ProviderEndpointVersionID == "" ||
		task.ProviderCredentialVersionID == "" || order.UserID != scope.ActorUserID || order.TaskID != task.ID ||
		order.ChannelID != input.Analysis.VisionModel.ChannelID || order.ChannelModelID != input.Analysis.VisionModelRecordID ||
		order.Model != input.Analysis.VisionModel.Model || order.Capability != "vision" ||
		order.Scene != agentVisualAnalysisOperationForRun(scope.RunID) || order.Quantity != 1 {
		return errors.New("agent visual analysis execution facts conflict")
	}
	return nil
}

func visualAnalysisProviderPrompt(arguments agentVisualAnalysisExecutionFacts) (string, error) {
	facts := struct {
		SourceRevision      agentruntime.ArtifactRevisionRef `json:"sourceRevision"`
		VisionModelRecordID string                           `json:"visionModelRecordId"`
		RequestIdentity     string                           `json:"requestIdentity"`
	}{
		SourceRevision: arguments.SourceRevision, VisionModelRecordID: arguments.VisionModelRecordID,
		RequestIdentity: arguments.RequestIdentity,
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	return "分析图片并返回 visual_evidence.v1。以下字段必须逐字复用：" + string(encoded), nil
}

func (s *Service) currentAgentVisualAnalysisSource(
	scope agentruntime.Scope,
	arguments agentVisualAnalysisExecutionFacts,
) (*model.Resource, error) {
	source, err := s.repo.ArtifactRevisionForArtifactInScope(
		scope, arguments.SourceRevision.ArtifactID, arguments.SourceRevision.RevisionID,
	)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errAgentVisualInputUnavailable
		}
		return nil, err
	}
	head, err := s.repo.ArtifactHeadRevisionForScope(scope, arguments.SourceRevision.ArtifactID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errAgentVisualInputUnavailable
		}
		return nil, err
	}
	if source.LifecycleStatus == model.AgentArtifactRevisionStale || head.ID != source.ID {
		return nil, errAgentVisualSourceRevisionStale
	}
	if source.ResourceID == "" || source.ResourceID != arguments.ResourceID {
		return nil, errAgentVisualInputUnavailable
	}
	resource, err := s.productionResourceForScope(scope, arguments.ResourceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errAgentVisualInputUnavailable
		}
		return nil, err
	}
	if !visualAnalysisResourceReady(resource) {
		return nil, errAgentVisualInputUnavailable
	}
	return resource, nil
}

func (s *Service) existingAgentVisualEvidence(
	scope agentruntime.Scope,
	arguments agentVisualAnalysisExecutionFacts,
) (*model.AgentArtifactRevision, bool, error) {
	revision, err := s.repo.ArtifactHeadRevisionForScope(scope, arguments.OutputArtifactID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := validateStoredAgentVisualEvidence(*revision, arguments); err != nil {
		return nil, false, err
	}
	return revision, true, nil
}

func (s *Service) appendAgentVisualEvidence(
	scope agentruntime.Scope,
	arguments agentVisualAnalysisExecutionFacts,
	payload []byte,
) (*model.AgentArtifactRevision, error) {
	draft := agentruntime.ArtifactDraft{
		ArtifactKey: arguments.OutputArtifactKey, Kind: "visual_evidence", SchemaVersion: 1,
		Payload: payload, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{arguments.SourceRevision},
		ModelRequestIdentity: arguments.RequestIdentity, SkillVersions: []agentruntime.SkillSelection{},
	}
	revision, err := s.repo.AppendArtifactRevision(scope, arguments.OutputArtifactID, 0, draft)
	if err == nil {
		return revision, nil
	}
	if errors.Is(err, repository.ErrArtifactUpstreamRevisionStale) {
		return nil, errAgentVisualSourceRevisionStale
	}
	if !errors.Is(err, repository.ErrArtifactRevisionConflict) {
		return nil, err
	}
	existing, found, loadErr := s.existingAgentVisualEvidence(scope, arguments)
	if loadErr != nil {
		return nil, errors.Join(err, loadErr)
	}
	if !found {
		return nil, err
	}
	return existing, nil
}

func validateStoredAgentVisualEvidence(revision model.AgentArtifactRevision, arguments agentVisualAnalysisExecutionFacts) error {
	if revision.Revision != 1 || revision.ArtifactID != arguments.OutputArtifactID || revision.ArtifactKey != arguments.OutputArtifactKey ||
		revision.Kind != "visual_evidence" || revision.SchemaVersion != 1 || revision.ModelRequestIdentity != arguments.RequestIdentity ||
		revision.LifecycleStatus == model.AgentArtifactRevisionStale {
		return errors.New("stored agent visual evidence facts conflict")
	}
	evidence, err := agentruntime.DecodeVisualEvidence([]byte(revision.PayloadJSON))
	if err != nil {
		return err
	}
	if evidence.SourceRevision != arguments.SourceRevision || evidence.VisionModelRecordID != arguments.VisionModelRecordID ||
		evidence.RequestIdentity != arguments.RequestIdentity {
		return errors.New("stored agent visual evidence audit facts conflict")
	}
	var upstream []agentruntime.ArtifactRevisionRef
	if err := json.Unmarshal([]byte(revision.UpstreamRevisionsJSON), &upstream); err != nil ||
		len(upstream) != 1 || upstream[0] != arguments.SourceRevision {
		return errors.New("stored agent visual evidence upstream facts conflict")
	}
	return nil
}

func agentVisualAnalysisResultMap(revision model.AgentArtifactRevision, providerRequestID string) map[string]interface{} {
	result := map[string]interface{}{
		"artifactId": revision.ArtifactID, "artifactRevisionId": revision.ID,
		"revision": revision.Revision, "schema": agentruntime.ArtifactSchemaVisualEvidenceV1,
	}
	if requestID := strings.TrimSpace(providerRequestID); requestID != "" {
		result["providerRequestId"] = requestID
	}
	return result
}
