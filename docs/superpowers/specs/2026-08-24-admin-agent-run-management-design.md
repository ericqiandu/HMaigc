# HMaigc 管理员 Agent 运行管理与升级阻塞治理设计

**日期：** 2026-08-24

**状态：** 用户已确认架构，待书面规格复核

**关联规格：**

- `2026-08-23-agent-native-streaming-chat-design.md`
- `2026-08-22-agent-conversation-foundation-design.md`

## 1. 业务目标

为系统管理员提供一个长期可用的 Agent 运行管理入口，使管理员能够查看所有租户中尚未结束的 Agent 运行、判断它们是真正执行中、正常等待用户，还是已经失去进展，并通过受权限、CAS、计费和审计约束的单一路径终止指定运行。

同时修复从 v1.0.50 升级到新运行时时的启动阻塞：旧版本没有用户中断接口，历史 `waiting_approval` 运行无法通过界面结束，导致新后端因契约不兼容拒绝启动。新版本必须只对已经证明没有外部执行与计费事实的旧暂停运行进行可审计退休；任何正在调用供应商、运行媒体任务或存在未决账务的运行仍应阻止升级。

本设计不删除 Agent 历史，不把“终止”实现为直接修改一列状态，也不允许管理员操作绕过 Artifact Ledger、Task、BillingOrder、供应商请求或画布 revision/CAS。

## 2. 已确认的产品边界

- 管理员可以查看全部未结束 Agent 运行，而不只查看系统推断为“卡住”的运行。
- 列表必须区分“运行中”“等待询问”“等待确认”“等待工具”和“可能停滞”；等待用户本身不是失败。
- 管理员可以单条终止运行；首版不提供批量终止。
- 管理员必须填写终止原因并输入精确确认短语，服务端必须校验 `expectedStateVersion`。
- 普通用户的既有停止能力继续使用同一个 Agent Runtime 中断领域契约，不创建管理员专用状态机。
- 已开始媒体生成时，系统只能如实表达“已请求取消”；若供应商仍完成生成，已产出资产必须保留并记录，费用按真实账单事实结算或退款。
- 升级引导只自动退休旧契约下无外部副作用的暂停运行；不得自动终止真实生成中或计费未决的运行。
- Ops Controller 只负责编排升级、备份、回滚和展示脚本日志，不直接写业务数据库，也不复制 Agent/计费领域逻辑。

## 3. 方案选择

### 3.1 采用：业务后台管理 + 领域中断 + 安全升级退休

```text
管理后台 /admin/agent-runs
  -> Admin Agent Run API（管理员权限、筛选、分页）
  -> AgentRunControlService（状态分类、终止策略、CAS）
  -> Agent Runtime Interrupt（唯一状态机）
  -> Task cancel request / billing reconciliation（已有商业事实路径）
  -> Agent event + checkpoint + timeline + structured audit

版本升级
  -> Ops Controller 创建恢复点并启动新后端
  -> schema integrity migration
  -> 退休可证明无副作用的旧 paused run
  -> 拒绝仍有外部执行或账务风险的 incompatible run
  -> 新后端健康检查
```

正常运营时，管理员通过业务后台处理运行；跨契约升级时，数据库完整性迁移仅处理能够由持久事实证明安全的暂停运行。两者最终都落到 Agent 的终态事实，不保留“数据库已取消但时间线仍运行”的双轨状态。

### 3.2 不采用：只在管理后台增加一个状态更新按钮

直接把 `agent_runs.status` 改成 `cancelled` 会遗留 checkpoint、pending tool call、时间线 Item、模型 Task、媒体 Task 和 BillingOrder，后续恢复器仍可能继续执行或重复计费。这会破坏事件溯源、幂等与审计，不可接受。

### 3.3 不采用：由 Ops Controller 直接连接 PostgreSQL 清理

