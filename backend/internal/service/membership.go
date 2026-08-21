package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type MembershipPlanView struct {
	model.MembershipPlan
	Benefits []string `json:"benefits"`
}

type MembershipEntitlement struct {
	PlanID                    string                   `json:"planId"`
	PlanName                  string                   `json:"planName"`
	Tier                      string                   `json:"tier"`
	Audience                  model.MembershipAudience `json:"audience"`
	IsActiveMember            bool                     `json:"isActiveMember"`
	ImageConcurrency          int                      `json:"imageConcurrency"`
	VideoConcurrency          int                      `json:"videoConcurrency"`
	UnlimitedTaskQueue        bool                     `json:"unlimitedTaskQueue"`
	TeamStorageBytes          int64                    `json:"teamStorageBytes"`
	SharedAssetsEnabled       bool                     `json:"sharedAssetsEnabled"`
	ProjectPermissionsEnabled bool                     `json:"projectPermissionsEnabled"`
	InvoicingEnabled          bool                     `json:"invoicingEnabled"`
	CommercialUseEnabled      bool                     `json:"commercialUseEnabled"`
	TopupDiscountBasis        int                      `json:"topupDiscountBasisPoints"`
	TeamID                    string                   `json:"teamId,omitempty"`
	ExpiresAt                 *time.Time               `json:"expiresAt,omitempty"`
}

type CreateMembershipOrderRequest struct {
	PlanID string `json:"planId"`
	TeamID string `json:"teamId"`
	Seats  int    `json:"seats"`
}

type UpdateMembershipPlanRequest struct {
	Name                      string   `json:"name"`
	PriceCents                int64    `json:"priceCents"`
	OriginalPriceCents        int64    `json:"originalPriceCents"`
	CreditsPerPeriod          int64    `json:"creditsPerPeriod"`
	ImageConcurrency          int      `json:"imageConcurrency"`
	VideoConcurrency          int      `json:"videoConcurrency"`
	UnlimitedTaskQueue        bool     `json:"unlimitedTaskQueue"`
	TeamStorageBytes          int64    `json:"teamStorageBytes"`
	SharedAssetsEnabled       bool     `json:"sharedAssetsEnabled"`
	ProjectPermissionsEnabled bool     `json:"projectPermissionsEnabled"`
	InvoicingEnabled          bool     `json:"invoicingEnabled"`
	CommercialUseEnabled      bool     `json:"commercialUseEnabled"`
	TopupDiscountBasisPoints  int      `json:"topupDiscountBasisPoints"`
	MinSeats                  int      `json:"minSeats"`
	MaxSeats                  int      `json:"maxSeats"`
	Benefits                  []string `json:"benefits"`
	Enabled                   bool     `json:"enabled"`
	SortOrder                 int      `json:"sortOrder"`
}

type CloseMembershipOrderRequest struct {
	Note string `json:"note"`
}

type membershipOrderRequestFingerprint struct {
	PlanID string `json:"planId"`
	TeamID string `json:"teamId"`
	Seats  int    `json:"seats"`
}

func (s *Service) MembershipPlans(admin *model.User) ([]MembershipPlanView, error) {
	plans, err := s.repo.MembershipPlans(admin == nil)
	if err != nil {
		return nil, err
	}
	if admin != nil {
		if err := s.RequireAdmin(admin); err != nil {
			return nil, err
		}
	}
	plans = currentMembershipCatalogPlans(plans)
	return membershipPlanViews(plans)
}

func membershipPlanViews(plans []model.MembershipPlan) ([]MembershipPlanView, error) {
	views := make([]MembershipPlanView, 0, len(plans))
	for _, plan := range plans {
		benefits, err := membershipPlanBenefits(plan)
		if err != nil {
			return nil, err
		}
		views = append(views, MembershipPlanView{MembershipPlan: plan, Benefits: benefits})
	}
	return views, nil
}

