package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

var ErrAgentRunRecoveryFactsInvalid = errors.New("agent run recovery frozen facts are invalid")

func (s *Service) RecoverAgentRunTree(scope agentruntime.Scope) error {
	_, err := s.RecoverAgentRunTreeProgress(scope)
	return err
}

// RecoverAgentRunTreeProgress validates the same immutable recovery facts and
// exposes their read-only structural projection. It never advances a stage or
// chooses the Main Agent's semantic next action.
func (s *Service) RecoverAgentRunTreeProgress(
	scope agentruntime.Scope,
) (*agentruntime.ProductionNextActionProjection, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return nil, recoveryFactsError("load run identity", err)
	}
	if run.RuntimeVersion != agentruntime.ProductionRuntimeVersion {
		return nil, nil
	}
	snapshot, err := s.repo.AgentRunTreeRecoverySnapshot(scope)
	if err != nil {
		return nil, recoveryFactsError("load run tree snapshot", err)
	}
	if err := validateAgentRunRecoverySnapshot(snapshot); err != nil {
		return nil, recoveryFactsError("validate persisted run tree", err)
	}
	if err := s.validateFrozenSkillSelections(snapshot.State.Configuration.Skills); err != nil {
		return nil, recoveryFactsError("validate run skills", err)
	}
	for _, specialist := range snapshot.Specialists {
		request, decodeErr := specialistRequestFromRecoveryFacts(specialist)
		if decodeErr != nil {
			return nil, recoveryFactsError("decode specialist "+specialist.ID, decodeErr)
		}
		if err := s.validateFrozenSkillSelections(request.LoadedSkills); err != nil {
			return nil, recoveryFactsError("validate specialist "+specialist.ID+" skills", err)
		}
	}
	return snapshot.Production.Progress, nil
}

func validateAgentRunRecoverySnapshot(snapshot *repository.AgentRunTreeRecoverySnapshot) error {
	if snapshot == nil || snapshot.Production == nil {
		return errors.New("run tree snapshot is missing")
	}
	run := snapshot.Run
	state := snapshot.State
	if run.RuntimeVersion != agentruntime.ProductionRuntimeVersion || run.PolicyVersion != agentruntime.ProductionPolicyVersion ||
		run.ToolSchemaVersion != agentruntime.ProductionToolSchemaVersion || strings.TrimSpace(run.ModelRecordID) == "" ||
		strings.TrimSpace(run.ModelKey) == "" || run.StateVersion != state.StateVersion || run.StepNumber != state.StepNumber ||
		run.MaxSteps != state.MaxSteps || run.Status != state.Status {
		return errors.New("run and checkpoint facts conflict")
	}
	if err := agentruntime.ValidateRuntimeState(state); err != nil {
		return err
	}
	if snapshot.Production.Graph == nil {
		if len(snapshot.Specialists) != 0 || snapshot.Production.Progress != nil {
			return errors.New("specialists exist without a production graph")
		}
		return nil
	}
	if snapshot.Production.Draft == nil || len(snapshot.Production.Stages) != len(snapshot.Production.Draft.Stages) {
		return errors.New("production graph facts conflict")
	}
	progress := snapshot.Production.Progress
	if progress == nil || progress.GraphVersionID != snapshot.Production.Graph.ID ||
		progress.GraphVersion != snapshot.Production.Graph.Version {
		return errors.New("production progress facts conflict")
	}
	stageByID := make(map[string]agentruntime.ProductionStageDraft, len(snapshot.Production.Stages))
	for index, stage := range snapshot.Production.Stages {
		if index >= len(snapshot.Production.Draft.Stages) {
			return errors.New("production stage facts conflict")
		}
		stageByID[stage.ID] = snapshot.Production.Draft.Stages[index]
	}
	specialistByID := make(map[string]model.AgentSpecialistRun, len(snapshot.Specialists))
	for _, specialist := range snapshot.Specialists {
		if _, duplicated := specialistByID[specialist.ID]; duplicated {
			return errors.New("specialist identity is duplicated")
		}
		specialistByID[specialist.ID] = specialist
	}
	for _, specialist := range snapshot.Specialists {
		request, err := specialistRequestFromRecoveryFacts(specialist)
		if err != nil {
			return err
		}
		if err := agentruntime.ValidateSpecialistRequest(request, run.ModelRecordID, run.ModelKey); err != nil {
			return err
		}
		stage, found := stageByID[specialist.StageID]
		if !found || stage.SpecialistKey != specialist.SpecialistKey ||
			!reflect.DeepEqual(stage.InputRevisions, request.InputRevisions) ||
			!reflect.DeepEqual(stage.ExpectedDelivery, request.ExpectedDelivery) {
			return errors.New("specialist and stage facts conflict")
		}
		if specialist.ParentSpecialistRunID != "" {
			if _, found := specialistByID[specialist.ParentSpecialistRunID]; !found {
				return errors.New("specialist parent is missing")
			}
		}
		if !validRecoverableSpecialistLifecycle(specialist) {
			return errors.New("specialist lifecycle facts are invalid")
		}
	}
	if specialistParentCycle(snapshot.Specialists, specialistByID) {
		return errors.New("specialist parent graph contains a cycle")
	}
	return nil
}

