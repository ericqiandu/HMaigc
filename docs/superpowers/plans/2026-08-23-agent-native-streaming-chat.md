# Agent Native Streaming Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stream a provider's real Agent final message through durable sequenced events into one incrementally growing React assistant bubble without exposing reasoning or structured decision fields.

**Architecture:** A provider-only SSE adapter emits ordered content and terminal usage/request facts. A strict incremental JSON observer releases only the confirmed top-level `kind=final` → `final.message` string; Runtime transactionally persists a deterministic `agent_message` lifecycle and each visible delta before SSE projection. A pure Web reducer folds snapshots and sequence events by `itemId`, rejects protocol conflicts, and renders domain cards plus the same live assistant bubble.

**Tech Stack:** Go `net/http`/`bufio`/`encoding/json`, GORM transactions and existing Agent Runtime event projection, TypeScript/React/Vitest.

**Spec:** `docs/superpowers/specs/2026-08-23-agent-native-streaming-chat-design.md`

## Global Constraints

- Provider request always uses `stream: true` and `stream_options.include_usage: true`; no non-stream fallback or model fallback.
- Raw `reasoning_content` is never emitted as user content or persisted in conversation正文; only aggregate usage and restricted diagnostics facts may retain evidence.
- Only strictly confirmed `final.message` deltas are user-visible; tool/clarification/approval/expectedDelivery JSON never leaks.
- Every client-visible delta is persisted first with a continuous sequence; replay deduplicates only by sequence.
- Final `ModelDecision` still passes existing strict parser, completion/delivery verifier, billing, revision/CAS, Skills, and Artifact Ledger contracts.
- Do not change the confirmed panel styling; only timeline data/reducer/rendering needed for truthful behavior may change.
- Milestone budget: at most 10 production files and about 800 net-new production lines.

---

### Task 1: Parse the provider SSE protocol

**Files:**
- Create: `backend/internal/service/provider_chat_stream.go`
- Modify: `backend/internal/service/provider.go`
- Test: `backend/internal/service/provider_test.go`

**Interfaces:**
- Produces: a streaming result/callback contract carrying ordered content deltas plus final `TokenUsageFact`, request ID, and finish reason.
- Produces stable errors: unsupported content type, invalid frame/payload, truncated stream, empty content, and context cancellation.

- [ ] **Step 1: Write failing table tests** for frames split across reads, UTF-8 boundaries, multiple `data:` lines, usage-only chunks, response/request IDs, `[DONE]`, provider error payload, non-SSE response, unknown JSON shape, early EOF, no content, and cancelled context. Assert `reasoning_content` never reaches the content callback.
- [ ] **Step 2: Run RED:** `go test ./internal/service -run 'KuaiziChatCompletionStream|ProviderChatStream' -count=1`; confirm missing adapter/request streaming behavior.
- [ ] **Step 3: Implement the narrow SSE adapter and make the Agent provider request set the strict stream fields.** Preserve billing evidence returned before a terminal protocol failure and never call the legacy full-response decoder.
- [ ] **Step 4: Run GREEN:** repeat the focused provider tests.

### Task 2: Project only a verified final message prefix

**Files:**
- Create: `backend/internal/agentruntime/decision_stream.go`
- Test: `backend/internal/agentruntime/decision_stream_test.go`

**Interfaces:**
- Produces: `DecisionStreamObserver.Push(delta string) ([]string, error)` and `Finish() (ModelDecision, error)` (or an equivalently explicit typed API) whose visible output consists only of newly decoded `final.message` suffixes.
- Consumes: existing `ParseModelDecision` as the sole final authority.

- [ ] **Step 1: Write failing tests** for arbitrary chunk splits, escaped quotes/backslashes/newlines, Unicode escapes and literal UTF-8, `kind` before/after `final`, tool/clarification decisions, misleading nested `message` keys, duplicate/unknown fields, invalid/truncated JSON, and a final payload whose earlier visible prefix later fails strict parsing.
- [ ] **Step 2: Run RED:** `go test ./internal/agentruntime -run 'DecisionStream' -count=1`.
- [ ] **Step 3: Implement a stateful JSON tokenizer/observer that buffers until top-level final-kind certainty and releases monotonic decoded suffixes.** Do not parse semantics with regex/keywords and do not duplicate the final schema validator.
- [ ] **Step 4: Run GREEN:** repeat the focused Agent Runtime package test.

### Task 3: Persist the assistant item lifecycle and deltas