Ops Controller 的独立性用于在业务数据库回滚时保留部署审计，不代表它应理解 Agent 状态机和计费事务。让控制器拼 SQL 会形成第二套领域写入路径，并在版本演进后持续扩大维护风险。

### 3.4 不采用：升级时无条件取消所有非终态运行

`running` 和 `waiting_tool` 可能已经提交供应商请求或产生账务事实。无条件取消会制造资产丢失、供应商已收费但平台退款、或运行显示取消但结果继续写入等冲突。升级必须对风险运行显式失败并保留恢复点。

## 4. 管理员运行目录

### 4.1 页面入口

管理后台新增独立路由 `/admin/agent-runs`，导航名称为“Agent 任务”，归入现有运营/运维分组。它不塞入普通用户任务中心，也不把内部模型 Task 暴露给普通用户。

页面遵循根 `Design.md`：使用高密度数据页、单层表格表面、已有后台筛选和确认弹层；加载、空数据和失败是三个独立状态。页面不展示伪进度，也不把读取失败显示成零任务。

### 4.2 列表契约

`GET /admin/agent-runs` 使用数据库分页并支持以下筛选：

- `status`：全部非终态、queued、running、waiting_input、waiting_approval、waiting_tool。
- `activity`：全部、等待用户、可能停滞。
- `user`：用户 ID、邮箱或显示名。
- `scope`：Run ID、项目 ID 或画布 ID。
- `updatedBefore`：服务端验证的 UTC 时间边界。
- `page` 与 `pageSize`：页容量固定允许 20、50、100。

返回的每行至少包含：

```text
runId, threadId, actorUserId, actorDisplayName,
domainProjectId, canvasId,
status, stateVersion, stepNumber, maxSteps,
toolSchemaVersion, runtimeVersion, policyVersion,
pendingKind, pendingToolName,
updatedAt, inactiveSeconds, activityClassification,
linkedModelTaskStatus, linkedMediaTaskStatus,
billingState, providerRequestState,
controlDisposition, controlBlockedReason
```

`activityClassification` 只根据确定性时间和状态事实计算：

- `awaiting_user`：状态为 `waiting_input` 或 `waiting_approval`，无论等待多久都不自动称为故障。
- `possibly_stalled`：状态为 queued、running 或 waiting_tool，连续 10 分钟没有 `updated_at` 变化。
- `active`：其余非终态运行。

该分类只用于筛选和提示，不自动触发终止，也不进行语义判断。`inactiveSeconds` 由服务端以数据库 UTC 时间和 `updated_at` 计算，Web 不使用本地时钟重新判定。

`controlDisposition` 为：

- `interruptible_now`：可以立即形成 Agent 终态。
- `cancel_request_required`：已启动外部 Task，需请求取消并继续账务核对。
- `blocked_by_unresolved_billing`：账务状态不允许终止收口。
- `already_terminal`：读取与操作发生并发变化。

分页、状态过滤和租户范围聚合必须在数据库查询层完成，不得先拉取全部运行再由 Web 过滤。

### 4.3 详情与隐私

详情抽屉展示运行身份、配置摘要、状态版本、时间线状态摘要、关联模型/媒体 Task、BillingOrder、供应商 request ID 是否存在、Artifact 数量及最近结构化错误。默认不展示完整用户 prompt、完整模型回复、密钥或供应商原始响应。

需要诊断正文时继续使用既有受限日志与审计入口；Agent 任务管理页不是新的敏感内容浏览器。

## 5. 管理员终止契约

### 5.1 API

```http
POST /admin/agent-runs/{runId}/interrupt
Content-Type: application/json

{
  "expectedStateVersion": 7,
  "reason": "用户已离开审批页面，任务超过运营窗口未处理",
  "confirmation": "STOP f70d5e85"
}
```

服务端生成并返回本次运行需要的精确确认短语；Web 不自行猜测。`reason` 去除首尾空白后必须为 4–500 个字符。接口只允许管理员调用，并拒绝终态、版本冲突、确认短语错误和不可安全处理的账务状态。

