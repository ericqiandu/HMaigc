# HMaigc Multi-Specialist Production Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有 Go 商业 Runtime 内硬切出一个可逐阶段审核、可调用版本化 Skills、可按需委派专家、可追溯 Artifact revision、并与现有 Task/计费/资产/画布/SSE 真源闭环的多专家短片生产体系。

**Architecture:** 保留单一用户会话和现有 `AgentRun` 作为唯一主运行身份，新增追加式 ProductionGraph、Stage、SpecialistRun、Artifact Revision、Binding 与 Publication 领域事实。主 Agent 通过结构化 `specialist.delegate`、`vision.analyze`、`media.generate`、`canvas.project` 工具做动态语义编排；Go Runtime 只执行权限、版本、依赖、CAS、报价、Task、Billing、Resource、资产发布和画布投影等确定性动作。旧 `AgentProductionPlanVersion` / `AgentProductionArtifact` 只读，新 Run 在最终切换提交中一次性进入 Runtime v3，不维护新旧执行双轨。

**Tech Stack:** Go 1.25、Gin、GORM、SQLite/PostgreSQL、React 18、TypeScript strict、Vite、Bun test、持久 sequence SSE、现有 BillingOrder/Task/Resource/Canvas CAS/Artifact Ledger。

**Spec:** `docs/superpowers/specs/2026-08-27-agent-multi-specialist-production-design.md`

## Global Constraints

- 固定版本：`RuntimeVersion=3`、`PolicyVersion=3`、`ToolSchemaVersion=4`、`AgentUIProtocolVersion=3`、`ProductionSchemaVersion=1`。
- 新表统一采用 `agent_production_graph_versions`、`agent_production_stages`、`agent_specialist_runs`、`agent_artifacts`、`agent_artifact_revisions`、`agent_asset_binding_revisions`、`agent_asset_publications`；不覆盖、不回填、不双写旧生产表。
- 所有新事实必须携带同一 `tenantKind + tenantId + actorUserId + domainProjectId + canvasId + threadId + runId` scope；任何跨租户、跨项目、跨画布、跨用户访问原地失败。
- 主 Agent 和 Specialist 必须使用同一 `ModelRecordID + ModelKey`；继承失败返回 `specialist_model_inheritance_failed`，不得选择默认或备用模型。
- 语义判断、专家选择、Skill 选择和 ProductionGraph 内容由 Agent 完成；Go/Web 只做 schema、类型、权限、revision、依赖、数量、金额、URL、状态和 CAS 校验，禁止关键词、正则、固定 route 或固定专家顺序。
- 用户可见阶段的 `reviewPolicy` 首期恒为 `required`；`guided` 与 `automatic` 都不能跨过内容审核，任何付费媒体都不能跨过冻结报价审批。
- 第一份用户可见交付只能是完整剧本与角色目录；需求分析、上下文裁剪、Skill 加载和内部文本 Task 不建立用户审核卡。
- `reasoning_content` 不写入会话、Artifact 或用户日志；只允许使用供应商 usage 计费，并在受限诊断记录长度、Token 数和请求身份。
- Timeline/Canvas/Asset Library 只引用同一 `artifactId + revisionId`；完整正文和媒体事实只保存在 Artifact Ledger / Resource 真源。
- 任何 delta、状态、审批和 Artifact 投影必须先持久化 sequence，再推送 SSE；重连按 sequence 补发，前端按 sequence 去重。
- 图片、视频、音频和可计费视觉分析执行统一使用现有冻结报价、积分预留、幂等 Task、结算/不确定核对链路；内容审核结果不改变已发生的真实账务。
- 停止或费用拒绝终止主 Run 与全部未终态 SpecialistRun，并禁止创建新模型步骤、工具或 Task；已提交供应商的迟到结果继续持久化为 `unadopted`，不得删除或覆盖当前 revision。
- 生成成功的全部候选都必须保留；一致性审查只能追加证据、排序和修订建议，不能自动删除、回滚或丢弃资产。
- Skills 运行时只从已发布 `SkillVersion` 加载，并冻结 version/checksum/license/capability manifest；不得把本规格、`docs/`、临时 ViMax 克隆、`assets/` 或 `ai-metadata/` 注入运行时。
- ViMax 固定参考提交为 `05a48943878312d88fe5a016c12a9654940ecc43`，来源许可为 MIT；实质改编内容必须进入第三方声明。
- 不新增 `any` / `as any`；所有 JSON 边界使用严格解码、`unknown` 收窄或显式 schema。
- 不改 Agent 对话框总体视觉，只增加必要的 Artifact、媒体、阶段审核和费用卡片内容。
- 每个任务严格 RED -> GREEN；日常只跑 focused tests，M1–M6 稳定点和最终收口才跑昂贵门禁。
- 执行时不使用子代理并行；这些任务共享 Runtime、计费、资产、事件和迁移状态，必须按本文顺序由 `superpowers:executing-plans` 内联完成。
- 每个任务的提交只暂存列出的本任务文件/相关 hunk；先检查 `git diff --cached`，不得带入用户其他修改，不得 push。

---

## Change Budget and Release Boundaries

| Milestone | Production responsibility | Production files | Net-new production code | Release boundary |
| --- | --- | ---: | ---: | --- |
| M1 | Graph、Stage、Artifact Revision、Binding、Publication 数据契约与追加迁移 | 8–12 | 700–1,000 | 只加表和读写能力，不改变旧 Run |
| M2 | Specialist Runtime、模型继承、Skill/Tool allowlist、停止与恢复 | 8–12 | 800–1,100 | 新内部能力仍不从用户入口创建 v3 Run |
| M3 | Narrative/Asset、阶段审核、资产发布、共享 Vision Analysis | 10–14 | 900–1,300 | 免费/模拟流程可在服务测试中完成 |
| M4 | Storyboard/Visual、ViMax Skills、Camera Tree 与一致性证据 | 10–14 | 1,000–1,500 | 视觉方法与结构校验闭合 |
| M5 | Video/Assembly、可选 Audio、动态模型能力与计费闭环 | 10–14 | 900–1,300 | 所有媒体走现有商业真源 |
| M6 | Timeline 卡片、Canvas 投影、SSE 恢复、v3 硬切与 E2E | 8–12 | 700–1,100 | 单一发布硬切新 Run，旧终态只读 |

预算只用于暴露职责扩散。任一里程碑明显超出时，先检查是否引入第二真源、固定工作流、重复计费、重复事件协议或超大文件；不能为了行数删掉必要的事务、日志、权限或测试。

## Business Acceptance Coverage

以下矩阵把已确认的用户体验映射到唯一实施里程碑。技术契约不能替代这些业务验收项，任一项缺少真实证据均视为对应里程碑未完成。

| 已确认能力 | 唯一事实与行为 | 实施与验收落点 |
| --- | --- | --- |
| 一句话开始短片创作 | Main Agent 建立 ProductionGraph；首个用户可见结果必须是完整剧本包与角色目录，不能直接消费媒体额度 | M3 Task 9–10；M6 浏览器场景 1 |
| 剧本不满意可反复修改 | 用户针对精确 `artifactId + revisionId` 请求修订；新 revision 追加，旧 revision 不覆盖；只有明确批准才进入下一阶段 | M1 Task 1–3；M3 Task 10；M6 浏览器场景 2 |
| 理解剧本、角色素材库和用户上传素材 | Asset Specialist 只基于真实 Resource、项目资产和已批准剧本建立绑定；缺真实 URL 显式失败 | M3 Task 9、11–12；M6 浏览器场景 3 |
| 图片理解 | `vision.analyze` 读取真实图片 Resource，输出可追溯 Visual Evidence；人物、服装、场景、构图和连续性事实与来源 revision 绑定 | M3 Task 12；M4 Task 13–15 |
| 角色一致性 | Character Visual Bible、角色资产绑定、候选图和一致性证据均追加保存；审查不删除生成结果 | M4 Task 13–15；M6 浏览器场景 4、6 |
| 电影化镜头语言与 Camera Tree | Storyboard/Visual Specialists 按需加载版本化 Skills，产出结构化景别、机位、运动、构图、光线和镜头树，不在 Go/Web 硬编码创作 SOP | M4 Task 13–14 |
| 首帧—运动—尾帧连续性 | 每个镜头引用精确上游视觉 revision，并保存 first-frame、motion、last-frame 证据；缺真实前置资产不得执行下游媒体 | M4 Task 13–15；M5 Task 17–18 |
| 分镜、角色图和视频先在对话中可见 | Timeline 使用真实 Artifact/Resource 卡片；同一 revision 可聚焦到对应画布占位节点，不能用文本假卡片代替 | M6 Task 19–21；M6 浏览器场景 5、10 |
| 用户确认后加入资产库 | 只有已批准媒体 revision 可发布；Asset/Version/Link/Representation/Publication 同事务且重复点击幂等 | M3 Task 11；M6 浏览器场景 4 |
| 视频原生对白与可选独立音频 | 原生带声视频不重复创建独立 Audio Artifact；只有用户/Agent 明确选择特殊音频需求才进入 Audio Specialist 和独立报价 | M5 Task 16–18；M6 浏览器场景 9 |
| 每阶段确认与付费确认分离 | `guided` 与 `automatic` 都必须停在每个用户可见内容审核；所有付费图片、视频、音频和计费视觉分析还要单独确认冻结报价 | M3 Task 9–10；M5 Task 16–17；M6 浏览器场景 7–8 |
| Skills 可识别、调用且可审计 | Agent 自主选择已发布 SkillVersion；运行时冻结 version/checksum/license/capability manifest，不把规格、临时仓库或 docs 当运行时知识 | M2 Task 5–7；M4 Task 13 |
| 停止、拒绝与恢复 | 停止或拒绝费用终止整棵 Run 树并阻止新副作用；迟到资产保留为未采纳；SSE 按持久 sequence 补发且不重复 | M2 Task 8；M6 Task 21–22 |

## Locked Domain Contracts

以下名称是后续任务的跨文件契约，实施时不得在不同任务中另起同义类型：

```go
const (
    CurrentRuntimeVersion          = 3
    CurrentPolicyVersion           = 3
    CurrentToolSchemaVersion       = 4
    CurrentProductionSchemaVersion = 1
    CurrentAgentUIProtocolVersion  = 3
)

type SpecialistKey string
const (
    SpecialistNarrative     SpecialistKey = "narrative"
    SpecialistAsset         SpecialistKey = "asset"
    SpecialistStoryboard    SpecialistKey = "storyboard"
    SpecialistVisual        SpecialistKey = "visual"
    SpecialistVideoAssembly SpecialistKey = "video_assembly"
    SpecialistAudio         SpecialistKey = "audio"
)

type ProductionStageStatus string
const (
    StagePlanned        ProductionStageStatus = "planned"
    StageRunning        ProductionStageStatus = "running"
    StageAwaitingReview ProductionStageStatus = "awaiting_review"
    StageApproved       ProductionStageStatus = "approved"
    StageCompleted      ProductionStageStatus = "completed"
    StageFailed         ProductionStageStatus = "failed"
    StageStopped        ProductionStageStatus = "stopped"
    StageStale          ProductionStageStatus = "stale"
)

type StageReviewDecision string
const (
    StageReviewApprove         StageReviewDecision = "approved"
    StageReviewRequestRevision StageReviewDecision = "revision_requested"
    StageReviewStop            StageReviewDecision = "stopped"
)

type AgentToolName string
const (
    ToolSkillLoad          AgentToolName = "skill.load"
    ToolSpecialistDelegate AgentToolName = "specialist.delegate"
    ToolVisionAnalyze      AgentToolName = "vision.analyze"
    ToolMediaGenerate      AgentToolName = "media.generate"
    ToolCanvasProject      AgentToolName = "canvas.project"
)
```

```go
type ArtifactRevisionRef struct {
    ArtifactID string `json:"artifactId"`
    RevisionID string `json:"revisionId"`
}

type ReviewPolicy string
const ReviewRequired ReviewPolicy = "required"

type CostPolicy string
const (
    CostNone             CostPolicy = "none"
    CostApprovalRequired CostPolicy = "approval_required"
)

type StageReviewCommand struct {
    StageVersion    int64               `json:"stageVersion"`
    RevisionID      string              `json:"revisionId"`
    Decision        StageReviewDecision `json:"decision"`
    ClientRequestID string              `json:"clientRequestId"`
    Comment         string              `json:"comment,omitempty"`
}

type ArtifactDraft struct {
    ArtifactKey          string                `json:"artifactKey"`
    Kind                 string                `json:"kind"`
    SchemaVersion        int                   `json:"schemaVersion"`
    Payload              json.RawMessage       `json:"payload"`
    ResourceID           string                `json:"resourceId,omitempty"`
    UpstreamRevisions    []ArtifactRevisionRef `json:"upstreamRevisions"`
    ModelRequestIdentity string                `json:"modelRequestIdentity,omitempty"`
    SkillVersions        []SkillSelection      `json:"skillVersions"`
}

type SpecialistRequest struct {
    SpecialistRunID       string                `json:"specialistRunId"`
    SpecialistKey         SpecialistKey         `json:"specialistKey"`
    SpecialistVersion     int                   `json:"specialistVersion"`
    Objective             string                `json:"objective"`
    InputRevisions        []ArtifactRevisionRef `json:"inputRevisions"`
    LoadedSkills          []SkillSelection      `json:"loadedSkills"`
    ToolAllowlist         []AgentToolName       `json:"toolAllowlist"`
    ExpectedOutputSchema  string                `json:"expectedOutputSchema"`
    ExpectedDelivery      ExpectedDelivery      `json:"expectedDelivery"`
}

type SpecialistResult struct {
    Summary       string          `json:"summary"`
    Artifacts     []ArtifactDraft `json:"artifacts"`
    Delivery     DeliveryEvidence `json:"delivery"`
    NextActions  []RequiredAction `json:"nextActions"`
}

type ArtifactEvidence struct {
    ArtifactID    string       `json:"artifactId"`
    RevisionID    string       `json:"revisionId"`
    Kind          ArtifactKind `json:"kind"`
    ResourceID    string       `json:"resourceId,omitempty"`
    ResourceReady bool         `json:"resourceReady"`
    PublicationID string       `json:"publicationId,omitempty"`
}
```

