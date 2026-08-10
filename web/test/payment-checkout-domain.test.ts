import { describe, expect, test } from "bun:test";

import {
    CheckoutRequestCoordinator,
    applyCheckoutTransaction,
    automaticPaymentProvider,
    checkoutPaymentExpiresAt,
    checkoutRequestFailed,
    checkoutRequestFailedForToken,
    checkoutRequestSucceeded,
    checkoutRequestSucceededForToken,
    checkoutDiscountOriginalPrice,
    checkoutTerminalPresentation,
    checkoutRemainingSeconds,
    checkoutServerOffsetMs,
    checkoutSummary,
    createCheckoutLoadState,
    hasCheckoutToken,
    mergeCheckoutResponse,
    mergePaymentCheckout,
    paymentCheckoutTokenFromURL,
    resolveCheckoutProviderSelection,
    restoreActiveCheckoutPayment,
    selectCheckoutProvider,
    shouldContinueCheckoutPolling,
    visibleCheckoutForToken,
    type CheckoutRequestState,
} from "../src/pages/payment/payment-checkout-domain";
import type { MembershipPaymentCheckout, CreditTopupPaymentCheckout } from "../src/services/api/payment";

const membershipCheckout = {
    orderType: "membership",
    orderNumber: "M202608090001",
    orderStatus: "pending",
    checkoutStatus: "active",
    currency: "CNY",
    serverNow: "2026-08-09T04:00:00.000Z",
    expiresAt: "2026-08-09T04:02:00.000Z",
    providers: ["wechat", "alipay"],
    membershipSummary: {
        audience: "team",
        code: "team-year",
        name: "团队版",
        tier: "team",
        billingCycle: "year",
        seats: 3,
        actualPriceCents: 7500,
        originalPriceCents: 10500,
        creditsPerPeriod: 500,
        totalCreditsPerPeriod: 1500,
    },
} satisfies MembershipPaymentCheckout;

