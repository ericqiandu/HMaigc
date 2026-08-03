import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

const adminWorkspaceStyles = readFileSync(
    new URL("../src/pages/admin/admin-workspace.css", import.meta.url),
    "utf8",
);
const adminShellSource = readFileSync(
    new URL("../src/pages/admin/components/admin-shell.tsx", import.meta.url),
    "utf8",
);

describe("admin mobile navigation", () => {
    test("does not paint the full-screen drawer root after its panel closes", () => {
        expect(adminWorkspaceStyles).not.toContain(".admin-mobile-navigation-drawer.workspace-ui-scope");
        expect(adminWorkspaceStyles).toContain(".admin-workspace {\n        background: var(--workspace-ui-page);");
        expect(adminShellSource).toContain('rootClassName="admin-mobile-navigation-drawer"');
        expect(adminShellSource).not.toContain('rootClassName="admin-mobile-navigation-drawer workspace-ui-scope"');
        expect(adminShellSource).not.toContain('className="workspace-ui-scope"');
    });

    test("closes navigation and destroys the hidden drawer content", () => {
        expect(adminShellSource).toContain("destroyOnHidden");
        expect(adminShellSource).toContain("<AdminNavigation collapsed={false} onNavigate={() => setOpen(false)} />");
    });
});
