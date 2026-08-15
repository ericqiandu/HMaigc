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

const agentRuntimeModelPromptPrefix = "以下 JSON 是本轮唯一可信的运行事实。请自主决定直接交付或调用一个可用工具，并严格按系统约定返回一个 JSON 对象：\n"

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

func (s *Service) agentRuntimeModelPrompt(scope agentruntime.Scope, state agentruntime.RuntimeState) (string, error) {
	models, err := s.agentRuntimeCallableModels(scope.ActorUserID)
	if err != nil {
		return "", err
	}
	return encodeAgentRuntimeModelPrompt(scope, state, models)
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

func encodeAgentRuntimeModelPrompt(scope agentruntime.Scope, state agentruntime.RuntimeState, models []agentRuntimeCallableModelFact) (string, error) {
	context := agentRuntimeModelContext{
		RunID: scope.RunID, CanvasID: scope.CanvasID, StepNumber: state.StepNumber, MaxSteps: state.MaxSteps,
		UserMessage: state.UserMessage, ExpectedDelivery: state.ExpectedDelivery,
		Verification: state.Verification, LastToolResult: state.LastToolResult, PreviousMessage: state.FinalMessage,
		CallableModels: models,
	}
	encoded, err := json.Marshal(context)
	if err != nil {
		return "", err
	}
	return agentRuntimeModelPromptPrefix + string(encoded), nil
}

func validateFrozenAgentRuntimeModelPrompt(scope agentruntime.Scope, state agentruntime.RuntimeState, prompt string) error {
	if !strings.HasPrefix(prompt, agentRuntimeModelPromptPrefix) {
		return errors.New("agent runtime model prompt facts conflict")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(strings.TrimPrefix(prompt, agentRuntimeModelPromptPrefix))))
	decoder.DisallowUnknownFields()
	var frozen agentRuntimeModelContext
	if err := decoder.Decode(&frozen); err != nil {
		return errors.New("agent runtime model prompt facts conflict")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("agent runtime model prompt facts conflict")
	}
	if err := validateAgentRuntimeCallableModels(frozen.CallableModels); err != nil {
		return err
	}
	expected, err := encodeAgentRuntimeModelPrompt(scope, state, frozen.CallableModels)
	if err != nil {
		return err
	}
	if prompt != expected {
		return errors.New("agent runtime model prompt facts conflict")
	}
	return nil
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
