# Canvas Rendering Performance Hard Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Do not use subagent-driven development: these tasks share node geometry, viewport, persistence, and benchmark contracts and must be executed sequentially.

**Goal:** Replace transformed canvas node surfaces and native in-node video controls with a flat, measurable rendering path that remains geometrically correct and responsive under canvas zoom, drag, resize, and large mixed-media graphs.

**Architecture:** Keep the existing DOM/SVG canvas and its CSS-variable viewport preview. Introduce one flat node surface, a custom video player, a preview/commit interaction runtime, a deferred persistence scheduler, and a spatial render index; the page commits business state only at gesture boundaries while viewport queries control which nodes, edges, and media mount.

**Tech Stack:** React 19, TypeScript 7 strict mode, Bun test + happy-dom, Vite 8, CSS custom properties, Puppeteer performance gate, Playwright cross-browser contract tests.

**Spec:** `docs/superpowers/specs/2026-09-04-canvas-rendering-performance-hard-cutover-design.md`

## Global Constraints

- Preserve the existing DOM/SVG canvas; do not migrate to React Flow, Canvas 2D, or WebGL.
- Use one geometry source for node root, surface, media, outline, and hit area.
- Remove the canvas node `CometCard` path completely; do not keep a feature flag or compatibility branch.
- Do not use native `<video controls>` inside canvas nodes.
- Pointer/wheel hot paths must not scan all nodes or edges, serialize project state, persist, call the network, or emit per-frame business logs.
- A drag or resize gesture produces at most one document commit; cancellation produces none.
- Keep billing, generation, model, asset ownership, Agent, API, and database contracts unchanged.
- Add no runtime dependency. `@playwright/test` is permitted only as pinned development tooling for Chromium/Firefox/WebKit validation and must not enter the production bundle.
- Do not add `any` or `as any`; narrow `unknown` values explicitly.
- Every JSX/TSX tag added or modified must have a descriptive named `className`.
- Node surface radius is `12px`; internal control radius is `8px`; default shadow is none; hover may use only `--shadow-sm` without changing geometry.
- Node title is `13px / 20px / 500`; metadata is at least `11px / 16px`; video controls are 32px high with 16px icons and 44px touch targets.
- Failures remain explicit and diagnosable; do not add fallback media, silent retries, or fake success states.
- Each task ends in one focused Conventional Commit after its tests pass and staged scope is reviewed; do not push.

## File Map

### Create

- `web/src/components/canvas/canvas-node-surface.tsx` — flat node surface, outline, overlay, and handle slots.
- `web/src/components/canvas/canvas-video-player.tsx` — custom canvas video playback and control state.
- `web/src/components/canvas/canvas-video-player.css` — video controls consuming existing semantic tokens.
- `web/src/lib/canvas/canvas-interaction-runtime.ts` — live drag/connection/resize DOM previews and resize geometry calculation.
- `web/src/lib/canvas/canvas-render-index.ts` — spatial grid for visible/near nodes and intersecting connections.
- `web/src/stores/canvas/canvas-persistence-scheduler.ts` — delayed serialization and explicit flush contract.
- `web/test/canvas-node-surface.test.tsx` — flat surface structural contract.
- `web/test/canvas-video-player.test.tsx` — custom controls and media error contract.
- `web/test/canvas-interaction-runtime.test.ts` — preview/commit calculation and DOM mutation contract.
- `web/test/canvas-render-index.test.ts` — visibility, boundary, hidden-node, and connection queries.
- `web/test/canvas-persistence-scheduler.test.ts` — coalescing, deferred serialization, flush, and write failure tests.
- `web/e2e/canvas-media-rendering.spec.ts` — real-browser geometry and media-control assertions.
- `web/playwright.config.ts` — Chromium, Firefox, and WebKit projects against the deterministic fixture.

### Modify

