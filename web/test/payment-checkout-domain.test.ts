import { describe, expect, test } from "bun:test";

import { checkoutDiscountOriginalPrice, checkoutRemainingSeconds, checkoutServerOffsetMs, checkoutSummary, mergePaymentCheckout, restoreActiveCheckoutPayment } from "../src/pages/payment/payment-checkout-domain";
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
    test("the membership discriminator exposes only the membership summary", () => {
        const summary = checkoutSummary(membershipCheckout);

        expect(summary).toEqual({
            kind: "membership",
            title: "团队版",
            audience: "team",
            billingCycle: "year",
            seats: 3,
            actualPriceCents: 7500,
            originalPriceCents: 10500,
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
});