describe("payment checkout domain", () => {
    test("extracts only a non-empty capability from an exact payment path", () => {
        expect(paymentCheckoutTokenFromURL("https://hm.kunagent.com/pay/order-token-1", "https://hm.kunagent.com")).toBe("order-token-1");
        expect(paymentCheckoutTokenFromURL("/pay/local-token", "http://127.0.0.1:3000")).toBe("local-token");

        for (const invalidURL of [
            "https://hm.kunagent.com/prefix/pay/order-token-1",
            "https://hm.kunagent.com/pay/%20%20",
            "https://hm.kunagent.com/pay/order-token-1/extra",
            "https://hm.kunagent.com/pay/order-token-1?token=other",
            "https://hm.kunagent.com/pay/order-token-1#other",
        ]) {
            expect(() => paymentCheckoutTokenFromURL(invalidURL, "https://hm.kunagent.com")).toThrow("支付链接返回的结算凭证无效");
        }
    });

    test("auto-starts only one real provider once for a payable checkout", () => {
        const alipayOnly: MembershipPaymentCheckout = { ...membershipCheckout, providers: ["alipay"] };
        const activeTransaction = {
            provider: "alipay",
            status: "pending",
            codeUrl: "https://qr.example.com/alipay-payment",
            expiresAt: membershipCheckout.expiresAt,
        } as const;

        expect(automaticPaymentProvider(alipayOnly, "alipay", false)).toBe("alipay");
        expect(automaticPaymentProvider({ ...membershipCheckout, providers: ["wechat", "alipay"] }, "wechat", false)).toBeNull();
        expect(automaticPaymentProvider(alipayOnly, "alipay", true)).toBeNull();
        expect(automaticPaymentProvider({ ...alipayOnly, activeTransaction }, "alipay", false)).toBeNull();
        expect(automaticPaymentProvider({ ...alipayOnly, checkoutStatus: "expired" }, "alipay", false)).toBeNull();
        expect(automaticPaymentProvider({ ...alipayOnly, orderStatus: "paid", checkoutStatus: "consumed" }, "alipay", false)).toBeNull();
    });

    test("the membership discriminator exposes only the membership summary", () => {
        const summary = checkoutSummary(membershipCheckout);

        expect(summary).toEqual({
            kind: "membership",
            title: "团队版",
            audience: "team",
            billingCycle: "year",
            seats: 3,
            unitPriceCents: 2500,
            actualPriceCents: 7500,
            originalUnitPriceCents: 3500,
            originalPriceCents: 10500,
            discountCents: 3000,
            creditsPerPeriod: 500,
            totalCredits: 1500,
        });
        expect("creditTopupSummary" in membershipCheckout).toBe(false);
    });

    test("a crossed-out original price exists only for a real discount", () => {
        const discounted = checkoutSummary(membershipCheckout);
        expect(checkoutDiscountOriginalPrice(discounted)).toBe(10500);
        if (discounted.kind !== "membership") throw new Error("expected membership summary");
        for (const originalPriceCents of [0, discounted.actualPriceCents, discounted.actualPriceCents - 1]) {
            expect(checkoutDiscountOriginalPrice({ ...discounted, originalPriceCents })).toBeNull();
        }
    });

    test("the countdown uses server time instead of trusting the client clock", () => {
        const receivedAt = Date.parse("2026-08-09T03:59:30.000Z");
        const offset = checkoutServerOffsetMs(membershipCheckout.serverNow, receivedAt);

        expect(offset).toBe(30_000);
        expect(checkoutRemainingSeconds(membershipCheckout.expiresAt, offset, Date.parse("2026-08-09T04:00:30.000Z"))).toBe(60);
        expect(checkoutRemainingSeconds(membershipCheckout.expiresAt, offset, Date.parse("2026-08-09T04:01:31.000Z"))).toBe(0);
    });

    test("a stale poll cannot regress terminal order or checkout state", () => {
        const terminal: MembershipPaymentCheckout = {
            ...membershipCheckout,
            orderStatus: "paid",
            checkoutStatus: "consumed",
        };
        const stale: MembershipPaymentCheckout = {
            ...membershipCheckout,
            orderStatus: "pending",
            checkoutStatus: "active",
        };

        expect(mergePaymentCheckout(terminal, stale)).toEqual(terminal);
        expect(mergePaymentCheckout(membershipCheckout, { ...membershipCheckout, checkoutStatus: "expired" }).checkoutStatus).toBe("expired");
    });

    test("terminal checkout and order facts follow an explicit monotonic order", () => {
        const expired: MembershipPaymentCheckout = {
            ...membershipCheckout,
            orderStatus: "cancelled",
            checkoutStatus: "expired",
        };
        const consumed: MembershipPaymentCheckout = {
            ...membershipCheckout,
            orderStatus: "paid",
            checkoutStatus: "consumed",
        };
        const refunded: MembershipPaymentCheckout = {
            ...consumed,
            orderStatus: "refunded",
        };

        expect(mergePaymentCheckout(consumed, expired)).toEqual(consumed);
        expect(mergePaymentCheckout(expired, consumed)).toEqual(consumed);
        expect(mergePaymentCheckout(refunded, consumed)).toEqual(refunded);

        const stalePayableWithQR: MembershipPaymentCheckout = {
            ...membershipCheckout,
            activeTransaction: {
                provider: "wechat",
                status: "pending",
                codeUrl: "https://qr.example.com/stale-payment",
                expiresAt: membershipCheckout.expiresAt,
            },
        };
        const terminalAfterStaleQR = mergePaymentCheckout(consumed, stalePayableWithQR);
        expect(terminalAfterStaleQR.activeTransaction).toBeUndefined();
        expect(restoreActiveCheckoutPayment(terminalAfterStaleQR)).toBeNull();
    });

    test("a POST transaction atomically outranks an older GET without hiding a newer GET", () => {
        const initial = { checkout: membershipCheckout, revision: 1 } satisfies CheckoutRequestState;
        const transaction = {
            provider: "wechat",
            status: "pending",
            codeUrl: "https://qr.example.com/new-payment",
            expiresAt: membershipCheckout.expiresAt,
        } as const;
        const afterPost = applyCheckoutTransaction(initial, transaction, 3);

        const afterOlderGet = mergeCheckoutResponse(afterPost, membershipCheckout, 2);
        expect(afterOlderGet).toBe(afterPost);
        expect(afterOlderGet.checkout.activeTransaction).toEqual(transaction);

        const afterNewerGet = mergeCheckoutResponse(afterPost, membershipCheckout, 4);
        expect(afterNewerGet.revision).toBe(4);
        expect(afterNewerGet.checkout.activeTransaction).toBeUndefined();
    });

    test("an active transaction restores the selected provider and QR code", () => {
        const withTransaction: MembershipPaymentCheckout = {
            ...membershipCheckout,
            providers: ["wechat"],
            activeTransaction: {
                provider: "alipay",
                status: "pending",
                codeUrl: "https://qr.example.com/alipay-order",
                expiresAt: "2026-08-09T04:02:00.000Z",
            },
        };

        expect(restoreActiveCheckoutPayment(withTransaction)).toEqual({
            provider: "alipay",
            transaction: withTransaction.activeTransaction,
        });
        expect(restoreActiveCheckoutPayment(membershipCheckout)).toBeNull();
    });

    test("credit topups retain a separate minimal checkout contract", () => {
        const topup = {
            orderType: "credit_topup",
            orderNumber: "C202608090001",
            orderStatus: "pending",
            checkoutStatus: "active",
            currency: "CNY",
            serverNow: "2026-08-09T04:00:00.000Z",
            expiresAt: "2026-08-09T04:02:00.000Z",
            providers: ["wechat"],
            creditTopupSummary: {
                actualPriceCents: 990,
                totalMicrocredits: 1250,
            },
        } satisfies CreditTopupPaymentCheckout;

        expect(checkoutSummary(topup)).toEqual({
            kind: "credit_topup",
            title: "积分充值",
            actualPriceCents: 990,
            totalCredits: 1250,
        });
        expect("membershipSummary" in topup).toBe(false);
    });

    test("zero-valued credit topup facts remain valid read-only checkout history", () => {
        const zeroTopup = {
            orderType: "credit_topup",
            orderNumber: "C202608090002",
            orderStatus: "cancelled",
            checkoutStatus: "consumed",
            currency: "CNY",
            serverNow: "2026-08-09T04:00:00.000Z",
            expiresAt: "2026-08-09T04:02:00.000Z",
            providers: [],
            creditTopupSummary: {
                actualPriceCents: 0,
                totalMicrocredits: 0,
            },
        } satisfies CreditTopupPaymentCheckout;

        expect(checkoutSummary(zeroTopup)).toEqual({
            kind: "credit_topup",
            title: "积分充值",
            actualPriceCents: 0,
            totalCredits: 0,
        });
    });

    test("team totals are validated against frozen per-seat facts", () => {
        expect(() =>
            checkoutSummary({
                ...membershipCheckout,
                membershipSummary: {
                    ...membershipCheckout.membershipSummary,
                    totalCreditsPerPeriod: 1499,
                },
            }),
        ).toThrow("团队积分合计与冻结单席积分不一致");

        expect(() =>
            checkoutSummary({
                ...membershipCheckout,
                membershipSummary: {
                    ...membershipCheckout.membershipSummary,
                    actualPriceCents: 7501,
                },
            }),
        ).toThrow("团队实付金额无法还原为单席冻结金额");
    });

    test("a transient refresh failure preserves the loaded summary and active QR", () => {
        const activeCheckout: MembershipPaymentCheckout = {
            ...membershipCheckout,
            activeTransaction: {
                provider: "wechat",
                status: "pending",
                codeUrl: "https://qr.example.com/active-payment",
                expiresAt: "2026-08-09T04:01:30.000Z",
            },
        };
        const loaded = checkoutRequestSucceeded({ checkout: null, initialError: "", refreshError: "", token: "checkout-token-a" }, activeCheckout);
        const failed = checkoutRequestFailed(loaded, "网络暂时不可用");

        expect(failed).toEqual({
            checkout: activeCheckout,
            initialError: "",
            refreshError: "网络暂时不可用",
            token: "checkout-token-a",
        });
        if (!failed.checkout) throw new Error("expected preserved checkout");
        expect(checkoutSummary(failed.checkout)).toMatchObject({ title: "团队版", totalCredits: 1500 });
        expect(failed.checkout.activeTransaction?.codeUrl).toBe("https://qr.example.com/active-payment");
        expect(checkoutRequestSucceeded(failed, activeCheckout).refreshError).toBe("");

        const initialFailure = checkoutRequestFailed({ checkout: null, initialError: "", refreshError: "", token: "checkout-token-a" }, "结算凭证无效");
        expect(initialFailure).toEqual({ checkout: null, initialError: "结算凭证无效", refreshError: "", token: "checkout-token-a" });
    });

    test("a checkout loaded for token A is never visible on token B's first render", () => {
        const state = checkoutRequestSucceeded(createCheckoutLoadState("checkout-token-a"), membershipCheckout);

        expect(visibleCheckoutForToken(state, "checkout-token-a")).toBe(membershipCheckout);
        expect(visibleCheckoutForToken(state, "checkout-token-b")).toBeNull();
        expect(createCheckoutLoadState("checkout-token-b")).toEqual({
            checkout: null,
            initialError: "",
            refreshError: "",
            token: "checkout-token-b",
        });
    });

    test("only a non-whitespace checkout token enables loading and retry", () => {
        expect(hasCheckoutToken("")).toBe(false);
        expect(hasCheckoutToken("   \t")).toBe(false);
        expect(hasCheckoutToken(" checkout-token ")).toBe(true);
    });

    test("a queued token A state update cannot bind its checkout or error to token B", () => {
        const tokenB = createCheckoutLoadState("checkout-token-b");

        expect(checkoutRequestSucceededForToken(tokenB, "checkout-token-a", membershipCheckout)).toBe(tokenB);
        expect(checkoutRequestFailedForToken(tokenB, "checkout-token-a", "token A failed")).toBe(tokenB);
        expect(checkoutRequestSucceededForToken(tokenB, "checkout-token-b", membershipCheckout).checkout).toBe(membershipCheckout);
        expect(checkoutRequestFailedForToken(tokenB, "checkout-token-b", "token B failed").initialError).toBe("token B failed");
    });

    test("an active transaction locks its provider even after the provider leaves the current list", () => {
        const activeCheckout: MembershipPaymentCheckout = {
            ...membershipCheckout,
            providers: ["wechat"],
            activeTransaction: {
                provider: "alipay",
                status: "pending",
                codeUrl: "https://qr.example.com/alipay-payment",
                expiresAt: "2026-08-09T04:01:30.000Z",
            },
        };

        expect(resolveCheckoutProviderSelection(activeCheckout, "wechat")).toEqual({
            options: ["alipay", "wechat"],
            selected: "alipay",
            locked: true,
            error: "",
        });
        expect(selectCheckoutProvider(activeCheckout, "wechat")).toBe("alipay");
        expect(checkoutPaymentExpiresAt(activeCheckout)).toBe("2026-08-09T04:01:30.000Z");

        expect(resolveCheckoutProviderSelection({ ...membershipCheckout, providers: [] }, null)).toEqual({
            options: [],
            selected: null,
            locked: false,
            error: "当前没有可用的支付渠道，请稍后重试或联系管理员。",
        });
    });

    test("every server terminal fact has Chinese copy, clears payment actions, and stops polling", () => {
        const cases: Array<{
            checkout: MembershipPaymentCheckout;
            title: string;
        }> = [
            { checkout: { ...membershipCheckout, orderStatus: "paid", checkoutStatus: "consumed" }, title: "支付成功" },
            { checkout: { ...membershipCheckout, orderStatus: "refunded", checkoutStatus: "consumed" }, title: "订单状态待核对" },
            { checkout: { ...membershipCheckout, orderStatus: "cancelled" }, title: "订单已关闭" },
            { checkout: { ...membershipCheckout, checkoutStatus: "expired" }, title: "收银台已过期" },
            { checkout: { ...membershipCheckout, checkoutStatus: "consumed" }, title: "收银台已关闭" },
        ];

        for (const item of cases) {
            const presentation = checkoutTerminalPresentation(item.checkout);
            expect(presentation?.title).toBe(item.title);
            expect(presentation?.description).not.toMatch(/pending|paid|cancelled|refunded|active|expired|consumed/);
            expect(presentation?.showPaymentControls).toBe(false);
            expect(shouldContinueCheckoutPolling(item.checkout)).toBe(false);
        }

        expect(checkoutTerminalPresentation(membershipCheckout)).toBeNull();
        expect(shouldContinueCheckoutPolling(membershipCheckout)).toBe(true);
    });

    test("expired checkout copy directs the user through the executable order workflow", () => {
        const presentation = checkoutTerminalPresentation({ ...membershipCheckout, checkoutStatus: "expired" });

        expect(presentation?.description).toContain("关闭旧订单后重新下单");
        expect(presentation?.description).toContain("提示需对账");
        expect(presentation?.description).not.toContain("重新获取付款链接");
    });

    test("the request coordinator prevents overlapping GETs and same-tick duplicate POSTs", () => {
        const coordinator = new CheckoutRequestCoordinator();
        coordinator.activate("checkout-token-a");

        const firstLoad = coordinator.beginLoad("checkout-token-a");
        if (!firstLoad) throw new Error("expected load lease");
        expect(coordinator.beginLoad("checkout-token-a")).toBeNull();

        const firstSubmit = coordinator.beginSubmission("checkout-token-a");
        if (!firstSubmit) throw new Error("expected submission lease");
        expect(coordinator.beginSubmission("checkout-token-a")).toBeNull();

        expect(coordinator.completeLoad(firstLoad)).toBe(1);
        expect(coordinator.completeSubmission(firstSubmit)).toBe(2);
    });

    test("a POST that completes before an older GET keeps its QR after the GET returns", () => {
        const coordinator = new CheckoutRequestCoordinator();
        coordinator.activate("checkout-token-a");
        const olderLoad = coordinator.beginLoad("checkout-token-a");
        const submission = coordinator.beginSubmission("checkout-token-a");
        if (!olderLoad || !submission) throw new Error("expected overlapping GET and POST leases");
        expect(coordinator.beginLoad("checkout-token-a")).toBeNull();

        const submissionRevision = coordinator.completeSubmission(submission);
        if (submissionRevision === null) throw new Error("expected accepted submission");
        const transaction = {
            provider: "wechat",
            status: "pending",
            codeUrl: "https://qr.example.com/new-payment",
            expiresAt: membershipCheckout.expiresAt,
        } as const;
        const afterPost = applyCheckoutTransaction({ checkout: membershipCheckout, revision: 0 }, transaction, submissionRevision);

        const loadRevision = coordinator.completeLoad(olderLoad);
        if (loadRevision === null) throw new Error("expected accepted load");
        const afterOlderGet = mergeCheckoutResponse(afterPost, membershipCheckout, loadRevision);
        expect(loadRevision).toBe(1);
        expect(submissionRevision).toBe(2);
        expect(afterOlderGet.checkout.activeTransaction).toEqual(transaction);
    });

    test("token switches and disposal reject old GET and POST responses without touching the new lifecycle", () => {
        const coordinator = new CheckoutRequestCoordinator();
        coordinator.activate("checkout-token-a");
        const oldLoad = coordinator.beginLoad("checkout-token-a");
        const oldSubmit = coordinator.beginSubmission("checkout-token-a");
        if (!oldLoad || !oldSubmit) throw new Error("expected first lifecycle leases");

        coordinator.activate("checkout-token-b");
        const newLoad = coordinator.beginLoad("checkout-token-b");
        if (!newLoad) throw new Error("expected new lifecycle lease");
        expect(coordinator.completeLoad(oldLoad)).toBeNull();
        expect(coordinator.completeSubmission(oldSubmit)).toBeNull();
        expect(coordinator.releaseLoad(oldLoad)).toBe(false);
        expect(coordinator.releaseSubmission(oldSubmit)).toBe(false);
        expect(coordinator.beginLoad("checkout-token-b")).toBeNull();
        expect(coordinator.completeLoad(newLoad)).toBe(1);

        const pending = coordinator.beginLoad("checkout-token-b");
        if (!pending) throw new Error("expected current lifecycle lease");
        coordinator.dispose();
        expect(coordinator.completeLoad(pending)).toBeNull();
        expect(coordinator.beginLoad("checkout-token-b")).toBeNull();
    });

    test("a failed request releases only its own in-flight slot", () => {
        const coordinator = new CheckoutRequestCoordinator();
        coordinator.activate("checkout-token-a");
        const load = coordinator.beginLoad("checkout-token-a");
        const submission = coordinator.beginSubmission("checkout-token-a");
        if (!load || !submission) throw new Error("expected active leases");

        expect(coordinator.releaseLoad(load)).toBe(true);
        expect(coordinator.beginLoad("checkout-token-a")).toBeNull();
        expect(coordinator.releaseSubmission(submission)).toBe(true);
        expect(coordinator.beginLoad("checkout-token-a")).not.toBeNull();
        expect(coordinator.beginSubmission("checkout-token-a")).not.toBeNull();
    });
});
