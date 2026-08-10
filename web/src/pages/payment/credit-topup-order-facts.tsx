import type { ReactElement } from "react";

import type { PaymentCheckout } from "@/services/api/payment";

import { checkoutSummary } from "./payment-checkout-domain";
import { formatPaymentOrderCredits, formatPaymentOrderMoney } from "./payment-order-formatters";

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
                        <dd className="membership-checkout-detail-value">{formatPaymentOrderCredits(summary.totalCredits)} 积分</dd>
                    </div>
                    <div className="membership-checkout-detail-row">
                        <dt className="membership-checkout-detail-label">应付金额</dt>
                        <dd className="membership-checkout-detail-value membership-checkout-total-price">{formatPaymentOrderMoney(summary.actualPriceCents, checkout.currency)}</dd>
                    </div>
                </dl>
            </section>
        </section>
    );
}
