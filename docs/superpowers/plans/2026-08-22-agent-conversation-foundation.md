# HMaigc Agent 对话运行时基础实施计划

> **For Codex:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. 本计划涉及共享 Run 状态机、数据库事务、审批和 SSE 游标，必须由主代理串行执行；禁止使用 `superpowers:subagent-driven-development`。

**Goal:** 在不建立第二套 Agent Runtime 的前提下，把现有 Run/Checkpoint 任务日志硬切为可恢复的 Thread/Turn/Item 对话协议，补齐手动/自动审批矩阵、运行中追加指令、显式中断、版本化 SSE 与前端严格契约。

**Architecture:** `AgentRunEvent` 继续作为追加式审计真源，新增 `AgentTimelineItem` 作为同事务写入、可从事件重建的查询投影。Go Runtime 是唯一商业真源；Service 将领域事件纯投影为版本化 `AgentUIEvent`，Handler 只负责鉴权、严格解码与 SSE。Web 只解析版本化事件并提交用户消息、审批、澄清、steer、interrupt，不生成工具、任务、账单、资源或画布事实。

**Tech Stack:** Go 1.25、Gin、GORM、SQLite/PostgreSQL、React 19、TypeScript 7、Vite 8、Bun Test、现有持久 SSE。

**Spec:** `docs/superpowers/specs/2026-08-22-agent-conversation-foundation-design.md`

**Global constraints:** 不新增第三方依赖；禁止 `any`；禁止正则/关键词语义路由；禁止旧新协议双轨和隐式 fallback；所有写入保持租户/用户/项目/画布作用域；已提交的付费任务、账单、资源和 Artifact 在中断后仍保留；AI 对话架构变更必须同步根 `README.md`；不修改计费算法、Provider、Artifact 生成和画布视觉。

## 变更预算与熔断

仓库核对后，批准规格中的“第一阶段不超过 8 个生产文件”不能在保持职责单一的情况下覆盖完整硬切：现有边界分布在 Runtime、模型/迁移、Repository、Service、Handler、Web 契约与 Hook。强行压缩会把状态机、事务和协议混入同一大文件。

因此把第一阶段拆成四个可独立验证、可独立回滚的提交里程碑：

1. **1A — 领域状态机与版本栅栏：** 最多 4 个生产文件，目标净新增不超过 350 行。
2. **1B — 持久事实与 Artifact 时间线：** 最多 5 个生产文件，目标净新增不超过 550 行。
3. **1C — API 与版本化事件：** 最多 4 个生产文件，目标净新增不超过 500 行。
4. **1D — Web 硬切与恢复：** 最多 2 个生产文件，目标净新增不超过 400 行。

整个阶段预计触及 15 个既有生产文件，净新增目标不超过 1,400 行。任一里程碑超过预算 50%、出现超过 500 行的新生产文件、定向复审仍发现新的跨模块 Critical/Important，立即停止并回到架构拆分，不继续第三轮补丁。

昂贵门禁只在 1A、1B、1C、1D 各自稳定时执行 focused tests；全量 Go、PostgreSQL、Web build/test 只在 1D 最终收口执行。

---

## Milestone 1A — 领域状态机、审批语义与版本栅栏

### Task 1: 先用失败测试冻结审批、steer 与 interrupt 状态机

**Files:**

- Modify: `backend/internal/agentruntime/runtime_test.go`
- Modify: `backend/internal/agentruntime/contracts_test.go`
- Modify: `backend/internal/agentruntime/tools.go`
- Modify: `backend/internal/agentruntime/contracts.go`
- Modify: `backend/internal/agentruntime/runtime.go`
- Modify: `backend/internal/service/agent_runtime.go`

- [ ] **Step 1: 为审批矩阵写失败测试**

在 `runtime_test.go` 增加表驱动测试，逐项验证：

