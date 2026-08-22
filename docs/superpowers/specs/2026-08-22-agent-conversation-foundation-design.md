# HMaigc Agent 对话运行时基础设计

**日期：** 2026-08-22

**状态：** 待用户书面确认

**适用阶段：** Agent 改造第一阶段——对话协议、审批语义与可恢复时间线基础

## 1. 目标

将现有“任务日志式 Agent”改造成可持续多轮协作的 Agent 基础，使后续前端能够呈现接近 Codex 的工作方式：用户在同一会话中持续对话，Agent 流式展示真实过程，能够询问、规划、调用 Skill 和工具、等待审批、接收追加指令、停止后续执行，并在刷新、换设备或服务重启后恢复。

本阶段不替换 HMaigc 已有商业 Runtime。租户隔离、权限、计费、任务、资源、Artifact Ledger、画布 revision/CAS 和交付验收继续由现有 Go 服务与数据库负责。

## 2. 已确认的产品决策

### 2.1 执行模式

系统保留两种显式模式，模式是每个 Run 冻结的事实：

- `guided`（手动模式）：读取上下文和加载 Skill 不需要审批；任何画布写入、计划提交、媒体生成或其他外部副作用都必须先获得用户批准。
- `automatic`（自动模式）：Agent 可以自动完成无费用的规划、画布创建和画布调整；每个会产生费用的图片、视频、音频或其他付费 Artifact 仍必须逐项展示冻结报价并获得用户批准。

模式只决定审批策略，不改变模型能力、工具清单、账务规则或交付合同。模式切换只对下一条 Run 生效，不能修改正在运行或等待审批的 Run。

### 2.2 运行中追加与停止

- 用户在 Run 活动期间发送新消息时，消息先作为持久化 `steer` Item 写入当前 Turn，在下一个安全边界注入 Agent 上下文。
- 安全边界是模型步骤结束、工具结果已持久化或审批完成之后；不能在数据库事务、计费预留、供应商提交或画布 CAS 中途改变参数。
- “停止”表示停止 Agent 创建后续模型步骤、工具调用和付费任务。
- 已经成功提交的付费任务不回滚、不删除。其回调、资产、账单和日志继续持久化；Run 标记为 `interrupted` 后，这些迟到结果作为审计事实和可复用资产保留。

### 2.3 上下文与记忆

上下文分层保存，禁止把全部历史无条件塞入模型：

1. **会话上下文**：Thread、Turn、Item、消息、工具调用、审批、错误和交付结果。
2. **运行上下文**：Checkpoint、执行模式、模型、Skill 版本、待处理工具、任务、账单和交付合同。
3. **项目上下文**：当前租户、项目、画布、剧本、角色、场景、服装、道具、镜头计划和真实资产，按需读取。
4. **个人偏好记忆**：只在当前租户与用户作用域内生效，并保留来源 Item。
5. **系统进化候选**：从用户反馈中形成的 Skill、提示词、策略或工具变更建议。候选本身不改变生产行为，只有管理员审核、发布并冻结版本后才生效。

长会话压缩必须保留结构化事实引用、用户决策、审批、工具结果、资产和失败原因。摘要不是业务真源；任何需要执行的动作都必须重新读取当前项目、资产、权限、报价和画布 revision。

### 2.4 Skills 兼容

HMaigc 兼容标准 Codex Skill 目录及 `SKILL.md`，但将“格式可导入”和“运行可启用”分开：

- 标准 Skill 可以导入并解析名称、说明、触发条件、指令和相对引用资源。
- 外部 Skill 导入后默认为 `pending_review`。
- Skill 只有在来源、许可证、版本、SHA-256、资源边界和工具依赖全部明确后，管理员才可发布。
- Codex Skill 未声明工具依赖时，管理员必须建立显式 `SkillCapabilityManifest`；系统不得通过正则或关键词猜测依赖，也不得静默忽略未知工具。
- 发布后的 Skill 版本不可变；Run 冻结目录、版本、校验值和完整指令。恢复时找不到完全一致的版本必须显式失败。
- Agent 只能通过 `skill.load` 按需读取已冻结 Skill。Skill 不拥有权限、计费或画布写入能力；它只能指导 Agent 选择 HMaigc 已发布的版本化工具。

