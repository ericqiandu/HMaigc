# Upstream-inspired Cloud Agent Hard Cutover Implementation Plan

> **For Codex:** Execute this plan with `superpowers:executing-plans` in the current worktree. Do not use `superpowers:subagent-driven-development`: the runtime, approval, billing, asset, and canvas changes share one transactional boundary and are not independent work packets.

**Goal:** Replace the specialist/production-graph Agent execution path with one cloud-hosted, model-driven Agent loop that reads real canvas/project facts, loads Skills on demand, and executes atomic canvas, asset, and media capabilities with independent user approval for every mutation and every paid action.

**Architecture:** Keep the existing authenticated thread/run/checkpoint/event stream, dynamic model catalog, task/billing, asset, and canvas persistence services. Hard-cut the active runtime to a new tool schema and protocol built around six atomic capabilities: `canvas.read`, `canvas.apply_ops`, `assets.read`, `assets.publish`, `media.generate`, and `skills.load`. Read tools execute immediately; write and cost tools persist an immutable, hashed approval proposal before execution. The Web client only renders events, presents approvals, applies server-authorized canvas operations, and reports deterministic execution evidence. Historical specialist/production records remain readable, but no new run can enter those execution paths.

**Tech Stack:** Go 1.25, Gin, GORM/PostgreSQL/SQLite tests, React 19, TypeScript strict, Vite, Bun test, existing SSE and media/billing services.

---

## Change budget and release boundaries

- **Production responsibilities:** one Agent loop, one capability registry, one approval proposal contract, one event projection, one Web execution adapter.
- **Expected implementation scope:** 24–32 production files, 12–18 focused test files, README/CHANGELOG updates. Net production code should decrease in the final retirement milestone.
- **Expected net change:** approximately +800–1,400 lines before retirement and neutral-to-negative after deleting the executable specialist/production path.
- **Expensive gates:** full Go suite, PostgreSQL integration, Web build, Docker stack, and real browser acceptance run only at stable milestones. Paid model/media calls are forbidden in automated tests; one bounded real call requires separate explicit user authorization.
- **Deferred until separate destructive-data approval:** dropping production/stage/specialist tables or deleting historical rows. This plan only makes historical records non-executable.
- **Release shape:** ship as one versioned hard-cut release after all deterministic gates pass. Do not deploy an intermediate state where old and new Agent paths are both executable.

## Milestone 1 — Freeze the new kernel contract

### Task 1: Add the v6 capability vocabulary and fail-closed tool policy

**Files:**

- Modify: `backend/internal/agentruntime/contracts.go`
- Modify: `backend/internal/agentruntime/decision.go`
- Modify: `backend/internal/agentruntime/tools.go`
- Modify: `backend/internal/agentruntime/contracts_test.go`
- Modify: `backend/internal/agentruntime/decision_test.go`
- Modify: `backend/internal/agentruntime/runtime_test.go`

**Step 1: Write failing contract tests**

Replace production-v5 expectations with v6 expectations:

```go
func TestCurrentToolSchemaContainsOnlyAtomicCloudCapabilities(t *testing.T) {
	policies, ok := agentruntime.ToolPoliciesForSchema(agentruntime.CurrentToolSchemaVersion)
	if !ok {
		t.Fatal("current tool schema missing")
	}
	want := []agentruntime.ToolName{
		agentruntime.ToolCanvasRead,
		agentruntime.ToolCanvasApplyOps,
		agentruntime.ToolAssetsRead,
		agentruntime.ToolAssetsPublish,
		agentruntime.ToolMediaGenerate,
		agentruntime.ToolSkillsLoad,
	}
	// Assert exact order and exact set; retired specialist/production tools are rejected.
}

func TestEveryWriteAndCostToolRequiresApprovalInEveryMode(t *testing.T) {
	for _, mode := range []agentruntime.ExecutionMode{agentruntime.ExecutionGuided, agentruntime.ExecutionAutomatic} {
		for _, name := range []agentruntime.ToolName{agentruntime.ToolCanvasApplyOps, agentruntime.ToolAssetsPublish, agentruntime.ToolMediaGenerate} {
			policy, _ := agentruntime.ToolPolicyFor(name)
			if !agentruntime.ApprovalRequiredFor(policy, mode) {
				t.Fatalf("%s in %s bypassed approval", name, mode)
			}
		}
	}
}
```

Add strict decision-parser cases for each new tool and negative cases for `specialist.delegate`, `vision.analyze`, `canvas.project`, `media.assemble`, `production.plan`, and `canvas.commit`.

**Step 2: Run the focused tests to prove RED**

Run:

```powershell
Set-Location backend
go test ./internal/agentruntime -run 'Test(CurrentToolSchema|EveryWriteAndCostTool|ParseModelDecision)' -count=1
```

Expected: FAIL because the v6 tool constants/policies do not exist and retired tools are still current.

