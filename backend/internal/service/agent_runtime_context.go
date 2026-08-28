package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
)

const agentRuntimeModelPromptPrefix = "以下 JSON 是本轮唯一可信的运行事实。请自主决定直接交付、发起结构化追问或调用一个可用工具，并严格按系统约定返回一个 JSON 对象：\n"

type agentRuntimeCallableModelFact struct {
	ChannelID             string                        `json:"channelId"`
	Model                 string                        `json:"model"`
	DisplayName           string                        `json:"displayName"`
	Capability            string                        `json:"capability"`
	BillingMode           string                        `json:"billingMode"`
	PriceStrategy         string                        `json:"priceStrategy"`
	UnitPriceMicrocredits int64                         `json:"unitPriceMicrocredits"`
	PriceTiers            []PublicChannelModelPriceTier `json:"priceTiers"`
	ProviderCapabilities  *PublicProviderCapabilities   `json:"providerCapabilities,omitempty"`
}

type agentRuntimeCallableToolFact struct {
	Name             agentruntime.ToolName      `json:"name"`
	ActionVersion    int                        `json:"actionVersion"`
	RiskLevel        agentruntime.ToolRiskLevel `json:"riskLevel"`
	RequiredAccess   agentruntime.AccessLevel   `json:"requiredAccess"`
	ApprovalRequired bool                       `json:"approvalRequired"`
}

type agentRuntimeProductionStageFact struct {
	ID                 string                             `json:"id"`
	StageKey           string                             `json:"stageKey"`
	SpecialistKey      agentruntime.SpecialistKey         `json:"specialistKey"`
	DependsOnStageKeys []string                           `json:"dependsOnStageKeys"`
	InputRevisions     []agentruntime.ArtifactRevisionRef `json:"inputRevisions"`
	ExpectedDelivery   agentruntime.ExpectedDelivery      `json:"expectedDelivery"`
	ReviewPolicy       agentruntime.ReviewPolicy          `json:"reviewPolicy"`
	CostPolicy         agentruntime.CostPolicy            `json:"costPolicy"`
	Status             agentruntime.ProductionStageStatus `json:"status"`
	Version            int64                              `json:"version"`
	ReviewRevisionID   string                             `json:"reviewRevisionId,omitempty"`
	LastErrorCode      string                             `json:"lastErrorCode,omitempty"`
}

type agentRuntimeProductionGraphFact struct {
	ID            string                            `json:"id"`
	GraphKey      string                            `json:"graphKey"`
	Version       int64                             `json:"version"`
	SchemaVersion int                               `json:"schemaVersion"`
	Stages        []agentRuntimeProductionStageFact `json:"stages"`
}

type agentRuntimeArtifactSkillVersionFact struct {
	Dir      string `json:"dir"`
	Version  int    `json:"version"`
	Checksum string `json:"checksum"`
}

type agentRuntimeArtifactSummaryFact struct {
	ArtifactID            string                                 `json:"artifactId"`
	ArtifactKey           string                                 `json:"artifactKey"`
	Kind                  string                                 `json:"kind"`
	HeadRevision          int64                                  `json:"headRevision"`
	RevisionID            string                                 `json:"revisionId"`
	SchemaVersion         int                                    `json:"schemaVersion"`
	ResourceID            string                                 `json:"resourceId,omitempty"`
	UpstreamRevisions     []agentruntime.ArtifactRevisionRef     `json:"upstreamRevisions"`
	SkillVersions         []agentRuntimeArtifactSkillVersionFact `json:"skillVersions"`
	ModelRequestIdentity  string                                 `json:"modelRequestIdentity,omitempty"`
	CreatedBySpecialistID string                                 `json:"createdBySpecialistId,omitempty"`
	LifecycleStatus       string                                 `json:"lifecycleStatus"`
}

type agentRuntimeProductionContextFact struct {
	Graph        *agentRuntimeProductionGraphFact
	CurrentStage *agentRuntimeProductionStageFact
	Artifacts    []agentRuntimeArtifactSummaryFact
}

type agentRuntimeProductionPlanFact struct {
	PlanKey           string                             `json:"planKey"`
	PlanVersion       int                                `json:"planVersion"`
	Title             string                             `json:"title"`
	TargetDurationMS  int                                `json:"targetDurationMs"`
	Script            string                             `json:"script"`
	References        []agentruntime.ReferenceAssetDraft `json:"references"`
	Shots             []agentruntime.ShotPlanDraft       `json:"shots"`
	Artifacts         []agentProductionArtifactResult    `json:"artifacts"`
	CommitArtifactIDs []string                           `json:"commitArtifactIds"`
}