**Files:**
- Modify: `backend/internal/repository/agent_runtime.go`
- Modify: `backend/internal/repository/agent_runtime_event_projection.go`
- Modify: `backend/internal/service/agent_runtime_model.go`
- Modify: `backend/internal/service/agent_runtime_transport.go`
- Test: `backend/internal/repository/agent_runtime_test.go`
- Test: `backend/internal/service/agent_runtime_test.go`
- Test: `backend/internal/service/agent_runtime_transport_test.go`

**Interfaces:**
- Consumes: provider content callback and decision stream observer.
- Produces: deterministic message `itemId`; transactional first `item.started` + first delta; later persisted `model.delta`; authoritative `agent.message` completion or `model.rejected` failure; safe `item.delta` payload `{delta,userVisible:true}`.

- [ ] **Step 1: Write failing repository/service tests** proving first delta cannot exist without its in-progress item, failed writes do not advance sequence, retried delivery does not duplicate characters, every emitted event was committed first, invalid final JSON fails the item while retaining shown facts, cancellation/stream truncation retains usage/request evidence, and successful billing settlement remains idempotent.
- [ ] **Step 2: Run RED:** `go test ./internal/repository ./internal/service -run 'Agent.*(Delta|Stream|Message)|ModelDelta' -count=1`.
- [ ] **Step 3: Add the focused repository transaction and wire provider → observer → persistence.** Log only task/run/item IDs, counts, sequence range, usage status, finish reason, and stable error code; never full prompt/reply/reasoning.
- [ ] **Step 4: Harden transport projection** so only persisted, `userVisible=true`, correctly owned message deltas become `item.delta`; malformed or unverified deltas fail explicitly.
- [ ] **Step 5: Run GREEN:** repeat the focused repository/service tests.

### Task 4: Fold sequence events into one React assistant bubble

**Files:**
- Create: `web/src/components/canvas/agent-conversation-reducer.ts`
- Modify: `web/src/components/canvas/use-agent-runtime.ts`
- Modify: `web/src/components/canvas/canvas-assistant-panel.tsx`
- Modify: `web/src/services/api/agent-runtime.ts`
- Test: `web/test/agent-conversation-reducer.test.ts`
- Test: closest existing Agent panel/runtime tests

**Interfaces:**
- Produces: a pure timeline reducer keyed by `itemId`, with last consumed sequence and explicit conflict result.
- Consumes: historical timeline snapshot plus `item.started/delta/completed/failed` events.

- [ ] **Step 1: Write failing reducer tests** for append-by-item, duplicate sequence replay, reconnect continuation, failed partial message retention, completed-prefix validation, conflict-triggered reload, and isolation of tool/approval/artifact cards.
- [ ] **Step 2: Run RED:** use the repository's focused Vitest command for `agent-conversation-reducer` and confirm the missing reducer/live bubble behavior.
- [ ] **Step 3: Implement the pure reducer and integrate it into the runtime hook.** Sequence, not text equality, is the dedupe key; a completion conflict sets a protocol error and requests a fresh Run rather than silently overwriting.
- [ ] **Step 4: Render ordered user/assistant/domain-card items in the existing panel classes.** Remove low-value raw event labels and ensure the same assistant message DOM node grows as deltas arrive; add no fake timer/typing animation.
- [ ] **Step 5: Run GREEN:** repeat focused reducer/panel/API tests, then Web typecheck.

### Task 5: Document, verify, review, and commit

**Files:**
- Modify: `README.md`
- Review: all milestone-two files and affected contracts

**Interfaces:**
- Produces: current documentation and one independently revertible streaming milestone commit.

- [ ] **Step 1: Update README Agent Runtime and model billing sections** to document provider SSE → durable sequence → replay → one bubble, internal Task visibility, reasoning privacy, strict failure, and removal of any future-streaming wording.
- [ ] **Step 2: Run focused tests, then `go test ./...`, affected-package `go test -race`, `go build ./...`, Web tests/typecheck/build, and `git diff --check`.** Run Docker/browser/real-provider regression only if the existing local data directory, credentials, and authorization make it truthful and safe.
- [ ] **Step 3: Perform explicit review** against requirements, both plans, actual diff, API/event types, migration state, permission, billing, idempotency, cancellation/error handling, documentation, and test evidence; make one concentrated fix pass.
- [ ] **Step 4: Re-run only affected gates plus one final complete confirmation and perform one targeted re-review.** If new cross-module Important/Critical issues remain, stop and revisit architecture instead of adding a third patch loop.
- [ ] **Step 5: Inspect staged hunks with `git diff --cached`; exclude unrelated work and commit using `feat(agent): 原生对话 - 接入可恢复真实流式回复` without pushing.**
