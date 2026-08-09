package handler

import (
	"net/http"
	"strconv"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterCreditStoreRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/credit-store", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		storefront, err := svc.CreditStorefront(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, storefront)
	})
	r.GET("/credit-store/orders", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
		items, total, err := svc.MyCreditTopupOrders(user, page, limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"items": items, "total": total, "page": page, "limit": limit})
	})
	r.POST("/credit-store/orders", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.CreateCreditTopupOrderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		order, err := svc.CreateCreditTopupOrder(user, req, c.GetHeader("Idempotency-Key"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, order)
	})
	r.POST("/credit-store/orders/:id/cancel", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		order, err := svc.CancelCreditTopupOrder(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, order)
	})
	r.GET("/admin/credit-store/products", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		products, err := svc.AdminCreditTopupProducts(actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"items": products})
	})
	r.PUT("/admin/credit-store/products/:id", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.SaveCreditTopupProductRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		product, err := svc.SaveCreditTopupProduct(actor, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, product)
	})
	r.POST("/admin/credit-store/products", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.SaveCreditTopupProductRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		product, err := svc.SaveCreditTopupProduct(actor, "", req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, product)
	})
	r.GET("/admin/credit-store/orders", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
		items, total, err := svc.AdminCreditTopupOrders(actor, page, limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"items": items, "total": total, "page": page, "limit": limit})
	})
}
