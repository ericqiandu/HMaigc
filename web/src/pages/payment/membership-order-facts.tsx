import { Minus, Plus } from "lucide-react";
import type { ReactElement } from "react";

import type { MembershipPlan } from "@/services/api/membership";

import { membershipBillingCycleLabel, membershipOrderFactsFromPlan, type MembershipOrderFactsModel } from "./membership-order-facts-domain";
import { formatPaymentOrderCredits, formatPaymentOrderMoney } from "./payment-order-formatters";

type OrderFactsRowProps = {
    label: string;
    value: string;
    valueClassName?: string;
    rowClassName?: string;
};

function OrderFactsRow({ label, rowClassName = "", value, valueClassName = "" }: OrderFactsRowProps): ReactElement {
    return (
        <div className={`membership-checkout-detail-row ${rowClassName}`}>
            <dt className="membership-checkout-detail-label">{label}</dt>
            <dd className={`membership-checkout-detail-value ${valueClassName}`}>{value}</dd>
        </div>
    );
}

type TeamSeatControl = {
    disabled: boolean;
    maxSeats: number;
    minSeats: number;
    onChange?: (seats: number) => void;
};

type MembershipOrderFactsProps = {
    facts: MembershipOrderFactsModel;
    onTeamPlanChange?: (planID: string) => void;
    selectedTeamPlanID?: string;
    teamPlanOptions?: MembershipPlan[];
    teamSeatControl?: TeamSeatControl;
};

function teamCycleLabel(cycle: MembershipOrderFactsModel["billingCycle"]): string {
    return cycle === "year" ? "12个月" : "1个月";
}

function TeamPlanOption({ disabled, facts, onClick, selected }: { disabled: boolean; facts: MembershipOrderFactsModel; onClick?: () => void; selected: boolean }): ReactElement {
    const hasDiscount = facts.originalUnitPriceCents > facts.unitPriceCents;
    return (
        <button aria-pressed={selected} className={`membership-team-plan-option ${selected ? "is-selected" : ""} ${disabled ? "is-locked" : ""}`} disabled={disabled} onClick={onClick} type="button">
            <span className="membership-team-plan-cycle">{teamCycleLabel(facts.billingCycle)}</span>
            <span className="membership-team-plan-price-line">
                <strong className="membership-team-plan-price">{formatPaymentOrderMoney(facts.unitPriceCents, facts.currency)}</strong>
                <span className="membership-team-plan-price-suffix">/席位</span>
                {hasDiscount ? <del className="membership-team-plan-original-price">{formatPaymentOrderMoney(facts.originalUnitPriceCents, facts.currency)}</del> : null}
            </span>
            {facts.billingCycle === "year" ? <span className="membership-team-plan-monthly">{formatPaymentOrderMoney(facts.unitPriceCents / 12, facts.currency)}/席位/月</span> : null}
        </button>
    );
}

function TeamPurchaseConfiguration({ facts, onTeamPlanChange, selectedTeamPlanID, teamPlanOptions, teamSeatControl }: Omit<MembershipOrderFactsProps, "facts"> & { facts: MembershipOrderFactsModel }): ReactElement {
    const planOptions = teamPlanOptions?.length ? teamPlanOptions : null;
    const selectedPlanID = selectedTeamPlanID ?? "frozen-team-plan";
    const lockedPlans = !onTeamPlanChange;
    const minSeats = teamSeatControl?.minSeats;
    const maxSeats = teamSeatControl?.maxSeats;
    const seatsLocked = teamSeatControl?.disabled ?? true;

    return (
        <section aria-label="团队套餐与席位" className="membership-team-purchase-configuration">
            <h2 className="membership-checkout-section-title">选择商品</h2>
            <div className="membership-team-plan-options">
                {planOptions ? (
                    planOptions.map((plan) => {
                        const selected = plan.id === selectedPlanID;
                        const optionFacts = selected ? facts : membershipOrderFactsFromPlan(plan, facts.seats);
                        return <TeamPlanOption disabled={lockedPlans} facts={optionFacts} key={plan.id} onClick={lockedPlans ? undefined : () => onTeamPlanChange?.(plan.id)} selected={selected} />;
                    })
                ) : (
                    <TeamPlanOption disabled facts={facts} selected />
                )}
            </div>
            <div className="membership-team-seat-section">
                <div className="membership-team-seat-heading">
                    <span className="membership-team-seat-label">席位数量</span>
                    {minSeats !== undefined && maxSeats !== undefined ? (
                        <span className="membership-team-seat-bounds">
                            （支持{minSeats}–{maxSeats}人）
                        </span>
                    ) : null}
                </div>
                <div aria-label="席位数量" className={`membership-team-seat-stepper ${seatsLocked ? "is-locked" : ""}`} role="group">
                    <button
                        aria-label="减少席位"
                        className="membership-team-seat-action membership-team-seat-decrease"
                        disabled={seatsLocked || minSeats === undefined || facts.seats <= minSeats}
                        onClick={() => teamSeatControl?.onChange?.(facts.seats - 1)}
                        type="button"
                    >
                        <Minus aria-hidden="true" className="membership-team-seat-icon" />
                    </button>
                    <output className="membership-team-seat-value">{facts.seats} 席位</output>
                    <button
                        aria-label="增加席位"
                        className="membership-team-seat-action membership-team-seat-increase"
                        disabled={seatsLocked || maxSeats === undefined || facts.seats >= maxSeats}
                        onClick={() => teamSeatControl?.onChange?.(facts.seats + 1)}
                        type="button"
                    >
                        <Plus aria-hidden="true" className="membership-team-seat-icon" />
                    </button>
                </div>
            </div>
        </section>
    );
}

