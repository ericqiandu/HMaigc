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
        <li className="membership-storefront-feature flex items-center gap-2.5 text-[13px] leading-6 text-[#c9d2dd]">
            <svg aria-hidden="true" className="membership-storefront-feature-icon h-[13px] w-[13px] shrink-0 text-[#aeb8c5]" fill="none" viewBox="0 0 16 16">
                <path className="membership-storefront-feature-path" d="M3 8.5l3.2 3L13 4.5" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
            </svg>
            <span className="membership-storefront-feature-text">{text}</span>
        </li>
    );
}

function InfoDot() {
    return (
        <span aria-hidden="true" className="membership-storefront-info-dot ml-1 inline-flex h-[13px] w-[13px] items-center justify-center rounded-full border border-[#5c6675] text-[9px] text-[#8b95a5]">
            i
        </span>
    );
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

function cycleOfferLabel(allPlans: MembershipPlan[], audience: MembershipAudience, cycle: MembershipBillingCycle): string {
    const discountedPlans = allPlans
        .filter((plan) => plan.audience === audience && plan.billingCycle === cycle && plan.originalPriceCents > plan.priceCents)
        .sort((left, right) => left.priceCents / left.originalPriceCents - right.priceCents / right.originalPriceCents);
    return discountedPlans[0] ? (discountLabel(discountedPlans[0]) ?? "优惠订阅") : cycle === "year" ? "年度优惠" : "灵活订阅";
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
        <article
            className={`membership-storefront-plan-card relative flex flex-col rounded-xl border p-6 transition-colors ${featured ? "is-featured border-[#1f6f78]/70 bg-gradient-to-b from-[#10303a] via-[#0e2029] to-[#0c141d]" : "border-[#232c38] bg-[#0f151e] hover:border-[#323d4c]"}`}
        >
            {featured ? <div className="membership-storefront-plan-recommendation">热门推荐</div> : null}
            {discount ? <span className="membership-storefront-plan-discount absolute right-5 top-6 rounded bg-[#0f4b52] px-2 py-0.5 text-[11px] text-[#4fd6e0]">{discount}</span> : null}
            <h2 className="membership-storefront-plan-name pr-16 text-[16px] font-medium text-white">{publicPlanName(plan)}</h2>
            <div className="membership-storefront-plan-price mt-3 flex flex-wrap items-baseline gap-2">
                <span className="membership-storefront-plan-currency text-[15px] font-bold text-white">¥</span>
                <strong className="membership-storefront-plan-price-value text-[40px] font-bold leading-none tracking-tight text-white">{planPriceLabel(plan, seats)}</strong>
                <span className="membership-storefront-plan-price-unit text-[13px] text-[#8b95a5]">/月{plan.audience === "team" ? ` · ${seats} 席位` : ""}</span>
                {originalPrice ? <span className="membership-storefront-plan-original-price text-[13px] text-[#5f6a78] line-through">¥{originalPrice}</span> : null}
            </div>
            <p className="membership-storefront-plan-period mt-2.5 text-[12px] text-[#7d8794]">{planPeriodDescription(plan, seats)}</p>
            <div className="membership-storefront-plan-credit-value mt-1.5 flex items-center gap-2 text-[12px]">
                <span className="membership-storefront-plan-credit-rate text-[#8b95a5]">{planCreditValueLabel(plan, seats)}</span>
                <span className="membership-storefront-plan-saving text-[#e8b45a]">{planCycleSavingsLabel(plan, allPlans)}</span>
            </div>
            <div className="membership-storefront-plan-credits mt-5 flex items-center gap-2">
                <strong className="membership-storefront-plan-credits-value text-[26px] font-bold text-white">{formatCredits(monthlyCredits(plan) * seats)}</strong>
                <span className="membership-storefront-plan-credits-unit text-[13px] text-[#8b95a5]">积分/月</span>
            </div>
            <p className="membership-storefront-plan-estimate mt-3 flex items-center text-[12px] text-[#7d8794]">
                最多生成约 {highlight.images}｜{highlight.videos}
                <InfoDot />
            </p>

            {plan.audience === "team" ? (
                <label className="membership-storefront-seat-field mt-4 flex items-center justify-between gap-3 text-[12px] text-[#8b95a5]">
                    <span className="membership-storefront-seat-label">团队席位</span>
                    <input
                        aria-label={`${publicPlanName(plan)}团队席位`}
                        className="membership-storefront-seat-input h-9 w-24 rounded-md border border-[#2a3442] bg-[#121924] px-3 text-right text-white outline-none focus:border-[#4fd6e0]"
                        max={plan.maxSeats}
                        min={plan.minSeats}
                        onChange={(event) => onSeatsChange(plan, Number(event.target.value))}
                        type="number"
                        value={seats}
                    />
                </label>
            ) : null}

            <button
                className={`membership-storefront-plan-action mt-6 w-full rounded-lg py-3 text-[14px] font-medium transition-all ${plan.tier === "ultra" ? "bg-gradient-to-r from-[#7fe3f0] to-[#3fc4f5] text-[#0a1218] hover:opacity-90" : "bg-white text-[#10161e] hover:bg-[#e6ebf0]"}`}
                onClick={() => onPurchase(plan, seats)}
                type="button"
            >
                {planActionLabel(plan, currentPlanId)}
            </button>

            {presentation.activities.length ? (
                <section aria-label={`${publicPlanName(plan)}限时活动`} className="membership-storefront-plan-activities mt-7">
                    <h3 className="membership-storefront-plan-activities-title flex items-center gap-1.5 text-[13px] font-medium text-[#4fd6e0]">🎁 {presentation.copy.activityHeading}</h3>
                    <ul className="membership-storefront-plan-activities-list mt-3 space-y-2.5">
                        {presentation.activities.map((activity) => (
                            <li className="membership-storefront-plan-activity flex items-center text-[13px] text-[#c9d2dd]" key={`${activity.icon}-${activity.text}`}>
                                <span className="membership-storefront-plan-activity-icon mr-2 text-[#8b95a5]">{activity.icon}</span>
                                <span className="membership-storefront-plan-activity-text">{activity.text}</span>
                                <InfoDot />
                            </li>
                        ))}
                    </ul>
                </section>
            ) : null}

            <div aria-hidden="true" className="membership-storefront-plan-divider my-5 border-t border-[#202935]" />
            <ul className="membership-storefront-plan-features space-y-2.5">
                {planFeatures.map((feature) => (
                    <CheckItem key={feature} text={feature} />
                ))}
            </ul>

            <section aria-label={`${publicPlanName(plan)}独家功能`} className="membership-storefront-exclusive mt-5">
                <h3 className="membership-storefront-exclusive-title text-[13px] font-medium text-white">{presentation.copy.exclusiveHeading}</h3>
                <ul className="membership-storefront-exclusive-list mt-3 space-y-2.5">
                    {visibleExclusive.map((feature) => (
                        <li className="membership-storefront-exclusive-item flex items-center gap-2.5 text-[13px] text-[#c9d2dd]" key={feature}>
                            <svg aria-hidden="true" className="membership-storefront-exclusive-icon h-[13px] w-[13px] shrink-0 text-[#aeb8c5]" fill="none" viewBox="0 0 16 16">
                                <path className="membership-storefront-exclusive-path" d="M3 8.5l3.2 3L13 4.5" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="1.8" />
                            </svg>
                            <span className="membership-storefront-exclusive-text">{feature}</span>
                            <span className="membership-storefront-exclusive-new rounded-sm bg-gradient-to-r from-[#37b6c9] to-[#4f8fe8] px-1 py-px text-[9px] font-bold italic text-white">NEW</span>
                        </li>
                    ))}
                    {expanded ? extraExclusive.map((feature) => <CheckItem key={feature} text={feature} />) : null}
                </ul>
            </section>

            {extraExclusive.length ? (
                <button className="membership-storefront-plan-expand mt-5 text-[12px] text-[#8b95a5] hover:text-white" onClick={() => setExpanded((value) => !value)} type="button">
                    {expanded ? "收起权益" : "查看更多权益"} {expanded ? "⌃" : "⌄"}
                </button>
            ) : null}
        </article>
    );
}