## 3. 架构选择

### 3.1 推荐方案

采用分层组合，而不是引入第二套业务 Runtime：

```text
React Agent Panel
  <- HMaigc Timeline API / persisted SSE
  <- AG-UI-compatible event projection
                       |
Go Agent Application Runtime（唯一商业真源）
  |- Thread / Turn / Item / Checkpoint
  |- approval policy / steer / interrupt
  |- expectedDelivery -> evidence -> verification
  |- tenant / billing / task / artifact / canvas transaction
                       |
AgentEngine Port
  |- 当前模型决策引擎（第一阶段）
  `- ADK-Go 隔离 PoC（后续阶段，验收后才可能硬切）
```

AG-UI 只作为对外事件语义，不成为持久化或业务状态真源。assistant-ui 只允许通过 ExternalStore 思路消费现有状态；不采用 Assistant Cloud，不允许前端工具直接修改画布、创建生成任务或扣除积分。

### 3.2 未采用的方案

- 不直接托管 Codex app-server 作为每个用户的云端 Runtime。其 shell、文件系统和本地 session 所有权与影视商业事实不同。
- 不引入 LangGraph/Python 或 Vercel AI SDK/Node 作为平行 Runtime，避免双 checkpoint、双工具 schema、双计费 usage 和双审批状态。
- 不在第一阶段接入 ADK-Go。先稳定 HMaigc 自身的会话、Item 和事件契约，再用同题故障测试评估是否只替换模型循环。

## 4. 领域模型

### 4.1 保留的权威实体

- `AgentThread`：租户、用户、项目和画布作用域内的长期会话。
- `AgentRun`：一次用户目标的执行事实，对应对话协议中的 Turn。
- `AgentRunEvent`：按 `runId + sequence` 排序的追加式事件日志。
- `AgentCheckpoint`：可恢复执行状态。
- `AgentToolCall`、`AgentProductionPlanVersion`、`AgentProductionArtifact`、Task、BillingOrder、Resource：工具、生产、资产和资金真源。

### 4.2 新增的时间线投影实体

新增 `AgentTimelineItem` 作为可查询投影，不建立第二真源：

```text
AgentTimelineItem
  id
  tenantId / threadId / runId
  kind
  status
  ordinal
  sourceEventSequence
  contentJSON
  startedAt / completedAt
  createdAt / updatedAt
```

`kind` 首期只允许：

- `user_message`
- `agent_message`
- `status`
- `clarification`
- `tool_call`
- `tool_result`
- `approval`
- `artifact`
- `error`

`status` 首期只允许 `in_progress`、`completed`、`failed`、`declined`、`interrupted`。

每次 Runtime 写入 Item 变化时，必须在同一数据库事务中追加 `AgentRunEvent` 并更新 Item 投影。`AgentRunEvent` 是审计和重建依据；Item 只用于高效查询。投影损坏时只能从事件重建，不能反向覆盖事件、账单、资产或画布。

### 4.3 Item 生命周期

每个 Item 遵循单一路径：

```text
item.started
  -> zero or more item.delta
  -> item.completed | item.failed | item.declined | item.interrupted
```

终态不可逆。媒体任务在 Run 中断后完成时，不修改已终结 Item；系统追加新的 `artifact` Item 记录迟到资产事实。

## 5. 事件契约

### 5.1 内部事件与 UI 事件

数据库继续保存 HMaigc 领域事件。新增纯函数 `ProjectAgentEvent` 把领域事件映射为版本化 UI 事件：

```text
HMaigc AgentRunEvent
  -> ProjectAgentEvent(event, protocolVersion)
  -> AgentUIEvent
  -> SSE
