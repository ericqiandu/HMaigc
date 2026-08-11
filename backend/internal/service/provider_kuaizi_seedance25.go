package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	kuaiziSeedance25CreatePath = "/ai-open-platform-api/v1/lz/video/task/create"
	kuaiziSeedance25StatusPath = "/ai-open-platform-api/v1/lz/video/task/status"
)

type kuaiziSeedance25Media struct {
	URL  string `json:"url"`
	Role string `json:"role"`
}

type kuaiziSeedance25CreateRequest struct {
	Prompt          string                  `json:"prompt,omitempty"`
	Mode            string                  `json:"mode"`
	Images          []kuaiziSeedance25Media `json:"images,omitempty"`
	Videos          []kuaiziSeedance25Media `json:"videos,omitempty"`
	Audios          []kuaiziSeedance25Media `json:"audios,omitempty"`
	Resolution      string                  `json:"resolution"`
	Ratio           string                  `json:"ratio"`
	Duration        int                     `json:"duration"`
	GenerateAudio   bool                    `json:"generate_audio"`
	Watermark       bool                    `json:"watermark"`
	ReturnLastFrame bool                    `json:"return_last_frame"`
}

type kuaiziSeedance25StatusRequest struct {
	TaskID string `json:"task_id"`
}

type kuaiziSeedance25Envelope struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	TraceID string          `json:"trace_id"`
}

type kuaiziSeedance25Created struct {
	TaskID string `json:"task_id"`
}

type kuaiziSeedance25Status struct {
	TaskID        string                  `json:"task_id"`
	Status        string                  `json:"status"`
	VideoURL      string                  `json:"video_url"`
	LastFrameURL  string                  `json:"last_frame_url"`
	Duration      int                     `json:"duration"`
	TotalTokens   kuaiziSeedance25Decimal `json:"total_tokens"`
	TokensPresent bool                    `json:"-"`
	Error         string                  `json:"error"`
}

func (status *kuaiziSeedance25Status) UnmarshalJSON(data []byte) error {
	type statusAlias kuaiziSeedance25Status
	var decoded statusAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	rawTokens, present := fields["total_tokens"]
	decoded.TokensPresent = present && string(rawTokens) != "null"
	*status = kuaiziSeedance25Status(decoded)
	return nil
}

type kuaiziSeedance25Decimal string

func (value *kuaiziSeedance25Decimal) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*value = ""
		return nil
	}
	if strings.HasPrefix(text, `"`) {
		var decoded string
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		text = decoded
	}
	if text == "" {
		*value = ""
		return nil
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return errors.New("Seedance 2.5 total_tokens 必须是非负十进制整数")
		}
	}
	*value = kuaiziSeedance25Decimal(text)
	return nil
}

type KuaiziSeedance25State struct {
	ProviderStatus        string
	Terminal              bool
	Succeeded             bool
	FailureReason         string
	AssetSourceURL        string
	LastFrameURL          string
	ActualDurationSeconds int
	TotalTokens           string
}

type KuaiziSeedance25Created struct {
	TaskID  string
	TraceID string
}

type KuaiziSeedance25Polled struct {
	TraceID string
	State   KuaiziSeedance25State
}

type KuaiziSeedance25Error struct {
	Stage   string
	Kind    string
	Code    string
	TraceID string
	Message string
	Cause   error
}

