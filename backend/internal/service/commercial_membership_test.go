package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func createCommercialTestUsers(t *testing.T, db *gorm.DB) (*model.User, *model.User, *model.User) {
	t.Helper()
	admin := &model.User{ID: "commercial-admin", Username: "admin", Email: "admin@example.com", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	owner := &model.User{ID: "commercial-owner", Username: "owner", Email: "owner@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive}
	other := &model.User{ID: "commercial-other", Username: "other", Email: "other@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive}
	if err := db.Create([]*model.User{admin, owner, other}).Error; err != nil {
		t.Fatal(err)
	}
	return admin, owner, other
}

func readyWechatPaymentSetting(t *testing.T) PaymentSettingRequest {
	t.Helper()
	merchantPrivateKey, _ := newWebhookTestRSAKey(t)
	_, wechatpayPublicKey := newWebhookTestRSAKey(t)
	return PaymentSettingRequest{
		CheckoutBaseURL: "https://checkout.example.com",
		Wechat: WechatPaymentChannelSettingRequest{
			Enabled:              true,
			AppID:                "wx-app-id",
			MerchantID:           "merchant-id",
			MerchantSerialNo:     "merchant-serial",
			MerchantPrivateKey:   rsaPrivateKeyPEM(t, merchantPrivateKey),
			WechatpayPublicKeyID: "PUB_KEY_ID_3000000001",
			WechatpayPublicKey:   wechatpayPublicKey,
			APIv3Key:             strings.Repeat("k", 32),
			NotifyURL:            "https://api.example.com/payment/wechat/notify",
			GatewayURL:           "https://api.mch.weixin.qq.com",
		},
	}
}

func checkoutToken(t *testing.T, checkoutURL string) string {
	t.Helper()
	parsed, err := url.Parse(checkoutURL)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(parsed.Path, "/pay/")
	if token == "" || strings.Contains(token, "/") {
		t.Fatalf("invalid checkout token path %q", parsed.Path)
	}
	return token
}

func requireAuthStatus(t *testing.T, err error, status int) {
	t.Helper()
	var authErr *AuthError
	if !errors.As(err, &authErr) || authErr.Status != status {
		t.Fatalf("error = %#v, want AuthError status %d", err, status)
	}
}

func TestPendingMembershipOrderDoesNotGrantEntitlementOrCredits(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	_, owner, _ := createCommercialTestUsers(t, db)
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "pending-membership-order")
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != model.MembershipOrderPending {
		t.Fatalf("order status = %s, want pending", order.Status)
	}
	var subscriptionCount int64
	if err := db.Model(&model.MembershipSubscription{}).Where("order_id = ?", order.ID).Count(&subscriptionCount).Error; err != nil {
		t.Fatal(err)
	}
	var ledgerCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ?", owner.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	var accountCount int64
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", owner.ID).Count(&accountCount).Error; err != nil {
		t.Fatal(err)
	}
	if subscriptionCount != 0 || ledgerCount != 0 || accountCount != 0 {
		t.Fatalf("pending order created grants: subscriptions=%d ledger=%d accounts=%d", subscriptionCount, ledgerCount, accountCount)
	}
	entitlement, err := svc.MembershipEntitlement(owner)
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.Tier != "origin" {
		t.Fatalf("pending order entitlement tier = %q, want origin", entitlement.Tier)
	}
}

func TestMembershipOrderCanOnlyBeCancelledByItsOwner(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	_, owner, other := createCommercialTestUsers(t, db)
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "cancel-owner-membership-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelMembershipOrder(other, order.ID); err == nil {
		t.Fatal("other user unexpectedly cancelled the order")
	}
	cancelled, err := svc.CancelMembershipOrder(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != model.MembershipOrderCancelled || cancelled.ResolutionNote != "用户主动取消订单" {
		t.Fatalf("unexpected cancelled order: %#v", cancelled)
	}
}

func TestStalePendingMembershipOrderIsClosedDuringLifecycleReconciliation(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	_, owner, _ := createCommercialTestUsers(t, db)
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "stale-membership-order")
	if err != nil {
		t.Fatal(err)
	}
	staleCreatedAt := time.Now().Add(-25 * time.Hour)
	if err := db.Model(&model.MembershipOrder{}).Where("id = ?", order.ID).Updates(map[string]interface{}{
		"created_at": staleCreatedAt,
		"updated_at": staleCreatedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MembershipEntitlement(owner); err != nil {
		t.Fatal(err)
	}
	var stored model.MembershipOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.MembershipOrderCancelled || stored.ResolutionNote != "订单超过 24 小时未支付，系统自动关闭" {
		t.Fatalf("stale order was not reconciled: %#v", stored)
	}
}

func TestRenewalCreditsAreGrantedExactlyOnceAtPaymentFulfillment(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	_, owner, _ := createCommercialTestUsers(t, db)
	plan := membershipPlanByCode(t, db, "pro-month")
	firstOrder, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "renewal-first-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payMembershipOrderForTest(t, svc, db, firstOrder, "renewal-first"); err != nil {
		t.Fatal(err)
	}
	secondOrder, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "renewal-second-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payMembershipOrderForTest(t, svc, db, secondOrder, "renewal-second"); err != nil {
		t.Fatal(err)
	}
	var secondSubscription model.MembershipSubscription
	if err := db.First(&secondSubscription, "order_id = ?", secondOrder.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !secondSubscription.StartsAt.After(time.Now()) {
		t.Fatalf("renewal starts at %s, want a future start", secondSubscription.StartsAt)
	}
	var ledgerCount int64
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ?", owner.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 {
		t.Fatalf("ledger count immediately after two paid orders = %d, want 2", ledgerCount)
	}
	now := time.Now()
	if err := db.Model(&model.MembershipSubscription{}).Where("order_id = ?", firstOrder.ID).Updates(map[string]interface{}{
		"ends_at":    now.Add(-time.Minute),
		"updated_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.MembershipSubscription{}).Where("order_id = ?", secondOrder.ID).Updates(map[string]interface{}{
		"starts_at":  now.Add(-time.Minute),
		"updated_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MembershipEntitlement(owner); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Where("user_id = ?", owner.ID).Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 {
		t.Fatalf("lifecycle duplicated payment grants: ledger count = %d, want 2", ledgerCount)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", owner.ID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != plan.CreditsPerPeriod*2 {
		t.Fatalf("credits after two paid orders = %d, want %d", account.AvailableMicrocredits, plan.CreditsPerPeriod*2)
	}
}

func TestTeamPurchaseGrantsTeamCreditsAndMemberEntitlement(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	_, owner, member := createCommercialTestUsers(t, db)
	team, err := svc.CreateTeam(owner, teamCreateRequest("商业制作团队"))
	if err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "team-pro-year")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID, TeamID: team.ID, Seats: 3}, "team-membership-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := payMembershipOrderForTest(t, svc, db, order, "team-trade-001"); err != nil {
		t.Fatal(err)
	}
	invitation, err := svc.CreateTeamInvitation(owner, team.ID, CreateTeamInvitationRequest{Email: member.Email, Role: model.TeamMemberRoleMember})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptTeamInvitationByToken(member, AcceptTeamInvitationRequest{Token: invitation.AcceptToken}); err != nil {
		t.Fatal(err)
	}
	var teamAccount model.TeamCreditAccount
	if err := db.First(&teamAccount, "team_id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if teamAccount.AvailableMicrocredits != plan.CreditsPerPeriod*3 {
		t.Fatalf("team credits = %d, want %d", teamAccount.AvailableMicrocredits, plan.CreditsPerPeriod*3)
	}
	var ownerAccountCount int64
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", owner.ID).Count(&ownerAccountCount).Error; err != nil {
		t.Fatal(err)
	}
	if ownerAccountCount != 0 {
		t.Fatalf("team purchase unexpectedly credited the owner's personal account")
	}
	var memberAccountCount int64
	if err := db.Model(&model.CreditAccount{}).Where("user_id = ?", member.ID).Count(&memberAccountCount).Error; err != nil {
		t.Fatal(err)
	}
	if memberAccountCount != 0 {
		t.Fatalf("team member unexpectedly received a separate credit account")
	}
	entitlement, err := svc.MembershipEntitlement(member)
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.TeamID != team.ID || entitlement.ImageConcurrency != plan.ImageConcurrency || entitlement.VideoConcurrency != plan.VideoConcurrency {
		t.Fatalf("unexpected member entitlement: %#v", entitlement)
	}
}