- `web/src/components/canvas/canvas-node.tsx` — consume flat surface, remove Comet behavior, commit resize once.
- `web/src/components/canvas/canvas-frame-node.tsx` — remove Comet behavior and use the same resize commit boundary.
- `web/src/components/canvas/canvas-media-node-content.tsx` — render `CanvasVideoPlayer` and respect media visibility tier.
- `web/src/pages/canvas/use-canvas-selection-controller.ts` — consume interaction runtime for live drag and one final commit.
- `web/src/pages/canvas/use-canvas-node-editor.ts` — rename resize callback to commit semantics.
- `web/src/pages/canvas/use-canvas-render-model.ts` — replace viewport-wide array filtering with render-index queries.
- `web/src/pages/canvas/canvas-project-world-layers.tsx` — render indexed nodes/edges and pass media tier.
- `web/src/pages/canvas/project.tsx` — wire renamed commit and render-model contracts.
- `web/src/pages/canvas/shared.tsx` — consume the flat node surface contract in read-only canvases.
- `web/src/stores/canvas/use-canvas-store.ts` — schedule object serialization outside the setter call stack.
- `web/src/styles/globals.css` — remove canvas Comet selectors and implement the 12px single-surface state styles.
- `web/src/styles/canvas-chrome.css` — map the flat surface to canvas typography and interaction state.
- `web/test/canvas-performance-hot-paths.test.ts` — enforce no per-frame full-state work.
- `web/test/canvas-media-streaming.test.tsx` — update video markup and visibility-tier expectations.
- `web/test/canvas-node-ui-standard.test.tsx` — lock the no-Comet/no-heavy-shadow visual contract.
- `web/perf/canvas-performance-fixture.tsx` — mixed node/media/edge datasets and all gesture samplers.
- `web/scripts/run-canvas-performance-benchmark.mjs` — run 50/200/500 scale scenarios and report every gesture.
- `web/scripts/canvas-performance-benchmark-domain.mjs` — budgets and relative-regression assessment.
- `web/test/canvas-performance-benchmark-domain.test.ts` — budget-domain coverage.
- `web/package.json` and `web/bun.lock` — cross-browser script and development dependency only.
- `web/DESIGN.md` — record the flat node, one-surface, custom control, and zero-hot-path-work contract.

### Delete

- `web/src/lib/canvas/canvas-drag-performance.ts` — responsibilities move to `canvas-interaction-runtime.ts`; no re-export compatibility wrapper.

---

### Task 1: Build the red-capable real-browser rendering loop

**Files:**
- Modify: `web/package.json`
- Modify: `web/bun.lock`
- Create: `web/playwright.config.ts`
- Create: `web/e2e/canvas-media-rendering.spec.ts`
- Modify: `web/perf/canvas-performance-fixture.tsx`
- Modify: `web/perf/canvas-performance.html`

**Interfaces:**
- Consumes: existing `CanvasNode`, `web/public/videos/hero.mp4`, and the Vite performance fixture.
- Produces: query parameter `scenario=media-contract`; stable selectors `data-canvas-node-surface`, `data-canvas-video-player`, and `data-canvas-video-controls`; command `bun run verify:canvas-media-rendering`.

- [ ] **Step 1: Install and pin browser-test tooling**

Run:

```bash
cd web
bun add --dev --exact @playwright/test
bunx playwright install chromium firefox webkit
```

Expected: `package.json` contains only a devDependency change and `bun.lock` pins its resolved version; production dependencies are unchanged.

- [ ] **Step 2: Add the deterministic media fixture mode**

Add a media fixture node that uses the repository-owned video:

```tsx
const mediaContractNode: CanvasNodeData = {
    id: "benchmark-video-contract",
    type: CanvasNodeType.Video,
    title: "视频渲染契约",
    position: { x: 320, y: 180 },
    width: 1280,
    height: 720,
    metadata: { content: "/videos/hero.mp4", status: "success" },
};
```

Render it under a world transform of `scale(0.55)`, set `data-canvas-benchmark-ready="true"` after two animation frames, and do not use application authentication or remote APIs.

- [ ] **Step 3: Configure three browser projects**

Create `web/playwright.config.ts` with one web server and three projects:

```ts
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
    testDir: "./e2e",
    fullyParallel: false,
    retries: 0,
    use: { baseURL: "http://127.0.0.1:4178", viewport: { width: 1440, height: 900 } },
    webServer: { command: "bunx vite --host 127.0.0.1 --port 4178 --strictPort", port: 4178, reuseExistingServer: false },
    projects: [
        { name: "chromium", use: { ...devices["Desktop Chrome"] } },
        { name: "firefox", use: { ...devices["Desktop Firefox"] } },
        { name: "webkit", use: { ...devices["Desktop Safari"] } },
    ],
});
```

Add `"verify:canvas-media-rendering": "playwright test e2e/canvas-media-rendering.spec.ts"` to `scripts`.

- [ ] **Step 4: Write the exact failing geometry test**

Create this test around stable selectors, without screenshot-only assertions:

```ts
import { expect, test } from "@playwright/test";

test("hover and zoom keep the video, surface, and node on one geometry", async ({ page }) => {
    await page.goto("/perf/canvas-performance.html?scenario=media-contract");
    await page.locator("html[data-canvas-benchmark-ready='true']").waitFor();
    const node = page.locator("[data-node-id='benchmark-video-contract']");
    const surface = node.locator("[data-canvas-node-surface]");
    const video = node.locator("video");
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
```

- [ ] **Step 5: Run the loop and record RED**

Run:

```bash
cd web
bun run verify:canvas-media-rendering --project=chromium
```

