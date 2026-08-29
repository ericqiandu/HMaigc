# Agent Runtime v4 与真实装配本地验收报告

日期：2026-08-29

范围：Task 29，`Runtime v4 / Policy v4 / Tool schema v5 / UI protocol v4 / Production schema v2` 硬切

外部成本：未调用任何真实供应商或付费媒体模型

## 验收结论

- 新建 Run 只写入 `4/4/5/4/2`；活动旧 v3 Run 在恢复或继续执行前以 `runtime_schema_retired` 显式拒绝，终态 v3 历史仍可只读。
- Tool schema v5 是唯一可执行工具词表，并包含 `media.assemble`；旧 schema 不能调用该工具。
- Runtime v4 只接受 `assembly_plan.v2`。`assembly_plan.v1` 仅保留在终态历史解码和展示路径，不可重新执行或原地升级。
- 两个已批准视频 revision 可通过现有内部 Task 队列装配为最终 Resource 和 Artifact revision；输出 Resource 已由服务读取并校验内容，不依赖前端假成功。
- 排队停止、租约过期后的 Worker 重启接管、取消后的迟到成功不采纳，以及 running/succeeded/failed/cancelled 的持久 sequence 投影均通过。
- Web 协议解析、终态 v3 只读历史、同一时间线卡片的恢复与增量合并、停止后忽略迟到输出等 107 个定向用例通过。

## 确定性装配证据

命令：

```text
go test ./internal/service -run 'Test(MediaAssemblySuccessMaterializesChecksummedResourceAndArtifact|MediaAssemblyCancellationStopsQueuedTask|MediaAssemblyExpiredLeaseIsRecoveredAfterWorkerRestart|MediaAssemblyLateSuccessAfterCancellationIsUnadopted|ProjectAgentEventMapsStrictMediaAssemblyLifecycle)$' -count=1 -v
```

结果：5 个顶层测试全部通过。成功装配用例使用两个真实存储的测试 Resource 和两个已批准候选 revision，最终事实为：

| 事实 | 标识 |
| --- | --- |
| Canvas | `runtime-canvas` |
| Run | `runtime-run` |
| Task | `assembly-1af2b3bbe4ce01165524aa14` |
| Resource | `assembled-1af2b3bbe4ce01165524aa14` |
| Artifact | `final-1af2b3bbe4ce01165524aa14` |
| Artifact revision | `8a8d308ba22527a53c53fe601372866bf4e59bba785d2076120ffd63b5feae6e` |

测试通过 `OpenResource` 打开最终 Resource，并读取到预期的确定性装配内容 `assembled-video`。这验证了最终交付落点是真实 Resource，而不是只有 timeline 文案或前端卡片。

## Sequence 与 SSE 回放证据

`TestProjectAgentEventMapsStrictMediaAssemblyLifecycle` 对同一个 `run-assembly` 的装配条目执行下列持久事件投影：

| sequence | itemId | 持久状态 | UI 事件 |
| ---: | --- | --- | --- |
| 1 | `assembly-item-running` | `running` | `item.delta` |
| 2 | `assembly-item-succeeded` | `succeeded` | `item.completed` |
| 3 | `assembly-item-failed` | `failed` | `item.failed` |
| 4 | `assembly-item-cancelled` | `cancelled` | `item.failed` |

投影要求 `threadId`、`runId`、`itemId`、`itemKind`、source sequence 和持久 payload 全部一致；未知协议、未绑定条目或包含受限 reasoning 字段的 payload 会显式失败。Web 定向测试同时验证乱序/重复事件不会重复副作用，刷新可从持久历史重建同一条目，停止后迟到 delta 不再显示。

## 交付校验

命令：

```text
go test ./internal/agentruntime -run 'Test(FinalAssemblyDeliveryRequiresSuccessfulTaskReadyResourceAndCurrentRevisions|AssemblyPlanV2AcceptsExplicitOrderedAssemblyContract|AssemblyPlanV2RejectsIncompleteContradictoryAndStaleContracts)$' -count=1 -v
```

结果：全部通过。Verifier 会拒绝缺失或失败的装配 Task、未 ready 的 Resource、陈旧 Artifact revision、陈旧 Canvas revision，以及缺字段、冲突音轨、越界时长、非法编码或陈旧上游 revision 的 `assembly_plan.v2`。

## Web 与本地浏览器检查

Web 定向命令：

```text
pnpm exec bun test test/agent-runtime-api.test.ts test/agent-conversation-reducer.test.ts test/agent-production-api.test.ts test/agent-production-card.test.tsx test/canvas-agent-collaboration.test.ts test/canvas-agent-runtime-panel.test.tsx test/use-agent-runtime-timeline.test.tsx
```

结果：`107 pass / 0 fail`。其中明确覆盖终态 `3/3/4` 保留原始 `guided/automatic` 配置并可只读查看，以及同合同非终态 Run 被拒绝继续执行。

Web 生产构建、TypeScript 检查和全部 bundle 预算通过。Web 全量测试为 `711 pass / 2 fail`；两项失败均来自本任务开始前已经存在且未暂存的 `web/src/lib/video-model-capabilities.ts` 改动，其错误为 `模型 doubao-seedance-2-0-260128 的后台视频能力契约不完整`，对应 `web/test/canvas-generation-defaults.test.ts` 的两个视频默认参数用例。本任务没有修改或暂存该能力契约。

内置浏览器刷新 `http://127.0.0.1:3000/canvas/Q7T5CS18n_TIYaFqCjqSp` 后，画布与 Agent 面板正常恢复，读取到 `当前画布 v88`、历史/新建对话入口、输入区、模型与 Skills 控件；浏览器控制台未发现 error。该检查只做现有本地应用的 UI smoke test，没有向运行中的本地服务注入测试数据库，也没有发送 Agent 指令，因此没有把服务测试中的假 Resource 冒充为浏览器真实媒体播放证据。

## 未执行的外部门禁

- 未调用真实视频供应商，也未产生付费订单。
- 未在云端部署或验证线上数据迁移。
- 本地确定性 fixture 位于隔离测试数据库，未注入开发环境账号；因此浏览器只完成协议/UI smoke test，最终 Resource 的可读取性由服务测试直接证明。
