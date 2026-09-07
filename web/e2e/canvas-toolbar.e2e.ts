import { expect, test, type Page } from "@playwright/test";

const expectedLabels = {
    "fixture-image": ["查看节点信息", "创建反推提示词的文本和配置节点", "在图片上绘制标记并保存为新节点", "添加蒙版遮罩后局部修改", "调整人物表情与情绪", "剪裁并生成新节点", "生成不同观察视角", "锁定位置和尺寸", "自定义节点工具"],
    "fixture-video": ["查看节点信息", "移除节点", "截取尾帧", "加入我的素材", "下载视频", "编辑", "替换视频", "锁定位置和尺寸"],
    "fixture-audio": ["查看节点信息", "移除节点", "下载音频", "替换音频", "锁定位置和尺寸"],
    "fixture-text": ["查看节点信息", "移除节点", "加入我的素材", "生成提示与参数", "放大编辑文本", "用文本生图", "减小字号", "增大字号", "锁定位置和尺寸"],
    "fixture-config": ["查看节点信息", "移除节点", "生成配置", "锁定位置和尺寸"],
} as const;

const representativeActions = {
    "fixture-image": { label: "创建反推提示词的文本和配置节点", action: "reversePrompt" },
    "fixture-video": { label: "截取尾帧", action: "extractLastFrame" },
    "fixture-audio": { label: "替换音频", action: "upload" },
    "fixture-text": { label: "用文本生图", action: "generateImage" },
    "fixture-config": { label: "生成配置", action: "toggleDialog" },
} as const;

async function toolbarLabels(page: Page) {
    return page.getByRole("toolbar", { name: "节点快捷工具" }).getByRole("button").evaluateAll((buttons) => buttons.map((button) => button.getAttribute("aria-label")));
}

test("real hover exposes ordered tools and enabled actions invoke their wired callback", async ({ page }) => {
    const browserErrors: string[] = [];
    page.on("console", (message) => { if (message.type() === "error") browserErrors.push(message.text()); });
    page.on("pageerror", (error) => browserErrors.push(error.message));
    await page.goto("/perf/canvas-toolbar-fixture.html");

    for (const [nodeId, labels] of Object.entries(expectedLabels)) {
        await page.getByTestId(nodeId).hover();
        await expect(page.getByRole("toolbar", { name: "节点快捷工具" })).toBeVisible();
        expect(await toolbarLabels(page)).toEqual(labels);
        const info = page.getByRole("toolbar", { name: "节点快捷工具" }).getByRole("button", { name: "查看节点信息" });
        await info.click();
        await expect(page.getByTestId("last-action")).toHaveText(`${nodeId}:info`);
        const representative = representativeActions[nodeId as keyof typeof representativeActions];
        await page.getByRole("toolbar", { name: "节点快捷工具" }).getByRole("button", { name: representative.label }).click();
        await expect(page.getByTestId("last-action")).toHaveText(`${nodeId}:${representative.action}`);
    }
    expect(browserErrors).toEqual([]);
});

test("frame extraction is disabled while busy and cannot invoke its callback", async ({ page }) => {
    const browserErrors: string[] = [];
    page.on("console", (message) => { if (message.type() === "error") browserErrors.push(message.text()); });
    page.on("pageerror", (error) => browserErrors.push(error.message));
    await page.goto("/perf/canvas-toolbar-fixture.html?busy=1");
    await page.getByTestId("fixture-video").hover();
    const extraction = page.getByRole("toolbar", { name: "节点快捷工具" }).getByRole("button", { name: "正在截取尾帧" });
    await expect(extraction).toBeDisabled();
    await extraction.click({ force: true });
    await expect(page.getByTestId("last-action")).toHaveText("none");
    expect(browserErrors).toEqual([]);
});