成功响应包含权威运行状态、终止处置类型、受影响的关联任务，以及是否仍有供应商/计费核对待完成。前端不得把 `cancel_request_required` 显示成“已完全停止供应商生成”。

### 5.2 单一状态机

管理员终止必须复用 `agentruntime.Interrupt` 的状态转换语义，并扩展仓库事务以原子完成：

1. 按 Run、Thread、租户、项目和画布作用域锁定当前 Run 与最新 checkpoint。
2. 校验管理员权限、`expectedStateVersion` 和当前非终态状态。
3. 关闭或中断当前 in-progress timeline Item。
4. 将未启动的 pending tool call 记录为 failed，稳定错误码为 `admin_agent_run_interrupted`，并清除 checkpoint 中的 pending 引用；对应 timeline Item 标记为 interrupted。
5. 追加 `run.interrupted` 事件和新 checkpoint，更新 Run 的 `status/stateVersion/lastEventSequence/completedAt`。
6. 在事件 payload 中记录 `source=admin`、管理员用户 ID、规范化原因和原状态；不得记录密钥或完整 prompt。
7. 事务提交后取消仍在本进程运行的模型 context，并按关联 Task 状态发起后续取消动作。

重复提交同一个已成功请求不得制造第二个终态事件。状态版本已变化时返回 409 和最新状态摘要，由页面刷新后让管理员重新确认。

### 5.3 关联 Task 与媒体资产

- 尚未发出供应商请求的内部模型 Task：取消 Task，释放可确定未消费的 Token 预留。
- 已发出文本模型请求：停止本地继续输出；usage 未确定时保留待核对账务，不猜测退款。
- 尚未批准的 `production.render`：不存在媒体 Task/BillingOrder；关闭等待审批的 tool call 和 Artifact 等待状态，不产生费用。
- 已创建但尚未提交供应商的媒体 Task：复用现有 Task 取消和预留释放契约。
- 已提交供应商的媒体 Task：记录取消请求；供应商支持取消时调用真实取消能力，不支持时显式保留运行事实。任何稍后到达的成功资产必须继续写入节点、日志和 Artifact Ledger，不得删除或回滚。
- 已成功或已收费的 BillingOrder：不得因 Agent 终止直接退款；只能由既有账务核对结果决定结算或退款。

管理员页面必须在确认弹层中展示上述实际影响，而不是统一承诺“停止一切上游执行”。

## 6. v1.0.53 升级引导迁移

### 6.1 当前阻塞事实

v1.0.50 没有 Agent interrupt HTTP 接口。云端当前存在两个旧契约 `waiting_approval` 运行，使用 `tool_schema_version=3/runtime_version=1/policy_version=1`；v1.0.52 要求 `3/2/2`，因此新后端在完整性检查时拒绝启动并自动回滚。

v1.0.53 必须包含一次性、事务化、幂等的 `retire-incompatible-paused-agent-runs` 迁移，以便在管理员页面尚未可用前安全完成首次跨越。

### 6.2 可退休条件

只有同时满足以下条件的旧运行可以自动退休：

- 契约版本低于当前版本；高于当前版本的运行一律拒绝。
- 状态为 `waiting_input` 或 `waiting_approval`。
- 最新 Run 与 checkpoint 的状态、stateVersion、stepNumber、maxSteps、lastEventSequence 完全一致。
- 当前 pending tool 尚未开始；不存在 running/waiting_tool 事实。此前已终结为 `succeeded` 或 `failed` 的历史 tool call 即使保留 `startedAt` 也不属于活动执行，迁移必须保留其状态、输出和审计时间。
- 不存在已提交的供应商请求。
- 不存在活动媒体 Task、未决 BillingOrder 或已开始的扣费事实。
- 当前尚未执行的 production Artifact 仅允许 `planned` 或 `awaiting_approval`；`queued` / `running` 表示仍有活动执行风险并阻止退休。
- 已经 `succeeded` / `failed` / `committed` 的历史 Artifact（包括创建计划时立即成功的脚本 Artifact）必须原样保留，不构成活动执行，也不得阻止当前等待项退休。

