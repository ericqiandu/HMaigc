# HMaigc 多专家 Agent 分阶段短片生产设计

**日期：** 2026-08-27

**状态：** 已确认，进入实施计划

**承接规格：**

- `2026-08-17-agent-production-runtime-design.md`
- `2026-08-22-agent-conversation-foundation-design.md`
- `2026-08-23-agent-native-streaming-chat-design.md`
- `2026-08-23-agent-production-deliverables-design.md`

**参考实现：** HKUDS/ViMax，固定提交 `05a48943878312d88fe5a016c12a9654940ecc43`，MIT License。

## 1. 业务目标

把当前单 Agent 媒体生产闭环硬切为“一个用户会话、一个主编排 Agent、多个按需专家 Agent、多个必须审核的生产阶段”。用户可以用一句话开始短片创作，但系统不得把“一句话开始”误解成“一次授权到底”：每个阶段先交付可审阅产物，用户可以持续要求修改，只有确认当前 revision 后才允许进入下一阶段。

完成后的产品体验必须满足：

1. 用户始终在同一个 Agent 对话中协作，不需要理解内部 Agent、Task、队列或供应商调用。
2. 主 Agent 根据真实上下文、已发布 Skills、工具结果和当前项目状态动态选择专家，不由前端或后端关键词路由固定工作流。
3. 剧本、角色表、素材绑定、分镜脚本、角色图、分镜图、视频及可选独立音频都能先在对话中审核，并与画布引用同一份持久事实。
4. 用户确认的角色图、场景图、道具图和独立音频可以由 Agent 幂等发布到项目资产库。
5. 图片理解、角色一致性、镜头语言、Camera Tree、首帧—运动—尾帧和连续性检查成为平台原生能力。
6. 所有付费媒体执行继续经过冻结报价与用户批准；停止、拒绝、断线、重启和迟到回调不造成重复扣费、资产丢失或伪成功。
7. 现有真实流式、Task audience、BillingOrder、Resource、画布 revision/CAS、Artifact Ledger 和 Delivery Verifier 保持唯一商业真源。

## 2. 非目标

- 本阶段不重做 Agent 对话框视觉，不进行 Liblib 风格的一比一 UI 改造；只定义必要的时间线卡片和数据契约。
- 不引入 ViMax 的 Python/LangChain Runtime，不建立第二套 checkpoint、工具、计费或资产状态机。
- 不把 ViMax 的固定 Agent 顺序或 Prompt 原文固化到 Go 后端。
- 不允许主 Agent 一次性跨过所有审核阶段，也不允许自动模式跳过阶段审核或付费确认。
- 不把独立音频设为所有视频的必经步骤；模型原生支持声音时优先使用同一个视频任务交付音轨。
- 不展示或持久化供应商原始 `reasoning_content`，不把内部文本 Task 暴露给普通用户。
- 不增加模型自动降级、非流式回退、默认工作流、默认素材、模板产物或语义关键词兜底。

## 3. 方案选择

采用“动态语义编排 + 固定商业门禁”。

```text
React 单一 Agent 对话 / Canvas
          <- persisted timeline + sequence SSE
Go Agent Application Runtime（唯一商业 Runtime）
  |- Main Orchestrator Agent
  |- Specialist Runtime / Registry
  |- Dynamic ProductionGraph
  |- Artifact Ledger + immutable revisions
  |- Stage Review Gate + Cost Approval Gate
  |- Billing / Task / Resource / Canvas CAS
  `- expectedDelivery -> evidence -> verification
          |
