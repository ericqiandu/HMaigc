package service

import (
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

type ConfirmMembershipOrderRequest struct {
	ProviderTradeNo string `json:"providerTradeNo"`
	Note            string `json:"note"`
}

type CloseMembershipOrderRequest struct {
	Note string `json:"note"`
}

func (s *Service) EnsureDefaultMembershipPlans() error {
	const teamStorage130TB int64 = 130 * (1 << 40)
	benefits := func(values ...string) string {
		payload, err := json.Marshal(values)
		if err != nil {
			panic(fmt.Sprintf("序列化内置会员权益失败: %v", err))
		}
		return string(payload)
	}
	plans := []model.MembershipPlan{
		{ID: newID(), Code: "origin-free", Name: "Origin", Tier: "origin", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleFree, Currency: "CNY", ImageConcurrency: 4, VideoConcurrency: 2, BenefitsJSON: benefits("基础图片与视频生成", "个人项目与素材空间"), Enabled: true, SortOrder: 10},
		{ID: newID(), Code: "pro-month", Name: "Pro", Tier: "pro", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth, PriceCents: 19800, OriginalPriceCents: 30900, Currency: "CNY", CreditsPerPeriod: 19800 * CreditScale, ImageConcurrency: 6, VideoConcurrency: 4, TopupDiscountBasisPoints: 8000, BenefitsJSON: benefits("每月 19,800 积分", "图片并发 6 路", "视频并发 4 路", "积分充值 8 折"), Enabled: true, SortOrder: 20},
		{ID: newID(), Code: "pro-year", Name: "Pro", Tier: "pro", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 94900, OriginalPriceCents: 237600, Currency: "CNY", CreditsPerPeriod: 19800 * 12 * CreditScale, ImageConcurrency: 6, VideoConcurrency: 4, TopupDiscountBasisPoints: 8000, BenefitsJSON: benefits("每年 237,600 积分", "图片并发 6 路", "视频并发 4 路", "积分充值 8 折"), Enabled: true, SortOrder: 21},
		{ID: newID(), Code: "max-month", Name: "Max", Tier: "max", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth, PriceCents: 64800, OriginalPriceCents: 101200, Currency: "CNY", CreditsPerPeriod: 64800 * CreditScale, ImageConcurrency: 8, VideoConcurrency: 6, TopupDiscountBasisPoints: 7500, BenefitsJSON: benefits("每月 64,800 积分", "图片并发 8 路", "视频并发 6 路", "积分充值 7.5 折"), Enabled: true, SortOrder: 30},
		{ID: newID(), Code: "max-year", Name: "Max", Tier: "max", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 310900, OriginalPriceCents: 777600, Currency: "CNY", CreditsPerPeriod: 64800 * 12 * CreditScale, ImageConcurrency: 8, VideoConcurrency: 6, TopupDiscountBasisPoints: 7500, BenefitsJSON: benefits("每年 777,600 积分", "图片并发 8 路", "视频并发 6 路", "积分充值 7.5 折"), Enabled: true, SortOrder: 31},
		{ID: newID(), Code: "ultra-month", Name: "Ultra", Tier: "ultra", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth, PriceCents: 131400, OriginalPriceCents: 205300, Currency: "CNY", CreditsPerPeriod: 131400 * CreditScale, ImageConcurrency: 12, VideoConcurrency: 8, TopupDiscountBasisPoints: 7000, BenefitsJSON: benefits("每月 131,400 积分", "图片并发 12 路", "视频并发 8 路", "积分充值 7 折"), Enabled: true, SortOrder: 40},
		{ID: newID(), Code: "ultra-year", Name: "Ultra", Tier: "ultra", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 630900, OriginalPriceCents: 1576800, Currency: "CNY", CreditsPerPeriod: 131400 * 12 * CreditScale, ImageConcurrency: 12, VideoConcurrency: 8, TopupDiscountBasisPoints: 7000, BenefitsJSON: benefits("每年 1,576,800 积分", "图片并发 12 路", "视频并发 8 路", "积分充值 7 折"), Enabled: true, SortOrder: 41},
		{ID: newID(), Code: "team-pro-year", Name: "团队 Pro", Tier: "pro", Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 149900, OriginalPriceCents: 237600, Currency: "CNY", CreditsPerPeriod: 19800 * 12 * CreditScale, ImageConcurrency: 6, VideoConcurrency: 4, UnlimitedTaskQueue: true, TeamStorageBytes: teamStorage130TB, SharedAssetsEnabled: true, ProjectPermissionsEnabled: true, InvoicingEnabled: true, CommercialUseEnabled: true, TopupDiscountBasisPoints: 9000, MinSeats: 2, MaxSeats: 200, BenefitsJSON: benefits("多人画布协作", "团队共享资产库", "团队任务不限排队（执行并发受模型渠道限制）", "团队席位管理", "积分用量管控", "项目权限管理", "发票申请与交付", "团队资产隔离", "云端存储空间 130 TB", "商业使用授权"), Enabled: true, SortOrder: 50},
		{ID: newID(), Code: "team-max-year", Name: "团队 Max", Tier: "max", Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 449900, OriginalPriceCents: 777600, Currency: "CNY", CreditsPerPeriod: 64800 * 12 * CreditScale, ImageConcurrency: 8, VideoConcurrency: 6, UnlimitedTaskQueue: true, TeamStorageBytes: teamStorage130TB, SharedAssetsEnabled: true, ProjectPermissionsEnabled: true, InvoicingEnabled: true, CommercialUseEnabled: true, TopupDiscountBasisPoints: 8500, MinSeats: 2, MaxSeats: 200, BenefitsJSON: benefits("多人画布协作", "团队共享资产库", "团队任务不限排队（执行并发受模型渠道限制）", "团队席位管理", "积分用量管控", "项目权限管理", "发票申请与交付", "团队资产隔离", "云端存储空间 130 TB", "商业使用授权"), Enabled: true, SortOrder: 60},
		{ID: newID(), Code: "team-ultra-year", Name: "团队 Ultra", Tier: "ultra", Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 809900, OriginalPriceCents: 1576800, Currency: "CNY", CreditsPerPeriod: 131400 * 12 * CreditScale, ImageConcurrency: 12, VideoConcurrency: 8, UnlimitedTaskQueue: true, TeamStorageBytes: teamStorage130TB, SharedAssetsEnabled: true, ProjectPermissionsEnabled: true, InvoicingEnabled: true, CommercialUseEnabled: true, TopupDiscountBasisPoints: 8000, MinSeats: 2, MaxSeats: 200, BenefitsJSON: benefits("多人画布协作", "团队共享资产库", "团队任务不限排队（执行并发受模型渠道限制）", "团队席位管理", "积分用量管控", "项目权限管理", "发票申请与交付", "团队资产隔离", "云端存储空间 130 TB", "商业使用授权"), Enabled: true, SortOrder: 70},
	}
	for index := range plans {
		if err := s.repo.CreateMembershipPlanIfMissing(&plans[index]); err != nil {
			return err
		}
	}
	return nil
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
			return entitlementFromPlan(plan, "", nil), nil
		}
	}
	return nil, errors.New("缺少启用的 Origin 基础套餐，无法确定并发权益")
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
	return entitlementFromPlan(snapshot, subscription.TeamID, subscription.EndsAt), nil
}

