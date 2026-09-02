package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const mediaQuoteLifetime = 15 * time.Minute

var ErrCostApprovalQuoteMismatch = errors.New("cost approval quote does not match the current media attempt")

type MediaGenerationCommand struct {
	ArtifactRevisionID   string
	Attempt              int
	TaskType             string
	Operation            string
	Prompt               string
	Capability           string
	ChannelID            string
	ModelKey             string
	ParametersJSON       string
	Quantity             int64
	ProviderCapabilities *PublicProviderCapabilities
}

type MediaGenerationAttempt struct {
	ArtifactRevisionID        string
	Attempt                   int
	TaskID                    string
	BillingIdempotencyKey     string
	TaskType                  string
	Operation                 string
	Prompt                    string
	Capability                string
	ChannelID                 string
	ChannelModelID            string
	ModelKey                  string
	ParametersJSON            string
	ProviderCapabilitiesJSON  string
	Quantity                  int64
	AmountMicrocredits        int64
	PerTaskAmountMicrocredits int64
	PriceVersion              int64
	BillingMode               string
	PricingResolution         string
	PricingInputVariant       string
	BillingQuoteFingerprint   string
	QuoteID                   string
	ApprovalFingerprint       string
	ExpiresAt                 time.Time
	ApprovedAt                time.Time
}

type mediaApprovalFingerprintFacts struct {
	TenantKind                agentruntime.TenantKind `json:"tenantKind"`
	TenantID                  string                  `json:"tenantId"`
	ActorUserID               string                  `json:"actorUserId"`
	DomainProjectID           string                  `json:"domainProjectId"`
	CanvasID                  string                  `json:"canvasId"`
	ThreadID                  string                  `json:"threadId"`
	RunID                     string                  `json:"runId"`
	ArtifactRevisionID        string                  `json:"artifactRevisionId"`
	Attempt                   int                     `json:"attempt"`
	TaskID                    string                  `json:"taskId"`
	BillingIdempotencyKey     string                  `json:"billingIdempotencyKey"`
	TaskType                  string                  `json:"taskType"`
	Operation                 string                  `json:"operation"`
	Prompt                    string                  `json:"prompt"`
	Capability                string                  `json:"capability"`
	ChannelID                 string                  `json:"channelId"`
	ChannelModelID            string                  `json:"channelModelId"`
	ModelKey                  string                  `json:"modelKey"`
	ParametersJSON            string                  `json:"parametersJson"`
	ProviderCapabilitiesJSON  string                  `json:"providerCapabilitiesJson"`
	Quantity                  int64                   `json:"quantity"`
	AmountMicrocredits        int64                   `json:"amountMicrocredits"`
	PerTaskAmountMicrocredits int64                   `json:"perTaskAmountMicrocredits"`
	PriceVersion              int64                   `json:"priceVersion"`
	BillingMode               string                  `json:"billingMode"`
	PricingResolution         string                  `json:"pricingResolution"`
	PricingInputVariant       string                  `json:"pricingInputVariant"`
	BillingQuoteFingerprint   string                  `json:"billingQuoteFingerprint"`
	ExpiresAt                 time.Time               `json:"expiresAt"`
}

func (s *Service) FreezeMediaQuote(scope agentruntime.Scope, command MediaGenerationCommand, now time.Time) (*MediaGenerationAttempt, error) {
	return s.freezeMediaQuoteAt(scope, command, now.UTC().Add(mediaQuoteLifetime))
}