Expected: FAIL because the current node has no `data-canvas-node-surface`, hover adds a 3D transform, the video has native controls, and no custom control layer exists.

- [ ] **Step 6: Commit only the red-capable harness**

```bash
git add web/package.json web/bun.lock web/playwright.config.ts web/e2e/canvas-media-rendering.spec.ts web/perf/canvas-performance-fixture.tsx web/perf/canvas-performance.html
git diff --cached --check
git commit -m "test(web): reproduce canvas media geometry regression"
```

---

### Task 2: Hard-cut canvas nodes to one flat surface

**Files:**
- Create: `web/src/components/canvas/canvas-node-surface.tsx`
- Modify: `web/src/components/canvas/canvas-node.tsx`
- Modify: `web/src/components/canvas/canvas-frame-node.tsx`
- Modify: `web/src/styles/globals.css`
- Modify: `web/src/styles/canvas-chrome.css`
- Create: `web/test/canvas-node-surface.test.tsx`
- Modify: `web/test/canvas-node-ui-standard.test.tsx`

**Interfaces:**
- Consumes: `CanvasTheme`, node selection/related/generation/error facts, and existing overlay/handle children.
- Produces: `CanvasNodeSurface`, `CanvasNodeVisualState`, and stable `data-canvas-node-surface`/`data-state` attributes.

- [ ] **Step 1: Write the flat-surface contract test**

```tsx
const markup = renderToStaticMarkup(
    <CanvasNodeSurface state="selected" theme={canvasThemes.dark} flushContent>
        <span className="test-node-content">内容</span>
    </CanvasNodeSurface>,
);
expect(markup).toContain("data-canvas-node-surface");
expect(markup).toContain('data-state="selected"');
expect(markup).not.toContain("aceternity-comet");
```

Also read `canvas-node.tsx`, `canvas-frame-node.tsx`, and `globals.css` and assert the canvas paths contain no `CometCard`, `matrix3d`, `rotateX`, glare selector, or heavy canvas-node shadow literal.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
cd web
bun test test/canvas-node-surface.test.tsx test/canvas-node-ui-standard.test.tsx
```

Expected: FAIL because the component does not exist and both canvas node families still use `CometCard`.

- [ ] **Step 3: Implement the flat surface**

Use this public contract:

```ts
export type CanvasNodeVisualState = "idle" | "related" | "selected" | "target" | "loading" | "error";

export type CanvasNodeSurfaceProps = PropsWithChildren<{
    state: CanvasNodeVisualState;
    theme: CanvasTheme;
    flushContent?: boolean;
    className?: string;
    onMouseDown?: MouseEventHandler<HTMLDivElement>;
    onDoubleClick?: MouseEventHandler<HTMLDivElement>;
}>;
```

Render one `canvas-node-surface relative size-full overflow-clip rounded-[12px]`, one non-layout outline, and slots for current children. Preserve existing facts and actions, but move resize handles and external overlays outside the clipped surface. Remove `CometCard` imports and props from both `CanvasNode` and `CanvasFrameNode`; keep the wallet use of `CometCard` untouched.

- [ ] **Step 4: Replace visual rules with semantic tokens**

Delete `.canvas-node-shell.aceternity-comet-card` rules. Define default no-shadow, 12px surface radius, 2px selected outline, and `--shadow-sm` hover without transform. Ensure `[data-canvas-viewport-interacting='true']` disables only transitions/animations that still exist.

- [ ] **Step 5: Run focused tests and the browser geometry probe**

```bash
cd web
bun test test/canvas-node-surface.test.tsx test/canvas-node-ui-standard.test.tsx
bun run verify:canvas-media-rendering --project=chromium
```

Expected: unit tests PASS; browser test still FAILS only on native controls/custom-control assertions, while node/surface/video geometry and `transform: none` are green.

- [ ] **Step 6: Commit the flat surface**

```bash
git add web/src/components/canvas/canvas-node-surface.tsx web/src/components/canvas/canvas-node.tsx web/src/components/canvas/canvas-frame-node.tsx web/src/styles/globals.css web/src/styles/canvas-chrome.css web/test/canvas-node-surface.test.tsx web/test/canvas-node-ui-standard.test.tsx
git diff --cached --check
git commit -m "refactor(web): hard-cut canvas nodes to flat surfaces"
```

---

### Task 3: Replace native video controls with CanvasVideoPlayer

**Files:**
- Create: `web/src/components/canvas/canvas-video-player.tsx`
- Create: `web/src/components/canvas/canvas-video-player.css`
- Modify: `web/src/components/canvas/canvas-media-node-content.tsx`
- Modify: `web/test/canvas-media-streaming.test.tsx`
- Create: `web/test/canvas-video-player.test.tsx`

**Interfaces:**
- Consumes: playable URL, node title, preload policy, and a typed playback error callback.
- Produces: `CanvasVideoPlayerProps` and `data-canvas-video-player`/`data-canvas-video-controls` elements.

- [ ] **Step 1: Write custom-player behavior tests**

Use happy-dom and stub media methods without `any`:

```tsx
Object.defineProperty(HTMLMediaElement.prototype, "play", {
    configurable: true,
    value: () => Promise.resolve(),
});
Object.defineProperty(HTMLMediaElement.prototype, "pause", {
    configurable: true,
    value: () => undefined,
});