```

首期 UI 事件覆盖：

- `run.started`
- `run.completed`
- `run.failed`
- `run.interrupted`
- `item.started`
- `item.delta`
- `item.completed`
- `item.failed`
- `approval.requested`
- `approval.resolved`
- `state.snapshot`

所有事件必须包含 `threadId`、`runId`、`sequence` 和 `protocolVersion`；Item 事件还必须包含 `itemId`。不得为 AG-UI 再生成平行 thread/run/tool identity。

### 5.2 流式真实性

本阶段禁止伪造逐字进度：

- 工具、审批、任务和资产状态来自已经持久化的真实事件，可以实时推送。
- 当前结构化 JSON 决策模型在完成严格解析前，不把原始 JSON 片段显示为 Agent 文本。
- `item.delta` 只有在供应商返回可验证的用户可见文本增量时才产生。
- 第二阶段将评估 provider streaming adapter 与最终回复流式契约；未支持真实文本 delta 的模型明确显示“正在处理”，但不能显示伪造百分比、伪造阶段或模板内容。

### 5.3 断线恢复

- SSE 继续使用服务端持久 `sequence` 作为唯一游标。
- 客户端重连提交 `afterSequence`；服务端先发送遗漏事件，再进入实时 tail。
- 如果客户端游标不存在、来自其他 Run 或超过服务端末序号，返回显式协议错误。
- UI 可请求 `state.snapshot` 加后续 delta 重建时间线；snapshot 只能投影已有事实。

## 6. 审批策略

工具风险保持三个结构级别：

- `L0 Read`：读取项目/画布事实、读取目录、加载已发布 Skill。
- `L1 Write`：计划提交、画布变更、免费确定性写操作。
- `L2 Cost`：任何可能创建 BillingOrder、预留积分或向供应商提交付费任务的动作。

审批矩阵：

| 模式 | L0 | L1 | L2 |
|---|---|---|---|
| `guided` | 自动执行 | 必须审批 | 必须审批 |
| `automatic` | 自动执行 | 自动执行 | 必须审批 |

审批请求必须冻结工具名、版本、参数摘要、作用域、模型、媒体参数、报价、计费单位、idempotency key、过期时间和预期交付。批准后任何冻结字段变化都必须创建新审批，禁止复用旧决定。

## 7. API 边界

保留现有 Thread/Run URL 所有权，在同一版本中硬切到新协议，不长期维护旧新双轨。第一阶段新增或扩展：

- Thread 历史返回按 Turn 分组的 Item 时间线。
- Run 事件接口返回版本化 UI 事件并继续支持 `afterSequence`。
- `POST /runs/:runId/steer`：持久化追加指令，使用 `clientRequestId` 幂等。
- `POST /runs/:runId/interrupt`：以 `expectedStateVersion` CAS 请求停止后续执行。

浏览器只能提交用户消息、审批决定、澄清答案、steer 和 interrupt。工具结果、任务终态、账单、资源和画布 revision 只能由服务端执行器写入。

## 8. 升级与历史数据

- 数据库迁移只允许增加 Item 投影和必要索引，不修改历史资金、Artifact、Task、Resource 或画布事实。
- 新版本发布前必须停止创建旧 Runtime 的新 Run，并等待活跃 Run 到达安全边界。
- 可以安全恢复且版本完全匹配的 Run 继续执行；其余非终态旧 Run 以明确 `runtime_schema_retired` 终结，不做隐式迁移。
- 历史终态 Run 继续只读展示；首次读取时可以从事件构造 Item 投影，但构造失败必须显式记录，不影响原审计事实。
- 后端迁移与健康检查通过后才启动新 Web；回滚只能回到能够读取新增结构的兼容后端镜像，禁止数据库降级或删除新表。

## 9. 错误处理与可观测性

关键错误必须具有稳定错误码、关联身份和结构化日志：

- `agent_event_projection_failed`
- `agent_item_state_conflict`
- `agent_stream_cursor_invalid`
- `agent_steer_conflict`
- `agent_interrupt_conflict`
- `skill_dependency_unresolved`
- `skill_version_unavailable`

日志至少包含 tenant、thread、run、item、event sequence、state version 和 tool call identity；不得包含 API Key、完整提示词、用户私密资产内容或供应商凭据。

## 10. 测试与验收

第一阶段必须覆盖：

1. Item started/delta/completed 与事件在同一事务内，失败时全部回滚。
2. 终态 Item 不能重新进入运行态。
3. 相同 clientRequestId 的 steer 只写一次；不同正文复用同一身份显式冲突。
4. interrupt CAS 竞争只有一个请求改变状态。
5. guided 的 L1/L2 等待审批；automatic 的 L1 自动执行、L2 等待审批。
6. SSE 断线后按 sequence 补发，无丢失、无乱序；重复事件不触发重复副作用。
7. 用户、租户、项目和画布之间的 Thread、Item、审批与事件不可越权查询。
8. Run 中断后迟到媒体成功仍保留 Task、BillingOrder、Resource 和 Artifact，并追加迟到资产 Item。
9. 旧终态 Run 能只读投影；不完整旧 Run 不被静默接管。
10. 前端未知事件、未知 Item kind 或协议版本不匹配时显式失败，不猜测展示。

门禁包括 focused Go tests、相关 React contract tests、Go 全量测试、`go vet`、Go build、Web 全量测试、Web build、`git diff --check`，以及 PostgreSQL 下的事务/CAS/多租户测试。

## 11. 分阶段交付

### 第一阶段：本规格范围

- Item 投影与生命周期。
- 版本化 UI 事件投影。
- 审批矩阵修正。
- steer/interrupt 服务端契约。
- 历史与 SSE 恢复契约。
- 必需的 README 架构同步。

### 第二阶段：真实流式对话与 React 表达层

- Provider streaming adapter。
- 用户可见 agent message delta。
- 以现有 store 为 owner 的 assistant-ui ExternalStore PoC。
- Codex 风格消息、工具、审批、附件、重试与停止交互。

### 第三阶段：分层记忆与上下文压缩

- 会话压缩、项目按需读取、个人偏好记忆。
- 进化候选、来源追溯和管理员发布。

### 第四阶段：Codex Skills 导入与治理

- Skill package importer、引用资源校验、Capability Manifest、管理员审核、冻结发布和回滚。

### 第五阶段：AgentEngine PoC

- 使用相同工具端口和故障矩阵比较现有模型循环与 ADK-Go。
- 只有 ADK-Go 明显降低复杂度且不建立双真源时，才另行设计硬切方案。

每个阶段单独形成规格、实施计划、测试证据和可回滚提交，禁止在第一阶段提前引入后续依赖。

## 12. 第一阶段变更预算

- 最多 3 个生产职责：Item/事件协议、审批/控制语义、API/前端契约。
- 生产文件目标不超过 8 个；测试与必需 README 不计入生产文件预算。
- 净新增生产代码目标约 800 行；超过 1,200 行或出现超过 500 行的新生产文件必须停止并重新拆分。
- 暂不接入新第三方依赖、ADK-Go、assistant-ui 或数据库外部服务。
- 暂不修改计费计算、供应商适配、Artifact 生成逻辑或画布节点视觉。

## 13. 完成定义

第一阶段完成不等于“Agent 已经拥有完整 Codex UI”。完成条件是：现有商业 Runtime 能以 Thread/Turn/Item 结构提供可恢复、可审批、可追加、可中断的唯一事件事实；手动与自动模式符合审批矩阵；刷新和升级不丢失真实状态；后续真实文本 streaming、记忆、Skills 和 AgentEngine 可以在不重写商业内核的前提下逐阶段接入。