```go
tests := []struct {
    mode agentruntime.ExecutionMode
    tool agentruntime.ToolName
    want agentruntime.RunStatus
}{
    {agentruntime.ExecutionGuided, agentruntime.ToolSkillLoad, agentruntime.RunWaitingTool},
    {agentruntime.ExecutionGuided, agentruntime.ToolProductionPlan, agentruntime.RunWaitingApproval},
    {agentruntime.ExecutionGuided, agentruntime.ToolCanvasCommit, agentruntime.RunWaitingApproval},
    {agentruntime.ExecutionGuided, agentruntime.ToolProductionRender, agentruntime.RunWaitingApproval},
    {agentruntime.ExecutionAutomatic, agentruntime.ToolProductionPlan, agentruntime.RunWaitingTool},
    {agentruntime.ExecutionAutomatic, agentruntime.ToolCanvasCommit, agentruntime.RunWaitingTool},
    {agentruntime.ExecutionAutomatic, agentruntime.ToolProductionRender, agentruntime.RunWaitingApproval},
}
```

运行：

```powershell
cd backend
go test ./internal/agentruntime -run 'Test.*Approval.*Mode' -count=1
```

预期：RED；guided 的 L1 当前仍进入 `waiting_tool`。

- [ ] **Step 2: 为 steer 身份与安全边界写失败测试**

新增结构类型并先写契约测试：

```go
type SteerRequest struct {
    ClientRequestID     string
    Message             string
    ExpectedStateVersion int
}

type PendingSteer struct {
    ClientRequestID string `json:"clientRequestId"`
    Message         string `json:"message"`
}
```

测试必须证明：活动 Run 可在安全边界追加；相同 `clientRequestId + message` 是幂等重放；同一 ID 不同正文冲突；终态 Run 拒绝追加；消息不会改写原始 `UserMessage`。

- [ ] **Step 3: 为 interrupt CAS 与终态语义写失败测试**

新增 `Interrupt(current, expectedStateVersion)` 的测试，验证：

- 只有版本匹配且非终态 Run 可转为 `RunCancelled`；
- 新事件是独立 `run.interrupted`，不能复用 `run.failed`；
- 清除尚未开始的 pending model/tool/approval/clarification；
- 已开始的付费工具只停止后续编排，不伪造退款或删除外部任务事实；
- 已终态或版本不匹配显式返回稳定领域错误。

- [ ] **Step 4: 实现最小领域合同**

在 `contracts.go` 增加：

```go
const EventRunInterrupted EventKind = "run.interrupted"
const EventRunSteered EventKind = "run.steered"
const EventArtifactAvailable EventKind = "artifact.available"
const EventUserMessageAdded EventKind = "user.message"
const EventAgentMessageCompleted EventKind = "agent.message"

var ErrSteerConflict = errors.New("agent steer conflict")
var ErrInterruptConflict = errors.New("agent interrupt conflict")
```

在 `RuntimeState` 中只增加结构化 `PendingSteers []PendingSteer`，不把 steer 拼回 `UserMessage`。在 `runtime.go` 实现纯函数 `AppendSteer`、`ConsumePendingSteersAtSafeBoundary` 与 `Interrupt`；安全边界由状态枚举和 `PendingToolStarted` 决定，不依赖提示词内容。

把 `CurrentRuntimeVersion` 与 `CurrentPolicyVersion` 移到 `agentruntime` 领域包：本次 checkpoint/item 协议与审批策略分别递增版本，`service/agent_runtime.go` 只引用领域常量，不再维护私有重复常量。`CurrentToolSchemaVersion` 只有工具 DTO 真变化时才递增，禁止拿工具版本冒充 Runtime 版本。

初始化必须追加 `run.created` 和 `user.message` 两个独立 sequence；最终成功必须先追加 `agent.message` 再追加 `run.completed`。保持“一条持久事件最多改变一个 Item”的不变量，避免同一 SSE sequence 承载多个 UI 事件。

- [ ] **Step 5: 修正唯一审批函数**

`ApprovalRequiredFor` 只能表达下表，不保留旧逻辑分支：

```go
func ApprovalRequiredFor(policy ToolPolicy, mode ExecutionMode) bool {
    if policy.RiskLevel == ToolRiskCost { return true }
    return mode == ExecutionGuided && policy.RiskLevel == ToolRiskWrite
}
```

未知 mode/risk 在调用前由结构校验显式失败，不能默认审批或默认放行。

- [ ] **Step 6: 运行领域 focused tests**

```powershell
cd backend
go test ./internal/agentruntime -count=1
```