func TestMembershipConcurrencyPolicyIsEnforcedAtTaskCreation(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	_, owner, _ := createCommercialTestUsers(t, db)
	runtimePolicy := defaultRuntimePolicy()
	runtimePolicy.Task.ActiveTaskLimit = 2
	imagePolicy, capability, err := svc.membershipActiveTaskPolicy(owner.ID, personalBillingAccountScope(), "canvas_image", runtimePolicy)
	if err != nil {
		t.Fatal(err)
	}
	if capability != taskCapabilityImage || imagePolicy.ClassLimit != 4 || imagePolicy.TotalLimit != 8 {
		t.Fatalf("unexpected origin image policy: %#v capability=%s", imagePolicy, capability)
	}
	// 此测试直接写入无计费订单的仓储级任务，只验证账户中立的并发计数；
	// 生产生成链路始终通过 BillingOrder 冻结并按账户范围计数。
	imagePolicy.BillingTeamID = nil
	for index := 0; index < imagePolicy.ClassLimit; index++ {
		task := &model.Task{
			ID: "image-task-" + string(rune('a'+index)), UserID: owner.ID, Type: "canvas_image",
			Capability: taskCapabilityImage, Status: model.TaskStatusQueued,
		}
		if err := svc.repo.CreateTaskWithActiveLimit(task, imagePolicy, model.WatermarkCapabilityNotApplicable); err != nil {
			t.Fatalf("create image task %d: %v", index+1, err)
		}
	}
	excess := &model.Task{ID: "image-task-excess", UserID: owner.ID, Type: "canvas_image", Capability: taskCapabilityImage, Status: model.TaskStatusQueued}
	if err := svc.repo.CreateTaskWithActiveLimit(excess, imagePolicy, model.WatermarkCapabilityNotApplicable); !errors.Is(err, repository.ErrCapabilityTaskLimit) {
		t.Fatalf("excess image task error = %v, want ErrCapabilityTaskLimit", err)
	}
	otherPolicy, _, err := svc.membershipActiveTaskPolicy(owner.ID, personalBillingAccountScope(), "canvas_text", runtimePolicy)
	if err != nil {
		t.Fatal(err)
	}
	otherPolicy.BillingTeamID = nil
	for index := 0; index < runtimePolicy.Task.ActiveTaskLimit; index++ {
		task := &model.Task{
			ID: "other-task-" + string(rune('a'+index)), UserID: owner.ID, Type: "canvas_text",
			Capability: "text", Status: model.TaskStatusRunning,
		}
		if err := svc.repo.CreateTaskWithActiveLimit(task, otherPolicy, model.WatermarkCapabilityNotApplicable); err != nil {
			t.Fatalf("create other task %d: %v", index+1, err)
		}
	}
	excessOther := &model.Task{ID: "other-task-excess", UserID: owner.ID, Type: "canvas_audio", Capability: "audio", Status: model.TaskStatusQueued}
	if err := svc.repo.CreateTaskWithActiveLimit(excessOther, otherPolicy, model.WatermarkCapabilityNotApplicable); !errors.Is(err, repository.ErrCapabilityTaskLimit) {
		t.Fatalf("excess other task error = %v, want ErrCapabilityTaskLimit", err)
	}
	if _, _, err := svc.membershipActiveTaskPolicy(owner.ID, personalBillingAccountScope(), "unknown-task", runtimePolicy); err == nil {
		t.Fatal("unknown task type unexpectedly received a fallback concurrency policy")
	}
}

