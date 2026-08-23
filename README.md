# HMaigc

HMaigc 是面向 AI 影视与短剧生产的商业化创作平台，覆盖项目、章节、角色资产、自由画布、图片/视频/音频生成、团队协作、会员计费、积分运营和管理后台。

## 代码结构

- `web/`：Vite、React、TypeScript 前端。
- `backend/`：Go、Gin、GORM 后端。
- `docker-compose.yml`：使用 SQLite 的本地一体化环境。
- `docker-compose.production.yml`：使用 PostgreSQL 和 Redis 的生产环境。

仓库只保留上述两条运行路径，不再维护旧镜像部署、重复 Compose 或上游一键安装脚本。
画布助手已硬切到单一服务端 Agent Runtime，并通过后端系统模型渠道完成鉴权、计费和请求审计；不再提供本机 Agent、Codex 插件连接或浏览器内模型循环。服务端负责冻结模型、决策循环、高层工具协调、通用交付验收和可恢复检查点；Web 只提交用户目标与真实选区事实、展示持久化事件并处理审批。

### 首页到 Agent 的创作链路

- 首页创作框先上传参考图片并创建真实画布项目，把提示词、账号级资源 ID、系统动态模型选择、平台第一方公开 Skill 目录与显式执行模式写入项目内的 `pendingAgentLaunch`；提示词和临时 Blob URL 均不进入 URL 或持久事实。
- 打开新画布后，Agent 面板用该请求创建或复用服务端 thread，并以持久化 `clientRequestId` 启动 run；只有取得运行事实后才消费启动请求。启动响应丢失时刷新或重试仍复用同一请求 ID，不重复创建运行。
- Agent 推理模型、工具规划与交付判断由服务端 Runtime 统一决定。首页与画布共用同一份草稿契约，允许用户从系统动态目录显式指定本次运行的图片/视频模型、公开 Skills、参考图片与执行模式；Web 只提交 `channelId + model`、Skill 目录标识、账号级 `resourceId + name` 和模式枚举。服务端重新校验模型可调用且已定价、读取 Skill 详情、核对资源归属与可用状态，并将模型、Skill 指令、资源 MIME/尺寸及执行模式冻结进 run checkpoint。每个 run 的步骤预算由服务端固定为 24，浏览器提交的步数不参与运行事实。手动模式自动执行只读工具，规划与画布写入必须审批；自动模式可连续完成无费用的规划和画布提交；两种模式下每一个 `production.render` 付费 Artifact 都必须展示冻结报价并由用户确认后才创建 Task、BillingOrder 和积分预留。
- 模型只可调用 `skill.load`、`production.plan`、`production.render`、`canvas.commit` 四个高层工具；角色、服装、道具、场景参考图和正式分镜/视频结果只以服务端 Task、BillingOrder、Resource、Artifact Ledger 和交付验收事实为准。

### 单一 Agent Runtime