export function MembershipStorefrontPricing(props: MembershipStorefrontPricingProps) {
    const { allPlans, audience, availableCycles, currentPlanId, cycle, onAudienceChange, onCycleChange, onOpenWallet, onPurchase, onSeatsChange, plans, presentation, teamSeats } = props;
    return (
        <section aria-label="会员套餐" className="membership-storefront-pricing mx-auto max-w-[1300px] px-6">
            <div className="membership-storefront-audience-tabs mt-10 flex justify-center gap-14 border-b border-[#1d2530]" role="tablist">
                {(
                    [
                        { key: "personal", label: presentation.copy.creatorTab },
                        { key: "team", label: presentation.copy.teamTab },
                    ] as const
                ).map((tab) => (
                    <button
                        aria-selected={audience === tab.key}
                        className={`membership-storefront-audience-tab relative min-h-11 pb-3.5 text-[16px] transition-colors ${audience === tab.key ? "is-active font-semibold text-white" : "text-[#7d8794] hover:text-[#aeb8c5]"}`}
                        key={tab.key}
                        onClick={() => onAudienceChange(tab.key)}
                        role="tab"
                        type="button"
                    >
                        {tab.label}
                        {audience === tab.key ? <span aria-hidden="true" className="membership-storefront-audience-indicator absolute inset-x-2 bottom-[-1px] h-[2px] bg-white" /> : null}
                    </button>
                ))}
            </div>

            <div className="membership-storefront-cycle-row mt-8 grid items-center gap-4">
                <div className="membership-storefront-cycle-switch mx-auto flex rounded-full border border-[#2a3442] bg-[#121924] p-1">
                    {availableCycles.map((availableCycle) => (
                        <button
                            className={`membership-storefront-cycle-option flex min-h-11 items-center gap-2 rounded-full px-7 py-2.5 text-[14px] transition-all max-sm:px-4 ${cycle === availableCycle ? "is-active bg-[#2c3646] font-medium text-white shadow" : "text-[#8b95a5] hover:text-white"}`}
                            key={availableCycle}
                            onClick={() => onCycleChange(availableCycle)}
                            type="button"
                        >
                            {availableCycle === "year"
                                ? audience === "team"
                                    ? billingCycleShortLabel.year
                                    : presentation.copy.yearCycle
                                : availableCycle === "month"
                                  ? audience === "team"
                                      ? billingCycleShortLabel.month
                                      : presentation.copy.monthCycle
                                  : billingCycleShortLabel[availableCycle]}
                            <span className={`membership-storefront-cycle-tag text-[11px] ${availableCycle === "year" ? "text-[#ff8a3c]" : "text-[#9aa5b3]"}`}>{cycleOfferLabel(allPlans, audience, availableCycle)}</span>
                        </button>
                    ))}
                </div>
                <button className="membership-storefront-wallet-action flex min-h-11 items-center gap-1.5 rounded-full border border-[#2f6f78] px-5 py-2 text-[13px] text-[#45c8d4] transition-colors hover:bg-[#12333a]" onClick={onOpenWallet} type="button">
                    {presentation.copy.creditStore}{" "}
                    <span aria-hidden="true" className="membership-storefront-wallet-arrow">
                        ›
                    </span>
                </button>
            </div>

            <div className="membership-storefront-plan-grid mt-8 grid gap-4">
                {plans.map((plan) => (
                    <StorefrontPlanCard allPlans={allPlans} currentPlanId={currentPlanId} key={plan.id} onPurchase={onPurchase} onSeatsChange={onSeatsChange} plan={plan} presentation={presentation} teamSeats={teamSeats} />
                ))}
            </div>

            <div className="membership-storefront-notes mt-10 text-[12px] leading-6 text-[#6b7684]">
                {presentation.membershipNotes.map((note, index) => (
                    <p className={`membership-storefront-note ${index > 0 ? "ml-4 font-medium text-[#8b95a5]" : ""}`} key={note}>
                        {index === 0 ? (
                            <span aria-hidden="true" className="membership-storefront-note-mark mr-1.5 text-[#4fd6e0]">
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
