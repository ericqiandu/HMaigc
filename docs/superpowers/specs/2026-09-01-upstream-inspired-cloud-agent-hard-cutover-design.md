# Upstream-inspired cloud Agent hard cutover — design

## Status and decision

The user approved a hard cutover from the current specialist and production-graph Agent runtime to a cloud-hosted adaptation of the upstream `ddcat-ai/open-ai-canvas` Agent architecture.

The new runtime keeps the upstream architecture's essential properties:

- one model-driven Agent loop;
- a small registry of atomic canvas capabilities;
- Skills loaded only when the Agent requests them;
- structured tool calls rather than fixed local workflows;
- explicit user confirmation before mutations.

It does not copy the upstream local Codex authentication or loopback-only deployment. HMaigc must remain usable by cloud users without installing a local process. Existing commercial identity, tenant isolation, model catalog, billing, media-task, asset, audit, and rollback services remain authoritative.

## Business objective

Provide a reliable cloud Agent that can understand a user's request, inspect the real canvas and project state, choose relevant Skills, and perform approved canvas or media actions without a locally hardcoded creative workflow.

The runtime must be substantially simpler than the current specialist graph while preserving commercial controls around money, permissions, data ownership, traceability, and generated assets.

## Why an exact upstream copy is not viable

Upstream runs a local TypeScript process that starts Codex `app-server`, connects through `127.0.0.1`, and exposes MCP canvas tools to the user's locally authenticated Codex session. That deployment model has no cloud multi-tenancy, centralized model billing, managed media tasks, or production tenant boundary.

Copying it byte-for-byte would require every user to install and authenticate a local Agent. It would also bypass HMaigc's authoritative model catalog and billing ledger. Therefore this design copies the architecture and protocol boundaries, not the local authentication and process topology.

The upstream browser-side fixed online prompt and hardcoded tool loop are also excluded. They would move semantic routing into the Web client and conflict with the project's single-path, model-driven orchestration rules.

## Goals

- A single cloud Agent execution path for all users.
- No local installation requirement.
- Model-driven intent, planning, Skill selection, and tool selection.
- Atomic, typed capabilities with narrow responsibilities.
- Separate confirmation for every canvas mutation and every paid media action.
- Existing model catalog, tenant permissions, billing ledger, media workers, and asset library remain authoritative.
- Explicit failure, idempotency, resumability, auditability, and structured observability.
- Fewer production responsibilities and less code than the current specialist and production-graph runtime.

## Non-goals

- Reproducing upstream's loopback Codex login flow.
- Preserving the current specialist runtime or production graph as a fallback.
- Encoding a fixed screenplay-to-storyboard-to-video workflow in Web or backend code.
- Inferring creative intent with regular expressions, keyword tables, aliases, or route enums.
- Automatically approving an entire workflow or automatically retrying a paid model call.
- Rewriting identity, model pricing, media generation, asset storage, or the billing ledger.

## First-principles constraints

1. The model is the Agent. Application code supplies facts, constraints, capabilities, and execution results; it does not replace semantic reasoning.
2. The backend is the commercial authority. The Agent cannot invent permissions, prices, balances, ownership, task state, or asset state.
3. A tool result is a fact, not a completion claim. The loop continues until the Agent responds, explicitly fails, or reaches a configured structural boundary.
4. A user's read authorization never implies mutation or spending authorization.
5. Each paid action and each canvas mutation receives its own confirmation. There is no blanket workflow authorization.
6. Failures remain failures. There is no hidden model switch, default workflow, silent retry, template result, or dual-path fallback.
7. Successfully generated assets are preserved even when later projection or presentation work fails.

## Target architecture

### 1. Cloud Agent loop

A focused backend module owns one conversation turn loop:

1. validate user, tenant, project, canvas, and selected model;
2. assemble bounded factual context;
3. expose the capability registry and Skill catalog descriptors;
4. invoke the configured Agent model through the existing provider and billing boundary;
5. validate structured tool calls;
6. execute read-only calls immediately or emit an approval request for protected calls;
7. append the factual result and continue the same turn;
8. stream structured events to the Web client;
9. persist the final response and delivery evidence.

The loop has structural limits for maximum tool calls, context size, and elapsed time. Reaching a limit is an explicit failure with diagnostics, not a fabricated answer.