任何条件不满足时，迁移不得部分处理该运行。审计必须先遍历全部候选 Run 和全部可独立查询的阻塞类别，再一次性返回按 `createdAt/runId/category` 稳定排序的完整摘要；摘要只允许包含 Run ID、状态、契约版本、事实类别、状态值和数量，不得包含 prompt、tool input/output、供应商原始响应、用户正文或密钥。单条 Run 的 checkpoint 已损坏时，仍须继续审计该 Run 可独立确认的 ToolCall、Task、BillingOrder 与 Artifact 事实；只有依赖有效 checkpoint 的检查可以标记为不可判定。

### 6.3 退休写入

迁移在一个数据库事务中为每个合格运行：

- 把状态转为 `cancelled` 并增加 stateVersion。
- 追加确定性的 `run.interrupted` 事件和终态 checkpoint。
- 将活动 timeline Item 标记为 interrupted。
- 将等待审批的 tool call 标记为 failed、对应 timeline Item 标记为 interrupted、等待中的 Artifact 标记为 failed，并统一使用稳定原因 `runtime_contract_retired`。
- 写入 `source=upgrade_migration` 与旧/新契约版本摘要。
- 设置 completedAt，但不改写历史事件、已终结 tool call、计划、资源或 prompt。

迁移批次必须全有或全无；一条候选记录无效时回滚整个批次。重复启动只验证已经完成的迁移，不重复追加事件。

### 6.4 只读预检与迁移复用

退休逻辑必须拆成同一套“只读审计/计划”和“事务写入”两阶段，并覆盖全部契约不兼容的活跃状态（`queued`、`running`、`waiting_input`、`waiting_approval`、`waiting_tool`）：

- 只读阶段读取全部候选 Run、最新 checkpoint、ToolCall、Task、BillingOrder 与 Artifact，构造确定性的退休计划或完整 blocker 集合，不写任何业务事实。
- `queued` 与暂停态 Run 只有在完整事实审计通过后才计为可退休；`running`、`waiting_tool`、未来版本和未知契约必须在同一份报告中列为不可自动退休 blocker。
- 写入阶段只接受已通过全活跃运行审计的退休计划，并在同一数据库事务内执行 CAS、event、checkpoint、timeline、tool call 与 Artifact 收口；不得重新实现第二套风险判断。
- 目标后端镜像必须携带独立的只读审计命令。该命令直接连接现有数据库，只调用共享审计入口，不执行 `MigrateSchema`、AutoMigrate、状态转换、Task 取消、账务变更或供应商请求。
- 审计命令成功时输出候选数、可退休数与零 blocker；存在 blocker 时输出完整、脱敏、稳定排序的 JSON 摘要并返回非零退出码；数据库查询失败与业务 blocker 使用不同错误类别。
- 审计和正式迁移必须覆盖相同的 active status、future/unknown contract、event history、checkpoint、pending/started ToolCall、活动 Task、未决 BillingOrder、Plan/Artifact 状态以及终态 payload 可构造性规则。测试必须证明二者不会漂移。

### 6.5 升级和回滚

升级脚本拉取目标镜像后，必须先在当前服务仍在线时运行一次目标镜像的只读审计；失败时保持当前版本在线并直接返回完整 blocker，不进入停写或备份。首次审计通过后停止 Web/后端写入，再对静止数据库运行第二次同一审计以关闭并发窗口；第二次失败时立即恢复当前版本，不启动目标版本。两次审计均通过后，才创建 PostgreSQL 与后端数据恢复点并启动新后端。

正式迁移仍是最终原子门禁。迁移失败或新后端未通过健康检查时，部署脚本恢复升级前数据库和资源卷，因此不会留下半迁移状态。只读预检不是迁移成功的伪状态，也不得代替恢复点与启动健康检查。