预期：GREEN。

- [ ] **Step 7: 里程碑 1A Review 与提交**

Review 重点：审批只由结构化风险和冻结模式决定；steer 不改写原始消息；interrupt 不碰已提交资金/资产事实；Runtime/Policy 版本单一来源；每个事件 sequence 最多对应一个 Item 变化。

提交信息：

```text
feat(agent): define conversation control semantics
```

---

## Milestone 1B — 持久事实与 Artifact 时间线

### Task 2: 新增 Item 投影模型与精确数据库约束

**Files:**

- Modify: `backend/internal/model/agent_runtime.go`
- Modify: `backend/internal/database/schema.go`
- Modify: `backend/internal/database/agent_runtime_schema.go`
- Modify: `backend/internal/database/agent_runtime_schema_test.go`

- [ ] **Step 1: 用迁移测试定义表与索引**

先在 `agent_runtime_schema_test.go` 增加 SQLite 精确定义测试：

- `agent_timeline_items` 表存在；
- `(run_id, ordinal)` 唯一；
- `(run_id, source_event_sequence)` 唯一；
- `(thread_id, created_at, id)` 查询索引存在；
- 名称相同但定义错误的索引必须使迁移失败，不能静默信任；
- 重跑迁移幂等；
- 不修改旧 Event、Task、BillingOrder、Resource、Artifact 行。

运行：

```powershell
cd backend
go test ./internal/database -run 'TestEnsureAgentRuntimeIntegritySchema.*Timeline' -count=1
```

预期：RED；模型和索引尚不存在。

- [ ] **Step 2: 定义严格枚举与模型**

在 `model/agent_runtime.go` 定义 `AgentTimelineItemKind`、`AgentTimelineItemStatus` 及 `Valid()`；模型字段必须与批准规格一致：

```go
type AgentTimelineItem struct {
    ID                  string
    TenantKind          agentruntime.TenantKind
    TenantID            string
    ThreadID            string
    RunID               string
    Kind                 AgentTimelineItemKind
    Status               AgentTimelineItemStatus
    Ordinal              int64
    SourceEventSequence  int64
    ContentJSON          string
    StartedAt            time.Time
    CompletedAt          *time.Time
    CreatedAt            time.Time
    UpdatedAt            time.Time
}
```

不存短期 OSS 签名地址；Artifact 内容只存 `resourceId` 和媒体元数据。

- [ ] **Step 3: 注册模型并建立精确约束**

在 `schema.go` 的唯一模型清单加入 `AgentTimelineItem`；在 `agent_runtime_schema.go` 沿用现有“验证定义后创建”的模式建立索引与历史冲突检测。不要依赖 GORM 自动生成的不可审计索引名。

兼容性栅栏必须同时比较 `tool_schema_version`、`runtime_version`、`policy_version`。只有完全 pristine、没有模型/媒体/账单外部事实的旧 queued Run 才能由既有迁移事务明确终结；其他旧活动 Run 让启动失败并列出结构化身份，禁止隐式接管。历史终态 Run 不改写，只允许后续只读投影。

- [ ] **Step 4: 运行迁移 focused tests**

```powershell
cd backend
go test ./internal/database -run 'AgentRuntime' -count=1
```

预期：GREEN。

### Task 3: 在同一事务维护 Event、Checkpoint 与 Item

**Files:**

- Modify: `backend/internal/repository/agent_runtime_execution.go`
- Modify: `backend/internal/repository/agent_runtime_production.go`
- Modify: `backend/internal/repository/agent_runtime_execution_test.go`
- Modify: `backend/internal/repository/agent_runtime_production_test.go`

- [ ] **Step 1: 写 Item 原子性失败测试**

覆盖：

- 初始化 Run 时，独立的 `run.created`、`user.message`、Checkpoint 与 `user_message completed` Item 同事务成功；
- Runtime transition 产生的 item lifecycle 与对应事件 sequence 一致；
- 人为让 Item 唯一索引失败时，Run、Event、Checkpoint、ToolCall 全部回滚；
- completed/failed/declined/interrupted Item 不能重新进入 `in_progress`；
- contentJSON 非法、kind/status 非法、ordinal 跳号全部显式失败。
- Artifact 从 queued/running 转为 succeeded 时，在同一事务追加 `artifact.available` 与 `artifact completed` Item；即使 Run 已 interrupted，也保留原 Task、BillingOrder、Resource、Artifact 并追加 Item，不改回 Run 状态。

