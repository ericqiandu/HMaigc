package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	imagePromptRequestVersion               = "image-prompt-request/v1"
	imagePromptResponseVersion              = "image-prompt/v1"
	imagePromptSpecialistName               = "image_prompt_specialist"
	imagePromptCommandPortraitTexture       = "portraitTexture"
	imagePromptSpecialistPath               = "/internal/v1/specialists/image-prompt"
	imagePromptSignatureVersion             = "tapcanvas-internal-request/v1"
	imagePromptMaxResponseBytes       int64 = 1_000_000

	imagePromptHeaderServiceID      = "x-tapcanvas-service-id"
	imagePromptHeaderTimestamp      = "x-tapcanvas-timestamp"
	imagePromptHeaderNonce          = "x-tapcanvas-nonce"
	imagePromptHeaderBodySHA256     = "x-tapcanvas-body-sha256"
	imagePromptHeaderIdempotencyKey = "x-tapcanvas-idempotency-key"
	imagePromptHeaderSignature      = "x-tapcanvas-signature"
	imagePromptHeaderResponseDigest = "x-tapcanvas-response-digest"
)

type imagePromptSpecialistErrorCode string

const (
	imagePromptErrorAuthInvalid            imagePromptSpecialistErrorCode = "specialist_auth_invalid"
	imagePromptErrorReplayRejected         imagePromptSpecialistErrorCode = "specialist_replay_rejected"
	imagePromptErrorRequestInvalid         imagePromptSpecialistErrorCode = "specialist_request_invalid"
	imagePromptErrorEvidenceInvalid        imagePromptSpecialistErrorCode = "specialist_evidence_invalid"
	imagePromptErrorIdempotencyConflict    imagePromptSpecialistErrorCode = "specialist_idempotency_conflict"
	imagePromptErrorModelBindingInvalid    imagePromptSpecialistErrorCode = "specialist_model_binding_invalid"
	imagePromptErrorUnavailable            imagePromptSpecialistErrorCode = "specialist_unavailable"
	imagePromptErrorTimeout                imagePromptSpecialistErrorCode = "specialist_timeout"
	imagePromptErrorOutputInvalid          imagePromptSpecialistErrorCode = "specialist_output_invalid"
	imagePromptErrorModelInheritanceFailed imagePromptSpecialistErrorCode = "specialist_model_inheritance_failed"
)

var imagePromptSpecialistErrorCodes = map[imagePromptSpecialistErrorCode]struct{}{
	imagePromptErrorAuthInvalid:            {},
	imagePromptErrorReplayRejected:         {},
	imagePromptErrorRequestInvalid:         {},
	imagePromptErrorEvidenceInvalid:        {},
	imagePromptErrorIdempotencyConflict:    {},
	imagePromptErrorModelBindingInvalid:    {},
	imagePromptErrorUnavailable:            {},
	imagePromptErrorTimeout:                {},
	imagePromptErrorOutputInvalid:          {},
	imagePromptErrorModelInheritanceFailed: {},
}

type imagePromptSpecialistActorV1 struct {
	TenantID string `json:"tenantId"`
	UserID   string `json:"userId"`
}

type imagePromptSpecialistScopeV1 struct {
	ProjectID    string `json:"projectId"`
	CanvasID     string `json:"canvasId"`
	NodeID       string `json:"nodeId"`
	NodeRevision int    `json:"nodeRevision"`
}

type imagePromptSpecialistCommercialContextV1 struct {
	GenerationTaskID string `json:"generationTaskId"`
	BillingOrderID   string `json:"billingOrderId"`
	QuoteFingerprint string `json:"quoteFingerprint"`
}

type imagePromptSpecialistOperationV1 struct {
	Command    string                 `json:"command"`
	Objective  string                 `json:"objective"`
	Parameters map[string]interface{} `json:"parameters"`
}

type imagePromptSpecialistReferenceV1 struct {
	ResourceID    string `json:"resourceId"`
	MediaType     string `json:"mediaType"`
	SemanticRole  string `json:"semanticRole"`
	Ordinal       int    `json:"ordinal"`
	ContentDigest string `json:"contentDigest"`
	AccessURL     string `json:"accessUrl"`
}