`SpecialistResult` 只在严格 schema 通过后追加 Artifact revision。专家输出不能直接写 SQL、创建 Task、扣费、发布资产或改 React Flow。

## Exact File Map

### Existing files to modify

- `backend/internal/agentruntime/contracts.go` — v3 runtime/tool/policy 常量、通用工具名和稳定错误码。
- `backend/internal/agentruntime/runtime.go` — checkpoint 只增加 graph/stage/specialist/revision 引用，不嵌入完整 Artifact payload。
- `backend/internal/agentruntime/decision.go` — v3 工具严格解码和 expected-delivery 校验；删除新 Run 对旧三种生产工具的可调用性。
- `backend/internal/agentruntime/delivery.go` — 扩展通用 Artifact/stage/publication/final-resource 证据维度，不增加 case-specific 关键词规则。
- `backend/internal/model/agent_runtime.go` — 仅保留主 Run 与旧生产表；新生产模型拆到独立文件。
- `backend/internal/database/schema.go` — 注册新模型并调用追加迁移。
- `backend/internal/repository/agent_runtime.go` — 主 Run v3 checkpoint / 终止事务接入，不承载新表细节。
- `backend/internal/service/agent_runtime.go` — 主循环接入 graph/specialist 事实、v3 Token 聚合和最终 verifier。
- `backend/internal/service/agent_runtime_context.go` — 模型上下文从旧 plan fact 切到冻结 graph/current stage/artifact summaries。
- `backend/internal/service/agent_runtime_configuration.go` — GenerationModels 增加 audio/vision，冻结 Skill capability manifest 与 license。
- `backend/internal/service/agent_runtime_tools.go` — v3 五个工具的冻结、审批、执行与恢复入口。
- `backend/internal/service/agent_runtime_transport.go` — UI protocol v3 和 `contentJSON.type` 子契约。
- `backend/internal/handler/agent_runtime.go` — stage review 和 exact artifact revision 读取路由。
- `backend/internal/database/agent_runtime_schema.go` — 旧 Runtime 退役审计和 schema marker 协同。
- `backend/internal/service/project_asset.go` — 复用现有资产领域校验；发布事务本身放新服务/仓储文件。
- `README.md` — 当前 Agent 架构、硬切版本和用户流程。
- `web/src/services/api/agent-runtime.ts` — v3 strict parser、stage review 和 artifact fetch API。
- `web/src/components/canvas/use-agent-runtime.ts` — sequence 重放、卡片引用和 stage actions。
- `web/src/components/canvas/agent-conversation-reducer.ts` — 同一 item 增量、Artifact card 生命周期和未知事件显式失败。
- `web/src/components/canvas/canvas-assistant-panel.tsx` — 在现有视觉内挂载业务卡片。
- `web/src/pages/canvas/use-canvas-agent-operations.ts` — `artifactId + revisionId` 到 Canvas CAS 投影。

### Focused files to create

- `backend/internal/agentruntime/production_graph.go` — Graph/Stage 输入、依赖 DAG、状态机、stale 传播纯函数。
- `backend/internal/agentruntime/artifact.go` — Artifact kind、revision ref、payload envelope、不可变字段与结构校验。
- `backend/internal/agentruntime/specialist.go` — specialist registry contract、request/result、模型/Skill/Tool 冻结校验。
- `backend/internal/agentruntime/visual_evidence.go` — VisualEvidence、CharacterBible、Shot、CameraTree 的纯结构校验。
- `backend/internal/model/agent_production_runtime.go` — 七张新表的 GORM 模型。
- `backend/internal/database/agent_production_runtime_schema.go` — SQLite/PostgreSQL 追加迁移、唯一索引、schema marker。
- `backend/internal/repository/agent_production_graph.go` — graph version、stage CAS、stale propagation 事务。
- `backend/internal/repository/agent_artifact.go` — stable artifact、immutable revision、exact revision 读取。
- `backend/internal/repository/agent_specialist_run.go` — specialist 创建/心跳/终止/恢复状态。
- `backend/internal/repository/agent_asset_publication.go` — publication + asset/version/link/representation 同事务。
- `backend/internal/service/agent_specialist_runtime.go` — 子 Agent 执行、模型继承、Token 汇总、结构校验。
- `backend/internal/service/agent_stage_review.go` — review 命令、revision 匹配、stage 完成与修订回路。
- `backend/internal/service/agent_asset_publication.go` — `PublishAsset` 应用端口和幂等身份。
- `backend/internal/service/agent_visual_analysis.go` — 真实 Resource 输入、视觉模型调用、证据 Artifact。
- `backend/internal/service/agent_media_generation.go` — image/video/audio 统一冻结报价和 Task 端口。
- `backend/internal/service/agent_canvas_projection.go` — Artifact revision 到 queued/running/succeeded/failed 节点投影。
- `backend/internal/handler/agent_production.go` — stage review 和 Artifact revision HTTP DTO/响应。
- `web/src/components/canvas/agent-artifact-review-card.tsx` — 剧本、角色、绑定、分镜文本审核。
- `web/src/components/canvas/agent-media-gallery-card.tsx` — 图片/视频/音频候选、资产发布状态和画布定位。
- `web/src/components/canvas/agent-stage-approval-card.tsx` — 确认、要求修改、停止；声明发布意图时显示入库语义。
- `web/src/components/canvas/agent-production-card-contract.ts` — `contentJSON.type` 严格解析与 exhaustiveness。
- `backend/internal/skillcatalog/catalog/character-visual-bible/SKILL.md`
- `backend/internal/skillcatalog/catalog/storyboard-cinematic-language/SKILL.md`
- `backend/internal/skillcatalog/catalog/camera-tree-continuity/SKILL.md`
- `backend/internal/skillcatalog/catalog/first-motion-last-frame/SKILL.md`
- `backend/internal/skillcatalog/catalog/visual-consistency-review/SKILL.md`
- `backend/internal/skillcatalog/catalog/visual-evidence-analysis/SKILL.md`
- `THIRD_PARTY_NOTICES.md` — ViMax 固定提交和 MIT 归属。

### Tests to create or extend

- `backend/internal/agentruntime/production_graph_test.go`
- `backend/internal/agentruntime/artifact_test.go`
- `backend/internal/agentruntime/specialist_test.go`
- `backend/internal/agentruntime/visual_evidence_test.go`
- `backend/internal/database/agent_production_runtime_schema_test.go`
- `backend/internal/repository/agent_production_graph_test.go`
- `backend/internal/repository/agent_artifact_test.go`
- `backend/internal/repository/agent_asset_publication_test.go`
- `backend/internal/service/agent_specialist_runtime_test.go`
- `backend/internal/service/agent_stage_review_test.go`
- `backend/internal/service/agent_asset_publication_test.go`
- `backend/internal/service/agent_visual_analysis_test.go`
- `backend/internal/service/agent_media_generation_test.go`
- `backend/internal/service/agent_production_recovery_test.go`
- `backend/internal/handler/agent_production_test.go`
- `web/test/agent-production-card-contract.test.ts`
- `web/test/agent-artifact-review-card.test.tsx`
- `web/test/agent-media-gallery-card.test.tsx`
- extend `web/test/agent-runtime-api.test.ts`
- extend `web/test/agent-conversation-reducer.test.ts`
- extend `web/test/canvas-agent-runtime-panel.test.tsx`

---

## M1 — ProductionGraph and Immutable Artifact Facts

### Task 1: Lock Graph, Stage, Artifact and Review Pure Contracts

**Files:**
- Create: `backend/internal/agentruntime/production_graph.go`
- Create: `backend/internal/agentruntime/artifact.go`
- Test: `backend/internal/agentruntime/production_graph_test.go`
- Test: `backend/internal/agentruntime/artifact_test.go`
- Modify: `backend/internal/agentruntime/contracts.go`

**Interfaces:**
- Consumes: existing `Scope`, `ExpectedDelivery`, stable runtime status constants.
- Produces: `ProductionGraphDraft`, `ProductionStageDraft`, `ArtifactDraft`, `ArtifactRevisionRef`, `StageReviewCommand`, `ValidateProductionGraph`, `ValidateArtifactDraft`, `TransitionProductionStage`, `StaleDependentStages`.

- [ ] **Step 1: Write failing DAG, revision and state-machine tests**

```go
func TestValidateProductionGraphRejectsCycle(t *testing.T) {
    draft := agentruntime.ProductionGraphDraft{Stages: []agentruntime.ProductionStageDraft{
        {StageKey: "a", DependsOnStageKeys: []string{"b"}, ReviewPolicy: agentruntime.ReviewRequired},
        {StageKey: "b", DependsOnStageKeys: []string{"a"}, ReviewPolicy: agentruntime.ReviewRequired},
    }}
    require.ErrorIs(t, agentruntime.ValidateProductionGraph(draft), agentruntime.ErrProductionGraphCycle)
}

func TestApproveStageRequiresExactReviewRevision(t *testing.T) {
    stage := agentruntime.ProductionStageState{Status: agentruntime.StageAwaitingReview, Version: 4, ReviewRevisionID: "rev-2"}
    _, err := agentruntime.TransitionProductionStage(stage, agentruntime.StageReviewCommand{
        StageVersion: 4, RevisionID: "rev-1", Decision: agentruntime.StageReviewApprove,
    })
    require.ErrorIs(t, err, agentruntime.ErrStageApprovalRevisionMismatch)
}

func TestValidateArtifactDraftRequiresExactUpstreamRevisionRefs(t *testing.T) {
    err := agentruntime.ValidateArtifactDraft(agentruntime.ArtifactDraft{Kind: "script", SchemaVersion: 1, Payload: json.RawMessage(`{"title":"片名"}`), UpstreamRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: "artifact-1"}}})
    require.ErrorIs(t, err, agentruntime.ErrArtifactRevisionInvalid)
}
```

- [ ] **Step 2: Run the focused domain tests and confirm RED**

Run: `cd backend && go test ./internal/agentruntime -run 'TestValidateProductionGraph|TestApproveStage|TestValidateArtifactDraft' -count=1`

Expected: compilation fails because the new graph/artifact contracts do not exist.

- [ ] **Step 3: Implement strict pure contracts**

```go
type ProductionGraphDraft struct {
    GraphKey string                 `json:"graphKey"`
    Stages   []ProductionStageDraft `json:"stages"`
}

type ProductionStageDraft struct {
    StageKey            string                `json:"stageKey"`
    SpecialistKey       SpecialistKey         `json:"specialistKey"`
    DependsOnStageKeys  []string              `json:"dependsOnStageKeys"`
    InputRevisions      []ArtifactRevisionRef `json:"inputRevisions"`
    ExpectedDelivery    ExpectedDelivery      `json:"expectedDelivery"`
    ReviewPolicy        ReviewPolicy          `json:"reviewPolicy"`
    CostPolicy          CostPolicy            `json:"costPolicy"`
}
```

Implement Kahn topological validation, duplicate-key rejection, exact revision-ref validation, allowed state transitions, and transitive stale propagation. Do not encode a Narrative→Asset→Storyboard order; dependency edges are model output and only structural validity is local.

- [ ] **Step 4: Run all `agentruntime` tests and confirm GREEN**

Run: `cd backend && go test ./internal/agentruntime -count=1`

Expected: all current and new tests pass; old decision/delivery tests stay unchanged.

- [ ] **Step 5: Review and commit the pure domain contract**

Run: `git diff --check && git diff -- backend/internal/agentruntime`

Commit:

```bash
git add backend/internal/agentruntime/contracts.go backend/internal/agentruntime/production_graph.go backend/internal/agentruntime/artifact.go backend/internal/agentruntime/production_graph_test.go backend/internal/agentruntime/artifact_test.go
git commit -m "feat(agent): define production graph contracts"
```

### Task 2: Add the Seven Append-Only Production Models and Schema

**Files:**
- Create: `backend/internal/model/agent_production_runtime.go`
- Create: `backend/internal/database/agent_production_runtime_schema.go`
- Create: `backend/internal/database/agent_production_runtime_schema_test.go`
- Modify: `backend/internal/database/schema.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: Task 1 status/ref constants and existing GORM database dialect helpers.
- Produces: seven GORM models, `EnsureAgentProductionRuntimeSchema(db *gorm.DB) error`, unique publication identity, graph/revision/stage CAS indexes.

- [ ] **Step 1: Write failing SQLite migration and constraint tests**

```go
func TestEnsureAgentProductionRuntimeSchemaCreatesAppendOnlyTables(t *testing.T) {
    db := openSQLiteTestDB(t)
    require.NoError(t, database.EnsureAgentProductionRuntimeSchema(db))
    for _, table := range []string{
        "agent_production_graph_versions", "agent_production_stages", "agent_specialist_runs",
        "agent_artifacts", "agent_artifact_revisions", "agent_asset_binding_revisions", "agent_asset_publications",
    } {
        require.True(t, db.Migrator().HasTable(table), table)
    }
}

