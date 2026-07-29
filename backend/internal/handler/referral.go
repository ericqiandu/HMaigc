package handler

import (
	"net/http"
	"strconv"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterReferralRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/referrals/me", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		center, err := svc.ReferralCenter(user, page, limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, center)
	})

	r.GET("/admin/referral-program", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		program, err := svc.AdminReferralProgram(actor, page, limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, program)
	})

	r.PATCH("/admin/referral-program", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var request service.ReferralProgramSetting
		if err := c.ShouldBindJSON(&request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		program, err := svc.UpdateReferralProgram(actor, request)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"program": program})
	})

	r.PUT("/admin/referral-program/rules/:planId", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var request service.UpdateReferralRewardRuleRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		rule, err := svc.UpdateReferralRewardRule(actor, c.Param("planId"), request)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"rule": rule})
	})

	r.POST("/admin/referral-program/relationships/:id/disqualify", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var request service.DisqualifyReferralRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		if err := svc.DisqualifyReferral(actor, c.Param("id"), request); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})
}
