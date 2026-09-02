# 画布跨浏览器删除事实 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让任一浏览器删除的画布成为服务端持久事实，阻止其他浏览器用旧本地缓存复活同一画布。

**Architecture:** 使用独立 `CanvasProjectDeletion` 墓碑作为删除真源；删除事务原子写墓碑并清理画布聚合，列表接口同步可见墓碑，创建接口对墓碑 ID 返回 410。Web 先应用墓碑再合并，并在创建与删除竞态收到 410 时清理陈旧本地副本。

**Tech Stack:** Go、Gin、GORM、SQLite/PostgreSQL、TypeScript、Zustand、Bun test

**Spec:** `docs/superpowers/specs/2026-08-25-canvas-deletion-tombstone-design.md`

## Global Constraints

- 保存、权限与删除错误必须显式失败，不得静默兜底。
- 不新增 `any` 或 `as any`；前后端字段名与类型保持一致。
- 只修改画布删除与同步契约，不改 Agent 对话、用户现有原型路由或其他未提交改动。
- 数据表变化必须同步 `docs/content/docs/backend/backend-database.mdx`。
- 严格 RED -> GREEN；最终执行 Go 全量测试/build、Web tests/typecheck/build 与 `git diff --check`。

---

### Task 1: 服务端删除墓碑与事务边界

**Files:**
- Modify: `backend/internal/model/canvas_collaboration.go`
- Modify: `backend/internal/database/schema.go`
- Modify: `backend/internal/repository/canvas_collaboration.go`
- Test: `backend/internal/service/user_data_test.go`
- Test: `backend/internal/database/schema_test.go`

**Interfaces:**
- Produces: `model.CanvasProjectDeletion{CanvasID, UserID, TeamID, DeletedByUserID, DeletedAt}`
- Produces: `Repository.CanvasProjectDeletion(canvasID string)`
- Produces: `Repository.CanvasProjectDeletionsForActor(userID string)`
- Produces: `Repository.CanvasProjectDeletionForActor(userID, canvasID string)`
- Changes: `Repository.DeleteCanvasProjectWithCollaboration(project *model.CanvasProject, deletedByUserID string, deletedAt time.Time)`

- [ ] **Step 1: Write failing backend tests**

Add tests proving that successful deletion writes a content-free tombstone, forced project-delete failure rolls it back, current team members can list a team deletion, unrelated users cannot see it, and `database.Models()` registers the new model.

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/service ./internal/database -run 'CanvasProjectDeletion|DeleteUserCanvasProject|ModelsRegistersCanvas' -count=1`

Expected: FAIL because `CanvasProjectDeletion` and repository deletion queries do not exist.

- [ ] **Step 3: Implement the model and repository transaction**

Use this exact persistent shape:

```go
type CanvasProjectDeletion struct {
    CanvasID        string    `json:"id" gorm:"primaryKey;size:80"`
    UserID          string    `json:"-" gorm:"index;size:36"`
    TeamID          string    `json:"-" gorm:"index;size:36"`
    DeletedByUserID string    `json:"-" gorm:"index;size:36"`
    DeletedAt       time.Time `json:"deletedAt" gorm:"index"`
}
```

Register it in `database.Models()`. In the repository deletion transaction create the tombstone from the already-authorized project before deleting child rows and the project. Query personal tombstones by owner and team tombstones by an active membership SQL predicate.

- [ ] **Step 4: Run focused tests to verify GREEN**

Run: `go test ./internal/service ./internal/database -run 'CanvasProjectDeletion|DeleteUserCanvasProject|ModelsRegistersCanvas' -count=1`

Expected: PASS.

### Task 2: 服务端同步、幂等删除与 410 契约

**Files:**
- Modify: `backend/internal/service/user_data.go`
- Modify: `backend/internal/handler/user_data.go`
- Test: `backend/internal/service/user_data_test.go`

**Interfaces:**
- Produces: `CanvasProjectDeletionSummary{ID string, DeletedAt time.Time}`
- Produces: `Service.UserCanvasProjectDeletions(userID string)`
- Changes: `GET /canvas-projects` response to `{projects, deletions}`
- Changes: deleted canvas ID creation to `AuthError{Status: http.StatusGone}`

- [ ] **Step 1: Write failing service tests**

Add tests proving: create-after-delete returns HTTP 410 and does not recreate; repeated delete by the original personal owner succeeds; unrelated users still receive 404; summary returns the exact deleted ID and server timestamp.

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/service -run 'CanvasProject.*(Gone|Idempotent|DeletionSummaries)' -count=1`

