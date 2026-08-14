import "./setup-happy-dom";

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { createMemoryRouter, RouterProvider } from "react-router";

import type { AdminReferralProgramData } from "../src/services/api/referral";

const referralFixture: AdminReferralProgramData = {
    program: { enabled: true },
    summary: {
        registeredCount: 24,
        purchasedCount: 9,
        grantedTotalMicrocredits: 18_500_000,
    },
    rules: [
        {
            billingCycle: "year",
            currency: "CNY",
            enabled: true,
            inviteeRewardMicrocredits: 2_000_000,
            inviterRewardMicrocredits: 3_000_000,
            membershipPlanId: "plan-1",
            planCode: "standard-year",
            planName: "标准版",
            planTier: "standard",
            priceCents: 59900,
        },
    ],
    invites: [
        {
            boundAt: "2026-08-14T02:00:00Z",
            id: "referral-1",
            inviteeDisplayName: "受邀用户",
            inviteeUserId: "user-2",
            inviteeUsername: "invited-user",
            inviterUserId: "user-1",
            referralCode: "HM2026",
            rewardedMicrocredits: 0,
            status: "eligible",
        },
    ],
    total: 21,
};

let referralRequest: () => Promise<AdminReferralProgramData> = async () => referralFixture;

mock.module("@/services/api/referral", () => ({
    disqualifyAdminReferral: async () => ({ ok: true }),
    getAdminReferralProgram: () => referralRequest(),
    updateAdminReferralProgram: async (enabled: boolean) => ({ program: { enabled } }),
    updateAdminReferralRule: async () => ({ rule: referralFixture.rules[0] }),
}));

const { default: ReferralProgramPage } = await import("../src/pages/admin/referrals/referral-program-page");

let root: Root | null = null;

beforeEach(() => {
    referralRequest = async () => referralFixture;
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("admin referrals layout", () => {
    test("orders the real summary, activity, reward rules and relationships in one data layout", async () => {
        await renderReferralPage();
        await flushEffects();

        const dataLayout = document.querySelector(".admin-data-layout");
        expect(Array.from(dataLayout?.children ?? [], (node) => node.className)).toEqual(["admin-metric-band", "admin-content-section", "admin-content-section", "admin-content-section"]);
        expect(dataLayout?.querySelector('.admin-metric-band[role="region"] .admin-data-section-title')?.textContent).toBe("邀请活动概览");
        expect(Array.from(dataLayout?.querySelectorAll(".admin-content-section-title") ?? [], (title) => title.textContent)).toEqual(["活动配置", "套餐奖励", "邀请关系"]);
        expect(dataLayout?.querySelector(".admin-metric-band-list")?.textContent).toContain("邀请注册关系24");
        expect(dataLayout?.querySelector(".admin-metric-band-list")?.textContent).toContain("已触发首购9");
        expect(dataLayout?.querySelector(".admin-metric-band-list")?.textContent).toContain("累计发放积分18.5");
        const contentSections = dataLayout?.querySelectorAll(".admin-content-section");
        expect(contentSections?.[2]?.querySelector(".admin-data-section-actions")?.textContent).toContain("共 21 条关系");
        expect(dataLayout?.querySelector(".referral-admin-program-switch")).not.toBeNull();
        expect(dataLayout?.querySelector(".referral-admin-rule-save")?.textContent).toContain("已保存");
        expect(dataLayout?.querySelector(".referral-admin-table")?.textContent).toContain("受邀用户");
        expect(dataLayout?.querySelector(".app-pagination-bar")).not.toBeNull();
        expect(dataLayout?.querySelector(".referral-admin-disqualify")?.textContent).toContain("取消资格");
    });

    test("keeps a failed first load explicit and retryable without fabricated metrics", async () => {
        referralRequest = async () => {
            throw new Error("邀请服务暂时不可用");
        };

        await renderReferralPage();
        await flushEffects();

        const error = document.querySelector(".admin-content-error");
        expect(error?.textContent).toContain("邀请运营数据读取失败");
        expect(error?.textContent).toContain("邀请服务暂时不可用");
        expect(error?.querySelector(".admin-content-error-retry")).not.toBeNull();
        expect(document.querySelector(".admin-metric-band")).toBeNull();
    });

    test("reuses the shared metric surface and keeps referral controls responsive", async () => {
        const styles = await Bun.file(new URL("../src/pages/admin/admin-domain-workspace.css", import.meta.url)).text();

        expect(styles).toContain(".admin-workspace .referral-admin-section-status");
        expect(styles).toContain(".admin-workspace .referral-admin-content .admin-metric-band-list");
        expect(styles).toContain("grid-template-columns: repeat(3, minmax(0, 1fr))");
        expect(styles).toContain(".admin-workspace .admin-content-section-body > .referral-admin-table");
        expect(styles).toContain("margin: -12px -16px 0");
        expect(styles).toContain("@media (max-width: 1279px)");
        expect(styles).toContain("@media (max-width: 639px)");
        expect(styles).toContain("grid-template-columns: minmax(0, 1fr)");
        expect(styles).toContain("min-height: 44px");
        expect(styles).not.toContain(".admin-workspace .referral-admin-overview");
        expect(styles).not.toContain(".admin-workspace .referral-admin-metric");
    });
});

async function renderReferralPage() {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
    const router = createMemoryRouter([{ path: "/", element: createElement(ReferralProgramPage) }], { initialEntries: ["/"] });

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
