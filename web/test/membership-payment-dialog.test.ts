import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { MembershipPaymentSetup } from "../src/pages/membership/membership-payment-setup";
import { shouldNavigateFromMembershipPage } from "../src/pages/membership/membership-payment-dialog";
import { PaymentCheckoutExperience } from "../src/pages/payment/payment-checkout-experience";
import type { MembershipPlan, Team } from "../src/services/api/membership";

const teamPlan = {
    id: "plan-team-flagship-year",
    code: "team-flagship-year",
    name: "至尊版",
    tier: "flagship",
    audience: "team",
    billingCycle: "year",
    priceCents: 1_329_900,
    originalPriceCents: 3_499_900,
    currency: "CNY",
    creditsPerPeriod: 50_500_000_000,
    imageConcurrency: 12,
    videoConcurrency: 6,
    unlimitedTaskQueue: true,
    teamStorageBytes: 1_000_000_000,
    sharedAssetsEnabled: true,
    projectPermissionsEnabled: true,
    invoicingEnabled: true,
    commercialUseEnabled: true,
    topupDiscountBasisPoints: 3_800,
    minSeats: 2,
    maxSeats: 20,
    benefitsJson: "[]",
    benefits: [],
    enabled: true,
    sortOrder: 1,
    createdAt: "2026-08-10T00:00:00.000Z",
    updatedAt: "2026-08-10T00:00:00.000Z",
} satisfies MembershipPlan;

const personalPlan = {
    ...teamPlan,
    id: "plan-creator-flagship-month",
    code: "creator-flagship-month",
    name: "豪华版",
    audience: "personal",
    billingCycle: "month",
    priceCents: 119_900,
    originalPriceCents: 129_900,
    minSeats: 1,
    maxSeats: 1,
} satisfies MembershipPlan;

const teams = [
    {
        id: "team-1",
        ownerUserId: "user-1",
        name: "星河工作室",
        status: "active",
        createdAt: "2026-08-10T00:00:00.000Z",
        updatedAt: "2026-08-10T00:00:00.000Z",
    },
] satisfies Team[];

const handlers = {
    onConfirm: () => undefined,
    onRetry: () => undefined,
    onSeatsChange: () => undefined,
    onTeamIdChange: () => undefined,
    onTeamNameChange: () => undefined,
};

describe("membership payment dialog", () => {
    test("Escape never navigates away while the payment dialog owns keyboard dismissal", () => {
        expect(shouldNavigateFromMembershipPage("Escape", true)).toBe(false);
        expect(shouldNavigateFromMembershipPage("Escape", false)).toBe(true);
        expect(shouldNavigateFromMembershipPage("Enter", false)).toBe(false);
    });

    test("team configuration presents editable purchase facts before the frozen checkout", () => {
        const markup = renderToStaticMarkup(
            createElement(MembershipPaymentSetup, {
                ...handlers,
                createdOrderNumber: "",
                creationError: "",
                openingCheckout: false,
                plan: teamPlan,
                seats: 2,
                submitting: false,
                teamId: "team-1",
                teamName: "",
                teams,
            }),
        );

        expect(markup).toContain("membership-payment-setup");
        expect(markup).toContain("开通团队会员");
        expect(markup).toContain("至尊版");
        expect(markup).toContain("开通团队");
        expect(markup).toContain("席位数量");
        expect(markup).toContain("2 席位");
        expect(markup).toContain("确认配置并生成付款码");
        expect(markup).not.toContain("确认购买");
        expect(markup).not.toContain("创建订单并支付");
    });

    test("personal purchase immediately exposes an in-place order creation state", () => {
        const markup = renderToStaticMarkup(
            createElement(MembershipPaymentSetup, {
                ...handlers,
                createdOrderNumber: "",
                creationError: "",
                openingCheckout: false,
                plan: personalPlan,
                seats: 1,
                submitting: true,
                teamId: undefined,
                teamName: "",
                teams: [],
            }),
        );

        expect(markup).toContain("开通创作会员");
        expect(markup).toContain("正在创建付款订单");
        expect(markup).not.toContain("确认配置并生成付款码");
    });

    test("checkout creation failure keeps the created order fact and a recovery action", () => {
        const markup = renderToStaticMarkup(
            createElement(MembershipPaymentSetup, {
                ...handlers,
                createdOrderNumber: "M202608100001",
                creationError: "支付渠道暂时不可用",
                openingCheckout: false,
                plan: personalPlan,
                seats: 1,
                submitting: false,
                teamId: undefined,
                teamName: "",
                teams: [],
            }),
        );

        expect(markup).toContain('role="alert"');
        expect(markup).toContain("订单 M202608100001 已创建");
        expect(markup).toContain("不会重复创建订单");
        expect(markup).toContain("重新打开付款码");
    });

    test("the direct checkout experience renders its shared dialog shell", () => {
        const markup = renderToStaticMarkup(createElement(PaymentCheckoutExperience, { mode: "dialog", onExit: () => undefined, token: "checkout-token-1" }));

        expect(markup).toContain("payment-checkout-shell is-dialog");
        expect(markup).toContain("正在加载订单");
        expect(markup).not.toContain("payment-checkout-header");
    });
});
