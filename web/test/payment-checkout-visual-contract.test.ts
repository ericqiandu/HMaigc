import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import postcss from "postcss";

import { legalDocumentRoutes } from "../src/constants/legal-documents";
import { CreditTopupOrderFacts } from "../src/pages/payment/credit-topup-order-facts";
import { membershipOrderFactsFromCheckout, membershipOrderFactsFromOrder, membershipOrderFactsFromPlan } from "../src/pages/payment/membership-order-facts-domain";
import { MembershipOrderFacts } from "../src/pages/payment/membership-order-facts";
import { PaymentCheckoutOrderPlaceholder } from "../src/pages/payment/payment-checkout-order-placeholder";
import { PaymentCheckoutInitialError, PaymentCheckoutShell } from "../src/pages/payment/payment-checkout-shell";
import { PaymentQrPanel } from "../src/pages/payment/payment-qr-panel";
import type { MembershipOrder, MembershipPlan } from "../src/services/api/membership";
import type { CreditTopupPaymentCheckout, MembershipPaymentCheckout } from "../src/services/api/payment";

const root = resolve(import.meta.dir, "..");

const personalCheckout = {
    orderType: "membership",
    orderNumber: "M202608100001",
    orderStatus: "pending",
    checkoutStatus: "active",
    currency: "CNY",
    serverNow: "2026-08-10T04:00:00.000Z",
    expiresAt: "2026-08-10T04:15:00.000Z",
    providers: ["wechat", "alipay"],
    membershipSummary: {
        audience: "personal",
        code: "creator-month",
        name: "豪华版VIP",
        tier: "premium",
        billingCycle: "month",
        seats: 1,
        actualPriceCents: 119_900,
        originalPriceCents: 129_900,
        creditsPerPeriod: 32_800_000_000,
        totalCreditsPerPeriod: 32_800_000_000,
    },
} satisfies MembershipPaymentCheckout;

const teamCheckout = {
    ...personalCheckout,
    orderNumber: "M202608100002",
    membershipSummary: {
        ...personalCheckout.membershipSummary,
        audience: "team",
        code: "team-year",
        name: "团队豪华版VIP",
        billingCycle: "year",
        seats: 2,
        actualPriceCents: 1_599_800,
        originalPriceCents: 3_399_800,
        creditsPerPeriod: 32_800_000_000,
        totalCreditsPerPeriod: 65_600_000_000,
    },
} satisfies MembershipPaymentCheckout;

const topupCheckout = {
    orderType: "credit_topup",
    orderNumber: "C202608100001",
    orderStatus: "pending",
    checkoutStatus: "active",
    currency: "CNY",
    serverNow: "2026-08-10T04:00:00.000Z",
    expiresAt: "2026-08-10T04:15:00.000Z",
    providers: ["alipay"],
    creditTopupSummary: {
        actualPriceCents: 990,
        totalMicrocredits: 1_250_000_000,
    },
} satisfies CreditTopupPaymentCheckout;

const personalPlan = {
    id: "plan-standard-year",
    code: "creator-standard-year",
    name: "标准版",
    tier: "standard",
    audience: "personal",
    billingCycle: "year",
    priceCents: 119_900,
    originalPriceCents: 129_900,
    currency: "CNY",
    creditsPerPeriod: 32_800_000_000,
    imageConcurrency: 2,
    videoConcurrency: 1,
    unlimitedTaskQueue: false,
    teamStorageBytes: 0,
    sharedAssetsEnabled: false,
    projectPermissionsEnabled: false,
    invoicingEnabled: false,
    commercialUseEnabled: false,
    topupDiscountBasisPoints: 0,
    minSeats: 1,
    maxSeats: 1,
    benefitsJson: "[]",
    benefits: [],
    enabled: true,
    sortOrder: 1,
    createdAt: "2026-08-10T00:00:00.000Z",
    updatedAt: "2026-08-10T00:00:00.000Z",
} satisfies MembershipPlan;

