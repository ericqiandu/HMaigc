# Third-Party Notices

HMaigc includes adaptations of third-party ideas and materials as described below. HMaigc's own source remains governed by the repository `LICENSE`; the upstream material retains its original license and attribution.

## ddcat-ai/open-ai-canvas Canvas Agent

- Project: Open AI Canvas
- Repository: https://github.com/ddcat-ai/open-ai-canvas
- Pinned source revision: `e8c6b5a2d977c96a539923df6e68f37c509b0392`
- Upstream license: GNU Affero General Public License v3.0
- Upstream copyright notice: `Open AI Canvas Copyright (C) 2026 ddcat`
- HMaigc adaptation date: 2026-09-01

HMaigc's `canvas-agent` package adapts the upstream package structure and Codex thread lifecycle behavior from `canvas-agent/src/agents.ts` and `canvas-agent/src/codex-thread.ts`. The adapted implementation replaces upstream browser-side canvas mutations and legacy project tools with HMaigc's six canonical capability adapters, a loopback-only HTTP/SSE boundary, exact Origin and token checks, bounded attachments, shared backend approvals, dynamic model pricing, authoritative canvas revision/CAS, billing and audit.

The upstream Claude route, browser direct-write path, legacy brand/config directory and parallel execution contracts are not included. The adapted package is distributed under `AGPL-3.0-only`; the complete license text is provided in the repository `LICENSE`.

## HKUDS/ViMax

- Project: ViMax
- Repository: https://github.com/HKUDS/ViMax
- Pinned source revision: `05a48943878312d88fe5a016c12a9654940ecc43`
- Upstream license: MIT
- Upstream copyright notice: `Copyright (c) 2025`
- HMaigc adaptation date: 2026-08-28

HMaigc's governed Skills named `character-visual-bible`, `storyboard-cinematic-language`, `camera-tree-continuity`, `first-motion-last-frame`, `visual-consistency-review`, and `visual-evidence-analysis` adapt high-level production methods from ViMax, including separation of stable and dynamic character features, cinematic shot planning, spatial camera coverage, frame-boundary continuity, reference-aware visual evaluation, and image-evidence extraction.

No ViMax source file or verbatim method text is included in these Skills. The instructions were newly written for HMaigc's durable Go runtime, immutable Artifact Ledger, approval, billing, audit, capability-manifest, and user-confirmation contracts. HMaigc does not include ViMax's Python/LangChain runtime, fixed pipeline orchestration, local-file state model, implicit semantic heuristics, or candidate deletion behavior.

### MIT License

Copyright (c) 2025

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
