package handler

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterTeamRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/teams", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		workspace, err := svc.TeamWorkspace(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, workspace)
	})
	r.POST("/teams", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.CreateTeamRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		team, err := svc.CreateTeam(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, team)
	})
	r.GET("/teams/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		detail, err := svc.TeamDetail(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.PATCH("/teams/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.RenameTeamRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		team, err := svc.RenameTeam(user, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, team)
	})
	r.POST("/teams/:id/invitations", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.CreateTeamInvitationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.CreateTeamInvitation(user, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
	r.POST("/team-invitations/accept", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.AcceptTeamInvitationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		member, err := svc.AcceptTeamInvitationByToken(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, member)
	})
	r.POST("/team-invitations/:id/accept", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		member, err := svc.AcceptTeamInvitationByID(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, member)
	})
	r.DELETE("/teams/:id/invitations/:invitationId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.RevokeTeamInvitation(user, c.Param("id"), c.Param("invitationId")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"revoked": true})
	})
	r.PATCH("/teams/:id/members/:memberId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.UpdateTeamMemberRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		if err := svc.UpdateTeamMember(user, c.Param("id"), c.Param("memberId"), req); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"updated": true})
	})
	r.DELETE("/teams/:id/members/:memberId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.RemoveTeamMember(user, c.Param("id"), c.Param("memberId")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"removed": true})
	})
	r.POST("/teams/:id/leave", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.LeaveTeam(user, c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"left": true})
	})
	r.GET("/teams/:id/resources", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
		resources, err := svc.TeamResources(user.ID, c.Param("id"), limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"resources": resources})
	})
	r.POST("/teams/:id/resources", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		policy, available := loadRuntimePolicy(c, svc)
		if !available || !enforceRateLimit(c, "team-resources-upload:"+user.ID, policy.Request.ResourceUploadPerMinute, time.Minute) {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (policy.Resource.ResourceUploadMB<<20)+(1<<20))
		file, err := c.FormFile("file")
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		width, _ := strconv.Atoi(c.PostForm("width"))
		height, _ := strconv.Atoi(c.PostForm("height"))
		durationMs, _ := strconv.ParseInt(c.PostForm("durationMs"), 10, 64)
		resource, err := svc.UploadTeamResource(user.ID, c.Param("id"), file, c.PostForm("kind"), width, height, durationMs)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"resource": resource})
	})
	r.GET("/teams/:id/resources/:resourceId/file", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		resource, err := svc.TeamResource(user.ID, c.Param("id"), c.Param("resourceId"))
		if err != nil {
			failService(c, err)
			return
		}
		etag := resourceResponseETag(resource)
		c.Header("Cache-Control", "private, no-cache")
		c.Header("ETag", etag)
		c.Header("Accept-Ranges", "bytes")
		c.Header("X-Content-Type-Options", "nosniff")
		if ifNoneMatch(c.GetHeader("If-None-Match"), etag) {
			c.Status(http.StatusNotModified)
			return
		}
		rangeHeader := c.GetHeader("Range")
		if ifRange := strings.TrimSpace(c.GetHeader("If-Range")); ifRange != "" && ifRange != etag {
			rangeHeader = ""
		}
		stream, err := svc.OpenTeamResourceRange(user.ID, c.Param("id"), resource.ID, rangeHeader)
		if err != nil {
			failService(c, err)
			return
		}
		defer stream.Body.Close()
		if resource.MimeType == "" {
			resource.MimeType = "application/octet-stream"
		}
		if resource.Provider == "local" {
			if seeker, ok := stream.Body.(io.ReadSeeker); ok {
				c.Header("Content-Type", resource.MimeType)
				http.ServeContent(c.Writer, c.Request, resource.ID, resource.UpdatedAt, seeker)
				return
			}
		}
		if stream.ContentRange != "" {
			c.Header("Content-Range", stream.ContentRange)
		}
		c.DataFromReader(stream.StatusCode, stream.ContentLength, resource.MimeType, stream.Body, nil)
	})
}
