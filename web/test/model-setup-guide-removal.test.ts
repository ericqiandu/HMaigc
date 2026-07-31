import { describe, expect, test } from "bun:test";

const workspaceShellSource = await Bun.file(new URL("../src/components/layout/app-top-nav.tsx", import.meta.url)).text();
const registrationSource = await Bun.file(new URL("../src/pages/auth/register.tsx", import.meta.url)).text();
const retiredGuide = Bun.file(new URL("../src/components/layout/model-setup-guide.tsx", import.meta.url));

describe("user model configuration policy", () => {
    test("does not mount or schedule the retired user-facing model setup guide", async () => {
        expect(workspaceShellSource).not.toContain("ModelSetupGuide");
        expect(registrationSource).not.toContain("infinite-canvas:model-setup-guide");
        expect(await retiredGuide.exists()).toBe(false);
    });
});
