import { expect, test } from "bun:test";

const readSource = (relativePath: string) => Bun.file(new URL(relativePath, import.meta.url)).text();

test("admin UI does not expose the retired operations controller", async () => {
    const [routerSource, navigationSource] = await Promise.all([
        readSource("../src/router.tsx"),
        readSource("../src/pages/admin/components/admin-navigation.tsx"),
    ]);

    expect(routerSource).not.toContain('path: "operations"');
    expect(routerSource).not.toContain("const OperationsPage");
    expect(routerSource).not.toContain("/pages/admin/operations/operations-page");
    expect(navigationSource).not.toContain('path: "/admin/operations"');
    expect(navigationSource).not.toContain("ServerCog");
});
