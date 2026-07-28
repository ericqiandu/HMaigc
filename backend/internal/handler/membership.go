package handler

import (
	"net/http"
	"strconv"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterMembershipRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/membership/plans", func(c *gin.Context) {
		plans, err := svc.MembershipPlans(nil)
		if err != nil {
			fail(c, http.StatusInternalServerError, err)
			return
		}
		ok(c, plans)
	})
	r.GET("/membership", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		entitlement, orders, teams, err := svc.MyMembership(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"entitlement": entitlement, "orders": orders, "teams": teams})
	})
	r.POST("/membership/orders", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.CreateMembershipOrderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		order, err := svc.CreateMembershipOrder(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, order)
	})
	r.POST("/membership/orders/:id/cancel", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		order, err := svc.CancelMembershipOrder(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, order)
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
	r.POST("/teams/:id/members", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.AddTeamMemberRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		member, err := svc.AddTeamMember(user, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, member)
	})

	r.GET("/admin/membership/plans", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		plans, err := svc.MembershipPlans(actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, plans)
	})
	r.PATCH("/admin/membership/plans/:id", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.UpdateMembershipPlanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		plan, err := svc.AdminUpdateMembershipPlan(actor, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, plan)
	})
	r.GET("/admin/membership/orders", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
		orders, total, err := svc.AdminMembershipOrders(actor, page, limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"items": orders, "total": total, "page": page, "limit": limit})
	})
	r.POST("/admin/membership/orders/:id/confirm", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.ConfirmMembershipOrderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		order, err := svc.AdminConfirmMembershipOrder(actor, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, order)
	})
	r.POST("/admin/membership/orders/:id/close", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.CloseMembershipOrderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		order, err := svc.AdminCloseMembershipOrder(actor, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, order)
	})
}