expect(requiredVideo().hasAttribute("controls")).toBeFalse();
expect(document.querySelector("[data-canvas-video-controls]")).not.toBeNull();
```

Cover play/pause, duration/time projection, seek, mute, fullscreen rejection, `error`, keyboard shortcuts, and propagation isolation.

- [ ] **Step 2: Run tests and verify RED**

```bash
cd web
bun test test/canvas-video-player.test.tsx test/canvas-media-streaming.test.tsx
```

Expected: FAIL because `CanvasVideoPlayer` and custom controls do not exist.

- [ ] **Step 3: Implement the player contract**

```ts
export type CanvasVideoPlayerProps = {
    nodeId: string;
    src: string;
    title: string;
    preload: "none" | "metadata";
    onPlaybackError: (error: CanvasVideoPlaybackError) => void;
};

export type CanvasVideoPlaybackError = {
    kind: "media" | "fullscreen";
    message: string;
};
```

The `<video>` uses `playsInline`, no `controls`, `className="canvas-video-player-media"`, and no autoplay. The control layer is an ordinary 100%-width DOM sibling with play/pause, range input, elapsed/duration, mute, and fullscreen buttons. Every control stops pointer/mouse/wheel propagation before calling the media API. A rejected media call must set a visible `role="alert"` message and call `onPlaybackError`; `CanvasMediaNodeContent` records one structured error containing `nodeId`, a credential-free resource URL summary, error kind, and message.

- [ ] **Step 4: Style the controls without new visual tokens**

Import `canvas-video-player.css` from the component. Use existing surface, text, muted, focus, danger, radius, and shadow variables; 32px desktop height and 44px coarse-pointer targets. Do not add blur, glare, gradient, nested border, or private hex colors.

- [ ] **Step 5: Run unit and three-browser geometry tests**

```bash
cd web
bun test test/canvas-video-player.test.tsx test/canvas-media-streaming.test.tsx
bun run verify:canvas-media-rendering
```

Expected: all tests PASS in Chromium, Firefox, and WebKit; surface, video, and node boxes match after hover at 55% scale.

- [ ] **Step 6: Commit the custom player**

```bash
git add web/src/components/canvas/canvas-video-player.tsx web/src/components/canvas/canvas-video-player.css web/src/components/canvas/canvas-media-node-content.tsx web/test/canvas-video-player.test.tsx web/test/canvas-media-streaming.test.tsx
git diff --cached --check
git commit -m "feat(web): add stable canvas video controls"
```

---

### Task 4: Make drag and resize preview-only until commit

**Files:**
- Create: `web/src/lib/canvas/canvas-interaction-runtime.ts`
- Delete: `web/src/lib/canvas/canvas-drag-performance.ts`
- Modify: `web/src/components/canvas/canvas-node.tsx`
- Modify: `web/src/components/canvas/canvas-frame-node.tsx`
- Modify: `web/src/pages/canvas/use-canvas-selection-controller.ts`
- Modify: `web/src/pages/canvas/use-canvas-node-editor.ts`
- Modify: `web/src/pages/canvas/canvas-project-world-layers.tsx`
- Modify: `web/src/pages/canvas/project.tsx`
- Create: `web/test/canvas-interaction-runtime.test.ts`
- Modify: `web/test/canvas-performance-hot-paths.test.ts`

**Interfaces:**
- Consumes: initial node geometry, pointer delta, scale, corner, minimum size, and optional aspect ratio.
- Produces: `CanvasResizePreview`, DOM preview functions, and `onResizeCommit(nodeId, width, height, position)`.

- [ ] **Step 1: Write pure resize and DOM-preview tests**

```ts
const preview = calculateCanvasResizePreview({
    initial: { x: 100, y: 80, width: 640, height: 360 },
    delta: { x: -64, y: -36 },
    corner: "top-left",
    minWidth: 220,
    minHeight: 160,
    aspectRatio: 16 / 9,
});
expect(preview).toEqual({ x: 164, y: 116, width: 576, height: 324 });
```

Apply 100 previews to a fake element, assert CSS variables change, and assert no commit callback is part of the runtime API. Cover non-finite input rejection and clear restoring all preview properties.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
cd web
bun test test/canvas-interaction-runtime.test.ts test/canvas-performance-hot-paths.test.ts
```

