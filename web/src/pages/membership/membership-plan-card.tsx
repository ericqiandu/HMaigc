import { Button } from "antd";
import { BadgePercent, Check, ImageIcon, Minus, Plus, Video, Zap } from "lucide-react";

import type { MembershipPlan } from "@/services/api/membership";

import { clampSeats, discountLabel, formatCredits, formatMoney, monthlyCredits, monthlyPriceCents, planTotalCredits, planTotalPriceCents, publicPlanName, topupDiscountLabel } from "./membership-formatters";

type MembershipPlanCardProps = {
    className?: string;
    currentPlanId?: string;
    featured: boolean;
    onPurchase: (plan: MembershipPlan, seats: number) => void;
    onSeatsChange: (planId: string, seats: number) => void;
    plan: MembershipPlan;
    seats: number;
};

export function MembershipPlanCard({ className = "", currentPlanId, featured, onPurchase, onSeatsChange, plan, seats }: MembershipPlanCardProps) {
    const teamPlan = plan.audience === "team";
    const normalizedSeats = clampSeats(plan, seats);
    const current = currentPlanId === plan.id;
    const discount = discountLabel(plan);
    const totalPrice = planTotalPriceCents(plan, normalizedSeats);
    const totalCredits = planTotalCredits(plan, normalizedSeats);
    const paidPlan = plan.billingCycle !== "free";
    const displayName = publicPlanName(plan);

    return (
        <article className={`membership-plan-card ${teamPlan ? "is-team" : ""} ${featured ? "is-featured" : ""} ${current ? "is-current" : ""} ${className}`}>
            {featured ? <span className="membership-plan-banner">热门推荐</span> : null}
            <div className="membership-plan-heading">
                <div className="membership-plan-name-row">
                    <h2 className="membership-plan-name">{displayName}</h2>
                    {discount ? <span className="membership-plan-discount">{discount}</span> : null}
                </div>
                {current ? <span className="membership-plan-current-badge">当前方案</span> : null}
            </div>

            <div className="membership-plan-price-block">
                <div className="membership-plan-price-row">
                    <span className="membership-plan-currency">¥</span>
                    <strong className="membership-plan-price">{formatMoney(teamPlan ? totalPrice : plan.priceCents)}</strong>
                    <span className="membership-plan-price-unit">{teamPlan ? `/${normalizedSeats} 席位` : plan.billingCycle === "year" ? "/年" : plan.billingCycle === "month" ? "/月" : ""}</span>
                </div>
                <div className="membership-plan-price-meta">
                    {plan.originalPriceCents > plan.priceCents ? (
                        <span className="membership-plan-original">原价 ¥{formatMoney(plan.originalPriceCents * (teamPlan ? normalizedSeats : 1))}</span>
                    ) : (
                        <span className="membership-plan-original-placeholder">后台实时价格</span>
                    )}
                    {paidPlan ? <span className="membership-plan-average">月均 ¥{formatMoney(monthlyPriceCents(plan) * (teamPlan ? normalizedSeats : 1))}</span> : null}
                </div>
            </div>

            {teamPlan ? (
                <div className="membership-seat-control">
                    <div className="membership-seat-copy">
                        <span className="membership-seat-label">团队席位</span>
                        <small className="membership-seat-range">
                            {plan.minSeats}–{plan.maxSeats} 人
                        </small>
                    </div>
                    <div className="membership-seat-stepper">
                        <button aria-label={`减少 ${displayName} 席位`} className="membership-seat-button" disabled={normalizedSeats <= plan.minSeats} onClick={() => onSeatsChange(plan.id, normalizedSeats - 1)} type="button">
                            <Minus className="membership-seat-button-icon" />
                        </button>
                        <strong className="membership-seat-value">{normalizedSeats}</strong>
                        <button aria-label={`增加 ${displayName} 席位`} className="membership-seat-button" disabled={normalizedSeats >= plan.maxSeats} onClick={() => onSeatsChange(plan.id, normalizedSeats + 1)} type="button">
                            <Plus className="membership-seat-button-icon" />
                        </button>
                    </div>
                </div>
            ) : null}

            <div className="membership-plan-credit">
                <span className="membership-plan-credit-label">{teamPlan ? "团队周期总积分" : "周期积分"}</span>
                <strong className="membership-plan-credit-value">{formatCredits(teamPlan ? totalCredits : plan.creditsPerPeriod)}</strong>
                {plan.billingCycle === "year" ? <small className="membership-plan-credit-monthly">月均 {formatCredits(monthlyCredits(plan) * (teamPlan ? normalizedSeats : 1))} 积分</small> : null}
            </div>

            <div className="membership-plan-core-benefits">
                <span className="membership-plan-core-benefit">
                    <ImageIcon className="membership-plan-benefit-icon" />
                    <strong className="membership-plan-benefit-value">{plan.imageConcurrency}</strong>
                    <small className="membership-plan-benefit-label">图片并发</small>
                </span>
                <span className="membership-plan-core-benefit">
                    <Video className="membership-plan-benefit-icon" />
                    <strong className="membership-plan-benefit-value">{plan.videoConcurrency}</strong>
                    <small className="membership-plan-benefit-label">视频并发</small>
                </span>
                <span className="membership-plan-core-benefit">
                    <BadgePercent className="membership-plan-benefit-icon" />
                    <strong className="membership-plan-benefit-value">{topupDiscountLabel(plan.topupDiscountBasisPoints)}</strong>
                    <small className="membership-plan-benefit-label">充值折扣</small>
                </span>
            </div>

            <Button className="membership-plan-action" disabled={!paidPlan} onClick={() => onPurchase(plan, normalizedSeats)} type={featured ? "primary" : "default"}>
                {!paidPlan ? "基础方案" : current ? "续费当前方案" : teamPlan ? "开通团队会员" : "立即开通"}
            </Button>

            <div className="membership-plan-benefits">
                <h3 className="membership-plan-benefits-title">
                    <Zap className="membership-plan-benefits-title-icon" />
                    套餐权益
                </h3>
                <ul className="membership-plan-benefit-list">
                    {plan.benefits.map((benefit) => (
                        <li className="membership-plan-benefit-item" key={benefit}>
                            <Check className="membership-plan-check-icon" />
                            <span className="membership-plan-benefit-text">{benefit}</span>
                        </li>
                    ))}
                </ul>
            </div>
        </article>
    );
}
