# HMaigc

HMaigc 是面向 AI 影视与短剧生产的商业化创作平台，覆盖项目、章节、角色资产、自由画布、图片/视频/音频生成、团队协作、会员计费、积分运营和管理后台。

## 代码结构

- `web/`：Vite、React、TypeScript 前端。
- `backend/`：Go、Gin、GORM 后端。
- `docker-compose.yml`：使用 SQLite 的本地一体化环境。
- `docker-compose.production.yml`：使用 PostgreSQL 和 Redis 的生产环境。

仓库只保留上述两条运行路径，不再维护旧镜像部署、重复 Compose 或上游一键安装脚本。
画布助手已硬切到单一服务端 Agent Runtime，并通过后端系统模型渠道完成鉴权、计费和请求审计；不再提供本机 Agent、Codex 插件连接或浏览器内模型循环。服务端负责冻结模型、决策循环、高层工具协调、通用交付验收和可恢复检查点；Web 只提交用户目标与真实选区事实、展示持久化事件并处理审批。

### Web 交付与页面切换

- Web HTML 随不可变 Web 镜像同源交付；哈希 JS/CSS、字体和程序图片以版本目录发布到 `https://static.hm.kunagent.com/hmaigc/web/releases/<version>/`。Actions 只构建一次 `dist`，先上传并验证完整 CDN 清单，再把同一份 `dist` 封装进 Web 镜像；HTML 中的程序资源 URL 必须精确绑定当前版本，CSP 只放行该固定静态域名，不提供同源或其他域名的静默回退。
- 用户上传与模型生成的图片、视频和音频继续保存在后端配置的对象存储中。程序包与用户媒体是两条独立生命周期：程序发布不会迁移、覆盖或删除用户媒体。
- 匿名首访只恢复鉴权外壳；画布、资产和模型配置工作区仅在登录、已登录刷新或退出边界按需加载。工作台路由只在鼠标按下、悬停或键盘聚焦形成真实导航意图时预取模块与对应数据，不再在首页加载后后台扫预取，避免与首屏程序资源和接口争抢带宽；预取失败会显式记录且不阻断当前页面。
- React Query 的共享新鲜度窗口负责页面复用；项目工作台不再在每次挂载时无条件重复请求。全局入口不预载首页专属大图，避免访问画布、后台或项目时争抢首屏带宽。

### 画布交互与媒体链路

- 图片与视频节点的 `@` 引用候选来自当前画布真实资源与公开 Skills；未连接资源必须先通过统一连线契约并建立连接，成功后才在当前光标位置插入稳定引用。拖拽连线、Agent 画布操作和服务端画布 mutation 使用同一方向语义，视频输出不能连接图片或音频输入。
- 画布拖动预览只修改被拖节点的 DOM transform 与相邻连接路径；连接目标命中使用连接开始时建立的空间索引，pointermove 热路径不扫描完整节点或连接集合，也不触发保存和网络请求。
- 用户媒体仍保存在对象存储中。画布使用受控的 `/api/resources/:id/file?direct=1` 地址直接渲染图片并流式播放视频、音频，完整 Blob 只在下载或明确复用时按需读取。登录用户上传必须成功写入后端资源，不会静默落入浏览器本地存储；访客本地模式是独立显式分支。
- 上传节点进入项目资产时只定向写入当前素材，再建立项目关联并保存关联事实；项目查询刷新不阻塞必要持久化链路，失败会留下可检索告警。

### 首页到 Agent 的创作链路

- 首页创作框先上传参考图片并创建真实画布项目，把提示词、账号级资源 ID、系统动态模型选择、平台第一方公开 Skill 目录与显式执行模式写入项目内的 `pendingAgentLaunch`；提示词和临时 Blob URL 均不进入 URL 或持久事实。
- 打开新画布后，Agent 面板用该请求创建或复用服务端 thread，并以持久化 `clientRequestId` 启动 run；只有取得运行事实后才消费启动请求。启动响应丢失时刷新或重试仍复用同一请求 ID，不重复创建运行。
- Agent 推理模型、语义规划与交付判断由服务端 Runtime 统一决定。首页与画布共用同一份草稿契约，Web 只提交用户目标、动态模型选择、已授权 Skill 版本、账号级 Resource 身份和显式执行模式；服务端重新核验并冻结这些事实。新 Run 固定使用 Runtime v5 / Policy v5 / Tool schema v6，工具调用预算固定为 24，并同时受 24 步决策上限约束；服务端还会冻结从首次启动时间计算的 30 分钟绝对截止时间。浏览器不能修改这些限制，进程重启也不会重置预算或截止时间。
- Tool schema v6 只接受 `canvas.read`、`canvas.apply_ops`、`assets.read`、`assets.publish`、`media.generate`、`skills.load` 六个原子能力及其严格参数。旧 `skill.load`、Specialist、Production Graph、`vision.analyze`、`media.assemble`、`canvas.project` 等决策不会映射或降级，而是以 `model_decision_invalid` 显式终结。六个能力均已接入当前权威数据源：读取能力只返回作用域内事实，`canvas.apply_ops` 通过画布 revision/CAS 真源原子提交，`media.generate` 复用现有 Task/BillingOrder/Resource 商业链路，`assets.publish` 在单一事务中创建 Asset、confirmed AssetVersion、ProjectAssetLink 与 AssetRepresentation。每次写入或付费执行都会重新核验租户、项目、画布、用户权限与冻结审批提案；输出只保留当前能力契约允许的已持久化事实，禁止回退旧执行图。
- 模型每轮接收精确身份与作用域、动态模型事实、按需加载的 Skill 描述，以及六原子能力的参数和结果 schema。上下文上限为 512 KiB；未加载 Skill 的 instructions 不进入上下文，`docs/`、`assets/`、`ai-metadata/`、固定工作流和固定 Specialist 顺序均不参与运行时知识装配。

