import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router";

import { createPaymentTransaction, getPaymentCheckout, type PaymentProvider } from "@/services/api/payment";

import { MembershipCheckoutSummary } from "./membership-checkout-summary";
import {
    CheckoutRequestCoordinator,
    applyCheckoutTransaction,
    checkoutPaymentExpiresAt,
    checkoutRemainingSeconds,
    checkoutRequestFailed,
    checkoutRequestFailedForToken,
    checkoutRequestSucceededForToken,
    checkoutServerOffsetMs,
    checkoutSummary,
    createCheckoutLoadState,
    hasCheckoutToken,
    mergeCheckoutResponse,
    resolveCheckoutProviderSelection,
    selectCheckoutProvider,
    shouldContinueCheckoutPolling,
    visibleCheckoutForToken,
    type CheckoutLoadState,
    type CheckoutRequestState,
} from "./payment-checkout-domain";
import { PaymentCheckoutInitialError, PaymentCheckoutShell } from "./payment-checkout-shell";
import { PaymentQrPanel } from "./payment-qr-panel";

import "./payment-checkout.css";

function errorMessage(reason: unknown, fallback: string): string {
    return reason instanceof Error ? reason.message : fallback;
}

export default function PaymentCheckoutPage() {
    const navigate = useNavigate();
    const { token = "" } = useParams();
    const coordinatorRef = useRef<CheckoutRequestCoordinator | null>(null);
    if (coordinatorRef.current === null) coordinatorRef.current = new CheckoutRequestCoordinator();

    const checkoutStateRef = useRef<CheckoutRequestState | null>(null);
    const providerRef = useRef<PaymentProvider | null>(null);
    const [loadState, setLoadState] = useState<CheckoutLoadState>(() => createCheckoutLoadState(token));
    const [provider, setProvider] = useState<PaymentProvider | null>(null);
    const [loading, setLoading] = useState(true);
    const [submitting, setSubmitting] = useState(false);
    const [submissionError, setSubmissionError] = useState("");
    const [checkoutSecondsLeft, setCheckoutSecondsLeft] = useState(0);
    const [paymentSecondsLeft, setPaymentSecondsLeft] = useState(0);
    const [serverOffsetMs, setServerOffsetMs] = useState(0);

    const checkout = visibleCheckoutForToken(loadState, token);
    const hasToken = hasCheckoutToken(token);
    const waitingForCurrentToken = loadState.token !== token;
    const membershipDestination = checkout?.orderType === "credit_topup" ? "/credit-store" : "/membership";

    const loadCheckout = useCallback(async (capturedToken: string) => {
        const coordinator = coordinatorRef.current;
        if (!coordinator) return;
        const lease = coordinator.beginLoad(capturedToken);
        if (!lease) return;

        try {
            const next = await getPaymentCheckout(capturedToken);
            const receivedAtMs = Date.now();
            checkoutSummary(next);
            const offset = checkoutServerOffsetMs(next.serverNow, receivedAtMs);
            const current = checkoutStateRef.current;
            const acceptedState = current ? mergeCheckoutResponse(current, next, lease.revision) : { checkout: next, revision: lease.revision };
            const accepted = acceptedState.checkout;
            const selection = resolveCheckoutProviderSelection(accepted, providerRef.current);
            const nextCheckoutSeconds = checkoutRemainingSeconds(accepted.expiresAt, offset, receivedAtMs);
            const nextPaymentSeconds = checkoutRemainingSeconds(checkoutPaymentExpiresAt(accepted), offset, receivedAtMs);
            const revision = coordinator.completeLoad(lease);
            if (revision === null || revision !== lease.revision) return;

            checkoutStateRef.current = acceptedState;
            providerRef.current = selection.selected;
            setProvider(selection.selected);
            setServerOffsetMs(offset);
            setCheckoutSecondsLeft(nextCheckoutSeconds);
            setPaymentSecondsLeft(nextPaymentSeconds);
            setLoadState((currentState) => checkoutRequestSucceededForToken(currentState, lease.token, accepted));
            if (accepted.activeTransaction) setSubmissionError("");
            setLoading(false);
        } catch (reason) {
            if (!coordinator.releaseLoad(lease)) return;
            setLoadState((currentState) => checkoutRequestFailedForToken(currentState, lease.token, errorMessage(reason, "支付订单加载失败")));
            setLoading(false);
        }
    }, []);

    useEffect(() => {
        const coordinator = coordinatorRef.current;
        if (!coordinator) return;

        checkoutStateRef.current = null;
        providerRef.current = null;
        const nextLoadState = createCheckoutLoadState(token);
        setLoadState(nextLoadState);
        setProvider(null);
        setSubmitting(false);
        setSubmissionError("");
        setCheckoutSecondsLeft(0);
        setPaymentSecondsLeft(0);
        setServerOffsetMs(0);
        setLoading(true);

        if (!hasToken) {
            coordinator.dispose();
            setLoadState(checkoutRequestFailed(nextLoadState, "支付链接缺少结算凭证"));
            setLoading(false);
            return;
        }

        coordinator.activate(token);
        void loadCheckout(token);
        return () => coordinator.dispose();
    }, [hasToken, loadCheckout, token]);

    useEffect(() => {
        if (!checkout || !shouldContinueCheckoutPolling(checkout)) return;
        const timer = window.setInterval(() => {
            const now = Date.now();
            setCheckoutSecondsLeft(checkoutRemainingSeconds(checkout.expiresAt, serverOffsetMs, now));
            setPaymentSecondsLeft(checkoutRemainingSeconds(checkoutPaymentExpiresAt(checkout), serverOffsetMs, now));
        }, 1000);
        const poller = window.setInterval(() => void loadCheckout(token), 3000);
        return () => {
            window.clearInterval(timer);
            window.clearInterval(poller);
        };
    }, [checkout, loadCheckout, serverOffsetMs, token]);

    const chooseProvider = useCallback(
        (candidate: PaymentProvider) => {
            if (!checkout) return;
            const selected = selectCheckoutProvider(checkout, candidate);
            providerRef.current = selected;
            setProvider(selected);
            setSubmissionError("");
        },
        [checkout],
    );

    const submitPayment = useCallback(async () => {
        if (!checkout || !provider || !hasToken || checkoutSecondsLeft <= 0 || checkout.activeTransaction) return;
        const coordinator = coordinatorRef.current;
        if (!coordinator) return;
        const lease = coordinator.beginSubmission(token);
        if (!lease) return;

        setSubmitting(true);
        setSubmissionError("");
        let completedCurrentLifecycle = false;
        try {
            const next = await createPaymentTransaction(lease.token, provider);
            if (!next.codeUrl.trim()) throw new Error("支付渠道未返回付款二维码，请检查后台渠道配置");
            const current = checkoutStateRef.current;
            if (!current || current.checkout.orderStatus !== "pending" || current.checkout.checkoutStatus !== "active") {
                throw new Error("收银台当前不可创建支付交易");
            }
            const revision = coordinator.completeSubmission(lease);
            if (revision === null) return;
            completedCurrentLifecycle = true;
            const acceptedState = applyCheckoutTransaction(current, next, revision);
            const now = Date.now();

            checkoutStateRef.current = acceptedState;
            providerRef.current = next.provider;
            setProvider(next.provider);
            setPaymentSecondsLeft(checkoutRemainingSeconds(next.expiresAt, serverOffsetMs, now));
            setLoadState((currentState) => checkoutRequestSucceededForToken(currentState, lease.token, acceptedState.checkout));
            setSubmitting(false);
        } catch (reason) {
            if (!completedCurrentLifecycle && !coordinator.releaseSubmission(lease)) return;
            setSubmissionError(errorMessage(reason, "发起支付失败"));
            setSubmitting(false);
        }
    }, [checkout, checkoutSecondsLeft, hasToken, provider, serverOffsetMs, token]);

    const retryCheckout = useCallback(() => {
        void loadCheckout(token);
    }, [loadCheckout, token]);

    const returnToOrderEntry = useCallback(() => {
        navigate(membershipDestination);
    }, [membershipDestination, navigate]);

    if ((loading || waitingForCurrentToken) && !checkout) {
        return (
            <PaymentCheckoutShell
                busy
                onBack={returnToOrderEntry}
                payment={
                    <div aria-hidden="true" className="payment-checkout-skeleton payment-checkout-payment-skeleton">
                        <span className="payment-checkout-skeleton-line is-short" />
                        <span className="payment-checkout-skeleton-block" />
                    </div>
                }
                summary={
                    <div aria-label="正在加载订单" className="payment-checkout-skeleton membership-checkout-summary" role="status">
                        <span className="payment-checkout-skeleton-line is-short" />
                        <span className="payment-checkout-skeleton-line" />
                        <span className="payment-checkout-skeleton-block" />
                    </div>
                }
            />
        );
    }

    if (!checkout) {
        return (
            <PaymentCheckoutShell
                busy={false}
                onBack={returnToOrderEntry}
                payment={<PaymentCheckoutInitialError canRetry={hasToken} message={loadState.initialError} onRetry={retryCheckout} />}
                summary={
                    <section className="membership-checkout-summary payment-checkout-error-summary">
                        <p className="membership-checkout-eyebrow">安全收银台</p>
                        <h1 className="membership-checkout-title" id="payment-checkout-title">
                            无法验证订单信息
                        </h1>
                        <p className="membership-checkout-order-number">请检查支付链接后重试，或返回订单入口重新获取链接。</p>
                    </section>
                }
            />
        );
    }

    return (
        <PaymentCheckoutShell
            busy={false}
            onBack={returnToOrderEntry}
            payment={
                <PaymentQrPanel
                    checkout={checkout}
                    checkoutSecondsLeft={checkoutSecondsLeft}
                    onProviderChange={chooseProvider}
                    onRetry={retryCheckout}
                    onReturn={returnToOrderEntry}
                    onSubmit={() => void submitPayment()}
                    paymentSecondsLeft={paymentSecondsLeft}
                    provider={provider}
                    refreshError={loadState.refreshError}
                    submissionError={submissionError}
                    submitting={submitting}
                />
            }
            summary={<MembershipCheckoutSummary checkout={checkout} />}
        />
    );
}
