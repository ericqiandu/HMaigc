import { afterAll, describe, expect, test } from "bun:test";
import axios, { type AxiosAdapter, type AxiosResponse, type InternalAxiosRequestConfig } from "axios";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { PaymentReconciliationConfirmation, PaymentTransactionReconciliationAction, executePaymentReconciliation, paymentReconciliationOutcomeLabel, paymentStatusLabel, webhookStatusLabel } from "../src/pages/admin/membership/payment-reconciliation";
import type { AdminPaymentReconciliationResult, PaymentTransaction } from "../src/services/api/payment";

const originalAdapter = axios.defaults.adapter;
let capturedRequest: InternalAxiosRequestConfig | null = null;
const testAdapter: AxiosAdapter = async (config): Promise<AxiosResponse> => {
    capturedRequest = config;
    return { data: { code: 0, data: { transaction: paidTransaction, providerState: "paid" }, msg: "ok" }, status: 200, statusText: "OK", headers: {}, config };
};
axios.defaults.adapter = testAdapter;

afterAll(() => {
    axios.defaults.adapter = originalAdapter;
});

const reviewTransaction: PaymentTransaction = {
    id: "transaction-review-1",
    orderType: "membership",
    orderId: "membership-order-1",
    userId: "user-1",
    provider: "wechat",
    merchantOrderNo: "HM-20260810-0001",
    amountCents: 119900,
    currency: "CNY",
    status: "review_required",
    failureReason: "渠道创建订单响应超时",
    createdAt: "2026-08-10T10:00:00Z",
    updatedAt: "2026-08-10T10:01:00Z",
};

const paidTransaction: PaymentTransaction = {
    ...reviewTransaction,
    status: "paid",
    providerTradeNo: "wechat-trade-1",
    paidAt: "2026-08-10T10:02:00Z",
    updatedAt: "2026-08-10T10:02:00Z",
};

