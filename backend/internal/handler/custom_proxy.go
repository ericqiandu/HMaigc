package handler

import (
	"errors"
	"net/http"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

var errCustomRelayDisabled = errors.New("用户自定义 API 已停用，请由管理员在后台配置系统模型渠道")

func RegisterCustomRelayRoutes(r *gin.RouterGroup, _ *service.Service) {
	r.Any("/ai/custom", func(c *gin.Context) {
		fail(c, http.StatusForbidden, errCustomRelayDisabled)
	})
}