func TestPaymentSecretsAreEncryptedAndBlankUpdatePreservesThem(t *testing.T) {
	svc, db := newMembershipTestService(t)
	admin, _, _ := createCommercialTestUsers(t, db)
	request := readyWechatPaymentSetting(t)
	public, err := svc.UpdatePaymentSetting(admin, request)
	if err != nil {
		t.Fatal(err)
	}
	if !public.Wechat.Ready || !public.Wechat.HasMerchantPrivateKey || !public.Wechat.HasWechatpayPublicKey || !public.Wechat.HasAPIv3Key {
		t.Fatalf("public setting does not report ready secret flags: %#v", public.Wechat)
	}
	var stored model.SystemSetting
	if err := db.First(&stored, "key = ?", paymentSettingKey).Error; err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range []string{request.Wechat.MerchantPrivateKey, request.Wechat.WechatpayPublicKey, request.Wechat.APIv3Key} {
		if strings.Contains(stored.ValueJSON, plaintext) {
			t.Fatalf("stored payment setting leaked plaintext %q", plaintext)
		}
	}
	blankSecrets := request
	blankSecrets.Wechat.MerchantPrivateKey = ""
	blankSecrets.Wechat.WechatpayPublicKey = ""
	blankSecrets.Wechat.APIv3Key = ""
	blankSecrets.Wechat.AppID = "wx-app-id-updated"
	updated, err := svc.UpdatePaymentSetting(admin, blankSecrets)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Wechat.AppID != "wx-app-id-updated" || !updated.Wechat.Ready {
		t.Fatalf("blank secret update did not preserve readiness: %#v", updated.Wechat)
	}
}

