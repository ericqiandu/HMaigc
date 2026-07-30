package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const channelModelIconUploadBodyLimit = (1 << 20) + (1 << 20)
const channelVoiceCloneUploadBodyLimit = (20 << 20) + (2 << 20)

func RegisterFinanceRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/channels/:id/voices", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		voices, err := svc.UserChannelVoices(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"voices": voices})
	})
	r.PUT("/channels/:id/voices/:voiceId/favorite", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<10)
		var req struct {
			Favorite bool `json:"favorite"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		voice, err := svc.SetUserChannelVoiceFavorite(user, c.Param("id"), c.Param("voiceId"), req.Favorite)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"voice": voice})
	})
	r.POST("/channels/:id/voices/clone", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "channel-voice-clone:"+user.ID+":"+c.Param("id"), 3, time.Hour) {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, channelVoiceCloneUploadBodyLimit)
		fileHeader, err := c.FormFile("file")
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		file, err := fileHeader.Open()
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		defer file.Close()
		voice, err := svc.CloneUserMiniMaxChannelVoice(c.Request.Context(), user, c.Param("id"), service.CloneUserChannelVoiceRequest{
			DisplayName: c.PostForm("displayName"), Language: c.PostForm("language"),
			ConsentConfirmed: strings.EqualFold(c.PostForm("consentConfirmed"), "true"),
			IdempotencyKey:   c.PostForm("idempotencyKey"),
		}, file, fileHeader.Filename)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"voice": voice})
	})
	r.GET("/wallet", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
		wallet, err := svc.Wallet(user, c.Query("type"), page, limit)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, wallet)
	})
	r.POST("/channels/:id/voices/:voiceId/preview", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "channel-voice-preview:"+user.ID, 20, time.Minute) {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var req service.ChannelVoicePreviewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		preview, err := svc.MiniMaxChannelVoicePreview(c.Request.Context(), user, c.Param("id"), c.Param("voiceId"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"preview": preview})
	})
	r.POST("/wallet/redeem", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "redeem:"+user.ID, 10, time.Hour) {
			return
		}
		var req struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		account, err := svc.RedeemCredits(user, req.Code, c.ClientIP())
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"account": account})
	})
	r.POST("/wallet/checkin", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		account, granted, err := svc.CheckinCredits(user)
		if err != nil {
			failService(c, err)
			return
		}
		if !granted {
			fail(c, http.StatusConflict, service.BadAuthRequest("今天已经签到过了"))
			return
		}
		ok(c, gin.H{"account": account, "granted": true})
	})

	r.GET("/admin/settings/linuxdo", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		setting, err := svc.AdminLinuxDOSetting(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"setting": setting})
	})
	r.PATCH("/admin/settings/linuxdo", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.LinuxDOSettingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		setting, err := svc.UpdateLinuxDOSetting(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"setting": setting})
	})
	r.GET("/admin/settings/registration", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		setting, err := svc.AdminRegistrationSetting(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"setting": setting})
	})
	r.PATCH("/admin/settings/registration", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.RegistrationSettingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		setting, err := svc.UpdateRegistrationSetting(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"setting": setting})
	})
	r.GET("/admin/settings/credits", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		policy, err := svc.AdminCreditPolicy(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"policy": policy})
	})
	r.PATCH("/admin/settings/credits", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.CreditPolicy
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		policy, err := svc.UpdateCreditPolicy(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"policy": policy})
	})
	r.GET("/admin/settings/email", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		setting, err := svc.AdminEmailSetting(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"setting": setting})
	})
	r.PATCH("/admin/settings/email", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.EmailSettingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		setting, err := svc.UpdateEmailSetting(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"setting": setting})
	})

	r.GET("/admin/channels/:id/models", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		items, err := svc.AdminChannelModels(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"models": items})
	})
	r.POST("/admin/channels/:id/models/fetch", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "admin-channel-models-fetch:"+user.ID+":"+c.Param("id"), 10, time.Minute) {
			return
		}
		// 上游密钥只在 service 内使用，handler 仅返回去重后的模型标识和新增数量。
		result, err := svc.FetchAdminChannelModels(c.Request.Context(), user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
	r.POST("/admin/channels/:id/models", func(c *gin.Context) {
		saveChannelModel(c, svc, "")
	})
	r.PATCH("/admin/channels/:id/models/:modelId", func(c *gin.Context) {
		saveChannelModel(c, svc, c.Param("modelId"))
	})
	r.POST("/admin/channels/:id/models/:modelId/icon", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, channelModelIconUploadBodyLimit)
		file, err := c.FormFile("file")
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		item, err := svc.UpdateAdminChannelModelIcon(user, c.Param("id"), c.Param("modelId"), file)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"model": item})
	})
	r.DELETE("/admin/channels/:id/models/:modelId/icon", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		item, err := svc.RemoveAdminChannelModelIcon(user, c.Param("id"), c.Param("modelId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"model": item})
	})
	r.DELETE("/admin/channels/:id/models/:modelId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeleteAdminChannelModel(user, c.Param("id"), c.Param("modelId")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})
	r.GET("/admin/channels/:id/voices", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		voices, err := svc.AdminChannelVoices(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"voices": voices})
	})
	r.POST("/admin/channels/:id/voices/sync", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "admin-channel-voices-sync:"+user.ID+":"+c.Param("id"), 6, time.Minute) {
			return
		}
		voices, err := svc.SyncMiniMaxChannelVoices(c.Request.Context(), user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"voices": voices})
	})
	r.POST("/admin/channels/:id/voices", func(c *gin.Context) {
		saveChannelVoice(c, svc, "")
	})
	r.PATCH("/admin/channels/:id/voices/:voiceId", func(c *gin.Context) {
		saveChannelVoice(c, svc, c.Param("voiceId"))
	})
	r.POST("/admin/channels/:id/voices/clone", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "admin-channel-voice-clone:"+user.ID+":"+c.Param("id"), 3, time.Minute) {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, channelVoiceCloneUploadBodyLimit)
		fileHeader, err := c.FormFile("file")
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		file, err := fileHeader.Open()
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		defer file.Close()
		var compatibleModels []string
		if value := strings.TrimSpace(c.PostForm("compatibleModels")); value != "" {
			if err := json.Unmarshal([]byte(value), &compatibleModels); err != nil {
				fail(c, http.StatusBadRequest, service.BadAuthRequest("兼容模型列表格式无效"))
				return
			}
		}
		voice, err := svc.CloneMiniMaxChannelVoice(c.Request.Context(), user, c.Param("id"), service.CloneChannelVoiceRequest{
			VoiceKey: c.PostForm("voiceKey"), DisplayName: c.PostForm("displayName"),
			Description: c.PostForm("description"), Language: c.PostForm("language"),
			AccessPolicy: model.ModelAccessPolicy(c.PostForm("accessPolicy")), CompatibleModels: compatibleModels,
			ConsentConfirmed: strings.EqualFold(c.PostForm("consentConfirmed"), "true"),
			IdempotencyKey:   c.PostForm("idempotencyKey"),
		}, file, fileHeader.Filename)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"voice": voice})
	})
	r.DELETE("/admin/channels/:id/voices/:voiceId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeleteAdminChannelVoice(c.Request.Context(), user, c.Param("id"), c.Param("voiceId")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})

	r.GET("/admin/redeem-batches", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		items, err := svc.AdminRedeemBatchPage(user, service.AdminListQuery{Keyword: c.Query("keyword"), Status: c.Query("validity"), Page: page, Limit: limit})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, items)
	})
	r.POST("/admin/redeem-batches", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "redeem-batch-create:"+user.ID, 20, time.Hour) {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		var req service.CreateRedeemBatchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.AdminCreateRedeemBatch(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		ok(c, result)
	})
	r.GET("/admin/redeem-batches/:id/codes", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		result, err := svc.AdminRedeemCodePage(user, c.Param("id"), c.Query("status"), page, limit)
		if err != nil {
			failService(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		ok(c, result)
	})
	r.POST("/admin/redeem-batches/:id/disable", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		count, err := svc.AdminDisableRedeemBatch(user, c.Param("id"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"disabledCount": count})
	})
	r.POST("/admin/redeem-batches/:id/codes/:codeId/disable", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.AdminDisableRedeemCode(user, c.Param("id"), c.Param("codeId")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"ok": true})
	})
	r.POST("/admin/users/:id/credits/adjust", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.AdminCreditAdjustmentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		account, err := svc.AdminAdjustCredits(user, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"account": account})
	})
	r.GET("/admin/billing-orders", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		items, err := svc.AdminBillingOrderPage(user, service.AdminListQuery{Keyword: c.Query("keyword"), Status: c.DefaultQuery("status", "review"), Page: page, Limit: limit})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, items)
	})
	r.POST("/admin/billing-orders/:id/resolve", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.ResolveBillingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		order, err := svc.ResolveBillingOrder(user, c.Param("id"), req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"order": order})
	})
}

func saveChannelVoice(c *gin.Context, svc *service.Service, id string) {
	user, err := currentUser(c, svc)
	if err != nil {
		failService(c, err)
		return
	}
	var req service.ChannelVoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	voice, err := svc.SaveAdminChannelVoice(user, c.Param("id"), id, req)
	if err != nil {
		failService(c, err)
		return
	}
	ok(c, gin.H{"voice": voice})
}

func saveChannelModel(c *gin.Context, svc *service.Service, id string) {
	user, err := currentUser(c, svc)
	if err != nil {
		failService(c, err)
		return
	}
	var req service.ChannelModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := svc.SaveAdminChannelModel(user, c.Param("id"), id, req)
	if err != nil {
		failService(c, err)
		return
	}
	ok(c, gin.H{"model": item})
}
