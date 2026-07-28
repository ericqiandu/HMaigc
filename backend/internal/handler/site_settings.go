package handler

import (
	"errors"
	"net/http"
	"os"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const siteSettingBodyLimit = 256 << 10
const siteLogoUploadBodyLimit = (2 << 20) + (1 << 20)

func RegisterSiteSettingRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/public/site", func(c *gin.Context) {
		result, err := svc.PublicSiteSetting()
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.GET("/public/site/logo", func(c *gin.Context) {
		filePath, mimeType, modifiedAt, err := svc.SiteLogoFile()
		if err != nil {
			if errors.Is(err, service.ErrSiteLogoNotConfigured) || errors.Is(err, os.ErrNotExist) {
				fail(c, http.StatusNotFound, service.ErrSiteLogoNotConfigured)
				return
			}
			failService(c, err)
			return
		}
		c.Header("Cache-Control", "public, max-age=86400, immutable")
		c.Header("Content-Type", mimeType)
		c.Header("Last-Modified", modifiedAt.UTC().Format(http.TimeFormat))
		c.Header("X-Content-Type-Options", "nosniff")
		http.ServeFile(c.Writer, c.Request, filePath)
	})

	r.GET("/admin/settings/site", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.AdminSiteSetting(actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.PUT("/admin/settings/site", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, siteSettingBodyLimit)
		var req service.SiteSettingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.UpdateSiteSetting(actor, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.POST("/admin/settings/site/logo", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, siteLogoUploadBodyLimit)
		file, err := c.FormFile("file")
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.UpdateSiteLogo(actor, file)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.DELETE("/admin/settings/site/logo", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.RemoveSiteLogo(actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
}