**Step 3: Implement the hard-cut constants and policies**

Set new current versions and keep old values only as retired history facts:

```go
const (
	CloudRuntimeVersion         = 5
	CloudPolicyVersion          = 5
	CloudToolSchemaVersion      = 6
	CloudAgentUIProtocolVersion = 5

	CurrentRuntimeVersion    = CloudRuntimeVersion
	CurrentPolicyVersion     = CloudPolicyVersion
	CurrentToolSchemaVersion = CloudToolSchemaVersion
)

const (
	ToolCanvasRead     ToolName = "canvas.read"
	ToolCanvasApplyOps ToolName = "canvas.apply_ops"
	ToolAssetsRead     ToolName = "assets.read"
	ToolAssetsPublish  ToolName = "assets.publish"
	ToolMediaGenerate  ToolName = "media.generate"
	ToolSkillsLoad     ToolName = "skills.load"
)
```

Policies must be exact:

- reads and Skill loads: `L0/viewer`, no approval;
- canvas apply and asset publish: `L1/editor`, approval always;
- media generation: `L2/editor`, approval always.

Delete current-schema validity branches for specialist/stage/production tools. Do not add aliases or compatibility names.

**Step 4: Run focused tests to GREEN**

Run:

```powershell
Set-Location backend
go test ./internal/agentruntime -count=1
```

Expected: PASS.

**Step 5: Commit the contract milestone**

```powershell
git add backend/internal/agentruntime/contracts.go backend/internal/agentruntime/decision.go backend/internal/agentruntime/tools.go backend/internal/agentruntime/contracts_test.go backend/internal/agentruntime/decision_test.go backend/internal/agentruntime/runtime_test.go
git commit -m "refactor(agent): hard-cut cloud capability contract"
```

### Task 2: Define strict atomic capability arguments and results

**Files:**

- Create: `backend/internal/agentruntime/capability_contracts.go`
- Create: `backend/internal/agentruntime/capability_contracts_test.go`
- Modify: `backend/internal/agentruntime/decision.go`
- Modify: `web/src/services/api/agent-runtime.ts`
- Create: `web/src/services/api/agent-capabilities.test.ts`

**Step 1: Write failing Go and TypeScript parser tests**

Each capability gets one versioned, strict request and result schema. Representative types:

```go
type CanvasReadArguments struct {
	CanvasID          string   `json:"canvasId"`
	SelectedNodeIDs   []string `json:"selectedNodeIds,omitempty"`
	IncludeViewport   bool     `json:"includeViewport"`
}

type CanvasApplyOpsArguments struct {
	CanvasID          string          `json:"canvasId"`
	BaseRevision      int64           `json:"baseRevision"`
	ClientMutationID  string          `json:"clientMutationId"`
	Operations        json.RawMessage `json:"operations"`
}

type AssetsPublishArguments struct {
	ResourceID        string `json:"resourceId"`
	DomainProjectID   string `json:"domainProjectId"`
	DisplayName       string `json:"displayName"`
	ClientMutationID  string `json:"clientMutationId"`
}
```

`MediaGenerateArguments` must identify media kind, selected dynamic model record/key, validated model parameters, source resource IDs, target canvas node identity, and client request identity. It must not accept client-supplied price, ownership, final URLs, task status, or billing status.

Test these invariants:

- unknown fields fail;
- empty/oversized identifiers fail;
- duplicate operation IDs fail;
- invalid node/edge operation structure fails;
- `media.generate` rejects unsupported model capability combinations before proposal creation;
- `assets.publish` rejects URLs and accepts only an authoritative `resourceId`;
- Web parsers reject retired tool names and malformed event payloads without `as any` or implicit defaults.

**Step 2: Run focused RED tests**

```powershell
Set-Location backend
go test ./internal/agentruntime -run 'TestCapability' -count=1
Set-Location ..\web
bun test src/services/api/agent-capabilities.test.ts
```

Expected: FAIL because the capability contracts do not exist.

**Step 3: Implement one strict decoder per capability**

Expose:

```go
func DecodeCapabilityArguments(name ToolName, payload json.RawMessage) (CapabilityArguments, error)
```

Use a closed interface implemented only by the six request structs. Use `json.Decoder.DisallowUnknownFields`, exact size limits, structural validation, and normalized copies. Do not inspect prompt semantics, use regex routing, or add default model/mode values.

Mirror only wire types and strict type guards in `web/src/services/api/agent-runtime.ts`. The backend remains authoritative for capability validity.

**Step 4: Run tests to GREEN**

```powershell
Set-Location backend
go test ./internal/agentruntime -count=1
Set-Location ..\web
bun test src/services/api/agent-capabilities.test.ts
```

Expected: PASS.

**Step 5: Commit**