- 运行作用域固定绑定租户、用户、项目、画布、会话和运行记录；每次工具执行都会重新读取真实画布权限，不信任浏览器缓存的权限声明。
- `AgentRunEvent` 是追加式审计真源，`AgentTimelineItem` 是与 Run、Event、Checkpoint、ToolCall 同事务维护的查询投影。Run 初始化固定写入 `run.created`、`user.message`、初始 Checkpoint 和已完成的用户消息 Item；Item 使用确定性身份、连续 ordinal 与唯一来源 sequence，任何投影冲突都会回滚整次状态迁移，禁止以不完整时间线伪装成功。
- Agent 文本模型仍通过耐久 Task 保留队列恢复、计费、供应商请求与审计事实，但 Task 受众固定为 `internal`；普通用户任务列表、详情、日志、取消和重试接口只查询 `customer` Task，管理员、账务与 Runtime 协调器继续按权限读取内部事实。媒体 Task 仍保持客户可见。
- Agent Chat Completions 固定使用供应商 `text/event-stream` 与 `stream_options.include_usage=true`。服务端只从严格 `ModelDecision` JSON 中释放已确认属于顶层 `kind=final` 的 `final.message` 增量；`reasoning_content`、工具参数、澄清结构和交付合同不进入用户正文。每段可见增量与助手 Item 同事务写入连续 sequence，再由 SSE 断线补发；任务断线重领会以新的 `item.started` 原子重置同一消息，协议截断或最终决策无效则追加 `agent.message_failed` 并保留已流出正文。UI 协议 v2 的每个 Item 事件显式携带持久化 `itemKind`；React 只把 `agent_message` 按 `itemId + sequence` 增量维护为同一个助手气泡，用户消息仍由 Run 状态单独展示。刷新时从零重放耐久事件、仅对新游标执行业务副作用，并只保留有界事件展示窗口；不提供完整响应回退、模型降级或前端假打字。
- 工具、审批和结果共用同一个 `tool_call` Item 生命周期：等待审批、已批准、已开始、完成、失败、拒绝或中断都更新同一身份，不创建并行的“执行中”残留项。运行中的追加指令以 `clientRequestId` 持久化并幂等去重；显式中断使用 `stateVersion` CAS，并关闭尚在等待的澄清或工具 Item。已经提交的媒体任务不被回滚，迟到成功结果追加独立 `artifact.available` 事件和 Artifact Item，保持 Run 终态及 Checkpoint 不变。
- 时间线、生产计划和迟到资产的每一次读写都重新校验租户、操作者、项目、画布、Thread 与 Run 所有权；时间线只保存已鉴权的 `resourceId` 和媒体元数据，不保存会过期的 OSS 签名地址。
- `stateVersion` 独立承担审批、工具结果和恢复操作的并发控制，`stepNumber` 只在模型作出下一次决策时递增，避免工具恢复被重复计费为模型步骤。
- Runtime 只有一个事件驱动协调入口：run 创建、模型任务终结、审批决定和媒体任务终结都会以持久化事实唤醒同一协调器；协调器在有界转换次数内继续免费工具或创建唯一下一模型任务。worker 不再每 5 秒扫描并盲推全部运行，只按分钟检查超过恢复阈值且仍未终结的 run，并使用跨实例互斥与游标恢复进程中断后的事件。
- 信息确实不足时，模型可返回严格的 `clarification_request`，由 Runtime 在同一 Run 冻结交付合同和 1–3 个结构化问题，并切换到不占用 worker 的 `waiting_input`。问题类型只允许单选、多选和自由文本；逐题保存只写 checkpoint/event，不消耗模型 Token，也不创建工具、媒体任务、账单或积分冻结。最终提交使用 `stateVersion` CAS 将 pending 问答原子追加到不可变 history，产生 `clarification.responded` 后只唤醒一次同一协调器；下一模型步骤读取原始结构化问答继续执行，不创建第二条 Run。完全相同重放不增加事件或任务，并发提交只有一个事务可推进。
- 追问内容由 Agent 基于本轮真实事实自主决定，本地仅校验问题/选项/答案的类型、数量、身份、长度和权限，不做关键词路由、默认答案或语义改写。`requestId` 复用、回答冲突、过期版本、非法问题身份与非等待态提交都会返回稳定错误码及最新状态版本；用户显式忽略的问题以 `skipped=true` 保留，禁止补写推测答案。
- 首个模型决策必须声明结构化 `expectedDelivery`，Runtime 将其冻结为整条 run 的不可变交付合同；`final_message` 与 `canvas_revision` 条件不得携带 Artifact 字段，只有 `artifact` 条件必须声明具体资产类型。后续每个工具调用和最终答复必须逐字段保持一致。工具失败、审批拒绝或证据不足不能把图片、视频或画布交付降级成文字回答，合同漂移会作为显式修复事实回灌同一条有界执行链。
- 工具调用按 `runId + toolCallId + actionVersion` 冻结并幂等登记。`production.plan` 的严格 DTO 同时保存非时间线 `references`（角色、服装、道具、场景）和正式镜头 `shotKey/order/durationMs/scriptText/imagePrompt/videoPrompt/referenceKeys/dependencies`。参考图不占时间线，不能伪装为 0 秒镜头；重复/缺失参考、非连续顺序、未来依赖或正式镜头总时长不匹配都会显式失败。计划成功后创建不可变版本和 Artifact Ledger：参考图以 `referenceKey` 标识，分镜与视频以 `shotKey` 标识。`production.render` 先逐项审批并生成参考图；分镜只有在绑定参考 Resource 全部就绪后才创建任务，并把真实参考图作为模型输入；视频继续以同镜分镜 Resource 为首帧。能力、报价、Task、BillingOrder、积分预留、远程结果物化和恢复都沿现有单一路径持久化，不依赖浏览器重复提交。
- `canvas.commit` 从计划、Artifact 和就绪 Resource 构造稳定节点/连线 ID 的确定性投影，使用画布 revision CAS 与稳定 `clientMutationId` 只提交一次。每轮模型事实都包含服务端权威 `canvasRevision`；画布已变更时显式返回 `canvas_revision_conflict + currentRevision`，投影或参数不完整时返回可审计的结构化 `reason`，供同一有界执行链按真实事实纠正，禁止猜测版本或重复盲试。画布提交成功后以 Artifact 状态/attempt CAS 回填 `canvasNodeId`，成功媒体 Artifact 同步进入 `committed`，进程在提交与回填之间中断时可安全重放。交付验收会从最后一次成功提交的完整计划恢复剧本、参考图、分镜和视频证据，续跑只生成剩余资产时也不会遗失前序交付事实或重复提交画布。
- 个人与团队画布统一使用同一条 WebSocket revision/CAS 增量变更通道；浏览器加载后必须先同步服务端权威快照，再提交本地差异。`PUT /api/canvas-projects/:id` 只负责首次创建远程画布，已存在项目一律拒绝整页覆盖，防止浏览器旧快照覆盖 Agent、协作者或其他设备刚提交的节点。
- Web 在启动 Agent run 前先将当前画布未提交变更收敛到权威 revision；首次权限预检只建立远程基线与权限事实，不覆盖 WebSocket 建连前的本地编辑。SSE 只消费协议版本 `2` 的 `AgentUIEvent`，并在收到携带成功 `canvas.commit` 工具结果的 `item.completed` 时读取 `committedRevision`，再通过已鉴权的协作查询获取画布事实；查询 revision 低于已确认提交或低于当前本地基线时显式失败或忽略过期响应，禁止旧快照回退已展示的 Agent 交付。
- 每个新工具动作必须使用未出现过的 `toolCallId + actionVersion`；模型误复用历史身份时，Runtime 记录显式 `tool_identity_reused` 修复事实并继续同一执行链，不会再次写入冲突记录。同一工具以语义相同的 JSON 参数连续返回相同错误码与相同结构化失败证据时，首次错误允许 Agent 根据事实修正，第二次直接以该错误终结 run；错误原因已经变化时继续留给 Agent 修正，禁止误判为死循环。最后一个模型步骤不得再开启新工具调用；历史运行若已进入该状态，拒绝或完成工具后会保存结果并以 `step_budget_exhausted` 明确终结。图片生成公共契约要求规范字符串字段 `size/count` 并强制匹配本次运行冻结的图片模型；`quality` 仅在动态 `providerCapabilities.qualities` 发布非空候选时才允许从候选中填写，候选为空则必须省略，禁止默认画质、`ratio/resolution` 或其他未知字段绕开正式任务契约。
- 模型每轮只接收当前用户真正可调用、已定价且凭据健康的图片、视频和音频模型事实；用户显式选择图片或视频模型时，本次 run 的对应能力目录只保留该模型，未选择的能力仍使用完整可调用目录。模型作出工具决策后，渲染准备严格使用该模型步骤 prompt 中冻结的可调用目录，不会在审批前重新读取已漂移的在线目录。Skill 目录由 HMaigc 自有数据库和随版本发布的 `SKILL.md` 建立，不再依赖外部社区网络接口；目录列表只返回元数据，完整指令只在查看详情或服务端冻结 run 时读取。每个已发布版本以目录、版本号和 SHA-256 校验值形成不可变事实，启动时发现同版本正文漂移会显式拒绝发布；升级迁移会从历史 checkpoint 与 event 已冻结的指令一次性计算并写入校验值，迁移后仍由同一严格契约读取，不保留旧分支。模型和 Skill 配置随 run checkpoint 冻结，不暴露 Base URL、Key 或凭据密文，也不会用硬编码候选兜底。已选 Skill 的完整执行说明必须先通过 `skill.load` 按冻结版本按需加载，未加载时模型只看到目录元数据；Runtime 会冻结已加载目录并在最终答复前拒绝遗漏选定 Skill 的交付。存在活动生产计划时，每轮同时注入当前 Agent thread 在同一租户、项目与画布作用域内最新的活动 `productionPlan` 与完整 Artifact Ledger，因此后续 run 能继续上一 run 的计划和已付费资产；不同 thread 之间严格隔离。工具准备或执行失败都会先持久化失败的 ToolCall/ToolResult，再把结构化原因回灌同一执行链；不能因 `LastToolResult` 覆盖而遗失计划事实或重复规划。
- 每次 run 同时冻结 `runtimeVersion`、`policyVersion` 与工具 schema 版本。工具 schema v3 保留四个高层工具，但把参考资产提升为正式计划与 Artifact Ledger 契约；v2 中尚未执行且检查点、模型 Task/Billing 事实完整的排队 run 可按既有事务退役为 `tool_schema_retired`。已经运行、等待输入/审批/工具、商业事实不一致、含工具事实或来自未来版本的非终态 run 不自动接管并会阻止启动；终态历史 run 继续保留审计证据。版本字段引入前已终结、工具 schema v1 且 Run/最终 Checkpoint 完整一致的历史记录，会通过一次性审计迁移明确标记为首代 Runtime/Policy 契约；混合零值、活动状态或事实不完整记录拒绝迁移，事件、Checkpoint、Task 与账单事实不改写。
- Agent 模型调用统一声明 Chat Completions 的 `response_format=json_object`，使 DeepSeek 与 GPT 共用同一结构化决策契约；模型仍必须通过 Runtime 的严格单 JSON 校验。决策结构无效时，Runtime 记录受控的 `model_decision_invalid` 事实并在同一有界 run 内回灌下一模型步骤自修，绝不提取文本、伪造默认决策或切换模型；达到步骤上限仍显式失败。
- 参考图片只接受当前账号已就绪的图片 Resource；服务端冻结资源 ID、显示名称、MIME 与尺寸，拒绝浏览器 Blob URL、跨账号资源和失效资源。执行模式是必填运行事实，幂等重放若更换模型、Skill、附件或模式会显式冲突，禁止静默采用新配置。
- 正式传输入口为 `GET /api/agent/threads?canvasId=...&limit=...`、`POST /api/agent/threads`、`POST /api/agent/threads/:threadId/runs`、`GET /api/agent/runs/:runId`、`GET /api/agent/runs/:runId/events?afterSequence=N`、`POST /api/agent/runs/:runId/steer`、`POST /api/agent/runs/:runId/interrupt`、`POST /api/agent/runs/:runId/clarifications/:requestId/responses` 和付费工具审批。追加指令严格接收 `clientRequestId/message/expectedStateVersion`，停止严格接收 `expectedStateVersion`，所有请求的未知字段与尾随 JSON 都直接拒绝。全部工具结果仍只由服务端执行器写入，浏览器不提交选区或工具结果事实。历史查询按当前用户、租户与画布返回最近 20 个 Thread，在校验每个 Run 最新 Checkpoint 与 Run 状态一致后，为每个 Thread 返回按创建时间排列的完整 Turn 与 Item；缺 Item 的旧终态 Run 只能从不可变事件日志构造只读投影，旧活动 Run 缺投影时显式失败。旧终态 Checkpoint 缺少当前传输必需的结构化询问或 Composer 集合字段时，传输层只读投影会补为空集合；仅当执行模式本身缺失时才明确返回 `executionMode: historical`，已有模型、Skill 与 guided/automatic 事实原样保留，也不回写 Checkpoint。SSE 只把已持久化领域事件纯投影为协议版本 `2` 的 `AgentUIEvent`，以持久 sequence 断线补发；工具、审批和澄清等会持续更新同一 Item 的生命周期事件，补发时必须根据不可变事件 payload 与上一版本 Checkpoint 重建该事件发生时的 Item 快照，再与当前材料化 Item 的身份、作用域和序号单调性核验，禁止用 Item 最新状态覆盖历史事件。未来游标、未知事件、Item 作用域冲突、非法 payload 或投影失败均返回稳定协议错误，不保留旧 RuntimeState 事件形态。
- 服务端历史是会话发现与跨设备恢复的权威来源；浏览器 `localForage` 只保存当前 Thread、活动 Run、事件游标或尚未确认的 `clientRequestId`，用于快速恢复和启动幂等，不构成会话事实。没有本地句柄时采用服务端最近活动会话；选择旧会话只切换观察和恢复目标，不取消服务端仍在运行的 Run。
- 浏览器不再调用 Agent 模型、不拼 system prompt、不维护 tool loop，也不再创建固定影视 Session。旧会话事实仅保留在历史项目数据中用于审计，不进入新运行链。