Skills（方法论） / Tools（确定性动作） / Providers（真实模型）
```

不采用固定 ViMax 流水线，因为用户会反复修改剧本、替换素材、跳过不需要的媒体类型，并且模型能力来自动态目录。也不采用完全自治的多 Agent 群，因为权限、资金、资产所有权和阶段审核必须有单一责任主体。主 Agent 拥有语义编排权，商业 Runtime 拥有所有副作用和强一致事实。

## 4. 统一语言与职责边界

### 4.1 Agent、Skill、Tool

- **Agent**：承担语义判断、证据规划、专业产物设计和修订建议。
- **Skill**：按需加载的版本化方法论，不拥有权限、资金或副作用。
- **Tool**：权限可验证、输入可冻结、结果可审计的确定性动作。

不得把一个创作方法论做成拥有数据库写权限的“万能 Agent”，也不得把专家 Agent 降级为固定 Prompt 模板。专家只能在已冻结的模型、Skill allowlist、Tool allowlist 和最小上下文中运行；写入、计费、供应商提交和画布操作必须通过现有工具执行层。

### 4.2 ProductionGraph

`ProductionGraph` 是主 Agent 基于用户目标和已确认事实形成的动态生产图，不是前端固定步骤条。图中节点代表生产阶段或交付物，边代表真实依赖。Runtime 只校验类型、依赖、权限、revision 和状态，不通过本地关键词决定应创建哪些节点。

每个图版本不可变。用户修改上游产物时，主 Agent 创建新图版本或新 Artifact revision；依赖旧 revision 的下游节点统一标记为 `stale`，不得静默继续。

每个图节点显式冻结 `reviewPolicy`、`costPolicy`、输入 revisions 和期望交付。首期所有用户可见生产阶段的 `reviewPolicy` 都是 `required`；需求理解、上下文裁剪和 Skill 加载等内部步骤不制造额外审核阶段。

### 4.3 Artifact Revision

所有可审核内容都是 Artifact：文本、结构化计划、图片、视频、音频、视觉证据和最终成片。Artifact 是稳定逻辑身份，Artifact Revision 是不可变内容快照。对话卡片、画布节点和资产库记录只能引用 `artifactId + revisionId`，不得复制成互不关联的三份内容。

## 5. Agent 体系

### 5.1 Main Orchestrator Agent

主 Agent 是用户会话的唯一语义负责人，负责：

- 理解用户目标并确定当前应推进的 ProductionGraph 节点。
- 按需选择专家和 Skills，冻结调用配置。
- 将已确认 Artifact、项目事实和工具证据裁剪成专家最小上下文。
- 综合专家结构化输出，决定提交审核、继续修订、请求澄清或调用工具。
- 在最终输出前执行交付自检；Delivery Verifier 不满足时在同一执行链内继续修正或显式失败。

主 Agent 不直接执行 SQL、扣费、供应商请求或 React Flow 变更。

### 5.2 核心专家

第一阶段包含五个核心专家：

1. **Narrative Agent**：创意理解、完整剧本、角色/场景/道具/声音角色目录及剧本修订。
2. **Asset Agent**：理解素材需求、搜索项目资产库/当前画布/用户上传、建立候选匹配与资产绑定 revision。
3. **Storyboard Agent**：把已确认剧本与绑定素材转为镜头计划、镜头语言、对白、声音描述和镜头依赖。
4. **Visual Agent**：角色视觉圣经、参考图选择、角色/场景一致性、首尾帧和生成结果视觉审查。
5. **Video & Assembly Agent**：根据已确认镜头和真实前置资产规划视频生成、原生音轨、片段排序与最终合成。

### 5.3 可选 Audio Agent

Audio Agent 不是固定阶段，只在以下语义条件下由主 Agent 建议并在需要时询问用户：

- 用户明确要求独立对白、旁白、音乐、环境音或音效。
- 需要可复用角色声线、精确发音、精确时间轴或后期混音。
- 当前选定视频模型的动态能力事实不能满足已确认的声音交付。

如果视频模型原生支持用户需要的声音和对白，就由视频任务同时交付音轨，不重复创建音频任务。无法确定是否需要独立音频时必须询问，不得默认增加成本。

### 5.4 Specialist Run 契约

每次专家调用形成持久 `SpecialistRun`，至少冻结：

```text
parentRunId / specialistRunId
specialistKey / specialistVersion
parentModelRecordId / parentModelKey
loadedSkillVersions[]
toolSchemaVersion / toolAllowlist
inputArtifactRevisions[]
expectedOutputSchema / expectedDelivery
status / errorCode / startedAt / completedAt
```

子 Agent 必须继承父 Agent 本轮实际模型配置。继承失败直接返回 `specialist_model_inheritance_failed`，禁止回落到默认或备用模型。专家结果必须通过严格结构校验后才能成为 Artifact revision；原始 reasoning 不进入用户会话或 Artifact 正文。

## 6. 分阶段用户流程

### 6.1 需求理解与剧本审核

需求理解是主 Agent 内部工作，不单独制造“需求分析报告”。第一份用户可见产物必须是完整剧本与角色目录。用户可以在同一会话中持续要求修改；每次修订创建新 revision，旧 revision 保留。只有用户确认的剧本 revision 才能成为下游依赖。

### 6.2 素材理解与绑定审核

Asset Agent 从确认剧本提取角色、场景、道具和声音角色需求，并只从当前租户/项目允许访问的资产库、画布和用户上传中搜索候选。对话展示绑定表：

- 已匹配
- 缺失
- 冲突
- 多候选待选择

用户可以选择已有资产、上传素材、修改描述或批准生成新资产。生成付费媒体前展示模型、参数、数量和冻结报价。用户确认绑定后创建不可变 `AssetBindingRevision`；在确认前不得猜测绑定，也不得开始依赖这些资产的分镜图或视频。

### 6.3 分镜与视觉审核

Storyboard Agent 依据确认剧本和 AssetBindingRevision 生成可审阅镜头计划。Visual Agent 补充角色视觉圣经、Camera Tree、参考图关系和一致性约束。文本分镜先在对话审核；付费分镜图创建占位画布节点和媒体卡片，再逐项报价、批准和生成。

### 6.4 视频与可选音频

视频生成只能消费真实已确认上游 Artifact revision 和真实资产 URL。模型能力从系统 Model Catalog 动态读取，不能在 Agent 或 Web 写死：是否支持原生音频、对白、角色声线参考、口型同步、分辨率、时长和参考素材类型都是冻结报价的一部分。

独立音频生成时创建 `AudioArtifact`，可在对话试听、审核、重新生成、绑定角色/台词/时间轴，并在确认后发布到项目资产库。视频自带音轨默认随 `VideoArtifact` 保存；只有用户明确选择提取时才创建独立音频 Artifact。

### 6.5 最终合成

最终合成只消费已确认的视频、可选独立音频和装配计划。交付完成必须有最终 Resource、当前画布 revision 和对应 Artifact revision 证据；不能因为“专家已完成”或“存在说明文字”宣布成片完成。

## 7. 阶段状态机与审批语义

生产阶段状态统一为：

```text
planned -> running -> awaiting_review -> approved -> completed
                  \-> failed
                  \-> stopped