Expected: FAIL because the runtime is absent and resize still invokes `onResize` during mousemove.

- [ ] **Step 3: Move live interaction functions into the runtime**

Export exact typed functions:

```ts
export function calculateCanvasResizePreview(input: CanvasResizeInput): CanvasResizePreview;
export function applyCanvasLiveResize(target: HTMLElement, preview: CanvasResizePreview): void;
export function clearCanvasLiveResize(target: HTMLElement): void;
export function applyCanvasLiveNodeDrag(targets: readonly HTMLElement[], offset: Position): void;
export function clearCanvasLiveNodeDrag(targets: readonly HTMLElement[]): void;
export function applyCanvasLiveConnectionDrag(targets: readonly CanvasLiveConnectionDragTarget[], offset: Position): void;
export function clearCanvasLiveConnectionDrag(targets: readonly CanvasLiveConnectionDragTarget[]): void;
```

Move the existing drag/connection implementation, update all imports, and delete the old file rather than re-exporting it.

- [ ] **Step 4: Convert both resize implementations to preview/commit**

On mousemove, calculate the latest preview and write CSS variables only. On mouseup, clear CSS variables and call the renamed `onResizeCommit` once with the last valid preview. On pointer cancellation/unmount, clear without commit. Keep aspect ratio and minimum-size rules identical.

- [ ] **Step 5: Prove the hot path is state-free**

Extend the source contract test to assert `handleResizeMove` contains `applyCanvasLiveResize` and does not contain `onResizeCommit`, `setNodes`, `updateProject`, `JSON.stringify`, `fetch`, or API calls. Then run:

```bash
cd web
bun test test/canvas-interaction-runtime.test.ts test/canvas-performance-hot-paths.test.ts
```

Expected: PASS; 100 mouse moves yield zero document commits and mouseup yields exactly one.

- [ ] **Step 6: Commit the interaction boundary**

```bash
git add web/src/lib/canvas/canvas-interaction-runtime.ts web/src/lib/canvas/canvas-drag-performance.ts web/src/components/canvas/canvas-node.tsx web/src/components/canvas/canvas-frame-node.tsx web/src/pages/canvas/use-canvas-selection-controller.ts web/src/pages/canvas/use-canvas-node-editor.ts web/src/pages/canvas/canvas-project-world-layers.tsx web/src/pages/canvas/project.tsx web/test/canvas-interaction-runtime.test.ts web/test/canvas-performance-hot-paths.test.ts
git diff --cached --check
git commit -m "refactor(web): commit canvas geometry at gesture boundaries"
```

---

### Task 5: Defer project serialization out of store setters

**Files:**
- Create: `web/src/stores/canvas/canvas-persistence-scheduler.ts`
- Modify: `web/src/stores/canvas/use-canvas-store.ts`
- Create: `web/test/canvas-persistence-scheduler.test.ts`
- Modify: `web/test/canvas-performance-hot-paths.test.ts`

**Interfaces:**
- Consumes: the latest `StorageValue<CanvasStore>`, storage key, serializer, and async writer.
- Produces: `CanvasPersistenceScheduler<T>` with `schedule`, `flush`, `retry`, `getSnapshot`, and `dispose`.

- [ ] **Step 1: Write coalescing and failure tests**

```ts
const scheduler = createCanvasPersistenceScheduler({
    delayMs: 0,
    serialize: (value: { revision: number }) => JSON.stringify(value),
    write: async (name, payload) => writes.push({ name, payload }),
});
scheduler.schedule("canvas", { revision: 1 });
scheduler.schedule("canvas", { revision: 2 });
expect(writes).toEqual([]);
await scheduler.flush();
expect(writes).toEqual([{ name: "canvas", payload: '{"revision":2}' }]);
```

Also test that serialization has not run before the scheduled task/flush, state transitions through `idle -> saving -> saved`, writer rejection is retained as `failed` and propagates from `flush`, `retry` writes the retained payload, and `dispose` prevents a pending timer from writing.

- [ ] **Step 2: Run tests and verify RED**

```bash
cd web
bun test test/canvas-persistence-scheduler.test.ts test/canvas-performance-hot-paths.test.ts
```

Expected: FAIL because the scheduler is absent and `setItem` synchronously calls `JSON.stringify`.

- [ ] **Step 3: Implement the generic scheduler**

```ts
export type CanvasPersistenceScheduler<T> = {
    schedule: (name: string, value: T) => void;
    flush: () => Promise<void>;
    retry: () => Promise<void>;
    getSnapshot: () => CanvasPersistenceSnapshot;
    dispose: () => void;
};

export type CanvasPersistenceSnapshot =
    | { status: "idle" | "saving" | "saved"; error: null }
    | { status: "failed"; error: Error };

export function createCanvasPersistenceScheduler<T>(options: {
    delayMs: number;
    serialize: (value: T) => string;
    write: (name: string, payload: string) => Promise<void>;
}): CanvasPersistenceScheduler<T>;
```

