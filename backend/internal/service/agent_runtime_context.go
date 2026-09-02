package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/agentruntime"
)

const agentRuntimeModelPromptPrefix = "以下 JSON 是本轮唯一可信的运行事实。请自主决定直接交付、发起结构化追问或调用一个可用工具，并严格按系统约定返回一个 JSON 对象：\n"
const agentRuntimeModelContextLimit = 512 * 1024

type agentRuntimeCallableModelFact struct {
	ChannelID             string                        `json:"channelId"`
	ModelRecordID         string                        `json:"modelRecordId"`
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
	ArgumentsSchema  json.RawMessage            `json:"argumentsSchema,omitempty"`
	ResultSchema     json.RawMessage            `json:"resultSchema,omitempty"`
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
	if err := validateAgentRuntimeExecutionContract(*run); err != nil {
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
	return encodeAgentRuntimeModelPrompt(
		scope,
		state,
		canvas.Revision,
		models,
		deliveryEvidence,
		deliveryVerification,
	)
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
		modelRecordIDs := make(map[string]string, len(items))
		for _, modelItem := range items {
			modelKey := strings.TrimSpace(modelItem.ModelKey)
			modelRecordID := strings.TrimSpace(modelItem.ID)
			if modelKey == "" || modelRecordID == "" {
				return nil, errors.New("agent callable model identity is invalid")
			}
			if _, duplicate := modelRecordIDs[modelKey]; duplicate {
				return nil, errors.New("agent callable model identity is invalid")
			}
			modelRecordIDs[modelKey] = modelRecordID
		}
		public := publicChannel(channel, false, items, hasMembership)
		for _, item := range public.ModelCosts {
			if !item.Accessible || !agentRuntimeMediaCapability(item.Capability) {
				continue
			}
			modelRecordID, found := modelRecordIDs[item.Model]
			if !found {
				return nil, errors.New("agent callable model identity is invalid")
			}
			result = append(result, agentRuntimeCallableModelFact{
				ChannelID: channel.ID, ModelRecordID: modelRecordID, Model: item.Model, DisplayName: item.DisplayName, Capability: item.Capability,
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
	deliveryEvidence *agentruntime.DeliveryEvidence,
	deliveryVerification *agentruntime.DeliveryVerification,
) (string, error) {
	callableTools, err := agentRuntimeCallableTools(state.Configuration.ExecutionMode)
	if err != nil {
		return "", err
	}
	context := agentRuntimeModelContext{
		RunID: scope.RunID, CanvasID: scope.CanvasID, Scope: agentRuntimeScopeFact{
			TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
			DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID,
		}, ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, CanvasRevision: canvasRevision, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		UserMessage: state.UserMessage, ExpectedDelivery: state.ExpectedDelivery, DeliveryEvidence: deliveryEvidence,
		Verification: deliveryVerification, LastToolResult: state.LastToolResult, DecisionFeedback: state.DecisionFeedback, PreviousMessage: state.FinalMessage,
		Configuration: promptAgentRuntimeConfiguration(state), LoadedSkillDirs: append([]string(nil), state.LoadedSkillDirs...), CallableTools: callableTools, CallableModels: models,
		ClarificationHistory: append([]agentruntime.CompletedClarification(nil), state.ClarificationHistory...), Limits: state.Limits,
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return "", err
	}
	if len(encoded) > agentRuntimeModelContextLimit {
		return "", errors.New("agent runtime model context exceeds limit")
	}
	return agentRuntimeModelPromptPrefix + string(encoded), nil
}

func agentRuntimeCallableTools(mode agentruntime.ExecutionMode) ([]agentRuntimeCallableToolFact, error) {
	if mode != agentruntime.ExecutionGuided && mode != agentruntime.ExecutionAutomatic {
		return nil, errors.New("agent runtime callable tool facts are invalid")
	}
	registry, err := newAgentCapabilityRegistry(nil)
	if err != nil {
		return nil, err
	}
	descriptors := registry.Descriptors()
	tools := make([]agentRuntimeCallableToolFact, 0, len(descriptors))
	for _, descriptor := range descriptors {
		policy, found := agentruntime.ToolPolicyForSchema(descriptor.Name, agentruntime.CurrentToolSchemaVersion)
		if !found || policy.RiskLevel != descriptor.RiskLevel || policy.RequiredAccess != descriptor.RequiredAccess {
			return nil, errors.New("agent runtime capability descriptor conflicts with policy")
		}
		tools = append(tools, agentRuntimeCallableToolFact{
			Name: descriptor.Name, ActionVersion: descriptor.ActionVersion, RiskLevel: descriptor.RiskLevel,
			RequiredAccess:   descriptor.RequiredAccess,
			ApprovalRequired: agentruntime.ApprovalRequiredFor(policy, mode),
			ArgumentsSchema:  append(json.RawMessage(nil), descriptor.ArgumentsSchema...),
			ResultSchema:     append(json.RawMessage(nil), descriptor.ResultSchema...),
		})
	}
	return tools, nil
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
	if frozen.ToolSchemaVersion != agentruntime.CurrentToolSchemaVersion {
		return agentRuntimeModelContext{}, errors.New(agentruntime.FailureRuntimeSchemaRetired)
	}
	expected, err := encodeAgentRuntimeModelPrompt(
		scope,
		state,
		frozen.CanvasRevision,
		frozen.CallableModels,
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
	seenRecordIDs := make(map[string]struct{}, len(models))
	previous := ""
	for _, item := range models {
		item.ChannelID = strings.TrimSpace(item.ChannelID)
		item.ModelRecordID = strings.TrimSpace(item.ModelRecordID)
		item.Model = strings.TrimSpace(item.Model)
		item.DisplayName = strings.TrimSpace(item.DisplayName)
		item.BillingMode = strings.TrimSpace(item.BillingMode)
		item.PriceStrategy = strings.TrimSpace(item.PriceStrategy)
		if item.ChannelID == "" || item.ModelRecordID == "" || item.Model == "" || item.DisplayName == "" || !agentRuntimeMediaCapability(item.Capability) || item.BillingMode == "" || item.PriceStrategy == "" {
			return errors.New("agent callable model facts are invalid")
		}
		if _, duplicate := seenRecordIDs[item.ModelRecordID]; duplicate {
			return errors.New("agent callable model facts are invalid")
		}
		seenRecordIDs[item.ModelRecordID] = struct{}{}
		key := item.ChannelID + "\x00" + item.Model
		if _, duplicate := seen[key]; duplicate || (previous != "" && key < previous) {
			return errors.New("agent callable model facts are invalid")
		}
		seen[key] = struct{}{}
		previous = key
		priced := item.UnitPriceMicrocredits > 0
		if item.Capability == "vision" && item.BillingMode == "token_usage" && item.PriceStrategy == "token" && item.UnitPriceMicrocredits == 0 && len(item.PriceTiers) == 0 && item.ProviderCapabilities != nil && item.ProviderCapabilities.SupportsTokenUsageBilling && item.ProviderCapabilities.MaxInputImageTokens > 0 {
			priced = true
		}
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