func (s *Service) agentRuntimeModelPrompt(scope agentruntime.Scope, state agentruntime.RuntimeState) (string, error) {
	canvas, _, err := s.canvasAccess(scope.ActorUserID, scope.CanvasID)
	if err != nil {
		return "", err
	}
	models, err := s.agentRuntimeCallableModels(scope.ActorUserID)
	if err != nil {
		return "", err
	}
	models, err = filterAgentRuntimeCallableModels(models, state.Configuration.GenerationModels)
	if err != nil {
		return "", err
	}
	run, err := s.repo.AgentRunForScope(scope)
	if err != nil {
		return "", err
	}
	var productionPlan *agentRuntimeProductionPlanFact
	var production *agentRuntimeProductionContextFact
	switch run.ToolSchemaVersion {
	case agentruntime.LegacyToolSchemaVersion:
		productionPlan, err = s.agentRuntimeProductionPlanFact(scope)
	case agentruntime.CurrentToolSchemaVersion:
		production, err = s.loadAgentRuntimeProductionContextFact(scope)
	default:
		return "", errors.New("agent runtime tool schema version is invalid")
	}
	if err != nil {
		return "", err
	}
	var deliveryEvidence *agentruntime.DeliveryEvidence
	var deliveryVerification *agentruntime.DeliveryVerification
	if state.ExpectedDelivery != nil {
		evidence, evidenceErr := s.agentRuntimeDeliveryEvidence(scope, state.FinalMessage)
		if evidenceErr != nil {
			return "", evidenceErr
		}
		verification := agentruntime.VerifyDelivery(*state.ExpectedDelivery, evidence)
		deliveryEvidence = &evidence
		deliveryVerification = &verification
	}
	return encodeAgentRuntimeModelPromptForToolSchema(
		scope,
		state,
		canvas.Revision,
		run.ToolSchemaVersion,
		models,
		productionPlan,
		production,
		deliveryEvidence,
		deliveryVerification,
	)
}

