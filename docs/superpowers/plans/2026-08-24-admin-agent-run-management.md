# Admin Agent Run Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 HMaigc 增加可审计、并发安全的后台 Agent 运行目录与终止能力，并让 v1.0.53 在不触碰外部执行或未决账务的前提下安全退休旧契约暂停运行。

**Architecture:** 管理端查询通过 repository 完成数据库分页和事实聚合，service 负责管理员鉴权、输入校验、控制处置与稳定错误契约，写入复用唯一 Agent Runtime 中断状态机和 CAS 事务。升级路径在 Agent schema 完整性事务内先退休可证明无副作用的旧暂停运行，再拒绝剩余不兼容运行；Ops Controller 不直接写业务数据库。

**Tech Stack:** Go 1.24、Gin、GORM、SQLite/PostgreSQL、React 19、TypeScript、Ant Design、Vitest。

**Spec:** `docs/superpowers/specs/2026-08-24-admin-agent-run-management-design.md`

## Global Constraints

- 普通用户任务中心不得显示 Agent 内部文本模型 Task；后台管理员与账务链路仍保留完整事实。
- 管理员写操作必须使用 `expectedStateVersion`、行锁和 CAS；不得直接覆盖 `agent_runs.status`。
- `activityClassification` 仅根据数据库 UTC 时间与持久状态计算；10 分钟阈值只用于提示与筛选，不自动终止。
- `reason` 去除首尾空白后必须为 4–500 个字符；确认短语由服务端生成，格式为 `STOP <runId前8位>`。
- 终止不得猜测供应商取消或退款结果；迟到成功资产必须继续保存并按真实账务事实结算。
- v1.0.53 迁移只自动退休旧契约 `waiting_input` / `waiting_approval` 且无外部、媒体、账务副作用的运行；批次必须全有或全无、可重复执行。
- 后台页面遵循 `Design.md` 与 `web/DESIGN.md`，加载、空数据、首次失败、保留旧数据的刷新失败必须是不同事实状态。
- 不新增 `any` / `as any`，不引入兼容双轨、静默回退、默认路由或前端伪成功。
- 只暂存本任务文件与 `web/src/router.tsx` 的后台路由 hunk；保留 prototype 相关用户修改。

---

### Task 1: 安全退休旧契约暂停运行

**Files:**
- Create: `backend/internal/database/agent_runtime_paused_retirement.go`
- Create: `backend/internal/database/agent_runtime_paused_retirement_test.go`
- Modify: `backend/internal/database/agent_runtime_schema.go`

**Interfaces:**
- Consumes: `model.AgentRun`、`model.AgentCheckpoint`、`agentruntime.Current*Version` 与现有 schema 事务。
- Produces: `retireIncompatiblePausedAgentRuns(db *gorm.DB, now time.Time) error`；合格运行写入 `run.interrupted`、终态 checkpoint、timeline/tool/artifact 收口，不合格运行返回带事实类别的 `agent_runtime_retirement_invalid` 错误。

- [ ] **Step 1: 写出 v1.0.50 暂停运行的失败测试**

  在新测试文件创建真实 SQLite fixture：两个契约 `3/1/1` 的 `waiting_input` / `waiting_approval` 运行，Run 与 checkpoint 完全一致，pending tool 未开始，Artifact 仅 `planned` / `awaiting_approval`。断言调用迁移后两条 Run 均为 `cancelled`、`stateVersion+1`、各新增且只新增一个 `run.interrupted` 与 checkpoint，活动 timeline 为 `interrupted`，tool call / Artifact 为 `failed` 且错误码为 `runtime_contract_retired`。

- [ ] **Step 2: 运行 RED**

  Run: `go test ./internal/database -run 'TestRetireIncompatiblePausedAgentRuns' -count=1`

  Expected: FAIL，提示 `retireIncompatiblePausedAgentRuns` 未定义。

