import { describe, expect, test } from "bun:test";

const pagePaths = {
    voices: new URL("../src/pages/admin/voices/voices-page.tsx", import.meta.url),
    superResolution: new URL("../src/pages/admin/super-resolution-pricing/super-resolution-pricing-page.tsx", import.meta.url),
    storyboardPrompts: new URL("../src/pages/admin/storyboard-prompts/storyboard-prompts-page.tsx", import.meta.url),
};

describe("admin model and billing category layout", () => {
    test("orders voice metrics, filters and catalog inside the shared data layout", async () => {
        const source = await Bun.file(pagePaths.voices).text();

        expectInOrder(source, ["<AdminDataLayout>", "<AdminMetricBand", "<AdminFilterSection", "<AdminContentSection"]);
        expect(source).toContain('title="音色目录概览"');
        expect(source).toContain('label="音色筛选条件"');
        expect(source).toContain('title="音色目录"');
        expect(source).toContain('className="admin-voice-model-center-link"');
        expect(source).toContain('href="/admin/models"');
    });

    test("does not project voices from a previous channel as current facts while the next catalog loads", async () => {
        const source = await Bun.file(pagePaths.voices).text();

        expect(source).toContain('const [loadedVoiceChannelId, setLoadedVoiceChannelId] = useState("")');
        expect(source).toContain("loadedVoiceChannelId === channelId");
        expectInOrder(source, ['setLoadedVoiceChannelId("")', "setVoices([])", "setVoices(result.voices)", "setLoadedVoiceChannelId(targetChannelId)"]);
    });

    test("orders super-resolution facts, edition filter and pricing table inside the shared data layout", async () => {
        const source = await Bun.file(pagePaths.superResolution).text();

        expectInOrder(source, ["<AdminDataLayout>", "<AdminMetricBand", "<AdminFilterSection", "<AdminContentSection"]);
        expect(source).toContain('title="超分规格概览"');
        expect(source).toContain('label="超分版本筛选"');
        expect(source).toContain('title="超分价格表"');
        expect(source).toContain("只有填写用户积分售价后，该规格才允许执行");
    });

    test("orders prompt metrics, filters and version table inside the shared data layout", async () => {
        const source = await Bun.file(pagePaths.storyboardPrompts).text();

        expectInOrder(source, ["<AdminDataLayout>", "<AdminMetricBand", "<AdminFilterSection", "<AdminContentSection"]);
        expect(source).toContain('title="分镜提示词概览"');
        expect(source).toContain('label="提示词版本筛选"');
        expect(source).toContain('title="提示词版本"');
        expect(source).toContain("启用版本会立即用于 Agent 分镜生成");
    });

    test("provides owned table surfaces and responsive controls for all three pages", async () => {
        const workspaceStyles = await Bun.file(new URL("../src/pages/admin/admin-workspace.css", import.meta.url)).text();
        const responsiveStyles = await Bun.file(new URL("../src/pages/admin/admin-responsive.css", import.meta.url)).text();

        for (const selector of ["admin-voice-table-surface", "super-resolution-pricing-table-surface", "admin-prompt-table-surface"]) {
            expect(workspaceStyles).toContain(`.${selector}`);
        }
        expect(workspaceStyles).toContain(".admin-content-section-body > :where(");
        expect(workspaceStyles).toContain("border-radius: 0");
        expect(responsiveStyles).toContain(".admin-voice-list-toolbar .workspace-list-toolbar-fields");
        expect(responsiveStyles).toContain(".super-resolution-pricing-edition");
        expect(responsiveStyles).toContain(".admin-prompt-list-toolbar .workspace-list-toolbar-fields");
        expect(responsiveStyles).toContain("min-height: 44px");
    });
});

function expectInOrder(source: string, fragments: string[]) {
    let cursor = -1;
    for (const fragment of fragments) {
        const next = source.indexOf(fragment, cursor + 1);
        expect(next).toBeGreaterThan(cursor);
        cursor = next;
    }
}