const frozenTeamPlan = {
    ...personalPlan,
    id: "plan-team-standard-year",
    code: "team-standard-year",
    name: "团队标准版",
    audience: "team",
    minSeats: 2,
    maxSeats: 20,
} satisfies MembershipPlan;

function frozenOrderFromPlan(plan: MembershipPlan, seats: number): MembershipOrder {
    return {
        id: `order-${plan.id}`,
        orderNumber: `M-${plan.id}`,
        userId: "user-1",
        teamId: plan.audience === "team" ? "team-1" : undefined,
        planId: plan.id,
        seats,
        unitPriceCents: plan.priceCents,
        totalPriceCents: plan.priceCents * seats,
        currency: plan.currency,
        status: "pending",
        planSnapshotJson: JSON.stringify(plan),
        paymentProvider: "",
        providerTradeNo: "",
        createdAt: "2026-08-10T00:00:00.000Z",
        updatedAt: "2026-08-10T00:00:00.000Z",
    };
}

const handlers = {
    onProviderChange: () => undefined,
    onRetry: () => undefined,
    onSubmit: () => undefined,
    onReturn: () => undefined,
};

function cssProperty(source: string, selector: string, property: string): string {
    let value = "";
    postcss.parse(source).walkRules((rule) => {
        if (!rule.selectors.includes(selector)) return;
        rule.walkDecls(property, (declaration) => {
            value = declaration.value;
        });
    });
    expect(value).not.toBe("");
    return value;
}

function rootCssProperty(source: string, selector: string, property: string): string {
    let value = "";
    postcss.parse(source).walkRules((rule) => {
        if (rule.parent?.type !== "root" || !rule.selectors.includes(selector)) return;
        rule.walkDecls(property, (declaration) => {
            value = declaration.value;
        });
    });
    expect(value).not.toBe("");
    return value;
}

function cssPropertyOwners(source: string, property: string): string[] {
    const owners: string[] = [];
    postcss.parse(source).walkRules((rule) => {
        rule.walkDecls(property, () => owners.push(...rule.selectors));
    });
    return owners;
}

