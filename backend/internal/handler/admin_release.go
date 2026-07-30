package handler

import (
	"github.com/gin-gonic/gin"
	"infinite-canvas/backend/internal/service"
)

func RegisterAdminReleaseRoutes(r *gin.RouterGroup, svc *service.Service, changelogPath string) {
	r.GET("/admin/releases/changelog", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.AdminReleaseNotes(user, changelogPath)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
}
