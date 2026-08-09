import type { PaymentCheckout } from "@/services/api/payment";

import { checkoutSummary } from "./payment-checkout-domain";

const moneyFormatters = new Map<string, Intl.NumberFormat>();

const creditFormatter = new Intl.NumberFormat("zh-CN", {
    maximumFractionDigits: 2,
    minimumFractionDigits: 0,
});

function formatMoney(cents: number, currency: string): string {
    const normalizedCurrency = currency.trim().toUpperCase();
    if (!normalizedCurrency) throw new Error("收银台币种不能为空");
    let formatter = moneyFormatters.get(normalizedCurrency);
    if (!formatter) {
        formatter = new Intl.NumberFormat("zh-CN", {
            currency: normalizedCurrency,
            currencyDisplay: "narrowSymbol",
            maximumFractionDigits: 2,
            minimumFractionDigits: 0,
            style: "currency",
        });
        moneyFormatters.set(normalizedCurrency, formatter);
    }
    return formatter.format(cents / 100);
}

function formatCredits(microcredits: number): string {
    return creditFormatter.format(microcredits / 1_000_000);
}

type SummaryRowProps = {
    label: string;
    value: string;
    valueClassName?: string;
};

function SummaryRow({ label, value, valueClassName = "" }: SummaryRowProps) {
    return (
        <div className="membership-checkout-detail-row">
            <dt className="membership-checkout-detail-label">{label}</dt>
            <dd className={`membership-checkout-detail-value ${valueClassName}`}>{value}</dd>
        </div>
    );
}

type MembershipCheckoutSummaryProps = {
    checkout: PaymentCheckout;
};

export function MembershipCheckoutSummary({ checkout }: MembershipCheckoutSummaryProps) {
    const summary = checkoutSummary(checkout);
    const isMembership = summary.kind === "membership";
    const isTeam = isMembership && summary.audience === "team";
    const hasDiscount = isMembership && summary.discountCents > 0;
    const cycleLabel = isMembership ? (summary.billingCycle === "year" ? "按年购买" : "按月购买") : "一次性充值";
    const periodLabel = isMembership ? (summary.billingCycle === "year" ? "年" : "月") : "次";

    return (
        <section aria-labelledby="payment-checkout-title" className="membership-checkout-summary">
            <header className="membership-checkout-heading">
                <p className="membership-checkout-eyebrow">{isTeam ? "开通团队会员" : isMembership ? "开通创作会员" : "积分充值"}</p>
                <h1 className="membership-checkout-title" id="payment-checkout-title">
                    {summary.title}
                </h1>
                <p className="membership-checkout-order-number">订单 {checkout.orderNumber}</p>
            </header>

            <section aria-label="商品信息" className="membership-checkout-product">
                <div className="membership-checkout-product-copy">
                    <strong className="membership-checkout-product-title">{summary.title}</strong>
                    <span className="membership-checkout-product-meta">{cycleLabel}</span>
                </div>
                {isMembership ? (
                    <div className="membership-checkout-product-price">
                        <strong className="membership-checkout-unit-price">{formatMoney(summary.unitPriceCents, checkout.currency)}</strong>
                        <span className="membership-checkout-unit-suffix">
                            /{periodLabel}
                            {isTeam ? "/席位" : ""}
                        </span>
                    </div>
                ) : null}
            </section>

            <section aria-labelledby="membership-checkout-detail-title" className="membership-checkout-details">
                <h2 className="membership-checkout-section-title" id="membership-checkout-detail-title">
                    订单明细
                </h2>
                <dl className="membership-checkout-detail-list">
                    {isTeam ? <SummaryRow label="席位数量" value={`${summary.seats} 席位`} /> : null}
                    {isTeam ? <SummaryRow label="单席价格" value={`${formatMoney(summary.unitPriceCents, checkout.currency)}/${periodLabel}`} /> : null}
                    {isTeam ? <SummaryRow label="单席积分" value={`${formatCredits(summary.creditsPerPeriod)} 积分/${periodLabel}/席位`} /> : null}
                    {isTeam ? <SummaryRow label="团队积分合计" value={`${formatCredits(summary.totalCredits)} 积分/${periodLabel}`} /> : null}
                    {isMembership && !isTeam ? <SummaryRow label="周期积分" value={`${formatCredits(summary.totalCredits)} 积分/${periodLabel}`} /> : null}
                    {!isMembership ? <SummaryRow label="充值积分" value={`${formatCredits(summary.totalCredits)} 积分`} /> : null}
                    {hasDiscount ? <SummaryRow label="商品原价" value={formatMoney(summary.originalPriceCents, checkout.currency)} valueClassName="membership-checkout-original-price" /> : null}
                    {hasDiscount ? <SummaryRow label="优惠金额" value={`−${formatMoney(summary.discountCents, checkout.currency)}`} valueClassName="membership-checkout-discount" /> : null}
                    <SummaryRow label="应付金额" value={formatMoney(summary.actualPriceCents, checkout.currency)} valueClassName="membership-checkout-total-price" />
                </dl>
            </section>

            {isMembership ? <p className="membership-checkout-renewal-note">本次为一次性购买，到期不自动续费。</p> : null}
        </section>
    );
}