func TestAgentArtifactRevisionIdentityIsUnique(t *testing.T) {
    db := migratedAgentProductionDB(t)
    first := model.AgentArtifactRevision{ID: "r1", ArtifactID: "a1", Revision: 1, PayloadJSON: `{}`, SchemaVersion: 1}
    duplicate := model.AgentArtifactRevision{ID: "r2", ArtifactID: "a1", Revision: 1, PayloadJSON: `{}`, SchemaVersion: 1}
    require.NoError(t, db.Create(&first).Error)
    require.Error(t, db.Create(&duplicate).Error)
}
```

- [ ] **Step 2: Run migration tests and confirm RED**

Run: `cd backend && go test ./internal/database -run 'TestEnsureAgentProductionRuntimeSchema|TestAgentArtifactRevisionIdentity' -count=1`

Expected: compilation fails because models and migration are absent.

- [ ] **Step 3: Implement models and dialect-safe indexes**

```go
type AgentArtifactRevision struct {
    ID                    string `gorm:"primaryKey;size:36"`
    ArtifactID            string `gorm:"size:36;uniqueIndex:idx_agent_artifact_revision_number,priority:1"`
    Revision              int64  `gorm:"uniqueIndex:idx_agent_artifact_revision_number,priority:2"`
    SchemaVersion         int    `gorm:"not null"`
    PayloadJSON           string `gorm:"type:text;not null"`
    ResourceID            string `gorm:"size:36;index"`
    UpstreamRevisionsJSON string `gorm:"type:text;not null"`
    ModelRequestIdentity  string `gorm:"size:180;index"`
    SkillVersionsJSON     string `gorm:"type:text;not null"`
    CreatedByRunID        string `gorm:"size:36;index"`
    CreatedBySpecialistID string `gorm:"size:36;index"`
    LifecycleStatus       string `gorm:"size:32;index"`
    CreatedAt             time.Time
}
```

All seven models include the full scope columns. `AgentAssetPublication` gets a unique composite identity over `tenant_kind, tenant_id, domain_project_id, artifact_revision_id, publication_purpose`; `AgentProductionStage` and mutable lifecycle rows get integer `Version` for CAS. Published Artifact payload/resource/upstream fields must have no update method.

- [ ] **Step 4: Run migration tests and existing database suite**

Run: `cd backend && go test ./internal/database -count=1`

Expected: all tests pass on SQLite; schema registration is additive and existing tables remain present.

- [ ] **Step 5: Document tables and commit the additive migration**

Add a table-by-table ownership section to the root `README.md`, including append-only fields, mutable lifecycle fields, unique identities, and “old tables read-only”.

Commit:

```bash
git add backend/internal/model/agent_production_runtime.go backend/internal/database/agent_production_runtime_schema.go backend/internal/database/agent_production_runtime_schema_test.go backend/internal/database/schema.go README.md
git commit -m "feat(agent): add production runtime schema"
```

### Task 3: Implement Graph, Stage and Artifact CAS Repositories

**Files:**
- Create: `backend/internal/repository/agent_production_graph.go`
- Create: `backend/internal/repository/agent_artifact.go`
- Create: `backend/internal/repository/agent_production_graph_test.go`
- Create: `backend/internal/repository/agent_artifact_test.go`

**Interfaces:**
- Consumes: `AgentProductionGraphVersion`, `AgentProductionStage`, `AgentArtifact`, `AgentArtifactRevision` and Task 1 validators.
- Produces: `AppendProductionGraphVersion`, `AdvanceProductionStageCAS`, `AppendArtifactRevision`, `ArtifactRevisionInScope`, `MarkDependentStagesStale`.

- [ ] **Step 1: Write failing repository transaction/CAS tests**

```go
func TestAppendArtifactRevisionIsMonotonicUnderConflict(t *testing.T) {
    repo := newProductionRepository(t)
    scope := productionScopeFixture()
    _, err := repo.AppendArtifactRevision(scope, "artifact-1", 0, artifactDraftFixture())
    require.NoError(t, err)
    _, err = repo.AppendArtifactRevision(scope, "artifact-1", 0, artifactDraftFixture())
    require.ErrorIs(t, err, repository.ErrArtifactRevisionConflict)
}

func TestMarkDependentStagesStaleIsTransitive(t *testing.T) {
    repo := seededGraphRepository(t, "script", "storyboard", "video")
    require.NoError(t, repo.MarkDependentStagesStale(productionScopeFixture(), "script", "rev-2"))
    require.Equal(t, agentruntime.StageStale, readStage(t, repo, "storyboard").Status)
    require.Equal(t, agentruntime.StageStale, readStage(t, repo, "video").Status)
}
```

- [ ] **Step 2: Run repository tests and confirm RED**

Run: `cd backend && go test ./internal/repository -run 'TestAppendArtifactRevision|TestMarkDependentStagesStale' -count=1`

Expected: compilation fails because repository methods are absent.

- [ ] **Step 3: Implement short transactions with rows-affected CAS**

```go
func (r *Repository) AdvanceProductionStageCAS(scope agentruntime.Scope, stageID string, expectedVersion int64, next agentruntime.ProductionStageStatus) error {
    result := r.db.Model(&model.AgentProductionStage{}).
        Where("id = ? AND tenant_kind = ? AND tenant_id = ? AND domain_project_id = ? AND canvas_id = ? AND run_id = ? AND version = ?", stageID, scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID, scope.RunID, expectedVersion).
        Updates(map[string]any{"status": next, "version": gorm.Expr("version + 1"), "updated_at": time.Now()})
    if result.Error != nil { return result.Error }
    if result.RowsAffected != 1 { return ErrProductionStageConflict }
    return nil
}
```

`AppendProductionGraphVersion` validates the full draft before the transaction. `AppendArtifactRevision` locks or CAS-updates the stable artifact head, inserts exactly one immutable revision, persists upstream refs, then emits no timeline itself. Cross-scope reads return `gorm.ErrRecordNotFound`, not a distinguishable ownership leak.

- [ ] **Step 4: Run repository tests, including PostgreSQL when configured**

Run: `cd backend && go test ./internal/repository -count=1`

Run when `POSTGRES_TEST_DSN` exists: `cd backend && go test ./internal/repository -run 'Postgres|ProductionGraph|Artifact' -count=1`

Expected: SQLite passes; configured PostgreSQL CAS tests pass without lost update.

- [ ] **Step 5: Commit repository facts**

```bash
git add backend/internal/repository/agent_production_graph.go backend/internal/repository/agent_artifact.go backend/internal/repository/agent_production_graph_test.go backend/internal/repository/agent_artifact_test.go
git commit -m "feat(agent): persist graph and artifact revisions"
```

### Task 4: Add the Additive Cutover Audit and Retirement Command

**Files:**
- Modify: `backend/internal/database/agent_runtime_schema.go`
- Modify: `backend/internal/database/agent_runtime_schema_test.go`
- Modify: `backend/internal/repository/agent_runtime.go`
- Test: `backend/internal/repository/agent_runtime_test.go`

**Interfaces:**
- Consumes: existing Runtime v2 rows and new production schema marker.
- Produces: `AuditRetirableLegacyAgentRuns`, `RetireLegacyAgentRunsAtSafeBoundary`, stable `runtime_schema_retired` failure code. It does not switch user run creation yet.

- [ ] **Step 1: Write failing retirement safety tests**

```go
func TestRetireLegacyAgentRunsLeavesTerminalHistoryUntouched(t *testing.T) {
    repo := seededLegacyRuns(t, agentruntime.RunSucceeded, agentruntime.RunWaitingApproval)
    retired, err := repo.RetireLegacyAgentRunsAtSafeBoundary(2, 3, "runtime_schema_retired")
    require.NoError(t, err)
    require.Equal(t, 1, retired)
    require.Equal(t, agentruntime.RunSucceeded, legacyRunStatus(t, repo, "terminal"))
    require.Equal(t, agentruntime.RunFailed, legacyRunStatus(t, repo, "nonterminal"))
}
```

- [ ] **Step 2: Run focused retirement tests and confirm RED**

Run: `cd backend && go test ./internal/database ./internal/repository -run 'LegacyAgentRuns|RuntimeSchemaRetired' -count=1`

Expected: compilation fails because audit/retirement methods do not exist.

- [ ] **Step 3: Implement explicit audit and idempotent retirement**

The audit must return counts grouped by runtime version/status and identify pending approvals/tools/tasks without changing data. Retirement may run only after an operator-visible audit and only against `runtime_version < 3 AND status NOT IN ('succeeded','failed','cancelled')`; it appends a terminal timeline/error event with persisted sequence in the same transaction.

```go
type LegacyRunRetirementAudit struct {
    RuntimeVersion int64
    Status         agentruntime.RunStatus
    Count          int64
    HasPendingTask bool
}
```

- [ ] **Step 4: Run focused schema/repository tests and inspect SQL scope**

Run: `cd backend && go test ./internal/database ./internal/repository -run 'LegacyAgentRuns|RuntimeSchemaRetired' -count=1`

Expected: tests pass; rerunning retirement reports zero additional rows and terminal history is unchanged.

- [ ] **Step 5: Commit the inactive cutover tooling**

```bash
git add backend/internal/database/agent_runtime_schema.go backend/internal/database/agent_runtime_schema_test.go backend/internal/repository/agent_runtime.go backend/internal/repository/agent_runtime_test.go
git commit -m "feat(agent): add runtime cutover audit"
```

### M1 Gate

- [ ] Run `cd backend && go test ./internal/agentruntime ./internal/database ./internal/repository -count=1`.
- [ ] Run configured PostgreSQL CAS tests.
- [ ] Run `git diff --check` and verify only additive schema/read-write capability changed; user-created runs still use v2 behavior.
- [ ] Review budget: 8–12 production files and approximately 700–1,000 net-new production lines; explain any excess before M2.

---

## M2 — Specialist Runtime, Inheritance, Stop and Recovery

### Task 5: Define Specialist Registry and Frozen Capability Manifest

**Files:**
- Create: `backend/internal/agentruntime/specialist.go`
- Create: `backend/internal/agentruntime/specialist_test.go`
- Modify: `backend/internal/agentruntime/runtime.go`
- Modify: `backend/internal/model/models.go`
- Modify: `backend/internal/skillcatalog/builtins.go`
- Modify: `backend/internal/skillcatalog/catalog.json`
- Modify: `backend/internal/database/skill_catalog_seed.go`
- Modify: `backend/internal/database/skill_catalog_schema_test.go`
- Modify: `backend/internal/service/skills.go`
- Modify: `backend/internal/service/agent_runtime_configuration.go`
- Test: `backend/internal/service/agent_runtime_context_test.go`

**Interfaces:**
- Consumes: current published `SkillVersion`, `AgentRun.ModelRecordID/ModelKey`, Task 1 refs.
- Produces: `SpecialistDefinition`, `SpecialistRequest`, `SpecialistResult`, `SkillCapabilityManifest`, frozen audio/vision/image/video selections, checkpoint reference fields.

- [ ] **Step 1: Write failing inheritance and manifest tests**

```go
func TestValidateSpecialistRequestRejectsDifferentParentModel(t *testing.T) {
    request := specialistRequestFixture()
    request.ParentModelRecordID = "other-record"
    err := agentruntime.ValidateSpecialistRequest(request, "parent-record", "deepseek-v4")
    require.ErrorIs(t, err, agentruntime.ErrSpecialistModelInheritance)
}

func TestResolveAgentRuntimeConfigurationRejectsSkillCapabilityMismatch(t *testing.T) {
    svc := serviceWithPublishedSkill(t, skillFixture("visual-evidence-analysis", `{"specialists":["visual"],"tools":["vision.analyze"]}`))
    _, err := svc.ResolveAgentRuntimeConfigurationForTest(ctx, userID, configurationSelectingSkillFor("narrative"))
    require.ErrorContains(t, err, "Skill Capability Manifest")
}
```

- [ ] **Step 2: Run focused tests and confirm RED**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'SpecialistRequest|SkillCapabilityMismatch|FreezesSeededFirstPartySkillVersion' -count=1`

Expected: new types/manifest validation are missing.

- [ ] **Step 3: Implement frozen registry/configuration contracts**

```go
type SkillCapabilityManifest struct {
    Specialists []SpecialistKey `json:"specialists"`
    Tools       []AgentToolName `json:"tools"`
    ArtifactSchemas []string    `json:"artifactSchemas"`
}

type GenerationModelSelections struct {
    Image  *GenerationModelSelection `json:"image,omitempty"`
    Video  *GenerationModelSelection `json:"video,omitempty"`
    Audio  *GenerationModelSelection `json:"audio,omitempty"`
    Vision *GenerationModelSelection `json:"vision,omitempty"`
}
```

Add `CapabilityManifestJSON` to immutable `SkillVersion`, parse it from the embedded catalog manifest, include it in version drift validation, and persist it during first-party seed. Do not infer capabilities from Skill text. Extend attachment freezing to authorized ready image/audio/video resources according to declared specialist input schema, with a total count/size limit and exact MIME checks.

- [ ] **Step 4: Run configuration and domain tests**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'Specialist|AgentRuntimeConfiguration|Skill' -count=1`

Expected: all pass; image/video behavior remains valid and audio/vision selections reject non-callable catalog records.

- [ ] **Step 5: Commit the frozen specialist contract**

```bash
git add backend/internal/agentruntime/specialist.go backend/internal/agentruntime/specialist_test.go backend/internal/agentruntime/runtime.go backend/internal/model/models.go backend/internal/skillcatalog/builtins.go backend/internal/skillcatalog/catalog.json backend/internal/database/skill_catalog_seed.go backend/internal/database/skill_catalog_schema_test.go backend/internal/service/skills.go backend/internal/service/agent_runtime_configuration.go backend/internal/service/agent_runtime_context_test.go
git commit -m "feat(agent): freeze specialist capabilities"
```

### Task 6: Persist and Execute Specialist Runs with Parent Model Billing

**Files:**
- Create: `backend/internal/repository/agent_specialist_run.go`
- Create: `backend/internal/service/agent_specialist_runtime.go`
- Create: `backend/internal/service/agent_specialist_runtime_test.go`
- Modify: `backend/internal/service/agent_runtime.go`
- Modify: `backend/internal/service/agent_runtime_model.go`

**Interfaces:**
- Consumes: `SpecialistRequest`, existing provider true-stream adapter, Agent text Task audience/internal billing, `AppendAgentEvent`.
- Produces: `RunSpecialist(ctx, scope, parentRun, request) (SpecialistCompletion, error)`, `agentSpecialistBillingKey`, persisted usage aggregation by main run/specialist/stage.

- [ ] **Step 1: Write failing model inheritance, invalid output and usage tests**

```go
func TestRunSpecialistUsesExactParentModelRecord(t *testing.T) {
    fixture := newSpecialistRuntimeFixture(t)
    completion, err := fixture.Service.RunSpecialist(fixture.Context, fixture.Scope, fixture.ParentRun, fixture.Request)
    require.NoError(t, err)
    require.Equal(t, fixture.ParentRun.ModelRecordID, completion.Run.ParentModelRecordID)
    require.Equal(t, fixture.ParentRun.ModelKey, completion.Run.ParentModelKey)
    require.Equal(t, int64(321), specialistUsage(t, fixture.DB, completion.Run.ID).TotalTokens)
}

