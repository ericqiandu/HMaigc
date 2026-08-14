import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";

import { AdminModelCenterTabs, MODEL_CENTER_SECTIONS } from "../src/pages/admin/components/admin-model-center-tabs";
import { ADMIN_NAVIGATION_GROUPS, findAdminNavigationGroup, findAdminNavigationItem } from "../src/pages/admin/components/admin-navigation";

describe("admin model center", () => {
    const routerSource = readFileSync(new URL("../src/router.tsx", import.meta.url), "utf8");

    test("uses one model-center navigation entry for the unified workflow", () => {
        const modelGroup = ADMIN_NAVIGATION_GROUPS.find((group) => group.id === "models-cost");
        expect(modelGroup?.items.map((item) => item.label)).toEqual(["模型中心", "音色管理", "超分定价", "分镜提示词"]);
        expect(findAdminNavigationItem("/admin/models/kuaizi")?.label).toBe("模型中心");
        expect(findAdminNavigationGroup("/admin/models/pricing")?.id).toBe("models-cost");
    });

    test("exposes the three ordered operational sections", () => {
        expect(MODEL_CENTER_SECTIONS.map(({ path, label, description }) => ({ path, label, description }))).toEqual([
            { path: "/admin/models", label: "渠道与模型", description: "接入渠道并维护用户可用模型" },
            { path: "/admin/models/kuaizi", label: "筷子账号", description: "维护统一服务地址、Key 与验证状态" },
            { path: "/admin/models/pricing", label: "价格与 Agent", description: "配置成本、积分售价与全站 Agent 模型" },
        ]);
    });

    test("hard-cuts the retired parallel model routes", () => {
        expect(routerSource).toContain('{ path: "models/kuaizi", element: deferredRoute(<KuaiziProviderPage />) }');
        expect(routerSource).toContain('{ path: "models/pricing", element: deferredRoute(<ModelPricingPage />) }');
        expect(routerSource).not.toContain('path: "providers/kuaizi"');
        expect(routerSource).not.toContain('path: "model-pricing"');
    });

    test("renders the model workflow as compact tabs instead of descriptive navigation cards", () => {
        const markup = renderToStaticMarkup(
            <MemoryRouter initialEntries={["/admin/models/kuaizi"]}>
                <AdminModelCenterTabs />
            </MemoryRouter>,
        );
        expect(markup).toContain('aria-label="模型中心"');
        expect(markup).not.toContain('role="tablist"');
        expect(markup).not.toContain('role="tab"');
        expect(markup).toContain('aria-current="page"');
        expect(markup).toContain("渠道与模型");
        expect(markup).toContain("筷子账号");
        expect(markup).toContain("价格与 Agent");
        expect(markup).not.toContain("接入渠道并维护用户可用模型");
        expect(markup).not.toContain("维护统一服务地址、Key 与验证状态");
        expect(markup).not.toContain("配置成本、积分售价与全站 Agent 模型");
        expect(markup).not.toContain("当前</span>");
    });
});