### 单一 Agent Runtime

#### 当前云能力状态机（Runtime v5）

当前唯一可创建的新 Run 契约是 Runtime v5 / Policy v5 / Tool schema v6 / UI protocol v5。工具身份固定为 `runId + toolCallId + actionVersion`，已完成工具在重启或重复唤醒后只重放持久结果，绝不再次执行。`canvas.apply_ops`、`assets.publish` 和 `media.generate` 会形成带 SHA-256 的不可变审批提案；审批只接受精确的 `toolCallId + actionVersion + proposalHash + decision`，其中哈希必须是小写 SHA-256，决定只能是 `approved | rejected`。拒绝、过期或哈希不匹配会写入同一 Run 的失败工具事实并返回运行态继续规划，不伪造批准，也不把 Run 伪装成成功。模型输出不满足严格单 JSON 决策契约时，Run 以 `model_decision_invalid` 明确失败并保留诊断；不会抽取正文、生成默认决策或切换模型。

Run 的工具调用次数和绝对截止时间是持久化运行事实。协调器在任何模型或工具副作用前检查截止时间；预算耗尽分别以 `tool_call_budget_exhausted` 或 `runtime_deadline_exceeded` 终结。用户停止 Run 时，检查点、活动工具和关联 Task 在同一事务中关闭；在途供应商请求通过本地取消句柄或跨实例检查点观察停止，已有供应商请求而费用事实不足的账单进入 `uncertain` 等待核对。

#### 当前 Web 运行投影

- Web 只投影 Runtime v5 的持久化 Turn/Item 与当前 SSE 生命周期事件；刷新时先从服务端历史恢复，再由更高 sequence 的实时事实覆盖同一 `itemId`。界面不推导 Production Stage、Specialist、内部思考步骤、阶段耗时或模拟进度，也不保留旧生产卡兼容入口。
- `canvas.read`、`assets.read`、`skills.load` 仅显示事实活动，不产生确认按钮。`canvas.apply_ops`、`assets.publish`、`media.generate` 共用唯一审批卡，分别展示冻结的画布操作、资产发布目标或模型参数；`media.generate` 还必须展示冻结报价与有效期。每张卡只允许一组“批准执行 / 拒绝执行”，提交身份固定为 `toolCallId + actionVersion + proposalHash`；过期、身份变化或影响不匹配时禁用执行并要求 Agent 创建新提案。
- 已完成工具必须提供与状态一致的 `succeeded` 事实；已完成的 `media.generate` 还必须提供全部可验证 Resource 身份、媒体类型与站内地址，缺失或畸形数据作为协议错误显式展示。真实生成资源一经出现即保留在活动记录中，后续画布写入或投影失败不得隐藏、覆盖或删除该资源。

#### 已退役的多专家生产事实（Runtime v4，仅历史审计）

下列 Graph、Stage、Specialist、Artifact Ledger、旧媒体生成和装配结构只用于读取既有 Runtime v4 / Policy v4 / Tool schema v5 / Production schema v2 审计事实，不再接受新模型决策，也不能恢复为活动 Run。正式 HTTP 路由不再注册阶段审核和 Artifact revision 操作，UI protocol v5 也拒绝 `stage_review_resolution`、`artifact_review` 等退役事件；历史数据库事实保留用于运维审计，但不会被投影成当前交互卡或重新执行。Web 的旧生产卡、阶段审核组件及其 API client 已删除，不存在兼容入口。更早的 Runtime v3 / Policy v3 / Tool schema v4 终态历史同样只读保留，不双写、不回填。

