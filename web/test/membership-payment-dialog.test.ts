import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { MembershipPaymentSetup } from "../src/pages/membership/membership-payment-setup";
import { shouldNavigateFromMembershipPage } from "../src/pages/membership/membership-payment-dialog";
import type { MembershipOrderFactsModel, MembershipOrderLifecycle } from "../src/pages/payment/membership-order-facts-domain";
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

const frozenPersonalFacts = {
    audience: "personal",
    billingCycle: "month",
    creditsPerPeriod: 32_800_000,
    currency: "CNY",
    orderNumber: "M202608100001",
    originalTotalPriceCents: 139_900,
    originalUnitPriceCents: 139_900,
    seats: 1,
    title: "冻结旗舰创作会员",
    totalCredits: 32_800_000,
    totalPriceCents: 129_900,
    unitPriceCents: 129_900,
} satisfies MembershipOrderFactsModel;

const preorderLifecycle = { kind: "preorder" } satisfies MembershipOrderLifecycle;
const frozenReadyLifecycle = { facts: frozenPersonalFacts, kind: "frozen-ready", orderId: "order-1" } satisfies MembershipOrderLifecycle;
const frozenInvalidLifecycle = { error: "订单冻结事实验证失败：订单号为空", kind: "frozen-invalid" } satisfies MembershipOrderLifecycle;

const handlers = {
    onConfirm: () => undefined,
    onClose: () => undefined,
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
        expect(setupSource).toContain('orderLifecycle.kind === "frozen-ready"');
        expect(setupSource).not.toContain("createdOrderNumber");
        expect(setupSource).not.toContain("frozenFactsError || creationError");
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
                creationError: "",
                orderLifecycle: preorderLifecycle,
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
                creationError: "支付渠道暂时不可用",
                orderLifecycle: frozenReadyLifecycle,
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
                creationError: "",
                orderLifecycle: preorderLifecycle,
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
        expect(failureMarkup).toContain("冻结旗舰创作会员");
        expect(failureMarkup).toContain("按月购买");
        expect(failureMarkup).toContain("32.8");
        expect(failureMarkup).toContain("¥1,399");
        expect(failureMarkup).toContain("−¥100");
        expect(failureMarkup).toContain("¥1,299");
        expect(failureMarkup).not.toContain("豪华版");
    });

    test("team configuration stays mounted and disabled while order creation or checkout opening writes", () => {
        const renderWritingTeamSetup = (submitting: boolean, openingCheckout: boolean) =>
            renderToStaticMarkup(
                createElement(MembershipPaymentSetup, {
                    ...handlers,
                    creationError: "",
                    orderLifecycle: preorderLifecycle,
                    openingCheckout,
                    plan: teamPlan,
                    seats: 2,
                    submitting,
                    teamId: "team-1",
                    teamName: "",
                    teams,
                }),
            );

        for (const markup of [renderWritingTeamSetup(true, false), renderWritingTeamSetup(false, true)]) {
            expect(markup).toContain("membership-payment-team-fields");
            expect(markup).toContain("membership-payment-team-select");
            expect(markup).toContain("membership-payment-team-seat-input");
            expect(markup).toContain("确认配置并生成付款码");
            expect(markup).toContain("membership-payment-setup-progress");
            expect(markup).toMatch(/<input[^>]*disabled=""/u);
            expect(markup).toMatch(/<button[^>]*membership-payment-setup-primary[^>]*disabled=""/u);
        }
    });

    test("an invalid created-order identity never falls back to mutable plan facts", () => {
        const markup = renderToStaticMarkup(
            createElement(MembershipPaymentSetup, {
                ...handlers,
                creationError: "",
                orderLifecycle: frozenInvalidLifecycle,
                openingCheckout: false,
                plan: personalPlan,
                seats: 1,
                submitting: false,
                teamId: undefined,
                teamName: "",
                teams: [],
            }),
        );

        expect(markup).toContain("订单冻结事实验证失败：订单号为空");
        expect(markup).toContain("membership-order-facts-skeleton");
        expect(markup).not.toContain("豪华版");
        expect(markup).not.toContain("¥1,199");
        expect(markup).not.toContain("重新打开付款码");
        expect(markup).toContain("关闭付款窗口");
    });

    test("frozen validation and checkout server errors remain independently visible", () => {
        const markup = renderToStaticMarkup(
            createElement(MembershipPaymentSetup, {
                ...handlers,
                creationError: "支付收银台暂时不可用",
                orderLifecycle: frozenInvalidLifecycle,
                openingCheckout: false,
                plan: personalPlan,
                seats: 1,
                submitting: false,
                teamId: undefined,
                teamName: "",
                teams: [],
            }),
        );

        expect(markup).toContain("订单冻结事实验证失败：订单号为空");
        expect(markup).toContain("支付收银台暂时不可用");
        expect(markup).toContain("membership-payment-setup-frozen-error");
        expect(markup).toContain("membership-payment-setup-server-error");
    });

    test("non-discount plans omit original and discount rows while frozen checkout loading keeps the shared shell", () => {
        const nonDiscountMarkup = renderToStaticMarkup(
            createElement(MembershipPaymentSetup, {
                ...handlers,
                creationError: "",
                orderLifecycle: preorderLifecycle,
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
