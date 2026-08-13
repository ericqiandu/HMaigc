import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";

import { AdminContentSection, AdminDataLayout, AdminFilterSection, AdminMetric, AdminMetricBand } from "../src/pages/admin/components/admin-data-layout";

let root: Root | null = null;

afterEach(async () => {
    if (root) await act(async () => root?.unmount());
    root = null;
    document.body.replaceChildren();
});

describe("admin data layout", () => {
    test("exposes one semantic reading order for metrics, filters and content", async () => {
        const host = document.createElement("div");
        document.body.append(host);
        root = createRoot(host);

        await act(async () =>
            root?.render(
                createElement(
                    AdminDataLayout,
                    null,
                    createElement(
                        AdminMetricBand,
                        { title: "运行概览", description: "用户活跃度、任务量与服务性能", queue: createElement("span", { className: "test-queue" }, "队列 0") },
                        createElement(AdminMetric, { label: "活跃用户", value: "1", detail: "DAU 0" }),
                        createElement(AdminMetric, { label: "生成任务", value: "16" }),
                    ),
                    createElement(AdminFilterSection, { label: "统计筛选" }, createElement("input", { className: "test-filter", "aria-label": "模型" })),
                    createElement(AdminContentSection, { title: "使用趋势", description: "真实请求趋势", actions: createElement("button", { className: "test-action" }, "导出") }, createElement("div", { className: "test-chart" }, "图表")),
                ),
            ),
        );

        expect(document.querySelector('[role="region"][aria-labelledby]')?.textContent).toContain("运行概览");
        expect(document.querySelector('[role="region"][aria-label="统计筛选"]')).not.toBeNull();
        expect(document.querySelector("h2")?.textContent).toBe("运行概览");
        expect(document.querySelector(".admin-content-section h2")?.textContent).toBe("使用趋势");
        expect(document.querySelector(".admin-data-section-actions button")?.textContent).toBe("导出");

        const terms = document.querySelectorAll("dt");
        const descriptions = document.querySelectorAll("dd");
        expect(Array.from(terms, (term) => term.textContent)).toEqual(["活跃用户", "生成任务"]);
        expect(Array.from(descriptions, (description) => description.textContent)).toEqual(["1", "DAU 0", "16"]);

        const text = document.body.textContent ?? "";
        expect(text.indexOf("运行概览")).toBeLessThan(text.indexOf("使用趋势"));
    });
});
