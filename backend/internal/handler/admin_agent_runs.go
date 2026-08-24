package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const adminAgentRunInterruptBodyLimit = 8 << 10

type adminAgentRunErrorData struct {
	ErrorCode string                          `json:"errorCode"`
	LatestRun *repository.AdminAgentRunRecord `json:"latestRun,omitempty"`
}

type adminAgentRunErrorEnvelope struct {
	Code int                    `json:"code"`
	Data adminAgentRunErrorData `json:"data"`
	Msg  string                 `json:"msg"`
}

func RegisterAdminAgentRunRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/admin/agent-runs", func(c *gin.Context) {
		user, err := currentAdminAgentRunUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		query, err := parseAdminAgentRunQuery(c)
		if err != nil {
			failAdminAgentRun(c, err)
			return
		}
		page, err := svc.AdminAgentRuns(c.Request.Context(), user, query)
		if err != nil {
			failAdminAgentRun(c, err)
			return
		}
		ok(c, page)
	})

	r.GET("/admin/agent-runs/:runId", func(c *gin.Context) {
		user, err := currentAdminAgentRunUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		record, err := svc.AdminAgentRun(c.Request.Context(), user, c.Param("runId"))
		if err != nil {
			failAdminAgentRun(c, err)
			return
		}
		ok(c, record)
	})

	r.POST("/admin/agent-runs/:runId/interrupt", func(c *gin.Context) {
		user, err := currentAdminAgentRunUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request service.AdminAgentRunInterruptRequest
		if err := decodeStrictAdminAgentRunInterrupt(c, &request); err != nil {
			failAdminAgentRun(c, err)
			return
		}
		request.RunID = c.Param("runId")
		response, err := svc.InterruptAdminAgentRun(c.Request.Context(), user, request)
		if err != nil {
			failAdminAgentRun(c, err)
			return
		}
		ok(c, response)
	})
}

func currentAdminAgentRunUser(c *gin.Context, svc *service.Service) (*model.User, error) {
	user, err := currentUser(c, svc)
	if err != nil {
		return nil, err
	}
	if err := svc.RequireAdmin(user); err != nil {
		return nil, err
	}
	return user, nil
}

func parseAdminAgentRunQuery(c *gin.Context) (repository.AdminAgentRunQuery, error) {
	page, err := parsePositiveAdminAgentRunInteger(c.DefaultQuery("page", "1"))
	if err != nil {
		return repository.AdminAgentRunQuery{}, repository.ErrAdminAgentRunQueryInvalid
	}
	pageSize, err := parsePositiveAdminAgentRunInteger(c.DefaultQuery("pageSize", "20"))
	if err != nil {
		return repository.AdminAgentRunQuery{}, repository.ErrAdminAgentRunQueryInvalid
	}
	query := repository.AdminAgentRunQuery{
		Status:   agentruntime.RunStatus(strings.TrimSpace(c.Query("status"))),
		Activity: repository.AdminAgentRunActivityClassification(strings.TrimSpace(c.Query("activity"))),
		User:     c.Query("user"), Scope: c.Query("scope"), Page: page, PageSize: pageSize,
	}
	if raw := strings.TrimSpace(c.Query("updatedBefore")); raw != "" {
		updatedBefore, parseErr := time.Parse(time.RFC3339Nano, raw)
		if parseErr != nil {
			return repository.AdminAgentRunQuery{}, repository.ErrAdminAgentRunQueryInvalid
		}
		utc := updatedBefore.UTC()
		query.UpdatedBefore = &utc
	}
	return query, nil
}

func parsePositiveAdminAgentRunInteger(raw string) (int, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return 0, repository.ErrAdminAgentRunQueryInvalid
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return 0, repository.ErrAdminAgentRunQueryInvalid
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, repository.ErrAdminAgentRunQueryInvalid
	}
	return value, nil
}

func decodeStrictAdminAgentRunInterrupt(c *gin.Context, target *service.AdminAgentRunInterruptRequest) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, adminAgentRunInterruptBodyLimit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &service.AdminAgentRunControlError{
			Status: http.StatusBadRequest, ErrorCode: "admin_agent_run_interrupt_blocked", Message: "Agent 终止请求格式无效",
		}
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return &service.AdminAgentRunControlError{
			Status: http.StatusBadRequest, ErrorCode: "admin_agent_run_interrupt_blocked", Message: "Agent 终止请求只能包含一个 JSON 对象",
		}
	}
	return nil
}

func failAdminAgentRun(c *gin.Context, err error) {
	var controlErr *service.AdminAgentRunControlError
	if errors.As(err, &controlErr) {
		c.JSON(controlErr.Status, adminAgentRunErrorEnvelope{
			Code: controlErr.Status,
			Data: adminAgentRunErrorData{ErrorCode: controlErr.ErrorCode, LatestRun: controlErr.Latest},
			Msg:  controlErr.Message,
		})
		return
	}
	if errors.Is(err, repository.ErrAdminAgentRunQueryInvalid) {
		c.JSON(http.StatusBadRequest, adminAgentRunErrorEnvelope{
			Code: http.StatusBadRequest,
			Data: adminAgentRunErrorData{ErrorCode: "admin_agent_run_query_invalid"},
			Msg:  "Agent 运行查询参数无效",
		})
		return
	}
	failService(c, err)
}
