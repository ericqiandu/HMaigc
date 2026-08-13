# Canvas Node UI Standard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. The repository explicitly disables subagent-driven development for shared UI/state work, so execution stays inline in the current session.

**Goal:** Unify every canvas node around the current production node appearance while preserving node-specific content, dimensions, behavior, and generation flows.

**Architecture:** Keep `CanvasNode` as the single renderer and introduce one small presentation module for shared empty/loading/error/action primitives. Replace only the visual forks in generic node renderers and the audio/video-composition private CSS; custom script/config/story nodes continue to render inside the existing shell and inherit the shared typography/state contract.

**Tech Stack:** React 19, TypeScript strict mode, Tailwind utility classes, semantic `CanvasTheme` tokens, Bun test runner, Vite.

## Global Constraints

- Use the current canvas node appearance as the only visual source of truth; do not introduce a new design direction.
- Do not change node data, backend APIs, generation flows, billing, resource loading, connection rules, drag/resize behavior, or media aspect ratios.
- No compatibility layer, duplicated renderer, hardcoded model list, `any`, raw secret, mock data, or silent fallback.
- Production budget: at most 3 responsibilities, 8 production files, about 600 net new production lines; stop and redesign if the implementation exceeds any budget by 50%.
- The final delivery must rebuild the local Docker image and verify healthy containers, `/api/health`, and the current canvas URL.

---

### Task 1: Shared Node Presentation Contract

**Files:**
- Create: `web/src/components/canvas/canvas-node-ui.tsx`
- Modify: `web/src/styles/globals.css`
- Test: `web/test/canvas-node-ui-standard.test.tsx`

**Interfaces:**
- Consumes: `CanvasTheme` from `web/src/lib/canvas-theme.ts` and native React nodes/callbacks.
- Produces:
  - `CanvasNodeEmptyState({ icon, title, description?, theme })`
  - `CanvasNodeStatusLayout({ icon, title, detail?, progress?, meta?, actions?, tone, theme })`
  - `CanvasNodeAction({ icon, label, tone?, onClick })`
  - shared semantic classes `canvas-node-state`, `canvas-node-state-*`, `canvas-node-action`, and `canvas-node-content-region`.

- [ ] **Step 1: Write the failing presentation contract test**

```tsx
test("共享节点状态使用当前画布语义层级", () => {
    const markup = renderToStaticMarkup(
        <CanvasNodeStatusLayout
            icon={<LoaderCircle />}
            title="任务处理中"
            detail="生成中 · 48%"
            progress={48}
            meta="8分38秒 · b29f...8614"
            tone="progress"
            theme={canvasThemes.dark}
        />,
    );
    expect(markup).toContain("canvas-node-state");
    expect(markup).toContain("canvas-node-state-progress");
    expect(markup).toContain('role="status"');
    expect(markup).toContain('aria-valuenow="48"');
    expect(markup).not.toContain("rounded-full");
});
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `cd web && bun test test/canvas-node-ui-standard.test.tsx`

Expected: FAIL because `canvas-node-ui.tsx` and its exports do not exist.

- [ ] **Step 3: Implement the minimal shared presentation module**

Create focused, stateless components. They must only arrange real props and theme tokens; they must not inspect node types, task semantics, or global canvas state.

```tsx
export type CanvasNodeStatusTone = "neutral" | "progress" | "danger";

export function CanvasNodeAction({ icon, label, tone = "neutral", onClick }: CanvasNodeActionProps) {
    return (
        <button type="button" className={`canvas-node-action canvas-node-action-${tone}`} aria-label={label} onClick={onClick}>
            {icon}
            <span className="canvas-node-action-label">{label}</span>
        </button>
    );
}
```

Add CSS only for structure, typography, focus, and semantic state. Colors must come from inline `CanvasTheme` values or existing workspace variables; do not add a private palette.

- [ ] **Step 4: Run focused GREEN and formatting checks**

Run:

```powershell
cd web
bun test test/canvas-node-ui-standard.test.tsx
bunx prettier --check src/components/canvas/canvas-node-ui.tsx test/canvas-node-ui-standard.test.tsx
```

Expected: all focused tests pass and formatting is clean.

### Task 2: Hard-Cut Existing Node Families to the Shared Contract

**Files:**
- Modify: `web/src/components/canvas/canvas-node.tsx`
- Modify: `web/src/components/canvas/canvas-audio-node.css`
- Modify: `web/src/components/canvas/canvas-video-composition-node.css`
- Modify: `web/src/styles/globals.css`
- Test: `web/test/canvas-node-ui-standard.test.tsx`

**Interfaces:**
- Consumes: the Task 1 components and classes.
- Produces: one generic node visual path for empty/loading/error/action states and the existing `CanvasNode` shell for every `CanvasNodeType`.

- [ ] **Step 1: Add failing family coverage**

Extend the test to verify:

```tsx
test.each(["image", "video", "audio"])("%s 空态使用共享节点空态", (kind) => {
    const markup = renderNodeContentFixture(kind, "empty");
    expect(markup).toContain("canvas-node-state-empty");
});