function hexLuminance(value: string): number {
    const match = /^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i.exec(value);
    if (!match) throw new Error(`expected six-digit hex color, received ${value}`);
    const channels = match.slice(1).map((channel) => {
        const component = Number.parseInt(channel, 16) / 255;
        return component <= 0.04045 ? component / 12.92 : ((component + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

function contrastRatio(foreground: string, background: string): number {
    const foregroundLuminance = hexLuminance(foreground);
    const backgroundLuminance = hexLuminance(background);
    return (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) / (Math.min(foregroundLuminance, backgroundLuminance) + 0.05);
}

describe("membership checkout presentation", () => {
    test("an order snapshot rejects either missing immutable identity before checkout can open", () => {
        const frozenOrder = frozenOrderFromPlan(personalPlan, 1);

        expect(() => membershipOrderFactsFromOrder({ ...frozenOrder, id: "" })).toThrow("订单 ID 为空");
        expect(() => membershipOrderFactsFromOrder({ ...frozenOrder, orderNumber: "" })).toThrow("订单号为空");
    });

    test("paid frozen membership orders reject zero unit or total prices", () => {
        const zeroPricePlan = { ...personalPlan, priceCents: 0 };
        expect(() => membershipOrderFactsFromOrder({ ...frozenOrderFromPlan(zeroPricePlan, 1), totalPriceCents: 1 })).toThrow("会员实付单价必须是安全的正整数");

        const frozenOrder = frozenOrderFromPlan(personalPlan, 1);
        expect(() => membershipOrderFactsFromOrder({ ...frozenOrder, totalPriceCents: 0 })).toThrow("会员实付总价必须是安全的正整数");
    });

    test("team frozen orders reject one seat and seats outside verified snapshot bounds", () => {
        expect(() => membershipOrderFactsFromOrder(frozenOrderFromPlan(frozenTeamPlan, 1))).toThrow("团队会员席位必须至少为 2");

        const minimumThree = { ...frozenTeamPlan, minSeats: 3 };
        expect(() => membershipOrderFactsFromOrder(frozenOrderFromPlan(minimumThree, 2))).toThrow("团队会员席位超出冻结范围");

        const maximumThree = { ...frozenTeamPlan, maxSeats: 3 };
        expect(() => membershipOrderFactsFromOrder(frozenOrderFromPlan(maximumThree, 4))).toThrow("团队会员席位超出冻结范围");
    });

    test("team frozen orders reject missing, non-positive, or inverted snapshot bounds", () => {
        const validOrder = frozenOrderFromPlan(frozenTeamPlan, 2);
        expect(() => membershipOrderFactsFromOrder({ ...validOrder, planSnapshotJson: JSON.stringify({ ...frozenTeamPlan, minSeats: undefined }) })).toThrow("订单冻结套餐 minSeats 无效");
        expect(() => membershipOrderFactsFromOrder({ ...validOrder, planSnapshotJson: JSON.stringify({ ...frozenTeamPlan, maxSeats: undefined }) })).toThrow("订单冻结套餐 maxSeats 无效");
        expect(() => membershipOrderFactsFromOrder({ ...validOrder, planSnapshotJson: JSON.stringify({ ...frozenTeamPlan, minSeats: 0 }) })).toThrow("团队会员冻结席位范围无效");
        expect(() => membershipOrderFactsFromOrder({ ...validOrder, planSnapshotJson: JSON.stringify({ ...frozenTeamPlan, minSeats: 5, maxSeats: 2 }) })).toThrow("团队会员冻结席位范围无效");
    });

    test("a frozen personal order accepts zero original price and keeps its payable facts", () => {
        const facts = membershipOrderFactsFromOrder(frozenOrderFromPlan({ ...personalPlan, originalPriceCents: 0 }, 1));
        const markup = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts }));

        expect(facts).toMatchObject({ originalTotalPriceCents: 0, originalUnitPriceCents: 0, totalPriceCents: 119_900, unitPriceCents: 119_900 });
        expect(markup).toContain("membership-order-facts");
        expect(markup).toContain("应付金额");
        expect(markup).toContain("¥1,199");
        expect(markup).not.toContain("会员原价");
        expect(markup).not.toContain("商品原价");
        expect(markup).not.toContain("优惠金额");
        expect(markup).not.toContain("<del");
    });

    test("a frozen team order accepts original price below actual and keeps its payable facts", () => {
        const facts = membershipOrderFactsFromOrder(frozenOrderFromPlan({ ...frozenTeamPlan, originalPriceCents: 100_000 }, 2));
        const markup = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts }));

        expect(facts).toMatchObject({ originalTotalPriceCents: 200_000, originalUnitPriceCents: 100_000, totalPriceCents: 239_800, unitPriceCents: 119_900 });
        expect(markup).toContain("membership-order-facts");
        expect(markup).toContain("2 席位");
        expect(markup).toContain("应付金额");
        expect(markup).toContain("¥2,398");
        expect(markup).not.toContain("会员原价");
        expect(markup).not.toContain("商品原价");
        expect(markup).not.toContain("优惠金额");
        expect(markup).not.toContain("<del");
    });

    test("membership checkout facts reject blank order and summary identities", () => {
        expect(() => membershipOrderFactsFromCheckout({ ...personalCheckout, orderNumber: "  " })).toThrow("订单号为空");
        expect(() => membershipOrderFactsFromCheckout({ ...personalCheckout, membershipSummary: { ...personalCheckout.membershipSummary, name: " " } })).toThrow("会员套餐名称为空");
        expect(() => membershipOrderFactsFromCheckout({ ...personalCheckout, membershipSummary: { ...personalCheckout.membershipSummary, code: " " } })).toThrow("会员套餐编码为空");
        expect(() => membershipOrderFactsFromCheckout({ ...personalCheckout, membershipSummary: { ...personalCheckout.membershipSummary, tier: " " } })).toThrow("会员套餐层级为空");
    });

    test("plan previews and frozen membership orders share one facts model and structural owner", () => {
        const preview = membershipOrderFactsFromPlan(personalPlan, 1);
        const frozen = membershipOrderFactsFromCheckout(personalCheckout);

        expect(preview).toEqual({
            audience: "personal",
            billingCycle: "year",
            creditsPerPeriod: personalPlan.creditsPerPeriod,
            currency: "CNY",
            orderNumber: "",
            originalTotalPriceCents: personalPlan.originalPriceCents,
            originalUnitPriceCents: personalPlan.originalPriceCents,
            seats: 1,
            title: "标准版",
            totalCredits: personalPlan.creditsPerPeriod,
            totalPriceCents: personalPlan.priceCents,
            unitPriceCents: personalPlan.priceCents,
        });
        expect(frozen.orderNumber).toBe("M202608100001");

        const previewMarkup = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts: preview }));
        const frozenMarkup = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts: frozen }));
        for (const markup of [previewMarkup, frozenMarkup]) {
            expect(markup).toContain("membership-order-facts");
            expect(markup).toContain("membership-order-facts-heading");
            expect(markup).toContain("membership-order-facts-product");
            expect(markup).toContain("membership-order-facts-details");
            expect(markup).toContain("商品信息");
            expect(markup).toContain("订单明细");
            expect(markup).toContain("本次为一次性购买，到期不自动续费。");
        }
    });

    test("membership facts reveal truthful discounts and annual monthly equivalents only when applicable", () => {
        const discountedMarkup = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts: membershipOrderFactsFromPlan(personalPlan, 1) }));
        const nonDiscountedMarkup = renderToStaticMarkup(
            createElement(MembershipOrderFacts, {
                facts: membershipOrderFactsFromPlan({ ...personalPlan, originalPriceCents: personalPlan.priceCents }, 1),
            }),
        );
        const monthlyMarkup = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts: membershipOrderFactsFromCheckout(personalCheckout) }));

        expect(discountedMarkup).toContain("会员原价");
        expect(discountedMarkup).toContain("商品原价");
        expect(discountedMarkup).toContain("优惠金额");
        expect(discountedMarkup).toMatch(/membership-order-facts-original-unit-price[^>]*>会员原价 <del[^>]*>¥1,299<\/del>/);
        expect(discountedMarkup).toMatch(/<dt[^>]*>商品原价<\/dt><dd[^>]*>¥1,299<\/dd>/);
        expect(discountedMarkup).toMatch(/<dt[^>]*>优惠金额<\/dt><dd[^>]*>−¥100<\/dd>/);
        expect(discountedMarkup).toContain("¥99.92/月");
        expect(nonDiscountedMarkup).not.toContain("会员原价");
        expect(nonDiscountedMarkup).not.toContain("商品原价");
        expect(nonDiscountedMarkup).not.toContain("优惠金额");
        expect(monthlyMarkup).not.toContain("每月约");
    });

    test("membership and credit topup fact owners reject the other order type", () => {
        expect(() => membershipOrderFactsFromCheckout(topupCheckout)).toThrow("积分充值订单不能映射为会员订单事实");
        expect(() => renderToStaticMarkup(createElement(CreditTopupOrderFacts, { checkout: personalCheckout }))).toThrow("会员订单不能使用积分充值订单事实展示");
    });

    test("personal, team, and topup summaries expose only their frozen facts", () => {
        const personal = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts: membershipOrderFactsFromCheckout(personalCheckout) }));
        const team = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts: membershipOrderFactsFromCheckout(teamCheckout) }));
        const topup = renderToStaticMarkup(createElement(CreditTopupOrderFacts, { checkout: topupCheckout }));

        expect(personal).toContain("开通创作会员");
        expect(personal).toContain("开通创作会员「豪华版VIP 按月购买」 32,800 积分");
        expect(personal).toContain("豪华版VIP");
        expect(personal).toContain("按月购买");
        expect(personal).toContain("32,800");
        expect(personal).toContain("1,199");
        expect(personal).toContain("优惠金额");
        expect(personal).toContain("到期不自动续费");
        expect(personal).not.toContain("席位数量");

        expect(team).toContain("开通团队会员");
        expect(team).toContain("开通团队会员「团队豪华版VIP 按年购买」 65,600 积分");
        expect(team).toContain("团队豪华版VIP");
        expect(team).toContain("席位数量");
        expect(team).toContain("2 席位");
        expect(team).toContain("单席积分");
        expect(team).toContain("32,800");
        expect(team).toContain("团队积分合计");
        expect(team).toContain("65,600");
        expect(team).toContain("7,999");
        expect(team).toContain("15,998");
        expect(team).not.toContain("团队名称");

        expect(topup).toContain("积分充值");
        expect(topup).toContain("充值积分");
        expect(topup).toContain("1,250");
        expect(topup).toContain("9.9");
        expect(topup).not.toContain("到期不自动续费");
    });

    test("checkout original prices at or below actual stay payable without discount markup", () => {
        for (const originalPriceCents of [0, personalCheckout.membershipSummary.actualPriceCents - 1, personalCheckout.membershipSummary.actualPriceCents]) {
            const facts = membershipOrderFactsFromCheckout({
                ...personalCheckout,
                membershipSummary: { ...personalCheckout.membershipSummary, originalPriceCents },
            });
            const markup = renderToStaticMarkup(createElement(MembershipOrderFacts, { facts }));

            expect(facts).toMatchObject({ originalTotalPriceCents: originalPriceCents, totalPriceCents: 119_900, unitPriceCents: 119_900 });
            expect(markup).toContain("membership-order-facts");
            expect(markup).toContain("应付金额");
            expect(markup).toContain("¥1,199");
            expect(markup).not.toContain("会员原价");
            expect(markup).not.toContain("商品原价");
            expect(markup).not.toContain("优惠金额");
            expect(markup).not.toContain("<del");
        }
    });

    test("frozen non-CNY orders use their own currency instead of a hard-coded yuan symbol", () => {
        const markup = renderToStaticMarkup(
            createElement(MembershipOrderFacts, {
                facts: membershipOrderFactsFromCheckout({
                    ...personalCheckout,
                    currency: "USD",
                }),
            }),
        );

        expect(markup).toContain("$1,199");
        expect(markup).not.toContain("¥");
    });

    test("the shell owns the stable two-surface checkout structure", () => {
        const markup = renderToStaticMarkup(
            createElement(PaymentCheckoutShell, {
                busy: false,
                mode: "page",
                onBack: () => undefined,
                summary: createElement(MembershipOrderFacts, { facts: membershipOrderFactsFromCheckout(personalCheckout) }),
                payment: createElement("p", { className: "payment-test-slot" }, "支付面板"),
            }),
        );

        expect(markup).toContain("payment-checkout-page");
        expect(markup).toContain("payment-checkout-shell");
        expect(markup).toContain("payment-checkout-shell is-page");
        expect(markup).toContain("payment-checkout-order-surface");
        expect(markup).toContain("payment-checkout-payment-surface");
        expect(markup).toContain("安全收银台");
        expect(markup).toContain('aria-busy="false"');

        const dialogMarkup = renderToStaticMarkup(
            createElement(PaymentCheckoutShell, {
                busy: false,
                mode: "dialog",
                onBack: () => undefined,
                summary: createElement(MembershipOrderFacts, { facts: membershipOrderFactsFromCheckout(personalCheckout) }),
                payment: createElement("p", { className: "payment-test-slot" }, "支付面板"),
            }),
        );
        expect(dialogMarkup).toContain("payment-checkout-shell is-dialog");
        expect(dialogMarkup).not.toContain("payment-checkout-header");
    });

    test("the neutral order placeholder distinguishes loading from failed presentation", () => {
        const loadingMarkup = renderToStaticMarkup(createElement(PaymentCheckoutOrderPlaceholder, { presentation: "loading" }));
        const failedMarkup = renderToStaticMarkup(createElement(PaymentCheckoutOrderPlaceholder, { presentation: "failed" }));

        expect(loadingMarkup).toContain("payment-checkout-order-placeholder is-loading");
        expect(loadingMarkup).toContain('aria-busy="true"');
        expect(loadingMarkup).toContain("正在识别订单");
        expect(loadingMarkup).toContain("请稍候");

        expect(failedMarkup).toContain("payment-checkout-order-placeholder is-failed");
        expect(failedMarkup).toContain("订单信息暂不可用");
        expect(failedMarkup).toContain("未能读取订单类型与金额，请查看右侧错误并重试。");
        expect(failedMarkup).not.toContain('aria-busy="true"');
        expect(failedMarkup).not.toContain("正在识别订单");
        expect(failedMarkup).not.toContain("请稍候");
        expect(failedMarkup).not.toContain("membership-order-facts");
        expect(failedMarkup).not.toContain("credit-topup-order-facts");
        expect(failedMarkup).not.toContain("到期不自动续费");
    });

    test("a checkout without a token exposes no fake retry action", () => {
        const markup = renderToStaticMarkup(
            createElement(PaymentCheckoutInitialError, {
                canRetry: false,
                message: "支付链接缺少结算凭证",
                onRetry: () => undefined,
            }),
        );

        expect(markup).toContain("支付链接缺少结算凭证");
        expect(markup).toContain('role="alert"');
        expect(markup).not.toContain("重新加载");
        expect(markup).not.toContain("<button");
    });

    test("an active QR keeps its removed provider locked and preserves refresh errors", () => {
        const activeCheckout: MembershipPaymentCheckout = {
            ...teamCheckout,
            providers: ["wechat"],
            activeTransaction: {
                provider: "alipay",
                status: "pending",
                codeUrl: "https://qr.example.com/private-capability",
                expiresAt: "2026-08-10T04:01:05.000Z",
            },
        };
        const markup = renderToStaticMarkup(
            createElement(PaymentQrPanel, {
                ...handlers,
                checkout: activeCheckout,
                checkoutSecondsLeft: 600,
                paymentSecondsLeft: 65,
                provider: "alipay",
                refreshError: "网络暂时不可用，正在保留当前付款码",
                submissionError: "",
                submitting: false,
            }),
        );

        expect(markup).toContain("payment-checkout-qr-panel");
        expect(markup).toContain("支付宝付款二维码");
        expect(markup).not.toContain("支付宝付款二维码 https://");
        expect(markup).not.toContain("private-capability");
        expect(markup).toContain("请使用支付宝扫码支付");
        expect(markup).toContain("01:05");
        expect(markup).toContain('aria-live="off"');
        expect(markup).toContain("网络暂时不可用，正在保留当前付款码");
        expect(markup).not.toContain("payment-checkout-provider-fieldset");
        expect(markup).not.toContain("payment-checkout-provider-input");
        expect(markup).toContain("开通即代表同意");
        expect(markup).toContain("《HMaigc会员服务协议》");
        expect(markup).toContain(`href="${legalDocumentRoutes.membershipAgreement}"`);
        expect(markup).toContain('target="_blank"');
        expect(markup).toContain('rel="noopener noreferrer"');
    });

    test("credit topup QR never presents the membership agreement", () => {
        const markup = renderToStaticMarkup(
            createElement(PaymentQrPanel, {
                ...handlers,
                checkout: {
                    ...topupCheckout,
                    activeTransaction: {
                        provider: "alipay",
                        status: "pending",
                        codeUrl: "https://qr.example.com/credit-topup",
                        expiresAt: "2026-08-10T04:01:05.000Z",
                    },
                },
                checkoutSecondsLeft: 600,
                paymentSecondsLeft: 65,
                provider: "alipay",
                refreshError: "",
                submissionError: "",
                submitting: false,
            }),
        );

        expect(markup).toContain("支付宝付款二维码");
        expect(markup).not.toContain("HMaigc会员服务协议");
        expect(markup).not.toContain(legalDocumentRoutes.membershipAgreement);
    });

    test("a transaction creation failure preserves the selected provider and an explicit retry action", () => {
        const markup = renderToStaticMarkup(
            createElement(PaymentQrPanel, {
                ...handlers,
                checkout: personalCheckout,
                checkoutSecondsLeft: 600,
                paymentSecondsLeft: 600,
                provider: "wechat",
                refreshError: "",
                submissionError: "支付渠道连接超时，请确认后重试",
                submitting: false,
            }),
        );

        expect(markup).toContain("支付渠道连接超时，请确认后重试");
        expect(markup).toContain('role="alert"');
        expect(markup).toMatch(/<input[^>]*type="radio"[^>]*checked[^>]*value="wechat"/);
        expect(markup).toContain("生成付款码");
    });

    test("missing providers and terminal facts render explicit accessible states", () => {
        const unavailable = renderToStaticMarkup(
            createElement(PaymentQrPanel, {
                ...handlers,
                checkout: { ...personalCheckout, providers: [] },
                checkoutSecondsLeft: 600,
                paymentSecondsLeft: 600,
                provider: null,
                refreshError: "",
                submissionError: "",
                submitting: false,
            }),
        );
        expect(unavailable).toContain("当前没有可用的支付渠道");
        expect(unavailable).toContain('role="alert"');
        expect(unavailable).not.toContain("生成付款码");

        const terminal = renderToStaticMarkup(
            createElement(PaymentQrPanel, {
                ...handlers,
                checkout: { ...personalCheckout, orderStatus: "paid", checkoutStatus: "consumed" },
                checkoutSecondsLeft: 0,
                paymentSecondsLeft: 0,
                provider: null,
                refreshError: "",
                submissionError: "",
                submitting: false,
            }),
        );
        expect(terminal).toContain("支付成功");
        expect(terminal).toContain("lucide-check");
        expect(terminal).toContain('aria-live="polite"');
        expect(terminal).not.toContain("付款二维码");
        expect(terminal).not.toContain("pending");
        expect(terminal).not.toContain("consumed");

        const warning = renderToStaticMarkup(
            createElement(PaymentQrPanel, {
                ...handlers,
                checkout: { ...personalCheckout, orderStatus: "cancelled" },
                checkoutSecondsLeft: 0,
                paymentSecondsLeft: 0,
                provider: null,
                refreshError: "",
                submissionError: "",
                submitting: false,
            }),
        );
        expect(warning).toContain("lucide-circle-x");
        expect(warning).not.toContain("lucide-check");

        const reviewRequired = renderToStaticMarkup(
            createElement(PaymentQrPanel, {
                ...handlers,
                checkout: { ...personalCheckout, orderStatus: "refunded", checkoutStatus: "consumed" },
                checkoutSecondsLeft: 0,
                paymentSecondsLeft: 0,
                provider: null,
                refreshError: "",
                submissionError: "",
                submitting: false,
            }),
        );
        expect(reviewRequired).toContain("lucide-circle-x");
        expect(reviewRequired).not.toContain("lucide-check");
    });
});