### Agent 模型计费链路

- 网站 Agent 的每次模型请求分别创建计费订单；工具调用本身不计费，图片、视频等媒体生成继续使用各自独立订单。
- Agent 模型任务创建前若确认账号余额不足或团队月额度耗尽，运行会以 `insufficient_credits` 或 `team_credit_limit_reached` 明确终结，不创建下一任务或账单，也不会由后台驱动器无限重试；并发与暂时性额度错误仍保持原有显式错误语义。
- 托管的筷子 DeepSeek 模型使用 `token_usage`：系统代理和服务端 Agent Runtime 共用同一预留/结算内核；请求发出前按后台发布的输入/缓存命中/输出单价和最大输出 Token 原子预留积分，并把同一最大输出值写入真实请求，同时冻结当时的服务地址版本和凭据版本；首版倍率固定为 1.0。
- Agent 流式请求强制开启 `stream=true` 与 `stream_options.include_usage=true`，响应 usage、Chat Completion request ID、finish reason 和断流事实会在 Task 终结前保留；缺失或无效 usage 分别标记为 `missing` / `invalid`，但资金结算仍以筷子账单的订单号、任务状态、总 Token 和实扣金额为准。账单 pending 时只异步核对，不重复调用模型。
- 筷子图片、视频与 Token 任务共用同一条账单核对 worker。新建付费任务会把供应商服务地址版本和凭据版本同时冻结到任务与账单；已取得上游任务号但本地结果不明确时，worker 只查询对应筷子账单：上游确认成功则结算本地冻结报价，明确失败且未扣费则退款，pending、矛盾或无法取得可复现运行时的账单继续保留人工核对，禁止猜测扣费结果。
- Chat Completion 响应 ID 按筷子契约从 `chatcmpl-<task_id>` 提取唯一内部任务 ID，写入计费订单后再通过任务账单接口取得真实扣费金额。成功账单原子消费实际积分并释放差额；待生成、缺失、重复或不可判定账单进入有租约、有限次数的后台核对，绝不重复发送模型请求。
- 上游账单事实不足时不会回退到固定 1 分或估算终值。超过预留、达到核对上限或凭据事实损坏都会保留冻结积分并进入显式人工核对。