func TestRunSpecialistRejectsMalformedStructuredOutput(t *testing.T) {
    fixture := newSpecialistRuntimeFixtureWithResponse(t, `{"summary":"missing artifacts"}`)
    _, err := fixture.Service.RunSpecialist(fixture.Context, fixture.Scope, fixture.ParentRun, fixture.Request)
    require.ErrorIs(t, err, service.ErrSpecialistOutputInvalid)
    require.Zero(t, artifactRevisionCount(t, fixture.DB))
}
```

- [ ] **Step 2: Run focused specialist tests and confirm RED**

Run: `cd backend && go test ./internal/service -run 'TestRunSpecialist' -count=1`

Expected: `RunSpecialist` is undefined.

- [ ] **Step 3: Implement one commercial path for specialist text inference**

```go
func agentSpecialistBillingKey(parentRunID string, specialistRunID string, attempt int) string {
    return fmt.Sprintf("agent-specialist:%s:%s:%d", parentRunID, specialistRunID, attempt)
}
```

Create a persistent SpecialistRun before invoking the model. Resolve the exact parent `ChannelModel` by `ModelRecordID`; verify `ModelKey`; create an internal-audience text Task and existing BillingOrder; stream provider output only into an internal buffer/diagnostic delta stream; strict-decode `SpecialistResult`; append validated Artifact revisions transactionally; aggregate usage to existing ledger/audit with main run, specialist run and stage identity. Do not surface raw specialist deltas or reasoning as user assistant text.

- [ ] **Step 4: Run specialist, token billing and model stream tests**

Run: `cd backend && go test ./internal/service -run 'Specialist|TokenBilling|AgentRuntimeModel|ProviderChatStream' -count=1`

Expected: exact inheritance, single billing identity, invalid-output failure and usage aggregation all pass.

- [ ] **Step 5: Commit Specialist Runtime**

```bash
git add backend/internal/repository/agent_specialist_run.go backend/internal/service/agent_specialist_runtime.go backend/internal/service/agent_specialist_runtime_test.go backend/internal/service/agent_runtime.go backend/internal/service/agent_runtime_model.go
git commit -m "feat(agent): execute persistent specialist runs"
```

### Task 7: Replace v3 Production Tools with Generic Delegation Ports

**Files:**
- Modify: `backend/internal/agentruntime/decision.go`
- Modify: `backend/internal/agentruntime/decision_test.go`
- Modify: `backend/internal/service/agent_runtime_tools.go`
- Create: `backend/internal/service/agent_runtime_tools_test.go`
- Modify: `backend/internal/service/agent_runtime_context.go`
- Modify: `backend/internal/service/agent_runtime_context_test.go`

**Interfaces:**
- Consumes: Task 5 registry and Task 6 executor.
- Produces: strict v3 argument DTOs for `specialist.delegate`, `vision.analyze`, `media.generate`, `canvas.project`; v2 old tools remain historical-only and cannot appear in v3 model context.

- [x] **Step 1: Write failing strict-tool tests**

```go
func TestParseV3DecisionRejectsLegacyProductionTool(t *testing.T) {
    _, err := agentruntime.ParseModelDecision([]byte(`{"type":"tool_call","toolCall":{"toolName":"production.plan","arguments":{}}}`), 3)
    require.ErrorIs(t, err, agentruntime.ErrModelDecisionInvalid)
}

func TestFreezeSpecialistDelegateRejectsUndeclaredTool(t *testing.T) {
    args := `{"specialistKey":"visual","toolAllowlist":["media.generate"],"expectedOutputSchema":"visual_evidence.v1"}`
    _, err := freezeSpecialistDelegateArguments(registryFixture(), []byte(args))
    require.ErrorIs(t, err, service.ErrSpecialistToolNotAllowed)
}
```

- [x] **Step 2: Run focused tool tests and confirm RED**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'V3Decision|SpecialistDelegate|AgentRuntimeTool' -count=1`

Expected: legacy tools are still accepted and generic tools are undefined.

- [x] **Step 3: Implement versioned strict decoding and frozen context**

```go
type SpecialistDelegateArguments struct {
    SpecialistKey        agentruntime.SpecialistKey         `json:"specialistKey"`
    Objective            string                             `json:"objective"`
    InputRevisions       []agentruntime.ArtifactRevisionRef `json:"inputRevisions"`
    SkillDirs            []string                           `json:"skillDirs"`
    ToolAllowlist        []agentruntime.AgentToolName       `json:"toolAllowlist"`
    ExpectedOutputSchema string                             `json:"expectedOutputSchema"`
    ExpectedDelivery     agentruntime.ExpectedDelivery      `json:"expectedDelivery"`
}
```

Each tool gets one decoder with `DisallowUnknownFields`, trailing-data rejection, exact frozen facts and structural validation. `agentRuntimeModelPrompt` includes current immutable graph version, current stage, exact Artifact summaries, prior verifier result and callable tools/models; it no longer serializes `agentRuntimeProductionPlanFact` for v3.

- [x] **Step 4: Run all decision/context/tool tests**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'Decision|AgentRuntimeContext|AgentRuntimeTool|SpecialistDelegate' -count=1`

Expected: v3 exposes only five locked tools; v2 historical parsing remains read-only and cannot create new work.

- [ ] **Step 5: Commit the generic orchestration ports**

```bash
git add backend/internal/agentruntime/decision.go backend/internal/agentruntime/decision_test.go backend/internal/service/agent_runtime_tools.go backend/internal/service/agent_runtime_tools_test.go backend/internal/service/agent_runtime_context.go backend/internal/service/agent_runtime_context_test.go
git commit -m "refactor(agent): use generic production tools"
```

### Task 8: Make Stop, Recovery and Late Results Span the Whole Run Tree

**Files:**
- Create: `backend/internal/service/agent_production_recovery_test.go`
- Modify: `backend/internal/repository/agent_specialist_run.go`
- Modify: `backend/internal/repository/agent_runtime.go`
- Modify: `backend/internal/service/agent_runtime.go`
- Modify: `backend/internal/service/agent_runtime_coordinator.go`
- Modify: `backend/internal/service/agent_runtime_transport.go`

**Interfaces:**
- Consumes: existing interrupt/reject flow, SpecialistRun rows, checkpoint references.
- Produces: `CancelAgentRunTree`, `RecoverAgentRunTree`, late-result `unadopted` persistence, checkpoint validation of frozen graph/model/Skill/tool versions.

- [ ] **Step 1: Write failing cancellation/restart/late-result tests**

```go
func TestRejectCostApprovalCancelsMainAndSpecialists(t *testing.T) {
    fixture := runningAgentTreeFixture(t)
    require.NoError(t, fixture.Service.ResolveAgentApproval(fixture.Scope, fixture.ApprovalID, false))
    require.Equal(t, agentruntime.RunCancelled, readMainRun(t, fixture).Status)
    require.Equal(t, model.AgentSpecialistRunCancelled, readSpecialistRun(t, fixture).Status)
    require.Zero(t, mediaTaskCountAfter(t, fixture.DB, fixture.RejectedAt))
}

func TestLateProviderSuccessPersistsUnadoptedArtifact(t *testing.T) {
    fixture := stoppedSubmittedMediaFixture(t)
    require.NoError(t, fixture.Service.ApplyProviderCompletion(fixture.Callback))
    revision := latestArtifactRevision(t, fixture.DB)
    require.Equal(t, "unadopted", revision.LifecycleStatus)
    require.NotEmpty(t, revision.ResourceID)
}
```

- [ ] **Step 2: Run focused recovery tests and confirm RED**

Run: `cd backend && go test ./internal/service -run 'RejectCostApprovalCancelsMainAndSpecialists|LateProviderSuccess|RecoverAgentRunTree' -count=1`

Expected: specialist rows remain running or late result cannot be represented.

- [ ] **Step 3: Implement tree cancellation and strict checkpoint recovery**

`CancelAgentRunTree` performs one database transaction for main status, all nonterminal specialist statuses, pending approval resolution and persisted terminal events. It cancels in-memory contexts after the commit. Tool/task creation checks durable run-tree status immediately before side effects. Recovery verifies graph version, pending stage/revision, each active specialist, exact model record, Skill version/checksum, tool schema and last sequence; any missing frozen fact fails with a stable code.

- [ ] **Step 4: Run recovery, approval, transport and duplicate callback tests**

Run: `cd backend && go test ./internal/service -run 'Recovery|Resume|Approval|Interrupt|LateProvider|DuplicateCallback' -count=1`

Expected: stop/reject is durable and idempotent; late results preserve Resource/Billing facts without changing the active graph revision.

- [ ] **Step 5: Commit run-tree lifecycle handling**

```bash
git add backend/internal/service/agent_production_recovery_test.go backend/internal/repository/agent_specialist_run.go backend/internal/repository/agent_runtime.go backend/internal/service/agent_runtime.go backend/internal/service/agent_runtime_coordinator.go backend/internal/service/agent_runtime_transport.go
git commit -m "fix(agent): close specialist run lifecycles"
```

### M2 Gate

- [ ] Run `cd backend && go test ./internal/agentruntime ./internal/repository ./internal/service -run 'Agent|Specialist|Runtime' -count=1`.
- [ ] Run affected race packages: `cd backend && go test -race ./internal/repository ./internal/service -run 'Specialist|Recovery|Approval' -count=1`.
- [ ] Confirm ordinary user task APIs never show specialist/internal model Tasks; admin billing/audit still resolve their main/specialist/stage identities.
- [ ] Review budget: 8–12 production files and approximately 800–1,100 net-new production lines.

---

## M3 — Narrative, Asset Review, Publication and Shared Vision

### Task 9: Add Script Bundle and Asset Binding Artifact Schemas

**Files:**
- Modify: `backend/internal/agentruntime/artifact.go`
- Modify: `backend/internal/agentruntime/artifact_test.go`
- Create: `backend/internal/service/agent_stage_review.go`
- Create: `backend/internal/service/agent_stage_review_test.go`
- Modify: `backend/internal/service/agent_specialist_runtime.go`

**Interfaces:**
- Consumes: `ArtifactDraft`, Specialist Runtime, graph/stage repository.
- Produces: strict `script_bundle.v1`, `asset_binding.v1`, `ScriptBundle`, `AssetBindingSet`, initial user-visible review stage and revision-request loop.

- [ ] **Step 1: Write failing first-delivery and binding tests**

```go
func TestScriptBundleRequiresCompleteScriptAndCharacterCatalog(t *testing.T) {
    draft := artifactDraft("script_bundle.v1", `{"title":"雨夜","script":"","characters":[]}`)
    require.ErrorIs(t, agentruntime.ValidateArtifactDraft(draft), agentruntime.ErrArtifactPayloadInvalid)
}

func TestAssetBindingRejectsUnconfirmedOrCrossScopeResource(t *testing.T) {
    fixture := stageReviewFixture(t)
    _, err := fixture.Service.AppendAssetBindingRevision(fixture.Scope, fixture.StageID, assetBindingWithResource("other-user-resource"))
    require.ErrorIs(t, err, service.ErrAssetBindingUnconfirmed)
}

func TestFirstVisibleReviewIsScriptBundle(t *testing.T) {
    fixture := firstProductionStageFixture(t)
    item := firstUserVisibleTimelineItem(t, fixture.DB, fixture.Scope.RunID)
    require.Equal(t, "artifact_review", item.ContentType)
    require.Equal(t, "script_bundle.v1", item.ArtifactSchema)
}

func TestGuidedAndAutomaticBothStopAtUserVisibleStageReview(t *testing.T) {
    for _, mode := range []agentruntime.ExecutionMode{agentruntime.ExecutionGuided, agentruntime.ExecutionAutomatic} {
        fixture := firstProductionStageFixtureForMode(t, mode)
        require.Equal(t, agentruntime.StageAwaitingReview, currentStageStatus(t, fixture.DB, fixture.StageID))
        require.Zero(t, paidTaskCount(t, fixture.DB, fixture.Scope.RunID))
    }
}
```

- [ ] **Step 2: Run focused schema/stage tests and confirm RED**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'ScriptBundle|AssetBinding|FirstVisibleReview|GuidedAndAutomatic' -count=1`

Expected: schema dispatch and stage application service are absent.

- [ ] **Step 3: Implement strict schemas and revision loop**

```go
type ScriptBundle struct {
    Title      string             `json:"title"`
    Logline    string             `json:"logline"`
    Script     string             `json:"script"`
    Characters []CharacterNeed    `json:"characters"`
    Scenes     []SceneNeed        `json:"scenes"`
    Props      []PropNeed         `json:"props"`
    VoiceRoles []VoiceRoleNeed    `json:"voiceRoles"`
}

type AssetBindingSet struct {
    ScriptRevision ArtifactRevisionRef `json:"scriptRevision"`
    Entries        []AssetBindingEntry `json:"entries"`
}
```

Validation checks non-empty identifiers, unique stable keys, exact refs and binding state (`matched`, `missing`, `conflict`, `choice_required`). It must not infer a binding from names. A revision request moves the same stage to `running`, records the exact user comment as a private main-agent fact, delegates a fresh specialist call, and appends a new revision without overwriting the previous one. `guided` may add extra confirmation for non-user-visible write tools while `automatic` may run safe internal preparation continuously, but both modes must stop at every user-visible content review and every paid quote approval.

