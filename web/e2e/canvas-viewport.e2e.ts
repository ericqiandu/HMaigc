import { expect, test } from "@playwright/test";

test.afterEach(async ({ page }, testInfo) => {
    if (testInfo.status === testInfo.expectedStatus || page.isClosed()) return;
    await testInfo.attach("input-and-viewport", { contentType: "application/json", body: JSON.stringify(await page.evaluate(() => {
        const grid = document.querySelector("[data-canvas-grid-layer]");
        return { events: document.documentElement.dataset.testInputEvents,
            live: grid?.parentElement?.getAttribute("style"), state: document.querySelector("output")?.textContent };
    })) });
});

test("grid stays stable through wheel zoom, pan and React commit", async ({ page }, testInfo) => {
    await page.addInitScript(() => {
        const events: { type: string; x: number; y: number; at: number }[] = [];
        const record = (type: string, x: number, y: number) => {
            events.push({ type, x, y, at: performance.now() });
            document.documentElement.dataset.testInputEvents = JSON.stringify(events.slice(-40));
        };
        window.addEventListener("wheel", event => record("wheel", event.deltaX, event.deltaY), { capture: true });
        window.addEventListener("pointermove", event => record("pointermove", event.clientX, event.clientY), { capture: true });
        window.addEventListener("pointerup", event => record("pointerup", event.clientX, event.clientY), { capture: true });
    });
    await page.goto("/perf/canvas-viewport.html");
    await expect(page.locator("html")).toHaveAttribute("data-viewport-fixture-ready", "true");
    const grid = page.locator("[data-canvas-grid-layer]");
    const expectGrid = async () => {
        try {
            // 浏览器对重复背景层的序列化不同，每一层仍须严格为 48px。
            await expect(grid).toHaveCSS("background-size", /^48px 48px(?:, 48px 48px)*$/);
        } catch (error) {
            await testInfo.attach("viewport-diagnostics", { contentType: "application/json", body: JSON.stringify(await page.evaluate(() => {
                const element = document.querySelector("[data-canvas-grid-layer]");
                return { readyState: document.readyState, grid: element ? getComputedStyle(element).backgroundSize : null,
                    style: element?.parentElement?.getAttribute("style"), state: document.querySelector("output")?.textContent };
            })) });
            throw error;
        }
    };
    await expectGrid();
    const state = () => page.evaluate(() => {
        const element = document.querySelector("output");
        if (!element?.textContent) throw new Error("视口测试状态未挂载");
        const value: { x: number; y: number; k: number } = JSON.parse(element.textContent!);
        return value;
    });
    const before = await state();
    await page.mouse.move(600, 400);
    await page.mouse.wheel(0, 100);
    await expect.poll(async () => (await state()).k).toBeLessThan(before.k);
    const zoomed = await state();
    expect((600 - zoomed.x) / zoomed.k).toBeCloseTo((600 - before.x) / before.k, 6);
    expect((400 - zoomed.y) / zoomed.k).toBeCloseTo((400 - before.y) / before.k, 6);
    await expectGrid();
    await page.mouse.wheel(30, 20);
    await expect.poll(async () => (await state()).x).toBeCloseTo(zoomed.x - 30, 6);
    const panned = await state();
    expect(panned.y).toBeCloseTo(zoomed.y - 20, 6);
    expect(panned.k).toBe(zoomed.k);
    await expectGrid();
    await page.mouse.move(700, 500);
    await page.mouse.down();
    await page.mouse.move(820, 560, { steps: 12 });
    await page.mouse.up();
    await expect.poll(async () => (await state()).x).toBeCloseTo(panned.x + 120, 6);
    const dragged = await state();
    expect(dragged.y).toBeCloseTo(panned.y + 60, 6);
    expect(dragged.k).toBe(panned.k);
    await expectGrid();
    await page.mouse.move(600, 400);
    for (let index = 0; index < 5; index++) await page.mouse.wheel(0, 100);
    const expectedScale = dragged.k * Math.pow(1.1, -5);
    await expect.poll(async () => (await state()).k).toBeCloseTo(expectedScale, 6);
    const burst = await state();
    expect((600 - burst.x) / burst.k).toBeCloseTo((600 - dragged.x) / dragged.k, 6);
    expect((400 - burst.y) / burst.k).toBeCloseTo((400 - dragged.y) / dragged.k, 6);
    await expectGrid();
    await page.reload();
    await expectGrid();
    expect(await state()).toEqual(before);
});
