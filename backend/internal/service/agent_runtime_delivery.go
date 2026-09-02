package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func (s *Service) agentRuntimeDeliveryEvidence(scope agentruntime.Scope, finalMessage string) (agentruntime.DeliveryEvidence, error) {
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return agentruntime.DeliveryEvidence{}, err
	}
	if err := validateAgentRuntimeExecutionContract(*run); err != nil {
		return agentruntime.DeliveryEvidence{}, err
	}
	calls, err := s.repo.AgentToolCallsForScope(scope)
	if err != nil {
		return agentruntime.DeliveryEvidence{}, err
	}
	evidence := agentruntime.DeliveryEvidence{FinalMessage: strings.TrimSpace(finalMessage)}
	artifactIndex := make(map[string]int)
	createdCanvasNodeIDs := make(map[string]struct{})
	upsertArtifact := func(artifact agentruntime.DeliveryArtifact) error {
		identity := artifact.ResourceID
		if identity == "" {
			identity = artifact.ArtifactID
		}
		key := string(artifact.Kind) + "\x00" + identity
		if index, found := artifactIndex[key]; found {
			merged, mergeErr := mergeCurrentCapabilityDeliveryArtifact(evidence.Artifacts[index], artifact)
			if mergeErr != nil {
				return mergeErr
			}
			evidence.Artifacts[index] = merged
			return nil
		}
		artifactIndex[key] = len(evidence.Artifacts)
		evidence.Artifacts = append(evidence.Artifacts, artifact)
		return nil
	}

	for _, call := range calls {
		if call.Status != agentruntime.ToolCallSucceeded {
			continue
		}
		switch agentruntime.ToolName(call.ToolName) {
		case agentruntime.ToolCanvasApplyOps:
			result, resultErr := currentCanvasApplyOpsDelivery(scope, call)
			if resultErr != nil {
				return agentruntime.DeliveryEvidence{}, resultErr
			}
			if result.CommittedRevision >= evidence.CanvasRevision {
				evidence.CanvasID = result.CanvasID
				evidence.CanvasRevision = result.CommittedRevision
			}
			for _, nodeID := range result.Evidence.AddedNodeIDs {
				createdCanvasNodeIDs[nodeID] = struct{}{}
			}
		case agentruntime.ToolMediaGenerate:
			artifacts, artifactsErr := s.currentMediaCapabilityDeliveryArtifacts(scope, call)
			if artifactsErr != nil {
				return agentruntime.DeliveryEvidence{}, artifactsErr
			}
			for _, artifact := range artifacts {
				if err := upsertArtifact(artifact); err != nil {
					return agentruntime.DeliveryEvidence{}, err
				}
			}
		case agentruntime.ToolAssetsPublish:
			artifact, artifactErr := s.currentAssetCapabilityDeliveryArtifact(scope, call)
			if artifactErr != nil {
				return agentruntime.DeliveryEvidence{}, artifactErr
			}
			if err := upsertArtifact(artifact); err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
		case agentruntime.ToolCanvasRead, agentruntime.ToolAssetsRead, agentruntime.ToolSkillsLoad, agentruntime.ToolVisionAnalyze:
			continue
		default:
			return agentruntime.DeliveryEvidence{}, errors.New("retired agent tool cannot provide current delivery evidence")
		}
	}
	if evidence.CanvasRevision > 0 {
		project, err := s.repo.CanvasProject(scope.CanvasID)
		if err != nil {
			return agentruntime.DeliveryEvidence{}, err
		}
		if err := reconcileCanvasDeliveryEvidence(&evidence, *project, scope); err != nil {
			return agentruntime.DeliveryEvidence{}, err
		}
		textArtifacts, err := canvasPayloadCreatedTextDeliveryArtifacts(project.PayloadJSON, createdCanvasNodeIDs, project.Revision)
		if err != nil {
			return agentruntime.DeliveryEvidence{}, err
		}
		for _, artifact := range textArtifacts {
			if err := upsertArtifact(artifact); err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
		}
	}
	return evidence, nil
}

func reconcileCanvasDeliveryEvidence(evidence *agentruntime.DeliveryEvidence, project model.CanvasProject, scope agentruntime.Scope) error {
	if evidence.CanvasID != scope.CanvasID || !agentCanvasProjectBelongsToScope(project, scope) {
		return errors.New("agent canvas current revision scope is invalid")
	}
	if project.Revision < evidence.CanvasRevision {
		return errors.New("agent canvas current revision is older than the committed delivery receipt")
	}

	evidence.CanvasRevision = project.Revision
	evidence.CanvasCurrent = true
	for index := range evidence.Artifacts {
		evidence.Artifacts[index].CanvasBound = canvasPayloadBindsGeneratedResource(project.PayloadJSON, evidence.Artifacts[index])
	}
	return nil
}