- [ ] **Step 4: Run schema, specialist and stage tests**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'ScriptBundle|AssetBinding|FirstVisibleReview|GuidedAndAutomatic|StageRevision' -count=1`

Expected: complete script/character bundle is the first visible review item; only exact confirmed binding revisions can satisfy downstream input.

- [ ] **Step 5: Commit Narrative/Asset artifact contracts**

```bash
git add backend/internal/agentruntime/artifact.go backend/internal/agentruntime/artifact_test.go backend/internal/service/agent_stage_review.go backend/internal/service/agent_stage_review_test.go backend/internal/service/agent_specialist_runtime.go
git commit -m "feat(agent): review scripts and asset bindings"
```

### Task 10: Expose Exact Stage Review and Artifact Revision APIs

**Files:**
- Create: `backend/internal/handler/agent_production.go`
- Create: `backend/internal/handler/agent_production_test.go`
- Modify: `backend/internal/handler/agent_runtime.go`
- Modify: `backend/internal/service/agent_runtime_transport.go`
- Modify: `backend/internal/repository/agent_artifact.go`

**Interfaces:**
- Consumes: `StageReviewCommand`, scoped Artifact reads, persisted timeline.
- Produces: `POST /api/agent/runs/:runId/stages/:stageId/reviews` and `GET /api/agent/runs/:runId/artifacts/:artifactId/revisions/:revisionId`.

- [ ] **Step 1: Write failing HTTP contract tests**

```go
func TestStageReviewRejectsStaleRevisionAndDuplicateIdentity(t *testing.T) {
    app := agentProductionHTTPFixture(t)
    body := `{"stageVersion":3,"revisionId":"rev-old","decision":"approved","clientRequestId":"review-1","comment":""}`
    response := app.Post("/api/agent/runs/run-1/stages/stage-1/reviews", body, userToken)
    require.Equal(t, http.StatusConflict, response.Code)
    require.JSONEq(t, `{"error":{"code":"stage_approval_revision_mismatch"}}`, errorEnvelope(response.Body.Bytes()))
}

func TestArtifactRevisionReadIsScopeBound(t *testing.T) {
    app := agentProductionHTTPFixture(t)
    response := app.Get("/api/agent/runs/run-1/artifacts/a-other/revisions/r-other", userToken)
    require.Equal(t, http.StatusNotFound, response.Code)
}
```

- [ ] **Step 2: Run handler tests and confirm RED**

Run: `cd backend && go test ./internal/handler -run 'StageReview|ArtifactRevisionRead' -count=1`

Expected: routes return 404 because they are not registered.

- [ ] **Step 3: Implement strict request/response DTOs and event ordering**

```go
type stageReviewRequest struct {
    StageVersion   int64                            `json:"stageVersion"`
    RevisionID     string                           `json:"revisionId"`
    Decision       agentruntime.StageReviewDecision `json:"decision"`
    ClientRequestID string                          `json:"clientRequestId"`
    Comment        string                           `json:"comment"`
}
```

Decode with `DisallowUnknownFields` and EOF enforcement. Approval, revision request or stop must update domain facts and append `approval.resolved` / `item.completed` / `run.interrupted` events with sequence in one transaction before response. Repeat `clientRequestId` returns the original result; a different identity against an already-resolved stage returns conflict.

- [ ] **Step 4: Run handler, transport and repository tests**

Run: `cd backend && go test ./internal/handler ./internal/service ./internal/repository -run 'StageReview|ArtifactRevision|AgentRuntimeTransport' -count=1`

Expected: exact revision approval works; stale/cross-scope/unknown-field requests fail with stable codes.

- [ ] **Step 5: Commit the stage review API**

```bash
git add backend/internal/handler/agent_production.go backend/internal/handler/agent_production_test.go backend/internal/handler/agent_runtime.go backend/internal/service/agent_runtime_transport.go backend/internal/repository/agent_artifact.go
git commit -m "feat(agent): expose stage review commands"
```

### Task 11: Publish Confirmed Media into the Project Asset Library Idempotently

**Files:**
- Create: `backend/internal/repository/agent_asset_publication.go`
- Create: `backend/internal/repository/agent_asset_publication_test.go`
- Create: `backend/internal/service/agent_asset_publication.go`
- Create: `backend/internal/service/agent_asset_publication_test.go`
- Modify: `backend/internal/service/project_asset.go`
- Modify: `backend/internal/service/agent_stage_review.go`

**Interfaces:**
- Consumes: exact approved Artifact revision, Resource, existing Asset/AssetVersion/Representation/ProjectAssetLink domain.
- Produces: `PublishAsset(ctx, scope, command) (AssetPublicationResult, error)` and unique `PublicationIdentity`.

- [ ] **Step 1: Write failing idempotency, permission and rollback tests**

```go
func TestPublishAssetIsIdempotentForExactPurpose(t *testing.T) {
    fixture := publishableArtifactFixture(t, "character_reference")
    first, err := fixture.Service.PublishAsset(fixture.Context, fixture.Scope, fixture.Command)
    require.NoError(t, err)
    second, err := fixture.Service.PublishAsset(fixture.Context, fixture.Scope, fixture.Command)
    require.NoError(t, err)
    require.Equal(t, first.PublicationID, second.PublicationID)
    require.Equal(t, int64(1), assetCountForRevision(t, fixture.DB, fixture.RevisionID))
}

func TestPublishAssetTransactionFailureDoesNotClaimPublished(t *testing.T) {
    fixture := publishableArtifactFixtureWithRepresentationFailure(t)
    _, err := fixture.Service.PublishAsset(fixture.Context, fixture.Scope, fixture.Command)
    require.ErrorIs(t, err, service.ErrAssetPublicationFailed)
    require.Zero(t, publicationSuccessCount(t, fixture.DB))
    require.NotEmpty(t, exactArtifactRevision(t, fixture.DB).ResourceID)
}
```

- [ ] **Step 2: Run focused publication tests and confirm RED**

Run: `cd backend && go test ./internal/repository ./internal/service -run 'PublishAsset|AssetPublication' -count=1`

Expected: publication port and transaction do not exist.

- [ ] **Step 3: Implement a single publication transaction**

```go
type PublishAssetCommand struct {
    ArtifactRevisionID string
    PublicationPurpose string
    TargetCategory     model.AssetCategory
    TargetBindingKey   string
    ApprovedByUserID   string
    StageReviewID      string
}
```

Validate scope, stage approval, exact revision, ready Resource, declared publication intent and approver authorization. In one repository transaction create/reuse Asset, confirmed AssetVersion, AssetRepresentation pointing at the existing Resource, ProjectAssetLink, binding source metadata and AgentAssetPublication. Store model/parameters/prompt version/fee/content hash/audit identity. Do not upload or copy the object. Append success event only after commit; on failure append `asset_publication_failed` while leaving generated Artifact intact.

- [ ] **Step 4: Run asset, transaction and duplicate request tests**

Run: `cd backend && go test ./internal/repository ./internal/service -run 'ProjectAsset|PublishAsset|AssetPublication' -count=1`

Expected: character/scene/prop/independent-audio publication is idempotent; duplicate confirmation produces one asset version and one publication.

- [ ] **Step 5: Commit the asset publication port**

```bash
git add backend/internal/repository/agent_asset_publication.go backend/internal/repository/agent_asset_publication_test.go backend/internal/service/agent_asset_publication.go backend/internal/service/agent_asset_publication_test.go backend/internal/service/project_asset.go backend/internal/service/agent_stage_review.go
git commit -m "feat(agent): publish approved artifacts"
```

### Task 12: Add Shared Vision Analysis with Real Resource Evidence

**Files:**
- Create: `backend/internal/agentruntime/visual_evidence.go`
- Create: `backend/internal/agentruntime/visual_evidence_test.go`
- Create: `backend/internal/service/agent_visual_analysis.go`
- Create: `backend/internal/service/agent_visual_analysis_test.go`
- Modify: `backend/internal/service/agent_runtime_configuration.go`
- Modify: `backend/internal/service/agent_runtime_tools.go`

**Interfaces:**
- Consumes: exact source Artifact revision/Resource URL, selected callable vision model, existing provider Task/Billing path when billable.
- Produces: `VisualEvidenceArtifact` (`visual_evidence.v1`), `AnalyzeVisualEvidence`, stale-on-source-change behavior.

- [ ] **Step 1: Write failing real-resource, scope and stale tests**

```go
func TestAnalyzeVisualEvidenceRequiresReadyImageResource(t *testing.T) {
    fixture := visualAnalysisFixture(t, resourceStatusQueued)
    _, err := fixture.Service.AnalyzeVisualEvidence(fixture.Context, fixture.Scope, fixture.Command)
    require.ErrorIs(t, err, service.ErrVisualAnalysisInputUnavailable)
}

func TestVisualEvidenceBecomesStaleWhenSourceRevisionChanges(t *testing.T) {
    fixture := completedVisualEvidenceFixture(t)
    require.NoError(t, fixture.Repository.AppendArtifactRevision(fixture.Scope, fixture.SourceArtifactID, 1, fixture.NewSource))
    require.Equal(t, "stale", exactArtifactRevision(t, fixture.DB, fixture.EvidenceRevisionID).LifecycleStatus)
}
```

- [ ] **Step 2: Run focused vision tests and confirm RED**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'VisualEvidence|AnalyzeVisual' -count=1`

Expected: visual evidence schema/service are absent.

- [ ] **Step 3: Implement strict visual evidence and provider call**

```go
type VisualEvidence struct {
    SourceRevision       ArtifactRevisionRef `json:"sourceRevision"`
    Characters           []VisualCharacter   `json:"characters"`
    IdentityEvidence     []IdentityEvidence  `json:"identityEvidence"`
    Scene                VisualScene         `json:"scene"`
    Props                []VisualProp        `json:"props"`
    SpatialRelations     []SpatialRelation   `json:"spatialRelations"`
    Shot                 VisualShotEvidence  `json:"shot"`
    ActionState          string              `json:"actionState"`
    OCRText              []string            `json:"ocrText"`
    Uncertainties        []EvidenceIssue     `json:"uncertainties"`
    Conflicts            []EvidenceIssue     `json:"conflicts"`
    ConfidenceBasisPoints int                `json:"confidenceBasisPoints"`
    VisionModelRecordID  string              `json:"visionModelRecordId"`
    RequestIdentity      string              `json:"requestIdentity"`
}
```

Resolve the Resource in scope and derive its current URL only at execution. The model performs semantic analysis; local code validates structure, confidence range, referenced stable keys and source revision. Billable batches freeze model/count/quote and wait for existing cost approval; nonbillable provider configurations still create auditable request facts.

- [ ] **Step 4: Run visual analysis, pricing and permission tests**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'VisualEvidence|AnalyzeVisual|Quote|Permission' -count=1`

Expected: prompt-only/placeholder/cross-scope input fails; exact source produces a persisted evidence Artifact and source changes mark it stale.

- [ ] **Step 5: Commit shared vision analysis**

```bash
git add backend/internal/agentruntime/visual_evidence.go backend/internal/agentruntime/visual_evidence_test.go backend/internal/service/agent_visual_analysis.go backend/internal/service/agent_visual_analysis_test.go backend/internal/service/agent_runtime_configuration.go backend/internal/service/agent_runtime_tools.go
git commit -m "feat(agent): persist visual evidence"
```

### M3 Gate

- [ ] Run `cd backend && go test ./internal/agentruntime ./internal/repository ./internal/service ./internal/handler -run 'Script|Asset|Stage|Visual|Publication' -count=1`.
- [ ] Run affected PostgreSQL publication/CAS transaction tests when configured.
- [ ] Verify one simulated flow: script revision → revision request → approved script → asset binding review → generated character Artifact approval → one Asset publication.
- [ ] Confirm no media Task exists before frozen cost approval and no publication success appears before the transaction commits.
- [ ] Review budget: 10–14 production files and approximately 900–1,300 net-new production lines.

---

## M4 — Storyboard, Visual Language and ViMax-Derived Skills

### Task 13: Publish Six Governed ViMax-Derived Skills

**Files:**
- Create: `backend/internal/skillcatalog/catalog/character-visual-bible/SKILL.md`
- Create: `backend/internal/skillcatalog/catalog/storyboard-cinematic-language/SKILL.md`
- Create: `backend/internal/skillcatalog/catalog/camera-tree-continuity/SKILL.md`
- Create: `backend/internal/skillcatalog/catalog/first-motion-last-frame/SKILL.md`
- Create: `backend/internal/skillcatalog/catalog/visual-consistency-review/SKILL.md`
- Create: `backend/internal/skillcatalog/catalog/visual-evidence-analysis/SKILL.md`
- Create: `THIRD_PARTY_NOTICES.md`
- Modify: `backend/internal/skillcatalog/catalog.json`
- Modify: `backend/internal/skillcatalog/builtins.go`
- Modify: `backend/internal/skillcatalog/builtins_test.go`
- Modify: `backend/internal/database/skill_catalog_schema_test.go`

**Interfaces:**
- Consumes: existing immutable `SkillVersion`, source/license/checksum governance, Task 5 capability manifest.
- Produces: six published version-1 Skills with exact source revision, MIT license, checksum and declared specialist/tool/schema capabilities.

- [ ] **Step 1: Write failing catalog governance tests**

```go
func TestViMaxDerivedSkillsArePublishedWithPinnedSourceAndManifest(t *testing.T) {
    catalog := seededSkillCatalog(t)
    for _, dir := range []string{"character-visual-bible", "storyboard-cinematic-language", "camera-tree-continuity", "first-motion-last-frame", "visual-consistency-review", "visual-evidence-analysis"} {
        skill := catalog.MustPublished(dir)
        require.Equal(t, "MIT", skill.SourceLicense)
        require.Contains(t, skill.SourceRevision, "05a48943878312d88fe5a016c12a9654940ecc43")
        require.Len(t, skill.Checksum, 64)
        require.NotEmpty(t, skill.CapabilityManifestJSON)
    }
}
```

- [ ] **Step 2: Run Skill seed tests and confirm RED**

Run: `cd backend && go test ./internal/database ./internal/service -run 'Skill|ViMaxDerived' -count=1`

Expected: the six governed Skills are missing.

- [ ] **Step 3: Write complete Skill instructions and seed immutable versions**

Each `SKILL.md` must state inputs, outputs, evidence requirements, prohibited assumptions, revision rules and failure behavior. Keep creative methods in Skills; do not add these methods to `hono-api`, Go system prompts or Web. `THIRD_PARTY_NOTICES.md` records repository URL, pinned commit, MIT copyright/license, which ideas/text were adapted, and that ViMax Python/LangChain Runtime is not included.

Manifest example:

```json
{"specialists":["visual","storyboard"],"tools":["vision.analyze"],"artifactSchemas":["character_visual_bible.v1","visual_evidence.v1"]}
```

- [ ] **Step 4: Run Skill catalog/checksum/license tests**

Run: `cd backend && go test ./internal/database ./internal/service -run 'Skill|ViMaxDerived|Checksum|License' -count=1`

Expected: all six versions publish; checksum mismatch, missing license or undeclared tool dependency fails.

- [ ] **Step 5: Commit governed Skills and notice**

Stage only the six embedded Skill directories, catalog manifest/builtin validation, tests and notice, then commit:

```bash
git add backend/internal/skillcatalog/catalog/character-visual-bible backend/internal/skillcatalog/catalog/storyboard-cinematic-language backend/internal/skillcatalog/catalog/camera-tree-continuity backend/internal/skillcatalog/catalog/first-motion-last-frame backend/internal/skillcatalog/catalog/visual-consistency-review backend/internal/skillcatalog/catalog/visual-evidence-analysis
git add backend/internal/skillcatalog/catalog.json backend/internal/skillcatalog/builtins.go backend/internal/skillcatalog/builtins_test.go backend/internal/database/skill_catalog_schema_test.go THIRD_PARTY_NOTICES.md
git commit -m "feat(skills): add cinematic continuity methods"
```

### Task 14: Validate Character Bible, Cinematic Shot and Camera Tree Schemas

**Files:**
- Modify: `backend/internal/agentruntime/visual_evidence.go`
- Modify: `backend/internal/agentruntime/visual_evidence_test.go`
- Modify: `backend/internal/agentruntime/artifact.go`
- Modify: `backend/internal/agentruntime/artifact_test.go`

**Interfaces:**
- Consumes: exact AssetBindingRevision and VisualEvidence refs.
- Produces: `character_visual_bible.v1`, `storyboard_plan.v1`, `camera_tree.v1`, `first_motion_last_frame.v1` structural validators.

- [ ] **Step 1: Write failing graph and frame-state tests**

```go
func TestCameraTreeRejectsSelfReferenceAndCycle(t *testing.T) {
    tree := agentruntime.CameraTree{Cameras: []agentruntime.CameraNode{
        {CameraKey: "wide", ParentCameraKey: "close"},
        {CameraKey: "close", ParentCameraKey: "wide"},
    }}
    require.ErrorIs(t, agentruntime.ValidateCameraTree(tree), agentruntime.ErrCameraTreeCycle)
}