- [ ] **Step 3: 实现最小迁移事务**

  `retireIncompatiblePausedAgentRuns` 必须：锁定并按 `created_at,id` 读取不兼容暂停运行；解析最新 checkpoint；逐项验证 Run/checkpoint、pending start、provider request、Task、BillingOrder、Artifact；先验证整个候选批次，再写入全部终态事实。事件 payload 只写 `source=upgrade_migration`、Run ID、原状态与旧/新版本摘要，不写 prompt。

- [ ] **Step 4: 运行 GREEN**

  Run: `go test ./internal/database -run 'TestRetireIncompatiblePausedAgentRuns' -count=1`

  Expected: PASS。

- [ ] **Step 5: 增加回滚、幂等和风险阻断 RED**

  添加表驱动测试，分别注入：started pending tool、provider request ID、活动媒体 Task、未决 BillingOrder、queued/running Artifact、Run/checkpoint 不一致、批次中一条非法记录、高于当前契约版本。断言错误含稳定码和 Run ID，且任一非法候选使整批无写入；第二次执行不增加 event/checkpoint。

- [ ] **Step 6: 运行 RED 并确认失败原因**

  Run: `go test ./internal/database -run 'TestRetireIncompatiblePausedAgentRuns_(RejectsRiskFacts|RollsBackBatch|IsIdempotent|RejectsFutureContract)' -count=1`

  Expected: FAIL 于缺失风险校验或重复事实。

- [ ] **Step 7: 补齐校验并接入 schema 顺序**

  在 `agent_runtime_schema.go` 的完整性事务中按以下顺序调用：现有 integrity conflict 校验 → 现有 queued retirement → 新 paused retirement → `rejectIncompatibleActiveAgentRuns`。保持剩余 running/waiting_tool 风险运行显式阻止启动。

- [ ] **Step 8: 运行数据库 focused tests**

  Run: `go test ./internal/database -run 'Test(AgentRuntimeSchema|RetireIncompatiblePausedAgentRuns)' -count=1`

  Expected: PASS。

- [ ] **Step 9: 形成里程碑提交**

  ```powershell
  git add backend/internal/database/agent_runtime_paused_retirement.go backend/internal/database/agent_runtime_paused_retirement_test.go backend/internal/database/agent_runtime_schema.go
  git commit -m "fix(database): Agent 升级 - 安全退休旧契约暂停运行"
  ```

### Task 2: 管理员运行目录的数据库查询契约

**Files:**
- Create: `backend/internal/repository/admin_agent_runs.go`
- Create: `backend/internal/repository/admin_agent_runs_test.go`

**Interfaces:**
- Produces: `AdminAgentRunQuery`、`AdminAgentRunRecord`、`AdminAgentRunPage`；`func (r *Repository) AdminAgentRuns(query AdminAgentRunQuery, now time.Time) (AdminAgentRunPage, error)`；`func (r *Repository) AdminAgentRun(runID string, now time.Time) (*AdminAgentRunRecord, error)`。
- Row contract includes every field listed in spec section 4.2 plus `confirmationPhrase` for the detail response.

- [ ] **Step 1: 写数据库分页和分类 RED**

  使用真实表 fixture 创建不同租户、用户、项目、画布、状态与更新时间的运行，关联内部模型 Task、媒体 Task、BillingOrder 与 provider request。断言默认只返回非终态，排序为 `updated_at ASC,id ASC`，页容量仅允许 20/50/100，总数准确；`waiting_input` / `waiting_approval` 为 `awaiting_user`，其他非终态超过 600 秒为 `possibly_stalled`。

- [ ] **Step 2: 运行 RED**

  Run: `go test ./internal/repository -run 'TestAdminAgentRuns' -count=1`

  Expected: FAIL，提示目录查询契约未定义。

- [ ] **Step 3: 实现单条 SQL 聚合和分页**

  通过数据库 WHERE、JOIN/子查询与 COUNT 实现 status/activity/user/scope/updatedBefore；不把全量结果拉到 Go/Web 再过滤。只选择摘要字段，不选择 timeline 正文、prompt、模型回复、密钥或供应商原始 body。