func specialistRequestFromRecoveryFacts(specialist model.AgentSpecialistRun) (agentruntime.SpecialistRequest, error) {
	inputs, err := decodeRecoveryArtifactRevisionRefs(specialist.InputRevisionsJSON)
	if err != nil {
		return agentruntime.SpecialistRequest{}, err
	}
	skills, err := decodeRecoverySkillSelections(specialist.SkillVersionsJSON)
	if err != nil {
		return agentruntime.SpecialistRequest{}, err
	}
	tools, err := decodeRecoveryToolAllowlist(specialist.ToolAllowlistJSON)
	if err != nil {
		return agentruntime.SpecialistRequest{}, err
	}
	delivery, err := decodeRecoveryExpectedDelivery(specialist.ExpectedDeliveryJSON)
	if err != nil {
		return agentruntime.SpecialistRequest{}, err
	}
	return agentruntime.SpecialistRequest{
		SpecialistRunID: specialist.ID, StageID: specialist.StageID, SpecialistKey: specialist.SpecialistKey,
		SpecialistVersion: specialist.SpecialistVersion, ParentModelRecordID: specialist.ModelRecordID,
		ParentModelKey: specialist.ModelKey, Objective: specialist.Objective, InputRevisions: inputs,
		LoadedSkills: skills, ToolAllowlist: tools, ExpectedOutputSchema: specialist.ExpectedOutputSchema,
		ExpectedDelivery: delivery,
	}, nil
}

func (s *Service) validateFrozenSkillSelections(selections []agentruntime.SkillSelection) error {
	if err := agentruntime.ValidateRunConfiguration(agentruntime.RunConfiguration{
		ExecutionMode: agentruntime.ExecutionGuided,
		Skills:        selections,
	}); err != nil {
		return err
	}
	for _, selection := range selections {
		record, err := s.repo.PublishedSkillVersionByDir(selection.Dir, selection.Version)
		if err != nil {
			return err
		}
		manifest, err := decodeRecoverySkillCapabilityManifest(record.CapabilityManifestJSON)
		if err != nil {
			return err
		}
		publishedAt := ""
		if record.PublishedAt != nil {
			publishedAt = record.PublishedAt.UTC().Format(time.RFC3339)
		}
		if record.Dir != selection.Dir || record.Name != selection.Name || record.Description != selection.Description ||
			record.Instructions != selection.Instructions || record.Version != selection.Version || record.Checksum != selection.Checksum ||
			record.SourceKind != selection.SourceKind || record.SourceURL != selection.SourceURL ||
			record.SourceRevision != selection.SourceRevision || record.SourceLicense != selection.SourceLicense ||
			publishedAt != selection.PublishedAt || !reflect.DeepEqual(manifest, selection.CapabilityManifest) {
			return errors.New("published skill facts changed")
		}
	}
	return nil
}

