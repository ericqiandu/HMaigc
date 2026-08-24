# HMaigc Agent 原生流式对话与内部任务隔离设计

**日期：** 2026-08-23

**状态：** 方案 A 已获用户确认，待书面规格复核

**承接规格：** `2026-08-22-agent-conversation-foundation-design.md` 第二阶段

## 1. 业务目标

把画布 Agent 从“文本模型调用产生一张旁侧任务卡，完成后一次性显示结果”硬切为真正的对话体验：用户消息、Agent 可见回复、询问、工具、审批、错误与媒体交付都在同一对话时间线中表达；文本模型的内部调度任务不再出现在普通用户的任务中心或画布活动任务浮层。

后台仍必须保留文本推理的 Task、BillingOrder、供应商请求、Token 用量、审计日志和恢复事实。隐藏只改变客户可见性与可操作权限，不删除、不绕过也不伪造商业事实。图片、视频、音频和其他拥有独立交付物的任务继续作为用户任务展示。

## 2. 已确认的产品边界

- 普通用户只在 Agent 对话中观察和控制 Agent 推理；不得从通用任务接口取消、重试或读取内部文本任务。
- 管理后台、计费核对、运营审计、故障诊断和数据分析仍可按权限读取内部任务。
- Agent 最终文字必须来自供应商真实流式响应；禁止前端定时拆字、完成后回放或其他假流式效果。
- 工具调用、结构化询问、审批和媒体产物继续使用现有持久事件与专用卡片，不伪装成普通文本。
- 手动/自动模式、付费媒体审批、交付验收、Skills、画布 revision/CAS 和 Artifact Ledger 不改变。
- 模型原始结构化 JSON、内部提示词、工具参数草稿和推理过程不得作为用户可见文字输出。

## 3. 方案选择

### 3.1 采用：内部任务 + 原生流式对话

```text
Provider Chat Completions SSE
  -> ProviderStreamAdapter（解析真实 chunk、usage、request ID）
  -> AgentDecisionStream（累积严格 JSON，提取已确认的 final.message 前缀）
  -> AgentRunEvent(model.delta / agent.message / model.rejected)
  -> AgentTimelineItem(agent_message: in_progress -> completed|failed)
  -> persisted SSE(sequence)
  -> React conversation reducer
  -> 同一 Agent 助手气泡逐段增长
```

模型调用仍由耐久 Task 执行，但该 Task 的受众固定为 `internal`。用户看见的是 Agent 领域时间线，不是底层队列实现。

### 3.2 不采用：只在 Web 过滤任务类型

仅隐藏卡片会让任务列表计数、直接查询、取消、重试和其他客户端继续暴露内部任务，也会把安全边界依赖在不同页面的重复过滤上。因此可见性必须由服务端领域和查询契约统一执行。

### 3.3 不采用：取消文本 Task、在 HTTP 请求内同步调用模型

这会破坏队列租约、进程重启恢复、Token 预留与结算、供应商账单核对、并发额度和故障审计，不符合商业运行要求。

### 3.4 不采用：模型完成后由前端模拟打字

这不是流式响应，无法降低首字等待时间，也无法支持断线补发；同时违反真实状态原则。

## 4. 领域边界

### 4.1 Task 受众

Task 增加显式、不可空的受众事实：

- `customer`：可进入普通用户任务列表，可通过用户任务接口查询和执行合法操作。
- `internal`：只供服务端协调、管理审计、计费和运维使用，普通用户任务接口不可见且不可操作。

新建 `agent_runtime_model` Task 必须写入 `internal`；所有媒体和现有用户创建任务写入 `customer`。迁移把已有 Task 先统一写为 `customer`，再把类型为 `agent_runtime_model` 的历史记录明确改为 `internal`，不得依赖空值或运行时默认值。

普通用户列表查询必须在数据库分页与计数之前过滤 `audience=customer`，避免先取固定条数再过滤造成少页、错页和活动任务闪烁。普通用户的详情、日志、取消和重试入口都要验证受众；内部任务只能通过 Agent Runtime 或管理员权限入口操作。

### 4.2 Agent 文本 Item 生命周期

每次模型步骤使用一个确定性 `agent_message` Item：

```text
provider 接受请求
  -> item.started(in_progress)
  -> zero or more model.delta(userVisible=true)
  -> agent.message(completed)
       或 model.rejected / run.failed(failed)
```