运行：

```powershell
cd backend
go test ./internal/repository -run 'Test.*Agent.*Timeline' -count=1
```

预期：RED。

- [ ] **Step 2: 增加纯 Item 变更计划**

Repository 不做语义判断。由领域 EventKind 和冻结 State 生成确定性的 `TimelineMutation`：

```go
type TimelineMutation struct {
    ItemID              string
    Kind                model.AgentTimelineItemKind
    FromStatus          *model.AgentTimelineItemStatus
    ToStatus            model.AgentTimelineItemStatus
    SourceEventSequence int64
    ContentJSON         json.RawMessage
}
```

Item ID、ordinal 和 sequence 必须稳定可重放；禁止随机生成导致恢复重复。身份规则固定为：Run 的初始输入和每个 steer 分别是 `user_message`；最终回复是 `agent_message`；每个状态事件是独立 `status`；澄清按 `requestId`；工具与审批按 `toolCallId + actionVersion`；Artifact 按不可变 `artifactId`。不得用正文或关键词推断身份。

- [ ] **Step 3: 扩展初始化与 transition 事务**

把 Item 写入放进 `InitializeAgentRun` 和 `CommitAgentRuntimeTransition` 已有事务。先锁 Run/Item，再用 CAS 更新；任何 Item 冲突统一返回 `ErrAgentTimelineConflict`，Service 再映射稳定错误码 `agent_item_state_conflict`。

- [ ] **Step 4: 增加 steer 幂等与 interrupt CAS Repository 方法**

实现：

```go
AppendAgentSteer(scope, request, now) (state RuntimeState, replayed bool, err error)
InterruptAgentRun(scope, expectedStateVersion, now) (state RuntimeState, err error)
```

两者必须复用 Event/Checkpoint/Item 同事务写入。相同 steer 重放不新增 sequence；不同正文复用身份返回冲突。两个 interrupt 并发请求只有一个 CAS 成功。

- [ ] **Step 5: 把成功 Artifact 事实接入同一时间线事务**

扩展 `TransitionAgentProductionArtifact`：仅在状态首次进入 `succeeded` 时，锁定对应 Plan/Run，用独立事实追加路径递增 `AgentRun.LastEventSequence`，写入 `artifact.available` 和 `artifact completed` Item。该路径不得修改 terminal Run 的 status/stateVersion/checkpoint，不得重新唤醒 Agent，也不得覆盖已生成 Artifact。重复供应商回调必须命中同一 Artifact/Item 并成为幂等重放。

- [ ] **Step 6: 验证并发、回滚与竞态**

```powershell
cd backend
go test ./internal/repository -run 'Agent(Runtime|Timeline|Steer|Interrupt|ProductionArtifact)' -count=1
go test -race ./internal/repository -run 'Agent.*(Steer|Interrupt|Timeline|Artifact)' -count=1
```

预期：GREEN；race 无报告。

- [ ] **Step 7: 里程碑 1B Review**

对照规格第 4、6、8、10 节检查：没有双真源、没有修改资金/Artifact 事实、没有静默迁移旧活动 Run、没有依赖 prompt 正文。执行一次独立 review，然后只允许一次集中修复和一次定向复审。

- [ ] **Step 8: 提交 1B**

提交前：

```powershell
git status --short
git diff -- backend/internal/agentruntime backend/internal/model/agent_runtime.go backend/internal/database/agent_runtime_schema.go backend/internal/repository/agent_runtime_execution.go backend/internal/repository/agent_runtime_production.go
git diff --check
```

只暂存本里程碑文件，不能带入用户现有计费、Provider、首页和视觉改动。

提交信息：

```text
feat(agent): persist conversation timeline facts
```

---

## Milestone 1C — 版本化事件、历史与控制 API

### Task 4: 用纯投影替换 RuntimeState 直出事件

**Files:**