迁移日志只记录数量、Run ID、旧状态、契约版本和结果，不记录用户 prompt。升级成功后，管理员可通过新页面处理未来的非终态运行，不再依赖一次性迁移。

## 7. 权限、隔离与审计

- 列表、详情和中断接口都必须先执行 `RequireAdmin`。
- 仓库层管理员查询可以跨租户读取摘要，但写入仍必须从 Run 解析真实 Thread/租户/项目/画布作用域，不接受客户端传入作用域。
- 普通用户 Agent、Task 和项目接口不增加跨租户能力。
- 成功中断写入不可变 Agent event；管理员 ID、原因、原状态、目标状态和关联 Task 处置结果可追溯。
- API 结构化日志记录 request ID、管理员 ID、Run ID、expected/actual stateVersion、处置类型和错误码，不记录密钥、完整 prompt 或模型正文。
- 管理页面不得通过 URL 查询参数暴露终止原因或敏感运行内容。

## 8. 并发、幂等与错误契约

稳定错误码：

- `admin_agent_run_not_found`
- `admin_agent_run_state_conflict`
- `admin_agent_run_terminal`
- `admin_agent_run_confirmation_invalid`
- `admin_agent_run_interrupt_blocked`
- `admin_agent_run_billing_unresolved`
- `agent_runtime_upgrade_blocked`
- `agent_runtime_retirement_invalid`

所有写入依赖行锁与 `expectedStateVersion`。用户审批、用户停止、模型完成、媒体回调和管理员终止中只有一个状态转换能够提交；失败方必须读取最新事实，不得重试覆盖。

终止 Run 与发起外部 Task 取消无法跨供应商形成单一数据库事务，因此采用事实化两阶段：先原子终止 Agent 并登记关联 Task 的取消意图，再由已有 Task 协调器执行真实取消/核对。取消意图和供应商结果都必须幂等。

## 9. Web 交互

页面顶部提供状态、活动分类、用户和作用域筛选。表格默认显示全部非终态运行并按 `updatedAt ASC, runId ASC` 排序，使最久未变化的运行在前。

每行只保留一个危险操作图标，配有 tooltip、`aria-label` 和键盘焦点。点击后打开统一后台确认弹层，依次展示：

1. 目标用户、画布和 Run ID。
2. 当前状态和最后活动时间。
3. 模型/媒体 Task 与 BillingOrder 影响。
4. 可能无法停止供应商执行的真实限制。
5. 原因输入与精确确认短语。

提交期间弹层不可关闭或重复提交。409 时保留管理员填写的原因，刷新权威状态并要求重新确认；首次加载失败替代表格，保留旧数据的刷新失败则继续展示旧数据并标明来源。

## 10. 数据库影响

- 不新增平行 Agent 状态表。
- 管理员成功终止复用 `agent_runs`、`agent_run_events`、`agent_checkpoints`、`agent_timeline_items`、`agent_tool_calls`、production Artifact、Task 与 BillingOrder。
- 如现有 event payload 长度与索引足够，不新增管理员操作表；成功操作的审计事实由不可变 Agent event 承载。
- 新增一次性 data migration ID，确保升级退休幂等。
- 不删除、不重写历史事件，不做数据库降级脚本。

## 11. 测试矩阵

必须按 RED → GREEN 覆盖：