- [ ] **Step 4: 运行 GREEN**

  Run: `go test ./internal/repository -run 'TestAdminAgentRuns' -count=1`

  Expected: PASS。

- [ ] **Step 5: 增加处置事实和隐私 RED**

  添加测试覆盖 `interruptible_now`、`cancel_request_required`、`blocked_by_unresolved_billing`、`already_terminal`，并通过 JSON 序列化断言详情不含完整 prompt、回复、密钥和 provider raw body。

- [ ] **Step 6: 实现处置派生并运行 GREEN**

  Run: `go test ./internal/repository -run 'TestAdminAgentRun' -count=1`

  Expected: PASS。

- [ ] **Step 7: 形成里程碑提交**

  ```powershell
  git add backend/internal/repository/admin_agent_runs.go backend/internal/repository/admin_agent_runs_test.go
  git commit -m "feat(admin): Agent 任务 - 增加跨租户运行事实目录"
  ```

### Task 3: 管理员终止的领域事务与关联任务处置

**Files:**
- Create: `backend/internal/repository/admin_agent_run_control.go`
- Create: `backend/internal/repository/admin_agent_run_control_test.go`
- Create: `backend/internal/service/admin_agent_runs.go`
- Create: `backend/internal/service/admin_agent_runs_test.go`
- Modify: `backend/internal/repository/agent_runtime_execution.go`

**Interfaces:**
- Produces: `AdminAgentRunInterruptCommand{RunID, ExpectedStateVersion, ActorUserID, Reason, Now}`、`AdminAgentRunInterruptResult`；`Repository.InterruptAdminAgentRun(command)` 从 Run 解析真实 scope，不接受客户端 scope。
- Produces: `Service.AdminAgentRuns(ctx, actor, query)`、`Service.AdminAgentRun(ctx, actor, runID)`、`Service.InterruptAdminAgentRun(ctx, actor, request)`。
- Stable failures: `admin_agent_run_not_found`、`admin_agent_run_state_conflict`、`admin_agent_run_terminal`、`admin_agent_run_confirmation_invalid`、`admin_agent_run_interrupt_blocked`、`admin_agent_run_billing_unresolved`。

- [ ] **Step 1: 写管理员中断事务 RED**

  测试真实 DB 中的 waiting_input、waiting_approval、queued、纯模型 running。断言事务锁定 scope/checkpoint，复用 `agentruntime.Interrupt`，只写一个终态 event/checkpoint，关闭 active timeline，未启动 pending tool 写为 `failed/admin_agent_run_interrupted`，事件 payload 包含 `source=admin`、管理员 ID、规范化原因与原状态。

- [ ] **Step 2: 运行 RED**

  Run: `go test ./internal/repository -run 'TestInterruptAdminAgentRun' -count=1`

  Expected: FAIL，提示管理员控制方法未定义。

- [ ] **Step 3: 扩展唯一 transition 提交器**

  给现有 transition 持久化增加受限 audit metadata 参数或专用包装器，使普通用户 interrupt 仍保持原契约，管理员路径能原子覆盖 event payload 的审计摘要，同时继续由 `persistAgentTimelineEvent` 关闭活动 Item。禁止新增第二套状态机。

- [ ] **Step 4: 运行 GREEN**

  Run: `go test ./internal/repository -run 'TestInterruptAdminAgentRun' -count=1`

  Expected: PASS。

- [ ] **Step 5: 写 CAS、终态、确认和账务 RED**

  repository 测试覆盖 stateVersion 冲突与并发只有一个提交；service 测试覆盖非管理员拒绝、原因长度、服务端确认短语、终态幂等、未批准 production.render 不创建 Task/BillingOrder、provider request/活动媒体 Task 只返回取消请求待核对、未决 BillingOrder 阻止终止、已结算订单不直接退款。