func (s *Service) billingTeamID(userID string, now time.Time) (string, error) {
	subscriptions, err := s.repo.ActiveMembershipSubscriptions(userID, now)
	if err != nil {
		return "", err
	}
	var selected *MembershipEntitlement
	for _, subscription := range subscriptions {
		candidate, parseErr := membershipEntitlementFromSubscription(subscription)
		if parseErr != nil {
			return "", parseErr
		}
		if selected == nil || candidate.ImageConcurrency+candidate.VideoConcurrency > selected.ImageConcurrency+selected.VideoConcurrency {
			selected = candidate
		}
	}
	if selected == nil {
		return "", nil
	}
	return selected.TeamID, nil
}

func entitlementFromPlan(plan model.MembershipPlan, teamID string, expiresAt *time.Time) *MembershipEntitlement {
	return &MembershipEntitlement{
		PlanID: plan.ID, PlanName: plan.Name, Tier: plan.Tier, Audience: plan.Audience,
		ImageConcurrency: plan.ImageConcurrency, VideoConcurrency: plan.VideoConcurrency,
		UnlimitedTaskQueue: plan.UnlimitedTaskQueue, TeamStorageBytes: plan.TeamStorageBytes,
		SharedAssetsEnabled: plan.SharedAssetsEnabled, ProjectPermissionsEnabled: plan.ProjectPermissionsEnabled,
		InvoicingEnabled: plan.InvoicingEnabled, CommercialUseEnabled: plan.CommercialUseEnabled,
		TopupDiscountBasis: plan.TopupDiscountBasisPoints, TeamID: teamID, ExpiresAt: expiresAt,
	}
}