func TestFirstMotionLastFrameRequiresStaticBoundaryStates(t *testing.T) {
    value := agentruntime.FirstMotionLastFrame{FirstFrame: agentruntime.FrameState{State: "奔跑中"}, Motion: "跑到门口", LastFrame: agentruntime.FrameState{State: "站在门口"}}
    require.ErrorIs(t, agentruntime.ValidateFirstMotionLastFrame(value), agentruntime.ErrFrameBoundaryInvalid)
}
```

- [ ] **Step 2: Run focused schema tests and confirm RED**

Run: `cd backend && go test ./internal/agentruntime -run 'CharacterVisualBible|CinematicShot|CameraTree|FirstMotionLastFrame' -count=1`

Expected: schema types/validators are absent.

- [ ] **Step 3: Implement structural validators without semantic keyword rules**

```go
type CinematicShot struct {
    ShotKey              string                `json:"shotKey"`
    NarrativePurpose     string                `json:"narrativePurpose"`
    ShotSize             string                `json:"shotSize"`
    CameraPosition       string                `json:"cameraPosition"`
    Angle                string                `json:"angle"`
    Composition          string                `json:"composition"`
    ScreenDirection      string                `json:"screenDirection"`
    CameraMotion         string                `json:"cameraMotion"`
    OnScreenAction       string                `json:"onScreenAction"`
    Dialogue             []DialogueLine        `json:"dialogue"`
    Sound                []SoundCue            `json:"sound"`
    DurationMS           int                   `json:"durationMs"`
    Transition           string                `json:"transition"`
    VisibleCharacterKeys []string              `json:"visibleCharacterKeys"`
    InputRevisions       []ArtifactRevisionRef `json:"inputRevisions"`
    FramePlan            FirstMotionLastFrame  `json:"framePlan"`
}
```

Local validation checks presence, key uniqueness, reference existence, graph acyclicity, duration bounds and that first/motion/last fields are distinct non-empty schema slots. Whether a phrase is truly static, whether axis/screen direction is correct and whether framing is cinematic remains a Specialist semantic result recorded as evidence, not a local text match.

- [ ] **Step 4: Run all Artifact/visual schema tests**

Run: `cd backend && go test ./internal/agentruntime -count=1`

Expected: invalid refs/cycles/bounds fail; valid multi-root tree only passes when roots are explicitly marked independent.

- [ ] **Step 5: Commit visual production schemas**

```bash
git add backend/internal/agentruntime/visual_evidence.go backend/internal/agentruntime/visual_evidence_test.go backend/internal/agentruntime/artifact.go backend/internal/agentruntime/artifact_test.go
git commit -m "feat(agent): validate cinematic artifacts"
```

### Task 15: Preserve Every Media Candidate and Append Consistency Evidence

**Files:**
- Modify: `backend/internal/repository/agent_artifact.go`
- Modify: `backend/internal/repository/agent_artifact_test.go`
- Modify: `backend/internal/service/agent_visual_analysis.go`
- Modify: `backend/internal/service/agent_visual_analysis_test.go`
- Modify: `backend/internal/service/agent_stage_review.go`

**Interfaces:**
- Consumes: media Task results, CharacterBible/Binding/VisualEvidence exact revisions.
- Produces: candidate ledger, recommendation evidence, no-delete/no-overwrite guarantee, selected-candidate approval by exact revision.

- [ ] **Step 1: Write failing all-candidate preservation tests**

```go
func TestConsistencyReviewKeepsAllGeneratedCandidates(t *testing.T) {
    fixture := generatedCandidateFixture(t, 4)
    result, err := fixture.Service.ReviewVisualCandidates(fixture.Context, fixture.Scope, fixture.Command)
    require.NoError(t, err)
    require.Len(t, artifactCandidates(t, fixture.DB, fixture.ArtifactID), 4)
    require.Len(t, result.RankedCandidateRevisionIDs, 4)
}

func TestApprovingCandidateNeverDeletesNonSelectedResources(t *testing.T) {
    fixture := generatedCandidateFixture(t, 3)
    require.NoError(t, fixture.Service.ApproveStageCandidate(fixture.Scope, fixture.StageID, fixture.RevisionIDs[1]))
    require.Equal(t, int64(3), resourceCount(t, fixture.DB, fixture.ResourceIDs))
}
```

- [ ] **Step 2: Run candidate tests and confirm RED**

Run: `cd backend && go test ./internal/repository ./internal/service -run 'ConsistencyReviewKeeps|ApprovingCandidateNeverDeletes' -count=1`

Expected: candidate/evidence relationships are not implemented.

- [ ] **Step 3: Implement append-only candidate and review evidence**

Each provider output becomes its own immutable Artifact revision or candidate child Artifact with ResourceID and request identity. Visual consistency creates a `visual_consistency_review.v1` Artifact referencing all candidates and the confirmed reference revisions. Recommendation order and deviations are evidence only. Approval records one selected exact revision for downstream dependency and leaves all nonselected facts reachable in the gallery/history.

- [ ] **Step 4: Run candidate, Resource and stage approval tests**

Run: `cd backend && go test ./internal/repository ./internal/service -run 'Candidate|Consistency|Resource|StageApproval' -count=1`

Expected: all candidates survive success, review, approval, retry and upstream stale transitions.

- [ ] **Step 5: Commit append-only candidate handling**

```bash
git add backend/internal/repository/agent_artifact.go backend/internal/repository/agent_artifact_test.go backend/internal/service/agent_visual_analysis.go backend/internal/service/agent_visual_analysis_test.go backend/internal/service/agent_stage_review.go
git commit -m "feat(agent): retain media candidate evidence"
```

### M4 Gate

- [ ] Run `cd backend && go test ./internal/agentruntime ./internal/database ./internal/repository ./internal/service -run 'Skill|Visual|Storyboard|Camera|Candidate|Consistency' -count=1`.
- [ ] Inspect runtime prompt assembly and prove no `docs/`, temporary ViMax path or fixed specialist order is loaded.
- [ ] Verify third-party notice, source revision, license and each Skill checksum.
- [ ] Review budget: 10–14 production files and approximately 1,000–1,500 net-new production lines.

---

## M5 — Media Generation, Optional Audio and Final Assembly

### Task 16: Freeze Dynamic Media and Native-Audio Capabilities

**Files:**
- Modify: `backend/internal/service/agent_runtime_context.go`
- Modify: `backend/internal/service/agent_runtime_context_test.go`
- Modify: `backend/internal/service/admin.go`
- Modify: `backend/internal/service/model_access_test.go`
- Modify: `web/src/services/api/wallet.ts`
- Modify: `web/src/stores/use-config-store.ts`
- Modify: `web/test/video-model-capabilities.test.ts`

**Interfaces:**
- Consumes: existing `ChannelModel`, `PublicProviderCapabilities`, price tiers and frozen run configuration.
- Produces: exact callable capability facts for reference types, duration/resolution, native audio/dialogue/voice/lip-sync and independent audio generation.

- [ ] **Step 1: Write failing capability round-trip tests**

```go
func TestCallableModelFactsPreserveNativeAudioCapabilities(t *testing.T) {
    model := channelModelWithCapabilities(t, `{"supportsNativeAudio":true,"supportsDialogue":true,"supportsVoiceReference":false,"supportsLipSync":true,"durations":[5,10,15]}`)
    facts := callableModelFacts(t, model)
    require.True(t, facts[0].ProviderCapabilities.SupportsNativeAudio)
    require.True(t, facts[0].ProviderCapabilities.SupportsDialogue)
    require.Equal(t, []int{5, 10, 15}, facts[0].ProviderCapabilities.Durations)
}

func TestVideoWithNativeAudioDoesNotRequireAudioModelSelection(t *testing.T) {
    require.NoError(t, validateMediaDeliveryCapability(nativeAudioVideoModel(), nil, confirmedNativeAudioDelivery()))
}
```

- [ ] **Step 2: Run focused catalog/context tests and confirm RED**

Run: `cd backend && go test ./internal/service -run 'CallableModelFactsPreserveNativeAudio|NativeAudio|ProviderCapabilities' -count=1`

Expected: capability fields are absent or lost from callable facts.

- [ ] **Step 3: Extend the dynamic capability contract**

```go
type PublicProviderCapabilities struct {
    Resolutions               []string `json:"resolutions"`
    Durations                 []int    `json:"durations"`
    InputVariants             []string `json:"inputVariants"`
    SupportsTextToVideo       bool     `json:"supportsTextToVideo"`
    SupportsImageToVideo      bool     `json:"supportsImageToVideo"`
    SupportsReferenceVideo    bool     `json:"supportsReferenceVideo"`
    SupportsNativeAudio       bool     `json:"supportsNativeAudio"`
    SupportsDialogue          bool     `json:"supportsDialogue"`
    SupportsVoiceReference    bool     `json:"supportsVoiceReference"`
    SupportsLipSync           bool     `json:"supportsLipSync"`
    SupportsIndependentAudio  bool     `json:"supportsIndependentAudio"`
}
```

The service derives these facts from the model catalog/vendor configuration and freezes them into the model prompt and quote. Web only renders returned values. No Agent/Web model-name table or implicit default is allowed.

- [ ] **Step 4: Run backend and Web parser tests**

Run: `cd backend && go test ./internal/service -run 'CallableModel|ProviderCapabilities|AgentRuntimeContext' -count=1`

Run: `cd web && bun test test/video-model-capabilities.test.ts`

Expected: backend and TypeScript agree on every capability field; unknown fields fail in strict runtime parsers where applicable.

- [ ] **Step 5: Commit dynamic capability facts**

```bash
git add backend/internal/service/agent_runtime_context.go backend/internal/service/agent_runtime_context_test.go backend/internal/service/admin.go backend/internal/service/model_access_test.go
git add web/src/services/api/wallet.ts web/src/stores/use-config-store.ts web/test/video-model-capabilities.test.ts
git commit -m "feat(models): expose media delivery capabilities"
```

### Task 17: Unify Image, Video, Audio and Billable Vision Quote/Task Execution

**Files:**
- Create: `backend/internal/service/agent_media_generation.go`
- Create: `backend/internal/service/agent_media_generation_test.go`
- Modify: `backend/internal/service/billing_quote.go`
- Modify: `backend/internal/service/billing_quote_test.go`
- Modify: `backend/internal/service/agent_runtime_tools.go`
- Modify: `backend/internal/service/agent_runtime_render_task.go`
- Modify: `backend/internal/repository/task_billing.go`

**Interfaces:**
- Consumes: callable model facts, exact Artifact attempt, existing `BillingQuote`, `BillingOrder`, `Task`, Resource/provider callbacks.
- Produces: `FreezeMediaQuote`, `ApproveMediaAttempt`, `EnsureMediaTask`, one idempotency identity across retries/replays/restarts.

- [ ] **Step 1: Write failing quote-mismatch and duplicate-execution tests**

```go
func TestMediaApprovalIsBoundToExactAttemptAndFrozenParameters(t *testing.T) {
    fixture := mediaQuoteFixture(t, "video", 15, "1080p", 2)
    quote := fixture.Service.MustFreezeMediaQuote(fixture.Command)
    changed := fixture.Command
    changed.DurationSeconds = 10
    _, err := fixture.Service.ApproveMediaAttempt(fixture.Scope, quote.ID, changed)
    require.ErrorIs(t, err, service.ErrCostApprovalQuoteMismatch)
}