- Modify: `backend/internal/repository/agent_runtime_history.go`
- Add: `backend/internal/repository/agent_runtime_history_test.go`
- Modify: `backend/internal/service/agent_runtime_transport.go`
- Modify: `backend/internal/service/agent_runtime_test.go`

- [ ] **Step 1: 写版本化投影失败测试**

测试 `ProjectAgentEvent(event, item, protocolVersion)`；其中 `item` 只能来自同一 scoped Run 的持久投影：

- 每个 UI 事件都有 `protocolVersion/threadId/runId/sequence`；
- Item 事件必须有 `itemId`；
- 支持首期 run/item/approval/state 事件；
- 未知领域事件、非法 payload、找不到 Item、未知协议版本返回 `agent_event_projection_failed`；
- `model.delta` 只有 payload 明确是已验证的用户可见文本 delta 才可映射，结构化模型 JSON 不得外泄；
- Artifact 事件只含 `resourceId` 与元数据，不含签名 URL；
- 投影函数不写数据库、不触发账单/任务/画布副作用。

- [ ] **Step 2: 定义唯一 `AgentUIEvent` DTO**

```go
type AgentUIEvent struct {
    ProtocolVersion int             `json:"protocolVersion"`
    ThreadID        string          `json:"threadId"`
    RunID           string          `json:"runId"`
    Sequence        int64           `json:"sequence"`
    Kind            AgentUIEventKind `json:"kind"`
    ItemID          string          `json:"itemId,omitempty"`
    Payload         json.RawMessage `json:"payload"`
    CreatedAt       time.Time       `json:"createdAt"`
}
```

首版协议常量固定为 `1`。不要同时返回旧 `AgentRuntimeEventView`。

Repository 新增批量读取 `events + 当前关联 items` 的作用域方法；查询必须以同一个 scoped Run 为边界，并校验每个 Item 的 `tenant/thread/run/sourceEventSequence`。Service 不允许绕过 Repository 直接查表，也不能从浏览器 payload 接受 itemId。

- [ ] **Step 3: 严格验证 SSE 游标**

读取前验证 `0 <= afterSequence <= run.LastEventSequence`。超过末序、跨 Run、负数分别返回 `agent_stream_cursor_invalid`；不能当成空结果。

- [ ] **Step 4: 运行 Service focused tests**

```powershell
cd backend
go test ./internal/service -run 'TestAgent(Runtime|UIEvent|StreamCursor)' -count=1
```

预期：GREEN。

### Task 5: Thread 历史返回完整 Turn/Item 时间线

**Files:**

- Modify: `backend/internal/repository/agent_runtime_history.go`
- Modify: `backend/internal/repository/agent_runtime_execution_test.go`
- Modify: `backend/internal/service/agent_runtime_history.go`
- Modify: `backend/internal/service/agent_runtime_test.go`

- [ ] **Step 1: 写作用域与顺序失败测试**

历史必须按 Thread 返回多个 Turn；每个 Turn 内 Item 按 `ordinal` 升序；只读旧终态 Run 可从事件重建缺失投影；旧非终态 Run 不被新 Runtime 接管；跨用户/团队/项目/画布读不到任何 Item。

- [ ] **Step 2: 扩展 Repository 查询 DTO**

避免 N+1：先选中最多 20 个 scoped threads，再批量读取这些 thread 的 runs/checkpoints/items，内存仅做 ID 分组和顺序校验。禁止每个 thread 单独查询。

- [ ] **Step 3: 硬切 History JSON**

返回结构固定为：

```json
{
  "items": [{
    "thread": {"id":"...","canvasId":"...","status":"active"},
    "activityAt":"...",
    "turns": [{"run": {"id":"...","status":"..."}, "items": []}]
  }]
}
```

不再返回仅有 `latestRun` 的旧形态；前后端在同一版本硬切。

- [ ] **Step 4: 运行历史 focused tests**

```powershell
cd backend
go test ./internal/repository ./internal/service -run 'Agent.*History' -count=1
```

预期：GREEN。

### Task 6: 增加 steer/interrupt 严格 HTTP 契约并升级 SSE

**Files:**

- Modify: `backend/internal/handler/agent_runtime.go`
- Modify: `backend/internal/handler/agent_runtime_test.go`
- Modify: `backend/internal/service/agent_runtime_transport.go`