func (s *Service) loadAgentRuntimeProductionContextFact(scope agentruntime.Scope) (*agentRuntimeProductionContextFact, error) {
	snapshot, err := s.repo.ProductionRuntimeSnapshotForScope(scope)
	if err != nil {
		return nil, err
	}
	context := &agentRuntimeProductionContextFact{Artifacts: []agentRuntimeArtifactSummaryFact{}}
	if snapshot.Graph == nil {
		return context, nil
	}
	if snapshot.Draft == nil || len(snapshot.Draft.Stages) != len(snapshot.Stages) {
		return nil, errors.New("production agent runtime snapshot facts are invalid")
	}

	graph := &agentRuntimeProductionGraphFact{
		ID:            snapshot.Graph.ID,
		GraphKey:      snapshot.Graph.GraphKey,
		Version:       snapshot.Graph.Version,
		SchemaVersion: snapshot.Graph.SchemaVersion,
		Stages:        make([]agentRuntimeProductionStageFact, 0, len(snapshot.Stages)),
	}
	activeStages := make([]agentRuntimeProductionStageFact, 0, 1)
	for index, stored := range snapshot.Stages {
		draft := snapshot.Draft.Stages[index]
		stage := agentRuntimeProductionStageFact{
			ID: stored.ID, StageKey: draft.StageKey, SpecialistKey: draft.SpecialistKey,
			DependsOnStageKeys: append([]string(nil), draft.DependsOnStageKeys...),
			InputRevisions:     append([]agentruntime.ArtifactRevisionRef(nil), draft.InputRevisions...),
			ExpectedDelivery:   draft.ExpectedDelivery, ReviewPolicy: draft.ReviewPolicy, CostPolicy: draft.CostPolicy,
			Status: stored.Status, Version: stored.Version, ReviewRevisionID: stored.ReviewRevisionID,
			LastErrorCode: stored.LastErrorCode,
		}
		graph.Stages = append(graph.Stages, stage)
		if stage.Status == agentruntime.StageRunning || stage.Status == agentruntime.StageAwaitingReview {
			activeStages = append(activeStages, stage)
		}
	}
	context.Graph = graph
	if len(activeStages) == 1 {
		current := activeStages[0]
		context.CurrentStage = &current
	}

	context.Artifacts = make([]agentRuntimeArtifactSummaryFact, 0, len(snapshot.Artifacts))
	for _, head := range snapshot.Artifacts {
		var upstream []agentruntime.ArtifactRevisionRef
		if err := decodeAgentRuntimeContextJSON(head.Revision.UpstreamRevisionsJSON, &upstream); err != nil {
			return nil, err
		}
		var skills []agentruntime.SkillSelection
		if err := decodeAgentRuntimeContextJSON(head.Revision.SkillVersionsJSON, &skills); err != nil {
			return nil, err
		}
		if len(skills) > 0 {
			if err := agentruntime.ValidateRunConfiguration(agentruntime.RunConfiguration{
				ExecutionMode: agentruntime.ExecutionGuided,
				Skills:        skills,
			}); err != nil {
				return nil, errors.New("production artifact skill version facts are invalid")
			}
		}
		skillFacts := make([]agentRuntimeArtifactSkillVersionFact, 0, len(skills))
		for _, skill := range skills {
			skillFacts = append(skillFacts, agentRuntimeArtifactSkillVersionFact{
				Dir: skill.Dir, Version: skill.Version, Checksum: skill.Checksum,
			})
		}
		sort.Slice(skillFacts, func(left int, right int) bool {
			return skillFacts[left].Dir+"\x00"+skillFacts[left].Checksum < skillFacts[right].Dir+"\x00"+skillFacts[right].Checksum
		})
		context.Artifacts = append(context.Artifacts, agentRuntimeArtifactSummaryFact{
			ArtifactID: head.Artifact.ID, ArtifactKey: head.Artifact.ArtifactKey, Kind: head.Artifact.Kind,
			HeadRevision: head.Artifact.HeadRevision, RevisionID: head.Revision.ID, SchemaVersion: head.Revision.SchemaVersion,
			ResourceID: head.Revision.ResourceID, UpstreamRevisions: upstream, SkillVersions: skillFacts,
			ModelRequestIdentity: head.Revision.ModelRequestIdentity, CreatedBySpecialistID: head.Revision.CreatedBySpecialistID,
			LifecycleStatus: head.Revision.LifecycleStatus,
		})
	}
	canonical := canonicalAgentRuntimeProductionContext(*context)
	if err := validateAgentRuntimeProductionContext(canonical); err != nil {
		return nil, err
	}
	return &canonical, nil
}

func decodeAgentRuntimeContextJSON(encoded string, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewBufferString(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("production agent runtime snapshot JSON is invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("production agent runtime snapshot JSON is invalid")
	}
	return nil
}

func (s *Service) agentRuntimeProductionPlanFact(scope agentruntime.Scope) (*agentRuntimeProductionPlanFact, error) {
	record, err := s.repo.ActiveAgentProductionPlanForThread(scope)
	if err != nil || record == nil {
		return nil, err
	}
	var references []agentruntime.ReferenceAssetDraft
	if err := json.Unmarshal([]byte(record.Plan.ReferencesJSON), &references); err != nil {
		return nil, errors.New("active agent production plan references are invalid")
	}
	var shots []agentruntime.ShotPlanDraft
	if err := json.Unmarshal([]byte(record.Plan.ShotsJSON), &shots); err != nil {
		return nil, errors.New("active agent production plan shots are invalid")
	}
	fact := &agentRuntimeProductionPlanFact{
		PlanKey: record.Plan.PlanKey, PlanVersion: record.Plan.Version, Title: record.Plan.Title,
		TargetDurationMS: record.Plan.TargetDurationMS, Script: record.Plan.Script, References: references, Shots: shots,
		Artifacts: make([]agentProductionArtifactResult, 0, len(record.Artifacts)), CommitArtifactIDs: make([]string, 0, len(record.Artifacts)),
	}
	for _, artifact := range record.Artifacts {
		fact.Artifacts = append(fact.Artifacts, agentProductionArtifactResult{
			ArtifactID: artifact.ID, Kind: artifact.Kind, ReferenceKey: artifact.ReferenceKey, ShotKey: artifact.ShotKey, Status: artifact.Status,
		})
		fact.CommitArtifactIDs = append(fact.CommitArtifactIDs, artifact.ID)
	}
	sort.Strings(fact.CommitArtifactIDs)
	return fact, nil
}

func (s *Service) agentRuntimeCallableModels(actorUserID string) ([]agentRuntimeCallableModelFact, error) {
	hasMembership, err := s.HasActiveMembership(actorUserID)
	if err != nil {
		return nil, err
	}
	channels, err := s.repo.SystemChannels(false)
	if err != nil {
		return nil, err
	}
	result := make([]agentRuntimeCallableModelFact, 0)
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		items, err := s.repo.ChannelModels(channel.ID, false)
		if err != nil {
			return nil, err
		}
		items, err = s.publiclyCallableChannelModels(items)
		if err != nil {
			return nil, err
		}
		public := publicChannel(channel, false, items, hasMembership)
		for _, item := range public.ModelCosts {
			if !item.Accessible || !agentRuntimeMediaCapability(item.Capability) {
				continue
			}
			result = append(result, agentRuntimeCallableModelFact{
				ChannelID: channel.ID, Model: item.Model, DisplayName: item.DisplayName, Capability: item.Capability,
				BillingMode: item.BillingMode, PriceStrategy: item.PriceStrategy,
				UnitPriceMicrocredits: item.UnitPriceMicrocredits,
				PriceTiers:            append([]PublicChannelModelPriceTier(nil), item.PriceTiers...),
				ProviderCapabilities:  clonePublicProviderCapabilities(item.ProviderCapabilities),
			})
		}
	}
	sort.Slice(result, func(left int, right int) bool {
		if result[left].ChannelID == result[right].ChannelID {
			return result[left].Model < result[right].Model
		}
		return result[left].ChannelID < result[right].ChannelID
	})
	if err := validateAgentRuntimeCallableModels(result); err != nil {
		return nil, err
	}
	return result, nil
}

