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

## Homepage Agent Composer QA (2026-07-29)

- Source visual truth: `qa-artifacts/home-agent-composer-20260729/agent-composer-full.png`.
- Implementation screenshot: `qa-artifacts/home-agent-composer-20260729/home-composer-full-current.png`.
- Focused comparison: `qa-artifacts/home-agent-composer-20260729/composer-comparison.png`.
- Viewport: 1086 × 912 CSS px.
- Device scale factor: 1.
- State: dark homepage, empty composer, manual mode, no Agent request submitted.

### Verified scope

- The homepage now renders the same `AgentChatComposer` component used by the canvas Agent panel instead of maintaining a parallel visual implementation.
- Both instances have the same 139 px component height, surface treatment, radius, typography, 32 × 32 px toolbar actions and disabled submit state.
- Homepage width remains 680 px while the canvas drawer uses 399 px; this is an intentional container-width difference, not component drift.
- The control order is identical: add image, generation model, Skills, generation mode and submit.
- The image input accepts multiple `image/*` files and applies the homepage limit of four reference images.
- Model, Skill and mode popovers open below the homepage composer without clipping. Their measured sizes are 394 × 408, 474 × 424 and 264 × 138 CSS px including Ant Design outer padding.
- Manual and automatic mode switching updates the real mode icon and launch configuration.
- Model and Skill choices use the same live system catalog and shared request-context builder as the canvas Agent panel.
- Selected homepage reference images are uploaded and seeded as real canvas image nodes before the Agent launch begins.
- Browser console remained empty throughout the interaction checks.

### Comparison history

- Initial homepage implementation duplicated the Agent toolbar and drifted in structure, spacing and interaction behavior.
- The first reuse pass shared only the model, Skill and mode controls; the homepage still lacked the Agent add-image flow and used a separate composer shell.
- The final pass removed the parallel shell and reused the complete `AgentChatComposer`, with one shared launch contract for mode, models and Skills.

### Final result

No actionable P0, P1 or P2 difference remains in the requested homepage-versus-Agent composer scope.

## Agent Toolbar Icon Polish QA (2026-07-29)

- Source visual truth: the user-supplied model SVG path data and the existing 20 px neighboring toolbar icons.
- Implementation screenshot: `qa-artifacts/agent-icon-polish-20260729/home-after.png`.
- Before/after comparison: `qa-artifacts/agent-icon-polish-20260729/toolbar-before-after.png`.
- Viewport: 1086 × 912 CSS px.
- State: dark homepage, empty composer, no Agent request submitted.

### Verified scope

- The add-image plus glyph increased from 16 × 16 to 20 × 20 CSS px while retaining its existing 32 × 32 px accessible button hit area.
- The model selector now uses the exact two-path geometry supplied by the user, normalized to the shared muted icon color.
- The plus and model glyphs both render at 20 × 20 CSS px and align to the same toolbar center line.
- The model asset URL carries an explicit version marker so local and deployed clients do not continue displaying the previous cached SVG.
- The external SVG loads successfully, tooltips and `aria-label` remain intact, and the browser console is empty.

### Final result

No actionable P0, P1 or P2 difference remains in the requested icon-polish scope.

## Agent Toolbar Tooltip Sync QA (2026-07-29)

- Source screenshot: `qa-artifacts/agent-tooltip-sync-20260729/reference.png`.
- Homepage screenshot: `qa-artifacts/agent-tooltip-sync-20260729/home-tooltip.png`.
- Canvas Agent screenshot: `qa-artifacts/agent-tooltip-sync-20260729/canvas-tooltip.png`.
- Focused comparison: `qa-artifacts/agent-tooltip-sync-20260729/tooltip-comparison.png`.
- Viewport: 1086 × 912 CSS px.
- State: dark homepage and an opened canvas Agent panel; no Agent request was submitted.

### Verified scope

