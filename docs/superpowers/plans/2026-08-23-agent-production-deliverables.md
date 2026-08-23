# Agent Production Deliverables Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `production.plan` represent the exact per-shot media deliverables so a video-only Agent run creates, commits, and verifies only script and video artifacts.

**Architecture:** Add a strict per-shot `deliverables` value object in the agent runtime domain and use it as the only source for prompt validation and Artifact Ledger derivation. Keep `canvas.commit` exact-set semantics, but project nodes, storyboard row bindings, and edges from the artifacts the immutable plan actually declares. Delivery verification remains generic and succeeds from the resulting committed text/video facts.

**Tech Stack:** Go 1.25, GORM, SQLite/PostgreSQL repository tests, existing Agent Runtime state machine, Bun/Vite Web verification.

**Spec:** `docs/superpowers/specs/2026-08-23-agent-production-deliverables-design.md`

## Global Constraints

- Preserve every unrelated user modification in the dirty worktree; never reset or checkout over them.
- Do not add semantic keyword matching, prompt inference, fallback deliverables, model downgrade, fake resources, or verifier exceptions.
- Do not add a database migration; the contract is persisted in the existing `shots_json` field.
- Do not call a paid media provider during implementation or acceptance.
- Keep billing, approval, Task audience, Artifact state transitions, revision/CAS, SSE, and Agent UI behavior unchanged.
- Modify at most 7 production files and keep net new production code near 180–320 lines; the shared stored-plan validation may touch both render and commit, but re-plan before expanding into another subsystem.
- Use one final focused Conventional Commit and do not push.

---

### Task 1: Define the strict per-shot deliverable contract

**Files:**
- Modify: `backend/internal/agentruntime/production.go`
- Create: `backend/internal/agentruntime/production_test.go`

**Interfaces:**
- Produces: `type ProductionShotDeliverable string`
- Produces: constants `ProductionShotDeliverableStoryboardImage` and `ProductionShotDeliverableVideoClip`
- Produces: `ShotPlanDraft.Deliverables []ProductionShotDeliverable`
- Consumes: existing `ProductionPlanDraft.Validate()` callers in service and repository layers.

- [ ] **Step 1: Write failing domain validation tests**

Add literal video-only and validation cases that demonstrate the desired JSON contract:

```go
videoOnly := agentruntime.ProductionPlanDraft{
    Title: "5秒纯视频", TargetDurationMS: 5_000, Script: "抽象光影。",
    Shots: []agentruntime.ShotPlanDraft{{
        ShotKey: "shot-1", Order: 1, DurationMS: 5_000,
        ScriptText: "光带聚合", VideoPrompt: "原创抽象光影",
        Deliverables: []agentruntime.ProductionShotDeliverable{
            agentruntime.ProductionShotDeliverableVideoClip,
        },
        Dependencies: []string{},
    }},
}
```

Assert `videoOnly.Validate()` succeeds. Add table cases for empty, duplicate and unknown deliverables; missing or unused image/video Prompt; and `referenceKeys` on a video-only shot.

- [ ] **Step 2: Run the focused test and verify RED**

Run from `backend`:

```powershell
go test ./internal/agentruntime -run 'TestProductionPlan.*(VideoOnly|Deliverable|Validation)' -count=1
```

Expected: compilation or assertion failure because `ProductionShotDeliverable` and the selective Artifact behavior do not exist.

- [ ] **Step 3: Implement the minimal domain contract**

In `agentruntime/production.go`:

```go
type ProductionShotDeliverable string

const (
    ProductionShotDeliverableStoryboardImage ProductionShotDeliverable = "storyboard_image"
    ProductionShotDeliverableVideoClip       ProductionShotDeliverable = "video_clip"
)
```

Add `Deliverables []ProductionShotDeliverable` to `ShotPlanDraft`. Validate one or two unique known values. Require exactly the Prompt belonging to each selected deliverable, reject a non-empty Prompt for an unselected deliverable, and reject `referenceKeys` unless `storyboard_image` is selected. Keep all existing structural checks.

- [ ] **Step 4: Run domain/repository validation tests and verify GREEN**

```powershell
go test ./internal/agentruntime -run 'TestProductionPlan.*(VideoOnly|Deliverable|Validation)' -count=1
```

Expected: all new domain validation tests pass.

---

### Task 2: Derive immutable Artifact Ledger and delivery counts from the contract

**Files:**
- Modify: `backend/internal/repository/agent_runtime_production.go`
- Test: `backend/internal/repository/agent_runtime_production_test.go`
- Test: `backend/internal/repository/agent_runtime_postgres_test.go`
- Test: `backend/internal/repository/agent_runtime_execution_test.go`
- Test: `backend/internal/service/agent_runtime_production_test.go`
- Test: `backend/internal/service/agent_runtime_production_reference_test.go`
- Test: `backend/internal/service/agent_runtime_production_postgres_test.go`
- Test: `backend/internal/service/agent_runtime_canvas_commit_test.go`