func encodeAgentRuntimeModelPrompt(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	canvasRevision int64,
	models []agentRuntimeCallableModelFact,
	productionPlan *agentRuntimeProductionPlanFact,
	deliveryEvidence *agentruntime.DeliveryEvidence,
	deliveryVerification *agentruntime.DeliveryVerification,
) (string, error) {
	return encodeAgentRuntimeModelPromptForToolSchema(
		scope,
		state,
		canvasRevision,
		agentruntime.CurrentToolSchemaVersion,
		models,
		productionPlan,
		nil,
		deliveryEvidence,
		deliveryVerification,
	)
}

func encodeAgentRuntimeModelPromptForToolSchema(
	scope agentruntime.Scope,
	state agentruntime.RuntimeState,
	canvasRevision int64,
	toolSchemaVersion int,
	models []agentRuntimeCallableModelFact,
	productionPlan *agentRuntimeProductionPlanFact,
	production *agentRuntimeProductionContextFact,
	deliveryEvidence *agentruntime.DeliveryEvidence,
	deliveryVerification *agentruntime.DeliveryVerification,
) (string, error) {
	callableTools, err := agentRuntimeCallableTools(toolSchemaVersion, state.Configuration.ExecutionMode)
	if err != nil {
		return "", err
	}
	var productionGraph *agentRuntimeProductionGraphFact
	var currentStage *agentRuntimeProductionStageFact
	var artifacts []agentRuntimeArtifactSummaryFact
	switch toolSchemaVersion {
	case agentruntime.LegacyToolSchemaVersion:
		if production != nil {
			return "", errors.New("legacy agent runtime context contains production graph facts")
		}
	case agentruntime.CurrentToolSchemaVersion:
		if productionPlan != nil || production == nil {
			return "", errors.New("production agent runtime context facts are invalid")
		}
		canonical := canonicalAgentRuntimeProductionContext(*production)
		if err := validateAgentRuntimeProductionContext(canonical); err != nil {
			return "", err
		}
		productionGraph = canonical.Graph
		currentStage = canonical.CurrentStage
		artifacts = canonical.Artifacts
	default:
		return "", errors.New("agent runtime tool schema version is invalid")
	}
	context := agentRuntimeModelContext{
		RunID: scope.RunID, CanvasID: scope.CanvasID, ToolSchemaVersion: toolSchemaVersion, CanvasRevision: canvasRevision, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		UserMessage: state.UserMessage, ExpectedDelivery: state.ExpectedDelivery, DeliveryEvidence: deliveryEvidence,
		Verification: deliveryVerification, LastToolResult: state.LastToolResult, DecisionFeedback: state.DecisionFeedback, PreviousMessage: state.FinalMessage,
		Configuration: promptAgentRuntimeConfiguration(state), LoadedSkillDirs: append([]string(nil), state.LoadedSkillDirs...), CallableTools: callableTools, CallableModels: models,
		ClarificationHistory: append([]agentruntime.CompletedClarification(nil), state.ClarificationHistory...), ProductionPlan: productionPlan,
		ProductionGraph: productionGraph, CurrentStage: currentStage, Artifacts: artifacts,
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return "", err
	}
	return agentRuntimeModelPromptPrefix + string(encoded), nil
}