func (s *Service) ApproveMediaAttempt(
	scope agentruntime.Scope,
	frozen MediaGenerationAttempt,
	current MediaGenerationCommand,
	now time.Time,
) (*MediaGenerationAttempt, error) {
	now = now.UTC()
	if !now.Before(frozen.ExpiresAt) {
		return nil, costApprovalQuoteMismatch(errors.New("media quote has expired"))
	}
	if err := validateMediaAttemptIntegrity(scope, frozen); err != nil {
		return nil, costApprovalQuoteMismatch(err)
	}
	recomputed, err := s.freezeMediaQuoteAt(scope, current, frozen.ExpiresAt)
	if err != nil {
		return nil, costApprovalQuoteMismatch(err)
	}
	if recomputed.ApprovalFingerprint != frozen.ApprovalFingerprint || recomputed.QuoteID != frozen.QuoteID {
		return nil, costApprovalQuoteMismatch(errors.New("media parameters, model, capabilities, quantity, or price changed"))
	}
	recomputed.ApprovedAt = now
	return recomputed, nil
}

func (s *Service) EnsureMediaTask(
	ctx context.Context,
	scope agentruntime.Scope,
	attempt MediaGenerationAttempt,
) (*model.Task, *model.BillingOrder, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if attempt.ApprovedAt.IsZero() || !attempt.ApprovedAt.Before(attempt.ExpiresAt) {
		return nil, nil, costApprovalQuoteMismatch(errors.New("media attempt has no timely approval"))
	}
	if err := validateMediaAttemptIntegrity(scope, attempt); err != nil {
		return nil, nil, costApprovalQuoteMismatch(err)
	}
	existing, err := s.repo.TaskForUser(scope.ActorUserID, attempt.TaskID)
	if err == nil {
		return s.validateMediaTaskFacts(scope, attempt, existing)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}
	task, err := s.createTaskWithIdentity(scope.ActorUserID, CreateTaskRequest{
		ProjectID: scope.CanvasID, Type: attempt.TaskType, Operation: attempt.Operation,
		Prompt: attempt.Prompt, QuotePriceVersion: attempt.PriceVersion,
		QuoteFingerprint: attempt.BillingQuoteFingerprint,
	}, taskCreationIdentity{
		TaskID: attempt.TaskID, BillingIdempotencyKey: attempt.BillingIdempotencyKey,
		TypedInputJSON: json.RawMessage(attempt.ParametersJSON), Audience: model.TaskAudienceInternal,
	})
	if err != nil {
		var changed *QuoteChangedError
		if errors.As(err, &changed) {
			return nil, nil, costApprovalQuoteMismatch(err)
		}
		return nil, nil, err
	}
	stored, err := s.repo.TaskForUser(scope.ActorUserID, task.ID)
	if err != nil {
		return nil, nil, err
	}
	return s.validateMediaTaskFacts(scope, attempt, stored)
}

func (s *Service) freezeMediaQuoteAt(
	scope agentruntime.Scope,
	command MediaGenerationCommand,
	expiresAt time.Time,
) (*MediaGenerationAttempt, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	command = normalizeMediaGenerationCommand(command)
	if err := validateMediaGenerationCommand(command); err != nil {
		return nil, err
	}
	parameters, input, err := canonicalMediaParameters(command)
	if err != nil {
		return nil, err
	}
	capabilities, err := canonicalMediaCapabilities(command.ProviderCapabilities)
	if err != nil {
		return nil, err
	}
	channelModel, err := s.requireAccessibleChannelModel(scope.ActorUserID, command.ChannelID, command.ModelKey)
	if err != nil {
		return nil, err
	}
	if normalizeCapability(channelModel.Capability) != command.Capability {
		return nil, errors.New("media command capability conflicts with the callable channel model")
	}
	identity := MediaAttemptIdentity(scope, command)
	virtualTask := model.Task{
		ID: identity, UserID: scope.ActorUserID, ProjectID: scope.CanvasID,
		Type: command.TaskType, Operation: command.Operation, Prompt: command.Prompt,
	}
	order, err := s.taskBillingOrder(scope.ActorUserID, &virtualTask, input)
	if err != nil {
		return nil, err
	}
	quote, err := taskBillingQuoteFromOrder(order, 1)
	if err != nil {
		return nil, err
	}
	if quote.Quantity != command.Quantity {
		return nil, errors.New("media command quantity conflicts with the current billing facts")
	}
	attempt := MediaGenerationAttempt{
		ArtifactRevisionID: command.ArtifactRevisionID, Attempt: command.Attempt,
		TaskID: identity, BillingIdempotencyKey: "agent-media:" + identity,
		TaskType: command.TaskType, Operation: command.Operation, Prompt: command.Prompt,
		Capability: command.Capability, ChannelID: command.ChannelID,
		ChannelModelID: channelModel.ID, ModelKey: command.ModelKey,
		ParametersJSON: string(parameters), ProviderCapabilitiesJSON: string(capabilities),
		Quantity: quote.Quantity, AmountMicrocredits: quote.AmountMicrocredits,
		PerTaskAmountMicrocredits: quote.PerTaskAmountMicrocredits, PriceVersion: quote.PriceVersion,
		BillingMode: quote.BillingMode, PricingResolution: quote.PricingResolution,
		PricingInputVariant:     quote.PricingInputVariant,
		BillingQuoteFingerprint: quote.QuoteFingerprint, ExpiresAt: expiresAt.UTC(),
	}
	attempt.ApprovalFingerprint, err = mediaApprovalFingerprint(scope, attempt)
	if err != nil {
		return nil, err
	}
	attempt.QuoteID = mediaQuoteID(attempt.ApprovalFingerprint)
	return &attempt, nil
}