1. 非管理员无法列表、读取或终止任何 Agent 运行。
2. 管理员分页与状态/用户/作用域/停滞筛选在数据库层生效，总数准确。
3. 详情不返回完整 prompt、回复、密钥或供应商原始 body。
4. waiting_input、waiting_approval、queued 和纯模型 running 的合法终止产生一个终态 event/checkpoint，并关闭活动 Item。
5. 错误确认短语、空原因、终态运行和 stateVersion 冲突均不写入事实。
6. 用户审批、模型完成与管理员终止并发时只有一个转换成功；重复请求不重复取消或退款。
7. 未批准媒体生成被终止时不创建 Task/BillingOrder；等待 Artifact 收口且无费用。
8. 已提交供应商的媒体 Task 只记录真实取消请求；迟到成功资产仍被保留。
9. 预留、usage 不确定、已结算和退款中的 BillingOrder 分别遵循现有账务契约。
10. v1.0.50 形状的 waiting_approval/waiting_input 无副作用 fixture 能被原子退休并允许新 schema 启动；历史 succeeded/failed tool call 与 succeeded/failed/committed Artifact 保持原事实且不阻断当前等待项退休。
11. 两条 Run 同时含多种风险事实时，只读审计一次返回每条 Run 的全部可确认 blocker；checkpoint 损坏不应吞掉同 Run 可独立查询的 Task/BillingOrder/Artifact blocker，输出不含用户正文或供应商原始数据。
12. 只读审计前后数据库事实计数与内容完全不变；部署脚本在在线预检失败时不停止服务，在静止预检失败时恢复当前版本且不启动目标版本。
13. 正式迁移与只读审计共享同一规则；旧 running、waiting_tool、存在供应商请求、媒体 Task、BillingOrder 或非法 checkpoint 时升级明确失败且不部分退休。
14. 迁移重复执行不增加 event/checkpoint；批次中一条非法记录会回滚全部退休。
15. Web 正确展示加载、旧数据刷新、空数据、失败、409 冲突和取消请求待核对状态。

最终门禁：focused Go/Web tests、`go test ./...`、受影响 repository/service 包 race、Go build、Web tests/typecheck/build、`git diff --check`，以及隔离 PostgreSQL 中从 v1.0.50 fixture 升级到目标版本的真实回归。完成后再执行一次本地 Docker 升级、健康检查和管理员页面浏览器流程。

## 12. 实施预算

### 里程碑一：升级引导与领域中断完整性

- 生产职责：安全退休旧暂停运行、管理员中断服务、Task/账务处置事实。
- 生产文件目标：不超过 8 个。
- 净新增生产代码目标：约 450 行。
- 昂贵门禁：PostgreSQL 迁移、并发/race、v1.0.50 fixture 升级。

### 里程碑二：管理员目录与交互

- 生产职责：后台 API、Web 数据页、详情/确认弹层、导航。
- 生产文件目标：不超过 8 个。
- 净新增生产代码目标：约 450 行。
- 昂贵门禁：Web 全量测试、typecheck/build、浏览器真实流程。

若实现发现媒体 Task 取消或账务核对缺少可复用领域契约，应先把该契约补在现有 Task/Billing 模块，再继续；不得为了维持预算在 Agent 管理服务中复制退款逻辑。

## 13. 文档与发布

- 修改 Agent Runtime、interrupt、Task gating 或诊断行为时，同步更新 `backend/README.md` 的当前 Agent 对话架构章节。
- 更新根 `README.md` 的运维升级与管理员能力说明。
- 发布说明必须明确：v1.0.53 会退休符合安全条件的旧暂停 Agent 运行，不删除历史、不产生媒体费用；风险运行仍阻止升级。
- 当前 v1.0.52 不重新标记或覆盖；修复形成新的聚焦版本。

## 14. 完成定义

- 管理员能在后台准确定位所有非终态 Agent 运行，并看见真实状态、停留时间、关联任务和账务风险。
- 管理员终止使用同一 Agent Runtime 状态机、CAS、事件、checkpoint 和审计链路，无直接状态覆盖。
- 普通用户权限与多租户隔离不受影响。
- 媒体任务和费用按真实供应商/账务事实处理，迟到资产不丢失。
- 当前两条旧 `waiting_approval` 运行能够在无媒体/账务副作用的前提下被升级迁移安全退休，目标版本完成健康启动。
- 真正处于外部执行或账务未决的旧运行继续显式阻止升级并给出可定位原因。
- 全量审查、自动化测试、本地升级和浏览器验收都有可复核证据。