```powershell
git add backend/internal/agentruntime/capability_contracts.go backend/internal/agentruntime/capability_contracts_test.go backend/internal/agentruntime/decision.go web/src/services/api/agent-runtime.ts web/src/services/api/agent-capabilities.test.ts
git commit -m "feat(agent): add strict atomic capability schemas"
```

## Milestone 2 — Make protected actions immutable and idempotent

### Task 3: Persist immutable approval proposals

**Files:**

- Create: `backend/internal/agentruntime/approval_proposal.go`
- Create: `backend/internal/agentruntime/approval_proposal_test.go`
- Modify: `backend/internal/model/agent_runtime.go`
- Modify: `backend/internal/database/agent_runtime_schema.go`
- Modify: `backend/internal/database/agent_runtime_schema_test.go`
- Modify: `backend/internal/repository/agent_runtime_tools.go`
- Modify: `backend/internal/repository/agent_runtime_execution.go`
- Modify: `backend/internal/repository/agent_runtime_test.go`

**Step 1: Write failing proposal/hash tests**

Define a canonical proposal:

```go
type ApprovalProposal struct {
	Version          int                 `json:"version"`
	RunID            string              `json:"runId"`
	ToolCallID       string              `json:"toolCallId"`
	ActionVersion    int                 `json:"actionVersion"`
	Scope            ApprovalScope       `json:"scope"`
	ToolName         ToolName             `json:"toolName"`
	Arguments        json.RawMessage      `json:"arguments"`
	Effect           ApprovalEffect       `json:"effect"`
	Quote            *ApprovalCostQuote   `json:"quote,omitempty"`
	IdempotencyKey   string              `json:"idempotencyKey"`
	ExpiresAt        time.Time           `json:"expiresAt"`
}
```

Test:

- canonical JSON produces a stable SHA-256 hash;
- changing arguments, scope, model, price version, expiry, or effect changes the hash;
- replaying the identical proposal returns the existing tool call;
- approval after expiry or with a mismatched hash fails;
- duplicate approval executes at most once;
- SQLite migration is additive and preserves existing rows;
- incompatible active v5 runs are reported/retired atomically only when safe under existing retirement rules.

**Step 2: Run RED tests**

```powershell
Set-Location backend
go test ./internal/agentruntime ./internal/database ./internal/repository -run 'Test(ApprovalProposal|AgentToolCall|EnsureAgentRuntime)' -count=1
```

Expected: FAIL.

**Step 3: Implement the additive persistence contract**

Extend `model.AgentToolCall` with:

```go
ApprovalProposalJSON string     `json:"-" gorm:"type:text;not null;default:''"`
ApprovalProposalHash string     `json:"approvalProposalHash,omitempty" gorm:"size:64;not null;default:''"`
ApprovalExpiresAt    *time.Time `json:"approvalExpiresAt,omitempty"`
```

Add an index on `(run_id, approval_proposal_hash)` with a predicate excluding empty legacy facts. Keep proposal creation and decision in repository transactions. Approval submission must require `toolCallId`, `actionVersion`, and `proposalHash`; approval of one proposal never authorizes another action.

**Step 4: Run tests to GREEN**

```powershell
Set-Location backend
go test ./internal/agentruntime ./internal/database ./internal/repository -count=1
```

Expected: PASS.

**Step 5: Commit**

```powershell
git add backend/internal/agentruntime/approval_proposal.go backend/internal/agentruntime/approval_proposal_test.go backend/internal/model/agent_runtime.go backend/internal/database/agent_runtime_schema.go backend/internal/database/agent_runtime_schema_test.go backend/internal/repository/agent_runtime_tools.go backend/internal/repository/agent_runtime_execution.go backend/internal/repository/agent_runtime_test.go
git commit -m "feat(agent): persist immutable approval proposals"
```

### Task 4: Simplify the runtime state machine around proposal, execution, and resume

**Files:**

- Modify: `backend/internal/agentruntime/runtime.go`
- Modify: `backend/internal/agentruntime/runtime_test.go`
- Create: `backend/internal/agentruntime/recovery_test.go`
- Modify: `backend/internal/service/agent_runtime.go`
- Modify: `backend/internal/service/agent_runtime_coordinator.go`
- Modify: `backend/internal/service/agent_runtime_resume_test.go`

**Step 1: Add failing transition tests**

Cover one state path:

```text
queued -> running -> waiting_tool(read) -> running
running -> waiting_approval(write/cost) -> waiting_tool(approved) -> running
waiting_approval(rejected) -> running with factual rejection result
running -> succeeded only when expected delivery verifier is satisfied
```

Also test:

- process restart resumes a non-terminal run from checkpoint;
- a completed tool result is never executed again;
- expired/mismatched proposal returns to `running` with explicit factual failure and does not execute;
- max tool calls, max elapsed time, and invalid model output end as explicit failure with diagnostics;
- no transition invokes specialist, production graph, stage review, or automatic workflow authorization.