func (s *Service) CreateMembershipOrder(user *model.User, req CreateMembershipOrderRequest) (*model.MembershipOrder, error) {
	plan, err := s.repo.MembershipPlan(strings.TrimSpace(req.PlanID))
	if err != nil || !plan.Enabled || plan.BillingCycle == model.MembershipBillingCycleFree {
		return nil, BadAuthRequest("套餐不存在、已下架或不可购买")
	}
	seats := 1
	teamID := ""
	if plan.Audience == model.MembershipAudienceTeam {
		seats = req.Seats
		if seats < plan.MinSeats || seats > plan.MaxSeats {
			return nil, BadAuthRequest(fmt.Sprintf("团队席位数必须在 %d 到 %d 之间", plan.MinSeats, plan.MaxSeats))
		}
		team, teamErr := s.repo.TeamForOwner(user.ID, strings.TrimSpace(req.TeamID))
		if teamErr != nil || team.Status != model.TeamStatusActive {
			return nil, BadAuthRequest("团队不存在或当前用户不是团队所有者")
		}
		teamID = team.ID
	}
	snapshot, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	order := &model.MembershipOrder{ID: newID(), OrderNumber: "M" + time.Now().Format("20060102150405") + strings.ToUpper(newID()[:6]), UserID: user.ID, TeamID: teamID, PlanID: plan.ID, Seats: seats, UnitPriceCents: plan.PriceCents, TotalPriceCents: plan.PriceCents * int64(seats), Currency: plan.Currency, Status: model.MembershipOrderPending, PlanSnapshotJSON: string(snapshot)}
	if err := s.repo.Create(order); err != nil {
		return nil, err
	}
	return order, nil
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
	if err := s.repo.CloseMembershipOrder(orderID, user.ID, user.ID, "用户主动取消订单", now); err != nil {
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
	if err := s.repo.CloseMembershipOrder(orderID, "", actor.ID, note, now); err != nil {
		if errors.Is(err, repository.ErrMembershipOrderNotPending) {
			return nil, &AuthError{Status: http.StatusConflict, Message: "订单已处理，不能关闭"}
		}
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "membership_order.close", "membership_order", orderID, "关闭待支付会员订单", req); err != nil {
		return nil, err
	}
	return s.repo.MembershipOrder(orderID)
}

func (s *Service) AdminConfirmMembershipOrder(actor *model.User, id string, req ConfirmMembershipOrderRequest) (*model.MembershipOrder, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.reconcileMembershipLifecycle(now); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.ProviderTradeNo) == "" || strings.TrimSpace(req.Note) == "" {
		return nil, BadAuthRequest("支付渠道交易号和人工核验备注不能为空")
	}
	order, err := s.repo.MembershipOrder(id)
	if err != nil {
		return nil, err
	}
	subscription, ledger, err := s.membershipFulfillmentForOrder(order, actor.ID, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ActivateMembershipOrder(order.ID, actor.ID, "manual", strings.TrimSpace(req.ProviderTradeNo), strings.TrimSpace(req.Note), subscription, ledger); err != nil {
		if errors.Is(err, repository.ErrMembershipOrderNotPending) {
			return nil, &AuthError{Status: http.StatusConflict, Message: "订单已处理，不能重复开通"}
		}
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "membership_order.confirm", "membership_order", order.ID, "确认会员订单并开通订阅", req); err != nil {
		return nil, err
	}
	return s.repo.MembershipOrder(order.ID)
}

func (s *Service) membershipFulfillmentForOrder(order *model.MembershipOrder, actorID string, now time.Time) (*model.MembershipSubscription, *model.CreditLedgerEntry, error) {
	plan, err := membershipPlanFromOrderSnapshot(order)
	if err != nil {
		return nil, nil, err
	}
	start := now
	latestEnd, err := s.repo.LatestMembershipSubscriptionEnd(order.UserID, order.TeamID, now)
	if err != nil {
		return nil, nil, err
	}
	if latestEnd != nil && latestEnd.After(start) {
		start = *latestEnd
	}
	end := start.AddDate(0, 1, 0)
	if plan.BillingCycle == model.MembershipBillingCycleYear {
		end = start.AddDate(1, 0, 0)
	}
	subscription := &model.MembershipSubscription{
		ID: newID(), UserID: order.UserID, TeamID: order.TeamID, PlanID: plan.ID, OrderID: order.ID,
		Status: model.MembershipSubscriptionActive, Seats: order.Seats, PlanSnapshotJSON: order.PlanSnapshotJSON,
		StartsAt: start, EndsAt: &end, CreatedAt: now, UpdatedAt: now,
	}
	grant := plan.CreditsPerPeriod * int64(order.Seats)
	if grant == 0 || start.After(now) {
		return subscription, nil, nil
	}
	reference := "membership-order:" + order.ID
	ledger := &model.CreditLedgerEntry{
		ID: newID(), UserID: order.UserID, Type: model.CreditLedgerMembership,
		AmountMicrocredits: grant, AvailableDeltaMicrocredits: grant, ActorUserID: actorID,
		Scene: "membership", Note: "会员套餐积分到账", ReferenceKey: &reference,
		CreatedAt: now,
	}
	return subscription, ledger, nil
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
		grant := plan.CreditsPerPeriod * int64(subscription.Seats)
		if grant <= 0 {
			continue
		}
		reference := "membership-order:" + subscription.OrderID
		ledger := &model.CreditLedgerEntry{
			ID: newID(), UserID: subscription.UserID, Type: model.CreditLedgerMembership,
			AmountMicrocredits: grant, AvailableDeltaMicrocredits: grant, ActorUserID: "system",
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
	if order == nil {
		return nil, errors.New("会员订单不能为空")
	}
	if strings.TrimSpace(order.PlanSnapshotJSON) == "" {
		return nil, fmt.Errorf("会员订单 %s 缺少套餐快照，禁止开通权益", order.ID)
	}
	var snapshot model.MembershipPlan
	if err := json.Unmarshal([]byte(order.PlanSnapshotJSON), &snapshot); err != nil {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照损坏: %w", order.ID, err)
	}
	if snapshot.ID == "" || snapshot.ID != order.PlanID {
		return nil, fmt.Errorf("会员订单 %s 的套餐快照与订单套餐不一致", order.ID)
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
	return &snapshot, nil
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