// membershipPlanBenefits keeps operational team entitlements as the public source of truth.
// Personal plans still use editable marketing copy because they have no team capability switches.
func membershipPlanBenefits(plan model.MembershipPlan) ([]string, error) {
	if plan.Audience != model.MembershipAudienceTeam {
		var benefits []string
		if err := json.Unmarshal([]byte(plan.BenefitsJSON), &benefits); err != nil {
			return nil, fmt.Errorf("套餐 %s 的权益配置损坏: %w", plan.Code, err)
		}
		return benefits, nil
	}
	benefits := []string{"多人画布协作"}
	if plan.SharedAssetsEnabled {
		benefits = append(benefits, "团队共享资产库")
	}
	if plan.UnlimitedTaskQueue {
		benefits = append(benefits, "团队任务不限排队（执行并发受模型渠道限制）")
	}
	benefits = append(benefits, "团队席位管理", "积分用量管控")
	if plan.ProjectPermissionsEnabled {
		benefits = append(benefits, "项目权限管理")
	}
	if plan.InvoicingEnabled {
		benefits = append(benefits, "发票申请与交付")
	}
	benefits = append(benefits, "团队资产隔离")
	if plan.TeamStorageBytes > 0 {
		benefits = append(benefits, "云端存储空间 "+formatTeamStorage(plan.TeamStorageBytes))
	}
	if plan.CommercialUseEnabled {
		benefits = append(benefits, "商业使用授权")
	}
	return benefits, nil
}

func formatTeamStorage(bytes int64) string {
	const tebibyte int64 = 1 << 40
	if bytes%tebibyte == 0 {
		return fmt.Sprintf("%d TB", bytes/tebibyte)
	}
	return fmt.Sprintf("%.1f TB", float64(bytes)/float64(tebibyte))
}

func (s *Service) MembershipEntitlement(user *model.User) (*MembershipEntitlement, error) {
	now := time.Now()
	if err := s.reconcileMembershipLifecycle(now); err != nil {
		return nil, err
	}
	subscriptions, err := s.repo.ActiveMembershipSubscriptions(user.ID, now)
	if err != nil {
		return nil, err
	}
	var selected *MembershipEntitlement
	for _, subscription := range subscriptions {
		candidate, entitlementErr := membershipEntitlementFromSubscription(subscription)
		if entitlementErr != nil {
			return nil, entitlementErr
		}
		if selected == nil || candidate.ImageConcurrency+candidate.VideoConcurrency > selected.ImageConcurrency+selected.VideoConcurrency {
			selected = candidate
		}
	}
	if selected != nil {
		return selected, nil
	}
	plans, err := s.repo.MembershipPlans(true)
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if plan.Code == "origin-free" {
			return entitlementFromPlan(plan, false, "", nil), nil
		}
	}
	return nil, errors.New("缺少启用的 Origin 基础套餐，无法确定并发权益")
}

func (s *Service) membershipEntitlementForBillingAccount(userID string, billingScope billingAccountScope) (*MembershipEntitlement, error) {
	now := time.Now()
	if err := s.reconcileMembershipLifecycle(now); err != nil {
		return nil, err
	}
	subscriptions, err := s.repo.ActiveMembershipSubscriptionsForBillingAccount(userID, billingScope.TeamID, now)
	if err != nil {
		return nil, err
	}
	var selected *MembershipEntitlement
	for _, subscription := range subscriptions {
		candidate, entitlementErr := membershipEntitlementFromSubscription(subscription)
		if entitlementErr != nil {
			return nil, entitlementErr
		}
		if selected == nil || candidate.ImageConcurrency+candidate.VideoConcurrency > selected.ImageConcurrency+selected.VideoConcurrency {
			selected = candidate
		}
	}
	if selected != nil {
		return selected, nil
	}
	if billingScope.TeamID != "" {
		return nil, &AuthError{Status: http.StatusPaymentRequired, Message: "当前团队没有有效会员，无法创建生成任务"}
	}
	plans, err := s.repo.MembershipPlans(true)
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		if plan.Code == "origin-free" {
			return entitlementFromPlan(plan, false, "", nil), nil
		}
	}
	return nil, errors.New("缺少启用的 Origin 基础套餐，无法确定个人并发权益")
}

// HasActiveMembership 只认数据库中的有效个人订阅或有效团队席位；Origin 基础权益不属于付费会员。
func (s *Service) HasActiveMembership(userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, Unauthorized("请先登录")
	}
	now := time.Now()
	subscriptions, err := s.repo.ActiveMembershipSubscriptions(userID, now)
	if err != nil {
		return false, err
	}
	return len(subscriptions) > 0, nil
}