**Interfaces:**
- Consumes: `ShotPlanDraft.Deliverables` from Task 1.
- Produces: `ExpectedDeliveryJSON` whose `storyboardImages` and `videoClips` equal the declared counts.
- Produces: exact Artifact identities used unchanged by render and commit services.

- [ ] **Step 1: Write the failing repository Artifact tests**

Use the Task 1 video-only Draft and assert `AppendAgentProductionPlanVersion` succeeds with literal kinds `[script, video_clip]`. Assert the persisted delivery JSON decodes to:

```json
{"scripts":1,"referenceImages":0,"storyboardImages":0,"videoClips":1}
```

Add image-only and dual-output fixtures with hand-counted Artifact kinds and delivery counts.

- [ ] **Step 2: Run the repository test and verify RED**

```powershell
go test ./internal/repository -run 'Test.*ProductionPlan.*(VideoOnly|ImageOnly|Deliverable)' -count=1
```

Expected: video-only still contains a storyboard Artifact/count under the old unconditional derivation.

- [ ] **Step 3: Implement selective count and Artifact derivation**

Replace `len(shots)` delivery counts with a loop over declared deliverables. In `productionArtifactsForPlan`, append only the Artifact kind explicitly declared by each shot, while retaining the single succeeded script and declared reference images. Do not infer from Prompt presence or provider capability.

- [ ] **Step 4: Update valid test fixtures to declare their existing intent**

Every valid historical fixture containing both Prompts must explicitly set:

```go
Deliverables: []agentruntime.ProductionShotDeliverable{
    agentruntime.ProductionShotDeliverableStoryboardImage,
    agentruntime.ProductionShotDeliverableVideoClip,
},
```

Equivalent JSON fixtures must include:

```json
"deliverables":["storyboard_image","video_clip"]
```

Do not add defaults in production code merely to keep tests passing.

- [ ] **Step 5: Run repository and production service tests and verify GREEN**

```powershell
go test ./internal/repository ./internal/service -run 'Test.*(ProductionPlan|ProductionRender|TextToVideo|ProductionReference)' -count=1
```

Expected: selective Artifact tests and existing render prerequisite tests pass with explicit fixtures.

---

### Task 3: Project and commit only declared canvas deliverables

**Files:**
- Modify: `backend/internal/service/agent_runtime_canvas_projection.go`
- Test: `backend/internal/service/agent_runtime_canvas_commit_test.go`
- Test: `backend/internal/service/agent_runtime_delivery_test.go`

**Interfaces:**
- Consumes: exact Artifact Ledger from Task 2.
- Produces: `CanvasMutationPatch` containing only declared media nodes and valid edges.
- Preserves: `productionCanvasCommitFacts` exact Artifact-ID set comparison.
- Produces: generic delivery evidence through existing `committedPlanDeliveryArtifacts`.

- [ ] **Step 1: Write the failing video-only projection test**

Build a plan whose shot JSON contains `"deliverables":["video_clip"]`, with script and succeeded video Artifacts plus a ready video Resource. Assert literal behavior:

- two upsert nodes: one `script`, one `video`;
- one connection from the script node directly to the video node;
- zero image nodes;
- the storyboard row has no serialized `imageNodeId` and has a non-empty `videoNodeId`;
- every Artifact receives exactly one binding.

- [ ] **Step 2: Run the canvas test and verify RED**

```powershell
go test ./internal/service -run 'Test.*ProductionCanvas.*VideoOnly' -count=1
```

Expected: `production canvas shot artifacts are incomplete` or an assertion showing an unwanted image node.

- [ ] **Step 3: Implement dynamic projection**

Make row node IDs optional with `omitempty`. For each shot, look up only its declared Artifact roles and reject missing or extra roles. Construct edges by exact shape:

```text
image-only: script -> image
video-only: script -> video
dual:       script -> image -> video
```

Only connect reference nodes to a declared image node. Keep stable IDs, node metadata, resource validation, bindings, and exact artifact-set checks unchanged.

- [ ] **Step 4: Add/extend generic delivery evidence regression**

Commit the video-only patch through the existing service path and assert delivery evidence contains a canvas revision, one `text` canvas URL and one real `video` resource URL, with no `image` Artifact. Do not mock the verifier or compute expected artifacts with production helpers.

- [ ] **Step 5: Run canvas, delivery, and render tests and verify GREEN**

```powershell
go test ./internal/service -run 'Test.*(ProductionCanvas|DeliveryEvidence|ProductionRender|TextToVideo)' -count=1
```

