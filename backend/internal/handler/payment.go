package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const paymentWebhookBodyLimit = 1 << 20

func RegisterPaymentRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.POST("/payments/webhooks/wechat", func(c *gin.Context) {
		body, err := readPaymentWebhookBody(c)
		if err != nil {
			writeWechatWebhookFailure(c, err)
			return
		}
		err = svc.HandleWechatPaymentWebhook(service.WechatPaymentWebhookHeaders{
			Timestamp: c.GetHeader("Wechatpay-Timestamp"),
			Nonce:     c.GetHeader("Wechatpay-Nonce"),
			Signature: c.GetHeader("Wechatpay-Signature"),
		}, body)
		if err != nil {
			writeWechatWebhookFailure(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})

	r.POST("/payments/webhooks/alipay", func(c *gin.Context) {
		body, err := readPaymentWebhookBody(c)
		if err == nil {
			err = svc.HandleAlipayPaymentWebhook(body)
		}
		if err != nil {
			_ = c.Error(err)
			c.String(paymentWebhookErrorStatus(err), "failure")
			return
		}
		c.String(http.StatusOK, "success")
	})

	r.POST("/membership/orders/:id/checkout", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.CreatePaymentCheckout(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.POST("/credit-store/orders/:id/checkout", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.CreateCreditTopupCheckout(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.GET("/payments/checkout/:token", func(c *gin.Context) {
		result, err := svc.PaymentCheckout(c.Param("token"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.POST("/payments/checkout/:token/transactions", func(c *gin.Context) {
		var req service.CreatePaymentTransactionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.CreatePaymentTransaction(c.Param("token"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.GET("/admin/settings/payment", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		result, err := svc.AdminPaymentSetting(actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.PUT("/admin/settings/payment", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.PaymentSettingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.UpdatePaymentSetting(actor, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.GET("/admin/payments/transactions", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
		result, err := svc.AdminPaymentTransactions(actor, service.AdminListQuery{
			Keyword: c.Query("keyword"), Type: c.Query("provider"), Status: c.Query("status"), Page: page, Limit: limit,
		})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.GET("/admin/payments/webhooks", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
		result, err := svc.AdminPaymentWebhookEvents(actor, service.AdminListQuery{
			Type: c.Query("provider"), Status: c.Query("status"), Page: page, Limit: limit,
		})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
}

func readPaymentWebhookBody(c *gin.Context) ([]byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, paymentWebhookBodyLimit)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, errors.New("支付回调请求体无效或超过大小限制")
	}
	if len(body) == 0 {
		return nil, errors.New("支付回调请求体不能为空")
	}
	return body, nil
}

func writeWechatWebhookFailure(c *gin.Context, err error) {
	_ = c.Error(err)
	c.JSON(paymentWebhookErrorStatus(err), gin.H{
		"code":    "FAIL",
		"message": "支付通知处理失败",
	})
}

func paymentWebhookErrorStatus(err error) int {
	var authErr *service.AuthError
	if errors.As(err, &authErr) {
		return authErr.Status
	}
	return http.StatusInternalServerError
}
