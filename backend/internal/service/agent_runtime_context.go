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

type agentRuntimeProductionPlanFact struct {
	PlanKey          string                             `json:"planKey"`
	PlanVersion      int                                `json:"planVersion"`
	Title            string                             `json:"title"`
	TargetDurationMS int                                `json:"targetDurationMs"`
	Script           string                             `json:"script"`
	References       []agentruntime.ReferenceAssetDraft `json:"references"`
	Shots            []agentruntime.ShotPlanDraft       `json:"shots"`
	Artifacts        []agentProductionArtifactResult    `json:"artifacts"`
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
	productionPlan, err := s.agentRuntimeProductionPlanFact(scope)
	if err != nil {
		return "", err
	}
	return encodeAgentRuntimeModelPrompt(scope, state, canvas.Revision, models, productionPlan)
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
		Artifacts: make([]agentProductionArtifactResult, 0, len(record.Artifacts)),
	}
	for _, artifact := range record.Artifacts {
		fact.Artifacts = append(fact.Artifacts, agentProductionArtifactResult{
			ArtifactID: artifact.ID, Kind: artifact.Kind, ReferenceKey: artifact.ReferenceKey, ShotKey: artifact.ShotKey, Status: artifact.Status,
		})
	}
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
				ProviderCapabilities:  item.ProviderCapabilities,
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

func encodeAgentRuntimeModelPrompt(scope agentruntime.Scope, state agentruntime.RuntimeState, canvasRevision int64, models []agentRuntimeCallableModelFact, productionPlan *agentRuntimeProductionPlanFact) (string, error) {
	context := agentRuntimeModelContext{
		RunID: scope.RunID, CanvasID: scope.CanvasID, CanvasRevision: canvasRevision, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		UserMessage: state.UserMessage, ExpectedDelivery: state.ExpectedDelivery,
		Verification: state.Verification, LastToolResult: state.LastToolResult, DecisionFeedback: state.DecisionFeedback, PreviousMessage: state.FinalMessage,
		Configuration: promptAgentRuntimeConfiguration(state), LoadedSkillDirs: append([]string(nil), state.LoadedSkillDirs...), CallableModels: models,
		ClarificationHistory: append([]agentruntime.CompletedClarification(nil), state.ClarificationHistory...), ProductionPlan: productionPlan,
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return "", err
	}
	return agentRuntimeModelPromptPrefix + string(encoded), nil
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
	expected, err := encodeAgentRuntimeModelPrompt(scope, state, frozen.CanvasRevision, frozen.CallableModels, frozen.ProductionPlan)
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
		for _, tier := range item.PriceTiers {
			if (strings.TrimSpace(tier.Resolution) == "" && strings.TrimSpace(tier.InputVariant) == "") || tier.UnitPriceMicrocredits <= 0 {
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
	case "image", "video", "audio":
		return true
	default:
		return false
	}
}
