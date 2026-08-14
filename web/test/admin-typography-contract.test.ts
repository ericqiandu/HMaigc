import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import postcss from "postcss";

import { createAdminAntTheme } from "../src/pages/admin/admin-theme";

const adminWorkspaceStyles = readFileSync(new URL("../src/pages/admin/admin-workspace.css", import.meta.url), "utf8");

function declaration(source: string, selector: string, property: string): string {
    let value = "";
    postcss.parse(source).walkRules((rule) => {
        if (!rule.selectors.includes(selector)) return;
        rule.walkDecls(property, (item) => {
            value = item.value;
        });
    });
    if (!value) throw new Error(`missing ${property} on ${selector}`);
    return value;
}

describe("admin typography contract", () => {
    test("keeps native admin copy and Ant Design controls on one stable system font stack", () => {
        const fontFamily = declaration(adminWorkspaceStyles, ".admin-theme-root", "--admin-font-family");
        const workspaceFontFamily = declaration(adminWorkspaceStyles, ".admin-theme-root .admin-workspace", "--workspace-ui-font-text");
        const theme = createAdminAntTheme("light", "#2979C9");

        expect(fontFamily).toBe('system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei UI", "Microsoft YaHei", sans-serif');
        expect(workspaceFontFamily).toBe("var(--admin-font-family)");
        expect(theme.token?.fontFamily).toBe("var(--admin-font-family)");
        expect(theme.token?.fontWeightStrong).toBe(600);
        expect(declaration(adminWorkspaceStyles, ".admin-theme-root .admin-page-title", "font-weight")).toBe("600");
        expect(declaration(adminWorkspaceStyles, ".admin-theme-root .admin-data-section-title", "font-weight")).toBe("600");
    });

    test("does not compress Chinese admin copy with negative tracking", () => {
        expect(declaration(adminWorkspaceStyles, ".admin-workspace.workspace-ui-scope", "letter-spacing")).toBe("0");
        expect(declaration(adminWorkspaceStyles, ".admin-workspace.workspace-ui-scope", "line-height")).toBe("22px");
        expect(declaration(adminWorkspaceStyles, ".admin-page-title", "letter-spacing")).toBe("0");
    });

    test("maintains a readable page, section, metric and table hierarchy", () => {
        expect(declaration(adminWorkspaceStyles, ".admin-page-title", "font-size")).toBe("20px");
        expect(declaration(adminWorkspaceStyles, ".admin-page-title", "line-height")).toBe("28px");
        expect(declaration(adminWorkspaceStyles, ".admin-page-description", "font-size")).toBe("13px");
        expect(declaration(adminWorkspaceStyles, ".admin-page-description", "line-height")).toBe("20px");
        expect(declaration(adminWorkspaceStyles, ".admin-workspace .admin-data-section-title", "font-size")).toBe("15px");
        expect(declaration(adminWorkspaceStyles, ".admin-workspace .admin-data-section-description", "font-size")).toBe("13px");
        expect(declaration(adminWorkspaceStyles, ".admin-workspace .admin-metric-value", "font-size")).toBe("24px");
        expect(declaration(adminWorkspaceStyles, ".admin-workspace .app-table-surface .ant-table-thead > tr > th", "font-size")).toBe("13px");
        expect(declaration(adminWorkspaceStyles, ".admin-workspace .app-table-surface .ant-table-tbody > tr > td", "font-size")).toBe("14px");
        expect(declaration(adminWorkspaceStyles, ".admin-workspace .app-table-surface .ant-table-tbody > tr > td", "line-height")).toBe("22px");
    });
});
