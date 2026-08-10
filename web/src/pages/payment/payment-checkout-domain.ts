import type { PaymentCheckout, PaymentCheckoutActiveTransaction, PaymentCheckoutStatus, PaymentOrderStatus, PaymentProvider } from "@/services/api/payment";

import { validateMembershipOrderFacts } from "./membership-order-validation";

export type CheckoutSummary =
    | {
          kind: "membership";
          title: string;
          audience: "personal" | "team";
          billingCycle: "month" | "year";
          seats: number;
          unitPriceCents: number;
          actualPriceCents: number;
          originalUnitPriceCents: number;
          originalPriceCents: number;
          discountCents: number;
          creditsPerPeriod: number;
          totalCredits: number;
      }
    | {
          kind: "credit_topup";
          title: "积分充值";
          actualPriceCents: number;
          totalCredits: number;
      };

export type RestoredCheckoutPayment = {
    provider: PaymentProvider;
    transaction: PaymentCheckoutActiveTransaction;
};

export type CheckoutRequestState = {
    checkout: PaymentCheckout;
    revision: number;
};

export type CheckoutLoadState = {
    checkout: PaymentCheckout | null;
    initialError: string;
    refreshError: string;
    token: string;
};

export type CheckoutProviderSelection = {
    options: PaymentProvider[];
    selected: PaymentProvider | null;
    locked: boolean;
    error: string;
};

export type CheckoutTerminalPresentation = {
    title: string;
    description: string;
    actionLabel: string;
    tone: "success" | "neutral" | "warning";
    showPaymentControls: false;
};

export type CheckoutRequestLease = {
    generation: number;
    token: string;
    kind: "load" | "submission";
    id: number;
    revision: number;
};

export class CheckoutRequestCoordinator {
    private active = false;
    private generation = 0;
    private token = "";
    private nextLeaseID = 0;
    private revision = 0;
    private loadLeaseID: number | null = null;
    private submissionLeaseID: number | null = null;

    activate(token: string): void {
        if (!hasCheckoutToken(token)) throw new Error("支付链接缺少结算凭证");
        this.generation += 1;
        this.active = true;
        this.token = token;
        this.revision = 0;
        this.loadLeaseID = null;
        this.submissionLeaseID = null;
    }

    dispose(): void {
        this.generation += 1;
        this.active = false;
        this.token = "";
        this.loadLeaseID = null;
        this.submissionLeaseID = null;
    }

    beginLoad(token: string): CheckoutRequestLease | null {
        if (!this.active || token !== this.token || this.loadLeaseID !== null || this.submissionLeaseID !== null) return null;
        this.revision += 1;
        const lease = this.createLease("load", this.revision);
        this.loadLeaseID = lease.id;
        return lease;
    }

    completeLoad(lease: CheckoutRequestLease): number | null {
        if (!this.isCurrentLease(lease, "load", this.loadLeaseID)) return null;
        this.loadLeaseID = null;
        return lease.revision;
    }

    releaseLoad(lease: CheckoutRequestLease): boolean {
        if (!this.isCurrentLease(lease, "load", this.loadLeaseID)) return false;
        this.loadLeaseID = null;
        return true;
    }

    beginSubmission(token: string): CheckoutRequestLease | null {
        if (!this.active || token !== this.token || this.submissionLeaseID !== null) return null;
        const lease = this.createLease("submission", 0);
        this.submissionLeaseID = lease.id;
        return lease;
    }

    completeSubmission(lease: CheckoutRequestLease): number | null {
        if (!this.isCurrentLease(lease, "submission", this.submissionLeaseID)) return null;
        this.submissionLeaseID = null;
        this.revision += 1;
        return this.revision;
    }

    releaseSubmission(lease: CheckoutRequestLease): boolean {
        if (!this.isCurrentLease(lease, "submission", this.submissionLeaseID)) return false;
        this.submissionLeaseID = null;
        return true;
    }

    private createLease(kind: CheckoutRequestLease["kind"], revision: number): CheckoutRequestLease {
        this.nextLeaseID += 1;
        return { generation: this.generation, token: this.token, kind, id: this.nextLeaseID, revision };
    }

