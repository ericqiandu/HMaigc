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

const terminalOrderStatuses = new Set<PaymentOrderStatus>(["paid", "cancelled", "refunded"]);
const terminalCheckoutStatuses = new Set<PaymentCheckoutStatus>(["expired", "consumed"]);

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
    const orderStatus = terminalOrderStatuses.has(current.orderStatus) && !terminalOrderStatuses.has(incoming.orderStatus) ? current.orderStatus : incoming.orderStatus;
    const checkoutStatus = terminalCheckoutStatuses.has(current.checkoutStatus) && !terminalCheckoutStatuses.has(incoming.checkoutStatus) ? current.checkoutStatus : incoming.checkoutStatus;
    return { ...incoming, orderStatus, checkoutStatus };
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
