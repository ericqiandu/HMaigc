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
	calls, err := s.repo.AgentToolCallsForScope(scope)
	if err != nil {
		return agentruntime.DeliveryEvidence{}, err
	}
	evidence := agentruntime.DeliveryEvidence{FinalMessage: strings.TrimSpace(finalMessage)}
	seenArtifacts := make(map[string]bool)
	currentArtifactIndex := make(map[string]int)
	upsertCurrentArtifact := func(artifact agentruntime.DeliveryArtifact) error {
		key := string(artifact.Kind) + "\x00" + artifact.ResourceID
		if index, found := currentArtifactIndex[key]; found {
			merged, mergeErr := mergeCurrentCapabilityDeliveryArtifact(evidence.Artifacts[index], artifact)
			if mergeErr != nil {
				return mergeErr
			}
			evidence.Artifacts[index] = merged
			return nil
		}
		currentArtifactIndex[key] = len(evidence.Artifacts)
		evidence.Artifacts = append(evidence.Artifacts, artifact)
		seenArtifacts[key] = true
		return nil
	}
	approvedRevisionIDs := map[string]struct{}{}
	publicationByRevisionID := map[string]string{}
	mediaFactsLoaded := false
	loadMediaFacts := func() error {
		if mediaFactsLoaded {
			return nil
		}
		approved, err := s.repo.ApprovedArtifactRevisionIDsForScope(scope)
		if err != nil {
			return err
		}
		publications, err := s.repo.SucceededAgentAssetPublicationsForScope(scope)
		if err != nil {
			return err
		}
		publicationByRevisionID, err = successfulPublicationIDsByRevision(publications)
		if err != nil {
			return err
		}
		approvedRevisionIDs = approved
		mediaFactsLoaded = true
		return nil
	}
	var latestCanvasCommit model.AgentToolCall
	hasCanvasCommit := false
	for _, call := range calls {
		if call.Status != agentruntime.ToolCallSucceeded {
			continue
		}
		switch agentruntime.ToolName(call.ToolName) {
		case agentruntime.ToolCanvasCommit:
			var result agentProductionCanvasCommitResult
			if err := json.Unmarshal([]byte(call.OutputJSON), &result); err != nil || result.CanvasID != scope.CanvasID || result.CommittedRevision < 1 || strings.TrimSpace(result.ClientMutationID) == "" {
				return agentruntime.DeliveryEvidence{}, errors.New("agent canvas delivery evidence is invalid")
			}
			if result.CommittedRevision >= evidence.CanvasRevision {
				evidence.CanvasID = result.CanvasID
				evidence.CanvasRevision = result.CommittedRevision
				latestCanvasCommit = call
				hasCanvasCommit = true
			}
		case agentruntime.ToolCanvasProject:
			result, err := validateAgentCanvasProjectionDelivery(scope, call)
			if err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
			if result.CommittedRevision >= evidence.CanvasRevision {
				evidence.CanvasID = result.CanvasID
				evidence.CanvasRevision = result.CommittedRevision
			}
		case agentruntime.ToolProductionRender:
			var result agentProductionRenderResult
			if err := json.Unmarshal([]byte(call.OutputJSON), &result); err != nil || result.ArtifactID == "" || result.ArtifactStatus != model.AgentProductionArtifactSucceeded || result.ResourceID == "" {
				return agentruntime.DeliveryEvidence{}, errors.New("agent generation delivery evidence is invalid")
			}
			resource, err := s.productionResourceForScope(scope, result.ResourceID)
			if err != nil || resource.Status != model.ResourceStatusReady {
				return agentruntime.DeliveryEvidence{}, errors.New("agent generation resource evidence is invalid")
			}
			kind := agentruntime.ArtifactKind("")
			switch result.ArtifactKind {
			case model.AgentProductionArtifactReferenceImage, model.AgentProductionArtifactStoryboardImage:
				kind = agentruntime.ArtifactImage
			case model.AgentProductionArtifactVideoClip:
				kind = agentruntime.ArtifactVideo
			default:
				return agentruntime.DeliveryEvidence{}, errors.New("agent generation artifact kind is invalid")
			}
			artifact := agentruntime.DeliveryArtifact{Kind: kind, URL: "/api/resources/" + resource.ID + "/file"}
			key := string(artifact.Kind) + "\x00" + artifact.URL
			if !seenArtifacts[key] {
				evidence.Artifacts = append(evidence.Artifacts, artifact)
				seenArtifacts[key] = true
			}
		case agentruntime.ToolMediaGenerate:
			if run.ToolSchemaVersion == agentruntime.CurrentToolSchemaVersion {
				artifacts, err := s.currentMediaCapabilityDeliveryArtifacts(scope, call)
				if err != nil {
					return agentruntime.DeliveryEvidence{}, err
				}
				for _, artifact := range artifacts {
					if err := upsertCurrentArtifact(artifact); err != nil {
						return agentruntime.DeliveryEvidence{}, err
					}
				}
				continue
			}
			arguments, err := decodeFrozenAgentMediaGenerationArguments(json.RawMessage(call.InputJSON))
			if err != nil {
				return agentruntime.DeliveryEvidence{}, errors.New("agent media delivery input is invalid")
			}
			result, err := agentruntime.DecodeMediaGenerationToolResult([]byte(call.OutputJSON))
			if err != nil {
				return agentruntime.DeliveryEvidence{}, errors.New("agent media delivery evidence is invalid")
			}
			frozenAudioMode, err := agentMediaAudioModeForArguments(arguments)
			if err != nil || result.AudioMode != frozenAudioMode {
				return agentruntime.DeliveryEvidence{}, errors.New("agent media audio delivery evidence is invalid")
			}
			if err := loadMediaFacts(); err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
			candidateSet := make(map[agentruntime.ArtifactRevisionRef]struct{}, len(result.Candidates))
			for _, reference := range result.Candidates {
				if reference.Validate() != nil {
					return agentruntime.DeliveryEvidence{}, errors.New("agent media candidate reference is invalid")
				}
				if _, duplicate := candidateSet[reference]; duplicate {
					return agentruntime.DeliveryEvidence{}, errors.New("agent media candidate reference is duplicated")
				}
				candidateSet[reference] = struct{}{}
				revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
				if err != nil {
					return agentruntime.DeliveryEvidence{}, err
				}
				resource, err := s.productionResourceForScope(scope, revision.ResourceID)
				if err != nil {
					return agentruntime.DeliveryEvidence{}, err
				}
				_, approved := approvedRevisionIDs[revision.ID]
				artifact, err := mediaCandidateDeliveryArtifact(*revision, *resource, approved, publicationByRevisionID[revision.ID])
				if err != nil {
					return agentruntime.DeliveryEvidence{}, err
				}
				key := artifact.ArtifactID + "\x00" + artifact.RevisionID
				if !seenArtifacts[key] {
					evidence.Artifacts = append(evidence.Artifacts, artifact)
					seenArtifacts[key] = true
				}
			}
		case agentruntime.ToolAssetsPublish:
			if run.ToolSchemaVersion != agentruntime.CurrentToolSchemaVersion {
				continue
			}
			artifact, err := s.currentAssetCapabilityDeliveryArtifact(scope, call)
			if err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
			if err := upsertCurrentArtifact(artifact); err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
		case agentruntime.ToolMediaAssemble:
			artifact, err := s.mediaAssemblyDeliveryArtifact(scope, call)
			if err != nil {
				return agentruntime.DeliveryEvidence{}, err
			}
			key := artifact.ArtifactID + "\x00" + artifact.RevisionID
			if !seenArtifacts[key] {
				evidence.Artifacts = append(evidence.Artifacts, artifact)
				seenArtifacts[key] = true
			}
		}
	}
	if hasCanvasCommit {
		arguments, err := decodeAgentProductionCanvasCommitArguments(json.RawMessage(latestCanvasCommit.InputJSON))
		if err != nil {
			return agentruntime.DeliveryEvidence{}, errors.New("agent committed plan delivery input is invalid")
		}
		_, artifacts, resources, _, err := s.productionCanvasCommitFacts(scope, arguments)
		if err != nil {
			return agentruntime.DeliveryEvidence{}, err
		}
		committedArtifacts, err := committedPlanDeliveryArtifacts(scope.CanvasID, artifacts, resources)
		if err != nil {
			return agentruntime.DeliveryEvidence{}, err
		}
		for _, artifact := range committedArtifacts {
			key := string(artifact.Kind) + "\x00" + artifact.URL
			if !seenArtifacts[key] {
				evidence.Artifacts = append(evidence.Artifacts, artifact)
				seenArtifacts[key] = true
			}
		}
	}
	if evidence.CanvasRevision > 0 {
		project, err := s.repo.CanvasProject(scope.CanvasID)
		if err != nil {
			return agentruntime.DeliveryEvidence{}, err
		}
		owned := project.ID == scope.CanvasID && project.ProjectID == scope.DomainProjectID
		if scope.TenantKind == agentruntime.TenantTeam {
			owned = owned && project.TeamID == scope.TenantID
		} else {
			owned = owned && project.UserID == scope.ActorUserID && project.TeamID == ""
		}
		if !owned {
			return agentruntime.DeliveryEvidence{}, errors.New("agent canvas current revision scope is invalid")
		}
		evidence.CanvasCurrent = project.Revision == evidence.CanvasRevision
	}
	return evidence, nil
}