    private isCurrentLease(lease: CheckoutRequestLease, kind: CheckoutRequestLease["kind"], activeLeaseID: number | null): boolean {
        return this.active && lease.kind === kind && lease.generation === this.generation && lease.token === this.token && lease.id === activeLeaseID;
    }
}

export function hasCheckoutToken(token: string): boolean {
    return token.trim() !== "";
}

export function paymentCheckoutTokenFromURL(checkoutURL: string, baseOrigin: string): string {
    try {
        const parsed = new URL(checkoutURL, baseOrigin);
        if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || parsed.search || parsed.hash) throw new Error("invalid checkout URL");
        const segments = parsed.pathname.split("/").filter(Boolean);
        if (segments.length !== 2 || segments[0] !== "pay") throw new Error("invalid checkout path");
        const token = decodeURIComponent(segments[1]);
        if (!hasCheckoutToken(token) || token.includes("/")) throw new Error("invalid checkout token");
        return token;
    } catch {
        throw new Error("支付链接返回的结算凭证无效");
    }
}

const orderStatusRank: Record<PaymentOrderStatus, number> = {
    pending: 0,
    cancelled: 1,
    paid: 2,
    refunded: 3,
};

const checkoutStatusRank: Record<PaymentCheckoutStatus, number> = {
    active: 0,
    expired: 1,
    consumed: 2,
};

function parseCheckoutTime(value: string, field: string): number {
    const timestamp = Date.parse(value);
    if (!Number.isFinite(timestamp)) throw new Error(`${field}不是有效时间`);
    return timestamp;
}

export function checkoutServerOffsetMs(serverNow: string, receivedAtMs: number): number {
    if (!Number.isFinite(receivedAtMs)) throw new Error("客户端接收时间无效");
    return parseCheckoutTime(serverNow, "服务端时间") - receivedAtMs;
}

export function checkoutRemainingSeconds(expiresAt: string, serverOffsetMs: number, clientNowMs: number): number {
    if (!Number.isFinite(serverOffsetMs) || !Number.isFinite(clientNowMs)) throw new Error("收银台计时基准无效");
    return Math.max(0, Math.ceil((parseCheckoutTime(expiresAt, "收银台过期时间") - clientNowMs - serverOffsetMs) / 1000));
}

export function createCheckoutLoadState(token: string): CheckoutLoadState {
    return { checkout: null, initialError: "", refreshError: "", token };
}

export function visibleCheckoutForToken(state: CheckoutLoadState, token: string): PaymentCheckout | null {
    return state.token === token ? state.checkout : null;
}

export function checkoutRequestSucceeded(current: CheckoutLoadState, checkout: PaymentCheckout): CheckoutLoadState {
    return { ...current, checkout, initialError: "", refreshError: "" };
}

export function checkoutRequestFailed(current: CheckoutLoadState, reason: string): CheckoutLoadState {
    const message = reason.trim() || "支付订单加载失败";
    if (current.checkout) return { ...current, refreshError: message };
    return { checkout: null, initialError: message, refreshError: "", token: current.token };
}

export function checkoutRequestSucceededForToken(current: CheckoutLoadState, token: string, checkout: PaymentCheckout): CheckoutLoadState {
    return current.token === token ? checkoutRequestSucceeded(current, checkout) : current;
}

export function checkoutRequestFailedForToken(current: CheckoutLoadState, token: string, reason: string): CheckoutLoadState {
    return current.token === token ? checkoutRequestFailed(current, reason) : current;
}

export function mergePaymentCheckout(current: PaymentCheckout, incoming: PaymentCheckout): PaymentCheckout {
    if (current.orderType !== incoming.orderType || current.orderNumber !== incoming.orderNumber) {
        throw new Error("轮询返回了不同的收银台订单");
    }
    const orderStatus = orderStatusRank[current.orderStatus] > orderStatusRank[incoming.orderStatus] ? current.orderStatus : incoming.orderStatus;
    const checkoutStatus = checkoutStatusRank[current.checkoutStatus] > checkoutStatusRank[incoming.checkoutStatus] ? current.checkoutStatus : incoming.checkoutStatus;
    if (orderStatus !== "pending" || checkoutStatus !== "active") {
        return { ...incoming, orderStatus, checkoutStatus, activeTransaction: undefined };
    }
    return { ...incoming, orderStatus, checkoutStatus };
}