Expected: FAIL because create ignores tombstones and deleted projects cannot be deleted idempotently.

- [ ] **Step 3: Implement service and handler contracts**

Before new-canvas quota calculation, query the tombstone and return 410 if present. During delete, use the authorized project returned by `canvasAccess`; if the project is missing, accept the operation only when `CanvasProjectDeletionForActor` finds an authorized tombstone. Return deletion summaries beside project summaries.

- [ ] **Step 4: Run focused tests to verify GREEN**

Run: `go test ./internal/service -run 'CanvasProject.*(Gone|Idempotent|DeletionSummaries)' -count=1`

Expected: PASS.

### Task 3: Web 墓碑优先合并与并发删除收口

**Files:**
- Create: `web/src/lib/canvas/canvas-sync-state.ts`
- Create: `web/src/lib/canvas/canvas-sync-state.test.ts`
- Modify: `web/src/services/api/user-data.ts`
- Modify: `web/src/services/user-data-sync.ts`

**Interfaces:**
- Produces: `RemoteCanvasDeletion { id: string; deletedAt: string }`
- Produces: `mergeCanvasProjects(local, changedRemote, deletions)`
- Produces: `isRemoteCanvasDeletedError(error: unknown)` using `UserDataRequestError.status === 410`

- [ ] **Step 1: Write failing pure Web tests**

Test that a deletion ID always removes the local project even when its `updatedAt` is newer; unrelated local-only projects remain upload candidates; only a `UserDataRequestError` with status 410 is classified as authoritative deletion.

- [ ] **Step 2: Run tests to verify RED**

Run: `bun test src/lib/canvas/canvas-sync-state.test.ts`

Expected: FAIL because the pure sync contract module does not exist.

- [ ] **Step 3: Implement merge and race handling**

Parse `deletions` from the list API. Call the pure merge function instead of the generic local-first merge. Wrap each remote canvas create: on 410 call `finishProjectDeletions([id])`, flush localForage, and continue; rethrow every other error unchanged.

- [ ] **Step 4: Run focused Web tests to verify GREEN**

Run: `bun test src/lib/canvas/canvas-sync-state.test.ts`

Expected: PASS.

### Task 4: 文档、全量验证与聚焦提交

**Files:**
- Modify: `docs/content/docs/backend/backend-database.mdx`
- Modify: `docs/content/docs/pending-test.mdx` if the repository's current pending-test structure accepts this canvas behavior
- Review: all task-related diffs only

**Interfaces:**
- Documents the tombstone table, visibility, 410 conflict and historical limitation.

- [ ] **Step 1: Update database and pending-test documentation**

Document that tombstones contain no canvas content, deletion is atomic, visible by personal/team scope, permanent for an ID, and historical hard deletes require one post-upgrade delete to establish a tombstone.

- [ ] **Step 2: Run backend gates**

Run from `backend/`: `go test ./...`, `go test -race ./internal/service ./internal/repository`, and `go build ./...`.

- [ ] **Step 3: Run Web gates**

Run from `web/`: `bun test`, `bunx tsc --noEmit`, and `bun run build`.

- [ ] **Step 4: Review and repair once**

Review requirement coverage, plan, diff, API types, migration, permission isolation, idempotency, concurrent create/delete, failure rollback, docs and tests. Apply one concentrated repair pass, rerun only affected focused gates, then perform one targeted re-review.

- [ ] **Step 5: Run final repository checks**

Run: `git diff --check`, `git status --short`, and inspect `git diff --name-only` plus task-only hunks. Do not stage `.agents/memory/hmaigc/core.md`, `web/src/router.tsx`, or `web/src/pages/prototype/`.

- [ ] **Step 6: Commit the verified task files only**

Stage only the tombstone implementation, its tests, spec/plan and database documentation. Commit message:

```text
fix(canvas): 画布删除 - 阻止跨浏览器缓存复活已删除项目
```

Do not push.