func currentCanvasApplyOpsDelivery(scope agentruntime.Scope, call model.AgentToolCall) (agentruntime.CanvasApplyOpsResult, error) {
	decodedArguments, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolCanvasApplyOps, json.RawMessage(call.InputJSON))
	if err != nil {
		return agentruntime.CanvasApplyOpsResult{}, errors.New("agent canvas delivery input is invalid")
	}
	arguments, ok := decodedArguments.(agentruntime.CanvasApplyOpsArguments)
	if !ok || arguments.CanvasID != scope.CanvasID {
		return agentruntime.CanvasApplyOpsResult{}, errors.New("agent canvas delivery scope is invalid")
	}
	decodedResult, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolCanvasApplyOps, json.RawMessage(call.OutputJSON))
	if err != nil {
		return agentruntime.CanvasApplyOpsResult{}, errors.New("agent canvas delivery evidence is invalid")
	}
	result, ok := decodedResult.(agentruntime.CanvasApplyOpsResult)
	if !ok || result.CanvasID != arguments.CanvasID || result.BaseRevision != arguments.BaseRevision ||
		result.ClientMutationID != arguments.ClientMutationID || result.ProposalHash != call.ApprovalProposalHash ||
		len(result.AppliedOperationIDs) != len(arguments.Operations) {
		return agentruntime.CanvasApplyOpsResult{}, errors.New("agent canvas delivery receipt conflicts with its approved request")
	}
	for index, operation := range arguments.Operations {
		if result.AppliedOperationIDs[index] != operation.OperationID {
			return agentruntime.CanvasApplyOpsResult{}, errors.New("agent canvas delivery operation receipt is invalid")
		}
	}
	return result, nil
}

func agentCanvasProjectBelongsToScope(project model.CanvasProject, scope agentruntime.Scope) bool {
	if project.ID != scope.CanvasID || project.ProjectID != scope.DomainProjectID {
		return false
	}
	if scope.TenantKind == agentruntime.TenantTeam {
		return project.TeamID == scope.TenantID
	}
	return project.UserID == scope.ActorUserID && project.TeamID == ""
}

func mergeCurrentCapabilityDeliveryArtifact(current, incoming agentruntime.DeliveryArtifact) (agentruntime.DeliveryArtifact, error) {
	if current.Kind != incoming.Kind || current.ResourceID == "" || current.ResourceID != incoming.ResourceID ||
		current.URL == "" || current.URL != incoming.URL || !current.ResourceReady || !incoming.ResourceReady ||
		(current.PublicationID != "" && incoming.PublicationID != "" && current.PublicationID != incoming.PublicationID) ||
		(current.SourceTaskID != "" && incoming.SourceTaskID != "" && current.SourceTaskID != incoming.SourceTaskID) {
		return agentruntime.DeliveryArtifact{}, errors.New("agent capability delivery facts conflict")
	}
	if current.PublicationID == "" {
		current.PublicationID = incoming.PublicationID
	}
	if current.SourceTaskID == "" {
		current.SourceTaskID = incoming.SourceTaskID
	}
	if current.TargetCanvasNodeID != "" && incoming.TargetCanvasNodeID != "" && current.TargetCanvasNodeID != incoming.TargetCanvasNodeID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent capability delivery canvas targets conflict")
	}
	if current.TargetCanvasNodeID == "" {
		current.TargetCanvasNodeID = incoming.TargetCanvasNodeID
	}
	current.Approved = current.Approved || incoming.Approved
	current.SourceTaskSucceeded = current.SourceTaskSucceeded || incoming.SourceTaskSucceeded
	current.CanvasBound = current.CanvasBound || incoming.CanvasBound
	return current, nil
}

