package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type TokenPricingSnapshot struct {
	InputPerMillionMicros  int64 `json:"inputPerMillionMicros"`
	CachedPerMillionMicros int64 `json:"cachedPerMillionMicros"`
	OutputPerMillionMicros int64 `json:"outputPerMillionMicros"`
	MaxOutputTokens        int64 `json:"maxOutputTokens"`
}

const (
	kuaiziBillingFenMicrocredits    int64 = 1_000_000
	tokenBillingProtocolMarginBytes int64 = 256
)

type TokenUsageFact struct {
	InputTokens  int64
	CachedTokens int64
	OutputTokens int64
	Available    bool
}

// PrepareTokenBilledChatRequest freezes the commercial output ceiling in the
// exact request body and returns a conservative UTF-8 byte upper bound for
// input-token reservation.
func PrepareTokenBilledChatRequest(body []byte, maxOutputTokens int64) ([]byte, int64, error) {
	if maxOutputTokens <= 0 || len(body) == 0 || !utf8.Valid(body) {
		return nil, 0, errors.New("Token 计费请求不符合 Chat Completions 契约")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return nil, 0, errors.New("Token 计费请求 JSON 无效")
	}
	limit, err := json.Marshal(maxOutputTokens)
	if err != nil {
		return nil, 0, err
	}
	payload["max_tokens"] = limit
	if streamRaw, exists := payload["stream"]; exists {
		var stream bool
		if err := json.Unmarshal(streamRaw, &stream); err != nil {
			return nil, 0, errors.New("Token 计费请求 stream 字段无效")
		}
		if stream {
			payload["stream_options"] = json.RawMessage(`{"include_usage":true}`)
		}
	}
	prepared, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	return prepared, int64(len(prepared)) + tokenBillingProtocolMarginBytes, nil
}

func validateTokenUsageModelBilling(item model.ChannelModel, pricing *model.ModelPricing) (TokenPricingSnapshot, error) {
	if !kuaiziModelSupportsTokenUsageBilling(item.ModelKey) || normalizeCapability(item.Capability) != "text" {
		return TokenPricingSnapshot{}, errors.New("Token 用量计费仅支持筷子托管文本模型")
	}
	if item.BillingMode != "token_usage" || item.PriceStrategy != "token" || item.UnitPriceMicrocredits != 0 || !item.PriceConfigured {
		return TokenPricingSnapshot{}, errors.New("Token 用量计费模型配置不完整")
	}
	if pricing == nil || strings.ToUpper(strings.TrimSpace(pricing.Currency)) != "CNY" || pricing.InputPerMillionMicros <= 0 || pricing.OutputPerMillionMicros <= 0 || pricing.CachedPerMillionMicros < 0 || pricing.ExpectedOutputTokens <= 0 {
		return TokenPricingSnapshot{}, errors.New("Token 供应商价格配置不完整")
	}
	return TokenPricingSnapshot{
		InputPerMillionMicros:  pricing.InputPerMillionMicros,
		CachedPerMillionMicros: pricing.CachedPerMillionMicros,
		OutputPerMillionMicros: pricing.OutputPerMillionMicros,
		MaxOutputTokens:        pricing.ExpectedOutputTokens,
	}, nil
}

func tokenChargeMicrocredits(pricing TokenPricingSnapshot, usage TokenUsageFact, multiplierBPS int64) (int64, error) {
	if pricing.InputPerMillionMicros <= 0 || pricing.OutputPerMillionMicros <= 0 || pricing.CachedPerMillionMicros < 0 || pricing.MaxOutputTokens <= 0 {
		return 0, errors.New("Token 价格快照无效")
	}
	if usage.InputTokens < 0 || usage.CachedTokens < 0 || usage.OutputTokens < 0 || usage.CachedTokens > usage.InputTokens {
		return 0, errors.New("Token 用量事实无效")
	}
	if multiplierBPS <= 0 {
		return 0, errors.New("计费倍率无效")
	}

	uncachedTokens := usage.InputTokens - usage.CachedTokens
	numerator := new(big.Int)
	numerator.Add(numerator, new(big.Int).Mul(big.NewInt(uncachedTokens), big.NewInt(pricing.InputPerMillionMicros)))
	numerator.Add(numerator, new(big.Int).Mul(big.NewInt(usage.CachedTokens), big.NewInt(pricing.CachedPerMillionMicros)))
	numerator.Add(numerator, new(big.Int).Mul(big.NewInt(usage.OutputTokens), big.NewInt(pricing.OutputPerMillionMicros)))
	numerator.Mul(numerator, big.NewInt(localCreditsPerCurrencyUnit))
	numerator.Mul(numerator, big.NewInt(multiplierBPS))

	denominator := big.NewInt(microsPerCurrencyUnit * basisPointsScale)
	amount := new(big.Int).Quo(new(big.Int).Add(numerator, new(big.Int).Sub(denominator, big.NewInt(1))), denominator)
	if !amount.IsInt64() {
		return 0, errors.New("Token 计费金额溢出")
	}
	return amount.Int64(), nil
}