func normalizeMediaGenerationCommand(command MediaGenerationCommand) MediaGenerationCommand {
	command.ArtifactRevisionID = strings.TrimSpace(command.ArtifactRevisionID)
	command.TaskType = strings.TrimSpace(command.TaskType)
	command.Operation = strings.TrimSpace(command.Operation)
	command.Prompt = strings.TrimSpace(command.Prompt)
	command.Capability = normalizeCapability(command.Capability)
	command.ChannelID = strings.TrimSpace(command.ChannelID)
	command.ModelKey = strings.TrimPrefix(strings.TrimSpace(command.ModelKey), "models/")
	command.ParametersJSON = strings.TrimSpace(command.ParametersJSON)
	return command
}

func validateMediaGenerationCommand(command MediaGenerationCommand) error {
	if command.ArtifactRevisionID == "" || len(command.ArtifactRevisionID) > 120 || command.Attempt < 1 ||
		command.TaskType == "" || command.Operation == "" || command.Prompt == "" ||
		command.ChannelID == "" || command.ModelKey == "" || command.Quantity < 1 ||
		command.ProviderCapabilities == nil {
		return errors.New("media generation command is incomplete")
	}
	if command.Capability != "image" && command.Capability != "video" && command.Capability != "audio" && command.Capability != "vision" {
		return errors.New("media generation capability is invalid")
	}
	if capabilityFromTaskType(command.TaskType) != command.Capability {
		return errors.New("media task type conflicts with capability")
	}
	if strings.TrimSpace(command.ProviderCapabilities.ModelKey) != command.ModelKey ||
		normalizeCapability(command.ProviderCapabilities.Capability) != command.Capability {
		return errors.New("provider capability snapshot conflicts with the media command")
	}
	return nil
}

func canonicalMediaParameters(command MediaGenerationCommand) ([]byte, map[string]any, error) {
	canonical, err := canonicalAgentJSON([]byte(command.ParametersJSON))
	if err != nil || len(canonical) == 0 || canonical[0] != '{' {
		return nil, nil, errors.New("media parameters must be a valid JSON object")
	}
	var input map[string]any
	if err := json.Unmarshal(canonical, &input); err != nil {
		return nil, nil, err
	}
	normalized, err := normalizeTaskInput(input)
	if err != nil {
		return nil, nil, err
	}
	config, _ := normalized["config"].(map[string]any)
	if normalizeCapability(fmt.Sprint(normalized["mode"])) != command.Capability || config == nil ||
		strings.TrimSpace(fmt.Sprint(config["channelId"])) != command.ChannelID ||
		strings.TrimPrefix(strings.TrimSpace(fmt.Sprint(config["model"])), "models/") != command.ModelKey ||
		strings.TrimSpace(fmt.Sprint(normalized["prompt"])) != command.Prompt {
		return nil, nil, errors.New("media parameters conflict with command facts")
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, nil, err
	}
	canonical, err = canonicalAgentJSON(encoded)
	return canonical, normalized, err
}

