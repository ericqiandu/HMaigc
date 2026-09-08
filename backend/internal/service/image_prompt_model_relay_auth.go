package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ImagePromptModelRelayPath = "/internal/v1/model-relays/image-prompt/chat-completions"

	ImagePromptModelRelayHeaderServiceID        = "x-tapcanvas-service-id"
	ImagePromptModelRelayHeaderTimestamp        = "x-tapcanvas-timestamp"
	ImagePromptModelRelayHeaderNonce            = "x-tapcanvas-nonce"
	ImagePromptModelRelayHeaderBodySHA256       = "x-tapcanvas-body-sha256"
	ImagePromptModelRelayHeaderGenerationTaskID = "x-tapcanvas-generation-task-id"
	ImagePromptModelRelayHeaderCorrelationID    = "x-tapcanvas-correlation-id"
	ImagePromptModelRelayHeaderSignature        = "x-tapcanvas-signature"

	imagePromptModelRelaySignatureVersion = "hmaigc-image-prompt-model-relay/v1"
	imagePromptModelRelayClockSkew        = 60 * time.Second
	imagePromptModelRelayNonceTTL         = 2 * time.Minute
)

type imagePromptModelRelayAuthConfig struct {
	allowedServiceID string
	secret           []byte
	now              func() time.Time
}

type ImagePromptModelRelayAuthInput struct {
	ServiceID        string
	Timestamp        string
	Nonce            string
	BodySHA256       string
	GenerationTaskID string
	CorrelationID    string
	Signature        string
	Body             []byte
}

func imagePromptModelRelayAuthFromEnvironment(enabled bool, coordinator *runtimeCoordinator) (*imagePromptModelRelayAuthConfig, error) {
	if !enabled {
		return nil, nil
	}
	serviceID := strings.TrimSpace(os.Getenv("CANVAS_IMAGE_PROMPT_RELAY_ALLOWED_SERVICE_ID"))
	secret := strings.TrimSpace(os.Getenv("CANVAS_IMAGE_PROMPT_RELAY_HMAC_SECRET"))
	if !boundedTrimmed(serviceID, 128) {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_RELAY_ALLOWED_SERVICE_ID 无效")
	}
	if !utf8.ValidString(secret) || len([]byte(secret)) < 32 || len([]byte(secret)) > 512 {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_RELAY_HMAC_SECRET 必须包含 32-512 个 UTF-8 字节")
	}
	if constantTimeStringEqual(secret, strings.TrimSpace(os.Getenv("CANVAS_IMAGE_PROMPT_HMAC_SECRET"))) {
		return nil, errors.New("CANVAS_IMAGE_PROMPT_RELAY_HMAC_SECRET 不得复用正向 Specialist HMAC 密钥")
	}
	if coordinator == nil || coordinator.redis == nil {
		return nil, errors.New("启用图片提示词 Specialist 时必须配置 REDIS_URL 以保护 HMaigc 私有模型中继")
	}
	return &imagePromptModelRelayAuthConfig{allowedServiceID: serviceID, secret: []byte(secret), now: time.Now}, nil
}

func (s *Service) ImagePromptModelRelayEnabled() bool {
	return s != nil && s.imagePromptModelRelayAuth != nil
}

func (s *Service) VerifyImagePromptModelRelayAuth(ctx context.Context, input ImagePromptModelRelayAuthInput) error {
	config := s.imagePromptModelRelayAuth
	if config == nil {
		return &ImagePromptModelRelayError{Code: imagePromptErrorUnavailable, Reason: "图片提示词模型中继未启用", HTTPStatus: http.StatusServiceUnavailable}
	}
	serviceID := strings.TrimSpace(input.ServiceID)
	nonce := strings.TrimSpace(input.Nonce)
	generationTaskID := strings.TrimSpace(input.GenerationTaskID)
	correlationID := strings.TrimSpace(input.CorrelationID)
	if !constantTimeStringEqual(serviceID, config.allowedServiceID) || !boundedTrimmed(nonce, 192) || !boundedTrimmed(generationTaskID, 160) || !boundedTrimmed(correlationID, 160) || !validSHA256Hex(input.BodySHA256) {
		return &ImagePromptModelRelayError{Code: imagePromptErrorAuthInvalid, Reason: "模型中继身份无效", HTTPStatus: http.StatusUnauthorized}
	}
	timestamp, err := strconv.ParseInt(strings.TrimSpace(input.Timestamp), 10, 64)
	if err != nil {
		return &ImagePromptModelRelayError{Code: imagePromptErrorAuthInvalid, Reason: "模型中继时间戳无效", HTTPStatus: http.StatusUnauthorized}
	}
	now := time.Now().UTC()
	if config.now != nil {
		now = config.now().UTC()
	}
	requestTime := time.Unix(timestamp, 0).UTC()
	if requestTime.Before(now.Add(-imagePromptModelRelayClockSkew)) || requestTime.After(now.Add(imagePromptModelRelayClockSkew)) {
		return &ImagePromptModelRelayError{Code: imagePromptErrorAuthInvalid, Reason: "模型中继请求已过期", HTTPStatus: http.StatusUnauthorized}
	}
	bodyDigest := sha256.Sum256(input.Body)
	bodyDigestHex := hex.EncodeToString(bodyDigest[:])
	if !constantTimeStringEqual(strings.ToLower(strings.TrimSpace(input.BodySHA256)), bodyDigestHex) {
		return &ImagePromptModelRelayError{Code: imagePromptErrorAuthInvalid, Reason: "模型中继正文摘要不一致", HTTPStatus: http.StatusUnauthorized}
	}
	canonical := strings.Join([]string{
		imagePromptModelRelaySignatureVersion,
		serviceID,
		strconv.FormatInt(timestamp, 10),
		nonce,
		http.MethodPost,
		ImagePromptModelRelayPath,
		bodyDigestHex,
		generationTaskID,
		correlationID,
	}, "\n")
	mac := hmac.New(sha256.New, config.secret)
	_, _ = mac.Write([]byte(canonical))
	wantSignature := "v1=" + hex.EncodeToString(mac.Sum(nil))
	if !constantTimeStringEqual(strings.TrimSpace(input.Signature), wantSignature) {
		return &ImagePromptModelRelayError{Code: imagePromptErrorAuthInvalid, Reason: "模型中继签名无效", HTTPStatus: http.StatusUnauthorized}
	}
	if err := s.claimImagePromptModelRelayNonce(ctx, serviceID, nonce, imagePromptModelRelayNonceTTL); err != nil {
		return &ImagePromptModelRelayError{Code: imagePromptErrorReplayRejected, Reason: "模型中继防重放校验失败", HTTPStatus: http.StatusConflict, Cause: err}
	}
	return nil
}

func imagePromptModelRelaySignature(config imagePromptModelRelayAuthConfig, timestamp int64, nonce string, body []byte, generationTaskID string, correlationID string) (string, string) {
	digest := sha256.Sum256(body)
	digestHex := hex.EncodeToString(digest[:])
	canonical := strings.Join([]string{imagePromptModelRelaySignatureVersion, config.allowedServiceID, strconv.FormatInt(timestamp, 10), nonce, http.MethodPost, ImagePromptModelRelayPath, digestHex, generationTaskID, correlationID}, "\n")
	mac := hmac.New(sha256.New, config.secret)
	_, _ = mac.Write([]byte(canonical))
	return digestHex, "v1=" + hex.EncodeToString(mac.Sum(nil))
}