func agentRuntimeCallableTools(toolSchemaVersion int, mode agentruntime.ExecutionMode) ([]agentRuntimeCallableToolFact, error) {
	policies, ok := agentruntime.ToolPoliciesForSchema(toolSchemaVersion)
	if !ok || (mode != agentruntime.ExecutionGuided && mode != agentruntime.ExecutionAutomatic) {
		return nil, errors.New("agent runtime callable tool facts are invalid")
	}
	tools := make([]agentRuntimeCallableToolFact, 0, len(policies))
	for _, policy := range policies {
		tools = append(tools, agentRuntimeCallableToolFact{
			Name:             policy.Name,
			ActionVersion:    1,
			RiskLevel:        policy.RiskLevel,
			RequiredAccess:   policy.RequiredAccess,
			ApprovalRequired: agentruntime.ApprovalRequiredFor(policy, mode),
		})
	}
	return tools, nil
}

func canonicalAgentRuntimeProductionContext(context agentRuntimeProductionContextFact) agentRuntimeProductionContextFact {
	if context.Graph != nil {
		graph := *context.Graph
		graph.Stages = append([]agentRuntimeProductionStageFact(nil), context.Graph.Stages...)
		for index := range graph.Stages {
			graph.Stages[index].DependsOnStageKeys = append([]string(nil), graph.Stages[index].DependsOnStageKeys...)
			if graph.Stages[index].DependsOnStageKeys == nil {
				graph.Stages[index].DependsOnStageKeys = []string{}
			}
			graph.Stages[index].InputRevisions = append([]agentruntime.ArtifactRevisionRef(nil), graph.Stages[index].InputRevisions...)
			if graph.Stages[index].InputRevisions == nil {
				graph.Stages[index].InputRevisions = []agentruntime.ArtifactRevisionRef{}
			}
		}
		context.Graph = &graph
	}
	if context.CurrentStage != nil {
		stage := *context.CurrentStage
		stage.DependsOnStageKeys = append([]string(nil), stage.DependsOnStageKeys...)
		if stage.DependsOnStageKeys == nil {
			stage.DependsOnStageKeys = []string{}
		}
		stage.InputRevisions = append([]agentruntime.ArtifactRevisionRef(nil), stage.InputRevisions...)
		if stage.InputRevisions == nil {
			stage.InputRevisions = []agentruntime.ArtifactRevisionRef{}
		}
		context.CurrentStage = &stage
	}
	context.Artifacts = append([]agentRuntimeArtifactSummaryFact(nil), context.Artifacts...)
	if context.Artifacts == nil {
		context.Artifacts = []agentRuntimeArtifactSummaryFact{}
	}
	for index := range context.Artifacts {
		context.Artifacts[index].UpstreamRevisions = append([]agentruntime.ArtifactRevisionRef(nil), context.Artifacts[index].UpstreamRevisions...)
		if context.Artifacts[index].UpstreamRevisions == nil {
			context.Artifacts[index].UpstreamRevisions = []agentruntime.ArtifactRevisionRef{}
		}
		context.Artifacts[index].SkillVersions = append([]agentRuntimeArtifactSkillVersionFact(nil), context.Artifacts[index].SkillVersions...)
		if context.Artifacts[index].SkillVersions == nil {
			context.Artifacts[index].SkillVersions = []agentRuntimeArtifactSkillVersionFact{}
		}
	}
	return context
}

