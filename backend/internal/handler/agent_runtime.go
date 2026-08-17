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

type submitAgentToolResultRequest struct {
	ToolCallID    string                             `json:"toolCallId"`
	ActionVersion int                                `json:"actionVersion"`
	Selection     *service.AgentCanvasSelectionFacts `json:"selection,omitempty"`
}

type agentRuntimeRequest interface {
	createAgentThreadRequest | startAgentRunRequest | submitAgentApprovalRequest | submitAgentToolResultRequest
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
			failService(c, err)
			return
		}
		ok(c, view)
	})
	agent.POST("/runs/:runId/tool-results", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request submitAgentToolResultRequest
		if err := decodeStrictAgentRequest(c, &request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		view, err := svc.SubmitScopedAgentToolResult(user, c.Param("runId"), service.CoordinateAgentToolInput{
			ToolCallID: request.ToolCallID, ActionVersion: request.ActionVersion, Selection: request.Selection,
		})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, view)
	})
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

func writeAgentRuntimeEvents(c *gin.Context, events []service.AgentRuntimeEventView, cursor *int64) error {
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