func validRecoverableSpecialistLifecycle(run model.AgentSpecialistRun) bool {
	if strings.TrimSpace(run.ID) != run.ID || run.ID == "" || run.Version < 1 || run.Attempt < 0 ||
		run.InputTokens < 0 || run.CachedTokens < 0 || run.OutputTokens < 0 || run.CachedTokens > run.InputTokens {
		return false
	}
	switch run.Status {
	case model.AgentSpecialistRunQueued:
		return run.TaskID == "" && run.BillingOrderID == "" && run.CompletedAt == nil
	case model.AgentSpecialistRunRunning, model.AgentSpecialistRunWaitingInput,
		model.AgentSpecialistRunWaitingApproval, model.AgentSpecialistRunWaitingTool:
		return run.TaskID != "" && run.BillingOrderID != "" && run.CompletedAt == nil
	case model.AgentSpecialistRunSucceeded, model.AgentSpecialistRunFailed, model.AgentSpecialistRunCancelled:
		return run.CompletedAt != nil
	default:
		return false
	}
}

func specialistParentCycle(runs []model.AgentSpecialistRun, runByID map[string]model.AgentSpecialistRun) bool {
	for _, run := range runs {
		seen := make(map[string]struct{}, len(runs))
		current := run
		for current.ParentSpecialistRunID != "" {
			if _, duplicated := seen[current.ID]; duplicated {
				return true
			}
			seen[current.ID] = struct{}{}
			parent, found := runByID[current.ParentSpecialistRunID]
			if !found {
				return false
			}
			current = parent
		}
	}
	return false
}

func recoveryFactsError(operation string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrAgentRunRecoveryFactsInvalid, operation, err)
}

func decodeRecoveryArtifactRevisionRefs(encoded string) ([]agentruntime.ArtifactRevisionRef, error) {
	var result []agentruntime.ArtifactRevisionRef
	if err := decodeRecoveryJSON(encoded, func(decoder *json.Decoder) error { return decoder.Decode(&result) }); err != nil {
		return nil, err
	}
	return result, nil
}

func decodeRecoverySkillSelections(encoded string) ([]agentruntime.SkillSelection, error) {
	var result []agentruntime.SkillSelection
	if err := decodeRecoveryJSON(encoded, func(decoder *json.Decoder) error { return decoder.Decode(&result) }); err != nil {
		return nil, err
	}
	return result, nil
}

func decodeRecoveryToolAllowlist(encoded string) ([]agentruntime.AgentToolName, error) {
	var result []agentruntime.AgentToolName
	if err := decodeRecoveryJSON(encoded, func(decoder *json.Decoder) error { return decoder.Decode(&result) }); err != nil {
		return nil, err
	}
	return result, nil
}

func decodeRecoveryExpectedDelivery(encoded string) (agentruntime.ExpectedDelivery, error) {
	var result agentruntime.ExpectedDelivery
	if err := decodeRecoveryJSON(encoded, func(decoder *json.Decoder) error { return decoder.Decode(&result) }); err != nil {
		return agentruntime.ExpectedDelivery{}, err
	}
	return result, nil
}

func decodeRecoverySkillCapabilityManifest(encoded string) (agentruntime.SkillCapabilityManifest, error) {
	var result agentruntime.SkillCapabilityManifest
	if err := decodeRecoveryJSON(encoded, func(decoder *json.Decoder) error { return decoder.Decode(&result) }); err != nil {
		return agentruntime.SkillCapabilityManifest{}, err
	}
	if err := agentruntime.ValidateSkillCapabilityManifest(result); err != nil {
		return agentruntime.SkillCapabilityManifest{}, err
	}
	return result, nil
}

type recoveryJSONDecoder func(decoder *json.Decoder) error

func decodeRecoveryJSON(encoded string, decode recoveryJSONDecoder) error {
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decode(decoder); err != nil {
		return errors.New("persisted recovery json is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("persisted recovery json has trailing data")
	}
	return nil
}