## 本地开发

```bash
cd backend
go run ./cmd/server

# 另开终端
cd web
bun install --frozen-lockfile
bun run dev
```

也可以直接启动本地 Docker 环境：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/local-compose.ps1
```

默认访问地址为 `http://localhost:3000`。本地业务数据只允许位于 Git 主项目的 `.local/data`，不得提交到 Git。脚本会从 Git 公共目录解析主项目根；从 worktree 直接执行 `docker compose` 且未显式设置 `CANVAS_DATA_PATH` 会拒绝启动，防止误建第二份数据库。

### MiniMax Speech 配置

1. 在管理后台“AI 模型”中新建 `MiniMax Speech` 系统渠道，Base URL 使用 `https://api.minimaxi.com/v1`，API Key 只保存在后端。
2. 为渠道添加实际开通的音频模型和积分价格，例如 `speech-2.8-hd` 或 `speech-2.8-turbo`；用户端模型列表不会使用硬编码候选。
3. 打开“音色管理”，同步供应商音色，或在确认已获得声音本人授权后上传 10 秒至 5 分钟、20MB 以内的 MP3、M4A、WAV 样本进行克隆。
4. 为音色配置兼容模型、会员权限和发布状态。画布只展示后台已发布且与当前模型兼容的音色，任务创建前后端会再次校验权限并按模型价格扣除积分。

