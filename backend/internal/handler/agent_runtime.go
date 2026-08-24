package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const agentRuntimeRequestLimit = 256 * 1024

type createAgentThreadRequest struct {
	CanvasID string `json:"canvasId"`
}

type startAgentRunRequest struct {
	ClientRequestID string `json:"clientRequestId"`
	UserMessage     string `json:"userMessage"`
	MaxSteps        int    `json:"maxSteps"`
	Configuration   struct {
		GenerationModels agentruntime.GenerationModelSelections `json:"generationModels"`
		SkillDirs        []string                               `json:"skillDirs"`
		Attachments      []struct {
			ResourceID string `json:"resourceId"`
			Name       string `json:"name"`
		} `json:"attachments"`
		ExecutionMode agentruntime.ExecutionMode `json:"executionMode"`
	} `json:"configuration"`
}

type submitAgentApprovalRequest struct {
	ToolCallID    string                            `json:"toolCallId"`
	ActionVersion int                               `json:"actionVersion"`
	Decision      agentruntime.ToolApprovalDecision `json:"decision"`
}

type submitAgentClarificationResponseRequest struct {
	ExpectedStateVersion int                                   `json:"expectedStateVersion"`
	QuestionID           string                                `json:"questionId"`
	Answer               agentruntime.ClarificationAnswerInput `json:"answer"`
	Complete             bool                                  `json:"complete"`
}

type steerAgentRunRequest struct {
	ClientRequestID      string `json:"clientRequestId"`
	Message              string `json:"message"`
	ExpectedStateVersion int    `json:"expectedStateVersion"`
}

type interruptAgentRunRequest struct {
	ExpectedStateVersion int `json:"expectedStateVersion"`
}

type agentRuntimeRequest interface {
	createAgentThreadRequest | startAgentRunRequest | submitAgentApprovalRequest | submitAgentClarificationResponseRequest |
		steerAgentRunRequest | interruptAgentRunRequest
}

func RegisterAgentRuntimeRoutes(r *gin.RouterGroup, svc *service.Service) {
	agent := r.Group("/agent", agentRuntimeSecurityHeaders())
	agent.GET("/threads", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		canvasID := strings.TrimSpace(c.Query("canvasId"))
		limit, err := strictAgentThreadHistoryLimit(c.Query("limit"))
		if canvasID == "" || err != nil {
			fail(c, http.StatusBadRequest, errors.New("Agent 会话历史查询参数无效"))
			return
		}
		view, err := svc.ListAgentThreads(user, canvasID, limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, view)
	})
	agent.POST("/threads", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request createAgentThreadRequest
		if err := decodeStrictAgentRequest(c, &request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		thread, err := svc.CreateAgentThread(user, request.CanvasID)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, thread)
	})
	agent.POST("/threads/:threadId/runs", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request startAgentRunRequest
		if err := decodeStrictAgentRequest(c, &request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		attachments := make([]service.AgentRuntimeResourceInput, 0, len(request.Configuration.Attachments))
		for _, attachment := range request.Configuration.Attachments {
			attachments = append(attachments, service.AgentRuntimeResourceInput{ResourceID: attachment.ResourceID, Name: attachment.Name})
		}
		view, err := svc.StartScopedAgentRun(user, c.Param("threadId"), service.StartScopedAgentRunInput{
			Context: c.Request.Context(), ClientRequestID: request.ClientRequestID,
			UserMessage: request.UserMessage, MaxSteps: request.MaxSteps,
			Configuration: service.AgentRuntimeConfigurationInput{
				GenerationModels: request.Configuration.GenerationModels,
				SkillDirs:        request.Configuration.SkillDirs,
				Attachments:      attachments,
				ExecutionMode:    request.Configuration.ExecutionMode,
			},
		})
		if err != nil {
			if failAgentControl(c, err) {
				return
			}
			failService(c, err)
			return
		}
		ok(c, view)
	})
	agent.GET("/runs/:runId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		view, err := svc.ReadScopedAgentRun(user, c.Param("runId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, view)
	})
	agent.GET("/runs/:runId/events", agentRuntimeEventStream(svc))
	agent.POST("/runs/:runId/steer", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request steerAgentRunRequest
		if err := decodeStrictAgentRequest(c, &request); err != nil {
			failAgentControl(c, &service.AgentControlError{
				Status: http.StatusBadRequest, ErrorCode: "agent_steer_conflict", Message: err.Error(),
			})
			return
		}
		view, err := svc.SubmitScopedAgentSteer(user, c.Param("runId"), agentruntime.SteerRequest{
			ClientRequestID: request.ClientRequestID, Message: request.Message,
			ExpectedStateVersion: request.ExpectedStateVersion,
		})
		if err != nil {
			if failAgentControl(c, err) {
				return
			}
			failService(c, err)
			return
		}
		ok(c, view)
	})
	agent.POST("/runs/:runId/interrupt", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request interruptAgentRunRequest
		if err := decodeStrictAgentRequest(c, &request); err != nil {
			failAgentControl(c, &service.AgentControlError{
				Status: http.StatusBadRequest, ErrorCode: "agent_interrupt_conflict", Message: err.Error(),
			})
			return
		}
		view, err := svc.SubmitScopedAgentInterrupt(user, c.Param("runId"), request.ExpectedStateVersion)
		if err != nil {
			if failAgentControl(c, err) {
				return
			}
			failService(c, err)
			return
		}
		ok(c, view)
	})
	agent.POST("/runs/:runId/approvals", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request submitAgentApprovalRequest
		if err := decodeStrictAgentRequest(c, &request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		view, err := svc.SubmitScopedAgentApproval(user, c.Param("runId"), service.AgentToolApprovalSubmission{
			ToolCallID: request.ToolCallID, ActionVersion: request.ActionVersion, Decision: request.Decision,
		})
		if err != nil {
			if failAgentControl(c, err) {
				return
			}
			failService(c, err)
			return
		}
		ok(c, view)
	})
	agent.POST("/runs/:runId/clarifications/:requestId/responses", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request submitAgentClarificationResponseRequest
		if err := decodeStrictAgentRequest(c, &request); err != nil {
			failAgentClarification(c, &service.AgentClarificationError{
				Status: http.StatusBadRequest, ErrorCode: "agent_clarification_invalid", Message: err.Error(),
			})
			return
		}
		view, err := svc.SubmitScopedAgentClarificationResponse(user, c.Param("runId"), c.Param("requestId"), agentruntime.ClarificationResponseSubmission{
			ExpectedStateVersion: request.ExpectedStateVersion, QuestionID: request.QuestionID,
			Answer: request.Answer, Complete: request.Complete,
		})
		if err != nil {
			if failAgentClarification(c, err) {
				return
			}
			if failAgentControl(c, err) {
				return
			}
			failService(c, err)
			return
		}
		ok(c, view)
	})
}