func mergeCurrentCapabilityDeliveryArtifact(
	current agentruntime.DeliveryArtifact,
	incoming agentruntime.DeliveryArtifact,
) (agentruntime.DeliveryArtifact, error) {
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
	current.Approved = current.Approved || incoming.Approved
	current.SourceTaskSucceeded = current.SourceTaskSucceeded || incoming.SourceTaskSucceeded
	return current, nil
}

func (s *Service) currentMediaCapabilityDeliveryArtifacts(scope agentruntime.Scope, call model.AgentToolCall) ([]agentruntime.DeliveryArtifact, error) {
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
		receipt := result.Resources[index]
		if receipt != resource {
			return nil, errors.New("agent media receipt resource conflicts with the authoritative task result")
		}
		artifacts = append(artifacts, agentruntime.DeliveryArtifact{
			Kind:                agentruntime.ArtifactKind(result.MediaKind),
			ResourceID:          resource.ResourceID,
			URL:                 resource.URL,
			ResourceReady:       true,
			SourceTaskID:        task.ID,
			SourceTaskSucceeded: true,
		})
	}
	return artifacts, nil
}

func (s *Service) currentAssetCapabilityDeliveryArtifact(
	scope agentruntime.Scope,
	call model.AgentToolCall,
) (agentruntime.DeliveryArtifact, error) {
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
		Kind:          agentruntime.ArtifactKind(fact.Resource.Kind),
		ResourceID:    fact.Resource.ID,
		URL:           "/api/resources/" + fact.Resource.ID + "/file",
		ResourceReady: true,
		PublicationID: fact.Asset.ID,
		Approved:      true,
	}, nil
}

