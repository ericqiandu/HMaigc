import "./setup-happy-dom";

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router";

import type { MembershipPlan } from "../src/services/api/membership";

const membershipApi = await import("../src/services/api/membership");
const paymentApi = await import("../src/services/api/payment");

const planFixture: MembershipPlan = {
    id: "plan-standard",
    code: "standard-year",
    name: "标准版",
    tier: "standard",
    audience: "personal",
    billingCycle: "year",
    priceCents: 59900,
    originalPriceCents: 72900,
    currency: "CNY",
    creditsPerPeriod: 1_500_000_000,
    imageConcurrency: 6,
    videoConcurrency: 4,
    unlimitedTaskQueue: false,
    teamStorageBytes: 0,
    sharedAssetsEnabled: false,
    projectPermissionsEnabled: false,
    invoicingEnabled: true,
    commercialUseEnabled: true,
    topupDiscountBasisPoints: 9_500,
    minSeats: 1,
    maxSeats: 1,
    benefitsJson: "[]",
    benefits: ["商业使用权益"],
    enabled: true,
    sortOrder: 10,
    createdAt: "2026-08-14T00:00:00Z",
    updatedAt: "2026-08-14T00:00:00Z",
};

let plansRequest: () => Promise<MembershipPlan[]> = async () => [planFixture];

mock.module("@/services/api/membership", () => ({
    ...membershipApi,
    closeAdminMembershipOrder: async () => ({ ok: true }),
    getAdminMembershipStorefront: async () => ({ enabled: false }),
    listAdminInvoiceRequests: async () => ({ items: [], total: 0 }),
    listAdminMembershipOrders: async () => ({ items: [], total: 0 }),
    listAdminMembershipPlans: () => plansRequest(),
    resolveAdminInvoiceRequest: async () => ({ ok: true }),
    updateAdminMembershipPlan: async () => planFixture,
    updateAdminMembershipStorefront: async () => ({ enabled: false }),
}));

mock.module("@/services/api/payment", () => ({
    ...paymentApi,
    listAdminPaymentTransactions: async () => ({ items: [], total: 0 }),
    listAdminPaymentWebhookEvents: async () => ({ items: [], total: 0 }),
    reconcileAdminPaymentTransaction: async () => ({ transaction: null, providerState: "unknown" }),
}));

const { default: MembershipAdminPage } = await import("../src/pages/admin/membership/membership-page");

let root: Root | null = null;

beforeEach(() => {
    plansRequest = async () => [planFixture];
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("admin membership layout", () => {
    test("orders the real membership summary and six business modules in one data layout", async () => {
        await renderMembershipPage();
        await flushEffects();

        const dataLayout = document.querySelector(".admin-data-layout");
        expect(Array.from(dataLayout?.children ?? [], (node) => node.className)).toEqual(["admin-metric-band", "admin-content-section"]);
        expect(dataLayout?.querySelector('.admin-metric-band[role="region"] .admin-data-section-title')?.textContent).toBe("会员运营概览");
        expect(dataLayout?.querySelector(".admin-content-section-title")?.textContent).toBe("会员业务模块");
        expect(Array.from(dataLayout?.querySelectorAll('[role="tab"]') ?? [], (tab) => tab.textContent)).toEqual(["套餐与权益", "商城展示", "会员订单", "开票处理", "支付交易", "回调审计"]);
        expect(dataLayout?.querySelector(".admin-metric-band-list")?.textContent).toContain("套餐目录1 个套餐");
        expect(dataLayout?.querySelector(".admin-membership-table")?.textContent).toContain("标准版");
    });

    test("keeps a failed first load explicit and retryable without fabricated membership metrics", async () => {
        plansRequest = async () => {
            throw new Error("会员套餐服务暂时不可用");
        };

        await renderMembershipPage();
        await flushEffects();

        const error = document.querySelector(".admin-content-error");
        expect(error?.textContent).toContain("套餐配置加载失败");
        expect(error?.textContent).toContain("会员套餐服务暂时不可用");
        expect(error?.querySelector(".admin-content-error-retry")).not.toBeNull();
        expect(document.querySelector(".admin-membership-table")).toBeNull();
        expect(document.body.textContent).not.toContain("0 个套餐");
    });
});

async function renderMembershipPage() {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const router = createMemoryRouter([{ path: "/", element: createElement(MembershipAdminPage) }], { initialEntries: ["/"] });

    await act(async () => {
        root?.render(createElement(ConfigProvider, null, createElement(App, null, createElement(RouterProvider, { router }))));
    });
}

async function flushEffects() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