MiniMax 同步语音当前支持 MP3、WAV、FLAC，语速范围为 0.5–2.0。克隆源文件不会保存在本系统，只保留文件名、大小、SHA-256 和授权确认时间用于审计。没有真实 API Key 时只能完成本地契约测试，不能将模拟响应视为供应商验收。

## 生产部署

生产服务器必须安装 Docker Engine 与 Docker Compose。先从私有仓库取得代码，然后创建生产配置：

```bash
cp .env.production.example .env.production
chmod 600 .env.production
openssl rand -hex 32
```

把生成结果写入 `.env.production` 的 `POSTGRES_PASSWORD`，并至少配置：

- `HMAIGC_IMAGE_REGISTRY`：例如 `ghcr.io/ericqiandu`。
- `HMAIGC_VERSION`：与 Git 标签一致的不可变版本，例如 `v1.0.14`。
- `HMAIGC_OPS_VERSION`：独立运维控制器的不可变版本。
- `HMAIGC_RELEASES_API_URL`：用于后台检查最新 GitHub Release。
- `CANVAS_CORS_ORIGINS`：实际 HTTPS 站点 Origin。
- `CANVAS_HTTP_HOST`：有反向代理时保持 `127.0.0.1`。
- `CANVAS_HTTP_PORT`：反向代理连接的本机端口。

