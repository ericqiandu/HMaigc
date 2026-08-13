import "./setup-happy-dom";

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { App, ConfigProvider } from "antd";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

import { DEFAULT_ADMIN_LAYOUT_SETTINGS } from "../src/pages/admin/admin-layout-settings";
import { AdminPageActions, AdminPageFrame, adminWorkspacePresentation } from "../src/pages/admin/components/admin-shell";
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
    test("projects the configured widths and layout facts while keeping collapsed navigation at 64px", () => {
        const expandedWidths = ([208, 236, 272] as const).map((siderWidth) => adminWorkspacePresentation({ ...DEFAULT_ADMIN_LAYOUT_SETTINGS, siderWidth }, false).workspaceStyle["--admin-sider-width"]);
        const expanded = adminWorkspacePresentation({ ...DEFAULT_ADMIN_LAYOUT_SETTINGS, contentWidth: "fixed", fixedHeader: false, siderWidth: 272 }, false);
        const collapsed = adminWorkspacePresentation({ ...DEFAULT_ADMIN_LAYOUT_SETTINGS, siderWidth: 208 }, true);

        expect(expandedWidths).toEqual(["208px", "236px", "272px"]);
        expect(expanded.workspaceAttributes).toEqual({ "data-admin-content-width": "fixed", "data-admin-fixed-header": "false" });
        expect(expanded.workspaceStyle["--admin-sider-width"]).toBe("272px");
        expect(expanded.sidebarWidth).toBe("var(--admin-sider-width)");
        expect(collapsed.sidebarWidth).toBe(64);
    });

    test("defines fixed and fluid content widths, independent menu themes, and mobile sticky navigation", async () => {
        const workspaceStyles = await Bun.file(new URL("../src/pages/admin/admin-workspace.css", import.meta.url)).text();
        const domainStyles = await Bun.file(new URL("../src/pages/admin/admin-domain-workspace.css", import.meta.url)).text();
        const artStyles = await Bun.file(new URL("../src/pages/admin/admin-art-layout.css", import.meta.url)).text();
        const responsiveStyles = await Bun.file(new URL("../src/pages/admin/admin-responsive.css", import.meta.url)).text();

        expect(workspaceStyles).toContain('[data-admin-content-width="fixed"] .admin-page-frame');
        expect(workspaceStyles).toContain("max-width: 1120px");
        expect(workspaceStyles).toContain('[data-admin-content-width="fluid"] .admin-page-frame');
        expect(workspaceStyles).toContain("max-width: 1304px");
        expect(workspaceStyles).toContain('[data-admin-menu-theme="light"]');
        expect(workspaceStyles).toContain('[data-admin-menu-theme="dark"]');
        expect(workspaceStyles).toContain('[data-admin-fixed-header="false"] .admin-workspace-main');
        expect(responsiveStyles).toContain("position: sticky");
        expect(responsiveStyles).toContain("top: 0");

        expect(workspaceStyles).toContain(".admin-data-layout");
        expect(workspaceStyles).toContain(".admin-metric-band-list");
        expect(workspaceStyles).toContain("grid-template-columns: repeat(4, minmax(0, 1fr))");
        expect(responsiveStyles).toContain(".admin-metric-band-list");
        expect(responsiveStyles).toContain("grid-template-columns: repeat(2, minmax(0, 1fr))");
        expect(responsiveStyles).toContain("grid-template-columns: minmax(0, 1fr)");
        const adminStyles = `${workspaceStyles}\n${domainStyles}\n${artStyles}\n${responsiveStyles}`;
        expect(adminStyles).not.toContain("admin-analytics-overview-grid");
        expect(adminStyles).not.toContain("admin-analytics-metric-section");
        expect(adminStyles).not.toContain("admin-analytics-metrics");
    });

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

    test("renders metrics, filters, trend and details in one semantic data-page order", async () => {
        await renderAnalyticsPage();
        await flushEffects();

        const layout = document.querySelector(".admin-analytics-layout");
        const dataLayout = layout?.querySelector(".admin-data-layout");
        const sections = Array.from(dataLayout?.children ?? []).map((node) => node.className);
        expect(sections).toEqual(["admin-metric-band", "admin-metric-band", "admin-filter-section", "admin-content-section", "admin-content-section"]);
        expect(dataLayout?.querySelectorAll(".admin-metric-band")).toHaveLength(2);
        expect(dataLayout?.querySelector('[role="region"][aria-label="统计筛选"]')).not.toBeNull();
        expect(Array.from(dataLayout?.querySelectorAll(".admin-content-section-title") ?? [], (title) => title.textContent)).toEqual(["使用趋势", "分析明细"]);

        const text = dataLayout?.textContent ?? "";
        expect(text.indexOf("运行概览")).toBeLessThan(text.indexOf("商业指标"));
        expect(text.indexOf("商业指标")).toBeLessThan(text.indexOf("时间范围"));
        expect(text.indexOf("时间范围")).toBeLessThan(text.indexOf("使用趋势"));
        expect(text.indexOf("使用趋势")).toBeLessThan(text.indexOf("模型分析"));
        expect(document.querySelector(".admin-page-actions .admin-analytics-refresh-button")?.textContent).toContain("刷新");
        expect(document.querySelector(".admin-filter-section .admin-analytics-refresh-button")).toBeNull();
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