- `agent_production_graph_versions` 按完整租户/用户/项目/画布/Thread/Run scope 追加不可变 Graph 版本；同一 scope 的 `graphKey + version` 唯一，完整阶段定义只写入一次。
- `agent_production_stages` 保存 Graph 内阶段的可恢复 CAS 生命周期；`graphVersionId + stageKey` 唯一，`status/version/reviewRevisionId/lastErrorCode` 是可变控制事实，正文和媒体内容不写入阶段行。
- `agent_specialist_runs` 保存 Specialist 的模型继承、Skill 版本、工具 allowlist、交付合同、心跳与终态；生命周期只通过 `version` CAS 推进，不能隐式换模型。
- `agent_artifacts` 只保存稳定 Artifact 身份、类型、head revision 与生命周期 CAS；`agent_artifact_revisions` 以 `artifactId + revision` 唯一追加 payload、真实 Resource、上游精确 revision、模型请求身份和 Skill 版本，发布后的内容字段没有原地更新路径。
- `agent_asset_binding_revisions` 追加角色、场景、道具等绑定快照及其精确上游 revision；修改绑定必须创建新 revision。
- `agent_asset_publications` 是已批准 Artifact revision 到现有 Asset/Version/Link/Representation 真源的幂等桥；`tenantKind + tenantId + domainProjectId + artifactRevisionId + publicationPurpose` 唯一，状态、错误和目标资产身份使用 `version` CAS 收口。
- Narrative Specialist 只能提交严格的 `script_bundle.v1`，其中完整剧本、角色目录及显式的场景、道具、声音角色集合共同构成首个用户可见 `artifact_review`；`guided` 与 `automatic` 都在该审核点停止，不会提前创建付费媒体 Task。用户要求修改时，原评论只作为私有 Specialist 输入保存，阶段以版本 CAS 回到 `running`，同模型、同已发布 Skill 事实的新子 Specialist 追加下一 revision，原稿不覆盖；相同客户端请求重放不会重复调用模型或追加版本。每个审核事件先写入连续 sequence，再由 SSE 按相同 sequence/content 重放。
- 阶段审核统一通过 `POST /api/agent/runs/:runId/stages/:stageId/reviews` 提交精确 `stageVersion + revisionId + clientRequestId`；批准、要求修改和停止都会在同一数据库事务内更新阶段 CAS、追加 `approval.resolved` 的持久 sequence 事实并完成同一个审批 Item，停止还会在该事务内终结整棵 Run。相同请求身份返回原结果，不同身份再次决议返回 `stage_review_conflict`，过期阶段或 revision 返回 `stage_approval_revision_mismatch`。用户查看正文只走 `GET /api/agent/runs/:runId/artifacts/:artifactId/revisions/:revisionId` 的四级精确作用域查询，跨租户、项目、画布、Thread、Run 或 Artifact 的读取统一表现为不存在；审核评论不进入用户会话正文。
- 图片或独立音频 revision 只有在同一候选批准请求中显式携带完整 `publicationIntent` 后才可发布到项目资产库；阶段审核事务会原子保存审核 revision、精确候选 revision、操作者、`clientRequestId` 与发布目标，发布事务只消费这份不可变授权，不按排名或默认分类推断。服务端按互斥的 `specialist | media_tool` 生产者类型重新核验当前项目编辑权限：Specialist 路径保留原有成功 Specialist 血缘，`media.generate` 路径则严格验证同一 Run 内唯一成功 ToolCall、冻结请求、`internal` 且成功的媒体 Task、Task 结果中的精确就绪 Resource、供应商请求身份与已结算 BillingOrder；视觉审核 revision 本身绝不被当作媒体发布。单一事务创建 `Asset + confirmed AssetVersion + ProjectAssetLink + AssetRepresentation + AgentAssetPublication`，Representation 直接引用原 Resource，不复制或重新上传对象；不可变审计同时冻结批准/选择内容、实际模型与请求参数、供应商版本、计费档位、实扣事实及内容哈希。发布身份按租户、项目、候选 revision 与 purpose 唯一；完全相同审批即使并发、父 Run 已结束或首次持久化失败后重试，也只恢复或重放同一发布和 AssetVersion，冲突候选或目标显式失败。事务失败时资产行整体回滚，审核与选择事实、全部候选 Artifact 及其 Resource 保留；成功与失败事件均分配连续 sequence，再由 SSE 分别投影为确定性的 Artifact 完成或失败 Item。视频候选不通过项目资产发布端口，仍等待真实装配 Runner 产出最终视频 Resource。
- Asset Specialist 只能提交严格的 `asset_binding.v1`。确认服务会重新核验审核 revision、当前阶段版本、成功 Asset Specialist 身份、精确 `script_bundle` 上游，以及每个匹配 Resource 的账号或团队归属、就绪状态和媒体类型；只有显式 `confirmed=true` 的绑定才追加不可变 `agent_asset_binding_revisions`。未确认、跨归属、跨阶段、非就绪或类型不符统一以稳定错误拒绝，不能凭名称推断、使用默认素材或静默跳过。
- `vision.analyze` 只接受当前完整作用域内就绪图片 Resource 对应的精确 Artifact head revision。模型作出决策后、进入付费审批前，Runtime 会把来源 revision、Resource、所选可调用视觉模型记录、`visual_evidence.v1` 交付合同、精确 ToolCall 身份和当时的报价冻结到 ToolCall；来源已过期、模型不可用、外层与参数内交付合同冲突、报价在批准前变化或跨作用域 Resource 都作为可审计修复事实回灌 Agent，且不会创建 Task、BillingOrder 或积分预留。用户批准后才创建唯一的 `internal` 视觉 Task 和冻结账单；执行时临时签发源图 URL 并调用真实视觉模型，签名 URL 不进入 Task、事件或 Artifact。严格结构化结果只能在事务内以仍为 head 的精确上游 revision 追加到 Artifact Ledger；同一 ToolCall 重试复用同一请求与 revision，不同调用不会错误共用付费结果。若来源在供应商分析期间变化，证据写入会显式失败并进入费用核对；来源在证据提交后产生新 revision 时，旧视觉证据显式进入 `stale`，不会被静默沿用。
- `media.generate` 是生产 Specialist 唯一的图片、视频和独立音频付费执行端口。模型只能提交严格的 capability 专属参数和 `media_candidate.v1` 交付 schema；Runtime 在展示费用审批前冻结所选动态模型记录、providerCapabilities、标准化参数、输入 Artifact head revision、就绪 Resource 的 ObjectKey/ETag、Skill 版本、输出身份、价格版本与报价到期时间。定向重生成视频时，模型还必须提交 stale 来源镜头 revision 与用户已批准的精确旧候选 revision，并把同一镜头当前 head revision 放入输入；Runtime 重新核验批准事实、stale 血缘及旧候选对应的成功 ToolCall/Task，再从不可变旧 attempt 推导唯一下一 attempt，当前镜头和未受影响候选保持原 revision、Task 与账务事实不变。批准前不会创建 Task、BillingOrder 或积分预留；批准后才以唯一幂等身份创建现有 `canvas_image`、`canvas_video` 或 `canvas_audio` Task，并沿现有供应商、计费与账务核对链执行。模型、价格、参数或输入资源在批准前发生变化会显式拒绝，禁止改用默认模型或重算后静默继续。Task 成功后，返回的每一个图片、视频或音频 Resource 都以稳定候选身份逐项追加到 Artifact Ledger；候选写入和生产 Artifact 状态推进必须在同一事务中同时匹配 `toolCallId + actionVersion + taskId + attempt + artifactRevisionId + approvalFingerprint`、当前 Run 检查点与活动计划，重复回调只重放同一事实。取消、计划替换或 attempt 被取代后的供应商成功结果仍以 `unadopted` revision 留作账务与审计证据，但绝不推进当前 Artifact head、Run、工具结果或时间线。进程中断可从持久 Task 恢复，任何已成功产出的资产都不会被质检或后处理删除、覆盖或回滚。内部媒体 Task 的终态会按 Run 身份唤醒同一个事件驱动协调器，完成工具结果后再由交付 verifier 决定下一步。
- 视频、音频与最终装配采用三份严格计划产物：`video_plan.v1` 冻结逐片段模型事实、上游 revision 与 `native | independent | none` 音频模式；仅在用户明确要求独立配音、旁白、音乐或音效时才允许 `audio_plan.v1`，并逐条冻结 voice、line、timeline、上游 revision、独立报价与候选身份；新 Run 只写 `assembly_plan.v2`，其中必须显式给出有序片段、裁剪、转场、独立音轨、混音参数、输出规格和精确上游 revision，且只可引用已批准、当前、就绪的视频或独立音频 revision。`native` 只在冻结的动态模型能力明确支持生成音频且用户接受时使用，声音随视频候选交付，不创建独立音频 Task、Artifact 或重复账单；`none` 不隐式补建音频。用户批准装配 revision 后，`media.assemble` 以确定性身份创建一个 `internal`、无 BillingOrder 的本地装配 Task；worker 使用冻结输入执行真实 FFmpeg 拼接/混音，校验输出后写入带校验和的 ready Resource 和最终 Artifact revision，再由 `canvas.project` 提交真实画布 revision。最终链路固定为“用户请求 → Specialist → 已批准 revision → 内部装配 Task → ready Resource → Artifact/Canvas revision → timeline/SSE → delivery verifier”；计划、Task completed、Specialist completed 或说明文字都不是成片证据。用户停止或拒绝会终止当前执行链并取消可取消 Task；供应商或本地进程已产出的迟到结果仍作为 `unadopted` Resource/revision 保留用于审计，但不会复活 Run、推进 Artifact head 或伪造完成态。

