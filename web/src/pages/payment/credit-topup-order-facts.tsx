import type { ReactElement } from "react";

import type { PaymentCheckout } from "@/services/api/payment";

import { checkoutSummary } from "./payment-checkout-domain";

const creditFormatter = new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 2, minimumFractionDigits: 0 });
const moneyFormatters = new Map<string, Intl.NumberFormat>();

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

export function CreditTopupOrderFacts({ checkout }: { checkout: PaymentCheckout }): ReactElement {
    const summary = checkoutSummary(checkout);
    if (summary.kind !== "credit_topup") throw new Error("会员订单不能使用积分充值订单事实展示");

    return (
        <section aria-labelledby="payment-checkout-title" className="credit-topup-order-facts membership-checkout-summary">
            <header className="membership-checkout-heading">
                <h1 className="membership-checkout-title" id="payment-checkout-title">
                    积分充值
                </h1>
                <p className="membership-checkout-order-number">订单 {checkout.orderNumber}</p>
            </header>
            <section aria-label="商品信息" className="membership-checkout-product">
                <h2 className="membership-checkout-section-title">商品信息</h2>
                <div className="membership-checkout-product-selection">
                    <div className="membership-checkout-product-copy">
                        <strong className="membership-checkout-product-title">积分充值</strong>
                        <span className="membership-checkout-product-meta">一次性充值</span>
                    </div>
                </div>
            </section>
            <section aria-labelledby="credit-topup-order-facts-detail-title" className="membership-checkout-details">
                <h2 className="membership-checkout-section-title" id="credit-topup-order-facts-detail-title">
                    订单明细
                </h2>
                <dl className="membership-checkout-detail-list">
                    <div className="membership-checkout-detail-row">
                        <dt className="membership-checkout-detail-label">充值积分</dt>
                        <dd className="membership-checkout-detail-value">{creditFormatter.format(summary.totalCredits / 1_000_000)} 积分</dd>
                    </div>
                    <div className="membership-checkout-detail-row">
                        <dt className="membership-checkout-detail-label">应付金额</dt>
                        <dd className="membership-checkout-detail-value membership-checkout-total-price">{formatMoney(summary.actualPriceCents, checkout.currency)}</dd>
                    </div>
                </dl>
            </section>
        </section>
    );
}
