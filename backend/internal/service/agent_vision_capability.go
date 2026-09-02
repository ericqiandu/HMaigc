package service

import (
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

const (
	agentVisionTaskType                = "agent_vision_analysis"
	agentVisionOperation               = "agent_vision"
	agentVisionProviderDispatchStarted = "provider_dispatch_started"
)

type agentVisionTaskInput struct {
	RunID       string                                `json:"runId"`
	ToolCallID  string                                `json:"toolCallId"`
	Arguments   agentruntime.VisionAnalyzeArguments   `json:"arguments"`
	FrozenModel agentruntime.GenerationModelSelection `json:"frozenModel"`
}

type agentVisionAttempt struct {
	TaskID                string
	BillingIdempotencyKey string
	Input                 agentVisionTaskInput
	Model                 model.ChannelModel
	Pricing               TokenPricingSnapshot
	BillingAuthority      tokenBillingAuthority
	EstimatedInputTokens  int64
	MaxOutputTokens       int64
	AmountMicrocredits    int64
}

func agentVisionOperationForRun(runID string) string {
	return agentVisionOperation + ":" + strings.TrimSpace(runID)
}

func agentVisionRunID(operation string) (string, bool) {
	prefix := agentVisionOperation + ":"
	operation = strings.TrimSpace(operation)
	if !strings.HasPrefix(operation, prefix) || len(operation) > 96 {
		return "", false
	}
	runID := strings.TrimSpace(strings.TrimPrefix(operation, prefix))
	return runID, runID != ""
}

func (s *Service) freezeAgentVisionCapabilityQuote(
	scope agentruntime.Scope,
	call agentruntime.ToolCallDecision,
	now time.Time,
) (*agentruntime.ApprovalCostQuote, error) {
	attempt, err := s.prepareAgentVisionAttempt(scope, call, now)
	if err != nil {
		return nil, err
	}
	return &agentruntime.ApprovalCostQuote{
		ModelRecordID: attempt.Model.ID, ModelKey: attempt.Model.ModelKey,
		PriceVersion: attempt.Model.PriceVersion, AmountMicrocredits: attempt.AmountMicrocredits,
	}, nil
}

func (s *Service) prepareAgentVisionAttempt(
	scope agentruntime.Scope,
	call agentruntime.ToolCallDecision,
	now time.Time,
) (*agentVisionAttempt, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		return nil, errors.New("vision.analyze quote time is invalid")
	}
	decoded, err := agentruntime.DecodeCapabilityArguments(call.ToolName, call.Arguments)
	if err != nil {
		return nil, err
	}
	arguments, ok := decoded.(agentruntime.VisionAnalyzeArguments)
	if !ok || call.ToolName != agentruntime.ToolVisionAnalyze || call.ActionVersion != 1 {
		return nil, errors.New("vision.analyze call identity is invalid")
	}
	state, err := s.repo.LoadAgentCheckpoint(scope)
	if err != nil {
		return nil, err
	}
	frozen := state.Configuration.GenerationModels.Vision
	if frozen == nil || strings.TrimSpace(frozen.ChannelID) == "" ||
		strings.TrimSpace(frozen.ModelRecordID) == "" || strings.TrimSpace(frozen.Model) == "" || frozen.PriceVersion < 1 ||
		frozen.ModelRecordID != arguments.ModelRecordID || frozen.Model != arguments.ModelKey {
		return nil, errors.New("vision.analyze model does not match the frozen Run")
	}
	selected, err := s.repo.ChannelModelByRecordID(frozen.ModelRecordID)
	if err != nil {
		return nil, err
	}
	if selected == nil || !selected.Enabled || !selected.PriceConfigured || selected.ChannelID != frozen.ChannelID ||
		selected.ModelKey != frozen.Model || selected.PriceVersion != frozen.PriceVersion || normalizeCapability(selected.Capability) != "vision" {
		return nil, errors.New("vision.analyze frozen model facts changed")
	}
	accessible, err := s.requireAccessibleChannelModel(scope.ActorUserID, selected.ChannelID, selected.ModelKey)
	if err != nil || accessible.ID != selected.ID {
		return nil, errors.Join(errors.New("vision.analyze model is not accessible"), err)
	}
	channel, err := s.repo.SystemChannel(selected.ChannelID)
	if err != nil {
		return nil, err
	}
	pricing, err := s.repo.ModelPricing(selected.ChannelID, selected.ModelKey, "vision")
	if err != nil {
		return nil, err
	}
	snapshot, authority, err := tokenBillingContract(*channel, *selected, pricing)
	if err != nil {
		return nil, err
	}
	if _, err := s.agentVisionResourcesForRun(scope, state.Configuration, arguments.SourceResourceIDs); err != nil {
		return nil, err
	}
	imageTokenCeiling := modelInputImageTokenCeiling(selected.ModelKey)
	if imageTokenCeiling <= 0 {
		return nil, errors.New("vision.analyze model has no image token ceiling")
	}
	estimatedInputTokens := int64(len([]byte(arguments.Prompt))) + tokenBillingProtocolMarginBytes + int64(len(arguments.SourceResourceIDs)*imageTokenCeiling)
	policy, err := s.creditPolicy()
	if err != nil {
		return nil, err
	}
	multiplierBPS := policy.DefaultMultiplierBPS
	if configured := policy.ModelMultiplierBPS[selected.ModelKey]; configured > 0 {
		multiplierBPS = configured
	}
	if multiplierBPS != basisPointsScale {
		return nil, errors.New("vision.analyze first release requires a 1.0 billing multiplier")
	}
	amount, err := tokenChargeMicrocredits(snapshot, TokenUsageFact{
		InputTokens: estimatedInputTokens, OutputTokens: snapshot.MaxOutputTokens,
	}, multiplierBPS)
	if err != nil {
		return nil, err
	}
	taskID, err := agentruntime.CapabilityIdempotencyKey(scope, call)
	if err != nil || strings.TrimSpace(taskID) == "" {
		return nil, errors.Join(errors.New("vision.analyze task identity is invalid"), err)
	}
	return &agentVisionAttempt{
		TaskID: taskID, BillingIdempotencyKey: "agent-vision:" + taskID,
		Input: agentVisionTaskInput{
			RunID: scope.RunID, ToolCallID: call.ToolCallID,
			Arguments: agentruntime.VisionAnalyzeArguments{
				ModelRecordID: arguments.ModelRecordID, ModelKey: arguments.ModelKey,
				SourceResourceIDs: append([]string(nil), arguments.SourceResourceIDs...), Prompt: arguments.Prompt,
				Detail: arguments.Detail, ClientRequestID: arguments.ClientRequestID,
			},
			FrozenModel: *frozen,
		},
		Model: *selected, Pricing: snapshot, BillingAuthority: authority,
		EstimatedInputTokens: estimatedInputTokens, MaxOutputTokens: snapshot.MaxOutputTokens,
		AmountMicrocredits: amount,
	}, nil
}
