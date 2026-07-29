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
- Copy and content: supported actions remain explicit. Unsupported LibTV controls were not copied as non-functional decoration, and the backend-managed Agent model is no longer exposed as a user selector.
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
- Removed the user-facing Agent model selector while retaining backend model resolution.

### Post-fix evidence

The full-view and focused comparisons show aligned panel proportions, header density, icon-button scale, neutral surface hierarchy, rich message formatting and bottom composer structure. Remaining content differences come from different real conversations, not component drift.

## Interaction Verification

- History view opens and returns to chat.
- Execution confirmation toggles off and back on with a visible pressed state.
- Send activates when text is entered and returns to disabled after clearing.
- Composer input was cleared after verification; no Agent request was submitted.
- Production build and the repository's 11 front-end tests pass.

## Follow-up Polish

- P3: if a future product requirement adds share, Skills or CLI actions, add them only after their real flows exist; do not add decorative buttons.

final result: passed