type TokenBillingReservation struct {
	TaskID               string
	EstimatedInputTokens int64
	MaxOutputTokens      int64
	Pricing              TokenPricingSnapshot
	EndpointVersionID    string
	CredentialVersionID  string
}

func (s *Service) ProxyTokenBillingConfig(userID string, channelID string, modelKey string) (TokenPricingSnapshot, bool, error) {
	item, err := s.requireAccessibleChannelModel(strings.TrimSpace(userID), strings.TrimSpace(channelID), strings.TrimPrefix(strings.TrimSpace(modelKey), "models/"))
	if err != nil {
		return TokenPricingSnapshot{}, false, err
	}
	if item.BillingMode != "token_usage" {
		return TokenPricingSnapshot{}, false, nil
	}
	pricing, err := s.repo.ModelPricing(item.ChannelID, item.ModelKey, "text")
	if err != nil {
		return TokenPricingSnapshot{}, false, err
	}
	snapshot, err := validateTokenUsageModelBilling(*item, pricing)
	return snapshot, err == nil, err
}

func (s *Service) ReserveProxyTokenBilling(userID string, channelID string, modelKey string, scene string, idempotencyKey string, reservation TokenBillingReservation) (*model.BillingOrder, error) {
	order, err := s.newTokenBillingOrder(userID, channelID, modelKey, scene, idempotencyKey, reservation)
	if err != nil {
		return nil, err
	}
	existing, lookupErr := s.repo.BillingOrderByUserIdempotency(order.UserID, order.IdempotencyKey)
	if lookupErr == nil {
		if !sameTokenBillingReservation(existing, order) {
			return nil, BadAuthRequest("Idempotency-Key 已用于不同的 Token 计费请求")
		}
		return existing, nil
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return nil, lookupErr
	}
	if err := s.repo.ReserveBillingOrder(order); err != nil {
		existing, existingErr := s.repo.BillingOrderByUserIdempotency(order.UserID, order.IdempotencyKey)
		if existingErr == nil {
			if !sameTokenBillingReservation(existing, order) {
				return nil, BadAuthRequest("Idempotency-Key 已用于不同的 Token 计费请求")
			}
			return existing, nil
		}
		if errors.Is(err, repository.ErrInsufficientCredits) {
			return nil, BadAuthRequest(creditInsufficientMessage(order.TeamID))
		}
		if errors.Is(err, repository.ErrTeamMemberCreditLimit) {
			return nil, BadAuthRequest("本月团队积分额度已用尽，请联系团队管理员调整额度")
		}
		return nil, err
	}
	return order, nil
}