func membershipEntitlementFromSubscription(subscription model.MembershipSubscription) (*MembershipEntitlement, error) {
	if strings.TrimSpace(subscription.PlanSnapshotJSON) == "" {
		return nil, fmt.Errorf("有效订阅 %s 缺少套餐快照，无法确定已购权益", subscription.ID)
	}
	var snapshot model.MembershipPlan
	if err := json.Unmarshal([]byte(subscription.PlanSnapshotJSON), &snapshot); err != nil {
		return nil, fmt.Errorf("有效订阅 %s 的套餐快照损坏: %w", subscription.ID, err)
	}
	if snapshot.ID == "" || snapshot.ID != subscription.PlanID {
		return nil, fmt.Errorf("有效订阅 %s 的套餐快照与订阅套餐不一致", subscription.ID)
	}
	if strings.TrimSpace(snapshot.Name) == "" || strings.TrimSpace(snapshot.Tier) == "" {
		return nil, fmt.Errorf("有效订阅 %s 的套餐快照缺少名称或等级", subscription.ID)
	}
	if snapshot.Audience != model.MembershipAudiencePersonal && snapshot.Audience != model.MembershipAudienceTeam {
		return nil, fmt.Errorf("有效订阅 %s 的套餐快照受众无效", subscription.ID)
	}
	if snapshot.ImageConcurrency < 1 || snapshot.VideoConcurrency < 1 {
		return nil, fmt.Errorf("有效订阅 %s 的套餐快照并发权益无效", subscription.ID)
	}
	if snapshot.TopupDiscountBasisPoints < 0 || snapshot.TopupDiscountBasisPoints > 10000 {
		return nil, fmt.Errorf("有效订阅 %s 的套餐快照充值折扣无效", subscription.ID)
	}
	return entitlementFromPlan(snapshot, true, subscription.TeamID, subscription.EndsAt), nil
}

func entitlementFromPlan(plan model.MembershipPlan, isActiveMember bool, teamID string, expiresAt *time.Time) *MembershipEntitlement {
	return &MembershipEntitlement{
		PlanID: plan.ID, PlanName: plan.Name, Tier: plan.Tier, Audience: plan.Audience,
		IsActiveMember:   isActiveMember,
		ImageConcurrency: plan.ImageConcurrency, VideoConcurrency: plan.VideoConcurrency,
		UnlimitedTaskQueue: plan.UnlimitedTaskQueue, TeamStorageBytes: plan.TeamStorageBytes,
		SharedAssetsEnabled: plan.SharedAssetsEnabled, ProjectPermissionsEnabled: plan.ProjectPermissionsEnabled,
		InvoicingEnabled: plan.InvoicingEnabled, CommercialUseEnabled: plan.CommercialUseEnabled,
		TopupDiscountBasis: plan.TopupDiscountBasisPoints, TeamID: teamID, ExpiresAt: expiresAt,
	}
}