func (s *Service) currentMediaCapabilityDeliveryArtifacts(scope agentruntime.Scope, call model.AgentToolCall) ([]agentruntime.DeliveryArtifact, error) {
	if call.ReplaySourceRunID != "" {
		if call.ReplaySourceToolCallID == "" || call.ReplaySourceActionVersion < 1 || call.ReplaySourceRunID == scope.RunID {
			return nil, errors.New("agent media replay source identity is invalid")
		}
		sourceScope := scope
		sourceScope.RunID = call.ReplaySourceRunID
		source, err := s.repo.AgentToolCallForScope(sourceScope, call.ReplaySourceToolCallID, call.ReplaySourceActionVersion)
		if err != nil || source.Status != agentruntime.ToolCallSucceeded || source.ReplaySourceRunID != "" ||
			!equalAgentToolArguments(source.InputJSON, json.RawMessage(call.InputJSON)) || source.OutputJSON != call.OutputJSON {
			return nil, errors.Join(errors.New("agent media replay source receipt is invalid"), err)
		}
		return s.currentMediaCapabilityDeliveryArtifacts(sourceScope, *source)
	}
	decodedArguments, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolMediaGenerate, json.RawMessage(call.InputJSON))
	if err != nil {
		return nil, errors.New("agent media delivery input is invalid")
	}
	arguments, ok := decodedArguments.(agentruntime.MediaGenerateArguments)
	if !ok {
		return nil, errors.New("agent media delivery input type is invalid")
	}
	decodedResult, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolMediaGenerate, json.RawMessage(call.OutputJSON))
	if err != nil {
		return nil, errors.New("agent media delivery evidence is invalid")
	}
	result, ok := decodedResult.(agentruntime.MediaGenerateResult)
	if !ok || result.MediaKind != arguments.MediaKind || result.ClientRequestID != arguments.ClientRequestID {
		return nil, errors.New("agent media delivery receipt conflicts with its approved request")
	}
	expectedTaskID := MediaAttemptIdentity(scope, MediaGenerationCommand{
		ArtifactRevisionID: agentMediaCapabilityIdentity(scope, arguments),
		Attempt:            1,
		TaskType:           "canvas_" + string(result.MediaKind),
		Capability:         string(result.MediaKind),
	})
	if result.TaskID != expectedTaskID {
		return nil, errors.New("agent media delivery task identity conflicts with its approved request")
	}
	task, err := s.repo.TaskForUser(scope.ActorUserID, result.TaskID)
	if err != nil || task.ProjectID != scope.CanvasID || task.Type != "canvas_"+string(result.MediaKind) ||
		task.Capability != string(result.MediaKind) || task.Operation != agentMediaGenerationOperationForRun(scope.RunID) ||
		task.Status != model.TaskStatusSucceeded || task.BillingOrderID != result.BillingOrderID {
		return nil, errors.Join(errors.New("agent media task delivery evidence is invalid"), err)
	}
	order, err := s.repo.BillingOrder(result.BillingOrderID)
	if err != nil || order.UserID != scope.ActorUserID || order.TeamID != taskTeamID(scope) ||
		order.TaskID != task.ID || order.Capability != string(result.MediaKind) || order.Status != model.BillingStatusSettled {
		return nil, errors.Join(errors.New("agent media billing delivery evidence is invalid"), err)
	}
	authoritativeResources, err := s.agentMediaCapabilityResources(scope, task, result.MediaKind)
	if err != nil || len(authoritativeResources) != len(result.Resources) {
		return nil, errors.Join(errors.New("agent media task result delivery evidence is invalid"), err)
	}
	artifacts := make([]agentruntime.DeliveryArtifact, 0, len(authoritativeResources))
	for index, resource := range authoritativeResources {
		if result.Resources[index] != resource {
			return nil, errors.New("agent media receipt resource conflicts with the authoritative task result")
		}
		artifacts = append(artifacts, agentruntime.DeliveryArtifact{
			Kind: agentruntime.ArtifactKind(result.MediaKind), ResourceID: resource.ResourceID, URL: resource.URL,
			ResourceReady: true, SourceTaskID: task.ID, SourceTaskSucceeded: true,
			TargetCanvasNodeID: arguments.TargetCanvasNodeID,
		})
	}
	return artifacts, nil
}

func canvasPayloadBindsGeneratedResource(payload string, artifact agentruntime.DeliveryArtifact) bool {
	if artifact.TargetCanvasNodeID == "" || artifact.ResourceID == "" || artifact.URL == "" {
		return false
	}
	var document struct {
		Nodes []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Metadata struct {
				Content    string `json:"content"`
				StorageKey string `json:"storageKey"`
				Status     string `json:"status"`
			} `json:"metadata"`
		} `json:"nodes"`
	}
	if json.Unmarshal([]byte(payload), &document) != nil || document.Nodes == nil {
		return false
	}
	matched := false
	for _, node := range document.Nodes {
		if node.ID != artifact.TargetCanvasNodeID {
			continue
		}
		if matched || node.Type != string(artifact.Kind) || node.Metadata.Content != artifact.URL ||
			node.Metadata.StorageKey != "resource:"+artifact.ResourceID || node.Metadata.Status != "success" {
			return false
		}
		matched = true
	}
	return matched
}