func (s *Service) newTokenBillingOrder(userID string, channelID string, modelKey string, scene string, idempotencyKey string, reservation TokenBillingReservation) (*model.BillingOrder, error) {
	userID = strings.TrimSpace(userID)
	channelID = strings.TrimSpace(channelID)
	modelKey = strings.TrimSpace(modelKey)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	reservation.TaskID = strings.TrimSpace(reservation.TaskID)
	reservation.EndpointVersionID = strings.TrimSpace(reservation.EndpointVersionID)
	reservation.CredentialVersionID = strings.TrimSpace(reservation.CredentialVersionID)
	if userID == "" || channelID == "" || modelKey == "" || idempotencyKey == "" || reservation.EstimatedInputTokens <= 0 || reservation.MaxOutputTokens <= 0 || reservation.EndpointVersionID == "" || reservation.CredentialVersionID == "" {
		return nil, BadAuthRequest("Token 计费预留事实不完整")
	}
	item, err := s.requireAccessibleChannelModel(userID, channelID, modelKey)
	if err != nil {
		return nil, err
	}
	pricing, err := s.repo.ModelPricing(channelID, modelKey, "text")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, BadAuthRequest("当前 Agent 模型尚未配置 Token 供应商价格")
	}
	if err != nil {
		return nil, err
	}
	configured, err := validateTokenUsageModelBilling(*item, pricing)
	if err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	if reservation.Pricing != configured || reservation.MaxOutputTokens != configured.MaxOutputTokens {
		return nil, BadAuthRequest("Token 计费预留价格与已发布配置不一致")
	}
	policy, err := s.creditPolicy()
	if err != nil {
		return nil, err
	}
	multiplierBPS := policy.DefaultMultiplierBPS
	if configuredMultiplier := policy.ModelMultiplierBPS[modelKey]; configuredMultiplier > 0 {
		multiplierBPS = configuredMultiplier
	}
	if multiplierBPS != basisPointsScale {
		return nil, BadAuthRequest("Agent Token 计费首版仅支持 1.0 倍率")
	}
	amount, err := tokenChargeMicrocredits(configured, TokenUsageFact{InputTokens: reservation.EstimatedInputTokens, OutputTokens: reservation.MaxOutputTokens}, multiplierBPS)
	if err != nil {
		return nil, err
	}
	pricingJSON, err := json.Marshal(configured)
	if err != nil {
		return nil, err
	}
	teamID, err := s.billingTeamID(userID, time.Now())
	if err != nil {
		return nil, err
	}
	now := time.Now()
	order := &model.BillingOrder{
		ID: newID(), UserID: userID, TeamID: teamID, IdempotencyKey: "proxy-token:" + idempotencyKey, TaskID: reservation.TaskID,
		ChannelID: channelID, ChannelModelID: item.ID, Model: modelKey, Capability: "text", Scene: truncateRunes(firstNonEmpty(strings.TrimSpace(scene), "agent"), 80),
		BillingMode: "token_usage", PriceVersion: item.PriceVersion, MultiplierBasisPoints: multiplierBPS, Quantity: 1,
		AmountMicrocredits: amount, ReservedAmountMicrocredits: amount, TokenPricingSnapshotJSON: string(pricingJSON),
		EstimatedInputTokens: reservation.EstimatedInputTokens, MaxOutputTokens: reservation.MaxOutputTokens,
		ProviderEndpointVersionID: reservation.EndpointVersionID, ProviderCredentialVersionID: reservation.CredentialVersionID,
		Status: model.BillingStatusReserved, CreatedAt: now, UpdatedAt: now,
	}
	return order, nil
}

func sameTokenBillingReservation(existing *model.BillingOrder, requested *model.BillingOrder) bool {
	return existing != nil && requested != nil &&
		existing.BillingMode == "token_usage" && existing.UserID == requested.UserID && existing.IdempotencyKey == requested.IdempotencyKey && existing.TaskID == requested.TaskID &&
		existing.ChannelID == requested.ChannelID && existing.ChannelModelID == requested.ChannelModelID && existing.Model == requested.Model &&
		existing.Scene == requested.Scene && existing.PriceVersion == requested.PriceVersion && existing.MultiplierBasisPoints == requested.MultiplierBasisPoints &&
		existing.ReservedAmountMicrocredits == requested.ReservedAmountMicrocredits && existing.TokenPricingSnapshotJSON == requested.TokenPricingSnapshotJSON &&
		existing.EstimatedInputTokens == requested.EstimatedInputTokens && existing.MaxOutputTokens == requested.MaxOutputTokens &&
		existing.ProviderEndpointVersionID == requested.ProviderEndpointVersionID && existing.ProviderCredentialVersionID == requested.ProviderCredentialVersionID
}