func TestRepeatedMediaApprovalCreatesOneTaskAndReservation(t *testing.T) {
    fixture := approvedMediaFixture(t)
    first, err := fixture.Service.EnsureMediaTask(fixture.Context, fixture.Scope, fixture.ApprovedAttempt)
    require.NoError(t, err)
    second, err := fixture.Service.EnsureMediaTask(fixture.Context, fixture.Scope, fixture.ApprovedAttempt)
    require.NoError(t, err)
    require.Equal(t, first.ID, second.ID)
    require.Equal(t, int64(1), billingReservationCount(t, fixture.DB, first.BillingOrderID))
}
```

- [ ] **Step 2: Run focused billing/media tests and confirm RED**

Run: `cd backend && go test ./internal/service ./internal/repository -run 'MediaApproval|RepeatedMediaApproval|CostApprovalQuoteMismatch|BillingQuote' -count=1`

Expected: a generic media attempt identity does not exist.

- [ ] **Step 3: Implement one frozen commercial command**

```go
type MediaGenerationAttempt struct {
    ArtifactRevisionID string
    Attempt            int
    Capability         string
    ChannelModelID     string
    ModelKey           string
    ParametersJSON     string
    Quantity           int64
    QuoteID            string
    QuoteFingerprint   string
    ExpiresAt          time.Time
}

func MediaAttemptIdentity(scope agentruntime.Scope, value MediaGenerationAttempt) string {
    return strings.Join([]string{scope.TenantKind, scope.TenantID, scope.DomainProjectID, scope.CanvasID, value.ArtifactRevisionID, strconv.Itoa(value.Attempt)}, ":")
}
```

Freeze model record/version, provider capabilities, exact input Resource revisions/URLs, parameters, quantity, unit price, total, user/project/canvas and expiry. Approval verifies the fingerprint, reserves once, creates an internal media Task once, then uses existing provider workers and settlement. Rejections call run-tree cancellation. Provider callbacks append every Resource/candidate and settle/refund/mark-uncertain according to existing billing facts.

- [ ] **Step 4: Run billing, Task, callback and restart tests**

Run: `cd backend && go test ./internal/service ./internal/repository -run 'Media|Billing|Quote|Task|Callback|Restart' -count=1`

Expected: changed parameters/quantity/model/expiry fail; replay and duplicate callback do not duplicate Task, reservation, settlement or Resource.

- [ ] **Step 5: Commit the commercial media port**

```bash
git add backend/internal/service/agent_media_generation.go backend/internal/service/agent_media_generation_test.go backend/internal/service/billing_quote.go backend/internal/service/billing_quote_test.go backend/internal/service/agent_runtime_tools.go backend/internal/service/agent_runtime_render_task.go backend/internal/repository/task_billing.go
git commit -m "feat(agent): unify media generation billing"
```

### Task 18: Complete Video, Optional Audio and Final Delivery Verification

**Files:**
- Modify: `backend/internal/agentruntime/delivery.go`
- Modify: `backend/internal/agentruntime/delivery_test.go`
- Modify: `backend/internal/agentruntime/artifact.go`
- Modify: `backend/internal/service/agent_specialist_runtime.go`
- Modify: `backend/internal/service/agent_media_generation.go`
- Modify: `backend/internal/service/agent_stage_review.go`
- Test: `backend/internal/service/agent_media_generation_test.go`

**Interfaces:**
- Consumes: confirmed storyboard/visual/media revisions, dynamic capability facts, existing Delivery Verifier.
- Produces: `video_plan.v1`, `audio_plan.v1`, `assembly_plan.v1`, native-audio branching evidence, final `ExpectedDelivery -> DeliveryEvidence -> DeliveryVerification` closure.

- [ ] **Step 1: Write failing native/independent audio and final delivery tests**

```go
func TestNativeAudioVideoDoesNotCreateIndependentAudioArtifact(t *testing.T) {
    fixture := videoDeliveryFixture(t, nativeAudioVideoModel(), confirmedDialogueDelivery())
    require.NoError(t, fixture.Service.ExecuteConfirmedVideoStage(fixture.Context, fixture.Command))
    require.Zero(t, artifactCountByKind(t, fixture.DB, "audio"))
    require.Equal(t, int64(1), artifactCountByKind(t, fixture.DB, "video"))
}

func TestExplicitIndependentAudioCreatesSeparateQuotedArtifact(t *testing.T) {
    fixture := videoDeliveryFixture(t, silentVideoModel(), explicitIndependentAudioDelivery())
    result, err := fixture.Service.PlanVideoAndAudio(fixture.Context, fixture.Command)
    require.NoError(t, err)
    require.NotEmpty(t, result.AudioArtifactRevisionID)
    require.NotEmpty(t, result.AudioQuoteID)
}

func TestFinalDeliveryNeedsResourceCanvasAndExactRevisionEvidence(t *testing.T) {
    expected := finalFilmExpectedDelivery()
    evidence := agentruntime.DeliveryEvidence{Artifacts: []agentruntime.ArtifactEvidence{{Kind: "video", RevisionID: "rev-final"}}}
    result := agentruntime.VerifyDelivery(expected, evidence)
    require.Equal(t, agentruntime.DeliveryRepairable, result.Status)
    require.ElementsMatch(t, []string{"resource", "canvas_revision"}, missingFactNames(result))
}
```

- [ ] **Step 2: Run focused audio/delivery tests and confirm RED**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'NativeAudio|IndependentAudio|FinalDelivery' -count=1`

Expected: audio semantics and final evidence dimensions are incomplete.

- [ ] **Step 3: Implement capability-driven planning and generic final evidence**

The main Agent decides whether audio is required from user intent and frozen model facts. Local code only rejects contradictions: native-audio requirement with a model that lacks it, independent audio without an audio model/quote, or missing confirmed upstream refs. `audio_plan.v1` can bind voice/line/timeline and publish only after its own content review. Final assembly consumes exact approved video/audio revisions and writes a final Resource plus canvas revision. Extend `DeliveryEvidence` with exact Artifact revision, Resource readiness, publication and canvas revision facts; never accept specialist completion or explanatory text alone.

- [ ] **Step 4: Run delivery, media, billing and stage tests**

Run: `cd backend && go test ./internal/agentruntime ./internal/service -run 'Delivery|Video|Audio|Assembly|Stage|Billing' -count=1`

Expected: native-audio flow uses one video task; explicit audio uses a separate quote/task/review; final success requires Resource + exact revision + canvas CAS evidence.

- [ ] **Step 5: Commit video/audio/assembly completion**

```bash
git add backend/internal/agentruntime/delivery.go backend/internal/agentruntime/delivery_test.go backend/internal/agentruntime/artifact.go backend/internal/service/agent_specialist_runtime.go backend/internal/service/agent_media_generation.go backend/internal/service/agent_stage_review.go backend/internal/service/agent_media_generation_test.go
git commit -m "feat(agent): verify final media delivery"
```

### M5 Gate

- [ ] Run `cd backend && go test ./internal/agentruntime ./internal/repository ./internal/service -run 'Media|Video|Audio|Assembly|Billing|Delivery' -count=1`.
- [ ] Run affected race tests: `cd backend && go test -race ./internal/repository ./internal/service -run 'Media|Billing|Callback|Stage' -count=1`.
- [ ] Verify all selected models come from dynamic catalog facts and all media approvals display model, parameters, quantity, total price and expiry.
- [ ] Verify native-audio and explicit-independent-audio flows do not duplicate cost or assets.
- [ ] Review budget: 10–14 production files and approximately 900–1,300 net-new production lines.

---

## M6 — Timeline Cards, Canvas Projection, Replay and Hard Cutover

### Task 19: Publish UI Protocol v3 with Strict Production Card Contracts

**Files:**
- Modify: `backend/internal/service/agent_runtime_transport.go`
- Modify: `backend/internal/service/agent_runtime_transport_test.go`
- Modify: `web/src/services/api/agent-runtime.ts`
- Modify: `web/test/agent-runtime-api.test.ts`
- Create: `web/src/components/canvas/agent-production-card-contract.ts`
- Create: `web/test/agent-production-card-contract.test.ts`

**Interfaces:**
- Consumes: persisted stable timeline kinds and Artifact revision API.
- Produces: UI protocol v3 `contentJSON.type` subtypes `agent_status`, `artifact_review`, `media_gallery`, `stage_approval`, `cost_approval`, `error`, `stopped`.

- [ ] **Step 1: Write failing Go/TypeScript contract fixtures**

```ts
test("parses artifact review by exact revision reference", () => {
  const content = parseAgentProductionCardContent({
    type: "artifact_review",
    stageId: "stage-1",
    stageVersion: 4,
    artifactId: "artifact-1",
    revisionId: "revision-3",
    artifactSchema: "script_bundle.v1",
    title: "剧本与角色",
  });
  expect(content.revisionId).toBe("revision-3");
});

test("rejects unknown production card subtype", () => {
  expect(() => parseAgentProductionCardContent({ type: "invented" })).toThrow(/unknown agent production card/i);
});
```

```go
func TestAgentUIProtocolV3ArtifactReviewCarriesOnlyRevisionSummary(t *testing.T) {
    event := projectArtifactReviewEvent(t, artifactReviewTimelineFixture())
    require.Equal(t, 3, event.ProtocolVersion)
    require.Equal(t, "artifact_review", event.Content.Type)
    require.Empty(t, event.Content.FullPayload)
    require.NotEmpty(t, event.Content.RevisionID)
}
```

- [ ] **Step 2: Run focused transport/parser tests and confirm RED**

Run: `cd backend && go test ./internal/service -run 'AgentUIProtocolV3|AgentRuntimeTransport' -count=1`

Run: `cd web && bun test test/agent-production-card-contract.test.ts test/agent-runtime-api.test.ts`

Expected: protocol v3 and strict card parser are absent.

- [ ] **Step 3: Implement one stable event envelope and exhaustive card parser**

```ts
export type AgentProductionCardContent =
  | { type: "agent_status"; specialistKey?: SpecialistKey; skillSelections: FrozenSkillSummary[]; status: string }
  | { type: "artifact_review"; stageId: string; stageVersion: number; artifactId: string; revisionId: string; artifactSchema: string; title: string }
  | { type: "media_gallery"; stageId: string; artifactId: string; candidateRevisionIds: string[]; selectedRevisionId?: string; publication?: AssetPublicationSummary }
  | { type: "stage_approval"; stageId: string; stageVersion: number; revisionId: string; publicationIntent?: PublicationIntent }
  | { type: "cost_approval"; approvalId: string; quote: FrozenQuoteSummary }
  | { type: "error"; code: string; message: string; recoveryActions: RecoveryAction[] }
  | { type: "stopped"; code: string; message: string };
```

Define the referenced types in the same contract module rather than as loose `Record<string, unknown>` aliases:

```ts
export type SpecialistKey = "narrative" | "asset" | "storyboard" | "visual" | "video_assembly" | "audio";
export type FrozenSkillSummary = { dir: string; version: number; checksum: string };
export type AssetPublicationSummary = { publicationId: string; assetId: string; assetVersionId: string; status: "published" | "failed" };
export type PublicationIntent = { purpose: string; assetKind: "image" | "video" | "audio" };
export type FrozenQuoteSummary = { modelRecordId: string; modelKey: string; parameters: Record<string, string | number | boolean>; quantity: number; totalMicrocredits: number; expiresAt: string };
export type RecoveryAction = { action: "retry" | "request_revision" | "stop"; label: string };
```

Keep the existing outer event kinds. Full Artifact content is fetched from exact scoped revision API and cached by `artifactId:revisionId`; unknown subtype/protocol fails visibly. No provider reasoning or internal Task fields enter the DTO.

- [ ] **Step 4: Run transport/API/card parser tests**

Run: `cd backend && go test ./internal/service -run 'AgentRuntimeTransport|AgentUIProtocol' -count=1`

Run: `cd web && bun test test/agent-production-card-contract.test.ts test/agent-runtime-api.test.ts`

Expected: Go fixture and TypeScript fixture agree on exact field names and unknown content fails.

- [ ] **Step 5: Commit the UI protocol**

```bash
git add backend/internal/service/agent_runtime_transport.go backend/internal/service/agent_runtime_transport_test.go web/src/services/api/agent-runtime.ts web/test/agent-runtime-api.test.ts web/src/components/canvas/agent-production-card-contract.ts web/test/agent-production-card-contract.test.ts
git commit -m "feat(agent): publish production card protocol"
```

### Task 20: Render Review, Media and Stage Cards in the Existing Conversation

**Files:**
- Create: `web/src/components/canvas/agent-artifact-review-card.tsx`
- Create: `web/test/agent-artifact-review-card.test.tsx`
- Create: `web/src/components/canvas/agent-media-gallery-card.tsx`
- Create: `web/test/agent-media-gallery-card.test.tsx`
- Create: `web/src/components/canvas/agent-stage-approval-card.tsx`
- Modify: `web/src/components/canvas/canvas-assistant-panel.tsx`
- Modify: `web/src/components/canvas/canvas-agent-chat-ui.tsx`
- Modify: `web/src/components/canvas/canvas-agent-panel.css`
- Modify: `web/test/canvas-agent-runtime-panel.test.tsx`

**Interfaces:**
- Consumes: Task 19 discriminated union, Artifact revision fetch, stage review command, existing stop/cost approval handlers.
- Produces: exact-revision review UI, all-candidate gallery/audio player/video player, “确认后加入项目资产库” copy and one-click stage+publication approval.

