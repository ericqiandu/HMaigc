package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

const CreditScale int64 = 1_000_000

type WalletSummary struct {
	Account model.CreditAccount       `json:"account"`
	Entries []model.CreditLedgerEntry `json:"entries"`
	Total   int64                     `json:"total"`
	Page    int                       `json:"page"`
	Limit   int                       `json:"limit"`
	Policy  PublicCreditPolicy        `json:"policy"`
}

type RedeemBatchPage struct {
	Batches []model.RedeemBatch `json:"batches"`
	Total   int64               `json:"total"`
	Page    int                 `json:"page"`
	Limit   int                 `json:"limit"`
}

type AdminRedeemCodeDetail struct {
	ID                  string     `json:"id"`
	Code                string     `json:"code,omitempty"`
	CodeSuffix          string     `json:"codeSuffix"`
	Status              string     `json:"status"`
	RedeemedBy          string     `json:"redeemedBy,omitempty"`
	RedeemedUsername    string     `json:"redeemedUsername,omitempty"`
	RedeemedDisplayName string     `json:"redeemedDisplayName,omitempty"`
	RedeemedAt          *time.Time `json:"redeemedAt"`
	RedeemedIP          string     `json:"redeemedIp,omitempty"`
	ExpiresAt           *time.Time `json:"expiresAt"`
	AmountMicrocredits  int64      `json:"amountMicrocredits"`
}

type AdminRedeemCodePage struct {
	Batch              model.RedeemBatch       `json:"batch"`
	Codes              []AdminRedeemCodeDetail `json:"codes"`
	PlaintextAvailable bool                    `json:"plaintextAvailable"`
	Total              int64                   `json:"total"`
	Page               int                     `json:"page"`
	Limit              int                     `json:"limit"`
}

type BillingOrderPage struct {
	Orders []model.BillingOrder `json:"orders"`
	Total  int64                `json:"total"`
	Page   int                  `json:"page"`
	Limit  int                  `json:"limit"`
}

type CreateRedeemBatchRequest struct {
	AmountMicrocredits int64      `json:"amountMicrocredits"`
	Count              int        `json:"count"`
	Note               string     `json:"note"`
	ExpiresAt          *time.Time `json:"expiresAt"`
}

type CreateRedeemBatchResult struct {
	Batch model.RedeemBatch `json:"batch"`
	Codes []string          `json:"codes"`
}

type AdminCreditAdjustmentRequest struct {
	AmountMicrocredits int64  `json:"amountMicrocredits"`
	Note               string `json:"note"`
}

type ResolveBillingRequest struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

func (s *Service) Wallet(user *model.User, entryType string, page int, limit int) (*WalletSummary, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	account, err := s.repo.CreditAccount(user.ID)
	if err != nil {
		return nil, err
	}
	entries, total, err := s.repo.CreditLedger(user.ID, strings.TrimSpace(entryType), limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	policy, err := s.publicCreditPolicy(user.ID)
	if err != nil {
		return nil, err
	}
	return &WalletSummary{Account: *account, Entries: entries, Total: total, Page: page, Limit: limit, Policy: policy}, nil
}

func (s *Service) RedeemCredits(user *model.User, code string, redeemedIP string) (*model.CreditAccount, error) {
	if user == nil {
		return nil, Unauthorized("请先登录")
	}
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) != 32 {
		return nil, BadAuthRequest("兑换码无效或已使用")
	}
	account, err := s.repo.RedeemCode(user.ID, hashRedeemCode(code), truncateRunes(strings.TrimSpace(redeemedIP), 64))
	if errors.Is(err, repository.ErrRedeemCodeInvalid) {
		return nil, BadAuthRequest("兑换码无效或已使用")
	}
	return account, err
}