func taskTeamID(scope agentruntime.Scope) string {
	if scope.TenantKind == agentruntime.TenantTeam {
		return scope.TenantID
	}
	return ""
}

func validateAgentCanvasProjectionDelivery(
	scope agentruntime.Scope,
	call model.AgentToolCall,
) (agentCanvasProjectionResult, error) {
	arguments, err := decodeCanvasProjectArguments(json.RawMessage(call.InputJSON))
	if err != nil || (arguments.ExpectedDelivery.Kind != agentruntime.DeliveryCanvasChange && arguments.ExpectedDelivery.Kind != agentruntime.DeliveryMixed) ||
		arguments.ExpectedDelivery.TargetCanvasID != scope.CanvasID {
		return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery input is invalid")
	}
	result, err := decodeAgentCanvasProjectionResult([]byte(call.OutputJSON))
	if err != nil || result.CanvasID != scope.CanvasID || result.BaseRevision != arguments.BaseRevision ||
		result.ClientMutationID != agentCanvasMutationID(call.IdempotencyKey) ||
		len(result.Bindings) != len(arguments.ArtifactRevisions) {
		return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery evidence is invalid")
	}
	revisions := make(map[string]string, len(arguments.ArtifactRevisions))
	for _, reference := range arguments.ArtifactRevisions {
		if reference.Validate() != nil {
			return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery revision is invalid")
		}
		if _, duplicate := revisions[reference.ArtifactID]; duplicate {
			return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery revision is duplicated")
		}
		revisions[reference.ArtifactID] = reference.RevisionID
	}
	for _, binding := range result.Bindings {
		if revisions[binding.ArtifactID] != binding.RevisionID ||
			binding.NodeID != agentCanvasProjectionNodeID(scope.CanvasID, binding.ArtifactID) ||
			binding.ProjectionID != productionCanvasStableID("artifact-projection", scope.CanvasID, binding.ArtifactID) {
			return agentCanvasProjectionResult{}, errors.New("agent canvas projection delivery binding is invalid")
		}
	}
	return result, nil
}