Store only the newest object before the timer fires. Serialize inside the timer or `flush`, never inside `schedule`. Preserve the last failed payload and `Error` in the snapshot; `retry` must attempt that exact payload and must not silently discard or replace the failure.

- [ ] **Step 4: Wire the Zustand storage adapter**

Replace `queuedPersistValue` and module-level timer code with one scheduler instance. `setItem` performs only reference deduplication and `schedule(name, value)`; `flushCanvasStorePersistence` calls `scheduler.flush()`. Export `getCanvasStorePersistenceSnapshot()` and `retryCanvasStorePersistence()` as the explicit diagnostic/retry boundary; do not place persistence status inside the persisted Zustand state.

- [ ] **Step 5: Run persistence and hot-path tests**

```bash
cd web
bun test test/canvas-persistence-scheduler.test.ts test/canvas-persistence-policy.test.ts test/canvas-performance-hot-paths.test.ts
```

Expected: PASS; source contract confirms `canvasStorage.setItem` has no `JSON.stringify`.

- [ ] **Step 6: Commit the persistence scheduler**

```bash
git add web/src/stores/canvas/canvas-persistence-scheduler.ts web/src/stores/canvas/use-canvas-store.ts web/test/canvas-persistence-scheduler.test.ts web/test/canvas-performance-hot-paths.test.ts
git diff --cached --check
git commit -m "perf(web): defer canvas project serialization"
```

---

### Task 6: Index visible nodes and connections

**Files:**
- Create: `web/src/lib/canvas/canvas-render-index.ts`
- Modify: `web/src/pages/canvas/use-canvas-render-model.ts`
- Modify: `web/src/pages/canvas/canvas-project-world-layers.tsx`
- Modify: `web/src/pages/canvas/project.tsx`
- Create: `web/test/canvas-render-index.test.ts`
- Modify: `web/test/canvas-performance-hot-paths.test.ts`

**Interfaces:**
- Consumes: committed nodes/connections and world-space visible/near bounds.
- Produces: `CanvasRenderIndex.query(bounds)` returning ordered node ids, media tiers, and visible connection ids.

- [ ] **Step 1: Write spatial query tests**

Use a 10,000-node grid with boundary cases and connections whose endpoints are inside, crossing, near, and far outside. Assert exact returned ids, stable frame-before-content ordering, collapsed batch/Frame exclusion, and no duplicate connection ids.

```ts
const index = createCanvasRenderIndex(nodes, connections, hiddenNodeIds);
const result = index.query({ visible: { left: 0, top: 0, right: 1440, bottom: 900 }, nearPadding: 480 });
expect(result.visibleNodeIds).toEqual(["frame-1", "node-1", "node-2"]);
expect(result.nearNodeIds).toEqual(["node-3"]);
expect(result.visibleConnectionIds).toEqual(["edge-inside", "edge-crossing"]);
```

- [ ] **Step 2: Run tests and verify RED**

```bash
cd web
bun test test/canvas-render-index.test.ts test/canvas-performance-hot-paths.test.ts
```

Expected: FAIL because viewport culling still filters the full nodes array and all valid connections render.

- [ ] **Step 3: Implement a fixed-cell spatial index**

Expose this contract:

```ts
export type CanvasWorldRect = { left: number; top: number; right: number; bottom: number };
export type CanvasMediaVisibilityTier = "visible" | "near";
export type CanvasRenderQuery = {
    nodeIds: readonly string[];
    mediaTierByNodeId: ReadonlyMap<string, CanvasMediaVisibilityTier>;
    connectionIds: readonly string[];
};
export type CanvasRenderIndex = {
    query: (input: { visible: CanvasWorldRect; nearPadding: number }) => CanvasRenderQuery;
};
export function createCanvasRenderIndex(nodes: readonly CanvasNodeData[], connections: readonly CanvasConnection[], hiddenNodeIds: ReadonlySet<string>): CanvasRenderIndex;
```

Use fixed world-space cells, index every node AABB once per committed graph, and index connection Bezier AABBs from endpoint/control points. Query only intersecting cells, then perform exact AABB checks. Sort results by original graph order so rendering remains deterministic.

- [ ] **Step 4: Replace render-model scans**

Build the index only when committed node geometry, connection structure, or hidden ids change. On viewport change, calculate visible bounds and call `index.query`; map ids through the existing `nodeById`/connection map. Remove the old `visibleNodes` full-array filter and full `displayConnections` render path.

- [ ] **Step 5: Run index, hot-path, and connection tests**

