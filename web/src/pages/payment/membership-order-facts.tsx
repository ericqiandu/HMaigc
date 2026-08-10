import type { ReactElement } from "react";

import { membershipBillingCycleLabel, type MembershipOrderFactsModel } from "./membership-order-facts-domain";
import { formatPaymentOrderCredits, formatPaymentOrderMoney } from "./payment-order-formatters";

type OrderFactsRowProps = {
    label: string;
    value: string;
    valueClassName?: string;
};

function OrderFactsRow({ label, value, valueClassName = "" }: OrderFactsRowProps): ReactElement {
    return (
        <div className="membership-checkout-detail-row">
            <dt className="membership-checkout-detail-label">{label}</dt>
            <dd className={`membership-checkout-detail-value ${valueClassName}`}>{value}</dd>
        </div>
    );
}

export function MembershipOrderFacts({ facts }: { facts: MembershipOrderFactsModel }): ReactElement {
    const isTeam = facts.audience === "team";
    const periodLabel = facts.billingCycle === "year" ? "年" : "月";
    const hasDiscount = facts.originalTotalPriceCents > facts.totalPriceCents;
    const title = `${isTeam ? "开通团队会员" : "开通创作会员"}「${facts.title} ${membershipBillingCycleLabel(facts)}」 ${formatPaymentOrderCredits(facts.totalCredits)} 积分`;

    return (
        <section aria-labelledby="payment-checkout-title" className="membership-order-facts membership-checkout-summary">
            <header className="membership-order-facts-heading membership-checkout-heading">
                <h1 className="membership-order-facts-title membership-checkout-title" id="payment-checkout-title">
                    {title}
                </h1>
                {facts.orderNumber ? <p className="membership-order-facts-order-number membership-checkout-order-number">订单 {facts.orderNumber}</p> : null}
            </header>
            <section aria-label="商品信息" className="membership-order-facts-product-section membership-checkout-product">
                <h2 className="membership-checkout-section-title">商品信息</h2>
                <div className="membership-checkout-product-selection">
                    <div className="membership-checkout-product-copy">
                        <strong className="membership-order-facts-product-title membership-checkout-product-title">{facts.title}</strong>
                        <span className="membership-checkout-product-meta">{membershipBillingCycleLabel(facts)}</span>
                    </div>
                    <div className="membership-checkout-product-price">
                        <strong className="membership-checkout-unit-price">{formatPaymentOrderMoney(facts.unitPriceCents, facts.currency)}</strong>
                        <span className="membership-checkout-unit-suffix">
                            /{periodLabel}
                            {isTeam ? "/席位" : ""}
                        </span>
                        {hasDiscount ? (
                            <span className="membership-order-facts-original-unit-price membership-checkout-product-meta">
                                会员原价 <del className="membership-checkout-product-original-price">{formatPaymentOrderMoney(facts.originalUnitPriceCents, facts.currency)}</del>
                            </span>
                        ) : null}
                        {facts.billingCycle === "year" ? <span className="membership-order-facts-monthly-equivalent membership-checkout-product-meta">每月约 {formatPaymentOrderMoney(facts.unitPriceCents / 12, facts.currency)}/月</span> : null}
                    </div>
                </div>
            </section>
            <section aria-labelledby="membership-order-facts-detail-title" className="membership-order-facts-details membership-checkout-details">
                <h2 className="membership-checkout-section-title" id="membership-order-facts-detail-title">
                    订单明细
                </h2>
                <dl className="membership-checkout-detail-list">
                    {isTeam ? <OrderFactsRow label="席位数量" value={`${facts.seats} 席位`} /> : null}
                    {isTeam ? <OrderFactsRow label="单席价格" value={`${formatPaymentOrderMoney(facts.unitPriceCents, facts.currency)}/${periodLabel}`} /> : null}
                    {isTeam ? <OrderFactsRow label="单席积分" value={`${formatPaymentOrderCredits(facts.creditsPerPeriod)} 积分/${periodLabel}/席位`} /> : null}
                    <OrderFactsRow label={isTeam ? "团队积分合计" : "周期积分"} value={`${formatPaymentOrderCredits(facts.totalCredits)} 积分/${periodLabel}`} />
                    <OrderFactsRow label="续费方式" value="到期不自动续费" />
                    {hasDiscount ? <OrderFactsRow label="商品原价" value={formatPaymentOrderMoney(facts.originalTotalPriceCents, facts.currency)} valueClassName="membership-checkout-original-price" /> : null}
                    {hasDiscount ? <OrderFactsRow label="优惠金额" value={`−${formatPaymentOrderMoney(facts.originalTotalPriceCents - facts.totalPriceCents, facts.currency)}`} valueClassName="membership-checkout-discount" /> : null}
                    <OrderFactsRow label="应付金额" value={formatPaymentOrderMoney(facts.totalPriceCents, facts.currency)} valueClassName="membership-checkout-total-price" />
                </dl>
            </section>
            <p className="membership-order-facts-renewal-note membership-checkout-renewal-note">本次为一次性购买，到期不自动续费。</p>
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
