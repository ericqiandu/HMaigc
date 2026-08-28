package handler

import (
	"net/http"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type stageReviewRequest struct {
	StageVersion      int64                                `json:"stageVersion"`
	RevisionID        string                               `json:"revisionId"`
	Decision          agentruntime.StageReviewDecision     `json:"decision"`
	ClientRequestID   string                               `json:"clientRequestId"`
	Comment           string                               `json:"comment"`
	PublicationIntent *agentruntime.AssetPublicationIntent `json:"publicationIntent,omitempty"`
}

func registerAgentProductionRoutes(agent *gin.RouterGroup, svc *service.Service) {
	agent.POST("/runs/:runId/stages/:stageId/reviews", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request stageReviewRequest
		if err := decodeStrictAgentRequest(c, &request); err != nil {
			failAgentControl(c, &service.AgentControlError{
				Status: http.StatusBadRequest, ErrorCode: "stage_review_invalid", Message: err.Error(),
			})
			return
		}
		view, err := svc.ReviewScopedProductionStage(
			c.Request.Context(), user, c.Param("runId"), c.Param("stageId"),
			agentruntime.StageReviewCommand{
				StageVersion: request.StageVersion, RevisionID: request.RevisionID,
				Decision: request.Decision, ClientRequestID: request.ClientRequestID, Comment: request.Comment,
				PublicationIntent: request.PublicationIntent,
			},
		)
		if err != nil {
			if failAgentControl(c, err) {
				return
			}
			failService(c, err)
			return
		}
		ok(c, view)
	})

	agent.GET("/runs/:runId/artifacts/:artifactId/revisions/:revisionId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		view, err := svc.ReadScopedAgentArtifactRevision(
			user, c.Param("runId"), c.Param("artifactId"), c.Param("revisionId"),
		)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, view)
	})
}