type imagePromptSpecialistEvidenceV1 struct {
	CurrentPrompt  string                             `json:"currentPrompt,omitempty"`
	NegativePrompt string                             `json:"negativePrompt,omitempty"`
	References     []imagePromptSpecialistReferenceV1 `json:"references"`
}

type imagePromptSpecialistModelBindingV1 struct {
	CatalogRecordID             string `json:"catalogRecordId"`
	ModelKey                    string `json:"modelKey"`
	ConfigurationRevision       string `json:"configurationRevision"`
	ProviderEndpointVersionID   string `json:"providerEndpointVersionId"`
	ProviderCredentialVersionID string `json:"providerCredentialVersionId"`
}

type imagePromptSpecialistRequestV1 struct {
	SchemaVersion     string                                   `json:"schemaVersion"`
	RequestID         string                                   `json:"requestId"`
	CorrelationID     string                                   `json:"correlationId"`
	IdempotencyKey    string                                   `json:"idempotencyKey"`
	Actor             imagePromptSpecialistActorV1             `json:"actor"`
	Scope             imagePromptSpecialistScopeV1             `json:"scope"`
	CommercialContext imagePromptSpecialistCommercialContextV1 `json:"commercialContext"`
	Operation         imagePromptSpecialistOperationV1         `json:"operation"`
	Evidence          imagePromptSpecialistEvidenceV1          `json:"evidence"`
	PromptModel       imagePromptSpecialistModelBindingV1      `json:"promptModel"`
}

type imagePromptSpecialistStructuredPromptV2 struct {
	Version               string   `json:"version"`
	ShotIntent            string   `json:"shotIntent"`
	SpatialLayout         []string `json:"spatialLayout"`
	CameraPlan            []string `json:"cameraPlan"`
	LightingPlan          []string `json:"lightingPlan"`
	ContinuityConstraints []string `json:"continuityConstraints"`
	NegativeConstraints   []string `json:"negativeConstraints"`
}

type imagePromptSpecialistUsageV1 struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

type imagePromptSpecialistTraceModelV1 struct {
	CatalogRecordID             string `json:"catalogRecordId"`
	ModelKey                    string `json:"modelKey"`
	ConfigurationRevision       string `json:"configurationRevision"`
	ProviderEndpointVersionID   string `json:"providerEndpointVersionId"`
	ProviderCredentialVersionID string `json:"providerCredentialVersionId"`
	EffectiveModel              string `json:"effectiveModel"`
}

type imagePromptSpecialistTraceV1 struct {
	RequestID      string                            `json:"requestId"`
	CorrelationID  string                            `json:"correlationId"`
	Specialist     string                            `json:"specialist"`
	EvidenceDigest string                            `json:"evidenceDigest"`
	Model          imagePromptSpecialistTraceModelV1 `json:"model"`
	Usage          imagePromptSpecialistUsageV1      `json:"usage"`
	StartedAt      string                            `json:"startedAt"`
	CompletedAt    string                            `json:"completedAt"`
}

type imagePromptSpecialistResponseV1 struct {
	SchemaVersion    string                                   `json:"schemaVersion"`
	ImagePrompt      string                                   `json:"imagePrompt"`
	StructuredPrompt *imagePromptSpecialistStructuredPromptV2 `json:"structuredPrompt,omitempty"`
	Trace            imagePromptSpecialistTraceV1             `json:"trace"`
}

type imagePromptSpecialistExecution struct {
	Response       imagePromptSpecialistResponseV1
	ResponseDigest string
}

type imagePromptSpecialistClient interface {
	Execute(context.Context, imagePromptSpecialistRequestV1) (imagePromptSpecialistExecution, error)
}

type imagePromptSpecialistFailure struct {
	Code   imagePromptSpecialistErrorCode
	Reason string
}

func (failure *imagePromptSpecialistFailure) Error() string {
	if failure == nil {
		return "image prompt specialist failed"
	}
	return fmt.Sprintf("图片提示词 Specialist 失败（%s）：%s", failure.Code, failure.Reason)
}