function requireCheckoutRevision(revision: number): void {
    if (!Number.isSafeInteger(revision) || revision < 0) throw new Error("收银台响应序号无效");
}

export function mergeCheckoutResponse(current: CheckoutRequestState, incoming: PaymentCheckout, revision: number): CheckoutRequestState {
    requireCheckoutRevision(revision);
    if (revision < current.revision) return current;
    return { checkout: mergePaymentCheckout(current.checkout, incoming), revision };
}

export function applyCheckoutTransaction(current: CheckoutRequestState, transaction: PaymentCheckoutActiveTransaction, revision: number): CheckoutRequestState {
    requireCheckoutRevision(revision);
    if (revision <= current.revision) throw new Error("支付交易响应序号必须晚于当前收银台事实");
    if (current.checkout.orderStatus !== "pending" || current.checkout.checkoutStatus !== "active") {
        throw new Error("终态收银台不能接收活动支付交易");
    }
    if (transaction.status !== "pending" || transaction.codeUrl.trim() === "") {
        throw new Error("支付交易事实不完整");
    }
    return {
        checkout: { ...current.checkout, activeTransaction: transaction },
        revision,
    };
}

export function restoreActiveCheckoutPayment(checkout: PaymentCheckout): RestoredCheckoutPayment | null {
    const transaction = checkout.activeTransaction;
    if (!transaction) return null;
    if (checkout.orderStatus !== "pending" || checkout.checkoutStatus !== "active") {
        throw new Error("终态收银台不能包含活动支付交易");
    }
    if (transaction.status !== "pending" || transaction.codeUrl.trim() === "") {
        throw new Error("活动支付交易与收银台事实不一致");
    }
    return { provider: transaction.provider, transaction };
}

export function resolveCheckoutProviderSelection(checkout: PaymentCheckout, requested: PaymentProvider | null): CheckoutProviderSelection {
    const restored = restoreActiveCheckoutPayment(checkout);
    if (restored) {
        return {
            options: [restored.provider, ...checkout.providers.filter((candidate) => candidate !== restored.provider)],
            selected: restored.provider,
            locked: true,
            error: "",
        };
    }
    const options = [...new Set(checkout.providers)];
    if (options.length === 0) {
        return {
            options,
            selected: null,
            locked: false,
            error: "当前没有可用的支付渠道，请稍后重试或联系管理员。",
        };
    }
    return {
        options,
        selected: requested && options.includes(requested) ? requested : options[0],
        locked: false,
        error: "",
    };
}

export function automaticPaymentProvider(checkout: PaymentCheckout, selected: PaymentProvider | null, attempted: boolean): PaymentProvider | null {
    if (attempted || checkout.orderStatus !== "pending" || checkout.checkoutStatus !== "active" || checkout.activeTransaction) return null;
    const selection = resolveCheckoutProviderSelection(checkout, selected);
    if (selection.locked || selection.options.length !== 1 || selection.selected !== selection.options[0]) return null;
    return selection.selected;
}

export function selectCheckoutProvider(checkout: PaymentCheckout, requested: PaymentProvider): PaymentProvider {
    const selection = resolveCheckoutProviderSelection(checkout, requested);
    if (selection.locked && selection.selected) return selection.selected;
    if (!selection.options.includes(requested)) throw new Error("所选支付渠道当前不可用");
    return requested;
}

export function checkoutPaymentExpiresAt(checkout: PaymentCheckout): string {
    return checkout.activeTransaction?.expiresAt ?? checkout.expiresAt;
}

export function shouldContinueCheckoutPolling(checkout: PaymentCheckout): boolean {
    return checkout.orderStatus === "pending" && checkout.checkoutStatus === "active";
}

