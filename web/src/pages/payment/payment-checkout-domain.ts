import type { PaymentCheckout, PaymentCheckoutActiveTransaction, PaymentCheckoutStatus, PaymentOrderStatus, PaymentProvider } from "@/services/api/payment";

export type CheckoutSummary =
    | {
          kind: "membership";
          title: string;
          audience: "personal" | "team";
          billingCycle: "month" | "year";
          seats: number;
          actualPriceCents: number;
          originalPriceCents: number;
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

export function checkoutSummary(checkout: PaymentCheckout): CheckoutSummary {
    if (checkout.orderType === "membership") {
        return {
            kind: "membership",
            title: checkout.membershipSummary.name,
            audience: checkout.membershipSummary.audience,
            billingCycle: checkout.membershipSummary.billingCycle,
            seats: checkout.membershipSummary.seats,
            actualPriceCents: checkout.membershipSummary.actualPriceCents,
            originalPriceCents: checkout.membershipSummary.originalPriceCents,
            totalCredits: checkout.membershipSummary.totalCreditsPerPeriod,
        };
    }
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
