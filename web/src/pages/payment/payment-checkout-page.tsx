import { Alert, Button, QRCode, Spin, message } from "antd";
import { ArrowLeft, Check, Clock3, ShieldCheck, WalletCards } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";

import { createPaymentTransaction, getPaymentCheckout, type PaymentCheckout, type PaymentProvider, type PaymentTransaction } from "@/services/api/payment";

import "./payment-checkout.css";

const providerLabels: Record<PaymentProvider, string> = {
    wechat: "微信支付",
    alipay: "支付宝",
};

function formatMoney(cents: number, currency: string) {
    return new Intl.NumberFormat("zh-CN", {
        currency: currency.toUpperCase(),
        currencyDisplay: "narrowSymbol",
        style: "currency",
    }).format(cents / 100);
}

function remainingSeconds(expiresAt: string) {
    return Math.max(0, Math.ceil((new Date(expiresAt).getTime() - Date.now()) / 1000));
}

function formatCountdown(seconds: number) {
    const minutes = Math.floor(seconds / 60).toString().padStart(2, "0");
    const remainder = (seconds % 60).toString().padStart(2, "0");
    return `${minutes}:${remainder}`;
}

export default function PaymentCheckoutPage() {
    const navigate = useNavigate();
    const { token = "" } = useParams();
    const [checkout, setCheckout] = useState<PaymentCheckout | null>(null);
    const [provider, setProvider] = useState<PaymentProvider | null>(null);
    const [transaction, setTransaction] = useState<PaymentTransaction | null>(null);
    const [loading, setLoading] = useState(true);
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState("");
    const [secondsLeft, setSecondsLeft] = useState(0);

    const loadCheckout = useCallback(async () => {
        if (!token) {
            setError("支付链接缺少结算凭证");
            setLoading(false);
            return;
        }
        try {
            const next = await getPaymentCheckout(token);
            setCheckout(next);
            setSecondsLeft(remainingSeconds(next.expiresAt));
            setProvider((current) => current && next.providers.includes(current) ? current : next.providers[0] ?? null);
            setError("");
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "支付订单加载失败");
        } finally {
            setLoading(false);
        }
    }, [token]);

    useEffect(() => {
        void loadCheckout();
    }, [loadCheckout]);

    useEffect(() => {
        if (!checkout || checkout.status !== "pending") return;
        const timer = window.setInterval(() => setSecondsLeft(remainingSeconds(checkout.expiresAt)), 1000);
        const poller = window.setInterval(() => void loadCheckout(), 3000);
        return () => {
            window.clearInterval(timer);
            window.clearInterval(poller);
        };
    }, [checkout, loadCheckout]);

    const expired = useMemo(() => secondsLeft <= 0 || checkout?.status === "expired", [checkout?.status, secondsLeft]);
    const paid = checkout?.status === "paid";

    const submitPayment = async () => {
        if (!provider || !token || expired) return;
        setSubmitting(true);
        try {
            const next = await createPaymentTransaction(token, provider);
            setTransaction(next);
            if (!next.codeUrl) throw new Error("支付渠道未返回付款二维码，请检查后台渠道配置");
        } catch (reason) {
            message.error(reason instanceof Error ? reason.message : "发起支付失败");
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <main className="payment-checkout-page">
            <header className="payment-checkout-header">
                <button aria-label="返回会员中心" className="payment-checkout-back" onClick={() => navigate("/membership")} type="button">
                    <ArrowLeft className="payment-checkout-back-icon" />
                    <span className="payment-checkout-back-label">会员中心</span>
                </button>
                <span className="payment-checkout-security"><ShieldCheck className="payment-checkout-security-icon" />安全收银台</span>
            </header>

            <section aria-labelledby="payment-checkout-title" className="payment-checkout-panel">
                {loading ? <div className="payment-checkout-loading"><Spin className="payment-checkout-spinner" /></div> : error ? (
                    <Alert action={<Button className="payment-checkout-retry" onClick={() => void loadCheckout()}>重新加载</Button>} className="payment-checkout-alert" description={error} message="无法打开收银台" showIcon type="error" />
                ) : checkout ? (
                    <>
                        <div className="payment-checkout-summary">
                            <div className="payment-checkout-summary-copy">
                                <span className="payment-checkout-eyebrow">订单 {checkout.orderNumber}</span>
                                <h1 className="payment-checkout-title" id="payment-checkout-title">确认并完成支付</h1>
                                <p className="payment-checkout-description">支付成功后，会员权益和积分将由服务端自动入账。</p>
                            </div>
                            <div className="payment-checkout-amount">
                                <small className="payment-checkout-amount-label">应付金额</small>
                                <strong className="payment-checkout-amount-value">{formatMoney(checkout.amountCents, checkout.currency)}</strong>
                                {!paid ? <span className="payment-checkout-countdown"><Clock3 className="payment-checkout-countdown-icon" />{expired ? "订单已过期" : `${formatCountdown(secondsLeft)} 后关闭`}</span> : null}
                            </div>
                        </div>

                        {paid ? (
                            <div className="payment-checkout-result is-success">
                                <span className="payment-checkout-result-icon"><Check className="payment-checkout-result-check" /></span>
                                <h2 className="payment-checkout-result-title">支付成功</h2>
                                <p className="payment-checkout-result-description">会员权益已激活，可返回会员中心查看。</p>
                                <Button className="payment-checkout-primary" onClick={() => navigate("/membership")} type="primary">查看会员权益</Button>
                            </div>
                        ) : expired || checkout.status !== "pending" ? (
                            <div className="payment-checkout-result">
                                <h2 className="payment-checkout-result-title">当前订单不可支付</h2>
                                <p className="payment-checkout-result-description">订单状态：{checkout.status}。请返回会员中心重新下单。</p>
                                <Button className="payment-checkout-primary" onClick={() => navigate("/membership")} type="primary">返回会员中心</Button>
                            </div>
                        ) : (
                            <div className="payment-checkout-body">
                                <div className="payment-checkout-methods">
                                    <h2 className="payment-checkout-section-title">选择支付方式</h2>
                                    <div className="payment-checkout-provider-list">
                                        {checkout.providers.map((candidate) => (
                                            <button aria-pressed={provider === candidate} className={`payment-checkout-provider ${provider === candidate ? "is-active" : ""}`} key={candidate} onClick={() => { setProvider(candidate); setTransaction(null); }} type="button">
                                                <WalletCards className="payment-checkout-provider-icon" />
                                                <span className="payment-checkout-provider-label">{providerLabels[candidate]}</span>
                                                <span className="payment-checkout-provider-check"><Check className="payment-checkout-provider-check-icon" /></span>
                                            </button>
                                        ))}
                                    </div>
                                    {!transaction?.codeUrl ? <Button className="payment-checkout-primary" disabled={!provider} loading={submitting} onClick={() => void submitPayment()} type="primary">确认支付</Button> : null}
                                </div>

                                <div className="payment-checkout-qr-panel">
                                    {transaction?.codeUrl ? (
                                        <>
                                            <QRCode bgColor="#ffffff" bordered={false} className="payment-checkout-qr" color="#111111" errorLevel="M" size={228} value={transaction.codeUrl} />
                                            <strong className="payment-checkout-qr-title">使用{provider ? providerLabels[provider] : "支付应用"}扫码</strong>
                                            <span className="payment-checkout-qr-description">付款完成后本页会自动更新，请勿重复支付。</span>
                                        </>
                                    ) : (
                                        <div className="payment-checkout-qr-placeholder">
                                            <WalletCards className="payment-checkout-qr-placeholder-icon" />
                                            <span className="payment-checkout-qr-placeholder-copy">确认支付方式后生成付款二维码</span>
                                        </div>
                                    )}
                                </div>
                            </div>
                        )}
                    </>
                ) : null}
            </section>
        </main>
    );
}