func validateAgentRuntimeProductionContext(context agentRuntimeProductionContextFact) error {
	if context.Graph == nil {
		if context.CurrentStage != nil || len(context.Artifacts) != 0 {
			return errors.New("production agent runtime context facts are invalid")
		}
		return nil
	}
	graph := context.Graph
	if strings.TrimSpace(graph.ID) != graph.ID || graph.ID == "" || len(graph.ID) > 80 ||
		strings.TrimSpace(graph.GraphKey) != graph.GraphKey || graph.GraphKey == "" || len(graph.GraphKey) > 120 ||
		graph.Version < 1 || graph.SchemaVersion != agentruntime.CurrentProductionSchemaVersion || len(graph.Stages) == 0 {
		return errors.New("production agent runtime graph facts are invalid")
	}
	draft := agentruntime.ProductionGraphDraft{GraphKey: graph.GraphKey, Stages: make([]agentruntime.ProductionStageDraft, 0, len(graph.Stages))}
	stageByID := make(map[string]agentRuntimeProductionStageFact, len(graph.Stages))
	for _, stage := range graph.Stages {
		if strings.TrimSpace(stage.ID) != stage.ID || stage.ID == "" || len(stage.ID) > 80 || stage.Version < 1 || !stage.Status.Valid() {
			return errors.New("production agent runtime stage facts are invalid")
		}
		if _, duplicate := stageByID[stage.ID]; duplicate {
			return errors.New("production agent runtime stage facts are invalid")
		}
		stageByID[stage.ID] = stage
		draft.Stages = append(draft.Stages, agentruntime.ProductionStageDraft{
			StageKey: stage.StageKey, SpecialistKey: stage.SpecialistKey,
			DependsOnStageKeys: stage.DependsOnStageKeys, InputRevisions: stage.InputRevisions,
			ExpectedDelivery: stage.ExpectedDelivery, ReviewPolicy: stage.ReviewPolicy, CostPolicy: stage.CostPolicy,
		})
	}
	if err := agentruntime.ValidateProductionGraph(draft); err != nil {
		return err
	}
	if context.CurrentStage != nil {
		stage, found := stageByID[context.CurrentStage.ID]
		if !found || !reflect.DeepEqual(stage, *context.CurrentStage) {
			return errors.New("production agent runtime current stage facts are invalid")
		}
	}
	previousArtifactID := ""
	for _, artifact := range context.Artifacts {
		if strings.TrimSpace(artifact.ArtifactID) != artifact.ArtifactID || artifact.ArtifactID == "" || len(artifact.ArtifactID) > 80 ||
			strings.TrimSpace(artifact.RevisionID) != artifact.RevisionID || artifact.RevisionID == "" || len(artifact.RevisionID) > 80 ||
			!validAgentRuntimeContractName(artifact.ArtifactKey) || !validAgentRuntimeContractName(artifact.Kind) ||
			artifact.HeadRevision < 1 || artifact.SchemaVersion < 1 || strings.TrimSpace(artifact.LifecycleStatus) == "" ||
			(previousArtifactID != "" && artifact.ArtifactID <= previousArtifactID) || validateAgentRuntimeRevisionRefs(artifact.UpstreamRevisions) != nil {
			return errors.New("production agent runtime artifact facts are invalid")
		}
		previousArtifactID = artifact.ArtifactID
		previousSkill := ""
		for _, skill := range artifact.SkillVersions {
			identity := skill.Dir + "\x00" + skill.Checksum
			if strings.TrimSpace(skill.Dir) != skill.Dir || skill.Dir == "" || skill.Version < 1 || strings.TrimSpace(skill.Checksum) != skill.Checksum || skill.Checksum == "" ||
				(previousSkill != "" && identity <= previousSkill) {
				return errors.New("production agent runtime artifact skill facts are invalid")
			}
			previousSkill = identity
		}
	}
	return nil
}

func promptAgentRuntimeConfiguration(state agentruntime.RuntimeState) agentruntime.RunConfiguration {
	configuration := state.Configuration
	configuration.Skills = append([]agentruntime.SkillSelection(nil), state.Configuration.Skills...)
	configuration.Attachments = append([]agentruntime.ResourceAttachment(nil), state.Configuration.Attachments...)
	loaded := make(map[string]struct{}, len(state.LoadedSkillDirs))
	for _, dir := range state.LoadedSkillDirs {
		loaded[dir] = struct{}{}
	}
	for index := range configuration.Skills {
		if _, ok := loaded[configuration.Skills[index].Dir]; !ok {
			configuration.Skills[index].Instructions = ""
		}
	}
	return configuration
}

func validateFrozenAgentRuntimeModelPrompt(scope agentruntime.Scope, state agentruntime.RuntimeState, prompt string) error {
	_, err := frozenAgentRuntimeModelContext(scope, state, prompt)
	return err
}