func (s *Service) AdminCreateRedeemBatch(actor *model.User, req CreateRedeemBatchRequest) (*CreateRedeemBatchResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if req.AmountMicrocredits <= 0 {
		return nil, BadAuthRequest("兑换码积分必须大于 0")
	}
	if req.Count <= 0 || req.Count > 5000 {
		return nil, BadAuthRequest("单批兑换码数量需为 1-5000")
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		return nil, BadAuthRequest("兑换码过期时间必须晚于当前时间")
	}
	batch := model.RedeemBatch{ID: newID(), AmountMicrocredits: req.AmountMicrocredits, Count: req.Count, Note: truncateRunes(strings.TrimSpace(req.Note), 500), CreatedBy: actor.ID, ExpiresAt: req.ExpiresAt}
	codes := make([]string, 0, req.Count)
	items := make([]model.RedeemCode, 0, req.Count)
	for range req.Count {
		plain, err := newRedeemCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, plain)
		items = append(items, model.RedeemCode{
			ID: newID(), BatchID: batch.ID, CodeHash: hashRedeemCode(plain), CodeSuffix: plain[len(plain)-4:],
			AmountMicrocredits: req.AmountMicrocredits, Status: model.RedeemCodeUnused, ExpiresAt: req.ExpiresAt,
		})
	}
	encodedCodes, err := json.Marshal(codes)
	if err != nil {
		return nil, err
	}
	batch.CodesCipher, err = s.encryptSettingSecret(string(encodedCodes))
	if err != nil {
		return nil, err
	}
	// SQLite 只有一个写入器；批次生成串行进入短事务，避免并发生成占满连接池拖住全站读取。
	s.redeemBatchMu.Lock()
	defer s.redeemBatchMu.Unlock()
	if err := s.repo.CreateRedeemBatch(&batch, items); err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "redeem_batch.create", "redeem_batch", batch.ID, "创建兑换码批次", map[string]any{"count": batch.Count, "amountMicrocredits": batch.AmountMicrocredits}); err != nil {
		return nil, err
	}
	return &CreateRedeemBatchResult{Batch: batch, Codes: codes}, nil
}