```bash
cd web
bun test test/canvas-render-index.test.ts test/canvas-performance-hot-paths.test.ts test/canvas-connection-contract.test.ts test/canvas-connection-hit-index.test.ts
```

Expected: PASS; viewport changes contain no `nodes.filter`, `nodes.forEach`, `connections.flatMap`, or full-graph serialization.

- [ ] **Step 6: Commit spatial culling**

```bash
git add web/src/lib/canvas/canvas-render-index.ts web/src/pages/canvas/use-canvas-render-model.ts web/src/pages/canvas/canvas-project-world-layers.tsx web/src/pages/canvas/project.tsx web/test/canvas-render-index.test.ts web/test/canvas-performance-hot-paths.test.ts
git diff --cached --check
git commit -m "perf(web): cull canvas nodes and connections spatially"
```

---

### Task 7: Apply visible/near media mount tiers

**Files:**
- Modify: `web/src/components/canvas/canvas-node.tsx`
- Modify: `web/src/components/canvas/canvas-media-node-content.tsx`
- Modify: `web/src/pages/canvas/canvas-project-world-layers.tsx`
- Modify: `web/src/pages/canvas/shared.tsx`
- Modify: `web/test/canvas-media-streaming.test.tsx`
- Modify: `web/test/deferred-media-loading.test.tsx`

**Interfaces:**
- Consumes: `CanvasMediaVisibilityTier` from Task 6.
- Produces: `mediaVisibilityTier` prop on `CanvasNode` and `CanvasMediaNodeContent`.

- [ ] **Step 1: Write media-tier tests**

```tsx
const nearMarkup = renderToStaticMarkup(
    <CanvasMediaNodeContent node={videoNode} theme={canvasThemes.dark} mediaVisibilityTier="near" />,
);
expect(nearMarkup).not.toContain("<video");
expect(nearMarkup).toContain("data-canvas-deferred-media");

const visibleMarkup = renderToStaticMarkup(
    <CanvasMediaNodeContent node={videoNode} theme={canvasThemes.dark} mediaVisibilityTier="visible" />,
);
expect(visibleMarkup).toContain("data-canvas-video-player");
```

Assert images remain `loading="lazy"`/`decoding="async"`, near video has no media element or listeners, and changing visible→near pauses before unmount.

- [ ] **Step 2: Run focused tests and verify RED**

```bash
cd web
bun test test/canvas-media-streaming.test.tsx test/deferred-media-loading.test.tsx
```

Expected: FAIL because media tier is not part of the current renderer contract.

- [ ] **Step 3: Implement tier projection**

Pass `mediaTierByNodeId.get(node.id) ?? "visible"` only for indexed render nodes. Visible media renders the real image/player. Near video renders the existing semantic node surface with a lightweight `data-canvas-deferred-media` marker and no `<video>`; no fake thumbnail is invented. When leaving visible, pause the player in cleanup before it unmounts.

- [ ] **Step 4: Preserve read-only canvas behavior**

The shared canvas has no spatial index in this milestone; explicitly pass `mediaVisibilityTier="visible"` so its current small-document behavior remains deterministic without introducing a second culling implementation.

- [ ] **Step 5: Run media and node tests**

```bash
cd web
bun test test/canvas-media-streaming.test.tsx test/deferred-media-loading.test.tsx test/canvas-node-surface.test.tsx test/canvas-video-player.test.tsx
```

Expected: PASS; only visible video nodes mount a video element.

- [ ] **Step 6: Commit media tiering**

```bash
git add web/src/components/canvas/canvas-node.tsx web/src/components/canvas/canvas-media-node-content.tsx web/src/pages/canvas/canvas-project-world-layers.tsx web/src/pages/canvas/shared.tsx web/test/canvas-media-streaming.test.tsx web/test/deferred-media-loading.test.tsx
git diff --cached --check
git commit -m "perf(web): tier canvas media mounting by visibility"
```

---

### Task 8: Expand performance gates and close the design contract

**Files:**
- Modify: `web/perf/canvas-performance-fixture.tsx`
- Modify: `web/scripts/run-canvas-performance-benchmark.mjs`
- Modify: `web/scripts/canvas-performance-benchmark-domain.mjs`
- Modify: `web/test/canvas-performance-benchmark-domain.test.ts`
- Modify: `web/test/canvas-performance-hot-paths.test.ts`
- Modify: `web/DESIGN.md`

**Interfaces:**
- Consumes: flat surface, custom player, interaction runtime, render index, and media tiers from Tasks 2–7.
- Produces: benchmark scenarios `pan`, `zoom`, `drag-one`, `drag-20`, `resize`, `video-controls`; JSON report per scale/scenario.

- [ ] **Step 1: Write benchmark-domain tests for all scales and gestures**