- [ ] **Step 6: 运行 RED**

  Run: `go test ./internal/repository ./internal/service -run 'Test(AdminAgentRun|InterruptAdminAgentRun)' -count=1`

  Expected: FAIL 于缺少鉴权、输入/处置映射或 CAS 错误契约。

- [ ] **Step 7: 实现 service 控制处置**

  `Service.InterruptAdminAgentRun` 先 `RequireAdmin`，规范化输入并读取权威详情，验证确认短语与账务 disposition，再提交领域事务；事务提交后取消本地模型 context，并复用现有 Task 取消/预留释放/账务核对入口登记外部取消意图。无法证明供应商已停止时响应保持 `cancel_request_required`。

- [ ] **Step 8: 运行 GREEN 与 race**

  Run: `go test ./internal/repository ./internal/service -run 'Test(AdminAgentRun|InterruptAdminAgentRun)' -count=1`

  Run: `go test -race ./internal/repository ./internal/service -run 'TestInterruptAdminAgentRun' -count=1`

  Expected: PASS。

- [ ] **Step 9: 形成里程碑提交**

  ```powershell
  git add backend/internal/repository/admin_agent_run_control.go backend/internal/repository/admin_agent_run_control_test.go backend/internal/repository/agent_runtime_execution.go backend/internal/service/admin_agent_runs.go backend/internal/service/admin_agent_runs_test.go
  git commit -m "feat(admin): Agent 任务 - 接通审计化终止与账务保护"
  ```

### Task 4: 管理员 Agent Run HTTP API