func (e *KuaiziSeedance25Error) Error() string {
	parts := []string{"筷子 Seedance 2.5 " + e.Stage + "失败", "kind=" + e.Kind}
	if e.Code != "" {
		parts = append(parts, "code="+e.Code)
	}
	if e.TraceID != "" {
		parts = append(parts, "trace_id="+e.TraceID)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	return strings.Join(parts, "；")
}

func (e *KuaiziSeedance25Error) Unwrap() error { return e.Cause }

type KuaiziSeedance25CreateUncertainError struct {
	Cause error
}

func (e *KuaiziSeedance25CreateUncertainError) Error() string {
	return "筷子 Seedance 2.5 创建请求已发出但未取得可验证结果"
}

func (e *KuaiziSeedance25CreateUncertainError) Unwrap() error { return e.Cause }

type KuaiziSeedance25Client struct {
	httpClient *http.Client
	cipher     *ProviderSecretCipher
}

func NewKuaiziSeedance25Client(httpClient *http.Client, cipher *ProviderSecretCipher) *KuaiziSeedance25Client {
	return &KuaiziSeedance25Client{httpClient: httpClient, cipher: cipher}
}

func kuaiziSeedance25Body(input canvasGenerationInput) (kuaiziSeedance25CreateRequest, error) {
	duration, err := kuaiziSeedance25Duration(input.Config.VideoSeconds)
	if err != nil {
		return kuaiziSeedance25CreateRequest{}, err
	}
	resolution, err := kuaiziSeedance25Resolution(input.Config.VQuality)
	if err != nil {
		return kuaiziSeedance25CreateRequest{}, err
	}
	ratio, err := kuaiziSeedance25Ratio(input.Config.Size)
	if err != nil {
		return kuaiziSeedance25CreateRequest{}, err
	}
	images, err := kuaiziSeedance25MediaList(input.ReferenceImages, "image", 30, input)
	if err != nil {
		return kuaiziSeedance25CreateRequest{}, err
	}
	videos, err := kuaiziSeedance25MediaList(input.ReferenceVideos, "video", 10, input)
	if err != nil {
		return kuaiziSeedance25CreateRequest{}, err
	}
	audios, err := kuaiziSeedance25MediaList(input.ReferenceAudios, "audio", 10, input)
	if err != nil {
		return kuaiziSeedance25CreateRequest{}, err
	}
	if strings.TrimSpace(input.Prompt) == "" && len(images)+len(videos)+len(audios) == 0 {
		return kuaiziSeedance25CreateRequest{}, errors.New("Seedance 2.5 至少需要提示词或一个参考素材")
	}
	return kuaiziSeedance25CreateRequest{
		Prompt: strings.TrimSpace(input.Prompt), Mode: "seedance2.5", Images: images, Videos: videos, Audios: audios,
		Resolution: resolution, Ratio: ratio, Duration: duration,
		GenerateAudio: parseBool(input.Config.VideoGenerateAudio, true),
		Watermark:     parseBool(input.Config.VideoWatermark, false), ReturnLastFrame: true,
	}, nil
}

func kuaiziSeedance25Duration(value string) (int, error) {
	duration, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || (duration != -1 && (duration < 4 || duration > 30)) {
		return 0, errors.New("Seedance 2.5 时长必须为 4–30 秒整数或 -1")
	}
	return duration, nil
}

func kuaiziSeedance25Resolution(value string) (string, error) {
	resolution := strings.ToLower(strings.TrimSpace(value))
	if resolution != "480p" && resolution != "720p" {
		return "", fmt.Errorf("Seedance 2.5 不支持分辨率：%s", strings.TrimSpace(value))
	}
	return resolution, nil
}

func kuaiziSeedance25Ratio(value string) (string, error) {
	ratio := strings.TrimSpace(value)
	if ratio == "" || ratio == "auto" {
		ratio = "adaptive"
	}
	switch ratio {
	case "16:9", "4:3", "1:1", "3:4", "9:16", "21:9", "adaptive":
		return ratio, nil
	default:
		return "", fmt.Errorf("Seedance 2.5 不支持画面比例：%s", ratio)
	}
}

func kuaiziSeedance25MediaList(items []providerMedia, kind string, limit int, input canvasGenerationInput) ([]kuaiziSeedance25Media, error) {
	if len(items) > limit {
		return nil, fmt.Errorf("Seedance 2.5 最多支持 %d 个%s参考素材", limit, kind)
	}
	result := make([]kuaiziSeedance25Media, 0, len(items))
	for _, item := range items {
		role, err := kuaiziSeedance25MediaRole(kind, item, input)
		if err != nil {
			return nil, err
		}
		mediaURL, err := kuaiziSeedance25MediaURL(item)
		if err != nil {
			return nil, err
		}
		result = append(result, kuaiziSeedance25Media{URL: mediaURL, Role: role})
	}
	return result, nil
}

func kuaiziSeedance25MediaRole(kind string, item providerMedia, input canvasGenerationInput) (string, error) {
	role := strings.TrimSpace(item.Role)
	switch kind {
	case "image":
		if role == "" {
			role = seedanceImageRole(input, item)
		}
		if role != "first_frame" && role != "last_frame" && role != "reference_image" {
			return "", fmt.Errorf("Seedance 2.5 不支持图片角色：%s", role)
		}
	case "video":
		if role == "" {
			role = "reference_video"
		}
		if role != "reference_video" {
			return "", fmt.Errorf("Seedance 2.5 不支持视频角色：%s", role)
		}
	case "audio":
		if role == "" {
			role = "reference_audio"
		}
		if role != "reference_audio" {
			return "", fmt.Errorf("Seedance 2.5 不支持音频角色：%s", role)
		}
	default:
		return "", fmt.Errorf("Seedance 2.5 不支持媒体类型：%s", kind)
	}
	return role, nil
}

func kuaiziSeedance25MediaURL(media providerMedia) (string, error) {
	if !strings.HasPrefix(strings.TrimSpace(media.StorageKey), "resource:") || strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(media.StorageKey), "resource:")) == "" {
		return "", errors.New("Seedance 2.5 参考素材必须来自已授权的平台资源")
	}
	rawURL := strings.TrimSpace(media.URL)
	if rawURL == "" {
		return "", errors.New("Seedance 2.5 参考素材缺少 HTTPS URL")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || parsed.Scheme != "https" || parsed.User != nil {
		return "", errors.New("Seedance 2.5 参考素材必须使用无凭据的 HTTPS URL")
	}
	if address := net.ParseIP(parsed.Hostname()); address != nil && blockedSpecialUseIP(address) {
		return "", errors.New("Seedance 2.5 参考素材不允许使用本机、内网或特殊用途地址")
	}
	return parsed.String(), nil
}

