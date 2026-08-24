# Agent Internal Task Audience Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hide durable Agent text-model tasks from every ordinary user task surface and operation while preserving administrator, billing, audit, worker, and recovery access.

**Architecture:** Add a required `TaskAudience` domain fact and migrate every existing row explicitly. Customer repository queries enforce `customer` before pagination/counting; user detail/log/mutation services fail closed for `internal`, while internal orchestration and administrator repositories continue using unrestricted task identity.

**Tech Stack:** Go 1.x, GORM, SQLite/PostgreSQL migrations, Gin service tests.

**Spec:** `docs/superpowers/specs/2026-08-23-agent-native-streaming-chat-design.md`

## Global Constraints

- Hard cutover only: no nullable audience, runtime default, compatibility branch, or Web-only filter.
- `agent_runtime_model` is `internal`; every customer-created media/text task is `customer`.
- Ordinary user access to an internal task returns the same not-visible semantics as an absent task.
- Billing orders, task logs, provider evidence, workers, administrator APIs, and Agent recovery retain unrestricted internal access.
- Preserve every unrelated working-tree hunk; stage only this plan's files and hunks.
- Milestone budget: at most 8 production files and about 350 net-new production lines.

---

### Task 1: Persist the Task audience fact

**Files:**
- Modify: `backend/internal/model/models.go`
- Modify: `backend/internal/database/schema.go`
- Modify: `backend/internal/database/schema_migrations.go`
- Test: `backend/internal/database/schema_test.go`

**Interfaces:**
- Produces: `model.TaskAudience`, `model.TaskAudienceCustomer`, `model.TaskAudienceInternal`, and non-null indexed `Task.Audience`.
- Produces: a migration that first assigns `customer` to all historical rows and then assigns `internal` where `type = 'agent_runtime_model'`.

- [ ] **Step 1: Write failing migration tests** that create pre-audience customer and Agent rows, run the migration, and assert exact non-empty audiences plus a database-level rejection of invalid/empty audience where supported by the repository's schema contract.
- [ ] **Step 2: Run RED:** `go test ./internal/database -run 'TaskAudience|Schema' -count=1` from `backend`; confirm failure is the absent field/migration.
- [ ] **Step 3: Add the typed domain constants, field, index/check constraint, and explicit two-phase data migration.** Do not infer audience at read time.
- [ ] **Step 4: Run GREEN:** repeat the focused database command and confirm zero failures.

### Task 2: Enforce customer visibility before query pagination

**Files:**
- Modify: `backend/internal/repository/repository.go`
- Test: `backend/internal/repository/repository_test.go` or the closest existing task repository test file

**Interfaces:**
- Produces: `TaskForCustomer(userID, id)` and customer-only `Tasks(...)` / `TaskLogs(...)` behavior used by ordinary user services.
- Retains: unrestricted `Task(id)` for workers/admin/audit and all billing joins.

- [ ] **Step 1: Write failing repository tests** with interleaved customer/internal rows proving list filtering occurs in SQL before `ORDER/LIMIT`, customer detail/log lookup cannot observe internal IDs, and unrestricted lookup still returns them.
- [ ] **Step 2: Run RED:** `go test ./internal/repository -run 'TaskAudience|CustomerTask' -count=1`; confirm internal rows leak today.
- [ ] **Step 3: Implement explicit customer-scoped repository methods/conditions.** Include `audience` in selected Task columns so downstream validation does not depend on an omitted zero value.
- [ ] **Step 4: Run GREEN:** repeat the focused repository command.

### Task 3: Close ordinary user operations and mark Agent tasks internal

**Files:**
- Modify: `backend/internal/service/service.go`
- Modify: `backend/internal/service/agent_runtime.go`
- Test: `backend/internal/service/task_security_test.go`
- Test: `backend/internal/service/agent_runtime_test.go`

**Interfaces:**
- Consumes: customer-scoped repository accessors and `TaskAudienceInternal`.
- Produces: list/detail/log/cancel/retry fail-closed behavior and internal Agent model Task creation.

- [ ] **Step 1: Write failing service tests** proving user list, detail, logs, cancel, and retry cannot observe/modify an internal task; a customer media task remains accessible; administrator/worker lookup and Agent model processing still work; newly created Agent model tasks are internal.
- [ ] **Step 2: Run RED:** `go test ./internal/service -run 'InternalTask|TaskAudience|StartAgentRuntimeCreates' -count=1`; confirm failures are visibility/creation behavior.
- [ ] **Step 3: Switch ordinary user services to the customer contract, return stable `agent_internal_task_not_user_accessible` not-visible semantics, and set Agent task audience explicitly at construction.** Ensure retry clones cannot turn internal tasks into customer tasks because user retry never receives them.
- [ ] **Step 4: Run GREEN:** repeat the focused service command.
- [ ] **Step 5: Run milestone gates:** `go test ./internal/database ./internal/repository ./internal/service -count=1`, then inspect `git diff --stat` against the 8-file/350-line production budget.

### Task 4: Review and commit the isolated milestone

**Files:**
- Review only: all milestone-one diffs and tests above

**Interfaces:**
- Produces: one independently revertible commit containing only Task audience isolation.

- [ ] **Step 1: Review requirements, migration ordering, user/admin/worker boundaries, billing visibility, pagination correctness, idempotency, and error semantics against the spec.**
- [ ] **Step 2: Run `git diff --check` and fresh focused tests.**
- [ ] **Step 3: Inspect each staged hunk with `git diff --cached`; exclude every unrelated pre-existing hunk.**
- [ ] **Step 4: Commit using `feat(agent): 内部任务 - 隔离文本模型任务受众` without pushing.**
