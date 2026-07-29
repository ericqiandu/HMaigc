# Agent 面板 LibTV 风格设计 QA

- Source visual truth: `qa-artifacts/agent-panel-lib-style-20260729/01-lib-agent-reference.jpg`
- Implementation screenshot: `qa-artifacts/agent-panel-lib-style-20260729/04-hmaigc-agent-final.jpg`
- Full-view comparison: `qa-artifacts/agent-panel-lib-style-20260729/05-final-side-by-side.jpg`
- Focused panel comparison: `qa-artifacts/agent-panel-lib-style-20260729/06-panel-focused.jpg`
- Viewport: 1080 × 900 CSS px
- Source pixels: 1080 × 900
- Implementation pixels: 1080 × 900
- Device scale factor: 1
- State: dark canvas, Agent panel open, existing conversation visible, empty composer

## Findings

No actionable P0, P1, or P2 differences remain for the requested scope.

- Fonts and typography: the implementation uses the product's 13–14 px / 500–700 hierarchy. Agent Markdown now renders headings, emphasis, lists and GFM tables instead of exposing raw Markdown symbols.
- Spacing and layout rhythm: the panel is a flush right-side drawer with a 48 px header, compact 28 px icon actions, a dense message stream and a bottom-pinned composer. The previous floating-card margin and oversized 520 px default width were removed.
- Colors and visual tokens: panel, cards, buttons and composer use the existing canvas light/dark theme tokens. Dark mode surface brightness now matches the reference hierarchy without introducing a separate gray palette.
- Image and icon fidelity: all controls use the existing Lucide icon library or supplied OpenAI model mark. No placeholder art, handcrafted SVG or CSS-drawn icon was introduced.
- Copy and content: supported actions remain explicit. The backend-managed Agent text model remains hidden; the composer exposes only real image/video generation models from the system model catalog.
- Accessibility: icon-only controls have `aria-label`, tooltips and visible pressed states. Composer focus, disabled send state and history/confirmation toggles remain operable.

## Comparison History

### Initial pass

- P1: Agent replies displayed raw Markdown tables and emphasis markers, unlike the readable LibTV conversation stream.
- P2: the panel used a floating rounded card, a 520 px default width and a visually heavy two-level header.
- P2: header actions mixed a text switch, circular buttons and a separate tab row.
- P2: the composer exposed the concrete system model and used a heavier nested-card treatment.

### Fixes

- Added safe React Markdown + GFM rendering and dense table/list typography.
- Converted the panel to a flush drawer with a 420 px default width and one subtle separating edge.
- Consolidated new conversation, history, undo, execution confirmation and collapse into one compact icon toolbar.
- Reworked user messages, tool cards, proposal actions and composer controls into the reference's flat neutral hierarchy.
- Removed the user-facing Agent text-model configuration while retaining backend model resolution.

### Post-fix evidence

The full-view and focused comparisons show aligned panel proportions, header density, icon-button scale, neutral surface hierarchy, rich message formatting and bottom composer structure. Remaining content differences come from different real conversations, not component drift.

## Interaction Verification

- History view opens and returns to chat.
- Execution confirmation toggles off and back on with a visible pressed state.
- Send activates when text is entered and returns to disabled after clearing.
- Composer input was cleared after verification; no Agent request was submitted.
- Production build and the repository's 11 front-end tests pass.

## Agent Composer Controls QA (2026-07-29)

- Source visual truth: user-provided LibTV toolbar screenshot `codex-clipboard-dc889d4c-5328-4941-bb0e-c0c08671d429.png` plus live LibTV model, Skill and generation-mode popovers.
- Implementation evidence: `qa-artifacts/agent-composer-popovers-20260729/toolbar-comparison.png`.
- State: dark canvas, Agent panel open, composer empty, no generation request submitted.

### Verified scope

- The composer toolbar order is add image, generation model, Skills and generation mode.
- All four hit areas are 32 × 32 CSS px; adjacent controls use a consistent 3–4 px rhythm.
- The supplied cube, Skill, manual and automatic SVG assets are used as external assets and optically normalized.
- The model popover is exactly 370 × 384 CSS px and reads image/video options only from the live system model catalog.
- The Skill popover is exactly 450 × 400 CSS px and loads the real general, favorite and activated Skill collections.
- The mode popover is exactly 240 × 114 CSS px; manual and automatic selections update the existing execution-confirmation state.
- Selecting a model or Skill changes the real Agent request configuration; no decorative or fake option was added.
- Browser console remained empty during model selection, Skill loading and mode switching.

### Final result

No actionable P0, P1 or P2 difference remains in the requested composer-control scope.

## Follow-up Polish

- P3: if a future product requirement adds share or CLI actions, add them only after their real flows exist; do not add decorative buttons.

final result: passed