**Files:**
- Create: `backend/internal/handler/admin_agent_runs.go`
- Create: `backend/internal/handler/admin_agent_runs_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Produces: `GET /api/admin/agent-runs`、`GET /api/admin/agent-runs/:runId`、`POST /api/admin/agent-runs/:runId/interrupt`。
- Interrupt body: `{expectedStateVersion:number, reason:string, confirmation:string}`；冲突响应 `data` 同时包含 `errorCode` 与最新权威状态摘要。

- [ ] **Step 1: 写 handler RED**

  真实 Gin/service fixture 覆盖未登录、普通用户、管理员分页与详情、strict JSON、超大 body、非法 pageSize、错误确认、409 CAS 和成功响应 envelope `{code,data,msg}`。

- [ ] **Step 2: 运行 RED**

  Run: `go test ./internal/handler -run 'TestAdminAgentRunRoutes' -count=1`

  Expected: FAIL，路由不存在。

- [ ] **Step 3: 实现薄 handler 并注册**

  handler 只解析/限制 HTTP 输入、调用 service、通过专用 `failAdminAgentRun` 输出稳定 `errorCode`；不得在 handler 重做状态或账务判断。`main.go` 只增加 `RegisterAdminAgentRunRoutes(api, svc)`。

- [ ] **Step 4: 运行 GREEN**

  Run: `go test ./internal/handler -run 'TestAdminAgentRunRoutes' -count=1`

  Expected: PASS。

- [ ] **Step 5: 形成里程碑提交**

  ```powershell
  git add backend/internal/handler/admin_agent_runs.go backend/internal/handler/admin_agent_runs_test.go backend/cmd/server/main.go
  git commit -m "feat(api): Agent 任务 - 提供管理员查询与终止接口"
  ```

### Task 5: Web API 与纯展示域

**Files:**
- Create: `web/src/services/api/admin-agent-runs.ts`
- Create: `web/src/pages/admin/agent-runs/agent-run-presenters.ts`
- Create: `web/src/pages/admin/agent-runs/agent-run-presenters.test.ts`

**Interfaces:**
- Produces exact TypeScript DTOs matching Task 4 and functions `getAdminAgentRuns`、`getAdminAgentRun`、`interruptAdminAgentRun`。
- Produces pure functions `formatAgentRunInactivity`、`agentRunStatusLabel`、`agentRunDispositionCopy`、`agentRunImpactFacts`，only based on authoritative DTO fields.

- [ ] **Step 1: 写 presenter RED**

  用 literal DTO fixtures 断言等待用户/可能停滞/活动、模型/媒体/账务影响、`cancel_request_required` 不能显示为“已完全停止”、UTC timestamp formatting 与缺失事实的显式文案。

- [ ] **Step 2: 运行 RED**

  Run: `pnpm --dir web test --run src/pages/admin/agent-runs/agent-run-presenters.test.ts`

  Expected: FAIL，presenter 模块不存在。

- [ ] **Step 3: 实现精确 DTO 和纯函数**

  API 使用现有 `request` envelope；错误类型保留 `errorCode/latestRun`，不吞 409。Presenter 不使用客户端时钟重新分类，也不以缺失字段猜测成功。

- [ ] **Step 4: 运行 GREEN**

  Run: `pnpm --dir web test --run src/pages/admin/agent-runs/agent-run-presenters.test.ts`

  Expected: PASS。

- [ ] **Step 5: 形成里程碑提交**

  ```powershell
  git add web/src/services/api/admin-agent-runs.ts web/src/pages/admin/agent-runs/agent-run-presenters.ts web/src/pages/admin/agent-runs/agent-run-presenters.test.ts
  git commit -m "feat(admin): Agent 任务 - 增加前端数据与事实文案契约"
  ```

### Task 6: 后台 Agent 任务数据页与终止弹层

**Files:**
- Create: `web/src/pages/admin/agent-runs/agent-runs-page.tsx`
- Create: `web/src/pages/admin/agent-runs/agent-run-interrupt-modal.tsx`
- Create: `web/src/pages/admin/agent-runs/agent-runs-page.test.tsx`
- Modify: `web/src/pages/admin/admin-domain-workspace.css`
- Modify: `web/src/pages/admin/components/admin-navigation.tsx`
- Modify: `web/src/router.tsx` (only task hunk)

**Interfaces:**
- Consumes Task 5 DTO/API/presenters.
- Produces `/admin/agent-runs` data page with server filters, pagination, detail facts, one danger icon, reason + exact confirmation modal, refresh-preserving 409 behavior.

- [ ] **Step 1: 写页面状态 RED**

  React Testing Library 测试：首次 loading 使用共享表格骨架；empty 使用 `AdminTableEmpty`；首次失败替代表格且有重试；旧数据刷新失败保留表格并显示错误；筛选调用服务端参数；409 保留 reason、更新 stateVersion/confirmationPhrase 并要求重新确认。

- [ ] **Step 2: 运行 RED**

  Run: `pnpm --dir web test --run src/pages/admin/agent-runs/agent-runs-page.test.tsx`

  Expected: FAIL，页面不存在。

- [ ] **Step 3: 实现高密度数据页**

  使用现有 `AdminDataLayout`、`AdminContentSection`、共享 skeleton/empty/error、Ant Table/Modal/Input；表格只在自身内容区横向滚动。每行危险 icon button 具 `aria-label`、tooltip 和焦点；提交期间 modal 不可关闭/重复提交。

- [ ] **Step 4: 运行 GREEN**

  Run: `pnpm --dir web test --run src/pages/admin/agent-runs/agent-runs-page.test.tsx`

  Expected: PASS。

- [ ] **Step 5: 接入导航、路由与语义样式**

  在“安全与运维”加入 `Bot` 图标的“Agent 任务”；在 router 新增 lazy import 和 `agent-runs` child route。编辑 `router.tsx` 时保留现有 `prototypeRoutes` 修改，暂存时只选后台路由 hunk。样式只消费 design token，保持单层表面、32px 图标热区、响应式宽表格。

- [ ] **Step 6: 跑 focused Web 验证**

  Run: `pnpm --dir web test --run src/pages/admin/agent-runs`

  Run: `pnpm --dir web typecheck`

  Expected: PASS。

- [ ] **Step 7: 形成里程碑提交**

  ```powershell
  git add web/src/services/api/admin-agent-runs.ts web/src/pages/admin/agent-runs web/src/pages/admin/admin-domain-workspace.css web/src/pages/admin/components/admin-navigation.tsx
  git add -p web/src/router.tsx
  git commit -m "feat(admin): Agent 任务 - 增加运行目录与安全终止交互"
  ```

### Task 7: 文档、审查与发布前验收

**Files:**
- Modify: `backend/README.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify at release only: `VERSION` and versioned deployment references required by the existing release script.