func classifyKuaiziSeedance25Status(status kuaiziSeedance25Status) (KuaiziSeedance25State, error) {
	taskID := strings.TrimSpace(status.TaskID)
	if taskID == "" {
		return KuaiziSeedance25State{}, errors.New("Seedance 2.5 状态响应缺少任务 ID")
	}
	providerStatus := strings.ToLower(strings.TrimSpace(status.Status))
	state := KuaiziSeedance25State{ProviderStatus: providerStatus}
	switch providerStatus {
	case "submitted", "pending", "running":
		return state, nil
	case "failed":
		state.Terminal = true
		state.FailureReason = strings.TrimSpace(status.Error)
		if state.FailureReason == "" {
			return KuaiziSeedance25State{}, errors.New("Seedance 2.5 失败状态缺少错误事实")
		}
		return state, nil
	case "succeeded":
		videoURL, err := validateKuaiziSeedance25OutputURL(status.VideoURL, "视频")
		if err != nil {
			return KuaiziSeedance25State{}, err
		}
		lastFrameURL, err := validateKuaiziSeedance25OutputURL(status.LastFrameURL, "尾帧")
		if err != nil {
			return KuaiziSeedance25State{}, err
		}
		if status.Duration < 4 || status.Duration > 30 {
			return KuaiziSeedance25State{}, errors.New("Seedance 2.5 成功状态实际时长无效")
		}
		if !status.TokensPresent || string(status.TotalTokens) == "" {
			return KuaiziSeedance25State{}, errors.New("Seedance 2.5 成功状态缺少完整用量事实")
		}
		state.Terminal = true
		state.Succeeded = true
		state.AssetSourceURL = videoURL
		state.LastFrameURL = lastFrameURL
		state.ActualDurationSeconds = status.Duration
		state.TotalTokens = string(status.TotalTokens)
		return state, nil
	default:
		return KuaiziSeedance25State{}, fmt.Errorf("Seedance 2.5 返回未知状态：%s", strings.TrimSpace(status.Status))
	}
}

