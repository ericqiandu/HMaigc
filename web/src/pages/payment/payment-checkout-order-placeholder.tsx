import type { ReactElement } from "react";

type PaymentCheckoutOrderPlaceholderProps = {
    presentation: "failed" | "loading";
};

export function PaymentCheckoutOrderPlaceholder({ presentation }: PaymentCheckoutOrderPlaceholderProps): ReactElement {
    if (presentation === "failed") {
        return (
            <section aria-labelledby="payment-checkout-order-placeholder-title" className="payment-checkout-order-placeholder is-failed" role="status">
                <header className="payment-checkout-order-placeholder-heading">
                    <h1 className="payment-checkout-order-placeholder-title" id="payment-checkout-order-placeholder-title">
                        订单信息暂不可用
                    </h1>
                </header>
                <div className="payment-checkout-order-placeholder-content">
                    <p className="payment-checkout-order-placeholder-copy">未能读取订单类型与金额，请查看右侧错误并重试。</p>
                </div>
            </section>
        );
    }

    return (
        <section aria-busy="true" aria-labelledby="payment-checkout-order-placeholder-title" className="payment-checkout-order-placeholder is-loading" role="status">
            <header className="payment-checkout-order-placeholder-heading">
                <h1 className="payment-checkout-order-placeholder-title" id="payment-checkout-order-placeholder-title">
                    正在识别订单
                </h1>
            </header>
            <div className="payment-checkout-order-placeholder-content">
                <p className="payment-checkout-order-placeholder-copy">正在读取冻结订单类型与金额，请稍候。</p>
                <span className="payment-checkout-skeleton-line" />
                <span className="payment-checkout-skeleton-block" />
            </div>
        </section>
    );
}