**Step 2: Run RED tests**

```powershell
Set-Location backend
go test ./internal/agentruntime ./internal/service -run 'Test(CloudAgent|AgentRuntime.*Resume|Approval)' -count=1
```

Expected: FAIL.

**Step 3: Implement the smaller state machine**

Retain `RuntimeState`, checkpoint CAS, tool identity, clarification, delivery verification, and event emission. Remove production/stage/specialist fields from the active state schema. The runtime may preserve decoding types for historical records, but current-version checkpoints must contain only generic messages, loaded Skill identities, pending capability call, delivery expectation/evidence, decision feedback, and limits.

`ApprovalRequiredFor` decides the pause. After an approval/rejection/tool result, requeue the same run; do not start a child production run or fixed stage sequence.

**Step 4: Run tests to GREEN**

```powershell
Set-Location backend
go test ./internal/agentruntime ./internal/service -run 'Test(CloudAgent|AgentRuntime.*Resume|Approval)' -count=1
```

Expected: PASS.

**Implementation status (2026-09-01): complete.** Runtime v5 now persists the absolute deadline and tool-call budget, resumes from checkpoints without rerunning completed tools, returns rejected/expired/mismatched proposals to the same Run as explicit failure facts, terminal-fails invalid model decisions with diagnostics, and transactionally closes the active Run tree on user interruption or deadline expiry. Focused transition tests and the complete backend suite pass; `go vet ./...` is clean. Runtime v4 / Tool schema v5 production-graph execution tests are explicitly retired pending their deletion in Task 10; no legacy tool was reopened. Capability adapters remain deferred to Task 5 as planned.

**Step 5: Commit**

```powershell
git add backend/internal/agentruntime/runtime.go backend/internal/agentruntime/runtime_test.go backend/internal/agentruntime/recovery_test.go backend/internal/service/agent_runtime.go backend/internal/service/agent_runtime_coordinator.go backend/internal/service/agent_runtime_resume_test.go
git commit -m "refactor(agent): simplify loop state and recovery"
```

## Milestone 3 — Execute atomic capabilities through existing authorities

### Task 5: Implement read-only canvas, asset, and Skill capabilities

**Files:**

- Create: `backend/internal/service/agent_capability_registry.go`
- Create: `backend/internal/service/agent_capability_registry_test.go`
- Create: `backend/internal/service/agent_capability_reads.go`
- Create: `backend/internal/service/agent_capability_reads_test.go`
- Modify: `backend/internal/service/agent_runtime_context.go`
- Modify: `backend/internal/service/agent_runtime_context_test.go`
- Modify: `backend/internal/skillcatalog/catalog.json`

**Step 1: Write failing authorization/fact tests**

Test:

- registry returns exactly six capabilities for v6;
- `canvas.read` returns only the scoped canvas revision, nodes, edges, selection, and viewport facts;
- `assets.read` returns only tenant/project-owned assets and never leaks another tenant's resources;
- `skills.load` accepts only an enabled catalog version authorized for the run and returns checksum-traceable instructions;
- context assembly contains identity, scope, model facts, Skill descriptors, and capability schemas, but no fixed workflow, specialist order, docs/assets/ai-metadata runtime content, or semantic route;
- invalid ownership, stale canvas scope, missing Skill version, or oversized context fails explicitly.

**Step 2: Run RED tests**

```powershell
Set-Location backend
go test ./internal/service -run 'TestAgentCapability(Read|Registry|Context|Skill)' -count=1
```

Expected: FAIL.

**Step 3: Implement the registry and read adapters**

Use a typed registry:

```go
type AgentCapabilityExecutor interface {
	Execute(ctx context.Context, scope agentruntime.Scope, call agentruntime.ToolCallDecision) (agentruntime.ToolExecutionResult, error)
}
```

Read adapters call existing repositories/services and sanitize outputs before appending them to model context. Do not let the client provide ownership facts. Replace the old pre-frozen production context with bounded factual context plus the registry description.

Update catalog tool allowlists to the new capability names. Skill instructions remain in the Skill catalog and load only on demand.

**Step 4: Run tests to GREEN**

```powershell
Set-Location backend
go test ./internal/service -run 'TestAgentCapability(Read|Registry|Context|Skill)' -count=1
```

Expected: PASS.

**Step 5: Commit**

```powershell
git add backend/internal/service/agent_capability_registry.go backend/internal/service/agent_capability_registry_test.go backend/internal/service/agent_capability_reads.go backend/internal/service/agent_capability_reads_test.go backend/internal/service/agent_runtime_context.go backend/internal/service/agent_runtime_context_test.go backend/internal/skillcatalog/catalog.json
git commit -m "feat(agent): execute scoped read capabilities"
```

### Task 6: Implement server-authorized canvas operation proposals and acknowledgements

**Files:**