- [ ] **Step 1: 写 HTTP 失败测试**

覆盖：

- `POST /api/agent/runs/:runId/steer` 只接受 `clientRequestId/message/expectedStateVersion`；
- `POST /api/agent/runs/:runId/interrupt` 只接受 `expectedStateVersion`；
- unknown field、超限正文、错误版本、终态 Run、跨租户/跨画布均显式失败；
- steer 幂等重放响应同一事实，不重复 sequence；
- interrupt 并发只有一方成功；
- SSE `event:` 使用 UI kind，`data:` 是完整 `AgentUIEvent`；
- 重连按 sequence 无丢失、无乱序；重复读取不触发副作用；
- terminal SSE 发送遗漏事件后关闭。

- [ ] **Step 2: 增加 Service 错误类型**

稳定错误码：

```text
agent_event_projection_failed
agent_item_state_conflict
agent_stream_cursor_invalid
agent_steer_conflict
agent_interrupt_conflict
```

错误响应可以携带 `latestStateVersion`，但不能泄露其他租户的 run/item 身份。

- [ ] **Step 3: 注册新路由并严格解码**

扩展 `agentRuntimeRequest` 泛型联合；沿用 256 KiB 总请求上限，并为 steer 正文增加 64 KiB 结构边界。Handler 不直接操作 Repository。

- [ ] **Step 4: 运行 Handler focused tests**

```powershell
cd backend
go test ./internal/handler -run 'TestAgentRuntimeHTTP' -count=1
```

预期：GREEN。

- [ ] **Step 5: 里程碑 1C Review 与提交**

Review 重点：协议只有一条路径；旧事件形态未保留；SSE 游标来自持久 sequence；所有操作重新鉴权；投影失败不吞错。

提交信息：

```text
feat(agent): expose versioned conversation events
```

---

## Milestone 1D — Web 严格契约、恢复与控制

### Task 7: Web API 客户端硬切到 Turn/Item 与版本化事件

**Files:**

- Modify: `web/src/services/api/agent-runtime.ts`
- Modify: `web/test/agent-runtime-api.test.ts`

- [ ] **Step 1: 写 parser 失败测试**

覆盖：

- 接受 `protocolVersion: 1` 的已知 UI event 和全部首期 Item kind/status；
- 未知 protocol、event kind、item kind/status、缺 itemId、sequence 非正整数、thread/run 归属冲突显式 throw；
- History 必须含 `turns[].items[]`，不接受旧 `latestRun`；
- Artifact item 不接受 `url`/`signedUrl`，只接受 `resourceId` 和媒体元数据；
- 同 sequence 重放由消费层丢弃，parser 不修改数据。

运行：

```powershell
cd web
bun test test/agent-runtime-api.test.ts
```

预期：RED。

- [ ] **Step 2: 定义严格 TypeScript 联合类型**

以 discriminated union 表达 event/item payload，不使用 `Record<string, any>` 或宽松断言。`AgentRuntimeClient` 增加：

```ts
steer(runId, { clientRequestId, message, expectedStateVersion })
interrupt(runId, { expectedStateVersion })
```

移除旧整份 RuntimeState 事件 parser，不保留 fallback。

- [ ] **Step 3: 更新 SSE 订阅 parser**

EventSource 收到未知协议立即 `onError(Error)` 并关闭当前订阅；不能忽略后继续显示可能错误的状态。

- [ ] **Step 4: 运行 API contract tests**

```powershell
cd web
bun test test/agent-runtime-api.test.ts
```

预期：GREEN。

### Task 8: Hook 以服务端时间线为 owner，并暴露追加/停止动作

**Files:**

- Modify: `web/src/components/canvas/use-agent-runtime.ts`
- Modify: `web/test/use-agent-runtime-clarification.test.tsx`
- Add: `web/test/use-agent-runtime-timeline.test.tsx`

- [ ] **Step 1: 写恢复与重复事件失败测试**

测试：