test("音频与视频合成不再把标题悬挂在公共外壳之外", () => {
    expect(canvasNodeSource).not.toContain("canvas-audio-node-heading");
    expect(canvasNodeSource).not.toContain("canvas-video-composition-heading");
});
```

The fixture helper may render exported content helpers or validate the source contract where full `CanvasNode` browser events make SSR inappropriate. It must assert semantic output rather than snapshots of entire files.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `cd web && bun test test/canvas-node-ui-standard.test.tsx`

Expected failures:

- image/video/audio empty states still use separate markup;
- loading/error still use private button shapes;
- audio and video composition titles still render outside the shell;
- media content still repeats inner rounding.

- [ ] **Step 3: Replace generic empty/loading/error/action markup**

In `canvas-node.tsx`:

- Replace `LoadingContent` layout with `CanvasNodeStatusLayout`, preserving the actual task stage, progress, elapsed time, task ID, details, and cancellation callbacks.
- Replace `ErrorContent` with the danger tone and preserve moderation-specific guidance and retry availability.
- Render image, video, and audio empty states through `CanvasNodeEmptyState` with their real icons and labels.
- Render deferred media loading through the same neutral/progress structure without changing its resource-loading callback.
- Replace the duplicate “替换” and “放大编辑” button markup with `CanvasNodeAction` while preserving visibility and event propagation.
- Remove media child `rounded-[18px]` where the public shell already clips or draws the boundary.

- [ ] **Step 4: Remove private shell forks**

In `canvas-node.tsx` and the two private CSS files:

- Remove external `.canvas-audio-node-heading` and `.canvas-video-composition-heading` markup.
- Remove `.canvas-node-shell--audio` radius/shadow override.
- Keep audio player controls and composition editor behavior, but align their padding, title/meta typography, empty surface, and actions with shared node classes.
- Keep script/config/story/character custom renderers inside the same shell; do not rewrite their editors.

- [ ] **Step 5: Run focused family tests and the existing canvas suites**

Run:

```powershell
cd web
bun test test/canvas-node-ui-standard.test.tsx test/canvas-generation-task-state.test.ts test/canvas-project-generation.test.ts
```

Expected: all selected tests pass; task details, cancellation, retry, media loading, and content rendering remain functional.

### Task 3: Review, Production Verification, and Local Preview

**Files:**
- Modify only if Task 1–2 review finds an in-scope defect.
- Do not create browser-generated test scripts or commit preview artifacts.

**Interfaces:**
- Consumes: the complete Task 1–2 diff.
- Produces: a commercial-grade, reviewable commit and a healthy local preview image.

- [ ] **Step 1: Explicit requirements and diff review**

Check the implementation against every section of `docs/superpowers/specs/2026-08-13-canvas-node-ui-standard-design.md`:

- every node type still has a renderer;
- no business/data/connection behavior changed;
- only the current theme tokens are used;
- no duplicated shell, decorative nested border, raw private palette, `any`, or compatibility branch was introduced;
- production file count and line budget remain within the approved limits;
- drag/resize hot paths are untouched.

- [ ] **Step 2: Run final Web gates once**

Run:

```powershell
cd web
bun test
bun run build
bunx prettier --check src/components/canvas/canvas-node-ui.tsx src/components/canvas/canvas-node.tsx src/components/canvas/canvas-audio-node.css src/components/canvas/canvas-video-composition-node.css test/canvas-node-ui-standard.test.tsx
cd ..
git diff --check
```

Expected: full tests, TypeScript, Vite, bundle budgets, formatting, and diff checks pass.

- [ ] **Step 3: Inspect the real canvas after rebuilding Docker**

Run:

```powershell
.\scripts\local-compose.ps1 up -d --build --wait web
```

Then verify:

```powershell
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3000/api/health
Invoke-WebRequest -UseBasicParsing http://127.0.0.1:3000/canvas/VGLzme7HdUI3JW8X9liwP
docker ps --filter name=hmaigc-local --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
```

On the actual canvas inspect image, video, audio, text, script, config, and Frame nodes in empty/selected/generating/error/completed states where the current project has those facts. Confirm no interaction changes and no second database/data directory is created.

- [ ] **Step 4: Commit the verified implementation**

Stage only the approved production/test files and commit:

```powershell
git commit -m "style(canvas): 节点系统 - 统一全部节点 UI 标准"
```

Do not stage `.superpowers/`, `dist/`, local data, screenshots, secrets, or unrelated formatting. Do not push.