- Create: `backend/internal/service/agent_canvas_capability.go`
- Create: `backend/internal/service/agent_canvas_capability_test.go`
- Modify: `backend/internal/repository/canvas_collaboration.go`
- Modify: `backend/internal/handler/agent_runtime.go`
- Modify: `backend/internal/handler/agent_runtime_test.go`
- Modify: `web/src/lib/canvas/canvas-agent-ops.ts`
- Create: `web/src/lib/canvas/canvas-agent-ops.test.ts`
- Modify: `web/src/pages/canvas/use-canvas-agent-operations.ts`

**Step 1: Write failing server and Web tests**

Test:

- `canvas.apply_ops` creates an immutable approval proposal with exact base revision, normalized operations, effect preview, proposal hash, and client mutation ID;
- approval rechecks tenant/editor access and base revision before execution;
- duplicate approve/apply returns the existing committed revision;
- stale revision, unknown handle, invalid edge, or unauthorized node operation fails without partial mutation;
- Web applies only a server-authorized proposal carrying a valid proposal hash and reports `committedRevision` plus structural evidence;
- `run_generation` is not a canvas operation in v6; paid generation remains solely `media.generate`.

**Step 2: Run RED tests**

```powershell
Set-Location backend
go test ./internal/service ./internal/handler -run 'TestAgentCanvasCapability' -count=1
Set-Location ..\web
bun test src/lib/canvas/canvas-agent-ops.test.ts
```

Expected: FAIL.

**Step 3: Implement deterministic canvas execution**

Use existing canvas revision CAS and idempotent `clientMutationId`. Keep operation validation pure and shared within Web. Add a strict acknowledgement endpoint only if the browser must execute UI-local operations; server-persisted node/edge mutations remain authoritative. Do not accept a browser-reported success without reading back the committed revision.

Remove `run_generation` from `CanvasAgentOp`. Split any generated-media node projection into a later, separately approved `canvas.apply_ops` proposal after the real asset exists.

**Step 4: Run tests to GREEN**

```powershell
Set-Location backend
go test ./internal/service ./internal/handler -run 'TestAgentCanvasCapability' -count=1
Set-Location ..\web
bun test src/lib/canvas/canvas-agent-ops.test.ts
```

Expected: PASS.

**Step 5: Commit**

```powershell
git add backend/internal/service/agent_canvas_capability.go backend/internal/service/agent_canvas_capability_test.go backend/internal/repository/canvas_collaboration.go backend/internal/handler/agent_runtime.go backend/internal/handler/agent_runtime_test.go web/src/lib/canvas/canvas-agent-ops.ts web/src/lib/canvas/canvas-agent-ops.test.ts web/src/pages/canvas/use-canvas-agent-operations.ts
git commit -m "feat(agent): govern canvas operations by proposal"
```

### Task 7: Adapt paid media generation and asset publication

**Files:**

- Create: `backend/internal/service/agent_media_capability.go`
- Create: `backend/internal/service/agent_media_capability_test.go`
- Create: `backend/internal/service/agent_asset_capability.go`
- Create: `backend/internal/service/agent_asset_capability_test.go`
- Modify: `backend/internal/service/agent_media_generation.go`
- Modify: `backend/internal/service/agent_asset_publication.go`
- Modify: `backend/internal/repository/agent_runtime_tools.go`

**Step 1: Write failing monetary and ownership tests**

Cover image, video, and optional audio separately:

- proposal freezes dynamic model record/key, validated parameters, source resources, pricing record/version, estimated points, expiry, and idempotency key;
- every paid task requires its own approval; approving an image never authorizes a video or audio call;
- duplicate approval/browser retry/worker retry creates one task and one billing reservation;
- success settles once and retains all generated resources;
- provider failure/cancel/timeout releases or refunds once;
- unknown settlement blocks a second attempt and exposes reconciliation-required state;
- projection/publication failure after generation retains the resource;
- `assets.publish` rechecks resource ownership and publishes once;
- another tenant cannot quote, approve, read, or publish the resource.

**Step 2: Run RED tests**

```powershell
Set-Location backend
go test ./internal/service ./internal/repository -run 'TestAgent(Media|Asset)Capability' -count=1
```

Expected: FAIL.

**Step 3: Implement adapters over existing commercial services**

Reuse `FreezeMediaQuote`, `ApproveMediaAttempt`, `EnsureMediaTask`, settlement/refund reconciliation, resource persistence, and asset publication. Do not reimplement billing. The capability returns authoritative task/order/resource IDs and final URLs only after persisted success.

Provider errors are normalized for users and retained in operator diagnostics. There is no hidden retry or alternate model.

**Step 4: Run tests to GREEN**

```powershell
Set-Location backend
go test ./internal/service ./internal/repository -run 'TestAgent(Media|Asset)Capability' -count=1
```

Expected: PASS.

**Step 5: Commit**