func canonicalMediaCapabilities(capabilities *PublicProviderCapabilities) ([]byte, error) {
	if capabilities == nil {
		return nil, errors.New("provider capability snapshot is required")
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return nil, err
	}
	return canonicalAgentJSON(encoded)
}

func MediaAttemptIdentity(scope agentruntime.Scope, command MediaGenerationCommand) string {
	facts := strings.Join([]string{
		string(scope.TenantKind), scope.TenantID, scope.ActorUserID, scope.DomainProjectID,
		scope.CanvasID, scope.ThreadID, scope.RunID,
		strings.TrimSpace(command.ArtifactRevisionID), fmt.Sprintf("%d", command.Attempt),
		strings.TrimSpace(command.TaskType), normalizeCapability(command.Capability),
	}, "\x00")
	digest := sha256.Sum256([]byte(facts))
	return "med-" + hex.EncodeToString(digest[:16])
}

func mediaApprovalFingerprint(scope agentruntime.Scope, attempt MediaGenerationAttempt) (string, error) {
	facts := mediaApprovalFingerprintFacts{
		TenantKind: scope.TenantKind, TenantID: scope.TenantID, ActorUserID: scope.ActorUserID,
		DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, ThreadID: scope.ThreadID, RunID: scope.RunID,
		ArtifactRevisionID: attempt.ArtifactRevisionID, Attempt: attempt.Attempt,
		TaskID: attempt.TaskID, BillingIdempotencyKey: attempt.BillingIdempotencyKey,
		TaskType: attempt.TaskType, Operation: attempt.Operation, Prompt: attempt.Prompt,
		Capability: attempt.Capability, ChannelID: attempt.ChannelID,
		ChannelModelID: attempt.ChannelModelID, ModelKey: attempt.ModelKey,
		ParametersJSON: attempt.ParametersJSON, ProviderCapabilitiesJSON: attempt.ProviderCapabilitiesJSON,
		Quantity: attempt.Quantity, AmountMicrocredits: attempt.AmountMicrocredits,
		PerTaskAmountMicrocredits: attempt.PerTaskAmountMicrocredits, PriceVersion: attempt.PriceVersion,
		BillingMode: attempt.BillingMode, PricingResolution: attempt.PricingResolution,
		PricingInputVariant:     attempt.PricingInputVariant,
		BillingQuoteFingerprint: attempt.BillingQuoteFingerprint, ExpiresAt: attempt.ExpiresAt.UTC(),
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func mediaQuoteID(approvalFingerprint string) string {
	if len(approvalFingerprint) < 32 {
		return ""
	}
	return "media-quote-" + approvalFingerprint[:32]
}

func validateMediaAttemptIntegrity(scope agentruntime.Scope, attempt MediaGenerationAttempt) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	var capabilities PublicProviderCapabilities
	decoder := json.NewDecoder(strings.NewReader(attempt.ProviderCapabilitiesJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&capabilities); err != nil {
		return errors.New("media provider capability snapshot is invalid")
	}
	command := normalizeMediaGenerationCommand(MediaGenerationCommand{
		ArtifactRevisionID: attempt.ArtifactRevisionID, Attempt: attempt.Attempt,
		TaskType: attempt.TaskType, Operation: attempt.Operation, Prompt: attempt.Prompt,
		Capability: attempt.Capability, ChannelID: attempt.ChannelID, ModelKey: attempt.ModelKey,
		ParametersJSON: attempt.ParametersJSON, Quantity: attempt.Quantity, ProviderCapabilities: &capabilities,
	})
	if err := validateMediaGenerationCommand(command); err != nil {
		return err
	}
	if attempt.ChannelModelID == "" || attempt.TaskID != MediaAttemptIdentity(scope, command) ||
		attempt.BillingIdempotencyKey != "agent-media:"+attempt.TaskID || attempt.BillingQuoteFingerprint == "" ||
		attempt.ExpiresAt.IsZero() || attempt.ApprovalFingerprint == "" {
		return errors.New("media attempt identity facts are invalid")
	}
	expected, err := mediaApprovalFingerprint(scope, attempt)
	if err != nil {
		return err
	}
	if expected != attempt.ApprovalFingerprint || attempt.QuoteID != mediaQuoteID(expected) {
		return errors.New("media attempt fingerprint is invalid")
	}
	parameters, _, err := canonicalMediaParameters(command)
	if err != nil || !bytes.Equal(parameters, []byte(attempt.ParametersJSON)) {
		return errors.New("media attempt parameters are invalid")
	}
	capabilitiesJSON, err := canonicalMediaCapabilities(&capabilities)
	if err != nil || !bytes.Equal(capabilitiesJSON, []byte(attempt.ProviderCapabilitiesJSON)) {
		return errors.New("media attempt provider capabilities are invalid")
	}
	if _, err := canonicalAgentJSON([]byte(attempt.ParametersJSON)); err != nil {
		return err
	}
	return nil
}

func (s *Service) validateMediaTaskFacts(
	scope agentruntime.Scope,
	attempt MediaGenerationAttempt,
	task *model.Task,
) (*model.Task, *model.BillingOrder, error) {
	if task == nil || task.ID != attempt.TaskID || task.UserID != scope.ActorUserID ||
		task.ProjectID != scope.CanvasID || task.Audience != model.TaskAudienceInternal ||
		task.Type != attempt.TaskType || task.Capability != attempt.Capability || task.Operation != attempt.Operation ||
		task.Prompt != attempt.Prompt || task.BillingOrderID == "" {
		return nil, nil, errors.New("media task identity facts conflict")
	}
	wantInput, err := canonicalAgentJSON([]byte(attempt.ParametersJSON))
	if err != nil {
		return nil, nil, err
	}
	gotInput, err := canonicalAgentJSON([]byte(task.InputJSON))
	if err != nil || !bytes.Equal(gotInput, wantInput) {
		terminalInput := task.Status == model.TaskStatusSucceeded || task.Status == model.TaskStatusCancelled
		publicInput, publicErr := canonicalAgentJSON([]byte(publicTaskInputJSON(attempt.ParametersJSON)))
		if !terminalInput || publicErr != nil || !bytes.Equal(gotInput, publicInput) {
			return nil, nil, errors.New("media task parameters conflict with the approved attempt")
		}
	}
	order, err := s.repo.BillingOrder(task.BillingOrderID)
	if err != nil {
		return nil, nil, err
	}
	if order.UserID != scope.ActorUserID || order.TaskID != task.ID ||
		order.IdempotencyKey != attempt.BillingIdempotencyKey || order.ChannelID != attempt.ChannelID ||
		order.ChannelModelID != attempt.ChannelModelID || order.Model != attempt.ModelKey ||
		order.Capability != attempt.Capability || order.Quantity != attempt.Quantity ||
		order.AmountMicrocredits != attempt.PerTaskAmountMicrocredits ||
		order.PriceVersion != attempt.PriceVersion || order.BillingMode != attempt.BillingMode ||
		order.PricingResolution != attempt.PricingResolution || order.PricingInputVariant != attempt.PricingInputVariant {
		return nil, nil, errors.New("media task billing facts conflict with the approved attempt")
	}
	fingerprint, err := billingOrderQuoteFingerprint(order)
	if err != nil {
		return nil, nil, err
	}
	if fingerprint != attempt.BillingQuoteFingerprint {
		return nil, nil, errors.New("media task billing fingerprint conflicts with the approved attempt")
	}
	return taskForOutput(*task), order, nil
}

func costApprovalQuoteMismatch(cause error) error {
	return errors.Join(ErrCostApprovalQuoteMismatch, cause)
}