```ts
expect(Object.keys(CANVAS_PERFORMANCE_BUDGETS).map(Number)).toEqual([50, 200, 500]);
expect(assessCanvasPerformanceBenchmark(greenMeasurement).passed).toBeTrue();
expect(assessCanvasPerformanceBenchmark({ ...greenMeasurement, p95FrameIntervalMs: 20.01 }).failures)
    .toContain("P95 帧间隔 20.01ms 超过 20ms");
expect(assessRelativeRegression({ baseline: 16, current: 18.5, maximumRatio: 1.15 }).passed).toBeFalse();
```

Cover every failure dimension and unsupported scale/scenario; do not silently accept absent measurements.

- [ ] **Step 2: Run benchmark-domain tests and verify RED**

```bash
cd web
bun test test/canvas-performance-benchmark-domain.test.ts
```

Expected: FAIL because the current domain supports only 500/1,000 nodes and drag-only metrics.

- [ ] **Step 3: Build mixed graph fixtures and gesture samplers**

For each scale, deterministically create the exact design counts: 50 nodes/50 media/300 edges, 200 nodes/100 media/1,000 edges, and 500 nodes/200 media/3,000 edges. Use repository assets, fixed timestamps, no random values, and no network. Expose:

```ts
window.runCanvasBenchmarkScenario = (scenario: CanvasBenchmarkScenario) => Promise<CanvasGestureSamples>;
```

Each scenario collects at least 120 frames, Long Tasks, mounted node count, mounted edge count, mounted video count, and a hot-path violation counter instrumented at persistence/network boundaries.

- [ ] **Step 4: Enforce absolute and relative budgets**

Run three samples per scale/scenario, take medians, and fail on: P95 >20ms, any >50ms Long Task during the gesture, any project serialization/network/hot-path scan violation, offscreen mounted video, or >15% regression from the checked-in baseline for CI-stable metrics. Do not overwrite the baseline automatically; baseline updates require an explicit reviewed change.

- [ ] **Step 5: Update the Web design execution contract**

Add a “画布节点渲染与性能” section to `web/DESIGN.md` that records: one 12px clipped surface, no canvas Comet/3D/glare/heavy shadow, custom video controls, gesture-boundary commits, spatial node/edge culling, visible/near/offscreen media tiers, and the 20ms/50ms performance gates.

- [ ] **Step 6: Run focused tests, then expensive final gates once**

```bash
cd web
bun test test/canvas-node-surface.test.tsx test/canvas-video-player.test.tsx test/canvas-interaction-runtime.test.ts test/canvas-persistence-scheduler.test.ts test/canvas-render-index.test.ts test/canvas-media-streaming.test.tsx test/deferred-media-loading.test.tsx test/canvas-performance-hot-paths.test.ts test/canvas-performance-benchmark-domain.test.ts
bun run verify:canvas-media-rendering
bun run benchmark:canvas
bun test
bun run build
git diff --check
```

Expected: every command exits 0; Playwright passes Chromium/Firefox/WebKit; all three benchmark scales and six gestures meet their budgets; production bundle contains no Playwright code.

- [ ] **Step 7: Perform the required independent review and one concentrated repair pass**

Review the original screenshot symptom, this spec, the actual diff, geometry and media contracts, persistence behavior, benchmark reports, accessibility, and rollback boundaries. Classify findings as current-diff defects, existing debt, or new scope. Fix current-diff defects once, rerun only affected focused gates, then rerun the complete final gate once. If the directed re-review still finds a new cross-module Critical/Important issue, stop and return to architecture rather than adding a third patch loop.

- [ ] **Step 8: Commit the final benchmark and documentation milestone**

```bash
git add web/perf/canvas-performance-fixture.tsx web/scripts/run-canvas-performance-benchmark.mjs web/scripts/canvas-performance-benchmark-domain.mjs web/test/canvas-performance-benchmark-domain.test.ts web/test/canvas-performance-hot-paths.test.ts web/DESIGN.md
git diff --cached --check
git commit -m "test(web): enforce canvas rendering performance gates"
```

## Final Acceptance Record

Before merge or deployment, attach these facts to the final handoff:

- Commit ids for all eight milestones and `git status --short` output.
- The original RED command/output and final GREEN command/output for the media geometry regression.
- Chromium, Firefox, and WebKit rendering results at 25%, 55%, 100%, and 200% scale.
- Benchmark JSON summaries for every scale/scenario, including P95, maximum interval, Long Tasks, DOM nodes, mounted edges, and mounted videos.
- Confirmation that billing, generation, Agent, asset ownership, API, and database files did not change.
- Confirmation that no native canvas video controls, canvas Comet dependency, compatibility flag, hidden fallback, or debug instrumentation remains.