非最终工具决策如果没有用户可见文字，可以没有 `agent_message` Item；工具状态仍按现有 `tool_call` Item 表达。结构化询问只创建 clarification Item。禁止为了让页面“有动静”而创建空消息或模板消息。

## 5. 供应商流式契约

### 5.1 请求

Agent 的 Chat Completions 请求固定携带：

- `stream: true`
- `stream_options.include_usage: true`
- 当前冻结模型、最大输出 Token、系统指令和用户上下文
- 现有 `response_format=json_object`

不支持上述契约的模型不得作为 Agent 可用模型发布；禁止静默退回非流式调用或切换其他模型。

### 5.2 响应适配器

新增单一 Provider streaming adapter，职责只包括：

1. 校验 HTTP 状态与 `text/event-stream` 协议。
2. 按 SSE frame 解析 Chat Completion chunk。
3. 顺序输出原始 content delta。
4. 收集最终 usage、finish reason 和供应商 request ID。
5. 对截断、未知 frame、无 `[DONE]`、无正文、非法 usage 和上下文取消返回稳定错误。

适配器不得理解 Agent 业务决策，也不得直接写数据库。

### 5.3 严格 JSON 与可见文字分离

当前模型仍返回严格 `ModelDecision` JSON。`AgentDecisionStream` 累积完整原文供最终 `ParseModelDecision` 校验，同时使用独立、可测试的增量 JSON 观察器定位 `final.message` 字符串。

只有在 JSON 语法已经确认当前字符串属于顶层 `kind=final` 的 `final.message` 时，才能发布解码后的新增长度。`kind` 尚未确认时可以在内存中暂存候选前缀；确认后按顺序释放。工具参数、clarification、expectedDelivery 和其他 JSON 字段永不进入用户可见事件。

完整响应仍必须通过现有严格解析和交付合同校验。若已显示部分文字后最终 JSON 无效，消息 Item 转为 `failed` 并保留已收到的供应商事实；Runtime 记录 `model_decision_invalid` 并按现有有界自修链路继续或失败，不能把未验证文字当作最终交付。

## 6. 事件与事务

- `model.delta` payload 只包含 `delta`、`userVisible=true` 和必要的确定性消息身份，不包含完整 JSON。
- 每个 delta 必须先持久化并取得连续 sequence，再通过 SSE 投影；Web 不消费未落库的临时 chunk。
- 首个可见 delta 与 `agent_message` Item 的 started 状态必须满足同一事务边界，断线重放不能出现“有 delta、无 Item”。
- `agent.message` 完成事件保存最终严格验证后的正文，与当前 RuntimeState 的 `finalMessage` 一致。
- usage 与供应商 request ID 必须在 Task 终结前持久化；断流或客户端取消仍按已有账单核对策略保留不确定状态，禁止退款猜测。
- 重放继续使用 `runId + sequence`，重复 delta 由 sequence 去重；不得以字符串内容去重。

## 7. Web 对话表达

前端建立以 `itemId` 为键的时间线 reducer，而不是把 SSE 事件只翻译成“执行项已更新”文字：

- `item.started` 创建对应的助手消息、工具、询问、审批或资产项。
- `item.delta` 只向对应 `agent_message` 追加服务端提供的增量。
- `item.completed` 用权威最终内容收口，校验已累计文本是最终内容前缀；冲突时显式报协议错误并重新读取 Run，不静默覆盖。
- `item.failed` 保留已经显示的文本和真实失败状态。
- 历史加载直接使用服务端 Item 最终投影；活动 Run 使用 snapshot 加 sequence 后续事件恢复。

运行过程区只展示有业务意义的状态、工具和审批，不再罗列每个底层事件序号。Agent 输入框在运行中允许 steer，停止按钮只停止后续 Agent 行为。画布活动任务浮层只消费 `customer` Task，因此不再出现文本推理记录。

## 8. 权限、安全与隐私

- 所有 Agent 事件继续校验租户、用户、项目、画布、Thread 和 Run 作用域。
- 普通用户访问内部 Task 的详情、日志、取消或重试统一返回不可见语义，不泄露其存在、提示词或供应商身份。
- 管理入口继续经过管理员权限校验，并保留 Task、BillingOrder、API call log 和 Agent run 的关联身份。
- 日志只记录字符数、chunk 数、sequence 范围、finish reason、usage 状态和错误码；不记录完整提示词、完整回复、API Key 或供应商凭据。
- SSE 写入使用请求上下文取消；客户端断开只停止向该连接发送，不取消已经提交的 Agent Task。用户显式停止必须走既有 interrupt/CAS 契约。