- [ ] **Step 1: Write failing interaction tests**

```tsx
it("submits the exact visible stage and revision", async () => {
  render(<AgentStageApprovalCard content={stageApprovalFixture({ revisionId: "rev-3" })} onReview={onReview} />);
  await user.click(screen.getByRole("button", { name: "确认并加入项目资产库" }));
  expect(onReview).toHaveBeenCalledWith({ stageId: "stage-1", stageVersion: 4, revisionId: "rev-3", decision: "approved" });
});

it("shows all candidates and never hides unselected results", () => {
  render(<AgentMediaGalleryCard content={galleryFixture(4)} />);
  expect(screen.getAllByTestId("agent-media-candidate")).toHaveLength(4);
});

it("requests revision without cancelling the run", async () => {
  render(<AgentStageApprovalCard content={stageApprovalFixture()} onReview={onReview} />);
  await user.type(screen.getByLabelText("修改要求"), "让角色更克制");
  await user.click(screen.getByRole("button", { name: "要求修改" }));
  expect(onReview).toHaveBeenCalledWith(expect.objectContaining({ decision: "revision_requested", comment: "让角色更克制" }));
});
```

- [ ] **Step 2: Run card/panel tests and confirm RED**

Run: `cd web && bun test test/agent-artifact-review-card.test.tsx test/agent-media-gallery-card.test.tsx test/canvas-agent-runtime-panel.test.tsx`

Expected: card components are missing.

- [ ] **Step 3: Implement accessible cards without redesigning the panel**

Every JSX tag receives a descriptive `className`. Reuse current spacing/color tokens; one visible rounded/bordered container per card. Render script/role/binding/storyboard payloads from exact Artifact revision; gallery includes every revision; audio uses native accessible controls; actions expose `aria-label`, disabled/submitting/error states. If `publicationIntent` is present, label is exactly “确认并加入项目资产库” and no second popup is created. Stop remains available while any main/specialist/tool/media work is nonterminal.

- [ ] **Step 4: Run component, panel and accessibility-focused tests**

Run: `cd web && bun test test/agent-artifact-review-card.test.tsx test/agent-media-gallery-card.test.tsx test/canvas-agent-runtime-panel.test.tsx`

Expected: exact action payloads, publication copy, error state, candidate count and keyboard-accessible controls pass.

- [ ] **Step 5: Commit production review cards**

```bash
git add web/src/components/canvas/agent-artifact-review-card.tsx web/test/agent-artifact-review-card.test.tsx web/src/components/canvas/agent-media-gallery-card.tsx web/test/agent-media-gallery-card.test.tsx web/src/components/canvas/agent-stage-approval-card.tsx web/src/components/canvas/canvas-assistant-panel.tsx web/src/components/canvas/canvas-agent-chat-ui.tsx web/src/components/canvas/canvas-agent-panel.css web/test/canvas-agent-runtime-panel.test.tsx
git commit -m "feat(web): render agent production reviews"
```

### Task 21: Project Exact Revisions to Canvas and Recover by Sequence

**Files:**
- Create: `backend/internal/service/agent_canvas_projection.go`
- Create: `backend/internal/service/agent_canvas_projection_test.go`
- Modify: `backend/internal/repository/agent_runtime_event_projection.go`
- Create: `backend/internal/repository/agent_runtime_event_projection_test.go`
- Modify: `web/src/components/canvas/use-agent-runtime.ts`
- Modify: `web/src/components/canvas/agent-conversation-reducer.ts`
- Modify: `web/test/agent-conversation-reducer.test.ts`
- Modify: `web/src/pages/canvas/use-canvas-agent-operations.ts`
- Modify: `web/src/lib/canvas/canvas-agent-ops.ts`

**Interfaces:**
- Consumes: Artifact revision, canvas CAS/revision, queued/running media lifecycle, persisted sequence SSE.
- Produces: same-identity placeholder/update projection, exact revision node metadata, reconnect `afterSequence`, dedupe and no double state.

- [ ] **Step 1: Write failing projection/replay tests**

```go
func TestMediaProjectionCreatesPlaceholderBeforeTask(t *testing.T) {
    fixture := canvasProjectionFixture(t)
    projection, err := fixture.Service.ProjectQueuedMedia(fixture.Scope, fixture.ArtifactRevision)
    require.NoError(t, err)
    require.NotEmpty(t, projection.CanvasNodeID)
    require.Zero(t, mediaTaskCount(t, fixture.DB))
    require.Equal(t, "queued", canvasNodeArtifactState(t, fixture.DB, projection.CanvasNodeID))
}

func TestMediaCompletionUpdatesSameProjectionIdentity(t *testing.T) {
    fixture := queuedProjectionFixture(t)
    require.NoError(t, fixture.Service.ProjectMediaCompletion(fixture.Scope, fixture.ProjectionID, fixture.CompletedRevision))
    require.Equal(t, fixture.CanvasNodeID, projectedCanvasNodeID(t, fixture.DB, fixture.ProjectionID))
}
```

```ts
it("deduplicates replayed sequence and preserves one growing assistant item", () => {
  const state = reduceAgentEvents(initialState, [delta(41, "item-1", "你"), delta(42, "item-1", "好"), delta(42, "item-1", "好")]);
  expect(state.lastSequence).toBe(42);
  expect(state.items.filter((item) => item.id === "item-1")).toHaveLength(1);
  expect(state.items.find((item) => item.id === "item-1")?.text).toBe("你好");
});
```

- [ ] **Step 2: Run projection/reducer tests and confirm RED**

Run: `cd backend && go test ./internal/service ./internal/repository -run 'MediaProjection|EventProjection' -count=1`

Run: `cd web && bun test test/agent-conversation-reducer.test.ts`

Expected: exact revision projection and reconnect cursor behavior are incomplete.

- [ ] **Step 3: Implement durable projection and sequence cursor**

Before a paid media Task, persist queued Artifact state, canvas placeholder CAS update, timeline item and sequence. Store only `artifactId`, `revisionId`, `projectionId`, `canvasNodeId`, status and summary in node/timeline metadata. Completion/failure updates the same projection identity with a new exact revision/status and Canvas CAS. `useAgentRuntime` subscribes from its last persisted sequence, not zero; reducer ignores `sequence <= lastSequence`, accumulates deltas by itemId, and fails visibly on gaps/unknown protocol rather than guessing.

- [ ] **Step 4: Run canvas CAS, SSE/replay and Web reducer tests**

Run: `cd backend && go test ./internal/service ./internal/repository -run 'Canvas|Projection|Sequence|Replay' -count=1`

Run: `cd web && bun test test/agent-conversation-reducer.test.ts test/canvas-agent-collaboration.test.ts test/canvas-agent-runtime-panel.test.tsx`

Expected: placeholder precedes Task; reconnect neither loses nor duplicates items; dialogue and canvas reference the same revision.

- [ ] **Step 5: Commit Canvas/SSE projection**

```bash
git add backend/internal/service/agent_canvas_projection.go backend/internal/service/agent_canvas_projection_test.go backend/internal/repository/agent_runtime_event_projection.go backend/internal/repository/agent_runtime_event_projection_test.go web/src/components/canvas/use-agent-runtime.ts web/src/components/canvas/agent-conversation-reducer.ts web/test/agent-conversation-reducer.test.ts web/src/pages/canvas/use-canvas-agent-operations.ts web/src/lib/canvas/canvas-agent-ops.ts
git commit -m "feat(agent): project revisions with durable replay"
```

### Task 22: Hard-Cut New Runs to Runtime v3 and Close Documentation/E2E Gates

**Files:**
- Modify: `backend/internal/agentruntime/contracts.go`
- Modify: `backend/internal/service/agent_runtime.go`
- Modify: `backend/internal/service/agent_runtime_test.go`
- Modify: `backend/internal/handler/agent_runtime.go`
- Modify: `backend/internal/handler/agent_runtime_test.go`
- Modify: `backend/internal/database/agent_runtime_schema.go`
- Modify: `web/src/services/api/agent-runtime.ts`
- Modify: `README.md`
- Create: `docs/superpowers/reports/2026-08-27-agent-multi-specialist-production-acceptance.md`

**Interfaces:**
- Consumes: all M1–M6 contracts.
- Produces: user run creation always uses v3, v2 terminal history read-only, audited legacy retirement, documented recovery/rollback and final acceptance evidence.

- [ ] **Step 1: Write failing hard-cut and historical-read tests**

```go
func TestNewAgentRunUsesOnlyProductionRuntimeV3(t *testing.T) {
    fixture := agentRuntimeFixture(t)
    started, err := fixture.Service.StartAgentRun(fixture.Context, fixture.Request)
    require.NoError(t, err)
    require.Equal(t, int64(3), started.Run.RuntimeVersion)
    require.Equal(t, int64(4), started.Run.ToolSchemaVersion)
    require.Zero(t, legacyProductionPlanCount(t, fixture.DB, started.Run.ID))
}

func TestTerminalV2RunRemainsReadableButCannotResume(t *testing.T) {
    fixture := historicalV2RunFixture(t)
    require.NotNil(t, fixture.Service.ReadAgentRun(fixture.Scope))
    _, err := fixture.Service.ResumeAgentRun(fixture.Context, fixture.Scope)
    require.ErrorIs(t, err, service.ErrRuntimeSchemaRetired)
}
```

- [ ] **Step 2: Run hard-cut tests and confirm RED**

Run: `cd backend && go test ./internal/service ./internal/handler -run 'ProductionRuntimeV3|TerminalV2Run|RuntimeSchemaRetired' -count=1`

Expected: new creation still uses prior current version or can call old tools.

- [ ] **Step 3: Perform the single-path cutover and document operations**

Set current constants to the locked v3 values and make `StartAgentRun` initialize a graph-capable checkpoint only. Remove v3 access to old plan/render/commit execution. Add an operator preflight command/endpoint that reports old nonterminal runs; run retirement only after output is reviewed. README sections must state current request→main agent→specialist→artifact→stage review→quote/task/billing→resource→canvas/timeline architecture, table ownership, error codes, migration order, rollback behavior and that rollback preserves/read-displays v3 facts without database downgrade.

- [ ] **Step 4: Execute the complete verification matrix**

Focused backend:

```bash
cd backend && go test ./internal/agentruntime ./internal/database ./internal/repository ./internal/service ./internal/handler -count=1
```

Full backend:

```bash
cd backend && go test ./... -count=1
cd backend && go test -race ./internal/agentruntime ./internal/repository ./internal/service -count=1
cd backend && go vet ./...
cd backend && go build ./...
```

Web:

```bash
cd web && bun test
cd web && bun x tsc --noEmit
cd web && bun run build
```

Database/format:

```bash
cd backend && go test ./internal/database -run 'SQLite|Migration|AgentProduction' -count=1
git diff --check
```

Run configured PostgreSQL transaction/CAS suite. Record every command, exit code and relevant test count in the acceptance report; record a real reason for any unavailable environment rather than marking it passed.

- [ ] **Step 5: Run local browser acceptance before any paid provider call**

Use the existing local app and browser test path to verify, with free/fake provider fixtures only:

1. one sentence stops at complete script + character review;
2. revision request creates a new revision and only exact approval advances;
3. upload/vision/binding uses real Resource evidence;
4. generated character candidate approval publishes one Asset and duplicate click is idempotent;
5. storyboard/media cards focus the exact canvas placeholder/node;
6. upstream change makes downstream stages stale;
7. every paid attempt shows frozen model/parameters/count/total/expiry before Task;
8. cost rejection and stop terminate all new behavior;
9. native-audio video creates no independent Audio Artifact, explicit audio does;
10. reconnect/reload replays sequences without duplicate cards or assistant bubbles.

Capture request/response identifiers, timeline sequences, graph/stage/revision IDs, Task/Billing absence before approval, and screenshots in the acceptance report. A real paid run requires a fresh user approval after this gate and must use the lowest-cost suitable models.

- [ ] **Step 6: Perform explicit final review and one concentrated fix pass**

Review requirements against the spec, this plan, actual diff, front/back contracts, migration, permissions, billing, idempotency, concurrency, error codes, docs, test evidence and rollback. Classify findings as current-diff defect, existing debt or new scope. Fix all current-diff defects once; rerun only affected gates, then perform one directed re-review. If directed review still finds a new cross-module Critical/Important or shared transaction cannot close, stop and return to architecture rather than adding a third patch cycle.

- [ ] **Step 7: Form the final focused commit without pushing**

Inspect scope:

```bash
git status --short
git diff --stat
git diff --check
git diff --cached --stat
git diff --cached
```

Stage only remaining M6 cutover/docs/report files and relevant hunks, then commit:

```bash
git commit -m "feat(agent): cut over multi-specialist production"
```

Do not push. Report the exact commit(s), validation commands/results, browser evidence, paid test not run unless separately approved, and every remaining external/environment limitation.

### M6 Final Gate

- [ ] User-visible behavior meets all ten real-flow scenarios from the spec.
- [ ] No new Run can call or write the legacy production path; terminal v2 facts remain readable.
- [ ] No duplicate state exists across timeline, canvas and asset library; all point to exact revisions.
- [ ] Billing, Task, Resource, publication and final delivery evidence remain internally consistent across retry, restart, stop and late callback.
- [ ] README, backend database documentation, third-party notice and acceptance report describe the shipped facts.
- [ ] Review budget: 8–12 production files and approximately 700–1,100 net-new production lines; any excess is explained by required contracts/tests/docs rather than duplicated paths.

---

## Execution Order and Checkpoints

Execute strictly in numeric order. Stop after each milestone gate and present: production-file count, net-new production lines, focused test evidence, open findings and the proposed next milestone. Do not start an expensive full suite, browser paid flow, migration retirement or deployment outside the gate where it is explicitly listed.

The only implementation entry for this repository is inline `superpowers:executing-plans`. The default subagent-driven option from the generic planning skill is intentionally unavailable because Runtime, Billing, Task, Asset, Canvas and event-sequence state are shared across every milestone.