func validateKuaiziSeedance25OutputURL(rawURL string, label string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return "", fmt.Errorf("Seedance 2.5 成功状态缺少有效%s HTTPS URL", label)
	}
	if address := net.ParseIP(parsed.Hostname()); address != nil && blockedSpecialUseIP(address) {
		return "", fmt.Errorf("Seedance 2.5 %s URL 不允许使用本机、内网或特殊用途地址", label)
	}
	return parsed.String(), nil
}

func (c *KuaiziSeedance25Client) Create(ctx context.Context, runtime *ProviderTaskRuntime, input canvasGenerationInput) (KuaiziSeedance25Created, error) {
	body, err := kuaiziSeedance25Body(input)
	if err != nil {
		return KuaiziSeedance25Created{}, err
	}
	var created kuaiziSeedance25Created
	traceID, err := c.requestJSON(ctx, runtime, "create", kuaiziSeedance25CreatePath, body, &created)
	if err != nil {
		return KuaiziSeedance25Created{}, err
	}
	created.TaskID = strings.TrimSpace(created.TaskID)
	if created.TaskID == "" {
		return KuaiziSeedance25Created{}, &KuaiziSeedance25CreateUncertainError{Cause: &KuaiziSeedance25Error{Stage: "create", Kind: "invalid_response", TraceID: traceID, Message: "响应缺少 task_id"}}
	}
	return KuaiziSeedance25Created{TaskID: created.TaskID, TraceID: traceID}, nil
}

func (c *KuaiziSeedance25Client) Status(ctx context.Context, runtime *ProviderTaskRuntime, taskID string) (KuaiziSeedance25Polled, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return KuaiziSeedance25Polled{}, errors.New("Seedance 2.5 查询缺少任务 ID")
	}
	var status kuaiziSeedance25Status
	traceID, err := c.requestJSON(ctx, runtime, "poll", kuaiziSeedance25StatusPath, kuaiziSeedance25StatusRequest{TaskID: taskID}, &status)
	if err != nil {
		return KuaiziSeedance25Polled{}, err
	}
	if strings.TrimSpace(status.TaskID) != taskID {
		return KuaiziSeedance25Polled{}, &KuaiziSeedance25Error{Stage: "poll", Kind: "invalid_response", TraceID: traceID, Message: "响应 task_id 与请求不一致"}
	}
	state, err := classifyKuaiziSeedance25Status(status)
	if err != nil {
		return KuaiziSeedance25Polled{}, &KuaiziSeedance25Error{Stage: "poll", Kind: "invalid_response", TraceID: traceID, Message: err.Error(), Cause: err}
	}
	return KuaiziSeedance25Polled{TraceID: traceID, State: state}, nil
}

func (c *KuaiziSeedance25Client) PollUntilTerminal(ctx context.Context, runtime *ProviderTaskRuntime, taskID string, interval time.Duration, observed func(KuaiziSeedance25Polled) error) (KuaiziSeedance25Polled, error) {
	if interval <= 0 {
		return KuaiziSeedance25Polled{}, errors.New("Seedance 2.5 轮询间隔必须大于 0")
	}
	for {
		if err := ctx.Err(); err != nil {
			return KuaiziSeedance25Polled{}, err
		}
		polled, err := c.Status(ctx, runtime, taskID)
		if err != nil {
			return KuaiziSeedance25Polled{}, err
		}
		if observed != nil {
			if err := observed(polled); err != nil {
				return KuaiziSeedance25Polled{}, err
			}
		}
		if polled.State.Terminal {
			return polled, nil
		}
		if err := sleepContext(ctx, interval); err != nil {
			return KuaiziSeedance25Polled{}, err
		}
	}
}