func (s *Service) AdminRedeemCodePage(actor *model.User, batchID string, status string, page int, limit int) (*AdminRedeemCodePage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	batch, err := s.repo.RedeemBatch(strings.TrimSpace(batchID))
	if err != nil {
		return nil, err
	}
	page, limit = normalizeAdminPage(page, limit)
	rows, total, err := s.repo.AdminRedeemCodes(batch.ID, strings.TrimSpace(status), limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	plainCodes, err := s.redeemBatchPlainCodes(batch.CodesCipher)
	if err != nil {
		return nil, err
	}
	plainByHash := make(map[string]string, len(plainCodes))
	for _, code := range plainCodes {
		plainByHash[hashRedeemCode(code)] = code
	}
	now := time.Now()
	details := make([]AdminRedeemCodeDetail, 0, len(rows))
	for _, row := range rows {
		status := string(row.Status)
		if row.Status == model.RedeemCodeUnused && row.ExpiresAt != nil && !row.ExpiresAt.After(now) {
			status = "expired"
		}
		details = append(details, AdminRedeemCodeDetail{
			ID: row.ID, Code: plainByHash[row.CodeHash], CodeSuffix: row.CodeSuffix, Status: status,
			RedeemedBy: row.RedeemedBy, RedeemedUsername: row.RedeemedUsername, RedeemedDisplayName: row.RedeemedDisplayName,
			RedeemedAt: row.RedeemedAt, RedeemedIP: row.RedeemedIP, ExpiresAt: row.ExpiresAt, AmountMicrocredits: row.AmountMicrocredits,
		})
	}
	batch.CodesCipher = ""
	return &AdminRedeemCodePage{Batch: *batch, Codes: details, PlaintextAvailable: len(plainCodes) > 0, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) redeemBatchPlainCodes(ciphertext string) ([]string, error) {
	if strings.TrimSpace(ciphertext) == "" {
		return nil, nil
	}
	encoded, err := s.decryptSettingSecret(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("兑换码批次密文无法解密：%w", err)
	}
	var codes []string
	if err := json.Unmarshal([]byte(encoded), &codes); err != nil {
		return nil, errors.New("兑换码批次密文内容无效")
	}
	return codes, nil
}

func (s *Service) AdminRedeemBatchPage(actor *model.User, query AdminListQuery) (*RedeemBatchPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	items, total, err := s.repo.AdminRedeemBatches(query.Keyword, query.Status, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	return &RedeemBatchPage{Batches: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) AdminAdjustCredits(actor *model.User, userID string, req AdminCreditAdjustmentRequest) (*model.CreditAccount, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if req.AmountMicrocredits == 0 {
		return nil, BadAuthRequest("调账积分不能为 0")
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		return nil, BadAuthRequest("请填写调账原因")
	}
	if _, err := s.repo.User(userID); err != nil {
		return nil, err
	}
	account, err := s.repo.AdjustCredits(userID, actor.ID, req.AmountMicrocredits, truncateRunes(note, 500))
	if errors.Is(err, repository.ErrInsufficientCredits) {
		return nil, BadAuthRequest("用户可用积分不足，不能执行本次扣减")
	}
	if err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "credits.adjust", "user", userID, "管理员调整用户积分", map[string]any{"amountMicrocredits": req.AmountMicrocredits, "note": truncateRunes(note, 500)}); err != nil {
		return nil, err
	}
	return account, nil
}

func (s *Service) AdminBillingOrderPage(actor *model.User, query AdminListQuery) (*BillingOrderPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	page, limit := normalizeAdminPage(query.Page, query.Limit)
	items, total, err := s.repo.AdminBillingOrders(query.Status, query.Keyword, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	return &BillingOrderPage{Orders: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) ResolveBillingOrder(actor *model.User, id string, req ResolveBillingRequest) (*model.BillingOrder, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		return nil, BadAuthRequest("请填写核对依据")
	}
	order, err := s.repo.BillingOrder(id)
	if err != nil {
		return nil, err
	}
	if order.Status != model.BillingStatusUncertain && order.Status != model.BillingStatusRunning && order.Status != model.BillingStatusReserved {
		return nil, BadAuthRequest("当前订单不需要人工核对")
	}
	switch strings.TrimSpace(req.Action) {
	case "settle":
		err = s.SettleBilling(id, order.ProviderRequestID)
	case "refund":
		err = s.RefundBilling(id, note)
	default:
		return nil, BadAuthRequest("请选择结算或退款")
	}
	if err != nil {
		return nil, err
	}
	if err := s.repo.RecordBillingResolution(id, actor.ID, truncateRunes(note, 500)); err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "billing.resolve", "user", order.UserID, "人工核对用户计费订单", map[string]any{"billingOrderId": id, "action": req.Action, "note": truncateRunes(note, 500)}); err != nil {
		return nil, err
	}
	return s.repo.BillingOrder(id)
}

func (s *Service) AdminDisableRedeemBatch(actor *model.User, batchID string) (int64, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return 0, err
	}
	if _, err := s.repo.RedeemBatch(strings.TrimSpace(batchID)); err != nil {
		return 0, err
	}
	count, err := s.repo.DisableRedeemBatch(batchID, time.Now())
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, BadAuthRequest("该批次没有可禁用的兑换码")
	}
	if err := s.appendAdminAudit(actor, "redeem_batch.disable", "redeem_batch", batchID, "禁用批次内全部未使用兑换码", map[string]any{"disabledCount": count}); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Service) AdminDisableRedeemCode(actor *model.User, batchID string, codeID string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	disabled, err := s.repo.DisableRedeemCode(batchID, codeID, time.Now())
	if err != nil {
		return err
	}
	if !disabled {
		return BadAuthRequest("兑换码不存在、已使用、已禁用或已过期")
	}
	return s.appendAdminAudit(actor, "redeem_code.disable", "redeem_code", codeID, "禁用单个兑换码", map[string]any{"batchId": batchID})
}

func (s *Service) taskBillingOrder(userID string, task *model.Task, input map[string]any) (*model.BillingOrder, error) {
	config, _ := input["config"].(map[string]any)
	if config == nil {
		return nil, BadAuthRequest("当前功能必须使用后台配置的系统模型")
	}
	channelID := strings.TrimSpace(fmt.Sprint(config["channelId"]))
	if channelID == "" {
		channelID = systemChannelIDFromBaseURL(fmt.Sprint(config["baseUrl"]))
	}
	if channelID == "" {
		return nil, BadAuthRequest("当前功能必须使用后台配置的系统模型")
	}
	modelKey := strings.TrimPrefix(strings.TrimSpace(fmt.Sprint(config["model"])), "models/")
	if modelKey == "" {
		return nil, BadAuthRequest("请选择后台已启用的系统模型")
	}
	taskCapability := capabilityFromTaskType(task.Type)
	inputCapability := normalizeCapability(fmt.Sprint(input["mode"]))
	if taskCapability != "" && inputCapability != "" && taskCapability != inputCapability {
		return nil, BadAuthRequest("任务类型与模型能力不匹配")
	}
	capability := taskCapability
	if capability == "" {
		capability = inputCapability
	}
	if capability == "" {
		return nil, BadAuthRequest("任务能力类型无效，无法计费")
	}
	scene := firstNonEmpty(strings.TrimSpace(task.Operation), task.Type)
	return s.newBillingOrder(userID, task.ID, "task:"+task.ID+":"+newID(), channelID, modelKey, capability, scene, billingUsage(capability, config))
}

type BillingUsage struct {
	Quantity                  int64
	Resolution                string
	SuperResolutionEnabled    bool
	SuperResolutionResolution string
}

func (s *Service) ReserveProxyBilling(userID string, channelID string, modelKey string, capability string, scene string, idempotencyKey string, usage BillingUsage) (*model.BillingOrder, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		idempotencyKey = newID()
	}
	order, err := s.newBillingOrder(userID, "", "proxy:"+idempotencyKey, channelID, modelKey, capability, firstNonEmpty(strings.TrimSpace(scene), "system_proxy"), usage)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReserveBillingOrder(order); err != nil {
		if errors.Is(err, repository.ErrInsufficientCredits) {
			return nil, BadAuthRequest("积分不足，请先使用兑换码充值")
		}
		return nil, err
	}
	return order, nil
}