func canvasPayloadCreatedTextDeliveryArtifacts(payload string, createdNodeIDs map[string]struct{}, canvasRevision int64) ([]agentruntime.DeliveryArtifact, error) {
	if len(createdNodeIDs) == 0 {
		return nil, nil
	}
	if canvasRevision < 1 {
		return nil, errors.New("agent canvas text delivery revision is invalid")
	}
	var document struct {
		Nodes []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Metadata struct {
				Content string `json:"content"`
				Status  string `json:"status"`
			} `json:"metadata"`
		} `json:"nodes"`
	}
	if json.Unmarshal([]byte(payload), &document) != nil || document.Nodes == nil {
		return nil, errors.New("agent canvas text delivery facts are invalid")
	}
	matchedNodeIDs := make(map[string]struct{}, len(createdNodeIDs))
	artifacts := make([]agentruntime.DeliveryArtifact, 0, len(createdNodeIDs))
	for _, node := range document.Nodes {
		if _, candidate := createdNodeIDs[node.ID]; !candidate {
			continue
		}
		if _, duplicate := matchedNodeIDs[node.ID]; duplicate {
			return nil, errors.New("agent canvas text delivery node is duplicated")
		}
		matchedNodeIDs[node.ID] = struct{}{}
		if node.Type != string(agentruntime.CanvasNodeText) && node.Type != string(agentruntime.CanvasNodeScript) {
			continue
		}
		if strings.TrimSpace(node.Metadata.Content) == "" || (node.Metadata.Status != "" && node.Metadata.Status != string(agentruntime.CanvasNodeIdle) && node.Metadata.Status != string(agentruntime.CanvasNodeSuccess)) {
			continue
		}
		artifacts = append(artifacts, agentruntime.DeliveryArtifact{
			Kind: agentruntime.ArtifactText, ArtifactID: node.ID, RevisionID: fmt.Sprintf("canvas-revision-%d", canvasRevision),
			Approved: true, CurrentRevision: true, TargetCanvasNodeID: node.ID,
		})
	}
	return artifacts, nil
}

func (s *Service) currentAssetCapabilityDeliveryArtifact(scope agentruntime.Scope, call model.AgentToolCall) (agentruntime.DeliveryArtifact, error) {
	decodedArguments, err := agentruntime.DecodeCapabilityArguments(agentruntime.ToolAssetsPublish, json.RawMessage(call.InputJSON))
	if err != nil {
		return agentruntime.DeliveryArtifact{}, errors.New("agent asset publication delivery input is invalid")
	}
	arguments, ok := decodedArguments.(agentruntime.AssetsPublishArguments)
	if !ok || arguments.DomainProjectID != scope.DomainProjectID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent asset publication delivery scope is invalid")
	}
	decodedResult, err := agentruntime.DecodeCapabilityResult(agentruntime.ToolAssetsPublish, json.RawMessage(call.OutputJSON))
	if err != nil {
		return agentruntime.DeliveryArtifact{}, errors.New("agent asset publication delivery receipt is invalid")
	}
	result, ok := decodedResult.(agentruntime.AssetsPublishResult)
	if !ok || result.DomainProjectID != arguments.DomainProjectID || result.ResourceID != arguments.ResourceID ||
		result.ClientMutationID != arguments.ClientMutationID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent asset publication receipt conflicts with its approved request")
	}
	fact, err := s.repo.AgentCapabilityAssetPublicationForScope(scope, result.AssetID, result.ResourceID)
	if err != nil {
		return agentruntime.DeliveryArtifact{}, errors.Join(errors.New("agent asset publication persisted evidence is invalid"), err)
	}
	if fact.Asset.Title != arguments.DisplayName {
		return agentruntime.DeliveryArtifact{}, errors.New("agent asset publication display name conflicts with persisted evidence")
	}
	return agentruntime.DeliveryArtifact{
		Kind: agentruntime.ArtifactKind(fact.Resource.Kind), ResourceID: fact.Resource.ID,
		URL: "/api/resources/" + fact.Resource.ID + "/file", ResourceReady: true,
		PublicationID: fact.Asset.ID, Approved: true,
	}, nil
}

func taskTeamID(scope agentruntime.Scope) string {
	if scope.TenantKind == agentruntime.TenantTeam {
		return scope.TenantID
	}
	return ""
}