func (s *Service) ReconcileTokenBillingNow(ctx context.Context, orderID string, providerTaskID string, usage TokenUsageFact) error {
	order, err := s.repo.BillingOrder(strings.TrimSpace(orderID))
	if err != nil {
		return err
	}
	providerTaskID, taskIDErr := tokenBillingTaskID(providerTaskID)
	if order.BillingMode != "token_usage" || taskIDErr != nil {
		return errors.New("Token 账单核对事实不完整")
	}
	if err := s.repo.UpdateBillingProviderRequestID(order.ID, providerTaskID); err != nil {
		return err
	}
	order.ProviderRequestID = providerTaskID
	usageStatus, storedUsage := normalizeTokenUsageFact(usage)
	if err := s.repo.RecordTokenBillingUsage(order.ID, repository.TokenUsageFact{InputTokens: storedUsage.InputTokens, CachedTokens: storedUsage.CachedTokens, OutputTokens: storedUsage.OutputTokens}, usageStatus); err != nil {
		return err
	}
	order.InputTokens, order.CachedTokens, order.OutputTokens, order.TokenUsageStatus = storedUsage.InputTokens, storedUsage.CachedTokens, storedUsage.OutputTokens, usageStatus
	return s.reconcileTokenBillingOrder(ctx, order, false)
}

func (s *Service) settleCompletedTaskBilling(ctx context.Context, orderID string) error {
	if strings.TrimSpace(orderID) == "" {
		return nil
	}
	order, err := s.repo.BillingOrder(orderID)
	if err != nil {
		return err
	}
	if order.BillingMode != "token_usage" {
		return s.SettleBilling(order.ID, order.ProviderRequestID)
	}
	if strings.TrimSpace(order.ProviderRequestID) == "" {
		return errors.New("Token 账单缺少可核对的上游任务 ID")
	}
	return s.reconcileTokenBillingOrder(ctx, order, false)
}

func (s *Service) ScheduleTokenBillingReconciliation(orderID string, providerTaskID string, reason string, usage TokenUsageFact) error {
	providerTaskID, err := tokenBillingTaskID(providerTaskID)
	if err != nil {
		return err
	}
	if err := s.repo.UpdateBillingProviderRequestID(strings.TrimSpace(orderID), providerTaskID); err != nil {
		return err
	}
	usageStatus, storedUsage := normalizeTokenUsageFact(usage)
	if err := s.repo.RecordTokenBillingUsage(strings.TrimSpace(orderID), repository.TokenUsageFact{InputTokens: storedUsage.InputTokens, CachedTokens: storedUsage.CachedTokens, OutputTokens: storedUsage.OutputTokens}, usageStatus); err != nil {
		return err
	}
	return s.repo.MarkTokenBillingForReconciliation(strings.TrimSpace(orderID), strings.TrimSpace(providerTaskID), reason, time.Now().Add(5*time.Second))
}

func tokenBillingTaskID(chatCompletionID string) (string, error) {
	const prefix = "chatcmpl-"
	chatCompletionID = strings.TrimSpace(chatCompletionID)
	if !strings.HasPrefix(chatCompletionID, prefix) || len(chatCompletionID) == len(prefix) {
		return "", errors.New("Token 账单任务 ID 契约无效")
	}
	return strings.TrimPrefix(chatCompletionID, prefix), nil
}

func (s *Service) RunKuaiziBillingReconciliationBatch(ctx context.Context, now time.Time, limit int) error {
	orders, err := s.repo.ClaimKuaiziBillingReconciliations(s.workerID, now, time.Minute, limit)
	if err != nil {
		return err
	}
	for index := range orders {
		order := &orders[index]
		var reconcileErr error
		if order.BillingMode == "token_usage" {
			reconcileErr = s.reconcileTokenBillingOrder(ctx, order, true)
		} else {
			reconcileErr = s.reconcileKuaiziMediaBillingOrder(ctx, order)
		}
		if reconcileErr != nil {
			return reconcileErr
		}
	}
	return nil
}