describe("payment admin reconciliation contract", () => {
    test("posts the encoded transaction id to the admin reconciliation endpoint", async () => {
        const { reconcileAdminPaymentTransaction } = await import("../src/services/api/payment");
        const result = await reconcileAdminPaymentTransaction("transaction review 1");

        expect(capturedRequest?.method).toBe("post");
        expect(capturedRequest?.url).toBe("/admin/payments/transactions/transaction%20review%201/reconcile");
        expect(result).toEqual({ transaction: paidTransaction, providerState: "paid" });
    });

    test("renders transaction and webhook review facts as explicit Chinese states", () => {
        expect(paymentStatusLabel("review_required")).toBe("待对账");
        expect(webhookStatusLabel("review_required")).toBe("待复核");
    });

    test("reports every provider reconciliation state without claiming an unknown state was closed", () => {
        expect(paymentReconciliationOutcomeLabel("paid")).toBe("渠道已确认到账并完成履约");
        expect(paymentReconciliationOutcomeLabel("unpaid")).toBe("渠道已确认未到账并完成关单");
        expect(paymentReconciliationOutcomeLabel("not_found")).toBe("渠道未找到付款并完成关单");
        expect(paymentReconciliationOutcomeLabel("unknown")).toBe("渠道状态仍不确定，交易保持待对账");
    });

    test("offers channel reconciliation only for review-required transactions", () => {
        const reviewMarkup = renderToStaticMarkup(createElement(PaymentTransactionReconciliationAction, { transaction: reviewTransaction, loading: false, onRequest: () => undefined }));
        const paidMarkup = renderToStaticMarkup(createElement(PaymentTransactionReconciliationAction, { transaction: { ...reviewTransaction, status: "paid" }, loading: false, onRequest: () => undefined }));

        expect(reviewMarkup).toContain("渠道对账");
        expect(reviewMarkup).toContain('aria-label="对账交易 HM-20260810-0001"');
        expect(paidMarkup).toBe("");
    });

    test("shows the persisted transaction and exact channel-query consequences before confirmation", () => {
        const markup = renderToStaticMarkup(createElement(PaymentReconciliationConfirmation, { transaction: reviewTransaction }));

        expect(markup).toContain("HM-20260810-0001");
        expect(markup).toContain("微信支付");
        expect(markup).toContain("CNY");
        expect(markup).toContain("渠道确认到账才会履约");
        expect(markup).toContain("确认未支付且远端关单成功才会关闭");
        expect(markup).toContain("结果不确定仍保持待对账");
    });

    test("claims a transaction synchronously so duplicate confirmations create one provider request", async () => {
        let resolveProvider: ((result: AdminPaymentReconciliationResult) => void) | undefined;
        const providerResult = new Promise<AdminPaymentReconciliationResult>((resolve) => {
            resolveProvider = resolve;
        });
        const requestedIds: string[] = [];
        const options: Parameters<typeof executePaymentReconciliation>[0] = {
            transaction: reviewTransaction,
            inFlightIds: new Set<string>(),
            reconcile: async (transactionId) => {
                requestedIds.push(transactionId);
                return providerResult;
            },
            replaceTransaction: () => undefined,
            refreshTransactions: async () => null,
            refreshWebhooks: async () => null,
            notifySuccess: () => undefined,
            notifyError: () => undefined,
            notifyRefreshError: () => undefined,
            setBusy: () => undefined,
        };

        const first = executePaymentReconciliation(options);
        const duplicate = await executePaymentReconciliation(options);

        expect(duplicate).toBe(false);
        expect(requestedIds).toEqual(["transaction-review-1"]);
        resolveProvider?.({ transaction: paidTransaction, providerState: "paid" });
        expect(await first).toBe(true);
    });

    test("replaces a reconciled transaction and then refreshes transaction and webhook facts", async () => {
        const events: string[] = [];
        const completed = await executePaymentReconciliation({
            transaction: reviewTransaction,
            inFlightIds: new Set<string>(),
            reconcile: async () => ({ transaction: paidTransaction, providerState: "paid" }),
            replaceTransaction: (transaction) => events.push(`replace:${transaction.status}`),
            refreshTransactions: async () => {
                events.push("refresh:transactions");
                return null;
            },
            refreshWebhooks: async () => {
                events.push("refresh:webhooks");
                return null;
            },
            notifySuccess: (result) => events.push(`success:${result.providerState}`),
            notifyError: (description) => events.push(`error:${description}`),
            notifyRefreshError: (description) => events.push(`refresh-error:${description}`),
            setBusy: (_transactionId, busy) => events.push(`busy:${busy}`),
        });

        expect(completed).toBe(true);
        expect(events).toEqual(["busy:true", "replace:paid", "success:paid", "refresh:transactions", "refresh:webhooks", "busy:false"]);
    });

    test("preserves current facts on failure, shows the server error, and refreshes persisted facts", async () => {
        const visibleTransactions = [reviewTransaction];
        const events: string[] = [];
        const inFlightIds = new Set<string>();
        const completed = await executePaymentReconciliation({
            transaction: reviewTransaction,
            inFlightIds,
            reconcile: async () => {
                throw new Error("渠道查单超时，请稍后重试");
            },
            replaceTransaction: (transaction) => {
                visibleTransactions[0] = transaction;
                events.push("replace");
            },
            refreshTransactions: async () => {
                events.push("refresh:transactions");
                return null;
            },
            refreshWebhooks: async () => {
                events.push("refresh:webhooks");
                return null;
            },
            notifySuccess: () => events.push("success"),
            notifyError: (description) => events.push(`error:${description}`),
            notifyRefreshError: (description) => events.push(`refresh-error:${description}`),
            setBusy: (_transactionId, busy) => events.push(`busy:${busy}`),
        });

        expect(completed).toBe(false);
        expect(visibleTransactions).toEqual([reviewTransaction]);
        expect(events).toEqual(["busy:true", "error:渠道查单超时，请稍后重试", "refresh:transactions", "refresh:webhooks", "busy:false"]);
        expect(inFlightIds.size).toBe(0);
    });

    test("keeps a successful reconciliation authoritative when a secondary refresh fails", async () => {
        const events: string[] = [];
        const completed = await executePaymentReconciliation({
            transaction: reviewTransaction,
            inFlightIds: new Set<string>(),
            reconcile: async () => ({ transaction: paidTransaction, providerState: "paid" }),
            replaceTransaction: (transaction) => events.push(`replace:${transaction.status}`),
            refreshTransactions: async () => {
                events.push("refresh:transactions");
                return "交易列表刷新失败";
            },
            refreshWebhooks: async () => {
                events.push("refresh:webhooks");
                return null;
            },
            notifySuccess: (result) => events.push(`success:${result.providerState}`),
            notifyError: (description) => events.push(`error:${description}`),
            notifyRefreshError: (description) => events.push(`refresh-error:${description}`),
            setBusy: (_transactionId, busy) => events.push(`busy:${busy}`),
        });

        expect(completed).toBe(true);
        expect(events).toEqual(["busy:true", "replace:paid", "success:paid", "refresh:transactions", "refresh:webhooks", "refresh-error:支付交易：交易列表刷新失败", "busy:false"]);
    });

    test("keeps the provider error primary and reports refresh failures separately", async () => {
        const events: string[] = [];
        const completed = await executePaymentReconciliation({
            transaction: reviewTransaction,
            inFlightIds: new Set<string>(),
            reconcile: async () => {
                throw new Error("渠道查单失败");
            },
            replaceTransaction: () => events.push("replace"),
            refreshTransactions: async () => {
                events.push("refresh:transactions");
                return "交易列表刷新失败";
            },
            refreshWebhooks: async () => {
                events.push("refresh:webhooks");
                return "回调列表刷新失败";
            },
            notifySuccess: () => events.push("success"),
            notifyError: (description) => events.push(`error:${description}`),
            notifyRefreshError: (description) => events.push(`refresh-error:${description}`),
            setBusy: (_transactionId, busy) => events.push(`busy:${busy}`),
        });

        expect(completed).toBe(false);
        expect(events).toEqual(["busy:true", "error:渠道查单失败", "refresh:transactions", "refresh:webhooks", "refresh-error:支付交易：交易列表刷新失败；回调审计：回调列表刷新失败", "busy:false"]);
    });
});