awaiting_review -> running（要求修改，创建新 revision）
approved/completed -> stale（上游确认 revision 被替代）
```

- `awaiting_review` 表示已有真实可审阅 Artifact，不表示完成。
- `approved` 只表示精确 Artifact revision 已被有权用户接受；`completed` 表示该批准要求的确定性发布、绑定、投影或阶段收口事务也已提交。没有额外副作用的阶段可以在同一事务中从 `approved` 进入 `completed`。
- “要求修改”继续当前会话和 ProductionGraph，不等同于拒绝付费审批。
- “拒绝费用”或显式“停止”终止当前 Agent Run 及其所有未终结子 Run，不再创建新模型步骤、工具或 Task。
- 已提交供应商的任务如果不能物理取消，回调、账单、Resource 和 Artifact 继续保存；返回资产标记为“终止后返回、未采用”，不得删除。
- 阶段确认只批准精确 Artifact revision；上游内容变化后旧批准自动失效。

### 7.1 手动与自动模式

两种模式继续保留：

- `guided`：读取和加载 Skill 可自动执行；写入、生产提交和付费任务依现有风险等级审批。
- `automatic`：可以在当前已批准阶段内部自动完成无费用规划和确定性写入。

两种模式都必须经过阶段内容审核，且所有付费媒体必须经过费用审批。自动模式不能跨阶段替用户接受剧本、角色、素材、分镜或成片。

## 8. ViMax 能力的产品化吸收

HMaigc 不复制 ViMax Runtime，而是把以下经过验证的方法沉淀为版本化 Skills 与结构化 Artifact schema。

### 8.1 Character Visual Bible

角色结构至少包含：

- 稳定身份、别名及故事内映射
- `staticFeatures`：脸部、发型、体型等相对稳定特征
- `dynamicFeatures`：服装、配饰、携带物及场景变化
- 确认参考资产 revisions
- 声音角色与可选声线资产
- 明确未知项和冲突证据

ViMax 的 `CharacterInScene`、`CharacterInEvent`、`CharacterInNovel` 分层思想可转化为项目级、场景级和镜头级覆盖关系，但不得自动补写剧本未提供且会改变人物设定的事实；需要创作补全时必须标记为专家提案并交用户审核。

ViMax 的 novel → event → scene → shot 信息拆解可以作为 Narrative/Storyboard Skills 的证据组织方法，但不能变成后端固定步骤。短创意可以直接形成单场景剧本，长文本才由主 Agent 决定是否委派压缩、事件提取和场景提取。

### 8.2 电影化镜头语言

每个镜头 revision 应结构化保存：叙事目的、景别、机位、角度、构图、画面位置、人物视线、轴线/屏幕方向、镜头运动、画内动作、对白、声音、时长、转场、可见角色与依赖资产。

### 8.3 First Frame — Motion — Last Frame

视频镜头拆为：

- 纯静态首帧状态
- 摄影机运动与画内运动
- 动作完成后的纯静态尾帧状态

首尾帧不得包含“正在发生”的模糊动作。尾帧必须是首帧与运动的可验证结果，并可作为后续镜头的连续性证据。

### 8.4 Camera Tree

Camera Tree 表达广角母机位、子机位、父镜头、包含关系、反打关系和缺失视角。结构层必须校验：

- camera/shot 引用存在
- 不能自引用
- 图无环
- 一个镜头的依赖可追溯到单一有效根或明确的独立机位
- 父子景别、空间覆盖、轴线和屏幕方向的语义判断由专家完成

HMaigc 只吸收这一结构和方法，不复制源代码中的重复字段、固定尺寸、固定转场 Prompt 或本地文件处理路径。

### 8.5 参考图选择与一致性检查

参考图只能来自已确认 AssetBindingRevision 或明确的上游尾帧。Visual Agent 基于真实图片证据检查角色身份、服装、场景、时间、空间、构图、屏幕方向和帧间连续性。检查结果只能追加为证据、偏差和重试建议；任何已成功生成的资产都必须保留。

如果一次任务生成多个候选，Visual Agent 可以基于结构化证据给出推荐排序，但全部候选都必须入 Artifact Ledger 并向用户提供查看入口。推荐结果不是删除、覆盖或自动采用其他候选的授权。

建议首批治理 Skills：

- `character-visual-bible`
- `storyboard-cinematic-language`
- `camera-tree-continuity`
- `first-motion-last-frame`
- `visual-consistency-review`
- `visual-evidence-analysis`

所有 Skills 必须经过现有来源、许可证、版本、checksum 和 Capability Manifest 治理后发布。复制或实质改编 ViMax 内容时必须保留 MIT 版权与许可声明，并在仓库第三方声明中记录固定来源提交。运行时只加载已发布的 Skills 和当前真实业务事实，不读取本设计文档、临时 ViMax 克隆、`docs/`、`assets/` 或 `ai-metadata/` 作为知识源。

## 9. 图片理解能力

图片理解是共享 Vision Analysis 能力，不新增固定流程 Agent。Asset、Storyboard、Visual、Video & Assembly 和主 Agent 可以在职责范围内调用。

每次分析生成结构化 `VisualEvidenceArtifact`：

```text
sourceArtifactId / sourceRevisionId
characters[] / identityEvidence[]
clothing / hair / stableFeatures
scene / props / spatialRelations
shotSize / angle / composition / screenDirection
actionState / gaze / firstLastFrameConditions
ocrText
uncertainties[] / conflicts[] / confidence
visionModelRecordId / requestIdentity
```

约束：

- 必须读取真实、当前且有权限的资产 URL；连线、Prompt、占位状态不是图片证据。
- 来源 revision 改变后旧视觉证据变为 `stale`。
- 角色一致性必须比较已确认角色参考 revision 与目标图片，不能仅按角色名判断。
- 模型负责语义理解，本地只做 schema、URL、权限、数量、revision 和范围校验。
- 分析失败显式返回原因，禁止使用 Prompt 正文或默认描述伪装结果。
- 如果视觉分析向用户计费，当前阶段必须展示冻结的批次数量、模型和总预计积分并获得批准。

## 10. Artifact 与资产库

### 10.1 新运行时数据模型

新多 Agent Run 使用通用、追加式领域事实：

```text
ProductionGraphVersion
ProductionStage
SpecialistRun
ProductionArtifact
ProductionArtifactRevision
AssetBindingRevision
AssetPublication
```

`ProductionArtifactRevision` 至少记录 kind、schemaVersion、payload、ResourceID、创建 Agent/Run、上游 Artifact revisions、模型请求身份、Skill versions、创建时间和状态。文本 payload 与媒体 Resource 分离；URL 继续只从 Resource 真源读取。

现有 `AgentProductionPlanVersion` 和 `AgentProductionArtifact` 保留历史只读事实。新 Run 不同时写新旧两套生产表；实施时采用明确数据库切换版本，旧非终态 Run 到安全边界后以 `runtime_schema_retired` 终结。

### 10.2 确认后发布资产库

生成图片或独立音频先作为待审核 Artifact revision 展示。用户确认后，主 Agent 通过 `PublishAsset` 端口执行一次幂等事务：

1. 校验租户、项目、Artifact revision、Resource、确认人和当前状态。
2. 创建或复用项目资产库记录，不重复上传对象存储文件。
3. 建立角色/场景/道具/声线绑定及来源关系。
4. 记录模型、参数、Prompt 版本、费用、内容哈希和审计身份。
5. 追加发布成功事件并更新对话/画布投影。

幂等身份使用 `tenant + project + artifactRevision + publicationPurpose`。资产记录、绑定和审计必须同事务成功；失败时保持 Artifact 已生成事实并明确报错，不显示“已入库”。

角色图、场景图、道具图和独立音频的审核卡必须明确展示“确认后加入项目资产库”及目标绑定。用户确认该卡片同时构成当前 revision 的内容确认与 `PublishAsset` 授权，不再弹第二次确认；卡片未声明发布意图或 revision 已变化时不得自动入库。

## 11. 对话时间线与画布投影

现有 `AgentTimelineItem` kind 保持稳定，通过 `contentJSON.type` 表达卡片子类型，避免为每种业务卡建立第二事件协议：

- `agent_status`：当前专家、已加载 Skills 和真实状态
- `artifact_review`：剧本、角色表、资产绑定、分镜文本
- `media_gallery`：角色图、场景图、分镜图、视频和音频
- `stage_approval`：确认、要求修改、停止
- `cost_approval`：模型、参数、数量、报价、过期时间
- `error` / `stopped`：稳定错误码、事实说明和可执行恢复动作

时间线只保存必要摘要与 `artifactId + revisionId`。Artifact Ledger 保存完整正文和结构化 payload。画布节点同样引用该 revision：

- 点击对话卡片可以聚焦对应画布节点。
- 从对话或画布修改内容都创建新 revision，不原位覆盖。
- 付费媒体在异步执行前先创建 queued/running 占位节点和时间线卡片；完成或失败后更新同一投影身份。
- 所有 delta、状态、审批和 Artifact 事件先持久化 sequence 再推 SSE；刷新或重连按 sequence 补发。
- UI 不显示内部文本 Task、原始 reasoning、伪百分比或推测阶段。

本设计只增加必要卡片契约，现有对话框视觉保持不变。

## 12. 计费、幂等与并发

用户发送消息继续按现有 Agent Token 规则授权当前阶段的主 Agent 与必要专家文本推理；所有专家 usage 必须按 main run、specialist run 和 stage 聚合到现有账务与管理员审计。内部子 Agent 数量不能成为隐藏媒体费用。任何单独向用户收费的视觉分析批次，以及所有图片、视频、独立音频和其他媒体生成，都必须执行下面的冻结报价闭环。

所有付费生成统一执行：

```text
确定 artifact revision / attempt
  -> 冻结模型能力、参数、数量和报价
  -> 用户批准
  -> 预留积分
  -> 幂等创建供应商 Task
  -> 持久 Resource 与 usage
  -> 结算或进入不确定核对
  -> 等待内容审核