export function MembershipOrderFacts({ facts, onTeamPlanChange, selectedTeamPlanID, teamPlanOptions, teamSeatControl }: MembershipOrderFactsProps): ReactElement {
    const isTeam = facts.audience === "team";
    const periodLabel = facts.billingCycle === "year" ? "年" : "月";
    const hasDiscount = facts.originalTotalPriceCents > facts.totalPriceCents;
    const title = isTeam ? `开通团队版会员「${facts.title}」` : `开通创作会员「${facts.title} ${membershipBillingCycleLabel(facts)}」 ${formatPaymentOrderCredits(facts.totalCredits)} 积分`;

    return (
        <section aria-labelledby="payment-checkout-title" className={`membership-order-facts membership-checkout-summary ${isTeam ? "is-team" : "is-personal"}`}>
            <header className="membership-order-facts-heading membership-checkout-heading">
                <h1 className="membership-order-facts-title membership-checkout-title" id="payment-checkout-title">
                    {title}
                </h1>
                {facts.orderNumber ? <p className="membership-order-facts-order-number membership-checkout-order-number">订单 {facts.orderNumber}</p> : null}
            </header>
            {isTeam ? (
                <TeamPurchaseConfiguration facts={facts} onTeamPlanChange={onTeamPlanChange} selectedTeamPlanID={selectedTeamPlanID} teamPlanOptions={teamPlanOptions} teamSeatControl={teamSeatControl} />
            ) : (
                <section aria-label="商品信息" className="membership-order-facts-product-section membership-checkout-product">
                    <h2 className="membership-checkout-section-title">商品信息</h2>
                    <div className="membership-checkout-product-selection">
                        <div className="membership-checkout-product-copy">
                            <strong className="membership-order-facts-product-title membership-checkout-product-title">{facts.title}</strong>
                            <span className="membership-checkout-product-meta">{membershipBillingCycleLabel(facts)}</span>
                        </div>
                        <div className="membership-checkout-product-price">
                            <strong className="membership-checkout-unit-price">{formatPaymentOrderMoney(facts.unitPriceCents, facts.currency)}</strong>
                            <span className="membership-checkout-unit-suffix">/{periodLabel}</span>
                            {hasDiscount ? (
                                <span className="membership-order-facts-original-unit-price membership-checkout-product-meta">
                                    会员原价 <del className="membership-checkout-product-original-price">{formatPaymentOrderMoney(facts.originalUnitPriceCents, facts.currency)}</del>
                                </span>
                            ) : null}
                            {facts.billingCycle === "year" ? <span className="membership-order-facts-monthly-equivalent membership-checkout-product-meta">每月约 {formatPaymentOrderMoney(facts.unitPriceCents / 12, facts.currency)}/月</span> : null}
                        </div>
                    </div>
                </section>
            )}
            <section aria-labelledby="membership-order-facts-detail-title" className="membership-order-facts-details membership-checkout-details">
                <h2 className="membership-checkout-section-title" id="membership-order-facts-detail-title">
                    订单明细
                </h2>
                <dl className="membership-checkout-detail-list">
                    {isTeam ? <OrderFactsRow label="权益生效" value="支付成功后按现有会员顺延" /> : null}
                    {isTeam ? <OrderFactsRow label="席位数量" value={`${facts.seats} 席位`} /> : null}
                    {isTeam ? <OrderFactsRow label="单席积分" value={`${formatPaymentOrderCredits(facts.creditsPerPeriod)} 积分/${periodLabel}/席位`} /> : null}
                    <OrderFactsRow label={isTeam ? "团队总积分" : "周期积分"} value={`${formatPaymentOrderCredits(facts.totalCredits)} 积分/${periodLabel}`} />
                    {!isTeam ? <OrderFactsRow label="续费方式" value="到期不自动续费" /> : null}
                    {hasDiscount ? (
                        <OrderFactsRow label="商品原价" rowClassName={isTeam ? "is-financial-start" : ""} value={formatPaymentOrderMoney(facts.originalTotalPriceCents, facts.currency)} valueClassName="membership-checkout-original-price" />
                    ) : null}
                    {hasDiscount ? <OrderFactsRow label="优惠金额" value={`−${formatPaymentOrderMoney(facts.originalTotalPriceCents - facts.totalPriceCents, facts.currency)}`} valueClassName="membership-checkout-discount" /> : null}
                    <OrderFactsRow label="应付金额" value={formatPaymentOrderMoney(facts.totalPriceCents, facts.currency)} valueClassName="membership-checkout-total-price" />
                </dl>
            </section>
            {!isTeam ? <p className="membership-order-facts-renewal-note membership-checkout-renewal-note">本次为一次性购买，到期不自动续费。</p> : null}
        </section>
    );
}

export function MembershipOrderFactsSkeleton(): ReactElement {
    return (
        <section aria-busy="true" aria-label="正在加载订单" className="membership-order-facts membership-checkout-summary payment-checkout-skeleton" role="status">
            <header className="membership-order-facts-heading membership-checkout-heading">
                <h1 className="membership-order-facts-title membership-checkout-title">正在加载订单</h1>
            </header>
            <section aria-label="商品信息" className="membership-order-facts-product-section membership-checkout-product">
                <h2 className="membership-checkout-section-title">商品信息</h2>
                <span className="payment-checkout-skeleton-line" />
            </section>
            <section aria-labelledby="membership-order-facts-skeleton-detail-title" className="membership-order-facts-details membership-checkout-details">
                <h2 className="membership-checkout-section-title" id="membership-order-facts-skeleton-detail-title">
                    订单明细
                </h2>
                <span className="payment-checkout-skeleton-block" />
            </section>
            <p className="membership-order-facts-renewal-note membership-checkout-renewal-note">本次为一次性购买，到期不自动续费。</p>
        </section>
    );
}