旧 Runtime 的切换工具只承担显式运维审计与安全退役：先按 runtime version/status 审计非终态 Run，并同时暴露待审批、待执行/执行中 Tool、内部及旧媒体 Task、活动媒体 Artifact、未结 Billing 与 Checkpoint 问题；只有 Production schema v2 marker 已安装、Checkpoint 可终止且不存在运行中工具、活动 Task、活动 Artifact 或未结账单时才允许退役。旧媒体事实通过 `created_by_run_id` 的计划版本继续追踪到 Task 与 Billing，不能因 operation 或账单幂等键不带 Agent run 前缀而漏审。退役事务追加稳定的 `runtime_schema_retired` 事件、Checkpoint 与失败时间线并中断可终止 Tool；非终态 v3 不能恢复或继续执行，终态 v3 历史与 `assembly_plan.v1` 原样只读，重复执行不产生第二份事实。数据库迁移失败会阻止服务进入新版本，发布回滚使用升级前的数据库恢复点和同一镜像摘要恢复，不在运行时增加兼容分支；用户入口、模型决策解析和新 Run 创建均固定到 v4/v5。

#### 当前通用运行事实

- 运行作用域固定绑定租户、用户、项目、画布、会话和运行记录；每次工具执行都会重新读取真实画布权限，不信任浏览器缓存的权限声明。
- `AgentRunEvent` 是追加式审计真源，`AgentTimelineItem` 是与 Run、Event、Checkpoint、ToolCall 同事务维护的查询投影。Run 初始化固定写入 `run.created`、`user.message`、初始 Checkpoint 和已完成的用户消息 Item；Item 使用确定性身份、连续 ordinal 与唯一来源 sequence，任何投影冲突都会回滚整次状态迁移，禁止以不完整时间线伪装成功。
- Agent 文本模型仍通过耐久 Task 保留队列恢复、计费、供应商请求与审计事实，但 Task 受众固定为 `internal`；普通用户任务列表、详情、日志、取消和重试接口只查询 `customer` Task，管理员、账务与 Runtime 协调器继续按权限读取内部事实。媒体 Task 仍保持客户可见。
- 所有 Task 在入队前都会签发规范化 HMAC-SHA256 执行信封，精确绑定租户、请求用户、项目、画布、Run、Artifact revision、Task 身份/类型以及 Prompt 与输入摘要；worker 必须先从数据库重读并匹配 `leaseOwner + leaseGeneration + leaseToken`，再校验信封和账务作用域，任何缺失、篡改、过期、跨作用域重放或未知字段都会在供应商副作用前显式失败。取消意图、租约代际和供应商请求身份均为耐久事实，进程重启后仍会使旧 worker 失去终态写入资格；已取消任务的迟到成功只能由独立核对租约保留为未采纳资产和待核对费用，不能覆盖活动 Artifact head、复活 Run 或重复退款。
- 供应商成功/失败、Task 终态、BillingOrder 结算或 `uncertain` 审计、Session/Message/Result 与 Agent 唤醒 Outbox 在同一数据库事务内提交；事务任一侧失败时不暴露半完成终态。事务提交后才由带独立租约和幂等键的 `task_outboxes` 投递 Run 唤醒，崩溃、重复消费或进程重启只会重放同一终态事实；投递错误保留尝试次数、最近错误和下次可用时间，后台 worker 持续恢复，SSE 与时间线只展示已经持久化的 sequence 事实。
- Agent Chat Completions 固定使用供应商 `text/event-stream` 与 `stream_options.include_usage=true`。Run 在等待 worker 时保持 `queued`；worker 真正发起首个供应商请求前先以 CAS 持久化 `queued -> running` 和 `run.status_changed`，SSE 将该事实投影为 `state.snapshot`，因此 Web 在首个可见增量前以可展开真实事件轨迹显示“思考中”，首个增量到达后收起为“已思考”并在下方同一无标题正文持续增长。服务端只从严格 `ModelDecision` JSON 中释放已确认属于顶层 `kind=final` 的 `final.message` 增量；`reasoning_content`、工具参数、澄清结构和交付合同不进入用户正文。每段可见增量与助手 Item 同事务写入连续 sequence，再由 UI protocol v5 SSE 断线补发；任务断线重领会以新的 `item.started` 原子重置同一消息，协议截断或最终决策无效则追加 `agent.message_failed` 并保留已流出正文。Web 始终携带最后持久化 `afterSequence` 重连，先按 sequence 丢弃旧事件再更新 Timeline 和副作用，避免刷新、断网或重复推送造成卡片重复。审批卡严格展示服务端冻结的影响、报价和到期时间，并只提交同一卡片的精确提案身份；React 只把 `agent_message` 按 `itemId + sequence` 增量维护为同一个助手气泡。不存在完整响应回退、旧生产卡兼容、模型降级或前端假打字。
- 工具、审批和结果共用同一个 `tool_call` Item 生命周期：等待审批、已批准、已开始、完成、失败、拒绝或中断都更新同一身份，不创建并行的“执行中”残留项。运行中的追加指令以 `clientRequestId` 持久化并幂等去重；Agent 活动期间 Composer 的发送按钮原位切换为停止按钮。用户点击停止时，Web 立即对当前 Run 建立事件闸门并使排队或在途状态刷新失效，不等待中断接口返回才停止正文增长；服务端再以 `stateVersion` CAS 进入终态，并在同一事务的消息投影写入边界拒绝终态 Run 的迟到 `model.delta` 与 `agent.message_failed`。停止接口会立即取消同一实例正在执行的内部模型 Task；其他 worker 实例上的模型请求会以有界间隔观察持久 Run 检查点并取消真实供应商 HTTP 流，即使供应商尚未发送可见正文也不会继续跑完整请求。取消后的内部 Task 进入 `cancelled`，供应商费用事实不足时账单保持 `uncertain` 等待核对。已经提交的媒体任务不被回滚，迟到成功结果追加独立 `artifact.available` 事件和 Artifact Item，保持 Run 终态及 Checkpoint 不变。
- 管理后台的“Agent 任务”只对管理员开放，跨用户列出未结束 Run，并在分页前按服务端权威状态、活动分类、用户和运行/项目/画布作用域筛选。列表与详情只返回 Run 身份、状态版本、停滞时长、等待类型、关联模型/媒体 Task、账务、供应商请求和控制处置事实，不返回用户提示词、助手正文、工具参数或凭据。`active`、`awaiting_user`、`possibly_stalled` 由服务端根据持久状态和更新时间分类，Web 不按本地时钟重新猜测。
- 管理员终止严格接收最新 `stateVersion`、4–500 字符审计原因和详情接口签发的确认短语。事务以 Run 行锁与 CAS 原子写入 `run.interrupted`、终态 Checkpoint、活动助手/工具/Artifact 时间线和关联 Task/Billing 处置；重复或过期提交返回稳定 409 及最新 Run，Web 保留原因但清空确认，要求管理员按最新事实重新确认。尚未提交供应商的排队任务会取消并退回预留积分；已经提交的任务只停止 Agent 继续推进、请求供应商取消并进入可恢复账务核对，不能宣称供应商计算已经停止。无法与活动任务安全对应的未决账务会阻断终止，避免删除成本事实或重复退款。
- 时间线、生产计划和迟到资产的每一次读写都重新校验租户、操作者、项目、画布、Thread 与 Run 所有权；时间线只保存已鉴权的 `resourceId` 和媒体元数据，不保存会过期的 OSS 签名地址。
- `stateVersion` 独立承担审批、工具结果和恢复操作的并发控制，`stepNumber` 只在模型作出下一次决策时递增，避免工具恢复被重复计费为模型步骤。每个新的模型步骤都会从当前 Run 的成功工具、真实 Resource、画布 revision 与 Artifact Ledger 重新构造累计 `deliveryEvidence`，再对冻结的 `expectedDelivery` 生成 `deliveryVerification`。最终媒体交付不能只凭 URL 或 Specialist 完成态：`artifact_revision` 必须指向当前作用域内精确已批准的候选 revision，`resource` 必须同时证明对应 Resource 已就绪；用户要求进入资产库时还必须存在同一 revision 的成功 `publication`，要求落画布时必须存在真实 `canvas_revision`。缺少任何事实都会以对应 criterion 显式返回，已满足的 criterion 不得重复执行；只缺 `final_message` 时 Agent 必须直接完成回复，避免已交付媒体后再次规划、审批或生成。
- Runtime 只有一个事件驱动协调入口：run 创建、模型任务终结、审批决定和媒体任务终结都会以持久化事实唤醒同一协调器；协调器在有界转换次数内继续免费工具或创建唯一下一模型任务。worker 不再每 5 秒扫描并盲推全部运行，只按分钟检查超过恢复阈值且仍未终结的 run，并使用跨实例互斥与游标恢复进程中断后的事件。
- 信息确实不足时，模型可返回严格的 `clarification_request`，由 Runtime 在同一 Run 冻结交付合同和 1–3 个结构化问题，并切换到不占用 worker 的 `waiting_input`。问题类型只允许单选、多选和自由文本；逐题保存只写 checkpoint/event，不消耗模型 Token，也不创建工具、媒体任务、账单或积分冻结。最终提交使用 `stateVersion` CAS 将 pending 问答原子追加到不可变 history，产生 `clarification.responded` 后只唤醒一次同一协调器；下一模型步骤读取原始结构化问答继续执行，不创建第二条 Run。完全相同重放不增加事件或任务，并发提交只有一个事务可推进。
- 追问内容由 Agent 基于本轮真实事实自主决定，本地仅校验问题/选项/答案的类型、数量、身份、长度和权限，不做关键词路由、默认答案或语义改写。`requestId` 复用、回答冲突、过期版本、非法问题身份与非等待态提交都会返回稳定错误码及最新状态版本；用户显式忽略的问题以 `skipped=true` 保留，禁止补写推测答案。
- 首个模型决策必须声明结构化 `expectedDelivery`，Runtime 将其冻结为整条 Run 的不可变交付合同；后续工具调用和最终答复必须保持一致。工具失败、审批拒绝或事实不足不能把图片、视频或画布交付降级成文字回答。审批拒绝会记录 `approval.decided` 和失败的 `tool.result`，清除 pending proposal 后回到 `running`，由 Agent 基于这项事实重新规划；拒绝本身不取消 Run，也不授权任何替代动作。
- 工具调用按 `runId + toolCallId + actionVersion` 冻结并幂等登记。Tool schema v6 只接受六个原子能力；只读能力不需要审批，画布写入、资产发布与付费媒体生成分别按写风险或付费风险形成精确提案。提案批准前不执行副作用，已完成调用在恢复时不会重跑。
- `canvas.apply_ops` 是当前唯一画布写能力，必须携带权威 `canvasId`、`baseRevision`、稳定 `clientMutationId` 和结构化 operations；不存在 `canvas.project` 兼容映射。`assets.publish` 与 `media.generate` 同样只消费各自严格契约，缺失或冲突事实显式失败，不从 Prompt 猜测字段、不补默认模型或媒体参数。
- `media.generate` 的批准提案冻结动态模型记录、标准化参数、输入 Resource、价格版本、预估积分、过期时间和幂等身份。批准后才以稳定 Task/BillingOrder 身份进入既有队列；排队或执行中工具保持 pending，同一 Task 终态 Outbox 再唤醒同一 Run。成功回执必须同时重读 succeeded Task、settled BillingOrder 和所有归属正确的 ready Resource，并返回 Task ID、BillingOrder ID、Resource ID 与同源站内资源地址。失败或取消只在账单已 refunded 时返回终态失败；结算不确定显式返回 `media_settlement_uncertain`，禁止第二次扣费尝试或暗中换模型。
- `assets.publish` 只接受已归属当前个人/团队作用域且状态为 ready 的 `resourceId`，不接受 URL。发布时会重新核验画布与项目编辑权限、当前 Run 及准确批准提案，再原子写入资产、版本、项目链接和 Resource 表示层。同一资源在同一项目只发布一次；完全一致的重试重放同一 Asset，重用幂等键指向不同资源或以不同名称重新解释旧资源都显式冲突，事务内不保留半成品。
- 个人与团队画布统一使用同一条 WebSocket revision/CAS 增量变更通道；浏览器加载后必须先同步服务端权威快照，再提交本地差异。`PUT /api/canvas-projects/:id` 只负责首次创建远程画布，已存在项目一律拒绝整页覆盖，防止浏览器旧快照覆盖 Agent、协作者或其他设备刚提交的节点。
- Web 在启动 Agent Run 前先将当前画布未提交变更收敛到权威 revision；首次权限预检只建立远程基线与权限事实，不覆盖 WebSocket 建连前的本地编辑。`canvas.apply_ops` 的成功时间线项只发布严格的服务端回执，包括审批提案 SHA-256、基础与已提交 revision、幂等 mutation 身份、已执行 operation 身份及结构化变更证据。Web 不再执行或确认节点/连线写入，只在完整回执通过严格解析后等待已鉴权协作查询读到该 committed revision，再更新远程快照与纯 UI 选区；退役工具、错误画布、畸形回执和过期查询均不会触发客户端副作用。后端和 Web 只接受 UI protocol v5；旧协议与退役生产事件原地失败，不兼容映射。
- 每个新工具动作必须使用未出现过的 `toolCallId + actionVersion`；模型误复用历史身份时，Runtime 记录显式 `tool_identity_reused` 修复事实并继续同一执行链，不会再次写入冲突记录。同一工具以语义相同的 JSON 参数连续返回相同错误码与相同结构化失败证据时，首次错误允许 Agent 根据事实修正，第二次直接以该错误终结 run；错误原因已经变化时继续留给 Agent 修正，禁止误判为死循环。最后一个模型步骤不得再开启新工具调用；历史运行若已进入该状态，拒绝或完成工具后会保存结果并以 `step_budget_exhausted` 明确终结。图片生成公共契约要求 `size` 精确取自本轮冻结模型的比例候选、`resolution` 精确取自分辨率候选、`count` 精确取自数量候选；`quality` 仅在动态 `providerCapabilities.qualities` 发布非空候选时必填且只能取其中之一，候选为空则必须省略。Runtime 在报价前把比例与分辨率冻结为供应商实际像素尺寸，禁止默认画质、默认分辨率或其他未知字段绕开正式任务和计费契约。
- 模型每轮只接收当前用户真正可调用、已定价且凭据健康的模型事实。Skill 目录由 HMaigc 自有数据库和随版本发布的 `SKILL.md` 建立；目录、版本号与 SHA-256 在 Run 中冻结，`skills.load` 只接受本轮已授权且仍为已发布状态的精确版本和校验值，核验通过后才返回完整 instructions；不会回退旧 `skill.load`。
- 每次新 Run 固定冻结 Runtime v5、Policy v5、Tool schema v6、UI protocol v5。Runtime v4/v5 工具图与更早版本只读保留历史事实，非终态旧 Run 不自动接管，任何恢复或写操作均不得把旧决策映射到 v6。
- Agent 模型调用统一声明 Chat Completions 的 `response_format=json_object`，使不同文本模型共用同一严格决策契约。结构无效时，Runtime 保存 `model_decision_invalid` 诊断并终结本 Run；不会抽取文本、伪造默认决策、自动换模型或在本地拼补语义。
- 参考图片只接受当前账号已就绪的图片 Resource；服务端冻结资源 ID、显示名称、MIME 与尺寸，拒绝浏览器 Blob URL、跨账号资源和失效资源。执行模式是必填运行事实，幂等重放若更换模型、Skill、附件或模式会显式冲突，禁止静默采用新配置。
- 正式传输入口为 `GET /api/agent/threads?canvasId=...&limit=...`、`POST /api/agent/threads`、`POST /api/agent/threads/:threadId/runs`、`GET /api/agent/runs/:runId`、`GET /api/agent/runs/:runId/events?afterSequence=N`、`POST /api/agent/runs/:runId/steer`、`POST /api/agent/runs/:runId/interrupt`、`POST /api/agent/runs/:runId/approvals` 和 `POST /api/agent/runs/:runId/clarifications/:requestId/responses`。追加指令严格接收 `clientRequestId/message/expectedStateVersion`，停止严格接收 `expectedStateVersion`；工具审批严格接收 `toolCallId/actionVersion/proposalHash/decision`，未知字段、尾随 JSON、非小写 SHA-256 哈希或非法决定全部返回 `agent_approval_invalid`。全部工具结果仍只由服务端执行器写入，浏览器不提交工具结果事实。历史查询按当前用户、租户与画布返回最近 20 个 Thread，在校验每个 Run 最新 Checkpoint 与 Run 状态一致后，为每个 Thread 返回按创建时间排列的完整 Turn 与 Item；缺 Item 的旧终态 Run 只能从不可变事件日志构造只读投影，旧活动 Run 缺投影时显式失败。SSE 只把已持久化领域事件纯投影为协议版本 `5` 的 `AgentUIEvent`，以持久 sequence 断线补发；工具、审批和澄清持续更新稳定 Item 生命周期。补发时必须根据不可变事件 payload 与上一版本 Checkpoint 重建事件发生时的快照，再与当前材料化 Item 的身份、作用域和序号单调性核验，禁止用 Item 最新状态覆盖历史事件。未来游标、未知/退役事件、Item 作用域冲突、非法 payload 或投影失败均返回稳定协议错误。
- 服务端历史是会话发现与跨设备恢复的权威来源；浏览器 `localForage` 只保存当前 Thread、活动 Run、事件游标或尚未确认的 `clientRequestId`，用于快速恢复和启动幂等，不构成会话事实。没有本地句柄时采用服务端最近活动会话；选择旧会话只切换观察和恢复目标，不取消服务端仍在运行的 Run。
- 浏览器不再调用 Agent 模型、不拼 system prompt、不维护 tool loop，也不再创建固定影视 Session。旧会话事实仅保留在历史项目数据中用于审计，不进入新运行链。