func (c *KuaiziSeedance25Client) requestJSON(ctx context.Context, runtime *ProviderTaskRuntime, stage string, path string, body any, target any) (string, error) {
	if c == nil || c.httpClient == nil || c.cipher == nil {
		return "", &KuaiziSeedance25Error{Stage: stage, Kind: "client_unavailable"}
	}
	if runtime == nil {
		return "", &KuaiziSeedance25Error{Stage: stage, Kind: "runtime_missing"}
	}
	if err := validateKuaiziSeedance25RuntimeIdentity(runtime); err != nil {
		return "", err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(runtime.EndpointVersion.BaseURL), "/") + path
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", &KuaiziSeedance25Error{Stage: stage, Kind: "invalid_request", Cause: err}
	}
	apiKey, err := c.cipher.Decrypt(runtime.Account.ID, runtime.Credential.ID, runtime.CredentialVersion.ID, runtime.CredentialVersion.KeyCipher)
	if err != nil {
		return "", &KuaiziSeedance25Error{Stage: stage, Kind: "credential_decrypt", Cause: err}
	}
	request.Header.Set("ApiKey", apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	wroteRequest := false
	trace := &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) { wroteRequest = info.Err == nil }}
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), trace))
	startedAt := time.Now()
	response, err := c.httpClient.Do(request)
	if err != nil {
		recordProviderRequest(request, startedAt, 0, nil, err)
		if stage == "create" && wroteRequest {
			return "", &KuaiziSeedance25CreateUncertainError{Cause: err}
		}
		kind := "network"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			kind = "timeout"
		}
		return "", &KuaiziSeedance25Error{Stage: stage, Kind: kind, Cause: err}
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if readErr != nil {
		recordProviderRequest(request, startedAt, response.StatusCode, nil, readErr)
		if stage == "create" {
			return "", &KuaiziSeedance25CreateUncertainError{Cause: readErr}
		}
		return "", &KuaiziSeedance25Error{Stage: stage, Kind: "response_read", Cause: readErr}
	}
	if len(responseBody) > 4<<20 {
		responseErr := &KuaiziSeedance25Error{Stage: stage, Kind: "invalid_response", Message: "响应超过大小限制"}
		recordProviderRequest(request, startedAt, response.StatusCode, nil, responseErr)
		if stage == "create" {
			return "", &KuaiziSeedance25CreateUncertainError{Cause: responseErr}
		}
		return "", responseErr
	}
	var envelope kuaiziSeedance25Envelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		responseErr := &KuaiziSeedance25Error{Stage: stage, Kind: "invalid_response", Message: "响应不是有效 JSON", Cause: err}
		recordProviderRequest(request, startedAt, response.StatusCode, nil, responseErr)
		if stage == "create" && response.StatusCode >= 200 && response.StatusCode < 300 {
			return "", &KuaiziSeedance25CreateUncertainError{Cause: responseErr}
		}
		return "", responseErr
	}
	sensitiveValues := kuaiziRuntimeSensitiveValues(runtime, apiKey)
	traceID := safeKuaiziTraceID(envelope.TraceID, sensitiveValues...)
	safeMessage := safeKuaiziMessage(envelope.Message, sensitiveValues...)
	if envelope.Code == nil {
		responseErr := &KuaiziSeedance25Error{Stage: stage, Kind: "invalid_response", TraceID: traceID, Message: "响应缺少整数 code"}
		recordProviderRequest(request, startedAt, response.StatusCode, nil, responseErr)
		if stage == "create" {
			return traceID, &KuaiziSeedance25CreateUncertainError{Cause: responseErr}
		}
		return traceID, responseErr
	}
	if response.StatusCode != http.StatusOK {
		responseErr := &KuaiziSeedance25Error{Stage: stage, Kind: "http", Code: strconv.Itoa(response.StatusCode), TraceID: traceID, Message: safeMessage}
		recordProviderRequest(request, startedAt, response.StatusCode, nil, responseErr)
		if stage == "create" && !kuaiziCreateHTTPDefinitiveRejection(response.StatusCode) {
			return traceID, &KuaiziSeedance25CreateUncertainError{Cause: responseErr}
		}
		return traceID, responseErr
	}
	if *envelope.Code != 0 {
		responseErr := &KuaiziSeedance25Error{Stage: stage, Kind: "business", Code: strconv.Itoa(*envelope.Code), TraceID: traceID, Message: safeMessage}
		recordProviderRequest(request, startedAt, response.StatusCode, nil, responseErr)
		return traceID, responseErr
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		responseErr := &KuaiziSeedance25Error{Stage: stage, Kind: "invalid_response", TraceID: traceID, Message: "响应缺少 data"}
		recordProviderRequest(request, startedAt, response.StatusCode, nil, responseErr)
		return traceID, responseErr
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		responseErr := &KuaiziSeedance25Error{Stage: stage, Kind: "invalid_response", TraceID: traceID, Message: "data 格式无效", Cause: err}
		recordProviderRequest(request, startedAt, response.StatusCode, nil, responseErr)
		if stage == "create" {
			return traceID, &KuaiziSeedance25CreateUncertainError{Cause: responseErr}
		}
		return traceID, responseErr
	}
	sanitizeKuaiziSeedance25Response(target, sensitiveValues)
	recordProviderRequest(request, startedAt, response.StatusCode, nil, nil)
	return traceID, nil
}