func (s *Service) reconcileKuaiziMediaBillingOrder(ctx context.Context, order *model.BillingOrder) error {
	runtime, err := s.resolveFrozenKuaiziBillingRuntime(order)
	if err != nil {
		return s.deferKuaiziBillingReconciliation(order, "frozen_runtime_unavailable")
	}
	client := NewKuaiziClient(KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), 20*time.Second))
	fact, err := client.BillingByTaskID(ctx, runtime.BaseURL, runtime.APIKey, order.ProviderRequestID)
	if err != nil {
		var billingError *KuaiziBillingError
		code := "billing_lookup_failed"
		if errors.As(err, &billingError) {
			code = billingError.Code
		}
		return s.deferKuaiziBillingReconciliation(order, code)
	}
	if providerBillingOrderIDConflict(order, fact) {
		return s.repo.RequireKuaiziBillingReview(order.ID, order.ReconcileLeaseOwner, order.ReconcileLeaseToken, "provider_billing_order_changed")
	}
	if err := s.repo.RecordProviderBillingObservation(order.ID, fact.OrderID, fact.Amount, fact.Status, fact.TaskStatus, fact.TotalTokens); err != nil {
		return err
	}
	if reason := providerBillingStatusContradiction(fact); reason != "" {
		return s.repo.RequireKuaiziBillingReview(order.ID, order.ReconcileLeaseOwner, order.ReconcileLeaseToken, reason)
	}
	switch fact.Status {
	case "pending":
		return s.deferKuaiziBillingReconciliation(order, "billing_pending")
	case "failed":
		if fact.Amount != 0 {
			return s.repo.RequireKuaiziBillingReview(order.ID, order.ReconcileLeaseOwner, order.ReconcileLeaseToken, "failed_bill_has_amount")
		}
		return s.repo.RefundBillingOrder(order.ID, "上游账单明确失败且未扣费")
	case "succeeded":
		return s.repo.SettleBillingOrder(order.ID, order.ProviderRequestID)
	default:
		return s.deferKuaiziBillingReconciliation(order, "billing_status_invalid")
	}
}

func (s *Service) reconcileTokenBillingOrder(ctx context.Context, order *model.BillingOrder, claimed bool) error {
	runtime, err := s.resolveFrozenKuaiziBillingRuntime(order)
	if err != nil {
		return s.deferTokenBillingReconciliation(order, claimed, "frozen_runtime_unavailable")
	}
	client := NewKuaiziClient(KuaiziHTTPClient(strings.TrimSpace(os.Getenv("CANVAS_ENVIRONMENT")), 20*time.Second))
	fact, err := client.BillingByTaskID(ctx, runtime.BaseURL, runtime.APIKey, order.ProviderRequestID)
	if err != nil {
		var billingError *KuaiziBillingError
		code := "billing_lookup_failed"
		if errors.As(err, &billingError) {
			code = billingError.Code
		}
		return s.deferTokenBillingReconciliation(order, claimed, code)
	}
	if providerBillingOrderIDConflict(order, fact) {
		return s.deferOrReviewTokenBilling(order, claimed, "provider_billing_order_changed")
	}
	if err := s.repo.RecordProviderBillingObservation(order.ID, fact.OrderID, fact.Amount, fact.Status, fact.TaskStatus, fact.TotalTokens); err != nil {
		return err
	}
	if reason := providerBillingStatusContradiction(fact); reason != "" {
		return s.deferOrReviewTokenBilling(order, claimed, reason)
	}
	switch fact.Status {
	case "pending":
		return s.deferTokenBillingReconciliation(order, claimed, "billing_pending")
	case "failed":
		if fact.Amount != 0 {
			return s.deferOrReviewTokenBilling(order, claimed, "failed_bill_has_amount")
		}
		return s.repo.RefundTokenBilling(order.ID, fact.OrderID, fact.Amount, fact.TaskStatus, fact.TotalTokens, "上游账单明确失败且未扣费")
	case "succeeded":
		warning := tokenBillingReconciliationWarning(order, fact.Amount)
		return s.repo.SettleTokenBilling(order.ID, repository.TokenSettlementFact{
			ProviderOrderID:        fact.OrderID,
			ProviderAmountSubunits: fact.Amount,
			ProviderTaskStatus:     fact.TaskStatus,
			ProviderTotalTokens:    fact.TotalTokens,
			Usage:                  repository.TokenUsageFact{InputTokens: order.InputTokens, CachedTokens: order.CachedTokens, OutputTokens: order.OutputTokens},
			UsageStatus:            order.TokenUsageStatus,
			ReconciliationWarning:  warning,
			SettledAt:              time.Now(),
		})
	default:
		return s.deferTokenBillingReconciliation(order, claimed, "billing_status_invalid")
	}
}

