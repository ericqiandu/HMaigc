package handler

import (
	"net/http"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterAdminStorageMigrationRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/admin/storage-migrations/overview", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		overview, err := svc.AdminStorageMigrationOverview(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, overview)
	})

	r.POST("/admin/storage-migrations", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request service.StartStorageMigrationRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		job, err := svc.StartStorageMigration(user, request)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, job)
	})

	r.POST("/admin/storage-migrations/:id/retry", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var request struct {
			Confirmation string `json:"confirmation"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		job, err := svc.RetryStorageMigration(user, c.Param("id"), request.Confirmation)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, job)
	})
}