func (s *Service) mediaAssemblyDeliveryArtifact(
	scope agentruntime.Scope,
	call model.AgentToolCall,
) (agentruntime.DeliveryArtifact, error) {
	arguments, err := decodeMediaAssembleArguments([]byte(call.InputJSON))
	if err != nil || arguments.ExpectedDelivery.TargetCanvasID != scope.CanvasID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly delivery input is invalid")
	}
	content, err := agentruntime.DecodeMediaAssemblyTimelineContent([]byte(call.OutputJSON))
	if err != nil || content.ToolCallID != call.ToolCallID || content.ActionVersion != call.ActionVersion ||
		content.TaskStatus != agentruntime.MediaAssemblyTaskSucceeded || content.Final == nil || !content.Final.Adopted ||
		content.PlanRevision != arguments.PlanRevision {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly delivery output is invalid")
	}
	task, err := s.repo.Task(content.TaskID)
	if err != nil {
		return agentruntime.DeliveryArtifact{}, err
	}
	operation, err := agentruntime.MediaAssemblyOperationForRun(scope.RunID)
	if err != nil {
		return agentruntime.DeliveryArtifact{}, err
	}
	if task.ID != content.TaskID || task.UserID != scope.ActorUserID || task.ProjectID != scope.CanvasID ||
		task.Audience != model.TaskAudienceInternal || task.ExecutionKind != model.TaskExecutionLocalMediaAssembly ||
		task.Type != agentruntime.MediaAssemblyTaskType || task.Capability != "video" || task.Status != model.TaskStatusSucceeded ||
		task.Operation != operation || task.BillingOrderID != "" || task.ProviderRequestID != "" {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly task delivery facts are invalid")
	}
	input, err := decodeMediaAssemblyTaskInput(task.InputJSON)
	if err != nil || input.Scope != scope || input.PlanRevision != arguments.PlanRevision ||
		input.ToolCallID != call.ToolCallID || input.ActionVersion != call.ActionVersion {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly frozen delivery facts are invalid")
	}
	result, err := decodeMediaAssemblyTaskResult(task.ResultJSON)
	if err != nil || result.PlanDigest != input.PlanDigest || result.ResourceID != content.Final.ResourceID ||
		result.ArtifactRevision != content.Final.ArtifactRevision || result.ArtifactRevision.ArtifactID != input.OutputArtifactID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly result delivery facts are invalid")
	}
	revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, result.ArtifactRevision.ArtifactID, result.ArtifactRevision.RevisionID)
	if err != nil {
		return agentruntime.DeliveryArtifact{}, err
	}
	if revision.ID != result.ArtifactRevision.RevisionID || revision.ArtifactID != result.ArtifactRevision.ArtifactID ||
		revision.ArtifactKey != input.OutputArtifactKey || revision.Kind != "media_candidate" || revision.SchemaVersion != 1 ||
		revision.ResourceID != result.ResourceID || revision.CreatedByRunID != scope.RunID ||
		revision.LifecycleStatus != model.AgentArtifactLifecycleActive {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly revision delivery facts are invalid")
	}
	candidate, err := agentruntime.DecodeMediaCandidateContent([]byte(revision.PayloadJSON))
	if err != nil || candidate.CandidateKey != input.OutputArtifactKey || candidate.MediaKind != agentruntime.ArtifactVideo ||
		candidate.ProviderRequestIdentity != "local-assembly:"+input.PlanDigest || candidate.ResourceID != result.ResourceID ||
		candidate.SourceTaskID != task.ID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly artifact delivery facts are invalid")
	}
	head, err := s.repo.ArtifactHeadRevisionForScope(scope, revision.ArtifactID)
	if err != nil || head.ID != revision.ID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly current revision evidence is invalid")
	}
	resource, err := s.productionResourceForScope(scope, result.ResourceID)
	if err != nil || resource.ID != result.ResourceID || resource.Kind != "video" || resource.Status != model.ResourceStatusReady {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media assembly resource delivery facts are invalid")
	}
	return agentruntime.DeliveryArtifact{
		Kind: agentruntime.ArtifactVideo, ArtifactID: revision.ArtifactID, RevisionID: revision.ID,
		ResourceID: resource.ID, URL: "/api/resources/" + resource.ID + "/file", ResourceReady: true,
		SourceTaskID: task.ID, SourceTaskSucceeded: true, CurrentRevision: true,
	}, nil
}