### Agent 模型计费链路

- 网站 Agent 的每次模型请求分别创建计费订单；工具调用本身不计费，图片、视频等媒体生成继续使用各自独立订单。
- Agent 模型任务创建前若确认账号余额不足或团队月额度耗尽，运行会以 `insufficient_credits` 或 `team_credit_limit_reached` 明确终结，不创建下一任务或账单，也不会由后台驱动器无限重试；并发与暂时性额度错误仍保持原有显式错误语义。
- 托管的筷子 DeepSeek 模型使用 `token_usage`：系统代理和服务端 Agent Runtime 共用同一预留/结算内核；`expectedOutputTokens` 只表达运营成本估算中的平均输出量，`maxOutputTokens` 独立承担单次请求硬上限和最坏成本预留。请求发出前按后台发布的输入/缓存命中/输出单价和最大输出 Token 原子预留积分，并把同一最大输出值写入真实请求，同时冻结当时的服务地址版本和凭据版本；首版倍率固定为 1.0。升级迁移只会把历史 `expected_output_tokens` 一次性复制到新增上限字段，保留原估算事实，管理员随后可分别维护两者。
- 筷子 Kling 3 Omni 的普通无声、普通有声和参考视频价格按后台能力目录分别配置：`std/pro` 可发布同步音频档位，`4k` 只允许无声普通生成，参考视频只允许 `std/pro` 且禁止同步音频。画布参数规范化、Agent 渲染门禁、供应商请求和计费档位消费同一份分辨率能力事实，不会把不受支持的组合发送到上游或隐式改用其他档位。
- 筷子 GPT Image 2 的后台价格矩阵由模型动态能力目录展开为 `1K/2K/4K × low/medium/high` 九个必配档位。普通画布询价、Agent `media.generate` 冻结报价、Task/BillingOrder 预留与结算、供应商调用审计和成本统计都使用同一个 `resolution + quality` 规格身份；任一参数缺失、组合未发布或价格未配置都会在供应商请求前显式失败，不再把画质映射成分辨率，也不使用默认档位。
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

