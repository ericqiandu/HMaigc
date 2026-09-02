# HMaigc 多专家生产 Runtime 验收报告

日期：2026-08-28

分支：`codex/release-v1.0.45`

基线提交：`5548305 feat(agent): add governed multi-specialist production`

## 1. 验收结论

本轮已完成多专家生产 Runtime 的代码收口与 Runtime v3 硬切。新 Run 固定使用 Runtime v3、Policy v3、Tool schema v4 与 UI protocol v3；旧 Runtime v2 / Policy v2 / Tool schema v3 只保留终态历史读取，不再承接、恢复或双写新的生产行为。

本轮可提交范围已通过 Agent focused tests、Go 全量测试、受影响包 race、`go vet`、Go build、Web Agent focused tests、TypeScript 检查、生产构建、SQLite 迁移专项和 `git diff --check`。本地浏览器完成了登录首页与既有画布恢复烟测，浏览器控制台无错误。

本报告不把以下尚未执行的门禁伪报为成功：最低成本真实付费生产流程、配置 PostgreSQL 的 CAS/事务门禁、真实视频拼接/混音执行器，以及十个业务场景全部通过浏览器逐项跑通。Web 全量测试还受到工作区中本任务之外的视频能力契约改动影响，当前结果为 696 通过、3 失败；详见第 6 节。

## 2. 已交付的单一事实链

- Main Agent 通过 `skill.load`、`specialist.delegate`、`vision.analyze`、`media.generate` 与 `canvas.project` 五个 v4 工具自主编排，不在 Go 或 Web 中加入关键词路由、固定子代理顺序或默认模型降级。
- Specialist Run 记录父子 Run、冻结模型、Skill version/checksum、Tool allowlist、输入 Artifact revision、严格输出 schema、Token 用量和账务证据；专家文本 Task 保持 `internal`，不会进入用户任务中心。
- ProductionGraph、Stage、Artifact revision、Review、Binding、Publication 与 Candidate 全部使用追加事实或带版本 CAS 的控制行。会话卡片、资产发布和画布节点均引用精确 `artifactId + revisionId`，不复制第二份正文状态。
- 剧本、素材绑定、分镜、视觉候选、视频计划、可选独立音频计划与装配计划都先进入 Artifact/Stage review。用户可确认、要求修改或停止；候选确认不得同时携带修改要求。
- 图片理解只消费当前作用域内就绪图片 Resource，并冻结来源 revision、模型、报价和 ToolCall 身份。来源变化后旧证据失效，不能静默沿用。
- 图片、视频、独立音频统一走 `media.generate`。每个付费动作在创建 Task、BillingOrder 或积分预留前单独冻结并展示模型、参数、数量、总价与过期时间；参数、模型、输入资源或价格漂移会显式失败。
- 已成功产出的每一个媒体候选都追加保存。停止、拒绝或迟到回调不会删除供应商已经产出的 Resource，只会把未采纳结果标记为 `unadopted` 并保留审计事实。
- 已批准图片或独立音频可按显式用途、分类和绑定键幂等发布到资产库。发布失败不覆盖原始 Artifact；重复请求不会创建第二份 Asset/Version/Link/Representation/Publication。
- 画布投影先校验精确已批准 revision 与真实 Resource，再通过单一 Canvas revision/CAS 写入；事件先持久化再通过 SSE 推送，Web 按 sequence 和 `itemId` 合并，同一生产卡片在重连后恢复而不重复。
- 停止和拒绝覆盖整棵 Run 树：先取消待处理/运行中的工具和内部任务，再关闭生命周期；恢复只从持久 checkpoint、Task、Billing、Artifact 与 sequence 事实继续。

## 3. 需求对照

