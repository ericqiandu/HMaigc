import { Check, ChevronDown, ChevronRight, Gift, Info } from "lucide-react";
import { useState } from "react";

import type { MembershipAudience, MembershipBillingCycle, MembershipPlan, MembershipStorefrontSetting } from "@/services/api/membership";

import { billingCycleShortLabel, clampSeats, discountLabel, formatCredits, monthlyCredits, publicPlanName } from "./membership-formatters";
import { planCreditValueLabel, planCycleSavingsLabel, planOriginalMonthlyPriceLabel, planPeriodDescription, planPriceLabel, planStorageLabel } from "./membership-storefront-domain";

type MembershipStorefrontPricingProps = {
    allPlans: MembershipPlan[];
    audience: MembershipAudience;
    availableCycles: MembershipBillingCycle[];
    currentPlanId?: string;
    cycle: MembershipBillingCycle;
    onAudienceChange: (audience: MembershipAudience) => void;
    onCycleChange: (cycle: MembershipBillingCycle) => void;
    onOpenWallet: () => void;
    onPurchase: (plan: MembershipPlan, seats: number) => void;
    onSeatsChange: (plan: MembershipPlan, seats: number) => void;
    plans: MembershipPlan[];
    presentation: MembershipStorefrontSetting;
    teamSeats: Record<string, number>;
};

function CheckItem({ text }: { text: string }) {
    return (
        <li className="membership-storefront-feature">
            <Check aria-hidden="true" className="membership-storefront-feature-icon" />
            <span className="membership-storefront-feature-text">{text}</span>
        </li>
    );
}

function InfoDot() {
    return <Info aria-hidden="true" className="membership-storefront-info-dot" />;
}