func TestPaymentCheckoutProtectsOwnershipTokenAndExpiration(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin, owner, other := createCommercialTestUsers(t, db)
	if _, err := svc.UpdatePaymentSetting(admin, readyWechatPaymentSetting(t)); err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "checkout-ownership-order")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePaymentCheckout(other, order.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-user checkout error = %v, want record not found", err)
	}
	before := time.Now()
	result, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiresAt.Before(before.Add(14*time.Minute)) || result.ExpiresAt.After(before.Add(16*time.Minute)) {
		t.Fatalf("checkout expiry = %s, want approximately 15 minutes", result.ExpiresAt)
	}
	token := checkoutToken(t, result.CheckoutURL)
	digest := sha256.Sum256([]byte(token))
	var session model.PaymentCheckoutSession
	if err := db.First(&session, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if session.TokenHash != hex.EncodeToString(digest[:]) || session.TokenHash == token {
		t.Fatalf("checkout token was not stored as its SHA-256 digest")
	}
	view, err := svc.PaymentCheckout(token)
	if err != nil {
		t.Fatal(err)
	}
	if view.OrderNumber != order.OrderNumber || view.MembershipSummary == nil || len(view.Providers) != 1 || view.Providers[0] != model.PaymentProviderWechat {
		t.Fatalf("unexpected checkout view: %#v", view)
	}
	if err := db.Model(&session).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	expiredView, err := svc.PaymentCheckout(token)
	if err != nil {
		t.Fatal(err)
	}
	if expiredView.CheckoutStatus != model.PaymentCheckoutExpired {
		t.Fatalf("expired checkout view status = %s, want expired", expiredView.CheckoutStatus)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	if session.Status != model.PaymentCheckoutExpired {
		t.Fatalf("expired checkout status = %s, want expired", session.Status)
	}
}

func TestPaymentDoesNotFabricateCheckoutOrProviderSuccess(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin, owner, _ := createCommercialTestUsers(t, db)
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "provider-failure-order")
	if err != nil {
		t.Fatal(err)
	}
	incomplete := readyWechatPaymentSetting(t)
	incomplete.Wechat.Enabled = false
	incomplete.Wechat.APIv3Key = ""
	if _, err := svc.UpdatePaymentSetting(admin, incomplete); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreatePaymentCheckout(owner, order.ID)
	requireAuthStatus(t, err, http.StatusBadRequest)
	var sessionCount int64
	if err := db.Model(&model.PaymentCheckoutSession{}).Where("order_id = ?", order.ID).Count(&sessionCount).Error; err != nil {
		t.Fatal(err)
	}
	if sessionCount != 0 {
		t.Fatalf("incomplete merchant config created %d checkout sessions", sessionCount)
	}
	if _, err := svc.UpdatePaymentSetting(admin, readyWechatPaymentSetting(t)); err != nil {
		t.Fatal(err)
	}
	result, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreatePaymentTransaction(checkoutToken(t, result.CheckoutURL), CreatePaymentTransactionRequest{Provider: model.PaymentProviderWechat})
	requireAuthStatus(t, err, http.StatusBadGateway)
	var transaction model.PaymentTransaction
	if err := db.First(&transaction, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if transaction.Status != model.PaymentTransactionReviewRequired || transaction.CodeURL != "" || transaction.FailureCode != "provider_result_unknown" || transaction.FailureReason == "" {
		t.Fatalf("provider connector failure was not recorded explicitly: %#v", transaction)
	}
	if _, err := svc.CreatePaymentTransaction(checkoutToken(t, result.CheckoutURL), CreatePaymentTransactionRequest{Provider: model.PaymentProviderWechat}); err == nil {
		t.Fatal("untrusted provider rejection unexpectedly returned success")
	}
	var transactionCount int64
	if err := db.Model(&model.PaymentTransaction{}).Where("order_id = ?", order.ID).Count(&transactionCount).Error; err != nil {
		t.Fatal(err)
	}
	if transactionCount != 1 {
		t.Fatalf("untrusted rejection transaction count = %d, want 1 retained review fact", transactionCount)
	}
}

func TestPaymentPayableProviderTimeoutKeepsSingleMerchantOrderForReconciliation(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin, owner, _ := createCommercialTestUsers(t, db)
	merchantPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
	setting := readyWechatPaymentSetting(t)
	setting.Wechat.MerchantPrivateKey = rsaPrivateKeyPEM(t, merchantPrivate)
	setting.Wechat.WechatpayPublicKey = platformPublicPEM
	setting.Wechat.APIv3Key = strings.Repeat("k", 32)
	if _, err := svc.UpdatePaymentSetting(admin, setting); err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "provider-timeout-order")
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, errors.New("injected provider timeout after request dispatch")
	}))
	token := checkoutToken(t, checkout.CheckoutURL)
	if _, err := svc.CreatePaymentTransaction(token, CreatePaymentTransactionRequest{Provider: model.PaymentProviderWechat}); err == nil {
		t.Fatal("ambiguous provider timeout unexpectedly returned success")
	}
	var first model.PaymentTransaction
	if err := db.First(&first, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.Status != model.PaymentTransactionReviewRequired || first.FailureCode != "provider_result_unknown" {
		t.Fatalf("timeout transaction = %#v", first)
	}
	if _, err := svc.CreatePaymentTransaction(token, CreatePaymentTransactionRequest{Provider: model.PaymentProviderWechat}); err == nil {
		t.Fatal("review-required replay unexpectedly created a public QR")
	}
	var count int64
	if err := db.Model(&model.PaymentTransaction{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("timeout retry created %d merchant orders, want 1", count)
	}
}

func TestPaymentPayableSameProviderReplayAndRefreshReuseOneQRWhileCrossProviderLocks(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin, owner, _ := createCommercialTestUsers(t, db)
	merchantPrivate, platformPublicPEM := newWebhookTestRSAKey(t)
	setting := readyWechatPaymentSetting(t)
	setting.Wechat.MerchantPrivateKey = rsaPrivateKeyPEM(t, merchantPrivate)
	setting.Wechat.WechatpayPublicKey = platformPublicPEM
	setting.Wechat.APIv3Key = strings.Repeat("k", 32)
	setting.Alipay = AlipayPaymentChannelSettingRequest{
		Enabled: true, AppID: "alipay-app", MerchantID: "alipay-merchant",
		MerchantPrivateKey: rsaPrivateKeyPEM(t, merchantPrivate), PlatformPublicKey: platformPublicPEM,
		NotifyURL: "https://merchant.example.com/alipay", GatewayURL: "https://openapi.alipay.com/gateway.do",
	}
	if _, err := svc.UpdatePaymentSetting(admin, setting); err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "same-provider-replay-order")
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	withPaymentHTTPTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return signedWechatTestResponse(t, request, http.StatusOK, `{"code_url":"weixin://wxpay/same-qr"}`, merchantPrivate), nil
	}))
	token := checkoutToken(t, checkout.CheckoutURL)
	first, err := svc.CreatePaymentTransaction(token, CreatePaymentTransactionRequest{Provider: model.PaymentProviderWechat})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.CreatePaymentTransaction(token, CreatePaymentTransactionRequest{Provider: model.PaymentProviderWechat})
	if err != nil {
		t.Fatal(err)
	}
	if first.CodeURL != replay.CodeURL || first.CodeURL != "weixin://wxpay/same-qr" {
		t.Fatalf("same-provider replay views first=%#v replay=%#v", first, replay)
	}
	if _, err := svc.CreatePaymentTransaction(token, CreatePaymentTransactionRequest{Provider: model.PaymentProviderAlipay}); err == nil {
		t.Fatal("cross-provider replay unexpectedly claimed a second transaction")
	} else {
		requireAuthStatus(t, err, http.StatusConflict)
	}
	refreshed, err := svc.PaymentCheckout(token)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.ActiveTransaction == nil || refreshed.ActiveTransaction.CodeURL != first.CodeURL {
		t.Fatalf("checkout refresh active transaction = %#v", refreshed.ActiveTransaction)
	}
	var count int64
	if err := db.Model(&model.PaymentTransaction{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("same/cross provider transaction count = %d, err=%v", count, err)
	}
}

