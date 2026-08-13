import "./setup-happy-dom";

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

import { AdminPageActions, AdminPageFrame } from "../src/pages/admin/components/admin-shell";
import type { AdminAnalytics } from "../src/services/api/auth";

const analyticsFixture: AdminAnalytics = {
    from: "2026-07-15",
    to: "2026-08-13",
    kpi: {
        activeUsers: 12,
        dau: 5,
        wau: 9,
        mau: 12,
        generationTasks: 48,
        upstreamRequests: 50,
        successRate: 96,
        p95DurationMs: 1600,
        currentQueuedTasks: 2,
        estimatedCostMicros: 1250000,
        costAvailable: true,
        currency: "USD",
        settledRevenueMicrocredits: 3200000,
        settledBaseCostMicrocredits: 1250000,
        grossProfitMicrocredits: 1950000,
        settledBillingOrders: 42,
        pendingAmountMicrocredits: 120000,
        pendingBillingOrders: 2,
        reviewAmountMicrocredits: 30000,
        reviewBillingOrders: 1,
    },
    trend: [{ day: "2026-08-13", tasks: 12, requests: 13, activeUsers: 5, requestSuccessRate: 92.3 }],
    models: [],
    users: [],
    failures: [],
};

let analyticsRequest: () => Promise<AdminAnalytics> = async () => analyticsFixture;

mock.module("@/services/api/auth", () => ({
    exportAdminAnalytics: async () => new Blob(["ok"]),
    getAdminAnalytics: () => analyticsRequest(),
    listAdminUsers: async () => ({ limit: 50, page: 1, total: 0, users: [] }),
}));

const { default: AnalyticsPanel } = await import("../src/pages/admin/components/analytics-panel");

let root: Root | null = null;

beforeEach(() => {
    analyticsRequest = async () => analyticsFixture;
});

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("admin pro layout", () => {
    test("renders descendant actions in the page header slot", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () =>
            root?.render(
                createElement(
                    MemoryRouter,
                    null,
                    createElement(AdminPageFrame, { description: "活跃、调用与成本趋势", title: "数据概览" }, createElement(AdminPageActions, null, createElement("button", { className: "analytics-refresh", type: "button" }, "刷新"))),
                ),
            ),
        );

        const actions = document.querySelector(".admin-page-actions");
        expect(actions?.querySelector(".analytics-refresh")?.textContent).toBe("刷新");
        expect(document.querySelector(".admin-page-content .analytics-refresh")).toBeNull();
    });

    test("renders the overview before filters and moves analytics actions into the page header", async () => {
        await renderAnalyticsPage();
        await flushEffects();

        const layout = document.querySelector(".admin-analytics-layout");
        const sections = Array.from(layout?.children ?? []).map((node) => node.className);
        expect(sections.slice(0, 3)).toEqual(["admin-analytics-overview-grid", "admin-analytics-toolbar", "admin-analytics-trend"]);
        expect(layout?.children.item(3)?.classList.contains("admin-analytics-tabs")).toBe(true);
        expect(document.querySelector(".admin-page-actions .admin-analytics-refresh-button")?.textContent).toContain("刷新");
        expect(document.querySelector(".admin-analytics-toolbar .admin-analytics-refresh-button")).toBeNull();
        expect(document.querySelector(".admin-table-empty")?.textContent).toContain("暂无模型统计");
    });

    test("shows an explicit first-load skeleton and an actionable load error", async () => {
        let rejectRequest: ((error: Error) => void) | undefined;
        analyticsRequest = () =>
            new Promise<AdminAnalytics>((_, reject) => {
                rejectRequest = reject;
            });

        await renderAnalyticsPage();
        expect(document.querySelector('[aria-label="正在加载数据概览"]')).not.toBeNull();

        await act(async () => rejectRequest?.(new Error("统计服务不可用")));
        await flushEffects();

        const error = document.querySelector(".admin-content-error");
        expect(error?.textContent).toContain("数据概览读取失败");
        expect(error?.textContent).toContain("统计服务不可用");
        expect(error?.querySelector(".admin-content-error-retry")).not.toBeNull();
    });

    test("preserves the last successful analytics snapshot when refresh fails", async () => {
        let callCount = 0;
        analyticsRequest = async () => {
            callCount += 1;
            if (callCount > 1) throw new Error("刷新请求失败");
            return analyticsFixture;
        };

        await renderAnalyticsPage();
        await flushEffects();
        expect(document.querySelector(".admin-analytics-layout")?.textContent).toContain("12");

        const refreshButton = document.querySelector<HTMLButtonElement>(".admin-page-actions .admin-analytics-refresh-button");
        await act(async () => refreshButton?.click());
        await flushEffects();

        expect(document.querySelector(".admin-content-error")?.textContent).toContain("当前继续显示上一次成功读取的数据");
        expect(document.querySelector(".admin-analytics-layout")?.textContent).toContain("12");
    });
});

async function renderAnalyticsPage() {
    const host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);

    await act(async () =>
        root?.render(
            createElement(
                ConfigProvider,
                null,
                createElement(
                    App,
                    null,
                    createElement(
                        MemoryRouter,
                        null,
                        createElement(
                            AdminPageFrame,
                            { description: "活跃、调用与成本趋势", title: "数据概览" },
                            createElement(AnalyticsPanel, {
                                channels: [{ id: "channel-1", models: ["seedance-2.5"], name: "筷子科技" }],
                                users: [{ displayName: "管理员", id: "user-1", username: "admin" }],
                            }),
                        ),
                    ),
                ),
            ),
        ),
    );
}

async function flushEffects() {
    await act(async () => {
        await Promise.resolve();
        await new Promise((resolve) => setTimeout(resolve, 0));
    });
}