func providerBillingOrderIDConflict(order *model.BillingOrder, fact KuaiziBillingFact) bool {
	return strings.TrimSpace(order.ProviderBillingOrderID) != "" && strings.TrimSpace(order.ProviderBillingOrderID) != strings.TrimSpace(fact.OrderID)
}

func providerBillingStatusContradiction(fact KuaiziBillingFact) string {
	switch fact.Status {
	case "succeeded":
		if fact.TaskStatus != "succeeded" {
			return "succeeded_bill_task_status_conflict"
		}
	case "failed":
		if fact.TaskStatus != "failed" {
			return "failed_bill_task_status_conflict"
		}
	}
	return ""
}

func (s *Service) deferOrReviewTokenBilling(order *model.BillingOrder, claimed bool, reason string) error {
	if claimed {
		return s.repo.RequireKuaiziBillingReview(order.ID, order.ReconcileLeaseOwner, order.ReconcileLeaseToken, reason)
	}
	return s.deferTokenBillingReconciliation(order, false, reason)
}

func tokenBillingReconciliationWarning(order *model.BillingOrder, providerAmountSubunits int64) string {
	switch order.TokenUsageStatus {
	case "missing":
		return "Token 用量缺失，已按上游账单实扣"
	case "invalid":
		return "Token 用量无效，已按上游账单实扣"
	case "reported":
		var pricing TokenPricingSnapshot
		if err := json.Unmarshal([]byte(order.TokenPricingSnapshotJSON), &pricing); err != nil {
			return "Token 本地价格快照无效，已按上游账单实扣"
		}
		localAmount, err := tokenChargeMicrocredits(pricing, TokenUsageFact{InputTokens: order.InputTokens, CachedTokens: order.CachedTokens, OutputTokens: order.OutputTokens}, order.MultiplierBasisPoints)
		if err != nil {
			return "Token 本地核算失败，已按上游账单实扣"
		}
		providerAmount := providerAmountSubunits * kuaiziBillingFenMicrocredits
		if localAmount != providerAmount {
			return fmt.Sprintf("Token 账单差异：本地核算 %d 微积分，上游实扣 %d 微积分", localAmount, providerAmount)
		}
	}
	return ""
}

func normalizeTokenUsageFact(usage TokenUsageFact) (string, TokenUsageFact) {
	if usage.InputTokens < 0 || usage.CachedTokens < 0 || usage.OutputTokens < 0 || usage.CachedTokens > usage.InputTokens {
		return "invalid", TokenUsageFact{}
	}
	if usage.Available || usage.InputTokens != 0 || usage.CachedTokens != 0 || usage.OutputTokens != 0 {
		return "reported", usage
	}
	return "missing", TokenUsageFact{}
}

func (s *Service) deferTokenBillingReconciliation(order *model.BillingOrder, claimed bool, reason string) error {
	if claimed && order.ReconcileAttempts >= 8 {
		return s.repo.RequireKuaiziBillingReview(order.ID, order.ReconcileLeaseOwner, order.ReconcileLeaseToken, reason)
	}
	next := time.Now().Add(tokenBillingReconcileDelay(order.ReconcileAttempts))
	if claimed {
		return s.repo.RescheduleKuaiziBillingReconciliation(order.ID, order.ReconcileLeaseOwner, order.ReconcileLeaseToken, reason, next)
	}
	return s.repo.MarkTokenBillingForReconciliation(order.ID, order.ProviderRequestID, reason, next)
}

func (s *Service) deferKuaiziBillingReconciliation(order *model.BillingOrder, reason string) error {
	if order.ReconcileAttempts >= 8 {
		return s.repo.RequireKuaiziBillingReview(order.ID, order.ReconcileLeaseOwner, order.ReconcileLeaseToken, reason)
	}
	next := time.Now().Add(tokenBillingReconcileDelay(order.ReconcileAttempts))
	return s.repo.RescheduleKuaiziBillingReconciliation(order.ID, order.ReconcileLeaseOwner, order.ReconcileLeaseToken, reason, next)
}

func tokenBillingReconcileDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<attempt) * 5 * time.Second
}