func TestPaymentCheckoutRemainsReadableAfterSuccessfulPayment(t *testing.T) {
	svc, db := newMembershipTestService(t)
	if err := svc.EnsureDefaultMembershipPlans(); err != nil {
		t.Fatal(err)
	}
	admin, owner, _ := createCommercialTestUsers(t, db)
	if _, err := svc.UpdatePaymentSetting(admin, readyWechatPaymentSetting(t)); err != nil {
		t.Fatal(err)
	}
	plan := membershipPlanByCode(t, db, "pro-month")
	order, err := svc.CreateMembershipOrder(owner, CreateMembershipOrderRequest{PlanID: plan.ID}, "successful-payment-order")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CreatePaymentCheckout(owner, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := checkoutToken(t, result.CheckoutURL)
	now := time.Now()
	if err := db.Model(&model.MembershipOrder{}).Where("id = ?", order.ID).Updates(map[string]interface{}{
		"status": model.MembershipOrderPaid, "paid_at": now, "updated_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.PaymentCheckoutSession{}).Where("order_id = ?", order.ID).Updates(map[string]interface{}{
		"status": model.PaymentCheckoutConsumed, "updated_at": now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	view, err := svc.PaymentCheckout(token)
	if err != nil {
		t.Fatal(err)
	}
	if view.OrderStatus != string(model.MembershipOrderPaid) || view.CheckoutStatus != model.PaymentCheckoutConsumed {
		t.Fatalf("checkout statuses = order:%s checkout:%s, want paid/consumed", view.OrderStatus, view.CheckoutStatus)
	}
	if _, err := svc.CreatePaymentTransaction(token, CreatePaymentTransactionRequest{Provider: model.PaymentProviderWechat}); err == nil {
		t.Fatal("consumed checkout unexpectedly created another transaction")
	}
}

func TestPaymentGatewayRejectsNonOfficialOrInsecureEndpoints(t *testing.T) {
	svc, db := newMembershipTestService(t)
	admin, _, _ := createCommercialTestUsers(t, db)

	for name, gatewayURL := range map[string]string{
		"insecure scheme":      "http://api.mch.weixin.qq.com",
		"unofficial host":      "https://payments.example.com",
		"host suffix trap":     "https://api.mch.weixin.qq.com.attacker.example",
		"embedded credentials": "https://merchant:secret@api.mch.weixin.qq.com",
	} {
		t.Run(name, func(t *testing.T) {
			request := readyWechatPaymentSetting(t)
			request.Wechat.GatewayURL = gatewayURL
			_, err := svc.UpdatePaymentSetting(admin, request)
			requireAuthStatus(t, err, http.StatusBadRequest)
		})
	}
}

func TestPaymentAuditEndpointsRequireAdminAndRejectUnknownFilters(t *testing.T) {
	svc, db := newMembershipTestService(t)
	admin, owner, _ := createCommercialTestUsers(t, db)

	if _, err := svc.AdminPaymentTransactions(owner, AdminListQuery{}); err == nil {
		t.Fatal("ordinary user unexpectedly accessed payment transactions")
	} else {
		requireAuthStatus(t, err, http.StatusForbidden)
	}
	if _, err := svc.AdminPaymentWebhookEvents(owner, AdminListQuery{}); err == nil {
		t.Fatal("ordinary user unexpectedly accessed payment webhook events")
	} else {
		requireAuthStatus(t, err, http.StatusForbidden)
	}

	for name, query := range map[string]AdminListQuery{
		"unknown provider":           {Type: "bank-transfer"},
		"unknown transaction status": {Status: "successful"},
	} {
		t.Run("transactions "+name, func(t *testing.T) {
			_, err := svc.AdminPaymentTransactions(admin, query)
			requireAuthStatus(t, err, http.StatusBadRequest)
		})
	}
	for name, query := range map[string]AdminListQuery{
		"unknown provider":       {Type: "bank-transfer"},
		"unknown webhook status": {Status: "ignored"},
	} {
		t.Run("webhooks "+name, func(t *testing.T) {
			_, err := svc.AdminPaymentWebhookEvents(admin, query)
			requireAuthStatus(t, err, http.StatusBadRequest)
		})
	}
}