**Interfaces:**
- Documents the actual Agent runtime/admin interrupt/upgrade retirement contract and v1.0.53 operator impact.

- [ ] **Step 1: 同步当前架构文档**

  `backend/README.md` 记录：管理员查询/终止走同一 runtime/CAS/event/checkpoint/timeline；外部取消采用事实化两阶段；升级迁移只退休无副作用 paused runs。根 README 增加管理员“Agent 任务”入口与升级说明；CHANGELOG `Unreleased` 归纳能力与风险边界。

- [ ] **Step 2: 执行第一次独立审查**

  对照 spec、plan 与 `git diff --stat`/`git diff`，逐项核对：权限、租户隔离、分页、隐私、CAS、事件/checkpoint/timeline、Task、BillingOrder、provider request、Artifact、迁移原子/幂等、错误码、Web 三态、409、文档。集中修复一次发现的当前 diff 缺陷。

- [ ] **Step 3: 定向复审**

  仅复查第一次审查发现项及其邻接契约；若仍出现新的跨模块 Critical/Important 或共享事务无法闭合，停止补丁循环并回到架构决策。

- [ ] **Step 4: 执行最终自动化门禁**

  ```powershell
  Push-Location backend
  go test ./...
  go test -race ./internal/database ./internal/repository ./internal/service ./internal/handler
  go build ./...
  Pop-Location
  pnpm --dir web test --run
  pnpm --dir web typecheck
  pnpm --dir web build
  git diff --check
  ```

  Expected: 全部 PASS；如因环境阻塞，保留准确命令、错误和未验证范围，不宣称成功。

- [ ] **Step 5: 隔离 PostgreSQL 与 Docker 升级回归**

  使用临时数据库/卷装载 v1.0.50 形状 fixture，验证两条无副作用 waiting_approval/waiting_input 被退休，风险 fixture 阻止升级且恢复点可用；再在 `.local/project-workbench-debug` 数据目录执行本地 v1.0.52→目标版本升级、健康检查和回滚点核验。不得直接改生产数据库。

- [ ] **Step 6: 浏览器真实流程**

  本地管理员登录 `/admin/agent-runs`，验证分页筛选、详情隐私、错误确认、合法终止、409、刷新后终态和普通用户 403；同时确认普通任务中心不出现 Agent 内部模型 Task。保存截图/网络响应作为 QA 证据。

- [ ] **Step 7: 核对并形成聚焦功能提交**

  逐文件检查 `git status`、`git diff --cached --stat`、`git diff --cached`，确认没有 prototype、密钥、数据库、构建产物或本地临时文件。若前面里程碑提交已覆盖实现，此提交只包含架构文档和收口修复：

  ```powershell
  git add backend/README.md README.md CHANGELOG.md
  git commit -m "docs(agent): Agent 任务 - 同步后台终止与升级退休架构"
  ```

- [ ] **Step 8: 发布 v1.0.53（仅在用户再次明确授权 push/tag/release 后）**

  使用现有发布脚本同步 VERSION、CHANGELOG、镜像标签和静态资产清单，形成 `chore(release): 版本发布 - publish v1.0.53`；发布前后分别核对健康、静态资源、管理员路由和回滚点。未获当次授权时只停在本地验证与聚焦提交，不 push、不打标签。