```powershell
git add backend/internal/service/agent_media_capability.go backend/internal/service/agent_media_capability_test.go backend/internal/service/agent_asset_capability.go backend/internal/service/agent_asset_capability_test.go backend/internal/service/agent_media_generation.go backend/internal/service/agent_asset_publication.go backend/internal/repository/agent_runtime_tools.go
git commit -m "feat(agent): adapt governed media and asset capabilities"
```

## Milestone 4 — Stream one protocol and simplify the Web client

### Task 8: Project the v5 Agent UI event protocol and approval API

**Files:**

- Modify: `backend/internal/service/agent_runtime_transport.go`
- Modify: `backend/internal/service/agent_runtime_transport_test.go`
- Modify: `backend/internal/handler/agent_runtime.go`
- Modify: `backend/internal/handler/agent_runtime_test.go`
- Modify: `web/src/services/api/agent-runtime.ts`
- Create: `web/src/services/api/agent-runtime.test.ts`
- Modify: `web/src/components/canvas/use-agent-runtime.ts`
- Create: `web/src/components/canvas/use-agent-runtime.test.ts`

**Step 1: Write failing protocol/reconnect tests**

The approval submission must include:

```json
{
  "toolCallId": "call-1",
  "actionVersion": 1,
  "proposalHash": "64-lowercase-hex",
  "decision": "approved"
}
```

Test:

- SSE protocol version is exactly 5;
- replay after `afterSequence` is ordered and duplicate-safe;
- reconnect during approval or media execution reconstructs the current state from persisted events;
- approval cards receive exact effect/quote/expiry facts;
- old protocol payloads and stage-review/artifact-revision events are rejected for new runs;
- user-visible failures are concise, while trace/error codes remain present for operator lookup.

**Step 2: Run RED tests**

```powershell
Set-Location backend
go test ./internal/service ./internal/handler -run 'TestAgent(RuntimeTransport|Event|Approval)' -count=1
Set-Location ..\web
bun test src/services/api/agent-runtime.test.ts src/components/canvas/use-agent-runtime.test.ts
```

Expected: FAIL.

**Step 3: Implement protocol v5**

Keep thread/run/start/read/SSE/steer/interrupt/approval/clarification endpoints. Remove stage review and artifact revision mutation routes from `RegisterAgentRuntimeRoutes`. Project only generic message, tool, approval, asset, canvas revision, status, and failure events.

The client stores the last sequence and reconnects without replaying effects. Remove production graph response parsing from the active Agent API.

**Step 4: Run tests to GREEN**

```powershell
Set-Location backend
go test ./internal/service ./internal/handler -run 'TestAgent(RuntimeTransport|Event|Approval)' -count=1
Set-Location ..\web
bun test src/services/api/agent-runtime.test.ts src/components/canvas/use-agent-runtime.test.ts
```

Expected: PASS.

**Step 5: Commit**

```powershell
git add backend/internal/service/agent_runtime_transport.go backend/internal/service/agent_runtime_transport_test.go backend/internal/handler/agent_runtime.go backend/internal/handler/agent_runtime_test.go web/src/services/api/agent-runtime.ts web/src/services/api/agent-runtime.test.ts web/src/components/canvas/use-agent-runtime.ts web/src/components/canvas/use-agent-runtime.test.ts
git commit -m "feat(agent): stream cloud agent protocol v5"
```

### Task 9: Replace production/stage UI with one approval-driven Agent panel

**Files:**

- Modify: `web/src/components/canvas/canvas-assistant-panel.tsx`
- Modify: `web/src/components/canvas/canvas-agent-chat-ui.tsx`
- Modify: `web/src/components/canvas/agent-approval-summary.tsx`
- Create: `web/src/components/canvas/agent-approval-card.tsx`
- Create: `web/src/components/canvas/agent-approval-card.test.tsx`
- Delete: `web/src/components/canvas/agent-production-card.tsx`
- Delete: `web/src/components/canvas/agent-production-review-card.tsx`
- Delete: `web/src/services/api/agent-production.ts`
- Delete: `web/src/services/api/agent-production-client.ts`
- Modify: `web/src/components/canvas/canvas-agent-panel.css`
- Modify: `web/DESIGN.md`

**Step 1: Write failing UI tests**

Test these user facts:

- read and Skill activity is visible but has no confirmation action;
- each canvas mutation card shows exact operations and one approval/reject pair;
- each media card shows model, parameters, quoted points, expiry, and one approval/reject pair;
- approving one card disables only that proposal and cannot approve later cards;
- expiry/mismatch displays a new-proposal requirement;
- refresh reconstructs the card from persisted events;
- generated assets remain visible when later canvas projection fails;
- no production stage, specialist, blanket authorization, fake progress, or unsupported control remains.

**Step 2: Run RED test**

```powershell
Set-Location web
bun test src/components/canvas/agent-approval-card.test.tsx
```