- Homepage and canvas Agent tooltips now use one shared `CanvasAgentTooltip` component instead of separate Ant Design defaults.
- The visible tooltip surface measures `rgb(5, 7, 11)` with `rgb(255, 255, 255)` text, 4 px × 8 px padding, 6 px radius, 12 px type and 600 font weight.
- Hover remains the primary pointer interaction; focus uses the same tooltip for keyboard accessibility.
- Both composers render the same four controls in the same order: add image, model, Skills and generation mode.
- The add-image, model and Skills glyphs are 20 × 20 CSS px; the manual-mode glyph remains optically normalized at 17 × 17 CSS px. Every control retains a 32 × 32 CSS px hit area.
- The model, Skills and generation-mode controls use the same shared asset URLs in both surfaces, including `/icons/agent-model.svg?v=2`.
- Homepage and canvas browser logs remained empty throughout the visual and interaction checks.

### Final result

No actionable P0, P1 or P2 difference remains in the requested tooltip-and-toolbar synchronization scope.

## Projects Workbench Reference QA (2026-07-29)

- Reference: `qa-artifacts/projects-workbench-v1-20260729/reference-updream.png`.
- Implementation: `qa-artifacts/projects-workbench-v1-20260729/implementation-hmaigc.png`.
- Viewport: 1086 × 912 CSS px.
- State: dark project workbench with one real project; no project was created or modified during QA.

### Verified scope

- The `/projects` page uses a 878px centered content frame aligned at x=104, matching the measured Updream reference frame.
- Header and search end at x=982 with no inherited floating-account padding; controls are 36px high and remain on one line at the reference viewport.
- The project grid starts at y=148 and resolves to three `284.66px` columns with 12px gaps.
- The new-project card is `284.66 × 227.72px`; the real project card is `284.67 × 228.13px`.
- Page typography resolves from one scoped 14px text base with explicit 16px page title, 14px card title and 11–12px metadata tiers.
- The create tile, project stage, completion, chapter/canvas/material counts and update time are derived from real project data; no reference-only folder or bulk-selection capability was fabricated.
- Filter, create-project modal and no-result search state were exercised in the real browser. No create request was submitted.
- The homepage was opened separately and contains no `.projects-workspace` scope; homepage and project-page browser consoles remained empty.
- Production TypeScript check and Vite build pass. The existing large-chunk warning remains outside this page-specific visual change.

### Final result

No actionable P0, P1 or P2 issue remains in the requested first-page workbench scope.

final result: passed

## Workspace UI Rollout QA (2026-07-30)

- Scope: projects, canvas library, tasks, assets, skills, settings, teams and wallet workspaces.
- Representative before/after evidence: `qa-artifacts/workspace-ui-rollout-20260730/`.
- Viewports: 390 × 844, 768 × 900, 1024 × 900 and 1440 × 900 CSS px.
- State: dark mode with real local account and workspace data; no project, task or asset was submitted during QA.

### Resolved findings

- P1: the floating account overlay left an inherited 118px right inset on workspace headers, so page actions did not align with the content edge.
- P1: header actions mixed neutral and blue treatments for operations at the same hierarchy level.
- P1: mobile action groups retained 36px desktop controls and inconsistent intrinsic widths.
- P2: pages used different base font stacks and unconstrained wide-screen content widths.

### Verified standard

- All scoped pages resolve to the same `SF Pro Text` / `PingFang SC` / `Microsoft YaHei` font stack.
- Desktop page actions, search and filter controls are 36px high; mobile controls are 44px high.
- Two or more mobile header actions share the available width; single actions retain their content width.
- Header action groups end at the same right edge as page content at 1024px and 1440px.
- Workspace content is capped at 1240px inside a 1448px page frame.
- No tested route produces document-level horizontal overflow at any of the four viewports.
- The task-create and asset-create dialogs open from their relocated header actions and close normally.
- The homepage contains no workspace style scope and remained outside this rollout.
- Browser console contains no warnings or errors after the visual and interaction checks.
- TypeScript/Vite production build passes; all 11 current frontend tests pass.

### Final result

No actionable P0, P1 or P2 issue remains in the requested workspace-layout, typography, button-alignment or responsive scope.
