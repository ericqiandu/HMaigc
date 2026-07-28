package handler

import (
	"net/http"

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
}
