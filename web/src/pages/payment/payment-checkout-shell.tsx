import { ArrowLeft, ShieldCheck } from "lucide-react";
import type { ReactNode } from "react";

type PaymentCheckoutShellProps = {
    busy: boolean;
    mode: "page" | "dialog";
    onBack: () => void;
    payment: ReactNode;
    summary: ReactNode;
};

type PaymentCheckoutInitialErrorProps = {
    canRetry: boolean;
    message: string;
    onRetry: () => void;
};

export function PaymentCheckoutInitialError({ canRetry, message, onRetry }: PaymentCheckoutInitialErrorProps) {
    return (
        <div className="payment-checkout-initial-error" role="alert">
            <p className="payment-checkout-initial-error-copy">{message}</p>
            {canRetry ? (
                <button className="payment-checkout-action" onClick={onRetry} type="button">
                    重新加载
                </button>
            ) : null}
        </div>
    );
}

export function PaymentCheckoutShell({ busy, mode, onBack, payment, summary }: PaymentCheckoutShellProps) {
    const checkout = (
        <section aria-busy={busy} aria-label="订单收银台" className={`payment-checkout-shell is-${mode}`}>
            <div className="payment-checkout-order-surface">{summary}</div>
            <aside aria-label="扫码支付" className="payment-checkout-payment-surface">
                {payment}
            </aside>
        </section>
    );

    if (mode === "dialog") return checkout;

    return (
        <main className="payment-checkout-page">
            <header className="payment-checkout-header">
                <button aria-label="返回订单入口" className="payment-checkout-back" onClick={onBack} type="button">
                    <ArrowLeft aria-hidden="true" className="payment-checkout-back-icon" />
                    <span className="payment-checkout-back-label">返回订单</span>
                </button>
                <span className="payment-checkout-security">
                    <ShieldCheck aria-hidden="true" className="payment-checkout-security-icon" />
                    <span className="payment-checkout-security-label">安全收银台</span>
                </span>
            </header>
            {checkout}
        </main>
    );
}