## 9. 错误契约

新增或明确以下稳定错误：

- `agent_provider_stream_unsupported`
- `agent_provider_stream_protocol_invalid`
- `agent_provider_stream_truncated`
- `agent_visible_delta_projection_failed`
- `agent_message_delta_conflict`
- `agent_internal_task_not_user_accessible`

错误必须关联 task、billing order、thread、run、item 和最后成功 sequence。任何异常都原地失败或进入现有有界自修，不做非流式回退、模型回退、模板回答或前端伪完成。

## 10. 实施分段与变更预算

### 里程碑一：内部任务隔离

- 生产职责：Task 受众事实、用户任务查询/操作策略、Agent Task 创建标记。
- 生产文件目标：不超过 8 个。
- 净新增生产代码目标：不超过 350 行。
- 门禁：迁移测试、用户列表/详情/日志/取消/重试权限测试、管理员可见性测试、任务中心与画布活动任务前端契约测试。

### 里程碑二：真实流式对话

- 生产职责：Provider SSE adapter、决策增量投影、持久事件/React reducer。
- 生产文件目标：不超过 10 个；若超过则先拆为 provider/runtime/web 三个独立模块再继续。
- 净新增生产代码目标：不超过 800 行。
- 昂贵门禁只在里程碑稳定后运行：Go 全量测试与 race、Web 全量测试与 build、Docker 重建、真实 DeepSeek 对话、断线恢复与一次计费核对。

两阶段分别形成职责单一、可独立回滚的提交。工作区中的其他未提交改动不得混入。

## 11. 测试矩阵

必须按 TDD 先建立失败用例：

1. 用户任务列表在分页前排除 internal Task，管理员列表仍可见。
2. 普通用户不能读取 internal Task 详情/日志，也不能取消或重试；Agent 协调器仍可推进它。
3. 流式 adapter 正确处理分帧边界、UTF-8 跨 chunk、多个 data 行、usage 和 `[DONE]`。
4. 非 SSE、未知 payload、提前 EOF、无正文和 context cancel 显式失败且不回退。
5. 增量 JSON 观察器只释放严格 `final.message`，不泄露 tool、clarification 或其他字段，正确处理转义和 Unicode。
6. delta 与 Item/Event 同事务；写入失败不推进 sequence，重试不重复字符。
7. 最终严格 JSON 无效时 Item 失败，已发布文字不被认定为最终交付。
8. Web reducer 按 itemId 追加 delta、忽略已消费 sequence、处理完成内容冲突和重连补发。
9. 活动文本推理不会出现在任务中心和画布活动任务浮层；媒体任务不受影响。
10. Token 预留、usage 持久化、供应商账单核对、成功结算、断流不确定和重复回调保持幂等。

最终门禁包括 focused tests、`go test ./...`、受影响包 race、Go build、Web tests、TypeScript/build、`git diff --check`、本地 Docker 健康检查，以及一次经授权的真实模型流式回归。

## 12. 文档、升级与回滚

- 实现时同步更新根 `README.md` 的“单一 Agent Runtime”和“Agent 模型计费链路”，删除“未来才评估 streaming”的过期表述。
- 数据迁移先于新后端启用；新 Web 只能连接已经支持新事件契约的后端。
- 协议版本升级，旧 Web 遇到新协议必须明确拒绝；不维护新旧流式双轨。
- 回滚只能回到能够读取 Task audience 字段和新事件的后端版本，不做数据库降级、不删除历史 delta。

## 13. 完成定义

只有同时满足以下条件才算完成：

- 普通用户不再看见或操作 Agent 文本内部任务，管理员和账务审计仍可追溯。
- 首个真实文本 delta 到达后，同一个 Agent 助手气泡逐段增长，无任务卡和假打字动画。
- 工具、询问、审批和媒体交付仍在同一会话中使用各自真实卡片。
- 刷新、断线、换页或服务重启后，文本与状态按持久 sequence 无丢失、无重复恢复。
- 严格决策、交付验收、积分预留、账单核对和多租户权限全部保持原有生产级约束。