### 2. Capability registry

The initial public capability surface contains five families:

- `canvas.read`: read the current canvas, selection, node metadata, and connection facts.
- `canvas.apply_ops`: apply validated node and edge operations after user confirmation.
- `assets.read_publish`: query owned project assets and publish an approved generated result.
- `media.generate`: create image, video, or optional audio tasks through existing media services after cost confirmation.
- `skills.load`: load one authorized Skill version on demand.

Capabilities use typed request and response contracts. They do not contain creative methodology or semantic routing. Canvas operations remain structural and deterministic. Media generation delegates pricing, reservation, task execution, settlement, and refund to existing authoritative services.

### 3. Skills

The Agent initially sees only Skill descriptors: identity, purpose, version, allowed capabilities, and checksum. Full instructions are loaded only through `skills.load` when the Agent decides they are relevant.

Skill content owns screenplay, character consistency, image prompting, video prompting, camera language, pacing, continuity, and other professional methods. The backend does not impose a fixed Skill order or specialist sequence.

### 4. Approval service

Protected tool calls create an immutable approval proposal containing:

- conversation and turn identity;
- tenant, user, project, and canvas scope;
- capability name and normalized arguments;
- human-readable effect preview;
- selected model and generation parameters when applicable;
- quoted estimated cost and pricing-version reference;
- proposal hash, expiration, and idempotency key.

The user approves exactly that proposal. Any material argument, model, parameter, scope, or price change invalidates the approval and requires a new proposal.

### 5. Web Agent panel

The Web client is a presentation and deterministic execution client. It:

- sends messages and current explicit scope;
- renders streamed Agent events;
- displays Skill and tool activity;
- renders approval cards and cost quotations;
- applies only server-authorized canvas operations;
- shows generated assets and final results;
- reconnects to an existing turn after refresh or network interruption.

The Web client does not perform semantic routing, select a hidden workflow, rewrite prompts, decide completion, or silently execute a protected action.

## Execution and confirmation lifecycle

1. The user submits a message in the canvas Agent panel.
2. The backend authenticates the request and freezes the factual scope for the turn.
3. The Agent reads context and may load a Skill or invoke read-only capabilities.
4. For a protected capability, the loop pauses and emits an approval proposal.
5. The user may approve, reject, or continue the conversation to revise the proposal.
6. Approval executes exactly once under its idempotency key.
7. A successful canvas operation records the applied operation version.
8. A successful media task settles actual cost, records the asset, and returns its real URL and metadata to the Agent.
9. A failed media task releases the reservation or records an idempotent refund according to the existing billing contract.
10. The loop resumes with the factual outcome. Any later protected action requires a new approval.
11. The final response references persisted delivery evidence rather than relying on prose alone.

## Billing and monetary consistency

- Agent reasoning and paid media calls use the existing dynamic model catalog and pricing records.
- A media approval quotes the model, parameters, price version, and estimated points before execution.
- The billing service reserves once, settles once, and refunds or releases once using stable idempotency keys.
- Duplicate confirmation, reconnect, browser retry, worker retry, and repeated callback cannot produce a second charge.
- Unknown settlement state is exposed as a billing error and blocks another paid attempt until reconciled.
- No Agent response may claim success from a provider response alone; real task, ledger, and asset evidence are required.

## Identity, tenancy, and authorization

- Every conversation, turn, proposal, tool call, task, asset, and canvas operation carries tenant, user, project, and canvas scope.
- Read and mutation permissions are checked independently at execution time.
- Client-supplied ownership, price, model capability, Skill instructions, and task results are never trusted.
- Tool results are filtered to the caller's authorized scope before entering model context.
- Shared or collaborative canvases retain the existing owner and collaborator permission semantics.

## Persistence and events

The new runtime persists only the facts necessary to resume and audit the simple loop:

- conversation and turn;
- ordered model messages and tool results;
- streamed event cursor;
- approval proposal and decision;
- tool execution and idempotency identity;
- billing, media-task, asset, and canvas-operation references;
- final delivery evidence and explicit terminal state.

Events are structured and replayable. A reconnecting client requests events after its last cursor. Event delivery may be repeated, but state transitions and charges remain idempotent.

## Failure model