Expected: FAIL.

**Step 3: Implement the simplified panel**

Follow `Design.md`: one visible rounded border per independent card, 4px spacing tokens, 32px icon controls, concise factual status, keyboard focus, and `prefers-reduced-motion`. Every TSX tag receives a descriptive named `className`. Do not put semantic routing or prompt rewriting in the client.

Delete production review UI and client APIs in the same commit so there is one active presentation path.

**Step 4: Run Web tests/build**

```powershell
Set-Location web
bun test src/components/canvas/agent-approval-card.test.tsx src/services/api/agent-runtime.test.ts src/components/canvas/use-agent-runtime.test.ts
bun run build
```

Expected: PASS.

**Step 5: Commit**

```powershell
git add -A web/src/components/canvas web/src/services/api web/DESIGN.md
git commit -m "refactor(web): replace production graph agent UI"
```

## Milestone 5 — Retire executable legacy paths and document the actual system

### Task 10: Remove specialist/production execution from dependency wiring

**Files:**

- Modify: `backend/internal/handler/agent_runtime.go`
- Delete: `backend/internal/handler/agent_production.go`
- Delete active-only service files after reference proof, including:
  - `backend/internal/service/agent_specialist_delegation.go`
  - `backend/internal/service/agent_specialist_runtime.go`
  - `backend/internal/service/agent_stage_review.go`
  - `backend/internal/service/agent_runtime_production.go`
  - `backend/internal/service/agent_runtime_production_tools.go`
  - `backend/internal/service/agent_production_recovery.go`
  - `backend/internal/service/agent_media_assembly_coordinator.go`
  - `backend/internal/service/agent_media_assembly.go`
  - `backend/internal/service/agent_visual_analysis_coordinator.go`
  - `backend/internal/service/agent_visual_analysis_execution.go`
  - `backend/internal/service/agent_visual_analysis.go`
  - `backend/internal/service/agent_visual_consistency.go`
- Delete corresponding active-only tests after replacement coverage exists.
- Preserve: production/stage/specialist models, repositories, and tables only where required to read historical audit records.
- Modify: `backend/internal/database/agent_runtime_schema.go`
- Modify: `backend/internal/database/agent_runtime_schema_test.go`
- Modify: `backend/internal/service/admin_agent_runs.go`
- Modify: `backend/internal/service/admin_agent_runs_test.go`

**Step 1: Prove references before deletion**

Run:

```powershell
rg -n "specialist\.delegate|canvas\.project|media\.assemble|vision\.analyze|ReviewProductionStage|registerAgentProductionRoutes" backend web/src
```

Expected: only retired/historical constants, migration/audit readers, and files scheduled for deletion. Any active reference must be removed or migrated before proceeding.

**Step 2: Write failing retirement tests**

Test:

- new v6 runs cannot parse or execute any retired tool;
- old non-terminal v5 runs are enumerated before mutation and retired only through the existing atomic retirement audit;
- legacy tool/approval endpoints are absent;
- admin history can still read terminal legacy records;
- historical rows/tables are not deleted.

**Step 3: Remove active wiring and executable files**

Delete the files only after replacement coverage from Tasks 1–9 is green. Keep immutable history models/repositories if admin audit reads them. Do not drop tables or records in this task.

**Step 4: Run backend and Web gates**

```powershell
Set-Location backend
go test ./internal/agentruntime ./internal/database ./internal/repository ./internal/service ./internal/handler -count=1
Set-Location ..\web
bun test
bun run build
```

Expected: PASS and no active code reference to the retired tools/routes.

**Step 5: Commit**

```powershell
git add -A backend web
git commit -m "refactor(agent): retire production graph execution"
```

### Task 11: Synchronize architecture and release documentation

**Files:**

- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `CONTEXT.md`
- Modify: `PRODUCTION.md`

**Step 1: Replace the outdated AI architecture section**

Document the actual v6/v5 system:

- single model-driven loop;
- six atomic capabilities;
- Skills descriptors and on-demand loads;
- approval proposal hash/expiry/idempotency;
- independent write/cost confirmations;
- current persistence/events/recovery;
- reuse of model catalog, billing, task, resource, asset, canvas revision;
- historical legacy records are readable but not executable;
- operator trace correlation and concise user errors.

Remove descriptions that claim `specialist.delegate`, stage reviews, `canvas.project`, or `media.assemble` are active.

**Step 2: Add deployment cutover checklist**

`PRODUCTION.md` must require:

1. read-only audit of active v5 runs and unsettled billing;
2. explicit operator approval before retiring safe non-terminal legacy runs;
3. additive schema migration and index verification;
4. deployment of backend/Web as one version;
5. post-deploy protocol and idempotency smoke tests;
6. image rollback if acceptance fails; no runtime fallback.

**Step 3: Verify documentation references**

