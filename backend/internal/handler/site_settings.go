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
const siteMarketingImageUploadBodyLimit = (8 << 20) + (1 << 20)

func RegisterSiteSettingRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/public/site", func(c *gin.Context) {
		result, err := svc.PublicSiteShellSetting()
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.GET("/public/legal/:document", func(c *gin.Context) {
		result, err := svc.PublicLegalDocument(c.Param("document"))
		if err != nil {
			if errors.Is(err, service.ErrUnsupportedLegalDocument) {
				fail(c, http.StatusBadRequest, err)
				return
			}
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

	r.GET("/public/site/marketing-image", func(c *gin.Context) {
		filePath, mimeType, modifiedAt, err := svc.MarketingPopupImageFile()
		if err != nil {
			if errors.Is(err, service.ErrSiteMarketingImageNotConfigured) || errors.Is(err, os.ErrNotExist) {
				fail(c, http.StatusNotFound, service.ErrSiteMarketingImageNotConfigured)
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

	r.PUT("/admin/settings/legal", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, siteSettingBodyLimit)
		var req service.LegalContentSettingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.UpdateLegalContentSetting(actor, req)
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

	r.POST("/admin/settings/site/marketing-image", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, siteMarketingImageUploadBodyLimit)
		file, err := c.FormFile("file")
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.UpdateMarketingPopupImage(actor, file)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.DELETE("/admin/settings/site/marketing-image", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.RemoveMarketingPopupImage(actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
}
