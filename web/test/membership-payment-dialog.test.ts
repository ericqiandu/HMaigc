import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
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
    test("all membership payment dialog states use the approved 766px reference width", () => {
        const dialogSource = readFileSync(resolve(import.meta.dir, "../src/pages/membership/membership-payment-dialog.tsx"), "utf8");
        const setupSource = readFileSync(resolve(import.meta.dir, "../src/pages/membership/membership-payment-setup.tsx"), "utf8");

        expect(dialogSource).toContain('checkoutToken ? "is-checkout" : "is-setup"');
        expect(dialogSource).toContain("width={766}");
        expect(dialogSource).not.toContain("checkoutToken ? 766 : 880");
        expect(setupSource).not.toContain("membership-payment-dialog-heading");
        expect(setupSource).not.toContain("membership-payment-preview");
        expect(setupSource).not.toContain("membership-payment-product");
    });
    test("Escape never navigates away while the payment dialog owns keyboard dismissal", () => {
        expect(shouldNavigateFromMembershipPage("Escape", true)).toBe(false);
        expect(shouldNavigateFromMembershipPage("Escape", false)).toBe(true);
        expect(shouldNavigateFromMembershipPage("Enter", false)).toBe(false);
    });

    test("creation, failure, and team confirmation retain the shared left order facts", () => {
        const personalCreationMarkup = renderToStaticMarkup(
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
        const failureMarkup = renderToStaticMarkup(
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
        const teamConfirmationMarkup = renderToStaticMarkup(
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

        for (const markup of [personalCreationMarkup, failureMarkup, teamConfirmationMarkup]) {
            expect(markup).toContain("payment-checkout-shell is-dialog membership-payment-setup");
            expect(markup).toContain("payment-checkout-order-surface");
            expect(markup).toContain("payment-checkout-payment-surface");
            expect(markup).toContain("membership-order-facts");
            expect(markup).toContain("membership-order-facts-product");
            expect(markup).toContain("membership-order-facts-details");
        }

        expect(personalCreationMarkup).toContain("正在创建付款订单");
        expect(teamConfirmationMarkup).toContain("开通团队");
        expect(teamConfirmationMarkup).toContain("席位数量");
        expect(teamConfirmationMarkup).toContain("2 席位");
        expect(teamConfirmationMarkup).toContain("确认配置并生成付款码");
        expect(teamConfirmationMarkup).not.toContain("确认购买");
        expect(teamConfirmationMarkup).not.toContain("创建订单并支付");

        expect(failureMarkup).toContain('role="alert"');
        expect(failureMarkup).toContain("订单 M202608100001 已创建");
        expect(failureMarkup).toContain("支付渠道暂时不可用");
        expect(failureMarkup).toContain("不会重复创建订单");
        expect(failureMarkup).toContain("重新打开付款码");
        expect(failureMarkup).toContain("豪华版");
        expect(failureMarkup).toContain("按月购买");
        expect(failureMarkup).toContain("50,500");
        expect(failureMarkup).toContain("¥1,299");
        expect(failureMarkup).toContain("−¥100");
        expect(failureMarkup).toContain("¥1,199");
    });

    test("non-discount plans omit original and discount rows while frozen checkout loading keeps the shared shell", () => {
        const nonDiscountMarkup = renderToStaticMarkup(
            createElement(MembershipPaymentSetup, {
                ...handlers,
                createdOrderNumber: "",
                creationError: "",
                openingCheckout: false,
                plan: { ...personalPlan, originalPriceCents: personalPlan.priceCents },
                seats: 1,
                submitting: true,
                teamId: undefined,
                teamName: "",
                teams: [],
            }),
        );
        const frozenLoadingMarkup = renderToStaticMarkup(createElement(PaymentCheckoutExperience, { mode: "dialog", onExit: () => undefined, token: "checkout-token-1" }));

        expect(nonDiscountMarkup).not.toContain("会员原价");
        expect(nonDiscountMarkup).not.toContain("商品原价");
        expect(nonDiscountMarkup).not.toContain("优惠金额");
        expect(frozenLoadingMarkup).toContain("payment-checkout-shell is-dialog");
        expect(frozenLoadingMarkup).toContain("正在加载订单");
        expect(frozenLoadingMarkup).not.toContain("payment-checkout-header");
    });
});