```

- 批准只对精确 revision、attempt、模型、参数、数量、报价、用户、项目、画布和过期时间有效。
- 任一冻结字段变化都必须重新报价和批准。
- 重复点击、网络重试、SSE 重放、服务重启和供应商重复回调不能重复创建 Task 或扣费。
- 计费成功不等于内容审核通过；内容不满意时资产保留，用户决定是否重新报价生成新 revision。
- 上游 revision 改变后，未提交任务可以停止；已提交任务继续记录真实账单和资产事实，但不能覆盖新 revision。
- ProductionGraph、Artifact revision、工具状态和画布写入分别使用版本/CAS；冲突必须重新读取事实后由主 Agent 决策，不覆盖用户修改。

## 13. 停止、错误与恢复

显式停止必须取消主 Run 和所有未终结 Specialist Run 的运行上下文，并阻止创建新工具和 Task。拒绝费用具有相同终止语义；“要求修改”不终止会话。

新增或明确稳定错误码：

- `specialist_model_inheritance_failed`
- `specialist_output_invalid`
- `production_graph_conflict`
- `production_stage_revision_stale`
- `artifact_revision_conflict`
- `asset_binding_unconfirmed`
- `asset_publication_failed`
- `visual_evidence_stale`
- `visual_analysis_failed`
- `native_audio_capability_unavailable`
- `stage_approval_revision_mismatch`
- `cost_approval_quote_mismatch`

日志至少关联 tenant、user、thread、main run、specialist run、stage、artifact/revision、tool call、task、billing order、resource、canvas revision 和 event sequence。日志只记录必要摘要、长度、状态和错误，不记录 API Key、完整私有 URL 签名、原始 reasoning 或用户私密全文。

Checkpoint 必须包含当前 ProductionGraph version、阶段状态、待审 revision、活跃专家、冻结 Skills/模型/工具版本和最后 sequence。恢复时任何冻结版本缺失都显式失败，不换模型、不换 Skill、不猜测继续点。

## 14. 权限与数据隔离

- 所有 Graph、Stage、SpecialistRun、Artifact、Binding、Publication、Task、Billing、Resource 和 Canvas 查询必须携带同一 tenant/project/canvas/user scope。
- 专家只获得完成当前任务所需的最小 Artifact revisions，不获得整租户数据。
- 图片理解和参考选择只能使用当前用户有权读取的 Resource。
- 用户确认不能被管理员、其他协作者或迟到请求冒用；协作项目按现有角色权限决定谁能审核和发布资产。
- Skill 不拥有工具权限。外部 Skill 未发布、许可证不清、checksum 不匹配或工具依赖未声明时不得用于 Run。
- 系统进化只能使用脱敏聚合事实；用户剧本、图像、音频、Prompt、URL 和偏好不得进入全局 Skill。

## 15. 硬切换与回滚

1. 先发布只包含新表、新索引和读取能力的后端迁移，不改变旧 Run 行为。
2. 审计全部非终态旧 Run；可在完全冻结版本中安全完成的先到达安全边界，其余以 `runtime_schema_retired` 终结。
3. 在单一发布中把新 Run 创建硬切到 ProductionGraph/Specialist Runtime；Web 同时切换新卡片契约，不保留旧新执行双轨。
4. 旧终态 Run 和旧生产表继续只读展示，不转换、覆盖或删除资金和资产事实。
5. 回滚时停止创建新 Run，保留新表和新事实；回滚版本必须能只读识别新 Runtime 创建的历史，不做数据库降级。

## 16. 实施里程碑与预算

本设计是架构改造，不能压成单个大提交。初步预算如下，实施计划必须进一步细化：

| 里程碑 | 生产职责 | 生产文件目标 | 净新增生产代码目标 |
| --- | --- | ---: | ---: |
| M1 | ProductionGraph、Artifact Revision、Stage/Binding 数据契约与迁移 | 8–12 | 700–1,000 行 |
| M2 | Specialist Runtime、模型继承、Skill/Tool allowlist、停止与恢复 | 8–12 | 800–1,100 行 |
| M3 | Narrative/Asset、阶段审核、资产发布、共享 Vision Analysis | 10–14 | 900–1,300 行 |
| M4 | Storyboard/Visual、ViMax 方法 Skills、Camera Tree 与一致性证据 | 10–14 | 1,000–1,500 行 |
| M5 | Video/Assembly、可选 Audio、动态模型能力与计费闭环 | 10–14 | 900–1,300 行 |
| M6 | 对话卡片、画布投影、断线恢复与端到端收口 | 8–12 | 700–1,100 行 |

预算是复杂度信号，不允许为满足行数把领域职责重新耦合。任一里程碑明显超出时必须先核对是否出现第二真源、固定工作流、重复计费链或超大文件，再重写实施计划。数据库文档、README 当前架构、测试和迁移说明不计入生产文件预算，但必须同步。

## 17. 测试策略

所有实现严格 RED → GREEN；日常只跑 focused tests，稳定里程碑和最终收口执行昂贵门禁。

### 17.1 领域与状态机

- ProductionGraph 版本 CAS、非法依赖、环和 stale 传播。
- Artifact revision 追加、上游引用、终态不可覆盖和旧资产保留。
- 阶段确认、要求修改、重复确认、旧 revision 确认和停止竞争。
- Specialist 模型继承、Skill/Tool allowlist、结构化输出校验和冻结版本缺失。

### 17.2 资产、视觉与音频

- 已确认图片/音频幂等发布资产库，事务失败不伪成功、不重复上传。
- 跨租户、跨项目、无权限 Resource 与过期 VisualEvidence 被拒绝。
- 图片理解只消费真实 URL；来源 revision 变化后证据 stale。
- Character Bible、镜头 schema、首帧/运动/尾帧和 Camera Tree 结构校验。
- 视频模型支持原生音轨时不创建 AudioArtifact；独立音频按明确意图单独报价、审核和入库。

### 17.3 计费、任务与恢复

- 报价冻结、批准、过期、参数变化、重复批准和拒绝。
- Task/Billing 幂等、进程重启、供应商重复/迟到回调、成功结算、退款和不确定核对。
- 停止后不创建新任务；迟到成功资产仍持久化但不覆盖当前 revision。
- 多 Agent 并发提交、Artifact CAS、Canvas revision/CAS 和事件 sequence。

### 17.4 时间线与交付

- 对话与画布引用同一 Artifact revision。
- 占位节点先创建，媒体完成/失败回填同一身份。
- SSE 断线补发、重复 sequence 去重、刷新/重启恢复和未知事件显式失败。
- `expectedDelivery -> deliveryEvidence -> deliveryVerification` 覆盖剧本、绑定、分镜图、视频、可选音频、资产发布和最终成片。

### 17.5 真实流程

最终至少验证：

1. 一句话开始，先停在完整剧本与角色表审核。
2. 多次修改剧本后只有确认 revision 能进入素材绑定。
3. 上传角色素材，图片理解形成证据并完成角色绑定。
4. 生成角色图，用户确认后自动加入资产库，重复确认不重复创建。
5. 分镜文本和分镜图在对话可审核并能定位画布节点。
6. 修改角色或剧本后下游变 stale。
7. 每个付费媒体展示模型、参数、数量和总预计积分。
8. 拒绝费用与停止能终止全部后续 Agent 行为。
9. 原生带声视频不产生独立音频；显式独立配音走可选 Audio Agent。
10. 一致性检查发现偏差时保留已生成资产，只追加诊断与修订建议。

## 18. 最终门禁与完成定义

最终执行：

- 后端 focused tests、`go test ./...`、受影响包 race、`go vet` 和 Go build。
- Web focused tests、全量 tests、TypeScript 检查和生产 build。
- SQLite 迁移测试及项目既有 PostgreSQL CAS/事务门禁。
- 计费、权限、幂等、并发、恢复、SSE 重放和数据库回滚演练。
- `git diff --check`。
- 本地浏览器完整免费/模拟流程；经用户再次批准后，使用最低成本真实模型执行一次付费生产验收。

只有同时满足以下条件才算完成：用户可以在单一会话中通过动态多专家协作逐阶段创作短片；每个内容 revision 可审核、可修订、可追溯；确认媒体能幂等进入资产库；图片理解、角色一致性和镜头语言有真实结构化证据；付费、停止、失败和恢复符合商业事实；对话与画布没有双份状态；旧 Runtime 不再承接新 Run；文档、数据库、权限、计费、测试与回滚证据全部闭合。

## 19. 参考来源

- ViMax repository：<https://github.com/HKUDS/ViMax/tree/05a48943878312d88fe5a016c12a9654940ecc43>
- ViMax character extractor：<https://github.com/HKUDS/ViMax/blob/05a48943878312d88fe5a016c12a9654940ecc43/agents/character_extractor.py>
- ViMax storyboard artist：<https://github.com/HKUDS/ViMax/blob/05a48943878312d88fe5a016c12a9654940ecc43/agents/storyboard_artist.py>
- ViMax camera image generator：<https://github.com/HKUDS/ViMax/blob/05a48943878312d88fe5a016c12a9654940ecc43/agents/camera_image_generator.py>
- ViMax MIT License：<https://github.com/HKUDS/ViMax/blob/05a48943878312d88fe5a016c12a9654940ecc43/LICENSE>