// frozenAgentRuntimeModelContext returns the exact facts shown to the model for
// this step. Render preparation must validate against this snapshot instead of
// re-reading a catalog that may have changed after the model made its choice.
func frozenAgentRuntimeModelContext(scope agentruntime.Scope, state agentruntime.RuntimeState, prompt string) (agentRuntimeModelContext, error) {
	if !strings.HasPrefix(prompt, agentRuntimeModelPromptPrefix) {
		return agentRuntimeModelContext{}, errors.New("agent runtime model prompt facts conflict")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(strings.TrimPrefix(prompt, agentRuntimeModelPromptPrefix))))
	decoder.DisallowUnknownFields()
	var frozen agentRuntimeModelContext
	if err := decoder.Decode(&frozen); err != nil {
		return agentRuntimeModelContext{}, errors.New("agent runtime model prompt facts conflict")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentRuntimeModelContext{}, errors.New("agent runtime model prompt facts conflict")
	}
	if err := validateAgentRuntimeCallableModels(frozen.CallableModels); err != nil {
		return agentRuntimeModelContext{}, err
	}
	var production *agentRuntimeProductionContextFact
	if frozen.ToolSchemaVersion == agentruntime.ProductionToolSchemaVersion {
		production = &agentRuntimeProductionContextFact{
			Graph: frozen.ProductionGraph, CurrentStage: frozen.CurrentStage, Artifacts: frozen.Artifacts,
		}
	}
	expected, err := encodeAgentRuntimeModelPromptForToolSchema(
		scope,
		state,
		frozen.CanvasRevision,
		frozen.ToolSchemaVersion,
		frozen.CallableModels,
		frozen.ProductionPlan,
		production,
		frozen.DeliveryEvidence,
		frozen.Verification,
	)
	if err != nil {
		return agentRuntimeModelContext{}, err
	}
	if prompt != expected {
		return agentRuntimeModelContext{}, errors.New("agent runtime model prompt facts conflict")
	}
	return frozen, nil
}

func validateAgentRuntimeCallableModels(models []agentRuntimeCallableModelFact) error {
	seen := make(map[string]struct{}, len(models))
	previous := ""
	for _, item := range models {
		item.ChannelID = strings.TrimSpace(item.ChannelID)
		item.Model = strings.TrimSpace(item.Model)
		item.DisplayName = strings.TrimSpace(item.DisplayName)
		item.BillingMode = strings.TrimSpace(item.BillingMode)
		item.PriceStrategy = strings.TrimSpace(item.PriceStrategy)
		if item.ChannelID == "" || item.Model == "" || item.DisplayName == "" || !agentRuntimeMediaCapability(item.Capability) || item.BillingMode == "" || item.PriceStrategy == "" {
			return errors.New("agent callable model facts are invalid")
		}
		key := item.ChannelID + "\x00" + item.Model
		if _, duplicate := seen[key]; duplicate || (previous != "" && key < previous) {
			return errors.New("agent callable model facts are invalid")
		}
		seen[key] = struct{}{}
		previous = key
		priced := item.UnitPriceMicrocredits > 0
		usageMetrics := make(map[string]struct{}, len(item.PriceTiers))
		for _, tier := range item.PriceTiers {
			resolution := strings.TrimSpace(tier.Resolution)
			inputVariant := strings.TrimSpace(tier.InputVariant)
			usageMetric := strings.ToLower(strings.TrimSpace(tier.UsageMetric))
			if tier.UnitPriceMicrocredits <= 0 {
				return errors.New("agent callable model pricing facts are invalid")
			}
			if usageMetric != "" {
				if usageMetric != inputImageUsageMetric || item.Capability != "image" || item.BillingMode != "fixed_request" || tier.IncludedQuantity < 0 || resolution != "" || inputVariant != "" {
					return errors.New("agent callable model pricing facts are invalid")
				}
				if _, duplicate := usageMetrics[usageMetric]; duplicate {
					return errors.New("agent callable model pricing facts are invalid")
				}
				usageMetrics[usageMetric] = struct{}{}
			} else if resolution == "" && inputVariant == "" {
				return errors.New("agent callable model pricing facts are invalid")
			}
			priced = true
		}
		if !priced {
			return errors.New("agent callable model pricing facts are invalid")
		}
	}
	return nil
}

func agentRuntimeMediaCapability(capability string) bool {
	switch strings.TrimSpace(capability) {
	case "image", "video", "audio", "vision":
		return true
	default:
		return false
	}
}