生产服务器必须安装 Docker Engine 与 Docker Compose。正式生产只有“受保护 Git 标签 → GitHub Actions → 严格 SSH 主机校验 → 版本化 bundle → `deploy/hmaigc.sh`”这一条发布写路径；服务器不执行 `git pull`、不现场编译，也不运行独立运维控制器。

业务配置保存在部署根目录的 `shared/production.env`，动态版本和回滚事实保存在独立部署状态目录。GitHub `production` Environment 必须配置审批、专用部署用户、SSH 私钥、预置 `known_hosts` 行、端口和绝对部署根目录。Backend/Web 镜像只接受 `repository@sha256:<digest>`，不使用 `latest` 或其他可变标签。

发布由正式标签自动触发；Actions 完成测试、版本化 Web 程序资源上传与 CDN 验证、同一 `dist` 的镜像构建和摘要解析后，把经过 SHA-256 校验的 bundle 上传到服务器，并由脱离 SSH 会话的主机 release runner 执行。日常升级不在管理后台或服务器命令行创建第二条任务；手工命令只用于基于已验证 bundle 的受控应急。生产入口应由 Caddy、Nginx 或云负载均衡器提供 HTTPS，后端、PostgreSQL 和 Redis 不得直接暴露公网。

业务后端不持有 Docker socket，也不能重启自己。主机事务脚本在在线态与停写态分别运行目标后端镜像自带的只读 Agent Runtime 升级审计；审计失败不会带病迁移。审计、同一恢复点备份和目标版本验活全部通过后才原子提交新版本状态，失败则按升级前镜像摘要与已校验恢复点恢复。完整安装、首次硬切、发布与应急契约见 [源码驱动生产发布说明](deploy/README.md) 与 [生产运行手册](PRODUCTION.md)。

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
- 升级前先在隔离副本完成恢复演练并保留既有安全恢复点；正式升级中的目标镜像双阶段只读审计通过后，执行器才为 PostgreSQL 与 `backend-data` 创建同一升级恢复点。新后端完成原子迁移和版本验活后才允许启动 Web。
- 禁止把 `.env`、数据库、日志、备份、上传文件或真实密钥提交到仓库。
- 页面路由名称不得复用于 `web/public` 静态目录；画布人脸模型统一位于 `/runtime-assets/canvas-models/`，避免静态目录抢占 `/canvas` 页面路由。

## 许可证与上游

本项目基于 `ddcat-ai/open-ai-canvas` 与 `basketikun/infinite-canvas` 继续开发。上游署名和版权信息保留在 [NOTICE](NOTICE) 中。

当前代码采用 [GNU Affero General Public License v3.0](LICENSE)。通过网络向用户提供修改后的程序时，需要履行 AGPL-3.0 对应的源码提供义务；正式商业上线前应由法务确认开源合规、第三方模型条款和素材版权。
