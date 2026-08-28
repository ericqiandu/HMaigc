package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const (
	agentVisualAnalysisTaskType  = "agent_visual_analysis"
	agentVisualAnalysisOperation = "visual_analysis"
)

func agentVisualAnalysisOperationForRun(runID string) string {
	return agentVisualAnalysisOperation + ":" + strings.TrimSpace(runID)
}

func agentVisualAnalysisRunID(operation string) (string, bool) {
	prefix := agentVisualAnalysisOperation + ":"
	operation = strings.TrimSpace(operation)
	if !strings.HasPrefix(operation, prefix) || len(operation) > 96 {
		return "", false
	}
	runID := strings.TrimSpace(strings.TrimPrefix(operation, prefix))
	return runID, runID != ""
}

var (
	errAgentVisualArgumentsInvalid    = errors.New("agent visual analysis arguments are invalid")
	errAgentVisualSourceRevisionStale = errors.New("agent visual analysis source revision is stale")
	errAgentVisualInputUnavailable    = errors.New("agent visual analysis input is unavailable")
	errAgentVisualModelUnavailable    = errors.New("agent visual analysis model is unavailable")
)

func agentVisualAnalysisFailureDetails(err error) (string, agentruntime.ToolFailureClass, bool) {
	switch {
	case errors.Is(err, errAgentVisualArgumentsInvalid):
		return "visual_analysis_invalid", agentruntime.ToolFailureAgentRepairable, true
	case errors.Is(err, errAgentVisualSourceRevisionStale):
		return "visual_evidence_stale", agentruntime.ToolFailureAgentRepairable, true
	case errors.Is(err, errAgentVisualInputUnavailable):
		return "visual_analysis_input_unavailable", agentruntime.ToolFailureAgentRepairable, true
	case errors.Is(err, errAgentVisualModelUnavailable):
		return "visual_analysis_model_unavailable", agentruntime.ToolFailureAgentRepairable, true
	default:
		return "", "", false
	}
}

type agentVisualAnalysisArguments struct {
	SourceRevision       agentruntime.ArtifactRevisionRef      `json:"sourceRevision"`
	ResourceID           string                                `json:"resourceId"`
	VisionModel          agentruntime.GenerationModelSelection `json:"visionModel"`
	VisionModelRecordID  string                                `json:"visionModelRecordId"`
	OutputArtifactID     string                                `json:"outputArtifactId"`
	OutputArtifactKey    string                                `json:"outputArtifactKey"`
	ExpectedOutputSchema string                                `json:"expectedOutputSchema"`
	ExpectedDelivery     agentruntime.ExpectedDelivery         `json:"expectedDelivery"`
	RequestIdentity      string                                `json:"requestIdentity"`
	Commercial           MediaGenerationAttempt                `json:"commercial"`
}

type agentVisualAnalysisExecutionFacts struct {
	SourceRevision       agentruntime.ArtifactRevisionRef      `json:"sourceRevision"`
	ResourceID           string                                `json:"resourceId"`
	VisionModel          agentruntime.GenerationModelSelection `json:"visionModel"`
	VisionModelRecordID  string                                `json:"visionModelRecordId"`
	OutputArtifactID     string                                `json:"outputArtifactId"`
	OutputArtifactKey    string                                `json:"outputArtifactKey"`
	ExpectedOutputSchema string                                `json:"expectedOutputSchema"`
	ExpectedDelivery     agentruntime.ExpectedDelivery         `json:"expectedDelivery"`
	RequestIdentity      string                                `json:"requestIdentity"`
}

type agentVisualAnalysisTaskInput struct {
	Mode     string                           `json:"mode"`
	Prompt   string                           `json:"prompt"`
	Scope    agentruntime.Scope               `json:"scope"`
	Config   providerConfig                   `json:"config"`
	Analysis agentVisualAnalysisExecutionFacts `json:"analysis"`
}

type agentVisualAnalysisTaskResult struct {
	ArtifactID       string `json:"artifactId"`
	ArtifactRevision string `json:"artifactRevisionId"`
	Revision         int64  `json:"revision"`
	Schema           string `json:"schema"`
	ProviderRequest  string `json:"providerRequestId,omitempty"`
}