Expected: pure video and existing image/dual/reference paths pass.

---

### Task 4: Publish the strict model tool contract and current architecture

**Files:**
- Modify: `backend/internal/service/agent_runtime.go`
- Modify: `README.md`
- Test: `backend/internal/service/agent_runtime_test.go`

**Interfaces:**
- Consumes: exact `production.plan` JSON contract from Tasks 1–3.
- Produces: model instructions that require `deliverables` and forbid undeclared Prompt fields.
- Documents: current single-source plan/Ledger/projection/verifier data flow.

- [ ] **Step 1: Update the behavior-level prompt contract test**

Extend the existing decision server test to capture the real model request and assert the prompt communicates a video-only example with `deliverables:["video_clip"]`, omits `imagePrompt` from that example, and explains that selected deliverables alone create Artifacts. Keep assertions on the request actually sent, not by reading source files.

- [ ] **Step 2: Run the prompt test and verify RED**

```powershell
go test ./internal/service -run 'Test.*AgentRuntime.*Prompt' -count=1
```

Expected: captured prompt lacks the new deliverable contract.

- [ ] **Step 3: Update runtime instructions and README**

Replace the unconditional dual-output `production.plan` example with explicit pure-video and dual-output rules. State that missing deliverables are invalid, Prompt fields must exactly match selected kinds, reference keys require a storyboard image, and text-to-video never creates an undeclared image. Update README’s current Agent architecture paragraph so plan versions, Ledger creation, projection and verifier all use `deliverables` as the single source.

- [ ] **Step 4: Run focused Agent Runtime tests and verify GREEN**

```powershell
go test ./internal/agentruntime ./internal/repository ./internal/service -run 'Test.*(AgentRuntime|Production|Delivery)' -count=1
```

Expected: focused contract suite passes without network media calls.

---

### Task 5: Review, verify, and form one focused commit

**Files:**
- Review only: all task-related source, tests, spec, plan and README files.
- Stage only: exact task-related files or hunks after verification.

**Interfaces:**
- Consumes: completed Tasks 1–4.
- Produces: evidence-backed acceptance and one independently revertible commit.

- [ ] **Step 1: Inspect scope and budget**

```powershell
git status --short
git diff --stat
git diff -- backend/internal/agentruntime/production.go backend/internal/repository/agent_runtime_production.go backend/internal/service/agent_runtime_canvas_projection.go backend/internal/service/agent_runtime.go README.md docs/superpowers/specs/2026-08-23-agent-production-deliverables-design.md docs/superpowers/plans/2026-08-23-agent-production-deliverables.md
```

Confirm no provider, billing, Task audience, SSE, Web UI, database schema or unrelated user edit entered the implementation.

- [ ] **Step 2: Perform the explicit independent review**

Review the requirement, spec, plan and actual diff for:

- exact plan DTO and error contract;
- Artifact identity/count determinism and CAS replay;
- image-only, video-only and dual projection;
- reference ownership and prerequisite safety;
- commit exact-set and revision/CAS behavior;
- generic delivery evidence with no case-specific verifier branch;
- no migration, fallback, model downgrade, semantic keyword logic or paid call;
- README and tests matching the actual implementation.

Collect all Important/Critical findings before making one concentrated correction.

- [ ] **Step 3: Run focused tests after concentrated correction**

```powershell
go test ./internal/agentruntime ./internal/repository ./internal/service -run 'Test.*(AgentRuntime|Production|Delivery)' -count=1
```

Expected: PASS.

- [ ] **Step 4: Run final backend gates**

From `backend`:

```powershell
go test ./... -count=1
go test -race ./internal/agentruntime ./internal/repository ./internal/service -count=1
go build ./...
```

Expected: all commands exit 0. PostgreSQL/Docker-only tests may report an explicit environment skip; record the exact output rather than claiming execution.

- [ ] **Step 5: Run final Web gates**

From `web`:

```powershell
bun test
bun run build
```

Expected: all tests, TypeScript check, Vite build and bundle budget pass. These are regression gates only; no Web implementation is expected.

- [ ] **Step 6: Run diff and targeted re-review gates**

```powershell
git diff --check
git diff --cached --check
```

Re-review only corrected areas and their neighboring contracts. If new cross-module Important/Critical defects appear, stop and return to architecture rather than entering a third patch loop.

- [ ] **Step 7: Stage exact task scope and commit once**

Use `git add` only for new task documents and clean task files. For any file containing unrelated user hunks, stage only the exact task hunk and inspect:

```powershell
git diff --cached --stat
git diff --cached
```

Commit only after the staged diff is focused:

```powershell
git commit -m "fix(agent): support explicit production deliverables"
```

Do not push.
