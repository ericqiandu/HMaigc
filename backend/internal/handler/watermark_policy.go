package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const watermarkPublicationBodyLimit = 256 << 10
const watermarkPreferenceBodyLimit = 4 << 10

func RegisterWatermarkPolicyRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/me/watermark-preference", watermarkPolicySecurityHeaders(), func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		view, err := svc.WatermarkPreference(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, view)
	})
	r.PUT("/me/watermark-preference", watermarkPolicySecurityHeaders(), func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		request, err := decodeStrictJSON[service.UpdateWatermarkPreferenceRequest](c, watermarkPreferenceBodyLimit)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		view, err := svc.UpdateWatermarkPreference(user, request)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, view)
	})
	r.GET("/admin/legal/ai-watermark-policy", watermarkPolicySecurityHeaders(), func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		publication, err := svc.AdminWatermarkPolicy(actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, publication)
	})
	r.POST("/admin/legal/ai-watermark-policy/publications", watermarkPolicySecurityHeaders(), func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		request, err := decodeStrictJSON[service.PublishWatermarkPolicyRequest](c, watermarkPublicationBodyLimit)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		publication, err := svc.PublishWatermarkPolicy(actor, request)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, publication)
	})
}

func watermarkPolicySecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Header("Pragma", "no-cache")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	}
}

func decodeStrictJSON[T any](c *gin.Context, limit int64) (T, error) {
	var value T
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return value, errors.New("请求体只能包含一个 JSON 对象")
	}
	return value, nil
}