func (s *Service) newBillingOrder(userID string, taskID string, idempotencyKey string, channelID string, modelKey string, capability string, scene string, usage BillingUsage) (*model.BillingOrder, error) {
	item, err := s.repo.ChannelModelByKey(channelID, modelKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, BadAuthRequest("当前系统渠道模型未配置或已停用")
	}
	if err != nil {
		return nil, err
	}
	if !item.PriceConfigured {
		return nil, BadAuthRequest("当前模型尚未配置用户积分价格")
	}
	itemCapability := normalizeCapability(item.Capability)
	if itemCapability == "" {
		return nil, BadAuthRequest("当前模型尚未配置有效能力类型")
	}
	requestedCapability := normalizeCapability(capability)
	if requestedCapability != "" && requestedCapability != itemCapability {
		return nil, BadAuthRequest("当前模型能力与本次生成功能不匹配")
	}
	capability = itemCapability
	switch item.BillingMode {
	case "fixed_request":
	case "per_second":
		if item.Capability != "video" || capability != "video" {
			return nil, BadAuthRequest("按秒计费仅适用于视频生成")
		}
		if usage.Quantity <= 0 {
			return nil, BadAuthRequest("视频生成时长无效，无法按秒计费")
		}
	default:
		return nil, BadAuthRequest("当前模型计费方式暂不支持")
	}
	unitPrice := item.UnitPriceMicrocredits
	priceTierID := ""
	pricingResolution := ""
	switch item.PriceStrategy {
	case "flat":
		if unitPrice <= 0 {
			return nil, BadAuthRequest("当前模型尚未配置有效的用户积分价格")
		}
	case "image_resolution":
		if capability != "image" || item.BillingMode != "fixed_request" {
			return nil, BadAuthRequest("分辨率阶梯价格仅适用于按次计费的图片模型")
		}
		pricingResolution = normalizeImagePricingResolution(usage.Resolution)
		if pricingResolution == "" {
			return nil, BadAuthRequest("图片分辨率无效，无法匹配积分价格")
		}
		for _, tier := range item.PriceTiers {
			if tier.Resolution == pricingResolution {
				unitPrice = tier.UnitPriceMicrocredits
				priceTierID = tier.ID
				break
			}
		}
		if priceTierID == "" || unitPrice <= 0 {
			return nil, BadAuthRequest("当前模型未配置该分辨率的积分价格")
		}
	case "video_resolution":
		if capability != "video" {
			return nil, BadAuthRequest("视频分辨率价格仅适用于视频模型")
		}
		pricingResolution = normalizeVideoPricingResolution(usage)
		if pricingResolution == "" {
			return nil, BadAuthRequest("视频分辨率无效，无法匹配积分价格")
		}
		for _, tier := range item.PriceTiers {
			if tier.Resolution == pricingResolution {
				unitPrice = tier.UnitPriceMicrocredits
				priceTierID = tier.ID
				break
			}
		}
		if priceTierID == "" || unitPrice <= 0 {
			return nil, BadAuthRequest("当前模型未配置规格 " + pricingResolution + " 的积分价格")
		}
	default:
		return nil, BadAuthRequest("当前模型尚未配置有效的价格策略")
	}
	quantity := int64(1)
	if item.BillingMode == "per_second" {
		quantity = usage.Quantity
	} else if capability == "image" {
		quantity = usage.Quantity
	}
	if quantity <= 0 {
		return nil, BadAuthRequest("生成数量无效，无法计费")
	}
	policy, err := s.creditPolicy()
	if err != nil {
		return nil, err
	}
	multiplierBPS := policy.DefaultMultiplierBPS
	if configured := policy.ModelMultiplierBPS[modelKey]; configured > 0 {
		multiplierBPS = configured
	}
	amount, err := creditAmount(unitPrice, quantity, multiplierBPS)
	if err != nil {
		return nil, err
	}
	return &model.BillingOrder{
		ID: newID(), UserID: userID, IdempotencyKey: idempotencyKey, TaskID: taskID,
		ChannelID: channelID, ChannelModelID: item.ID, Model: modelKey, Capability: capability,
		Scene: truncateRunes(scene, 80), BillingMode: item.BillingMode, PriceVersion: item.PriceVersion,
		PriceTierID: priceTierID, PricingResolution: pricingResolution,
		UnitPriceMicrocredits: unitPrice, MultiplierBasisPoints: multiplierBPS, Quantity: quantity, AmountMicrocredits: amount,
		Status: model.BillingStatusReserved,
	}, nil
}