func (*imagePromptSpecialistFailure) BillableProviderDispatchNotStarted() bool {
	return true
}

type imagePromptSpecialistClientConfig struct {
	Endpoint   string
	ServiceID  string
	Secret     string
	Timeout    time.Duration
	RequireTLS bool
	HTTPClient *http.Client
	Now        func() time.Time
	Nonce      func() (string, error)
}

type imagePromptSpecialistHTTPClient struct {
	endpoint   string
	serviceID  string
	secret     []byte
	httpClient *http.Client
	now        func() time.Time
	nonce      func() (string, error)
}

func newImagePromptSpecialistHTTPClient(config imagePromptSpecialistClientConfig) (*imagePromptSpecialistHTTPClient, error) {
	endpoint := strings.TrimSpace(config.Endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != imagePromptSpecialistPath {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_SPECIALIST_URL 必须是无账号、查询参数和片段的完整 specialist 地址")
	}
	if config.RequireTLS && parsed.Scheme != "https" {
		return nil, errors.New("生产环境 CANVAS_IMAGE_PROMPT_SPECIALIST_URL 必须使用 HTTPS")
	}
	serviceID := strings.TrimSpace(config.ServiceID)
	if serviceID == "" || len(serviceID) > 128 {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_HMAC_SERVICE_ID 无效")
	}
	secret := strings.TrimSpace(config.Secret)
	if !utf8.ValidString(secret) || len([]byte(secret)) < 32 || len([]byte(secret)) > 512 {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_HMAC_SECRET 必须包含 32-512 个 UTF-8 字节")
	}
	if config.Timeout < time.Second || config.Timeout > 120*time.Second {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_TIMEOUT_MS 必须是 1000-120000")
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("image prompt specialist redirect rejected")
			},
		}
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	nonce := config.Nonce
	if nonce == nil {
		nonce = randomImagePromptNonce
	}
	return &imagePromptSpecialistHTTPClient{endpoint: endpoint, serviceID: serviceID, secret: []byte(secret), httpClient: httpClient, now: now, nonce: nonce}, nil
}