func (s *Service) CreateMembershipOrder(user *model.User, req CreateMembershipOrderRequest, idempotencyKey string) (*model.MembershipOrder, error) {
	if user == nil || strings.TrimSpace(user.ID) == "" {
		return nil, Unauthorized("请先登录")
	}
	if len(idempotencyKey) < 1 || len(idempotencyKey) > 120 {
		return nil, BadAuthRequest("Idempotency-Key 必须为 1 到 120 字节")
	}
	fingerprint := membershipOrderRequestFingerprint{
		PlanID: strings.TrimSpace(req.PlanID),
		TeamID: strings.TrimSpace(req.TeamID),
		Seats:  req.Seats,
	}
	if fingerprint.Seats == 0 {
		fingerprint.Seats = 1
	}
	requestHash, err := membershipOrderRequestHash(fingerprint)
	if err != nil {
		return nil, err
	}
	existing, err := s.repo.MembershipOrderByIdempotencyKey(user.ID, idempotencyKey)
	if err == nil {
		return matchingMembershipOrderReplay(existing, requestHash)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	plan, err := s.repo.MembershipPlan(fingerprint.PlanID)
	if err != nil || !plan.Enabled || plan.BillingCycle == model.MembershipBillingCycleFree {
		return nil, BadAuthRequest("套餐不存在、已下架或不可购买")
	}
	if err := validatePaidMembershipPlanPrice(plan); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	seats := 1
	teamID := ""
	if plan.Audience == model.MembershipAudienceTeam {
		seats = fingerprint.Seats
		if seats < plan.MinSeats || seats > plan.MaxSeats {
			return nil, BadAuthRequest(fmt.Sprintf("团队席位数必须在 %d 到 %d 之间", plan.MinSeats, plan.MaxSeats))
		}
		team, teamErr := s.repo.TeamForOwner(user.ID, fingerprint.TeamID)
		if teamErr != nil || team.Status != model.TeamStatusActive {
			return nil, BadAuthRequest("团队不存在或当前用户不是团队所有者")
		}
		teamID = team.ID
	} else if fingerprint.TeamID != "" || fingerprint.Seats != 1 {
		return nil, BadAuthRequest("个人套餐不能指定团队或多人席位")
	}
	snapshot, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	totalPriceCents, err := checkedInt64Product(plan.PriceCents, int64(seats), "会员订单总价")
	if err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	order := &model.MembershipOrder{
		ID: newID(), OrderNumber: "M" + time.Now().Format("20060102150405") + strings.ToUpper(newID()[:6]),
		UserID: user.ID, IdempotencyKey: idempotencyKey, RequestHash: requestHash,
		TeamID: teamID, PlanID: plan.ID, Seats: seats, UnitPriceCents: plan.PriceCents,
		TotalPriceCents: totalPriceCents, Currency: plan.Currency,
		Status: model.MembershipOrderPending, PlanSnapshotJSON: string(snapshot),
	}
	winner, err := s.repo.CreateMembershipOrder(order)
	if err != nil {
		return nil, err
	}
	return matchingMembershipOrderReplay(winner, requestHash)
}

func membershipOrderRequestHash(fingerprint membershipOrderRequestFingerprint) (string, error) {
	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func matchingMembershipOrderReplay(order *model.MembershipOrder, requestHash string) (*model.MembershipOrder, error) {
	if order == nil || order.RequestHash != requestHash {
		return nil, &AuthError{Status: http.StatusConflict, Message: "Idempotency-Key 已绑定到不同的会员购买请求"}
	}
	return order, nil
}

func validatePaidMembershipPlanPrice(plan *model.MembershipPlan) error {
	if plan == nil {
		return errors.New("会员套餐不能为空")
	}
	if (plan.BillingCycle == model.MembershipBillingCycleMonth || plan.BillingCycle == model.MembershipBillingCycleYear) && plan.PriceCents <= 0 {
		return fmt.Errorf("付费套餐 %s 的价格必须大于 0", plan.Code)
	}
	return nil
}

func (s *Service) MyMembership(user *model.User) (*MembershipEntitlement, []model.MembershipOrder, []model.Team, error) {
	entitlement, err := s.MembershipEntitlement(user)
	if err != nil {
		return nil, nil, nil, err
	}
	orders, _, err := s.repo.MembershipOrders(user.ID, 30, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	teams, err := s.repo.TeamsForUser(user.ID)
	return entitlement, orders, teams, err
}

func (s *Service) CancelMembershipOrder(user *model.User, id string) (*model.MembershipOrder, error) {
	orderID := strings.TrimSpace(id)
	if orderID == "" {
		return nil, BadAuthRequest("订单 ID 不能为空")
	}
	now := time.Now()
	if err := s.repo.CloseMembershipOrder(orderID, user.ID, user.ID, "用户主动取消订单", now, nil); err != nil {
		if errors.Is(err, repository.ErrPaymentReconciliationRequired) {
			return nil, &AuthError{Status: http.StatusConflict, Message: "订单存在待对账支付交易，不能取消"}
		}
		if errors.Is(err, repository.ErrMembershipOrderNotPending) {
			return nil, &AuthError{Status: http.StatusConflict, Message: "订单已处理，不能取消"}
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, &AuthError{Status: http.StatusNotFound, Message: "订单不存在"}
		}
		return nil, err
	}
	return s.repo.MembershipOrderForUser(user.ID, orderID)
}

func (s *Service) AdminUpdateMembershipPlan(actor *model.User, id string, req UpdateMembershipPlanRequest) (*model.MembershipPlan, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	plan, err := s.repo.MembershipPlan(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Name) == "" || req.PriceCents < 0 || req.CreditsPerPeriod < 0 || req.ImageConcurrency < 1 || req.VideoConcurrency < 1 || req.TopupDiscountBasisPoints < 0 || req.TopupDiscountBasisPoints > 10000 {
		return nil, BadAuthRequest("套餐名称、价格、积分、并发或折扣配置无效")
	}
	priceCandidate := *plan
	priceCandidate.PriceCents = req.PriceCents
	if err := validatePaidMembershipPlanPrice(&priceCandidate); err != nil {
		return nil, BadAuthRequest(err.Error())
	}
	if plan.Audience == model.MembershipAudienceTeam && (req.MinSeats < 2 || req.MaxSeats < req.MinSeats) {
		return nil, BadAuthRequest("团队套餐席位范围无效")
	}
	if req.OriginalPriceCents < 0 || req.TeamStorageBytes < 0 {
		return nil, BadAuthRequest("套餐原价或团队存储额度无效")
	}
	if plan.Audience == model.MembershipAudienceTeam {
		if req.SharedAssetsEnabled && req.TeamStorageBytes <= 0 {
			return nil, BadAuthRequest("启用团队共享资产库时必须配置有效存储额度")
		}
	} else if req.UnlimitedTaskQueue || req.TeamStorageBytes != 0 || req.SharedAssetsEnabled || req.ProjectPermissionsEnabled || req.InvoicingEnabled || req.CommercialUseEnabled {
		return nil, BadAuthRequest("个人套餐不能配置团队商业权益")
	}
	benefits, err := json.Marshal(req.Benefits)
	if err != nil {
		return nil, err
	}
	plan.Name, plan.PriceCents, plan.OriginalPriceCents = strings.TrimSpace(req.Name), req.PriceCents, req.OriginalPriceCents
	plan.CreditsPerPeriod, plan.ImageConcurrency, plan.VideoConcurrency = req.CreditsPerPeriod, req.ImageConcurrency, req.VideoConcurrency
	plan.UnlimitedTaskQueue, plan.TeamStorageBytes = req.UnlimitedTaskQueue, req.TeamStorageBytes
	plan.SharedAssetsEnabled, plan.ProjectPermissionsEnabled = req.SharedAssetsEnabled, req.ProjectPermissionsEnabled
	plan.InvoicingEnabled, plan.CommercialUseEnabled = req.InvoicingEnabled, req.CommercialUseEnabled
	plan.TopupDiscountBasisPoints, plan.MinSeats, plan.MaxSeats = req.TopupDiscountBasisPoints, req.MinSeats, req.MaxSeats
	plan.BenefitsJSON, plan.Enabled, plan.SortOrder = string(benefits), req.Enabled, req.SortOrder
	audit, err := newAdminAuditEvent(actor, "membership_plan.update", "membership_plan", plan.ID, "更新会员套餐", req)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SaveMembershipPlan(plan, audit); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *Service) AdminMembershipOrders(actor *model.User, page int, limit int) ([]model.MembershipOrder, int64, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, 0, err
	}
	if err := s.reconcileMembershipLifecycle(time.Now()); err != nil {
		return nil, 0, err
	}
	page, limit = normalizePage(page, limit)
	return s.repo.MembershipOrders("", limit, (page-1)*limit)
}

func (s *Service) AdminCloseMembershipOrder(actor *model.User, id string, req CloseMembershipOrderRequest) (*model.MembershipOrder, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		return nil, BadAuthRequest("关闭原因不能为空")
	}
	orderID := strings.TrimSpace(id)
	now := time.Now()
	audit, err := newAdminAuditEvent(actor, "membership_order.close", "membership_order", orderID, "关闭待支付会员订单", req)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CloseMembershipOrder(orderID, "", actor.ID, note, now, audit); err != nil {
		if errors.Is(err, repository.ErrPaymentReconciliationRequired) {
			return nil, &AuthError{Status: http.StatusConflict, Message: "订单存在待对账支付交易，不能关闭"}
		}
		if errors.Is(err, repository.ErrMembershipOrderNotPending) {
			return nil, &AuthError{Status: http.StatusConflict, Message: "订单已处理，不能关闭"}
		}
		return nil, err
	}
	return s.repo.MembershipOrder(orderID)
}

func (s *Service) membershipFulfillmentForOrder(order *model.MembershipOrder, actorID string, now time.Time) (repository.MembershipActivation, error) {
	facts, err := validatedMembershipOrderSnapshot(order)
	if err != nil {
		return repository.MembershipActivation{}, err
	}
	plan := &facts.Plan
	subscription := &model.MembershipSubscription{
		ID: newID(), UserID: order.UserID, TeamID: order.TeamID, PlanID: plan.ID, OrderID: order.ID,
		Status: model.MembershipSubscriptionActive, Seats: order.Seats, PlanSnapshotJSON: order.PlanSnapshotJSON,
		CreatedAt: now, UpdatedAt: now,
	}
	activation := repository.MembershipActivation{BillingCycle: plan.BillingCycle, Subscription: subscription}
	grant := facts.TotalCreditsPerPeriod
	if grant > 0 {
		reference := "membership-order:" + order.ID
		activation.MembershipLedger = &model.CreditLedgerEntry{
			ID: newID(), UserID: order.UserID, Type: model.CreditLedgerMembership,
			AmountMicrocredits: grant, AvailableDeltaMicrocredits: grant, ActorUserID: actorID,
			Scene: "membership", Note: "会员套餐积分到账", ReferenceKey: &reference,
			CreatedAt: now,
		}
	}
	referral, err := s.referralFulfillmentForOrder(order, plan, now)
	if err != nil {
		return repository.MembershipActivation{}, err
	}
	activation.Referral = referral
	return activation, nil
}

func (s *Service) referralFulfillmentForOrder(order *model.MembershipOrder, plan *model.MembershipPlan, now time.Time) (*repository.ReferralFulfillment, error) {
	if order.TeamID != "" || plan.Audience != model.MembershipAudiencePersonal ||
		plan.BillingCycle == model.MembershipBillingCycleFree || plan.PriceCents <= 0 {
		return nil, nil
	}
	relationship, err := s.repo.ReferralRelationshipForInvitee(order.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if relationship.Status == model.ReferralRelationshipDisqualified || relationship.Status == model.ReferralRelationshipRewarded {
		return nil, nil
	}
	hasPriorPaidOrder, err := s.repo.HasPaidPersonalMembershipOrder(order.UserID, order.ID)
	if err != nil {
		return nil, err
	}
	if hasPriorPaidOrder {
		return nil, nil
	}
	rule, err := s.repo.ReferralRewardRuleForPlan(plan.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("该个人会员套餐缺少邀请奖励规则，订单暂不能履约")
	}
	if err != nil {
		return nil, err
	}
	if !rule.Enabled || rule.InviterRewardMicrocredits <= 0 || rule.InviteeRewardMicrocredits <= 0 {
		return nil, errors.New("该个人会员套餐的邀请奖励规则未生效，订单暂不能履约")
	}
	rewardID := newID()
	inviterReference := "referral-reward:" + rewardID + ":inviter"
	inviteeReference := "referral-reward:" + rewardID + ":invitee"
	reward := &model.ReferralReward{
		ID: rewardID, RelationshipID: relationship.ID, MembershipOrderID: order.ID,
		MembershipPlanID: plan.ID, RewardRuleID: rule.ID,
		InviterUserID: relationship.InviterUserID, InviteeUserID: order.UserID,
		InviterRewardMicrocredits: rule.InviterRewardMicrocredits,
		InviteeRewardMicrocredits: rule.InviteeRewardMicrocredits,
		Status:                    model.ReferralRewardGranted, GrantedAt: now, CreatedAt: now,
	}
	return &repository.ReferralFulfillment{
		Reward: reward,
		InviterLedger: &model.CreditLedgerEntry{
			ID: newID(), UserID: relationship.InviterUserID, Type: model.CreditLedgerReferralInviter,
			AmountMicrocredits: rule.InviterRewardMicrocredits, AvailableDeltaMicrocredits: rule.InviterRewardMicrocredits,
			ActorUserID: model.SystemActorID, Scene: "referral", Note: "邀请好友首购会员奖励",
			ReferenceKey: &inviterReference, CreatedAt: now,
		},
		InviteeLedger: &model.CreditLedgerEntry{
			ID: newID(), UserID: order.UserID, Type: model.CreditLedgerReferralInvitee,
			AmountMicrocredits: rule.InviteeRewardMicrocredits, AvailableDeltaMicrocredits: rule.InviteeRewardMicrocredits,
			ActorUserID: model.SystemActorID, Scene: "referral", Note: "受邀首购会员奖励",
			ReferenceKey: &inviteeReference, CreatedAt: now,
		},
	}, nil
}

func (s *Service) reconcileMembershipLifecycle(now time.Time) error {
	const unpaidOrderLifetime = 24 * time.Hour
	if err := s.repo.ReconcileMembershipLifecycle(now, now.Add(-unpaidOrderLifetime)); err != nil {
		return err
	}
	subscriptions, err := s.repo.MembershipSubscriptionsAwaitingCreditGrant(now)
	if err != nil {
		return err
	}
	for index := range subscriptions {
		subscription := &subscriptions[index]
		order := &model.MembershipOrder{
			ID:               subscription.OrderID,
			PlanID:           subscription.PlanID,
			PlanSnapshotJSON: subscription.PlanSnapshotJSON,
		}
		plan, parseErr := membershipPlanFromSubscriptionSnapshot(order)
		if parseErr != nil {
			return parseErr
		}
		grant, multiplyErr := checkedInt64Product(plan.CreditsPerPeriod, int64(subscription.Seats), "会员订阅周期积分")
		if multiplyErr != nil {
			return fmt.Errorf("会员订阅 %s: %w", subscription.ID, multiplyErr)
		}
		if grant <= 0 {
			continue
		}
		reference := "membership-order:" + subscription.OrderID
		ledger := &model.CreditLedgerEntry{
			ID: newID(), UserID: subscription.UserID, Type: model.CreditLedgerMembership,
			AmountMicrocredits: grant, AvailableDeltaMicrocredits: grant, ActorUserID: model.SystemActorID,
			Scene: "membership", Note: "会员套餐积分到账", ReferenceKey: &reference,
			CreatedAt: now,
		}
		if grantErr := s.repo.GrantMembershipSubscriptionCredits(subscription, ledger, now); grantErr != nil {
			return grantErr
		}
	}
	return nil
}

func membershipPlanFromSubscriptionSnapshot(order *model.MembershipOrder) (*model.MembershipPlan, error) {
	if order == nil || strings.TrimSpace(order.PlanSnapshotJSON) == "" {
		return nil, errors.New("会员订阅缺少套餐快照，无法发放积分")
	}
	var snapshot model.MembershipPlan
	if err := json.Unmarshal([]byte(order.PlanSnapshotJSON), &snapshot); err != nil {
		return nil, fmt.Errorf("会员订阅 %s 的套餐快照损坏: %w", order.ID, err)
	}
	if snapshot.ID == "" || snapshot.ID != order.PlanID || snapshot.CreditsPerPeriod < 0 {
		return nil, fmt.Errorf("会员订阅 %s 的套餐快照权益无效", order.ID)
	}
	return &snapshot, nil
}

func membershipPlanFromOrderSnapshot(order *model.MembershipOrder) (*model.MembershipPlan, error) {
	snapshot, err := parseMembershipOrderPlanSnapshot(order)
	if err != nil {
		return nil, err
	}
	if snapshot.PriceCents != order.UnitPriceCents || snapshot.Currency != order.Currency {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照金额或币种与订单不一致", order.ID)
	}
	if snapshot.BillingCycle != model.MembershipBillingCycleMonth && snapshot.BillingCycle != model.MembershipBillingCycleYear {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照计费周期无效", order.ID)
	}
	if snapshot.CreditsPerPeriod < 0 || snapshot.ImageConcurrency < 1 || snapshot.VideoConcurrency < 1 {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照权益无效", order.ID)
	}
	return snapshot, nil
}

func normalizePage(page int, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 30
	}
	return page, limit
}