describe("checkout implementation boundaries", () => {
    test("the QR and payment CSS consume invariant and semantic color tokens", () => {
        const qrPanel = readFileSync(resolve(root, "src/pages/payment/payment-qr-panel.tsx"), "utf8");
        const css = readFileSync(resolve(root, "src/pages/payment/payment-checkout.css"), "utf8");
        const tokens = readFileSync(resolve(root, "src/styles/design-tokens.css"), "utf8");

        expect(qrPanel).not.toMatch(/#[\da-f]{3,8}\b|rgba?\(/i);
        expect(qrPanel).toContain('bgColor="var(--qr-background)"');
        expect(qrPanel).toContain('color="var(--qr-foreground)"');
        expect(qrPanel).toContain("marginSize={4}");
        expect(qrPanel).toContain('type="svg"');
        expect(cssProperty(tokens, ":root", "--qr-background")).toBe("#ffffff");
        expect(cssProperty(tokens, ":root", "--qr-foreground")).toBe("#000000");
        expect(cssPropertyOwners(tokens, "--qr-background")).toEqual([":root"]);
        expect(cssPropertyOwners(tokens, "--qr-foreground")).toEqual([":root"]);
        expect(contrastRatio(cssProperty(tokens, ":root", "--text-on-brand"), cssProperty(tokens, ":root", "--brand-active"))).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(cssProperty(tokens, ".dark", "--text-on-brand"), cssProperty(tokens, ".dark", "--brand-active"))).toBeGreaterThanOrEqual(4.5);
        expect(css).not.toMatch(/#[\da-f]{3,8}\b|rgba?\(/i);
        expect(css).toContain("var(--bg-secondary)");
        expect(css).toContain("var(--bg-tertiary)");
        expect(rootCssProperty(css, ".payment-checkout-shell", "grid-template-columns")).toBe("minmax(0, 425px) minmax(0, 341px)");
        expect(rootCssProperty(css, ".payment-checkout-shell", "width")).toBe("min(calc(100% - 48px), 766px)");
        expect(rootCssProperty(css, ".payment-checkout-shell.is-dialog", "grid-template-columns")).toBe("minmax(0, 425px) minmax(0, 341px)");
        expect(rootCssProperty(css, ".payment-checkout-shell.is-dialog", "max-width")).toBe("100%");
        expect(css).toContain("grid-template-columns: minmax(0, 425fr) minmax(0, 341fr)");
        expect(css).toContain("@media (max-width: 767px)");
        expect(cssProperty(css, ".payment-checkout-page", "height")).toBe("100%");
        expect(cssProperty(css, ".payment-checkout-page", "overflow-y")).toBe("auto");
        expect(rootCssProperty(css, ".payment-checkout-shell", "overflow")).toBe("visible");
        expect(cssProperty(css, ".payment-checkout-payment-surface", "border-radius")).toBe("0");
        expect(css).not.toContain("calc(var(--radius-md) - 1px)");
        expect(cssProperty(css, ".payment-checkout-qr-code", "width")).toBe("128px");
        expect(cssProperty(css, ".payment-checkout-qr-code", "height")).toBe("128px");
        expect(cssProperty(css, ".payment-checkout-qr-code", "padding")).toBe("0");
        expect(cssProperty(css, ".payment-checkout-qr-code", "background")).toBe("var(--qr-background)");
        expect(cssProperty(css, ".payment-checkout-qr-image", "width")).toBe("112px");
        expect(cssProperty(css, ".payment-checkout-qr-image", "height")).toBe("112px");
        expect(cssProperty(css, ".payment-checkout-qr-image", "border")).toBe("0");
        expect(tokens).toContain("--status-warning-text: #a65300");
        expect(tokens).toContain("--status-warning-text: #ff9f0a");
        expect(cssProperty(css, ".payment-checkout-countdown", "color")).toBe("var(--status-warning-text)");
        expect(css).toContain("width: 112px !important");
        expect(css).toContain("height: 112px !important");
        expect(cssProperty(css, ".payment-checkout-provider-check", "opacity")).toBe("0");
        expect(cssProperty(css, ".payment-checkout-provider.is-active .payment-checkout-provider-check", "opacity")).toBe("1");
        expect(cssProperty(css, ".membership-checkout-total-price", "color")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-provider.is-active .payment-checkout-provider-icon", "color")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-provider.is-active .payment-checkout-provider-check", "color")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-action", "background")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-action:hover:not(:disabled)", "background")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-inline-action", "color")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-inline-action", "min-width")).toBe("44px");

        expect(qrPanel).toContain("size={112}");
        expect(qrPanel).toContain("legalDocumentRoutes.membershipAgreement");
        expect(qrPanel).not.toContain("useSiteSettings");
        expect(qrPanel).not.toContain("membershipAgreementPublished");

        for (const selector of [
            ".membership-checkout-order-number",
            ".membership-checkout-product-meta",
            ".membership-checkout-detail-label",
            ".membership-checkout-original-price",
            ".membership-checkout-discount",
            ".membership-checkout-renewal-note",
            ".payment-checkout-security",
            ".payment-checkout-qr-intro",
            ".payment-checkout-agreement",
            ".payment-checkout-clock-state-copy",
            ".payment-checkout-terminal-description",
        ]) {
            expect(cssProperty(css, selector, "color")).toBe("var(--text-secondary)");
        }
    });
});