func billingUsage(capability string, config map[string]any) BillingUsage {
	usage := BillingUsage{Quantity: 1}
	if capability == "image" {
		usage.Quantity = positiveInteger(config["count"])
		usage.Resolution = firstNonEmpty(strings.TrimSpace(fmt.Sprint(config["resolution"])), strings.TrimSpace(fmt.Sprint(config["quality"])))
		return usage
	}
	if capability == "video" {
		usage.Quantity = positiveInteger(config["videoSeconds"])
		usage.Resolution = strings.TrimSpace(fmt.Sprint(config["vquality"]))
		usage.SuperResolutionEnabled = strings.EqualFold(strings.TrimSpace(fmt.Sprint(config["videoSuperResolutionEnabled"])), "true")
		usage.SuperResolutionResolution = strings.TrimSpace(fmt.Sprint(config["videoSuperResolutionResolution"]))
	}
	return usage
}

func positiveInteger(value any) int64 {
	quantity, err := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
	if err != nil || quantity <= 0 {
		return 0
	}
	return quantity
}

func normalizeImagePricingResolution(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "1k":
		return "1K"
	case "medium", "2k":
		return "2K"
	case "high", "4k":
		return "4K"
	default:
		return ""
	}
}

func normalizeVideoPricingResolution(usage BillingUsage) string {
	value := usage.Resolution
	if usage.SuperResolutionEnabled {
		value = usage.SuperResolutionResolution
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "480", "480p":
		if usage.SuperResolutionEnabled {
			return ""
		}
		return "480P"
	case "720", "720p":
		if usage.SuperResolutionEnabled {
			return "SR_720P"
		}
		return "720P"
	case "1080", "1080p":
		if usage.SuperResolutionEnabled {
			return "SR_1080P"
		}
		return "1080P"
	case "2k":
		if usage.SuperResolutionEnabled {
			return "SR_2K"
		}
		return "2K"
	case "4k":
		if usage.SuperResolutionEnabled {
			return "SR_4K"
		}
		return "4K"
	default:
		return ""
	}
}

func (s *Service) MarkBillingRunning(orderID string) error {
	if orderID == "" {
		return nil
	}
	return s.repo.MarkBillingRunning(orderID)
}

func (s *Service) SettleBilling(orderID string, providerRequestID string) error {
	if orderID == "" {
		return nil
	}
	return s.repo.SettleBillingOrder(orderID, providerRequestID)
}

func (s *Service) RefundBilling(orderID string, errorText string) error {
	if orderID == "" {
		return nil
	}
	return s.repo.RefundBillingOrder(orderID, truncateRunes(errorText, 1000))
}

func (s *Service) MarkBillingUncertain(orderID string, errorText string) error {
	if orderID == "" {
		return nil
	}
	return s.repo.MarkBillingUncertain(orderID, truncateRunes(errorText, 1000))
}

func (s *Service) BillingFailureRequiresReview(orderID string, taskID string, err error) bool {
	if orderID == "" {
		return false
	}
	if billingFailureUncertain(err) {
		return true
	}
	order, orderErr := s.repo.BillingOrder(orderID)
	if orderErr != nil || order.Status == model.BillingStatusUncertain {
		return true
	}
	hasSuccessfulCall, logErr := s.repo.TaskHasSuccessfulBillableCall(taskID)
	return logErr != nil || hasSuccessfulCall
}

func billingFailureUncertain(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"524", "timeout", "超时", "deadline exceeded", "context canceled", "connection reset", "unexpected eof", "broken pipe"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func newRedeemCode() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return hex.EncodeToString(raw[:]), nil
}

func hashRedeemCode(code string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(code))))
	return hex.EncodeToString(sum[:])
}