func failAgentControl(c *gin.Context, err error) bool {
	var controlErr *service.AgentControlError
	if !errors.As(err, &controlErr) {
		return false
	}
	data := gin.H{"errorCode": controlErr.ErrorCode}
	if controlErr.LatestStateVersion > 0 {
		data["latestStateVersion"] = controlErr.LatestStateVersion
	}
	c.JSON(controlErr.Status, gin.H{"code": controlErr.Status, "data": data, "msg": controlErr.Message})
	return true
}

func failAgentClarification(c *gin.Context, err error) bool {
	var clarificationErr *service.AgentClarificationError
	if !errors.As(err, &clarificationErr) {
		return false
	}
	data := gin.H{"errorCode": clarificationErr.ErrorCode}
	if clarificationErr.LatestStateVersion > 0 {
		data["latestStateVersion"] = clarificationErr.LatestStateVersion
	}
	c.JSON(clarificationErr.Status, gin.H{"code": clarificationErr.Status, "data": data, "msg": clarificationErr.Message})
	return true
}

func strictAgentThreadHistoryLimit(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	if strings.TrimSpace(raw) != raw {
		return 0, errors.New("Agent 会话历史数量无效")
	}
	for _, digit := range raw {
		if digit < '0' || digit > '9' {
			return 0, errors.New("Agent 会话历史数量无效")
		}
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 20 {
		return 0, errors.New("Agent 会话历史数量无效")
	}
	return limit, nil
}

func agentRuntimeSecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Header("Pragma", "no-cache")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func decodeStrictAgentRequest[T agentRuntimeRequest](c *gin.Context, target *T) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, agentRuntimeRequestLimit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("Agent 请求格式无效")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Agent 请求只能包含一个 JSON 对象")
	}
	return nil
}

func agentRuntimeEventStream(svc *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		afterSequence, err := strconv.ParseInt(strings.TrimSpace(c.DefaultQuery("afterSequence", "0")), 10, 64)
		if err != nil || afterSequence < 0 {
			fail(c, http.StatusBadRequest, errors.New("Agent 事件游标无效"))
			return
		}
		events, view, err := svc.ReadScopedAgentEvents(user, c.Param("runId"), afterSequence, 100)
		if err != nil {
			if failAgentRuntimeProtocol(c, err) {
				return
			}
			failService(c, err)
			return
		}
		c.Header("Content-Type", "text/event-stream; charset=utf-8")
		c.Header("Cache-Control", "private, no-cache, no-store")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)
		if err := writeAgentRuntimeEvents(c, events, &afterSequence); err != nil {
			return
		}
		if agentRuntimeStatusTerminal(view.Run.Status) {
			return
		}

		poll := time.NewTicker(500 * time.Millisecond)
		heartbeat := time.NewTicker(15 * time.Second)
		defer poll.Stop()
		defer heartbeat.Stop()
		for {
			select {
			case <-c.Request.Context().Done():
				return
			case <-heartbeat.C:
				if _, err := fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
					return
				}
				c.Writer.Flush()
			case <-poll.C:
				events, view, err = svc.ReadScopedAgentEvents(user, c.Param("runId"), afterSequence, 100)
				if err != nil {
					return
				}
				if err := writeAgentRuntimeEvents(c, events, &afterSequence); err != nil {
					return
				}
				if agentRuntimeStatusTerminal(view.Run.Status) {
					return
				}
			}
		}
	}
}

func failAgentRuntimeProtocol(c *gin.Context, err error) bool {
	status := 0
	errorCode := ""
	message := ""
	switch {
	case errors.Is(err, service.ErrAgentStreamCursorInvalid):
		status = http.StatusBadRequest
		errorCode = "agent_stream_cursor_invalid"
		message = "Agent 事件游标无效"
	case errors.Is(err, service.ErrAgentEventProjectionFailed):
		status = http.StatusInternalServerError
		errorCode = "agent_event_projection_failed"
		message = "Agent 事件投影失败"
	default:
		return false
	}
	c.JSON(status, gin.H{"code": status, "data": gin.H{"errorCode": errorCode}, "msg": message})
	return true
}

func writeAgentRuntimeEvents(c *gin.Context, events []service.AgentUIEvent, cursor *int64) error {
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Kind, encoded); err != nil {
			return err
		}
		*cursor = event.Sequence
	}
	c.Writer.Flush()
	return nil
}

func agentRuntimeStatusTerminal(status agentruntime.RunStatus) bool {
	return status == agentruntime.RunSucceeded || status == agentruntime.RunFailed || status == agentruntime.RunCancelled
}
