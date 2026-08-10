import { QRCode } from "antd";
import { Check, CircleMinus, CircleX, Clock3, WalletCards } from "lucide-react";

import { legalDocumentRoutes } from "@/constants/legal-documents";
import type { PaymentCheckout, PaymentProvider } from "@/services/api/payment";

import { checkoutTerminalPresentation, resolveCheckoutProviderSelection } from "./payment-checkout-domain";

const providerLabels: Record<PaymentProvider, string> = {
    wechat: "微信支付",
    alipay: "支付宝",
};

function formatCountdown(seconds: number): string {
    const safeSeconds = Math.max(0, Math.floor(seconds));
    const minutes = Math.floor(safeSeconds / 60)
        .toString()
        .padStart(2, "0");
    const remainder = (safeSeconds % 60).toString().padStart(2, "0");
    return `${minutes}:${remainder}`;
}

type PaymentQrPanelProps = {
    checkout: PaymentCheckout;
    checkoutSecondsLeft: number;
    onProviderChange: (provider: PaymentProvider) => void;
    onRetry: () => void;
    onReturn: () => void;
    onSubmit: () => void;
    paymentSecondsLeft: number;
    provider: PaymentProvider | null;
    refreshError: string;
    submissionError: string;
    submitting: boolean;
};

export function PaymentQrPanel({ checkout, checkoutSecondsLeft, onProviderChange, onRetry, onReturn, onSubmit, paymentSecondsLeft, provider, refreshError, submissionError, submitting }: PaymentQrPanelProps) {
    const terminal = checkoutTerminalPresentation(checkout);
    const selection = resolveCheckoutProviderSelection(checkout, provider);
    const transaction = terminal ? undefined : checkout.activeTransaction;
    const transactionExpired = Boolean(transaction) && paymentSecondsLeft <= 0;
    const checkoutClockExpired = checkoutSecondsLeft <= 0;

    if (terminal) {
        const TerminalIcon = terminal.tone === "success" ? Check : terminal.tone === "warning" ? CircleX : CircleMinus;
        return (
            <section aria-live="polite" className={`payment-checkout-terminal is-${terminal.tone}`}>
                <span aria-hidden="true" className="payment-checkout-terminal-icon">
                    <TerminalIcon aria-hidden="true" className="payment-checkout-terminal-icon-svg" />
                </span>
                <h2 className="payment-checkout-terminal-title">{terminal.title}</h2>
                <p className="payment-checkout-terminal-description">{terminal.description}</p>
                <button className="payment-checkout-action" onClick={onReturn} type="button">
                    {terminal.actionLabel}
                </button>
            </section>
        );
    }

    return (
        <section className="payment-checkout-qr-panel">
            {!transaction ? (
                <header className="payment-checkout-qr-heading">
                    <h2 className="payment-checkout-qr-title">扫码支付</h2>
                    <p className="payment-checkout-qr-intro">选择支付方式并生成本订单唯一的付款码。</p>
                </header>
            ) : null}

            {refreshError ? (
                <div className="payment-checkout-refresh-error" role="alert">
                    <span className="payment-checkout-refresh-error-copy">{refreshError}</span>
                    <button className="payment-checkout-inline-action" onClick={onRetry} type="button">
                        重试刷新
                    </button>
                </div>
            ) : null}

            {submissionError ? (
                <div className="payment-checkout-submission-error" role="alert">
                    <span className="payment-checkout-submission-error-copy">{submissionError}</span>
                </div>
            ) : null}

            {transaction ? null : selection.error ? (
                <div className="payment-checkout-provider-error" role="alert">
                    <span className="payment-checkout-provider-error-copy">{selection.error}</span>
                    <button className="payment-checkout-inline-action" onClick={onRetry} type="button">
                        重新检查
                    </button>
                </div>
            ) : (
                <fieldset className="payment-checkout-provider-fieldset" disabled={selection.locked || submitting}>
                    <legend className="payment-checkout-provider-legend">支付方式</legend>
                    <div className="payment-checkout-provider-list">
                        {selection.options.map((candidate) => (
                            <label className={`payment-checkout-provider ${selection.selected === candidate ? "is-active" : ""}`} key={candidate}>
                                <input type="radio" value={candidate} checked={selection.selected === candidate} className="payment-checkout-provider-input" name="payment-provider" onChange={() => onProviderChange(candidate)} />
                                <WalletCards aria-hidden="true" className="payment-checkout-provider-icon" />
                                <span className="payment-checkout-provider-label">{providerLabels[candidate]}</span>
                                <Check aria-hidden="true" className="payment-checkout-provider-check" />
                            </label>
                        ))}
                    </div>
                </fieldset>
            )}

            {transaction && !transactionExpired ? (
                <div className="payment-checkout-qr-content">
                    <div aria-label={`${providerLabels[transaction.provider]}付款二维码`} className="payment-checkout-qr-code" role="img">
                        <QRCode bgColor="var(--qr-background)" bordered={false} className="payment-checkout-qr-image" color="var(--qr-foreground)" errorLevel="M" marginSize={4} size={112} type="svg" value={transaction.codeUrl} />
                    </div>
                    <strong className="payment-checkout-qr-provider">请使用{providerLabels[transaction.provider]}扫码支付</strong>
                    <span aria-live="off" className="payment-checkout-countdown" role="timer">
                        <Clock3 aria-hidden="true" className="payment-checkout-countdown-icon" />
                        {formatCountdown(paymentSecondsLeft)} 后付款码失效
                    </span>
                    {checkout.orderType === "membership" ? (
                        <p className="payment-checkout-agreement">
                            开通即代表同意
                            <a className="payment-checkout-agreement-link" href={legalDocumentRoutes.membershipAgreement} rel="noopener noreferrer" target="_blank">
                                《HMaigc会员服务协议》
                            </a>
                        </p>
                    ) : null}
                </div>
            ) : transactionExpired ? (
                <div aria-live="polite" className="payment-checkout-clock-state">
                    <h3 className="payment-checkout-clock-state-title">付款码已失效</h3>
                    <p className="payment-checkout-clock-state-copy">正在向服务端确认交易状态，请勿重复创建或重复支付。</p>
                    <button className="payment-checkout-inline-action" onClick={onRetry} type="button">
                        刷新订单状态
                    </button>
                </div>
            ) : checkoutClockExpired ? (
                <div aria-live="polite" className="payment-checkout-clock-state">
                    <h3 className="payment-checkout-clock-state-title">付款时间已结束</h3>
                    <p className="payment-checkout-clock-state-copy">正在等待服务端确认订单状态。</p>
                    <button className="payment-checkout-inline-action" onClick={onRetry} type="button">
                        刷新订单状态
                    </button>
                </div>
            ) : selection.error ? null : (
                <div className="payment-checkout-generate">
                    <span aria-live="off" className="payment-checkout-countdown" role="timer">
                        <Clock3 aria-hidden="true" className="payment-checkout-countdown-icon" />
                        {formatCountdown(checkoutSecondsLeft)} 后收银台关闭
                    </span>
                    <button className="payment-checkout-action" disabled={!selection.selected || submitting} onClick={onSubmit} type="button">
                        {submitting ? "正在生成…" : "生成付款码"}
                    </button>
                </div>
            )}
        </section>
    );
}
