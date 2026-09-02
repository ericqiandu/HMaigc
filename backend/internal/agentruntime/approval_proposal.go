package agentruntime

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

const (
	CurrentApprovalProposalVersion = 1
	DefaultApprovalProposalTTL     = 15 * time.Minute
	approvalProposalSummaryLimit   = 240
	approvalProposalTargetLimit    = 64
	approvalProposalPayloadLimit   = 256 * 1024
)

var (
	ErrApprovalProposalMismatch = errors.New("approval proposal hash mismatch")
	ErrApprovalProposalExpired  = errors.New("approval proposal expired")
)

type ApprovalEffectKind string

const (
	ApprovalEffectCanvasMutation  ApprovalEffectKind = "canvas_mutation"
	ApprovalEffectAssetPublish    ApprovalEffectKind = "asset_publish"
	ApprovalEffectMediaGeneration ApprovalEffectKind = "media_generation"
	ApprovalEffectVisionAnalysis  ApprovalEffectKind = "vision_analysis"
)

type ApprovalScope struct {
	TenantKind      TenantKind `json:"tenantKind"`
	TenantID        string     `json:"tenantId"`
	ActorUserID     string     `json:"actorUserId"`
	DomainProjectID string     `json:"domainProjectId"`
	CanvasID        string     `json:"canvasId"`
	ThreadID        string     `json:"threadId"`
}

type ApprovalEffect struct {
	Kind      ApprovalEffectKind `json:"kind"`
	Summary   string             `json:"summary"`
	TargetIDs []string           `json:"targetIds"`
}

type ApprovalCostQuote struct {
	ModelRecordID      string `json:"modelRecordId"`
	ModelKey           string `json:"modelKey"`
	PriceVersion       int64  `json:"priceVersion"`
	AmountMicrocredits int64  `json:"amountMicrocredits"`
}

type ApprovalProposal struct {
	Version        int                `json:"version"`
	RunID          string             `json:"runId"`
	ToolCallID     string             `json:"toolCallId"`
	ActionVersion  int                `json:"actionVersion"`
	Scope          ApprovalScope      `json:"scope"`
	ToolName       ToolName           `json:"toolName"`
	Arguments      json.RawMessage    `json:"arguments"`
	Effect         ApprovalEffect     `json:"effect"`
	Quote          *ApprovalCostQuote `json:"quote,omitempty"`
	IdempotencyKey string             `json:"idempotencyKey"`
	ExpiresAt      time.Time          `json:"expiresAt"`
}

type ApprovalProposalInput struct {
	Scope          Scope
	ToolCall       ToolCallDecision
	Effect         ApprovalEffect
	Quote          *ApprovalCostQuote
	IdempotencyKey string
	ExpiresAt      time.Time
}

func NewApprovalProposalForTool(
	scope Scope,
	toolCall ToolCallDecision,
	quote *ApprovalCostQuote,
	idempotencyKey string,
	expiresAt time.Time,
) (ApprovalProposal, error) {
	decoded, err := DecodeCapabilityArguments(toolCall.ToolName, toolCall.Arguments)
	if err != nil {
		return ApprovalProposal{}, err
	}
	effect, err := approvalEffectFor(toolCall.ToolName, decoded)
	if err != nil {
		return ApprovalProposal{}, err
	}
	return NewApprovalProposal(ApprovalProposalInput{
		Scope: scope, ToolCall: toolCall, Effect: effect, Quote: quote,
		IdempotencyKey: idempotencyKey, ExpiresAt: expiresAt,
	})
}

