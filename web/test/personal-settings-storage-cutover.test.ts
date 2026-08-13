import { expect, test } from "bun:test";

const settingsPage = Bun.file(new URL("../src/pages/settings/index.tsx", import.meta.url));
const resourceApi = Bun.file(new URL("../src/services/api/resources.ts", import.meta.url));

test("个人设置不再暴露用户级 OSS 配置", async () => {
    const source = await settingsPage.text();
    expect(source).not.toContain("UserOSSSettingsForm");
    expect(source).not.toContain("我的 OSS");
    expect(source).not.toContain('key: "storage"');
    expect(source).not.toContain("settingsSections");
});

test("前端资源 API 不再提供个人 OSS 读写入口", async () => {
    const source = await resourceApi.text();
    expect(source).not.toContain("getUserOSSSetting");
    expect(source).not.toContain("updateUserOSSSetting");
    expect(source).not.toContain('api.patch("/settings/oss"');
});