- Invalid model output: fail the turn with a structured validation reason and retained diagnostic sample.
- Missing semantic decision: expose insufficient decision evidence; do not choose a default route.
- Permission or ownership failure: reject before execution and log the authoritative scope facts.
- Approval mismatch or expiry: reject and require a new proposal.
- Provider failure: record provider, request ID, normalized user message, and full internal diagnostic; do not retry automatically.
- Media failure: release or refund the reservation exactly once.
- Projection failure after media success: preserve the asset and expose projection failure separately.
- Stream interruption: retain the turn and allow cursor-based reconnection.
- Process restart: recover non-terminal turns from persisted state without repeating completed tools.

## Observability and audit

Each turn receives a trace ID. Structured logs and stored records correlate:

- user, tenant, project, canvas, conversation, and turn;
- model record and provider request;
- loaded Skill versions and checksums;
- proposed and approved tool arguments;
- billing reservation, settlement, refund, and ledger references;
- media task and resulting asset;
- canvas operation version;
- terminal delivery evidence or failure reason.

User-facing errors are concise. Provider payloads, request IDs, validation details, and stack diagnostics remain available to authorized operators rather than being rendered inside canvas nodes.

## Hard cutover and legacy data

- The new Agent loop becomes the only executable Agent path.
- Current specialist delegation, production graph, creative-concept experiment, and fixed stage-review runtime are retired rather than wrapped or retained as a fallback.
- Historical conversations, tasks, ledger entries, assets, and audit records remain immutable and readable where operationally required.
- Historical Agent runs cannot resume under the new runtime. Non-terminal legacy runs must be explicitly stopped or completed before deployment.
- No destructive schema or data operation may execute without a separate user confirmation that identifies the exact affected tables and records.
- Deployment uses one versioned cutover and normal image rollback. Runtime logic never silently switches back to the legacy path.

## Change budget

Expected production responsibilities:

- one focused Agent-loop module;
- one typed capability registry;
- one approval and event contract integrated with existing services;
- one simplified Web panel integration;
- retirement of the legacy executable runtime.

Expected scope is approximately 25–35 production files plus focused tests and documentation. Net production code should decrease after legacy retirement. Any material expansion into a second model gateway, fixed workflow engine, duplicate billing implementation, or compatibility layer requires a new design decision.

The implementation is intentionally staged so destructive retirement occurs only after the replacement passes contract and browser acceptance tests.

## Verification strategy

### Focused automated tests

- Agent loop continues across read-only tool results and stops only on a valid response or explicit failure.
- Capability schemas reject invalid or unauthorized inputs.
- Skill loading is authorized, versioned, and checksum-traceable.
- Mutation and paid capabilities cannot execute without a matching approval.
- Duplicate approvals and repeated callbacks execute and charge once.
- Success, failure, cancellation, timeout, and unknown billing-state paths remain consistent.
- Generated assets survive projection or presentation failure.
- Event replay resumes from a cursor without duplicating state transitions.
- Tenant and project isolation hold across reads, approvals, tools, and assets.

### Integration tests

- Existing model catalog, provider bridge, billing ledger, media worker, asset publication, and canvas operations are exercised through their real contracts with deterministic fakes at external provider boundaries.
- Process restart and Web reconnect recover a non-terminal approved task without duplicate execution.
- Legacy runtime routes are absent or explicitly non-executable after cutover.

### Browser acceptance

- Cloud user signs in without installing a local process.
- Agent reads the selected canvas.
- Canvas mutation presents and honors an approval card.
- Image, video, and optional audio generation each quote cost and require separate approval.
- Rejecting or revising one stage does not start downstream paid work.
- Refresh and network interruption recover real task state.
- User and operator errors remain concise while diagnostics are traceable.

Automated tests do not invoke paid production models. Final production-like acceptance uses one explicitly authorized, bounded, low-cost real call only after all deterministic gates pass.

## Completion criteria

The cutover is complete only when:

- one cloud Agent path is active;
- the legacy executable runtime and experimental creative-concept path are retired;
- permissions, billing, assets, and canvas mutations have auditable evidence;
- all protected actions require independent approval;
- failure, retry, reconnect, and duplicate-request tests pass;
- browser acceptance passes without local installation;
- documentation describes the actual deployed architecture;
- the release can roll back as one versioned unit.
