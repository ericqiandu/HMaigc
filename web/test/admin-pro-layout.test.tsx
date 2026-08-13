import "./setup-happy-dom";

import { afterEach, describe, expect, test } from "bun:test";
import { act, createElement } from "react";
import { createRoot, type Root } from "react-dom/client";
import { MemoryRouter } from "react-router";

import { AdminPageActions, AdminPageFrame } from "../src/pages/admin/components/admin-shell";

let root: Root | null = null;

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
});