export function checkoutTerminalPresentation(checkout: PaymentCheckout): CheckoutTerminalPresentation | null {
    if (checkout.orderStatus === "refunded") {
        return {
            title: "订单状态待核对",
            description: "系统记录为退款状态，但当前不提供在线退款结果确认，请联系管理员核对支付渠道记录。",
            actionLabel: "查看订单记录",
            tone: "warning",
            showPaymentControls: false,
        };
    }
    if (checkout.orderStatus === "paid") {
        return {
            title: "支付成功",
            description: checkout.orderType === "membership" ? "会员权益已激活，可返回会员中心查看。" : "积分已到账，可返回积分商城查看。",
            actionLabel: "查看到账结果",
            tone: "success",
            showPaymentControls: false,
        };
    }
    if (checkout.orderStatus === "cancelled") {
        return {
            title: "订单已关闭",
            description: "本订单已经关闭，无法继续付款，请返回订单入口重新下单。",
            actionLabel: "返回订单入口",
            tone: "warning",
            showPaymentControls: false,
        };
    }
    if (checkout.checkoutStatus === "expired") {
        return {
            title: "收银台已过期",
            description: "本次付款链接已经过期。请返回订单记录，关闭旧订单后重新下单；若关闭时提示需对账，请等待管理员核对支付渠道结果。",
            actionLabel: "返回订单入口",
            tone: "warning",
            showPaymentControls: false,
        };
    }
    if (checkout.checkoutStatus === "consumed") {
        return {
            title: "收银台已关闭",
            description: "本次付款链接已经关闭，请返回订单入口查看订单状态。",
            actionLabel: "返回订单入口",
            tone: "neutral",
            showPaymentControls: false,
        };
    }
    return null;
}

function requireSafeNonNegativeInteger(value: number, field: string): void {
    if (!Number.isSafeInteger(value) || value < 0) throw new Error(`${field}必须是安全的非负整数`);
}

export function checkoutSummary(checkout: PaymentCheckout): CheckoutSummary {
    if (checkout.orderType === "membership") {
        const facts = checkout.membershipSummary;
        const validated = validateMembershipOrderFacts({
            audience: facts.audience,
            billingCycle: facts.billingCycle,
            code: facts.code,
            creditsPerPeriod: facts.creditsPerPeriod,
            currency: checkout.currency,
            name: facts.name,
            orderNumber: checkout.orderNumber,
            originalPriceCents: facts.originalPriceCents,
            seats: facts.seats,
            source: "checkout",
            tier: facts.tier,
            totalCredits: facts.totalCreditsPerPeriod,
            totalPriceCents: facts.actualPriceCents,
        });
        return {
            kind: "membership",
            title: facts.name,
            audience: facts.audience,
            billingCycle: facts.billingCycle,
            seats: facts.seats,
            unitPriceCents: validated.unitPriceCents,
            actualPriceCents: facts.actualPriceCents,
            originalUnitPriceCents: validated.originalUnitPriceCents,
            originalPriceCents: facts.originalPriceCents,
            discountCents: facts.originalPriceCents > facts.actualPriceCents ? facts.originalPriceCents - facts.actualPriceCents : 0,
            creditsPerPeriod: facts.creditsPerPeriod,
            totalCredits: facts.totalCreditsPerPeriod,
        };
    }
    requireSafeNonNegativeInteger(checkout.creditTopupSummary.actualPriceCents, "积分充值实付金额");
    requireSafeNonNegativeInteger(checkout.creditTopupSummary.totalMicrocredits, "积分充值数量");
    return {
        kind: "credit_topup",
        title: "积分充值",
        actualPriceCents: checkout.creditTopupSummary.actualPriceCents,
        totalCredits: checkout.creditTopupSummary.totalMicrocredits,
    };
}

export function checkoutDiscountOriginalPrice(summary: CheckoutSummary): number | null {
    if (summary.kind !== "membership" || summary.originalPriceCents <= summary.actualPriceCents) return null;
    return summary.originalPriceCents;
}
