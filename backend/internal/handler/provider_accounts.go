package handler

import (
	"errors"
	"net/http"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterProviderAccountRoutes(r *gin.RouterGroup, svc *service.Service) {
	providers := r.Group("/admin/providers/kuaizi", providerAccountSecurityHeaders())
	providers.GET("", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		view, err := svc.AdminKuaiziProvider(actor)
		if err != nil {
			failProviderAccount(c, err)
			return
		}
		ok(c, view)
	})
	providers.PUT("", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var request service.SaveProviderEndpointRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			failProviderAccountParse(c, svc, actor, "provider.endpoint.save", "", err, "筷子服务地址请求格式无效")
			return
		}
		view, err := svc.SaveKuaiziEndpointCandidate(c.Request.Context(), actor, request)
		if err != nil {
			failProviderAccount(c, err)
			return
		}
		ok(c, view)
	})
	providers.PUT("/credentials/:family", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var request service.SaveProviderCredentialRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			failProviderAccountParse(c, svc, actor, "provider.credential.save", c.Param("family"), err, "筷子凭据请求格式无效")
			return
		}
		view, err := svc.SaveKuaiziCredentialCandidate(c.Request.Context(), actor, c.Param("family"), request)
		if err != nil {
			failProviderAccount(c, err)
			return
		}
		ok(c, view)
	})
	providers.POST("/credentials/:family/verify", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		view, err := svc.VerifyKuaiziCredential(c.Request.Context(), actor, c.Param("family"))
		if err != nil {
			failProviderAccount(c, err)
			return
		}
		ok(c, view)
	})
}

func providerAccountSecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "private, no-store")
		c.Header("Pragma", "no-cache")
		c.Header("Referrer-Policy", "no-referrer")
		c.Next()
	}
}

func failProviderAccount(c *gin.Context, err error) {
	if errors.Is(err, repository.ErrProviderActivationConflict) {
		fail(c, http.StatusConflict, errors.New("供应商配置已被其他管理员更新，请刷新后重试"))
		return
	}
	var verificationError *service.KuaiziVerificationError
	if errors.As(err, &verificationError) {
		if verificationError.HealthStatus == "unavailable" {
			fail(c, http.StatusServiceUnavailable, err)
			return
		}
		if verificationError.HealthStatus == "unknown" {
			fail(c, http.StatusBadGateway, err)
			return
		}
		fail(c, http.StatusBadRequest, err)
		return
	}
	failService(c, err)
}

func failProviderAccountParse(c *gin.Context, svc *service.Service, actor *model.User, action string, family string, parseError error, message string) {
	code := "invalid_json"
	var maxBytesError *http.MaxBytesError
	if errors.As(parseError, &maxBytesError) {
		code = "body_too_large"
	}
	if auditErr := svc.AuditKuaiziProviderRejection(actor, action, family, code); auditErr != nil {
		failProviderAccount(c, auditErr)
		return
	}
	fail(c, http.StatusBadRequest, errors.New(message))
}