func successfulPublicationIDsByRevision(publications []model.AgentAssetPublication) (map[string]string, error) {
	byRevisionID := make(map[string]string, len(publications))
	for _, publication := range publications {
		if publication.Status != model.AgentAssetPublicationSucceeded ||
			!validStoredText(publication.ID, 120) || !validStoredText(publication.ArtifactRevisionID, 120) {
			return nil, errors.New("agent publication delivery facts are invalid")
		}
		if byRevisionID[publication.ArtifactRevisionID] == "" {
			byRevisionID[publication.ArtifactRevisionID] = publication.ID
		}
	}
	return byRevisionID, nil
}

func mediaCandidateDeliveryArtifact(
	revision model.AgentArtifactRevision,
	resource model.Resource,
	approved bool,
	publicationID string,
) (agentruntime.DeliveryArtifact, error) {
	if !validStoredMediaCandidate(revision) || revision.ResourceID != resource.ID {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media candidate delivery facts are invalid")
	}
	candidate, err := agentruntime.DecodeMediaCandidateContent([]byte(revision.PayloadJSON))
	if err != nil || resource.Kind != string(candidate.MediaKind) {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media candidate resource facts are invalid")
	}
	if strings.TrimSpace(publicationID) != publicationID || len(publicationID) > 120 ||
		(publicationID != "" && (!approved || !agentMediaResourceReady(&resource))) {
		return agentruntime.DeliveryArtifact{}, errors.New("agent media candidate publication facts are invalid")
	}
	kind := agentruntime.ArtifactKind(candidate.MediaKind)
	artifact := agentruntime.DeliveryArtifact{
		Kind: kind, ArtifactID: revision.ArtifactID, RevisionID: revision.ID,
		ResourceID: resource.ID, ResourceReady: agentMediaResourceReady(&resource),
		Approved: approved, PublicationID: publicationID,
	}
	if artifact.ResourceReady {
		artifact.URL = "/api/resources/" + resource.ID + "/file"
	}
	return artifact, nil
}

func committedPlanDeliveryArtifacts(
	canvasID string,
	artifacts []model.AgentProductionArtifact,
	resources map[string]model.Resource,
) ([]agentruntime.DeliveryArtifact, error) {
	canvasID = strings.TrimSpace(canvasID)
	if canvasID == "" {
		return nil, errors.New("agent committed plan canvas identity is invalid")
	}
	evidence := make([]agentruntime.DeliveryArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.Status != model.AgentProductionArtifactCommitted || strings.TrimSpace(artifact.CanvasNodeID) == "" {
			return nil, errors.New("agent committed plan artifact evidence is incomplete")
		}
		switch artifact.Kind {
		case model.AgentProductionArtifactScript:
			evidence = append(evidence, agentruntime.DeliveryArtifact{
				Kind: agentruntime.ArtifactText,
				URL:  fmt.Sprintf("canvas://%s/nodes/%s", canvasID, artifact.CanvasNodeID),
			})
		case model.AgentProductionArtifactReferenceImage, model.AgentProductionArtifactStoryboardImage:
			resource, exists := resources[artifact.ResourceID]
			if !exists || resource.Status != model.ResourceStatusReady {
				return nil, errors.New("agent committed image resource evidence is incomplete")
			}
			evidence = append(evidence, agentruntime.DeliveryArtifact{Kind: agentruntime.ArtifactImage, URL: "/api/resources/" + resource.ID + "/file"})
		case model.AgentProductionArtifactVideoClip:
			resource, exists := resources[artifact.ResourceID]
			if !exists || resource.Status != model.ResourceStatusReady {
				return nil, errors.New("agent committed video resource evidence is incomplete")
			}
			evidence = append(evidence, agentruntime.DeliveryArtifact{Kind: agentruntime.ArtifactVideo, URL: "/api/resources/" + resource.ID + "/file"})
		default:
			return nil, errors.New("agent committed plan artifact kind is invalid")
		}
	}
	return evidence, nil
}