func imagePromptSpecialistClientFromEnvironment() (imagePromptSpecialistClient, error) {
	enabledRaw := strings.TrimSpace(os.Getenv("CANVAS_IMAGE_PROMPT_SPECIALIST_ENABLED"))
	if enabledRaw == "" || enabledRaw == "false" {
		return nil, nil
	}
	if enabledRaw != "true" {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_SPECIALIST_ENABLED 只允许 true 或 false")
	}
	for _, key := range []string{"CANVAS_IMAGE_PROMPT_SPECIALIST_URL", "CANVAS_IMAGE_PROMPT_HMAC_SERVICE_ID", "CANVAS_IMAGE_PROMPT_HMAC_SECRET"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return nil, fmt.Errorf("%s 未配置", key)
		}
	}
	timeoutRaw := strings.TrimSpace(os.Getenv("CANVAS_IMAGE_PROMPT_TIMEOUT_MS"))
	if timeoutRaw == "" {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_TIMEOUT_MS 未配置")
	}
	timeoutMilliseconds, err := strconv.ParseInt(timeoutRaw, 10, 64)
	if err != nil {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_TIMEOUT_MS 无效")
	}
	client, err := newImagePromptSpecialistHTTPClient(imagePromptSpecialistClientConfig{
		Endpoint:   os.Getenv("CANVAS_IMAGE_PROMPT_SPECIALIST_URL"),
		ServiceID:  os.Getenv("CANVAS_IMAGE_PROMPT_HMAC_SERVICE_ID"),
		Secret:     os.Getenv("CANVAS_IMAGE_PROMPT_HMAC_SECRET"),
		Timeout:    time.Duration(timeoutMilliseconds) * time.Millisecond,
		RequireTLS: strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")) == "production",
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (client *imagePromptSpecialistHTTPClient) Execute(ctx context.Context, request imagePromptSpecialistRequestV1) (imagePromptSpecialistExecution, error) {
	if err := validateImagePromptSpecialistRequest(request); err != nil {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorRequestInvalid, Reason: err.Error()}
	}
	body, err := json.Marshal(request)
	if err != nil {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorRequestInvalid, Reason: "请求无法编码"}
	}
	nonce, err := client.nonce()
	if err != nil || strings.TrimSpace(nonce) == "" || len(nonce) > 192 {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorUnavailable, Reason: "请求 nonce 无法生成"}
	}
	timestamp := client.now().UTC().Unix()
	bodyDigest := sha256.Sum256(body)
	bodyDigestHex := hex.EncodeToString(bodyDigest[:])
	canonical := strings.Join([]string{imagePromptSignatureVersion, client.serviceID, strconv.FormatInt(timestamp, 10), nonce, http.MethodPost, imagePromptSpecialistPath, bodyDigestHex, request.IdempotencyKey}, "\n")
	mac := hmac.New(sha256.New, client.secret)
	_, _ = mac.Write([]byte(canonical))

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorRequestInvalid, Reason: "请求无法创建"}
	}
	httpRequest.Header.Set("accept", "application/json")
	httpRequest.Header.Set("content-type", "application/json")
	httpRequest.Header.Set(imagePromptHeaderServiceID, client.serviceID)
	httpRequest.Header.Set(imagePromptHeaderTimestamp, strconv.FormatInt(timestamp, 10))
	httpRequest.Header.Set(imagePromptHeaderNonce, nonce)
	httpRequest.Header.Set(imagePromptHeaderBodySHA256, bodyDigestHex)
	httpRequest.Header.Set(imagePromptHeaderIdempotencyKey, request.IdempotencyKey)
	httpRequest.Header.Set(imagePromptHeaderSignature, "v1="+hex.EncodeToString(mac.Sum(nil)))

	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		code := imagePromptErrorUnavailable
		var networkError net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
			code = imagePromptErrorTimeout
		}
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: code, Reason: "内部 Specialist 请求未完成"}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, imagePromptMaxResponseBytes+1))
	if err != nil {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorUnavailable, Reason: "读取 Specialist 响应失败"}
	}
	if int64(len(responseBody)) > imagePromptMaxResponseBytes {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "Specialist 响应超过大小限制"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return imagePromptSpecialistExecution{}, decodeImagePromptSpecialistFailure(responseBody, response.StatusCode)
	}
	var decoded imagePromptSpecialistResponseV1
	if err := decodeStrictJSON(responseBody, &decoded); err != nil {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "Specialist 响应契约无效"}
	}
	if err := validateImagePromptSpecialistResponse(request, decoded); err != nil {
		return imagePromptSpecialistExecution{}, err
	}
	canonicalResponse, err := canonicalJSON(responseBody)
	if err != nil {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "Specialist 响应无法规范编码"}
	}
	responseDigest := sha256.Sum256(canonicalResponse)
	responseDigestHex := hex.EncodeToString(responseDigest[:])
	if !constantTimeStringEqual(strings.TrimSpace(response.Header.Get(imagePromptHeaderResponseDigest)), responseDigestHex) {
		return imagePromptSpecialistExecution{}, &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "Specialist 响应摘要不一致"}
	}
	return imagePromptSpecialistExecution{Response: decoded, ResponseDigest: responseDigestHex}, nil
}

func decodeImagePromptSpecialistFailure(body []byte, status int) error {
	var payload struct {
		Code          imagePromptSpecialistErrorCode `json:"code"`
		Reason        string                         `json:"reason"`
		RequestID     string                         `json:"requestId,omitempty"`
		CorrelationID string                         `json:"correlationId,omitempty"`
	}
	if err := decodeStrictJSON(body, &payload); err != nil {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorUnavailable, Reason: fmt.Sprintf("内部 Specialist 返回 HTTP %d", status)}
	}
	if _, ok := imagePromptSpecialistErrorCodes[payload.Code]; !ok {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorUnavailable, Reason: fmt.Sprintf("内部 Specialist 返回 HTTP %d", status)}
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = fmt.Sprintf("内部 Specialist 返回 HTTP %d", status)
	}
	return &imagePromptSpecialistFailure{Code: payload.Code, Reason: truncateRunes(reason, 500)}
}