func kuaiziCreateHTTPDefinitiveRejection(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

func safeKuaiziMessage(value string, sensitiveValues ...string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	_ = sensitiveValues
	return "上游返回了受控错误摘要，原始内容已隐藏"
}

func kuaiziRuntimeSensitiveValues(runtime *ProviderTaskRuntime, apiKey string) []string {
	values := []string{apiKey, runtime.Task.Prompt}
	var input struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal([]byte(runtime.Task.InputJSON), &input) == nil {
		values = append(values, input.Prompt)
	}
	return values
}

func sanitizeKuaiziSeedance25Response(target any, sensitiveValues []string) {
	status, ok := target.(*kuaiziSeedance25Status)
	if !ok || status == nil {
		return
	}
	status.Error = safeKuaiziMessage(status.Error, sensitiveValues...)
	switch strings.ToLower(strings.TrimSpace(status.Status)) {
	case "submitted", "pending", "running", "failed", "succeeded":
		status.Status = strings.ToLower(strings.TrimSpace(status.Status))
	default:
		status.Status = "unknown"
	}
}

func validateKuaiziSeedance25RuntimeIdentity(runtime *ProviderTaskRuntime) error {
	if runtime.Account.ID == "" || runtime.Account.ProviderKind != kuaiziProviderKind ||
		runtime.EndpointVersion.ProviderAccountID != runtime.Account.ID || runtime.Credential.ProviderAccountID != runtime.Account.ID ||
		runtime.Credential.Family != "seedance" || runtime.CredentialVersion.ProviderCredentialID != runtime.Credential.ID ||
		runtime.ChannelModel.ProviderCredentialID != runtime.Credential.ID || runtime.ProviderFact.ProviderEndpointVersionID != runtime.EndpointVersion.ID ||
		runtime.ProviderFact.ProviderCredentialVersionID != runtime.CredentialVersion.ID || runtime.ProviderFact.ChannelModelID != runtime.ChannelModel.ID {
		return &KuaiziSeedance25Error{Stage: "runtime", Kind: "identity_mismatch"}
	}
	if runtime.ChannelModel.ModelKey != "kuaizi-seedance-2.5" || runtime.ChannelModel.Capability != "video" {
		return &KuaiziSeedance25Error{Stage: "runtime", Kind: "model_mismatch"}
	}
	return nil
}