function uniqueFeatures(values: string[]): string[] {
    return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

function requirePlanHighlight(presentation: MembershipStorefrontSetting, tier: string) {
    const highlight = presentation.planHighlights.find((item) => item.tier === tier);
    if (!highlight) throw new Error(`会员商城缺少套餐层级 ${tier} 的生成能力摘要`);
    return highlight;
}

function planActionLabel(plan: MembershipPlan, currentPlanId?: string): string {
    if (plan.id === currentPlanId) return "续费当前方案";
    if (plan.priceCents <= 0) return "使用免费方案";
    return plan.tier === "ultra" ? "立即升级至尊版" : "选择此方案";
}

type StorefrontPlanCardProps = Pick<MembershipStorefrontPricingProps, "allPlans" | "currentPlanId" | "onPurchase" | "onSeatsChange" | "presentation" | "teamSeats"> & {
    plan: MembershipPlan;
};

function StorefrontPlanCard({ allPlans, currentPlanId, onPurchase, onSeatsChange, plan, presentation, teamSeats }: StorefrontPlanCardProps) {
    const seats = clampSeats(plan, teamSeats[plan.id] ?? plan.minSeats);
    const featured = plan.tier === "ultra";
    const highlight = requirePlanHighlight(presentation, plan.tier);
    const originalPrice = planOriginalMonthlyPriceLabel(plan, seats);
    const discount = discountLabel(plan);
    const planFeatures = uniqueFeatures([`${plan.imageConcurrency} 个图片并发 · ${plan.videoConcurrency} 个视频并发`, planStorageLabel(plan), ...plan.benefits, ...presentation.commonFeatures]);
    const visibleExclusive = presentation.exclusiveFeatures.slice(0, 2);
    const extraExclusive = presentation.exclusiveFeatures.slice(2);
    const [expanded, setExpanded] = useState(false);

    return (
        <article className={`membership-storefront-plan-card${featured ? " is-featured" : ""}`}>
            {featured ? <div className="membership-storefront-plan-recommendation">热门推荐</div> : null}
            <div className="membership-storefront-plan-heading">
                <h2 className="membership-storefront-plan-name">{publicPlanName(plan)}</h2>
                {discount ? <span className="membership-storefront-plan-discount">{discount}</span> : null}
            </div>
            <div className="membership-storefront-plan-price">
                <span className="membership-storefront-plan-currency">¥</span>
                <strong className="membership-storefront-plan-price-value">{planPriceLabel(plan, seats)}</strong>
                <span className="membership-storefront-plan-price-unit">/月{plan.audience === "team" ? ` · ${seats} 席位` : ""}</span>
                {originalPrice ? <span className="membership-storefront-plan-original-price">¥{originalPrice}</span> : null}
            </div>
            <p className="membership-storefront-plan-period">{planPeriodDescription(plan, seats)}</p>
            <div className="membership-storefront-plan-credit-value">
                <span className="membership-storefront-plan-credit-rate">{planCreditValueLabel(plan, seats)}</span>
                <span className="membership-storefront-plan-saving">{planCycleSavingsLabel(plan, allPlans)}</span>
            </div>
            <div className="membership-storefront-plan-credits">
                <strong className="membership-storefront-plan-credits-value">{formatCredits(monthlyCredits(plan) * seats)}</strong>
                <span className="membership-storefront-plan-credits-unit">积分/月</span>
            </div>
            <p className="membership-storefront-plan-estimate">
                最多生成约 {highlight.images}｜{highlight.videos}
                <InfoDot />
            </p>

            {plan.audience === "team" ? (
                <label className="membership-storefront-seat-field">
                    <span className="membership-storefront-seat-label">团队席位</span>
                    <input
                        aria-label={`${publicPlanName(plan)}团队席位`}
                        className="membership-storefront-seat-input"
                        max={plan.maxSeats}
                        min={plan.minSeats}
                        onChange={(event) => onSeatsChange(plan, Number(event.target.value))}
                        type="number"
                        value={seats}
                    />
                </label>
            ) : null}

            <button className="membership-storefront-plan-action" onClick={() => onPurchase(plan, seats)} type="button">
                {planActionLabel(plan, currentPlanId)}
            </button>

            {presentation.activities.length ? (
                <section aria-label={`${publicPlanName(plan)}限时活动`} className="membership-storefront-plan-activities">
                    <h3 className="membership-storefront-plan-activities-title">
                        <Gift aria-hidden="true" className="membership-storefront-plan-activities-icon" />
                        {presentation.copy.activityHeading}
                    </h3>
                    <ul className="membership-storefront-plan-activities-list">
                        {presentation.activities.map((activity) => (
                            <li className="membership-storefront-plan-activity" key={`${activity.icon}-${activity.text}`}>
                                <span className="membership-storefront-plan-activity-icon">{activity.icon}</span>
                                <span className="membership-storefront-plan-activity-text">{activity.text}</span>
                                <InfoDot />
                            </li>
                        ))}
                    </ul>
                </section>
            ) : null}

            <div aria-hidden="true" className="membership-storefront-plan-divider" />
            <ul className="membership-storefront-plan-features">
                {planFeatures.map((feature) => (
                    <CheckItem key={feature} text={feature} />
                ))}
            </ul>

            <section aria-label={`${publicPlanName(plan)}独家功能`} className="membership-storefront-exclusive">
                <h3 className="membership-storefront-exclusive-title">{presentation.copy.exclusiveHeading}</h3>
                <ul className="membership-storefront-exclusive-list">
                    {visibleExclusive.map((feature) => (
                        <li className="membership-storefront-exclusive-item" key={feature}>
                            <Check aria-hidden="true" className="membership-storefront-exclusive-icon" />
                            <span className="membership-storefront-exclusive-text">{feature}</span>
                            <span className="membership-storefront-exclusive-new">NEW</span>
                        </li>
                    ))}
                    {expanded ? extraExclusive.map((feature) => <CheckItem key={feature} text={feature} />) : null}
                </ul>
            </section>

            {extraExclusive.length ? (
                <button className="membership-storefront-plan-expand" onClick={() => setExpanded((value) => !value)} type="button">
                    {expanded ? "收起权益" : "查看更多权益"}
                    <ChevronDown aria-hidden="true" className={`membership-storefront-plan-expand-icon${expanded ? " is-expanded" : ""}`} />
                </button>
            ) : null}
        </article>
    );
}

export function MembershipStorefrontPricing(props: MembershipStorefrontPricingProps) {
    const { allPlans, audience, availableCycles, currentPlanId, cycle, onAudienceChange, onCycleChange, onOpenWallet, onPurchase, onSeatsChange, plans, presentation, teamSeats } = props;
    return (
        <section aria-label="会员套餐" className="membership-storefront-pricing">
            <div className="membership-storefront-audience-tabs" role="tablist">
                {(
                    [
                        { key: "personal", label: presentation.copy.creatorTab },
                        { key: "team", label: presentation.copy.teamTab },
                    ] as const
                ).map((tab) => (
                    <button aria-selected={audience === tab.key} className={`membership-storefront-audience-tab${audience === tab.key ? " is-active" : ""}`} key={tab.key} onClick={() => onAudienceChange(tab.key)} role="tab" type="button">
                        {tab.label}
                        {audience === tab.key ? <span aria-hidden="true" className="membership-storefront-audience-indicator" /> : null}
                    </button>
                ))}
            </div>

            <div className="membership-storefront-cycle-row">
                <div className="membership-storefront-cycle-switch">
                    {availableCycles.map((availableCycle) => (
                        <button className={`membership-storefront-cycle-option${cycle === availableCycle ? " is-active" : ""}`} key={availableCycle} onClick={() => onCycleChange(availableCycle)} type="button">
                            {availableCycle === "year"
                                ? audience === "team"
                                    ? billingCycleShortLabel.year
                                    : presentation.copy.yearCycle
                                : availableCycle === "month"
                                  ? audience === "team"
                                      ? billingCycleShortLabel.month
                                      : presentation.copy.monthCycle
                                  : billingCycleShortLabel[availableCycle]}
                            <span className={`membership-storefront-cycle-tag${availableCycle === "year" ? " is-discount" : ""}`}>{availableCycle === "year" ? "年度优惠" : "灵活订阅"}</span>
                        </button>
                    ))}
                </div>
                <button className="membership-storefront-wallet-action" onClick={onOpenWallet} type="button">
                    {presentation.copy.creditStore}
                    <ChevronRight aria-hidden="true" className="membership-storefront-wallet-arrow" />
                </button>
            </div>

            <div className="membership-storefront-plan-grid">
                {plans.map((plan) => (
                    <StorefrontPlanCard allPlans={allPlans} currentPlanId={currentPlanId} key={plan.id} onPurchase={onPurchase} onSeatsChange={onSeatsChange} plan={plan} presentation={presentation} teamSeats={teamSeats} />
                ))}
            </div>

            <div className="membership-storefront-notes">
                {presentation.membershipNotes.map((note, index) => (
                    <p className={`membership-storefront-note${index > 0 ? " is-secondary" : ""}`} key={note}>
                        {index === 0 ? (
                            <span aria-hidden="true" className="membership-storefront-note-mark">
                                ✦
                            </span>
                        ) : null}
                        {note}
                    </p>
                ))}
            </div>
        </section>
    );
}