| 用户确认能力 | 代码/自动化证据 | 状态 |
| --- | --- | --- |
| 一句话进入短片生产，但先停在剧本审核 | ProductionGraph、Narrative Specialist、Artifact/Stage review 契约与服务测试 | 已实现；未做真实付费浏览器全流程 |
| 剧本不满意可反复修改 | 精确 revision 审核、追加 revision、旧 revision 拒绝与 CAS 测试 | 已实现 |
| 理解剧本、角色资产库和上传素材 | `asset_binding.v1`、Resource 作用域/媒体类型/就绪校验 | 已实现 |
| 图片理解 | `vision.analyze`、来源 revision/Resource/报价冻结与 stale 测试 | 已实现 |
| 角色一致性与电影化镜头语言 | 版本化 Skills、结构化 Artifact、候选与一致性证据；运行时不读取 `docs/` | 已实现运行时契约 |
| 首帧—运动—尾帧与 Camera Tree | 精确上游 revision、真实前置 Resource 与结构校验 | 已实现运行时契约 |
| 分镜、角色图、视频在同一对话可见 | UI protocol v3 生产卡片、精确 Artifact 查询、懒加载卡片组件 | 已实现 |
| 确认后加入资产库 | 发布事务、权限、失败保全与重复请求幂等测试 | 已实现 |
| 视频原生声音与可选独立音频 | `native / independent / none` 严格计划与媒体计费契约 | 已实现计划和媒体执行；真实拼接/混音执行器暂缓 |
| 每阶段确认与付费确认分离 | Stage review 与 frozen quote/tool approval 两套独立契约 | 已实现 |
| Skills 可识别、调用、审计 | `skill.load` 与冻结 Skill version/checksum/capability manifest | 已实现 |
| 停止、拒绝、恢复与迟到结果 | Run 树取消、持久恢复、`unadopted`、SSE sequence 测试 | 已实现 |

## 4. 前后端、数据库与商业契约复审

- 前后端生产卡片均固定 protocol v3；未知事件、未知卡片类型、未知字段和尾随 JSON 显式失败。
- 新 Run 无法调用旧 `production.plan`、`production.render`、`canvas.commit` 工具；九个旧 v3 集成用例仅以明确 `t.Skip` 保留历史说明，并指向对应 v4 测试套件。
- 数据库变更为追加迁移与索引调整，未删除旧事实；Runtime 退役需要先审计活动 Tool、Task、Billing、Artifact 与 Checkpoint，不做数据库降级。
- 用户/租户/项目/画布/Thread/Run scope 在 Graph、Artifact、Resource、Review、Publication 和投影入口重复核验；浏览器不能写入服务端 ToolResult。
- 计费在真实冻结报价批准后才创建内部 Task/Billing；模型继承失败、报价过期、参数漂移、重复请求和供应商迟到结果都有稳定错误或账务核对状态，没有自动降级与静默重算。
- 幂等身份覆盖 ToolCall/actionVersion、Specialist Run、Task、Billing、candidate、publication、stage review、canvas projection 与 SSE sequence。
- 最终 delivery verifier 只接受精确 Artifact revision、就绪 Resource、Publication 与 Canvas revision 事实，不把文本说明或 Specialist 完成状态当作交付成功。

## 5. 最终验证证据

### Backend

| 命令 | 结果 |
| --- | --- |
| `go test ./internal/agentruntime ./internal/database ./internal/repository ./internal/service ./internal/handler -count=1` | 通过；service 32.890s，handler 26.178s |
| `go test ./... -count=1` | 通过；全部 Go package 通过 |
| `go test -race ./internal/agentruntime ./internal/repository ./internal/service ./internal/handler -count=1` | 通过；agentruntime 1.063s，repository 16.160s，service 92.361s，handler 34.700s |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |
| `go test ./internal/database -run 'SQLite|Migration|AgentProduction' -count=1` | 通过，0.445s |

### Web

| 命令 | 结果 |
| --- | --- |
| `bun test test/agent-production-card.test.tsx test/canvas-agent-runtime-panel.test.tsx` | 通过，33 tests / 135 assertions |
| `bun test test/agent-production-card.test.tsx` | 通过，5 tests / 18 assertions |
| `bun x tsc --noEmit` | 通过 |
| `bun run build` | 通过；所有 bundle budget 通过，生产卡片拆为异步 chunk，canvas closure 由 2820.32 KiB 降至 2803.26 KiB |
| `bun test` | 未全绿：696 通过、3 失败、2825 assertions；失败均来自本任务外的 `web/src/lib/video-model-capabilities.ts` 工作区改动与旧测试夹具契约不一致 |