func agentVisualAnalysisExecution(arguments agentVisualAnalysisArguments) agentVisualAnalysisExecutionFacts {
	return agentVisualAnalysisExecutionFacts{
		SourceRevision: arguments.SourceRevision, ResourceID: arguments.ResourceID,
		VisionModel: arguments.VisionModel, VisionModelRecordID: arguments.VisionModelRecordID,
		OutputArtifactID: arguments.OutputArtifactID, OutputArtifactKey: arguments.OutputArtifactKey,
		ExpectedOutputSchema: arguments.ExpectedOutputSchema, ExpectedDelivery: arguments.ExpectedDelivery,
		RequestIdentity: arguments.RequestIdentity,
	}
}

func validateAgentVisualAnalysisExecution(arguments agentVisualAnalysisExecutionFacts) error {
	if arguments.SourceRevision.Validate() != nil || strings.TrimSpace(arguments.ResourceID) == "" ||
		strings.TrimSpace(arguments.VisionModel.ChannelID) == "" || strings.TrimSpace(arguments.VisionModel.Model) == "" ||
		strings.TrimSpace(arguments.VisionModelRecordID) == "" || strings.TrimSpace(arguments.OutputArtifactID) == "" ||
		strings.TrimSpace(arguments.OutputArtifactKey) == "" || arguments.ExpectedOutputSchema != agentruntime.ArtifactSchemaVisualEvidenceV1 ||
		arguments.ExpectedDelivery.Validate() != nil || strings.TrimSpace(arguments.RequestIdentity) == "" {
		return errAgentVisualArgumentsInvalid
	}
	return nil
}

func agentVisualProviderCapabilities(configured model.ChannelModel) *PublicProviderCapabilities {
	return &PublicProviderCapabilities{
		ProviderFamily: "chat_completion", ModelKey: configured.ModelKey,
		DisplayName: configured.DisplayName, UpstreamMode: configured.ModelKey,
		Capability: "vision", Resolutions: []string{}, ResolutionPixels: map[string]int64{},
		InputVariants: []string{}, ReferenceVideoResolutions: []string{}, GeneratedAudioResolutions: []string{},
		Ratios: []string{}, Qualities: []string{}, OutputCounts: []int{}, Durations: []int{},
		GenerationModes: []string{"vision"}, AdaptiveRatioModes: []string{},
		RequiredAdaptiveRatioModes: []string{}, Tools: []string{}, SupportsTokenUsageBilling: true,
	}
}

func agentVisualAnalysisMediaCommand(
	scope agentruntime.Scope,
	arguments agentVisualAnalysisArguments,
	capabilities *PublicProviderCapabilities,
) (MediaGenerationCommand, error) {
	input := agentVisualAnalysisTaskInput{
		Mode: "vision", Prompt: agentVisualAnalysisPrompt, Scope: scope,
		Config: providerConfig{ChannelID: arguments.VisionModel.ChannelID, Model: arguments.VisionModel.Model},
		Analysis: agentVisualAnalysisExecution(arguments),
	}
	parameters, err := json.Marshal(input)
	if err != nil {
		return MediaGenerationCommand{}, err
	}
	return MediaGenerationCommand{
		ArtifactRevisionID: arguments.OutputArtifactID, Attempt: 1,
		TaskType: agentVisualAnalysisTaskType, Operation: agentVisualAnalysisOperationForRun(scope.RunID),
		Prompt: agentVisualAnalysisPrompt, Capability: "vision",
		ChannelID: arguments.VisionModel.ChannelID, ModelKey: arguments.VisionModel.Model,
		ParametersJSON: string(parameters), Quantity: 1, ProviderCapabilities: capabilities,
	}, nil
}

const agentVisualAnalysisPrompt = "分析已冻结的图片输入并生成严格的 visual_evidence.v1 结构化视觉证据"

const agentVisualAnalysisSystemPrompt = `你是视觉证据分析器。你只能分析随请求提供的图片，并且只能返回一个符合 visual_evidence.v1 的 JSON 对象，禁止 Markdown、额外文本和 reasoning_content。
所有字段都必须存在；数组即使为空也必须返回空数组。sourceRevision、visionModelRecordId、requestIdentity 必须逐字复用用户消息中的冻结事实。只能记录图片中可观察到的角色外观、身份证据、场景、道具、空间关系、镜头语言、动作状态、OCR、未知项和冲突；不得猜测不可见事实。confidenceBasisPoints 必须为 0 到 10000 的整数。`

