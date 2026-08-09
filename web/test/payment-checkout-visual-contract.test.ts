import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import postcss from "postcss";

import { MembershipCheckoutSummary } from "../src/pages/payment/membership-checkout-summary";
import { PaymentCheckoutInitialError, PaymentCheckoutShell } from "../src/pages/payment/payment-checkout-shell";
import { PaymentQrPanel } from "../src/pages/payment/payment-qr-panel";
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
    test("personal, team, and topup summaries expose only their frozen facts", () => {
        const personal = renderToStaticMarkup(createElement(MembershipCheckoutSummary, { checkout: personalCheckout }));
        const team = renderToStaticMarkup(createElement(MembershipCheckoutSummary, { checkout: teamCheckout }));
        const topup = renderToStaticMarkup(createElement(MembershipCheckoutSummary, { checkout: topupCheckout }));

        expect(personal).toContain("开通创作会员");
        expect(personal).toContain("豪华版VIP");
        expect(personal).toContain("按月购买");
        expect(personal).toContain("32,800");
        expect(personal).toContain("1,199");
        expect(personal).toContain("优惠金额");
        expect(personal).toContain("到期不自动续费");
        expect(personal).not.toContain("席位数量");

        expect(team).toContain("开通团队会员");
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

    test("a non-discounted membership summary hides original-price and discount rows", () => {
        const markup = renderToStaticMarkup(
            createElement(MembershipCheckoutSummary, {
                checkout: {
                    ...personalCheckout,
                    membershipSummary: {
                        ...personalCheckout.membershipSummary,
                        originalPriceCents: personalCheckout.membershipSummary.actualPriceCents,
                    },
                },
            }),
        );

        expect(markup).not.toContain("商品原价");
        expect(markup).not.toContain("优惠金额");
        expect(markup).not.toContain("<del");
    });

    test("frozen non-CNY orders use their own currency instead of a hard-coded yuan symbol", () => {
        const markup = renderToStaticMarkup(
            createElement(MembershipCheckoutSummary, {
                checkout: {
                    ...personalCheckout,
                    currency: "USD",
                },
            }),
        );

        expect(markup).toContain("$1,199");
        expect(markup).not.toContain("¥");
    });

    test("the shell owns the stable two-surface checkout structure", () => {
        const markup = renderToStaticMarkup(
            createElement(PaymentCheckoutShell, {
                busy: false,
                onBack: () => undefined,
                summary: createElement(MembershipCheckoutSummary, { checkout: personalCheckout }),
                payment: createElement("p", { className: "payment-test-slot" }, "支付面板"),
            }),
        );

        expect(markup).toContain("payment-checkout-page");
        expect(markup).toContain("payment-checkout-shell");
        expect(markup).toContain("payment-checkout-order-surface");
        expect(markup).toContain("payment-checkout-payment-surface");
        expect(markup).toContain("安全收银台");
        expect(markup).toContain('aria-busy="false"');
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
        expect(markup).toMatch(/<fieldset[^>]*disabled/);
        expect(markup).toMatch(/<input[^>]*type="radio"[^>]*checked[^>]*value="alipay"/);
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

        const neutral = renderToStaticMarkup(
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
        expect(neutral).toContain("lucide-circle-minus");
        expect(neutral).not.toContain("lucide-check");
    });
});

describe("checkout implementation boundaries", () => {
    test("the page delegates presentation and payment CSS consumes semantic color tokens", () => {
        const page = readFileSync(resolve(root, "src/pages/payment/payment-checkout-page.tsx"), "utf8");
        const qrPanel = readFileSync(resolve(root, "src/pages/payment/payment-qr-panel.tsx"), "utf8");
        const css = readFileSync(resolve(root, "src/pages/payment/payment-checkout.css"), "utf8");
        const tokens = readFileSync(resolve(root, "src/styles/design-tokens.css"), "utf8");

        expect(page).toContain("PaymentCheckoutShell");
        expect(page).toContain("MembershipCheckoutSummary");
        expect(page).toContain("PaymentQrPanel");
        expect(page).toContain("checkoutRequestSucceededForToken(currentState, lease.token");
        expect(page).toContain("checkoutRequestFailedForToken(currentState, lease.token");
        expect(page).not.toContain("<QRCode");
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
        expect(css).toContain("grid-template-columns: minmax(0, 1fr) 320px");
        expect(css).toContain("width: min(calc(100% - 48px), 880px)");
        expect(css).toContain("@media (max-width: 767px)");
        expect(cssProperty(css, ".payment-checkout-page", "height")).toBe("100%");
        expect(cssProperty(css, ".payment-checkout-page", "overflow-y")).toBe("auto");
        expect(cssProperty(css, ".payment-checkout-shell", "overflow-x")).toBe("hidden");
        expect(cssProperty(css, ".payment-checkout-qr-code", "padding")).toBe("var(--space-3)");
        expect(cssProperty(css, ".payment-checkout-qr-code", "background")).toBe("var(--qr-background)");
        expect(cssProperty(css, ".payment-checkout-provider-check", "opacity")).toBe("0");
        expect(cssProperty(css, ".payment-checkout-provider.is-active .payment-checkout-provider-check", "opacity")).toBe("1");
        expect(cssProperty(css, ".membership-checkout-total-price", "color")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-provider.is-active .payment-checkout-provider-icon", "color")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-provider.is-active .payment-checkout-provider-check", "color")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-action", "background")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-action:hover:not(:disabled)", "background")).toBe("var(--brand-active)");
        expect(cssProperty(css, ".payment-checkout-inline-action", "color")).toBe("var(--brand-active)");

        for (const selector of [
            ".membership-checkout-order-number",
            ".membership-checkout-product-meta",
            ".membership-checkout-detail-label",
            ".membership-checkout-original-price",
            ".membership-checkout-discount",
            ".membership-checkout-renewal-note",
            ".payment-checkout-security",
            ".payment-checkout-qr-intro",
            ".payment-checkout-provider-lock-note",
            ".payment-checkout-payment-note",
            ".payment-checkout-clock-state-copy",
            ".payment-checkout-terminal-description",
        ]) {
            expect(cssProperty(css, selector, "color")).toBe("var(--text-secondary)");
        }
    });

    test("the pre-order modal locks dismissal and editable inputs while submitting", () => {
        const modal = readFileSync(resolve(root, "src/pages/membership/membership-purchase-modal.tsx"), "utf8");

        expect(modal).toContain("closable={!submitting}");
        expect(modal).toContain("keyboard={!submitting}");
        expect(modal).toContain("maskClosable={!submitting}");
        expect(modal).toContain("disabled={submitting}");
        expect(modal).toContain("membership-order-unit-price");
        expect(modal).toContain("membership-order-total-price");
        expect(modal).not.toMatch(/\bany\b/);
    });
});