首次安装：

```bash
bash deploy/hmaigc-ops.sh install v1.0.14
```

生产环境应由 Caddy、Nginx 或云负载均衡器提供 HTTPS。不要直接把后端、PostgreSQL 或 Redis 暴露到公网。

后续升级与回滚：

```bash
bash deploy/hmaigc-ops.sh upgrade v1.0.14
bash deploy/hmaigc-ops.sh rollback
```

业务后端不持有 Docker socket，也不会重启自己；后台运维升级中心和服务器命令行都把任务提交给独立控制器。完整契约见 [独立控制器与一键发布说明](deploy/README.md) 与 [生产运行手册](PRODUCTION.md)。

## 上线门禁

- 在受控网络注册首个管理员后，保持公开注册关闭。
- 后台配置正式模型渠道、供应商预算、模型积分售价和并发策略。
- 配置支付商户资料并完成签名、异步通知、退款、关单和对账验收后，才能开放自动支付。
- 配置站点名称、Logo、版权、用户协议、隐私政策及备案信息。
- 对 PostgreSQL、后端资源卷和 `.settings-key` 建立加密备份与恢复演练。
- 完成前端构建、Go 全量测试、生产镜像构建和关键业务回归。
- 运行 `bun scripts/verify-spa-routes.mjs http://127.0.0.1:3000`，确认首页、画布、项目和管理后台深链接都由当前 SPA 入口提供。
- GitHub Actions 质量门禁通过后才允许发布生产镜像。

## 数据与升级

- PostgreSQL 是生产业务数据真源，Redis 只承担队列和实时广播。
- `backend-data` 保存上传文件及服务端密钥材料；数据库备份不能替代文件备份。
- 发布镜像由 GitHub Actions 构建并以不可变版本标签推送；生产服务器禁止现场构建或使用 `latest` 升级。
- 升级前同时备份 PostgreSQL 与 `backend-data`，新后端完成迁移和版本验活后才允许启动 Web。
- 禁止把 `.env`、数据库、日志、备份、上传文件或真实密钥提交到仓库。
- 页面路由名称不得复用于 `web/public` 静态目录；画布人脸模型统一位于 `/runtime-assets/canvas-models/`，避免静态目录抢占 `/canvas` 页面路由。

## 许可证与上游

本项目基于 `ddcat-ai/open-ai-canvas` 与 `basketikun/infinite-canvas` 继续开发。上游署名和版权信息保留在 [NOTICE](NOTICE) 中。

当前代码采用 [GNU Affero General Public License v3.0](LICENSE)。通过网络向用户提供修改后的程序时，需要履行 AGPL-3.0 对应的源码提供义务；正式商业上线前应由法务确认开源合规、第三方模型条款和素材版权。
