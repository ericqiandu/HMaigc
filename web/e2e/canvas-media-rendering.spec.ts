import { expect, test } from "@playwright/test";

test.setTimeout(120_000);

test("hover and zoom keep the video, surface, and node on one geometry", async ({ page }) => {
    await page.goto("/perf/canvas-performance.html?scenario=media-contract", { waitUntil: "commit" });
    await page.locator("html[data-canvas-benchmark-ready='true']").waitFor();

    const node = page.locator("[data-node-id='benchmark-video-contract']");
    const surface = node.locator("[data-canvas-node-surface]");
    const video = node.locator("video");

    await expect(node).toHaveCount(1);
    await expect(surface).toHaveCount(1, { timeout: 5_000 });
    await node.hover();
    const [nodeBox, surfaceBox, videoBox, transform] = await Promise.all([
        node.boundingBox(),
        surface.boundingBox(),
        video.boundingBox(),
        surface.evaluate((element) => getComputedStyle(element).transform),
    ]);

    expect(surfaceBox).toEqual(nodeBox);
    expect(videoBox).toEqual(nodeBox);
    expect(transform).toBe("none");
    await expect(video).not.toHaveAttribute("controls", "");
    await expect(node.locator("[data-canvas-video-controls]")).toHaveCount(1);
});