func validateImagePromptSpecialistRequest(request imagePromptSpecialistRequestV1) error {
	if request.SchemaVersion != imagePromptRequestVersion || !boundedTrimmed(request.RequestID, 160) || !boundedTrimmed(request.CorrelationID, 160) || !boundedTrimmed(request.IdempotencyKey, 240) {
		return errors.New("请求身份无效")
	}
	if !boundedTrimmed(request.Actor.TenantID, 160) || !boundedTrimmed(request.Actor.UserID, 160) || !boundedTrimmed(request.Scope.ProjectID, 160) || !boundedTrimmed(request.Scope.CanvasID, 160) || !boundedTrimmed(request.Scope.NodeID, 160) || request.Scope.NodeRevision < 0 {
		return errors.New("请求作用域无效")
	}
	if !boundedTrimmed(request.CommercialContext.GenerationTaskID, 160) || !boundedTrimmed(request.CommercialContext.BillingOrderID, 160) || !boundedTrimmed(request.CommercialContext.QuoteFingerprint, 256) {
		return errors.New("商业上下文无效")
	}
	if request.Operation.Command != imagePromptCommandPortraitTexture || !boundedTrimmed(request.Operation.Objective, 1000) || len(request.Operation.Parameters) > 32 {
		return errors.New("图片命令无效")
	}
	for key, value := range request.Operation.Parameters {
		if strings.TrimSpace(key) != key || key == "" || len(key) > 80 || !imagePromptScalar(value) {
			return errors.New("图片命令参数无效")
		}
	}
	if len(request.Evidence.References) < 1 || len(request.Evidence.References) > 8 || len(request.Evidence.CurrentPrompt) > 8000 || len(request.Evidence.NegativePrompt) > 4000 {
		return errors.New("图片证据无效")
	}
	sourceFound := false
	ordinals := make(map[int]struct{}, len(request.Evidence.References))
	for _, reference := range request.Evidence.References {
		if reference.MediaType != "image" || !boundedTrimmed(reference.ResourceID, 160) || reference.Ordinal <= 0 || !validSHA256Hex(reference.ContentDigest) || !validImagePromptAccessURL(reference.AccessURL) {
			return errors.New("图片引用证据无效")
		}
		if reference.SemanticRole != "source" && reference.SemanticRole != "character" && reference.SemanticRole != "style" && reference.SemanticRole != "scene" {
			return errors.New("图片引用角色无效")
		}
		if _, exists := ordinals[reference.Ordinal]; exists {
			return errors.New("图片引用序号重复")
		}
		ordinals[reference.Ordinal] = struct{}{}
		if reference.SemanticRole == "source" {
			sourceFound = true
		}
	}
	if !sourceFound {
		return errors.New("缺少 source 图片证据")
	}
	if !boundedTrimmed(request.PromptModel.CatalogRecordID, 160) || !boundedTrimmed(request.PromptModel.ModelKey, 160) || !boundedTrimmed(request.PromptModel.ConfigurationRevision, 160) || !boundedTrimmed(request.PromptModel.ProviderEndpointVersionID, 160) || !boundedTrimmed(request.PromptModel.ProviderCredentialVersionID, 160) {
		return errors.New("提示词模型绑定无效")
	}
	return nil
}

