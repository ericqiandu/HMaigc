package service

import (
	"encoding/json"
	"fmt"

	"infinite-canvas/backend/internal/model"
)

const (
	membershipCatalogRevisionSettingKey       = "membership.catalog.revision"
	membershipCatalogRevisionValue            = `"frontend-catalog-2026-08-09-v1"`
	teamStorage130TB                    int64 = 130 * (1 << 40)
)

var membershipCatalogCodes = map[string]struct{}{
	"origin-free": {},
	"pro-month":   {}, "pro-year": {},
	"max-month": {}, "max-year": {},
	"ultra-month": {}, "ultra-year": {},
	"team-pro-year": {}, "team-max-year": {}, "team-ultra-year": {},
}

func (s *Service) EnsureDefaultMembershipPlans() error {
	return s.repo.ApplyMembershipPlanCatalogRevision(
		membershipCatalogRevisionSettingKey,
		membershipCatalogRevisionValue,
		defaultMembershipCatalog(),
	)
}

func defaultMembershipCatalog() []model.MembershipPlan {
	benefits := func(values ...string) string {
		payload, err := json.Marshal(values)
		if err != nil {
			panic(fmt.Sprintf("序列化内置会员权益失败: %v", err))
		}
		return string(payload)
	}
	teamBenefits := benefits(
		"多人画布协作", "团队共享资产库", "团队任务不限排队（执行并发受模型渠道限制）", "团队席位管理",
		"积分用量管控", "项目权限管理", "发票申请与交付", "团队资产隔离", "云端存储空间 130 TB", "商业使用授权",
	)

	return []model.MembershipPlan{
		{ID: newID(), Code: "origin-free", Name: "基础版", Tier: "origin", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleFree, Currency: "CNY", ImageConcurrency: 4, VideoConcurrency: 2, BenefitsJSON: benefits("基础图片与视频生成", "个人项目与素材空间"), Enabled: true, SortOrder: 10},
		{ID: newID(), Code: "pro-month", Name: "标准版", Tier: "pro", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth, PriceCents: 19800, OriginalPriceCents: 30900, Currency: "CNY", CreditsPerPeriod: 19800 * CreditScale, ImageConcurrency: 6, VideoConcurrency: 4, TopupDiscountBasisPoints: 8000, BenefitsJSON: benefits("每月 19,800 积分", "图片并发 6 路", "视频并发 4 路", "积分充值 8 折"), Enabled: true, SortOrder: 20},
		{ID: newID(), Code: "pro-year", Name: "标准版", Tier: "pro", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 94900, OriginalPriceCents: 237600, Currency: "CNY", CreditsPerPeriod: 19800 * 12 * CreditScale, ImageConcurrency: 6, VideoConcurrency: 4, TopupDiscountBasisPoints: 8000, BenefitsJSON: benefits("每年 237,600 积分", "图片并发 6 路", "视频并发 4 路", "积分充值 8 折"), Enabled: true, SortOrder: 21},
		{ID: newID(), Code: "max-month", Name: "高级版", Tier: "max", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth, PriceCents: 64800, OriginalPriceCents: 101200, Currency: "CNY", CreditsPerPeriod: 64800 * CreditScale, ImageConcurrency: 8, VideoConcurrency: 6, TopupDiscountBasisPoints: 7500, BenefitsJSON: benefits("每月 64,800 积分", "图片并发 8 路", "视频并发 6 路", "积分充值 7.5 折"), Enabled: true, SortOrder: 30},
		{ID: newID(), Code: "max-year", Name: "高级版", Tier: "max", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 310900, OriginalPriceCents: 777600, Currency: "CNY", CreditsPerPeriod: 64800 * 12 * CreditScale, ImageConcurrency: 8, VideoConcurrency: 6, TopupDiscountBasisPoints: 7500, BenefitsJSON: benefits("每年 777,600 积分", "图片并发 8 路", "视频并发 6 路", "积分充值 7.5 折"), Enabled: true, SortOrder: 31},
		{ID: newID(), Code: "ultra-month", Name: "至尊版", Tier: "ultra", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleMonth, PriceCents: 131400, OriginalPriceCents: 205300, Currency: "CNY", CreditsPerPeriod: 131400 * CreditScale, ImageConcurrency: 12, VideoConcurrency: 8, TopupDiscountBasisPoints: 7000, BenefitsJSON: benefits("每月 131,400 积分", "图片并发 12 路", "视频并发 8 路", "积分充值 7 折"), Enabled: true, SortOrder: 40},
		{ID: newID(), Code: "ultra-year", Name: "至尊版", Tier: "ultra", Audience: model.MembershipAudiencePersonal, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 630900, OriginalPriceCents: 1576800, Currency: "CNY", CreditsPerPeriod: 131400 * 12 * CreditScale, ImageConcurrency: 12, VideoConcurrency: 8, TopupDiscountBasisPoints: 7000, BenefitsJSON: benefits("每年 1,576,800 积分", "图片并发 12 路", "视频并发 8 路", "积分充值 7 折"), Enabled: true, SortOrder: 41},
		{ID: newID(), Code: "team-pro-year", Name: "标准版", Tier: "pro", Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 149900, OriginalPriceCents: 237600, Currency: "CNY", CreditsPerPeriod: 19800 * 12 * CreditScale, ImageConcurrency: 6, VideoConcurrency: 4, UnlimitedTaskQueue: true, TeamStorageBytes: teamStorage130TB, SharedAssetsEnabled: true, ProjectPermissionsEnabled: true, InvoicingEnabled: true, CommercialUseEnabled: true, TopupDiscountBasisPoints: 9000, MinSeats: 2, MaxSeats: 200, BenefitsJSON: teamBenefits, Enabled: true, SortOrder: 50},
		{ID: newID(), Code: "team-max-year", Name: "高级版", Tier: "max", Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 449900, OriginalPriceCents: 777600, Currency: "CNY", CreditsPerPeriod: 64800 * 12 * CreditScale, ImageConcurrency: 8, VideoConcurrency: 6, UnlimitedTaskQueue: true, TeamStorageBytes: teamStorage130TB, SharedAssetsEnabled: true, ProjectPermissionsEnabled: true, InvoicingEnabled: true, CommercialUseEnabled: true, TopupDiscountBasisPoints: 8500, MinSeats: 2, MaxSeats: 200, BenefitsJSON: teamBenefits, Enabled: true, SortOrder: 60},
		{ID: newID(), Code: "team-ultra-year", Name: "至尊版", Tier: "ultra", Audience: model.MembershipAudienceTeam, BillingCycle: model.MembershipBillingCycleYear, PriceCents: 809900, OriginalPriceCents: 1576800, Currency: "CNY", CreditsPerPeriod: 131400 * 12 * CreditScale, ImageConcurrency: 12, VideoConcurrency: 8, UnlimitedTaskQueue: true, TeamStorageBytes: teamStorage130TB, SharedAssetsEnabled: true, ProjectPermissionsEnabled: true, InvoicingEnabled: true, CommercialUseEnabled: true, TopupDiscountBasisPoints: 8000, MinSeats: 2, MaxSeats: 200, BenefitsJSON: teamBenefits, Enabled: true, SortOrder: 70},
	}
}

func currentMembershipCatalogPlans(plans []model.MembershipPlan) []model.MembershipPlan {
	current := make([]model.MembershipPlan, 0, len(membershipCatalogCodes))
	for _, plan := range plans {
		if _, exists := membershipCatalogCodes[plan.Code]; exists {
			current = append(current, plan)
		}
	}
	return current
}
