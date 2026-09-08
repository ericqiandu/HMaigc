package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const imagePromptModelRelayBodyLimit int64 = 1 << 20

type imagePromptModelRelayFailureResponse struct {
	Code             string `json:"code"`
	Reason           string `json:"reason"`
	GenerationTaskID string `json:"generationTaskId,omitempty"`
	CorrelationID    string `json:"correlationId,omitempty"`
}

type imagePromptModelRelayService interface {
	VerifyImagePromptModelRelayAuth(context.Context, service.ImagePromptModelRelayAuthInput) error
	ExecuteImagePromptModelRelay(context.Context, service.ImagePromptModelRelayRequestV1) (service.ImagePromptModelRelayResponseV1, error)
}

func RegisterImagePromptModelRelayRoutes(r *gin.RouterGroup, svc imagePromptModelRelayService) {
	r.POST("/model-relays/image-prompt/chat-completions", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, imagePromptModelRelayBodyLimit)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeImagePromptModelRelayFailure(c, &service.ImagePromptModelRelayError{Code: "specialist_request_invalid", Reason: "模型中继请求正文无效", HTTPStatus: http.StatusBadRequest}, "", "")
			return
		}
		authInput := service.ImagePromptModelRelayAuthInput{
			ServiceID: c.GetHeader(service.ImagePromptModelRelayHeaderServiceID), Timestamp: c.GetHeader(service.ImagePromptModelRelayHeaderTimestamp),
			Nonce: c.GetHeader(service.ImagePromptModelRelayHeaderNonce), BodySHA256: c.GetHeader(service.ImagePromptModelRelayHeaderBodySHA256),
			GenerationTaskID: c.GetHeader(service.ImagePromptModelRelayHeaderGenerationTaskID), CorrelationID: c.GetHeader(service.ImagePromptModelRelayHeaderCorrelationID),
			Signature: c.GetHeader(service.ImagePromptModelRelayHeaderSignature), Body: body,
		}
		if err := svc.VerifyImagePromptModelRelayAuth(c.Request.Context(), authInput); err != nil {
			writeImagePromptModelRelayFailure(c, err, authInput.GenerationTaskID, authInput.CorrelationID)
			return
		}
		var request service.ImagePromptModelRelayRequestV1
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || ensureImagePromptModelRelayJSONEOF(decoder) != nil {
			writeImagePromptModelRelayFailure(c, &service.ImagePromptModelRelayError{Code: "specialist_request_invalid", Reason: "模型中继请求契约无效", HTTPStatus: http.StatusBadRequest}, authInput.GenerationTaskID, authInput.CorrelationID)
			return
		}
		if request.CommercialContext.GenerationTaskID != authInput.GenerationTaskID || request.CorrelationID != authInput.CorrelationID {
			writeImagePromptModelRelayFailure(c, &service.ImagePromptModelRelayError{Code: "specialist_auth_invalid", Reason: "模型中继签名绑定与正文不一致", HTTPStatus: http.StatusUnauthorized}, authInput.GenerationTaskID, authInput.CorrelationID)
			return
		}
		response, err := svc.ExecuteImagePromptModelRelay(c.Request.Context(), request)
		if err != nil {
			writeImagePromptModelRelayFailure(c, err, request.CommercialContext.GenerationTaskID, request.CorrelationID)
			return
		}
		c.JSON(http.StatusOK, response)
	})
}

func ensureImagePromptModelRelayJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("JSON 包含多个值")
	}
	return err
}

func writeImagePromptModelRelayFailure(c *gin.Context, err error, generationTaskID string, correlationID string) {
	failure := &service.ImagePromptModelRelayError{}
	if !errors.As(err, &failure) {
		failure = &service.ImagePromptModelRelayError{Code: "specialist_unavailable", Reason: "图片提示词模型中继不可用", HTTPStatus: http.StatusServiceUnavailable, Cause: err}
	}
	c.JSON(failure.StatusCode(), imagePromptModelRelayFailureResponse{
		Code: failure.StableCode(), Reason: failure.SafeReason(), GenerationTaskID: strings.TrimSpace(generationTaskID), CorrelationID: strings.TrimSpace(correlationID),
	})
}