func validateImagePromptSpecialistResponse(request imagePromptSpecialistRequestV1, response imagePromptSpecialistResponseV1) error {
	if response.SchemaVersion != imagePromptResponseVersion || !boundedTrimmed(response.ImagePrompt, 8000) {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "图片提示词响应无效"}
	}
	if response.Trace.RequestID != request.RequestID || response.Trace.CorrelationID != request.CorrelationID || response.Trace.Specialist != imagePromptSpecialistName {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "Specialist 响应身份不一致"}
	}
	evidenceDigest, err := imagePromptEvidenceDigest(request.Evidence)
	if err != nil || response.Trace.EvidenceDigest != evidenceDigest {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorEvidenceInvalid, Reason: "Specialist 证据摘要不一致"}
	}
	model := response.Trace.Model
	if model.CatalogRecordID != request.PromptModel.CatalogRecordID || model.ModelKey != request.PromptModel.ModelKey || model.ConfigurationRevision != request.PromptModel.ConfigurationRevision || model.ProviderEndpointVersionID != request.PromptModel.ProviderEndpointVersionID || model.ProviderCredentialVersionID != request.PromptModel.ProviderCredentialVersionID || model.EffectiveModel != request.PromptModel.ModelKey {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorModelInheritanceFailed, Reason: "Specialist 模型绑定不一致"}
	}
	usage := response.Trace.Usage
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.TotalTokens < 0 || usage.TotalTokens != usage.InputTokens+usage.OutputTokens {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "Specialist token usage 无效"}
	}
	startedAt, startedErr := time.Parse(time.RFC3339Nano, response.Trace.StartedAt)
	completedAt, completedErr := time.Parse(time.RFC3339Nano, response.Trace.CompletedAt)
	if startedErr != nil || completedErr != nil || completedAt.Before(startedAt) {
		return &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "Specialist 时间证据无效"}
	}
	if response.StructuredPrompt != nil {
		structured := response.StructuredPrompt
		if structured.Version != "v2" || !boundedTrimmed(structured.ShotIntent, 600) || !validImagePromptStringList(structured.SpatialLayout) || !validImagePromptStringList(structured.CameraPlan) || !validImagePromptStringList(structured.LightingPlan) || !validImagePromptStringList(structured.ContinuityConstraints) || !validImagePromptStringList(structured.NegativeConstraints) {
			return &imagePromptSpecialistFailure{Code: imagePromptErrorOutputInvalid, Reason: "结构化图片提示词无效"}
		}
	}
	return nil
}

func imagePromptEvidenceDigest(evidence imagePromptSpecialistEvidenceV1) (string, error) {
	type digestReference struct {
		ResourceID    string `json:"resourceId"`
		MediaType     string `json:"mediaType"`
		SemanticRole  string `json:"semanticRole"`
		Ordinal       int    `json:"ordinal"`
		ContentDigest string `json:"contentDigest"`
	}
	payload := struct {
		CurrentPrompt  string            `json:"currentPrompt,omitempty"`
		NegativePrompt string            `json:"negativePrompt,omitempty"`
		References     []digestReference `json:"references"`
	}{CurrentPrompt: evidence.CurrentPrompt, NegativePrompt: evidence.NegativePrompt, References: make([]digestReference, 0, len(evidence.References))}
	for _, reference := range evidence.References {
		payload.References = append(payload.References, digestReference{ResourceID: reference.ResourceID, MediaType: reference.MediaType, SemanticRole: reference.SemanticRole, Ordinal: reference.Ordinal, ContentDigest: reference.ContentDigest})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(encoded)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func canonicalJSON(encoded []byte) ([]byte, error) {
	var value interface{}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func decodeStrictJSON(encoded []byte, target interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON 包含多个值")
		}
		return err
	}
	return nil
}

func randomImagePromptNonce() (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func boundedTrimmed(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= maximum
}

func imagePromptScalar(value interface{}) bool {
	switch typed := value.(type) {
	case nil, bool:
		return true
	case string:
		return len(typed) <= 1000
	case json.Number:
		number, err := strconv.ParseFloat(string(typed), 64)
		return err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	case float64:
		return !math.IsNaN(typed) && !math.IsInf(typed, 0)
	case float32:
		number := float64(typed)
		return !math.IsNaN(number) && !math.IsInf(number, 0)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	default:
		return false
	}
}

func validSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validImagePromptAccessURL(value string) bool {
	if len(value) > 4096 || value != strings.TrimSpace(value) {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func validImagePromptStringList(values []string) bool {
	if len(values) < 1 || len(values) > 12 {
		return false
	}
	for _, value := range values {
		if !boundedTrimmed(value, 600) {
			return false
		}
	}
	return true
}

func constantTimeStringEqual(left string, right string) bool {
	return hmac.Equal([]byte(left), []byte(right))
}