func (s *Service) freezeAgentVisualAnalysisDecisionArguments(
	scope agentruntime.Scope,
	configuration agentruntime.RunConfiguration,
	callableModels []agentRuntimeCallableModelFact,
	call *agentruntime.ToolCallDecision,
) ([]byte, error) {
	if call == nil || call.ToolName != agentruntime.ToolVisionAnalyze {
		return nil, errAgentVisualArgumentsInvalid
	}
	proposal, err := decodeVisionAnalyzeArguments(call.Arguments)
	if err != nil || !proposal.ExpectedDelivery.Equal(call.ExpectedDelivery) {
		return nil, errAgentVisualArgumentsInvalid
	}
	return s.freezeAgentVisualAnalysisArguments(
		scope,
		configuration,
		callableModels,
		call.Arguments,
		call.ToolCallID,
		call.ActionVersion,
	)
}

func (s *Service) freezeAgentVisualAnalysisArguments(
	scope agentruntime.Scope,
	configuration agentruntime.RunConfiguration,
	callableModels []agentRuntimeCallableModelFact,
	payload []byte,
	toolCallID string,
	actionVersion int,
) ([]byte, error) {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" || actionVersion < 1 {
		return nil, errAgentVisualArgumentsInvalid
	}
	proposal, err := decodeVisionAnalyzeArguments(payload)
	if err != nil {
		return nil, errors.Join(errAgentVisualArgumentsInvalid, err)
	}
	if configuration.GenerationModels.Vision == nil {
		return nil, errAgentVisualModelUnavailable
	}
	selection := *configuration.GenerationModels.Vision
	callable, found := exactCallableVisualModel(callableModels, selection)
	if !found {
		return nil, errAgentVisualModelUnavailable
	}
	configured, err := s.repo.ChannelModelByKey(selection.ChannelID, selection.Model)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errAgentVisualModelUnavailable
		}
		return nil, err
	}
	if !matchesFrozenVisualModel(*configured, callable) {
		return nil, errAgentVisualModelUnavailable
	}

	sourceRef := proposal.InputRevisions[0]
	source, err := s.repo.ArtifactRevisionForArtifactInScope(scope, sourceRef.ArtifactID, sourceRef.RevisionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errAgentVisualInputUnavailable
		}
		return nil, err
	}
	head, err := s.repo.ArtifactHeadRevisionForScope(scope, sourceRef.ArtifactID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errAgentVisualInputUnavailable
		}
		return nil, err
	}
	if source.LifecycleStatus == model.AgentArtifactRevisionStale || head.ID != source.ID {
		return nil, errAgentVisualSourceRevisionStale
	}
	resourceID := proposal.ResourceIDs[0]
	if source.ResourceID == "" || source.ResourceID != resourceID {
		return nil, errAgentVisualInputUnavailable
	}
	resource, err := s.productionResourceForScope(scope, resourceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errAgentVisualInputUnavailable
		}
		return nil, err
	}
	if !visualAnalysisResourceReady(resource) {
		return nil, errAgentVisualInputUnavailable
	}

	digest := visualAnalysisIdentity(scope, sourceRef, resourceID, configured.ID, toolCallID, actionVersion)
	frozen := agentVisualAnalysisArguments{
		SourceRevision:       sourceRef,
		ResourceID:           resourceID,
		VisionModel:          selection,
		VisionModelRecordID:  configured.ID,
		OutputArtifactID:     "visual-evidence-" + digest[:32],
		OutputArtifactKey:    "visual-evidence-" + digest[:32],
		ExpectedOutputSchema: proposal.ExpectedOutputSchema,
		ExpectedDelivery:     proposal.ExpectedDelivery,
		RequestIdentity:      "visual-analysis:" + digest,
	}
	command, err := agentVisualAnalysisMediaCommand(scope, frozen, agentVisualProviderCapabilities(*configured))
	if err != nil {
		return nil, err
	}
	commercial, err := s.FreezeMediaQuote(scope, command, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	frozen.Commercial = *commercial
	encoded, err := json.Marshal(frozen)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func (s *Service) ensureAgentVisualAnalysisTask(
	scope agentruntime.Scope,
	record *model.AgentToolCall,
	arguments agentVisualAnalysisArguments,
) (*model.Task, *model.BillingOrder, error) {
	if err := scope.Validate(); err != nil || record == nil || record.Status != agentruntime.ToolCallRunning ||
		strings.TrimSpace(record.IdempotencyKey) == "" || validateAgentVisualAnalysisArguments(arguments) != nil {
		return nil, nil, errAgentVisualArgumentsInvalid
	}
	if record.ApprovalDecision != agentruntime.ToolApprovalApproved || record.ApprovalDecidedAt == nil {
		return nil, nil, costApprovalQuoteMismatch(errors.New("visual analysis cost approval is missing"))
	}
	configured, err := s.repo.ChannelModelByKey(arguments.VisionModel.ChannelID, arguments.VisionModel.Model)
	if err != nil {
		return nil, nil, costApprovalQuoteMismatch(err)
	}
	command, err := agentVisualAnalysisMediaCommand(scope, arguments, agentVisualProviderCapabilities(*configured))
	if err != nil {
		return nil, nil, err
	}
	approved, err := s.ApproveMediaAttempt(scope, arguments.Commercial, command, record.ApprovalDecidedAt.UTC())
	if err != nil {
		return nil, nil, err
	}
	_, order, err := s.EnsureMediaTask(context.Background(), scope, *approved)
	if err != nil {
		return nil, nil, err
	}
	storedTask, err := s.repo.TaskForUser(scope.ActorUserID, approved.TaskID)
	if err != nil {
		return nil, nil, err
	}
	if err := validateAgentVisualAnalysisTask(scope, *storedTask, *order, arguments); err != nil {
		return nil, nil, err
	}
	return storedTask, order, nil
}

func validateAgentVisualAnalysisTask(
	scope agentruntime.Scope,
	task model.Task,
	order model.BillingOrder,
	arguments agentVisualAnalysisArguments,
) error {
	if task.UserID != scope.ActorUserID || task.ProjectID != scope.CanvasID || task.Audience != model.TaskAudienceInternal ||
		task.Type != agentVisualAnalysisTaskType || task.Operation != agentVisualAnalysisOperationForRun(scope.RunID) ||
		task.Prompt != agentVisualAnalysisPrompt || task.Model != arguments.VisionModel.Model || task.Capability != "vision" ||
		task.BillingOrderID == "" || task.ProviderAccountID == "" || task.ProviderEndpointVersionID == "" ||
		task.ProviderCredentialVersionID == "" {
		return errors.New("agent visual analysis task facts conflict")
	}
	if err := validateAgentVisualAnalysisTaskInput(scope, task, arguments); err != nil {
		return errors.New("agent visual analysis task input facts conflict")
	}
	quoteFingerprint, quoteErr := billingOrderQuoteFingerprint(&order)
	if quoteErr != nil || quoteFingerprint != arguments.Commercial.BillingQuoteFingerprint ||
		order.UserID != scope.ActorUserID || order.TaskID != task.ID || order.ChannelID != arguments.VisionModel.ChannelID ||
		order.ChannelModelID != arguments.VisionModelRecordID || order.Model != arguments.VisionModel.Model ||
		order.Capability != "vision" || order.Scene != agentVisualAnalysisOperationForRun(scope.RunID) || order.BillingMode != arguments.Commercial.BillingMode ||
		order.Quantity != 1 || order.AmountMicrocredits != arguments.Commercial.AmountMicrocredits ||
		order.PriceVersion != arguments.Commercial.PriceVersion || order.IdempotencyKey != arguments.Commercial.BillingIdempotencyKey {
		return errors.New("agent visual analysis billing facts conflict")
	}
	return nil
}

func validateAgentVisualAnalysisTaskInput(
	scope agentruntime.Scope,
	task model.Task,
	arguments agentVisualAnalysisArguments,
) error {
	input, err := decodeAgentVisualAnalysisTaskInput(task.InputJSON)
	if err == nil {
		if input.Mode != "vision" || input.Prompt != agentVisualAnalysisPrompt || input.Scope != scope ||
			input.Config.ChannelID != arguments.VisionModel.ChannelID || input.Config.Model != arguments.VisionModel.Model ||
			!equalAgentVisualAnalysisExecutionFacts(input.Analysis, agentVisualAnalysisExecution(arguments)) {
			return errAgentVisualArgumentsInvalid
		}
		return nil
	}
	if task.Status != model.TaskStatusSucceeded && task.Status != model.TaskStatusCancelled {
		return errAgentVisualArgumentsInvalid
	}
	// Successful tasks intentionally discard private execution input. The frozen tool
	// call, billing order and Artifact Ledger remain the authoritative recovery facts.
	if task.InputJSON != publicTaskInputJSON(`{"mode":"vision"}`) {
		return errAgentVisualArgumentsInvalid
	}
	return nil
}

func decodeAgentVisualAnalysisTaskInput(raw string) (agentVisualAnalysisTaskInput, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var input agentVisualAnalysisTaskInput
	if err := decoder.Decode(&input); err != nil {
		return agentVisualAnalysisTaskInput{}, errAgentVisualArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || input.Mode != "vision" ||
		input.Prompt != agentVisualAnalysisPrompt || input.Scope.Validate() != nil || validateAgentVisualAnalysisExecution(input.Analysis) != nil {
		return agentVisualAnalysisTaskInput{}, errAgentVisualArgumentsInvalid
	}
	return input, nil
}

func validateAgentVisualAnalysisArguments(arguments agentVisualAnalysisArguments) error {
	if arguments.SourceRevision.Validate() != nil || strings.TrimSpace(arguments.ResourceID) == "" ||
		strings.TrimSpace(arguments.VisionModel.ChannelID) == "" || strings.TrimSpace(arguments.VisionModel.Model) == "" ||
		strings.TrimSpace(arguments.VisionModelRecordID) == "" || strings.TrimSpace(arguments.OutputArtifactID) == "" ||
		strings.TrimSpace(arguments.OutputArtifactKey) == "" || arguments.ExpectedOutputSchema != agentruntime.ArtifactSchemaVisualEvidenceV1 ||
		arguments.ExpectedDelivery.Validate() != nil || strings.TrimSpace(arguments.RequestIdentity) == "" ||
		arguments.Commercial.ArtifactRevisionID != arguments.OutputArtifactID || arguments.Commercial.Capability != "vision" ||
		arguments.Commercial.ChannelID != arguments.VisionModel.ChannelID || arguments.Commercial.ModelKey != arguments.VisionModel.Model ||
		arguments.Commercial.ChannelModelID != arguments.VisionModelRecordID || arguments.Commercial.QuoteID == "" ||
		arguments.Commercial.ApprovalFingerprint == "" || arguments.Commercial.ExpiresAt.IsZero() {
		return errAgentVisualArgumentsInvalid
	}
	return nil
}

func equalAgentVisualAnalysisExecutionFacts(left agentVisualAnalysisExecutionFacts, right agentVisualAnalysisExecutionFacts) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func decodeFrozenAgentVisualAnalysisArguments(raw json.RawMessage) (agentVisualAnalysisArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentVisualAnalysisArguments
	if err := decoder.Decode(&arguments); err != nil {
		return agentVisualAnalysisArguments{}, errAgentVisualArgumentsInvalid
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) || validateAgentVisualAnalysisArguments(arguments) != nil {
		return agentVisualAnalysisArguments{}, errAgentVisualArgumentsInvalid
	}
	return arguments, nil
}

func exactCallableVisualModel(models []agentRuntimeCallableModelFact, selection agentruntime.GenerationModelSelection) (agentRuntimeCallableModelFact, bool) {
	for _, item := range models {
		if item.ChannelID == selection.ChannelID && item.Model == selection.Model && item.Capability == "vision" {
			return item, true
		}
	}
	return agentRuntimeCallableModelFact{}, false
}

func matchesFrozenVisualModel(configured model.ChannelModel, callable agentRuntimeCallableModelFact) bool {
	return configured.ID != "" && configured.Enabled && configured.PriceConfigured && configured.Capability == "vision" &&
		configured.ChannelID == callable.ChannelID && configured.ModelKey == callable.Model && configured.DisplayName == callable.DisplayName &&
		configured.BillingMode == callable.BillingMode && configured.PriceStrategy == callable.PriceStrategy &&
		configured.UnitPriceMicrocredits == callable.UnitPriceMicrocredits
}

func visualAnalysisResourceReady(resource *model.Resource) bool {
	return resource != nil && resource.Status == model.ResourceStatusReady && resource.Kind == "image" &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(resource.MimeType)), "image/") &&
		strings.TrimSpace(resource.Provider) != "" && resource.Provider != "local" &&
		strings.TrimSpace(resource.Endpoint) != "" && strings.TrimSpace(resource.Bucket) != "" &&
		strings.TrimSpace(resource.ObjectKey) != ""
}

func visualAnalysisIdentity(
	scope agentruntime.Scope,
	source agentruntime.ArtifactRevisionRef,
	resourceID string,
	modelRecordID string,
	toolCallID string,
	actionVersion int,
) string {
	facts := []string{
		string(scope.TenantKind), scope.TenantID, scope.ActorUserID, scope.DomainProjectID,
		scope.CanvasID, scope.ThreadID, scope.RunID, source.ArtifactID, source.RevisionID,
		resourceID, modelRecordID, agentruntime.ArtifactSchemaVisualEvidenceV1,
		strings.TrimSpace(toolCallID), strconv.Itoa(actionVersion),
	}
	digest := sha256.Sum256([]byte(strings.Join(facts, "\x00")))
	return hex.EncodeToString(digest[:])
}