### Repository and browser

| 检查 | 结果 |
| --- | --- |
| `git diff --check` | 通过 |
| 新增 `any` / `as any` / `TODO` / `FIXME` 扫描 | 未发现 |
| 本地 `http://127.0.0.1:3000/` | 已登录首页加载成功，钱包、导航、创作入口和最近项目可见 |
| 本地既有画布恢复 | 画布恢复到服务端 revision，Agent 历史与停止状态可见 |
| 本地浏览器 console | 无错误 |

## 6. 未通过或未执行事项

1. **Web 全量测试的 3 个外部失败**：`canvas-generation-defaults.test.ts` 两项和 `canvas-node-prompt-panel.test.ts` 一项因当前未纳入本任务的 `video-model-capabilities.ts` 要求完整布尔能力字段而失败。本提交不修改、不暂存该文件及其相关视频模型测试，避免混入用户的另一组改动。
2. **PostgreSQL 门禁未执行**：当前本地测试环境没有配置可用的 PostgreSQL 事务/CAS 测试连接；SQLite 追加迁移专项与全部 repository/service race 已通过，不能替代 PostgreSQL 生产门禁。
3. **未执行真实付费 E2E**：没有为本次收口取得新的最低成本付费模型调用授权，因此未创建真实图片、视频、音频或视觉分析 Task/Billing。
4. **浏览器十场景未全部逐项跑通**：本地没有现成的 v4 待审核生产 Run；仅完成免费页面、历史恢复和控制台烟测。对应状态机、权限、计费、幂等、恢复、投影和卡片交互由自动化测试覆盖。
5. **真实视频拼接/混音执行器暂缓**：当前只严格校验并保存 `assembly_plan.v1`。计划本身不能满足最终交付；只有后续执行器产出新的就绪视频 Resource、精确批准 revision 与真实 Canvas revision 后，delivery verifier 才能判定最终成片完成。

## 7. 变更预算复核

本次待提交 diff 横跨 M2–M6 的收口工作，共 32 个生产文件，新增 3,123 行、删除 88 行，净新增约 3,035 行；另有 35 个测试文件与 README/规格/计划/验收文档。该范围低于六个里程碑预算总和，但不应与单独 M6 的 8–12 文件预算直接比较。

较大的必要增量集中在：严格 TypeScript 生产协议解析、Canvas CAS 投影/协调、Specialist delegation/runtime、恢复与迟到结果、发布与审核卡片。复审未发现第二状态真源、固定语义工作流、重复计费链、前端伪输出或 legacy 双轨。`agent-production.ts` 虽然较长，但职责仅为 UI protocol v3 的严格判别联合、解析和 API DTO；本轮不为压缩行数拆散同一协议真源。

## 8. 最终审查记录

- 首轮独立审查发现两个当前 diff 缺陷：视觉候选可在保留修改要求时误点确认；生产卡片同步进入 canvas closure 导致 bundle budget 超限。
- 集中修复：审核卡在请求前拒绝歧义输入并引导“要求修改”；生产时间线改为 `lazy + Suspense` 异步加载。
- 定向复审：相关组件测试、面板测试、TypeScript 与生产 build 全部通过；Go 全量/race/vet/build、SQLite 迁移专项和 diff 格式通过；没有发现新的跨模块 Critical/Important。
- 现存 3 个 Web 全量失败属于未纳入本次提交的视频模型工作区改动，不在 Agent diff 中修补。

## 9. 发布限制

本次只形成聚焦 Git 提交，不执行 push、tag、部署、数据库退役或云端升级。正式发布前仍需在 CI/隔离生产副本执行 PostgreSQL 门禁，并在用户单独批准真实费用后完成最低成本付费生产验收。