- 打开历史 thread 后恢复完整 turns/items，而不是仅 latestRun；
- SSE 从最后 sequence 补发后按 itemId 更新，不重复追加；
- duplicate/out-of-order sequence 不触发 `onRuntimeEvent` 和画布副作用；
- active run 再次发送走 `steer`，terminal/no-run 才走 `startRun`；
- stop 使用当前 `stateVersion` 调 `interrupt`；409 后刷新真实 Run/History 并展示明确冲突；
- interrupt 后重新读取 History 能看到迟到 artifact item 并可预览，不把 Run 重新标记 running；
- 切换 thread 清空旧订阅，不能串线；
- 未知协议错误保持可见，不能自动清空或重建默认会话。

- [ ] **Step 2: 替换本地 `events.slice(-30)` 所有权**

Hook 维护规范化的 `turns/items + lastSequence`；Item 更新使用 `(runId,itemId)` 主键。LocalForage 只保存 thread/run/cursor 恢复句柄，不保存业务 Item 真源。

- [ ] **Step 3: 暴露最小控制接口**

返回 `sendOrSteer(message, configuration)` 与 `interrupt()`；现有 UI 可在后续视觉阶段接按钮。本阶段不重做 Agent 面板 CSS，不引入 assistant-ui。

- [ ] **Step 4: 运行 Hook focused tests**

```powershell
cd web
bun test test/agent-runtime-api.test.ts test/use-agent-runtime-clarification.test.tsx test/use-agent-runtime-timeline.test.tsx
```

预期：GREEN。

### Task 9: 同步架构文档并完成最终门禁

**Files:**

- Modify: `README.md`
- Verify: `docs/superpowers/specs/2026-08-22-agent-conversation-foundation-design.md`

- [ ] **Step 1: 更新 README 的当前事实**

在“单一 Agent Runtime”章节写明：

- Thread/Turn/Item 与 Event/Checkpoint 的真源关系；
- guided/automatic 审批矩阵；
- steer 安全边界和 interrupt 语义；
- versioned UI event + sequence SSE 恢复；
- Browser 只提交用户动作；
- 本阶段没有 provider 真实文本 delta、assistant-ui、ADK-Go、记忆压缩和外部 Skill importer。

- [ ] **Step 2: 后端全量门禁**

```powershell
cd backend
go test ./...
go vet ./...
go build ./cmd/server
```

预期：全部退出码 0。

- [ ] **Step 3: PostgreSQL 事务/CAS 门禁**

使用仓库既有 PostgreSQL 测试环境变量和测试入口，只运行已有可重复测试数据库，不修改生产数据库：

```powershell
cd backend
go test ./internal/repository ./internal/database -run 'Agent.*(Timeline|Steer|Interrupt|Integrity)' -count=1
```

若测试环境未配置，明确报告门禁未执行，不能以 SQLite 结果替代 PostgreSQL 通过结论。

- [ ] **Step 4: Web 全量门禁**

```powershell
cd web
bun test
bun run build
```

预期：全部退出码 0，bundle budget 通过。

- [ ] **Step 5: 最终需求审查**

逐项对照批准规格第 10、11、13 节和实际 diff，核验：

- Item/Event/Checkpoint 原子性；
- 审批矩阵；
- steer/interrupt 幂等/CAS；
- 多租户隔离；
- SSE 补发；
- 中断后迟到资产保留；
- 旧终态只读、旧活动不接管；
- 前端未知协议显式失败；
- README 同步；
- 没有新增第三方 Runtime、兼容分支或静默 fallback。

发现当前 diff 缺陷只允许一次集中修复和一次定向复审；新增范围或既有债务单独报告。

- [ ] **Step 6: 检查工作区并提交 1D**

```powershell
git status --short
git diff --stat
git diff --check
```

只暂存本计划列出的 Agent/README 文件；不得带入工作区现有首页、计费、Provider、模型定价或视觉修改。

提交信息：

```text
feat(agent): adopt durable conversation timeline
```

## 明确暂缓项

本计划不实现：Provider token/text streaming adapter、assistant-ui、AG-UI SDK 依赖、ADK-Go/Eino、会话摘要与长期记忆、系统进化候选、Codex Skill importer/Capability Manifest、Agent 面板视觉重做、计费计算、供应商适配或媒体生成逻辑。它们必须在本阶段协议事实稳定后分别形成规格与实施计划。