```powershell
rg -n "specialist\.delegate|canvas\.project|media\.assemble|production graph|stage review" README.md CONTEXT.md PRODUCTION.md
```

Expected: matches appear only in explicit historical/retired statements.

**Step 4: Commit**

```powershell
git add README.md CHANGELOG.md CONTEXT.md PRODUCTION.md
git commit -m "docs(agent): document cloud agent hard cutover"
```

## Milestone 6 — Independent review, concentrated repair, and acceptance

### Task 12: Run one independent review and one concentrated repair pass

**Files:** Review all diff from baseline `b228a5a` to `HEAD`.

**Step 1: Run deterministic quality gates**

```powershell
Set-Location .
$goFiles = @(git diff --name-only b228a5a..HEAD -- '*.go' | ForEach-Object { Join-Path $PWD $_ })
if ($goFiles.Count -gt 0) { gofmt -w @goFiles }
Set-Location backend
go test ./... -count=1
Set-Location ..\web
bun test
bun run build
Set-Location ..
git diff --check b228a5a..HEAD
```

Expected: PASS.

**Step 2: Perform the required independent review**

Review against:

- the approved design document;
- this implementation plan;
- actual diff and route/schema changes;
- tenant isolation and authorization;
- approval immutability and expiry;
- billing reservation/settlement/refund idempotency;
- generated-resource preservation;
- process restart and SSE replay;
- absence of regex/keyword semantic routing and local workflow switches;
- absence of hidden fallback/alternate model/automatic paid retry;
- README/PRODUCTION truthfulness;
- no secrets, local artifacts, build outputs, or unrelated changes.

Record findings by severity. Apply one concentrated repair pass for current-diff defects, then rerun only affected focused tests.

**Step 3: Run one targeted re-review**

Re-check repaired areas and their boundaries. If new cross-module Critical/Important defects still appear, stop and return to architecture rather than starting a third patch loop.

**Step 4: Commit verified repairs**

```powershell
git add <only-reviewed-repair-files>
git commit -m "fix(agent): close hard-cutover review findings"
```

Skip this commit if there are no repair changes.

### Task 13: Production-like integration and browser acceptance

**Files:**

- Create or modify deterministic integration fixtures under existing backend/Web test locations only as needed.
- Store screenshots/logs under `qa-artifacts/agent-cloud-cutover/`; do not commit secrets or provider payloads.

**Step 1: Start the real local stack**

Use the repository's existing Docker/local services and migrated test database. Verify backend health, Web health, and current Agent protocol version before browser work.

**Step 2: Execute no-cost browser acceptance**

Using a real signed-in browser session:

1. open a canvas and start a thread;
2. ask the Agent to read canvas facts and verify no approval appears;
3. request a node mutation, inspect the exact approval proposal, reject it, and verify no revision changed;
4. request it again, approve it, and verify exactly one revision and event sequence;
5. refresh during `waiting_approval` and verify proposal recovery;
6. disconnect/reconnect SSE and verify no duplicate action;
7. verify another user/tenant cannot read or act on the run/resource;
8. verify old production/stage UI and routes are unavailable.

**Step 3: Execute deterministic media integration with provider fakes**

Exercise image/video/audio proposals, duplicate approval, success, provider failure, timeout, refund/release, unknown settlement, asset publish, and projection failure after media success. Assert ledger/task/resource facts rather than UI prose.

**Step 4: Optional bounded real call**

Do not run this step without a new explicit user authorization that identifies model, media kind, quoted maximum points, and target test canvas. If authorized, execute one low-cost image call, confirm exactly one charge and one retained resource, then publish/project through separately approved actions.

**Step 5: Final verification and release handoff**

```powershell
Set-Location backend
go test ./... -count=1
Set-Location ..\web
bun test
bun run build
Set-Location ..
git status --short
git log --oneline --decorate -12
```

Expected: all gates pass, worktree clean, commits are scoped and Conventional. Do not push, merge, tag, or deploy without current-turn explicit user authorization.

## Final completion checklist

- [ ] New runs use only runtime v5 / tool schema v6 / UI protocol v5.
- [ ] Exactly six atomic capabilities are executable.
- [ ] Every write and paid action has its own immutable approval proposal.
- [ ] No specialist/production/stage execution path remains active.
- [ ] Billing, task, resource, asset, and canvas revision facts are authoritative and idempotent.
- [ ] Generated resources survive downstream failure.
- [ ] SSE replay/process restart do not duplicate effects.
- [ ] Historical Agent records remain readable and are not destructively deleted.
- [ ] No semantic regex, keyword router, fixed workflow, hidden fallback, or alternate-model downgrade exists.
- [ ] README, CONTEXT, PRODUCTION, CHANGELOG, and Web design mapping describe the real implementation.
- [ ] Focused tests, full tests, Web build, deterministic integration, browser acceptance, and independent review pass.
- [ ] Any real paid call was separately authorized and bounded.
