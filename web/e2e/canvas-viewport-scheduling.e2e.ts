import { expect, test } from "@playwright/test";

test("a pending paint must not suppress the next wheel commit", async ({ page }) => {
    await page.goto("/perf/canvas-viewport.html");
    await expect(page.locator("html")).toHaveAttribute("data-viewport-fixture-ready", "true");
    // 模拟绘制帧暂停，但普通计时器仍运行；不替换生产事件处理或提交函数。
    await page.evaluate(() => {
        window.requestAnimationFrame = () => 123;
        window.cancelAnimationFrame = () => {};
    });
    const wheel = () => page.evaluate(() => {
        const container = document.querySelector("[data-canvas-grid-layer]")?.parentElement;
        if (!container) throw new Error("画布未挂载");
        container.dispatchEvent(new WheelEvent("wheel", {
            deltaY: 100, clientX: 600, clientY: 400, bubbles: true, cancelable: true,
        }));
    });
    const scale = () => page.evaluate(() => {
        const raw = document.querySelector("output")?.textContent;
        if (!raw) throw new Error("视口状态缺失");
        const viewport: { k: number } = JSON.parse(raw);
        return viewport.k;
    });
    await wheel();
    await expect.poll(scale).toBeCloseTo(1 / 1.1, 6);
    await wheel();
    await expect.poll(scale).toBeCloseTo(1 / (1.1 * 1.1), 6);
});