func DecodeApprovalProposal(payload json.RawMessage) (ApprovalProposal, error) {
	if len(payload) == 0 || len(payload) > approvalProposalPayloadLimit {
		return ApprovalProposal{}, errors.New("approval proposal payload is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var proposal ApprovalProposal
	if err := decoder.Decode(&proposal); err != nil {
		return ApprovalProposal{}, errors.New("approval proposal payload is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ApprovalProposal{}, errors.New("approval proposal payload contains multiple values")
	}
	normalized, err := normalizeApprovalProposal(proposal)
	if err != nil {
		return ApprovalProposal{}, err
	}
	return normalized, nil
}

func NewApprovalProposal(input ApprovalProposalInput) (ApprovalProposal, error) {
	if err := input.Scope.Validate(); err != nil {
		return ApprovalProposal{}, err
	}
	policy, ok := ToolPolicyFor(input.ToolCall.ToolName)
	if !ok || !ApprovalRequiredFor(policy, ExecutionGuided) {
		return ApprovalProposal{}, errors.New("approval proposal tool is not protected")
	}
	decoded, err := DecodeCapabilityArguments(input.ToolCall.ToolName, input.ToolCall.Arguments)
	if err != nil {
		return ApprovalProposal{}, err
	}
	arguments, err := json.Marshal(decoded)
	if err != nil {
		return ApprovalProposal{}, errors.New("approval proposal arguments are invalid")
	}
	arguments, err = canonicalizeApprovalJSON(arguments)
	if err != nil {
		return ApprovalProposal{}, errors.New("approval proposal arguments are invalid")
	}
	canonicalEffect, err := approvalEffectFor(input.ToolCall.ToolName, decoded)
	if err != nil {
		return ApprovalProposal{}, err
	}
	if !approvalEffectsEqual(canonicalEffect, input.Effect) {
		return ApprovalProposal{}, errors.New("approval proposal effect does not match arguments")
	}
	proposal := ApprovalProposal{
		Version:       CurrentApprovalProposalVersion,
		RunID:         strings.TrimSpace(input.Scope.RunID),
		ToolCallID:    strings.TrimSpace(input.ToolCall.ToolCallID),
		ActionVersion: input.ToolCall.ActionVersion,
		Scope: ApprovalScope{
			TenantKind: input.Scope.TenantKind, TenantID: strings.TrimSpace(input.Scope.TenantID),
			ActorUserID: strings.TrimSpace(input.Scope.ActorUserID), DomainProjectID: strings.TrimSpace(input.Scope.DomainProjectID),
			CanvasID: strings.TrimSpace(input.Scope.CanvasID), ThreadID: strings.TrimSpace(input.Scope.ThreadID),
		},
		ToolName: input.ToolCall.ToolName, Arguments: arguments, Effect: canonicalEffect,
		Quote: cloneApprovalCostQuote(input.Quote), IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), ExpiresAt: input.ExpiresAt.UTC(),
	}
	if err := proposal.validate(); err != nil {
		return ApprovalProposal{}, err
	}
	return proposal, nil
}

func (proposal ApprovalProposal) Hash() (string, error) {
	normalized, err := normalizeApprovalProposal(proposal)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", errors.New("approval proposal is not serializable")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func normalizeApprovalProposal(proposal ApprovalProposal) (ApprovalProposal, error) {
	decoded, err := DecodeCapabilityArguments(proposal.ToolName, proposal.Arguments)
	if err != nil {
		return ApprovalProposal{}, err
	}
	canonicalArguments, err := json.Marshal(decoded)
	if err == nil {
		canonicalArguments, err = canonicalizeApprovalJSON(canonicalArguments)
	}
	if err != nil {
		return ApprovalProposal{}, errors.New("approval proposal arguments are invalid")
	}
	proposal.Arguments = canonicalArguments
	proposal.ExpiresAt = proposal.ExpiresAt.UTC()
	if err := proposal.validate(); err != nil {
		return ApprovalProposal{}, err
	}
	return proposal, nil
}

func ValidateApprovalProposalDecision(proposal ApprovalProposal, proposalHash string, now time.Time) error {
	if now.IsZero() {
		return errors.New("approval decision timestamp is required")
	}
	expectedHash, err := proposal.Hash()
	if err != nil {
		return err
	}
	provided, err := hex.DecodeString(strings.TrimSpace(proposalHash))
	if err != nil || len(provided) != sha256.Size || subtle.ConstantTimeCompare(provided, mustDecodeApprovalHash(expectedHash)) != 1 {
		return ErrApprovalProposalMismatch
	}
	if !now.UTC().Before(proposal.ExpiresAt.UTC()) {
		return ErrApprovalProposalExpired
	}
	if (proposal.ToolName == ToolMediaGenerate || proposal.ToolName == ToolVisionAnalyze) && proposal.Quote == nil {
		return errors.New("approval proposal cost quote is required")
	}
	return nil
}

func (proposal ApprovalProposal) validate() error {
	if proposal.Version != CurrentApprovalProposalVersion || strings.TrimSpace(proposal.RunID) == "" || len(proposal.RunID) > capabilityIdentifierLimit ||
		strings.TrimSpace(proposal.ToolCallID) == "" || len(proposal.ToolCallID) > capabilityIdentifierLimit || proposal.ActionVersion < 1 ||
		strings.TrimSpace(proposal.IdempotencyKey) == "" || len(proposal.IdempotencyKey) > 256 || proposal.ExpiresAt.IsZero() {
		return errors.New("approval proposal identity is invalid")
	}
	if proposal.RunID != strings.TrimSpace(proposal.RunID) || proposal.ToolCallID != strings.TrimSpace(proposal.ToolCallID) || proposal.IdempotencyKey != strings.TrimSpace(proposal.IdempotencyKey) {
		return errors.New("approval proposal identity is not normalized")
	}
	if err := proposal.Scope.validate(proposal.RunID); err != nil {
		return err
	}
	if strings.TrimSpace(proposal.Effect.Summary) == "" || len(proposal.Effect.Summary) > approvalProposalSummaryLimit || len(proposal.Effect.TargetIDs) == 0 || len(proposal.Effect.TargetIDs) > approvalProposalTargetLimit {
		return errors.New("approval proposal effect is invalid")
	}
	policy, ok := ToolPolicyFor(proposal.ToolName)
	if !ok || !ApprovalRequiredFor(policy, ExecutionGuided) {
		return errors.New("approval proposal tool is invalid")
	}
	decoded, err := DecodeCapabilityArguments(proposal.ToolName, proposal.Arguments)
	if err != nil {
		return err
	}
	canonicalArguments, err := json.Marshal(decoded)
	if err == nil {
		canonicalArguments, err = canonicalizeApprovalJSON(canonicalArguments)
	}
	if err != nil || string(canonicalArguments) != string(proposal.Arguments) {
		return errors.New("approval proposal arguments are not canonical")
	}
	expectedEffect, err := approvalEffectFor(proposal.ToolName, decoded)
	if err != nil || !approvalEffectsEqual(expectedEffect, proposal.Effect) {
		return errors.New("approval proposal effect is invalid")
	}
	switch arguments := decoded.(type) {
	case MediaGenerateArguments:
		if proposal.Quote == nil {
			return errors.New("approval proposal cost quote is required")
		}
		if err := proposal.Quote.validate(); err != nil {
			return err
		}
		if proposal.Quote.ModelRecordID != arguments.ModelRecordID || proposal.Quote.ModelKey != arguments.ModelKey {
			return errors.New("approval cost quote does not match media model")
		}
	case VisionAnalyzeArguments:
		if proposal.Quote == nil {
			return errors.New("approval proposal cost quote is required")
		}
		if err := proposal.Quote.validate(); err != nil {
			return err
		}
		if proposal.Quote.ModelRecordID != arguments.ModelRecordID || proposal.Quote.ModelKey != arguments.ModelKey {
			return errors.New("approval cost quote does not match vision model")
		}
	default:
		if proposal.Quote != nil {
			return errors.New("approval cost quote is only valid for cost-bearing tools")
		}
	}
	return nil
}

func (scope ApprovalScope) validate(runID string) error {
	runtimeScope := Scope{
		TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: runID,
		Access: AccessGrant{Level: AccessEditor, SubscriptionActive: true},
	}
	return runtimeScope.Validate()
}

func (quote ApprovalCostQuote) validate() error {
	if strings.TrimSpace(quote.ModelRecordID) == "" || len(quote.ModelRecordID) > capabilityIdentifierLimit ||
		strings.TrimSpace(quote.ModelKey) == "" || len(quote.ModelKey) > capabilityIdentifierLimit || quote.PriceVersion < 1 || quote.AmountMicrocredits <= 0 {
		return errors.New("approval cost quote is invalid")
	}
	if quote.ModelRecordID != strings.TrimSpace(quote.ModelRecordID) || quote.ModelKey != strings.TrimSpace(quote.ModelKey) {
		return errors.New("approval cost quote is not normalized")
	}
	return nil
}

func cloneApprovalCostQuote(quote *ApprovalCostQuote) *ApprovalCostQuote {
	if quote == nil {
		return nil
	}
	cloned := *quote
	return &cloned
}

func approvalEffectFor(toolName ToolName, decoded CapabilityArguments) (ApprovalEffect, error) {
	switch arguments := decoded.(type) {
	case CanvasApplyOpsArguments:
		return ApprovalEffect{
			Kind: ApprovalEffectCanvasMutation, Summary: fmt.Sprintf("执行 %d 项画布操作", len(arguments.Operations)),
			TargetIDs: []string{arguments.CanvasID},
		}, nil
	case AssetsPublishArguments:
		return ApprovalEffect{
			Kind: ApprovalEffectAssetPublish, Summary: "发布资源 " + arguments.ResourceID,
			TargetIDs: []string{arguments.ResourceID, arguments.DomainProjectID},
		}, nil
	case MediaGenerateArguments:
		return ApprovalEffect{
			Kind: ApprovalEffectMediaGeneration, Summary: "生成 " + string(arguments.MediaKind) + " 媒体",
			TargetIDs: []string{arguments.TargetCanvasNodeID},
		}, nil
	case VisionAnalyzeArguments:
		return ApprovalEffect{
			Kind: ApprovalEffectVisionAnalysis, Summary: fmt.Sprintf("理解 %d 张图片", len(arguments.SourceResourceIDs)),
			TargetIDs: append([]string(nil), arguments.SourceResourceIDs...),
		}, nil
	default:
		return ApprovalEffect{}, errors.New("approval proposal arguments are not protected")
	}
}

func approvalEffectsEqual(left ApprovalEffect, right ApprovalEffect) bool {
	return left.Kind == right.Kind && left.Summary == right.Summary && slices.Equal(left.TargetIDs, right.TargetIDs)
}

func mustDecodeApprovalHash(value string) []byte {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		panic("approval hash invariant violated")
	}
	return decoded
}

func canonicalizeApprovalJSON(value json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return nil, errors.New("approval JSON is invalid")
	}
	switch trimmed[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return nil, err
		}
		for key, raw := range object {
			canonical, err := canonicalizeApprovalJSON(raw)
			if err != nil {
				return nil, err
			}
			object[key] = canonical
		}
		return json.Marshal(object)
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, err
		}
		for index, raw := range items {
			canonical, err := canonicalizeApprovalJSON(raw)
			if err != nil {
				return nil, err
			}
			items[index] = canonical
		}
		return json.Marshal(items)
	default:
		var compact bytes.Buffer
		if err := json.Compact(&compact, trimmed); err != nil {
			return nil, err
		}
		return compact.Bytes(), nil
	}
}
