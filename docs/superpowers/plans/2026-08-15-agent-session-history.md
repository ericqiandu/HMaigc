# Agent 服务端会话历史与恢复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 为当前画布提供按用户强隔离的服务端 Agent 对话列表，并让 Web 跨设备选择和恢复每个对话的最新运行。

**Architecture:** 后端新增只读历史投影，在数据库层按用户、租户、项目和画布过滤，一次聚合每个 Thread 的最新 Run/Checkpoint；Service 复用现有 RuntimeState 一致性校验。Web 严格解析聚合 DTO，本地句柄只做快速恢复缓存，服务端列表负责权威发现；面板增加独立历史列表组件，不改变 Runtime 状态机。

**Tech Stack:** Go 1.24、Gin、GORM、SQLite/PostgreSQL；React 19、TypeScript strict、Ant Design、Vitest/Bun、Vite。

## Global Constraints

- 只参考 ViMax Agent Loop 的会话复用能力，不复制固定创作流水线或代码。
- 不修改数据库结构，不新增兼容层、默认会话、默认运行或静默回退。
- 历史查询必须在数据库层按用户、租户、项目和画布过滤，上限 20。
- activityAt 等于 latestRun.updatedAt；空 Thread 使用 thread.updatedAt。排序不得写 Runtime。
- latestRun 复用 AgentRuntimeView；空 Thread 显式返回 null。
- 不实现删除、归档、重命名、搜索、完整 transcript、多 Agent 或进化审批。
- 不新增 any / as any；新增 JSX 标签必须有具名 className。
- 日常只跑 focused tests；稳定后只做一次全量门禁和本地镜像重建。
- 本计划共享同一 Thread/Run/Checkpoint 契约，按仓库约束默认由主代理使用 superpowers:executing-plans 顺序执行；用户未明确授权时不启用子代理实现循环。

## File Map

### Create

- backend/internal/repository/agent_runtime_history.go：单次、有界、强作用域的历史投影。
- backend/internal/service/agent_runtime_history.go：授权、状态解析和公共 DTO。
- web/src/components/canvas/agent-runtime-history-list.tsx：历史列表纯展示组件。

### Modify

- backend/internal/handler/agent_runtime.go
- backend/internal/handler/agent_runtime_test.go
- backend/internal/service/agent_runtime_transport.go
- web/src/services/api/agent-runtime.ts
- web/src/components/canvas/use-agent-runtime.ts
- web/src/components/canvas/canvas-assistant-panel.tsx
- web/src/components/canvas/canvas-agent-panel.css
- web/test/agent-runtime-api.test.ts
- web/test/canvas-agent-runtime-panel.test.tsx
- README.md
- docs/content/docs/pending-test.mdx

---

### Task 1: 后端按作用域聚合对话与最新运行

**Files:**
- Create: backend/internal/repository/agent_runtime_history.go
- Create: backend/internal/service/agent_runtime_history.go
- Modify: backend/internal/service/agent_runtime_transport.go
- Modify: backend/internal/handler/agent_runtime.go:47-99
- Test: backend/internal/handler/agent_runtime_test.go

**Interfaces:**
- Consumes: Service.AuthorizeAgentScope、agentruntime.Scope、AgentRuntimeView、RuntimeState。
- Produces:

~~~go
type AgentThreadHistoryRecord struct {
    Thread model.AgentThread
    Run *model.AgentRun
    StateJSON string
    ActivityAt time.Time
}

func (r *Repository) AgentThreadHistory(scope agentruntime.Scope, limit int) ([]AgentThreadHistoryRecord, error)

type AgentThreadHistoryItem struct {
    Thread model.AgentThread
    ActivityAt time.Time
    LatestRun *AgentRuntimeView
}

type AgentThreadHistoryView struct {
    Items []AgentThreadHistoryItem
}

func (s *Service) ListAgentThreads(actor *model.User, canvasID string, limit int) (*AgentThreadHistoryView, error)
~~~

- [ ] **Step 1: 写 HTTP seam 的失败测试**

在 handler test 新增 TestAgentRuntimeHTTPListsOnlyCurrentCanvasActorThreadsByActivity。用现有 fixture 和 Repository 公共接口构造：

- 当前用户、当前画布两个带合法 checkpoint 的 Thread/Run，较新的 Run.UpdatedAt 更晚。
- 当前用户、当前画布一个没有 Run 的空 Thread。
- 当前用户其他画布一个 Thread。
- 其他用户当前画布一个 Thread。

只通过 GET 响应断言：

~~~go
response := fixture.request(
    http.MethodGet,
    "/api/agent/threads?canvasId=handler-agent-history-canvas&limit=20",
    "",
    fixture.userCookie,
    "",
)
if response.Code != http.StatusOK {
    t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
}
if len(envelope.Data.Items) != 3 {
    t.Fatalf("expected 3 isolated items, got %d", len(envelope.Data.Items))
}
if envelope.Data.Items[0].Thread.ID != "history-thread-newer" {
    t.Fatalf("expected stable activity order")
}
if envelope.Data.Items[0].LatestRun.State.UserMessage != "较新的任务" {
    t.Fatalf("latest run was not projected")
}
if empty.LatestRun != nil || !empty.ActivityAt.Equal(empty.Thread.UpdatedAt) {
    t.Fatalf("empty thread facts are invalid")
}
~~~

追加 HTTP 断言：匿名 401；缺 canvasId、limit=0、limit=21、limit=1.5、limit 尾随字符均 400；其他用户和画布事实不可见。

- [ ] **Step 2: 运行测试确认真实 RED**

~~~powershell
cd backend
go test ./internal/handler -run '^TestAgentRuntimeHTTPListsOnlyCurrentCanvasActorThreadsByActivity$' -count=1
~~~

Expected: FAIL，因为 GET 路由或 ListAgentThreads 尚不存在。测试夹具、JSON 和数据库初始化必须先能正常运行。

- [ ] **Step 3: 实现单次有界 Repository 投影**

使用窗口函数选择每个 Thread 的最新 Run，并连接对应 state_version 的 checkpoint：

~~~sql
WITH scoped_threads AS (
  SELECT *
    FROM agent_threads
   WHERE tenant_kind = ?
     AND tenant_id = ?
     AND created_by_user_id = ?
     AND domain_project_id = ?
     AND canvas_id = ?
), ranked_runs AS (
  SELECT agent_runs.*,
         ROW_NUMBER() OVER (
           PARTITION BY agent_runs.thread_id
           ORDER BY agent_runs.updated_at DESC, agent_runs.id DESC
         ) AS row_number
    FROM agent_runs
    JOIN scoped_threads ON scoped_threads.id = agent_runs.thread_id
), latest_runs AS (
  SELECT * FROM ranked_runs WHERE row_number = 1
)
SELECT scoped_threads.id AS thread_id,
       scoped_threads.tenant_kind AS thread_tenant_kind,
       scoped_threads.tenant_id AS thread_tenant_id,
       scoped_threads.created_by_user_id AS thread_created_by_user_id,
       scoped_threads.domain_project_id AS thread_domain_project_id,
       scoped_threads.canvas_id AS thread_canvas_id,
       scoped_threads.status AS thread_status,
       scoped_threads.created_at AS thread_created_at,
       scoped_threads.updated_at AS thread_updated_at,
       latest_runs.id AS run_id,
       latest_runs.thread_id AS run_thread_id,
       latest_runs.actor_user_id AS run_actor_user_id,
       latest_runs.client_request_id AS run_client_request_id,
       latest_runs.status AS run_status,
       latest_runs.last_event_sequence AS run_last_event_sequence,
       latest_runs.state_version AS run_state_version,
       latest_runs.step_number AS run_step_number,
       latest_runs.max_steps AS run_max_steps,
       latest_runs.model_record_id AS run_model_record_id,
       latest_runs.model_key AS run_model_key,
       latest_runs.tool_schema_version AS run_tool_schema_version,
       latest_runs.created_at AS run_created_at,
       latest_runs.updated_at AS run_updated_at,
       latest_runs.completed_at AS run_completed_at,
       agent_checkpoints.state_json AS latest_state_json,
       COALESCE(latest_runs.updated_at, scoped_threads.updated_at) AS activity_at
  FROM scoped_threads
  LEFT JOIN latest_runs ON latest_runs.thread_id = scoped_threads.id
  LEFT JOIN agent_checkpoints
    ON agent_checkpoints.run_id = latest_runs.id
   AND agent_checkpoints.state_version = latest_runs.state_version
 ORDER BY activity_at DESC, scoped_threads.id DESC
 LIMIT ?
~~~

所有列使用唯一 alias 扫描到私有扁平 row，再显式组装 record。校验：

~~~go
if err := scope.Validate(); err != nil { return nil, err }
if limit < 1 || limit > 20 {
    return nil, errors.New("agent thread history limit is invalid")
}
if row.LatestRunID.Valid != row.LatestStateJSON.Valid {
    return nil, errors.New("agent thread latest run checkpoint facts are incomplete")
}
~~~

不得在 Go 内存过滤作用域，不得按 Thread 循环查询 Run 或 Checkpoint。

- [ ] **Step 4: 实现 Service 与 Handler 最小闭环**

Service 在 actor 为空时返回现有 Unauthorized 错误；trim 后 canvasID 为空直接返回普通校验错误。随后使用 AuthorizeAgentScope(actor.ID, canvasID, "thread-history-probe", "run-history-probe") 授权。抽取共享 agentRuntimeViewFromFacts(run, state)，由 readAgentRuntimeView 与 history 共用，严格校验 status、stateVersion、stepNumber、maxSteps。DTO 的 JSON 字段名必须精确为 thread、activityAt、latestRun、items。

Handler 注册：

~~~go
agent.GET("/threads", func(c *gin.Context) {
    user, err := currentUser(c, svc)
    if err != nil {
        failService(c, err)
        return
    }
    limit, err := strictOptionalPositiveInt(c.Query("limit"), 20, 20)
    if err != nil {
        fail(c, http.StatusBadRequest, errors.New("limit 必须在 1 到 20 之间"))
        return
    }
    if strings.TrimSpace(c.Query("canvasId")) == "" {
        fail(c, http.StatusBadRequest, errors.New("canvasId 不能为空"))
        return
    }
    view, err := svc.ListAgentThreads(user, c.Query("canvasId"), limit)
    if err != nil {
        failService(c, err)
        return
    }
    ok(c, view)
})
~~~

整数解析只允许十进制 1–20，拒绝负数、小数、尾随字符和溢出。

- [ ] **Step 5: 运行 focused GREEN 并提交**

~~~powershell
cd backend
gofmt -w internal/repository/agent_runtime_history.go internal/service/agent_runtime_history.go internal/service/agent_runtime_transport.go internal/handler/agent_runtime.go internal/handler/agent_runtime_test.go
go test ./internal/handler -run '^TestAgentRuntimeHTTP(ListsOnlyCurrentCanvasActorThreadsByActivity|CreatesScopedThreadAndRejectsMalformedRequests|ReadsPersistedRunAndResumesSSEAfterSequence)$' -count=1
~~~

Expected: PASS。

~~~powershell
git add backend/internal/repository/agent_runtime_history.go backend/internal/service/agent_runtime_history.go backend/internal/service/agent_runtime_transport.go backend/internal/handler/agent_runtime.go backend/internal/handler/agent_runtime_test.go
git commit -m "feat(agent): 增加画布会话历史接口"
~~~

---

### Task 2: Web 严格解析服务端历史投影

**Files:**
- Modify: web/src/services/api/agent-runtime.ts
- Test: web/test/agent-runtime-api.test.ts

**Interfaces:**

~~~ts
export type AgentThreadHistoryItem = {
    thread: {
        id: string;
        canvasId: string;
        status: "active";
        createdAt: string;
        updatedAt: string;
    };
    activityAt: string;
    latestRun: AgentRuntimeView | null;
};
export type AgentThreadHistoryView = { items: AgentThreadHistoryItem[] };
export function parseAgentThreadHistory(value: unknown): AgentThreadHistoryView;
~~~

AgentRuntimeClient 新增 listThreads(canvasId, limit?)。

- [ ] **Step 1: 写 DTO 解析 RED**

新增合法列表测试，包含一个有 latestRun 和一个 latestRun:null 的 Thread；断言 userMessage、activityAt、Thread ID 被保留。再断言以下输入明确失败：

- Thread status 不是 active。
- latestRun.run.threadId 与 Thread ID 不同。
- activityAt 不是 UTC ISO-8601。
- items 超过 20。
- items、thread 或 latestRun 缺失必填字段。

合法测试核心：

~~~ts
const history = parseAgentThreadHistory({
    items: [
        {
            thread: {
                id: "thread-1",
                canvasId: "canvas-1",
                status: "active",
                createdAt: "2026-08-15T01:00:00Z",
                updatedAt: "2026-08-15T01:00:00Z",
            },
            activityAt: "2026-08-15T02:00:00Z",
            latestRun,
        },
        {
            thread: {
                id: "thread-empty",
                canvasId: "canvas-1",
                status: "active",
                createdAt: "2026-08-15T00:00:00Z",
                updatedAt: "2026-08-15T00:00:00Z",
            },
            activityAt: "2026-08-15T00:00:00Z",
            latestRun: null,
        },
    ],
});
expect(history.items[0].latestRun?.state.userMessage).toBe(latestRun.state.userMessage);
~~~

- [ ] **Step 2: 运行解析测试确认 RED**

~~~powershell
cd web
bun test test/agent-runtime-api.test.ts
~~~

Expected: FAIL，因为解析器和 client operation 不存在。

- [ ] **Step 3: 实现严格解析与 client operation**

新增 isoInstant 结构校验器，只用于日期格式，不用于语义判断。parseAgentThreadHistory 复用 parseAgentRuntimeView，并验证 latestRun.run.threadId。

~~~ts
listThreads: async (canvasId, limit = 20) =>
    parseAgentThreadHistory(
        await request(
            "/agent/threads?canvasId=" +
                encodeURIComponent(canvasId) +
                "&limit=" +
                String(limit),
        ),
    ),
~~~

解析结果超过 20 项原地失败，不截断。

- [ ] **Step 4: 运行 focused GREEN**

~~~powershell
cd web
bun test test/agent-runtime-api.test.ts
~~~

Expected: PASS，现有 Runtime DTO 测试继续通过。

- [ ] **Step 5: 提交 Web 契约切片**

~~~powershell
git add web/src/services/api/agent-runtime.ts web/test/agent-runtime-api.test.ts
git commit -m "feat(web): 解析 Agent 会话历史"
~~~

---

### Task 3: 面板历史选择与跨设备恢复

**Files:**
- Create: web/src/components/canvas/agent-runtime-history-list.tsx
- Modify: web/src/components/canvas/use-agent-runtime.ts
- Modify: web/src/components/canvas/canvas-assistant-panel.tsx
- Modify: web/src/components/canvas/canvas-agent-panel.css
- Test: web/test/canvas-agent-runtime-panel.test.tsx

**Interfaces:** useAgentRuntime 新增 threads、historyLoading、historyError、selectedThreadId、selectThread、reloadThreads；保留现有字段和 action。

- [ ] **Step 1: 写恢复与选择行为 RED**

为所有既有 AgentRuntimeClient fixture 明确增加 listThreads；没有历史的测试返回 items:[]。

新增三个公共 UI 行为：

1. 无本地句柄时采用服务端第一项，恢复 running Run 并保存 activeRunId。
2. 打开“历史对话”，选择旧终态对话后显示其 finalMessage 并保存 Thread。
3. 历史接口失败时显示独立错误，但有效本地 activeRunId 仍通过 getRun 恢复。

关键断言：

~~~tsx
await waitFor(() => expect(container.textContent).toContain("正在生成"));
expect(saved.at(-1)).toMatchObject({
    threadId: "thread-active",
    activeRunId: "run-active",
});
clickButton(container, "历史对话");
clickButton(container, "旧对话");
await waitFor(() => expect(container.textContent).toContain("旧结果"));
~~~

再增加新 Run 启动后 listThreads 再调用一次；刷新失败不得把已启动 Run 改成失败。

- [ ] **Step 2: 运行面板测试确认 RED**

~~~powershell
cd web
bun test test/canvas-agent-runtime-panel.test.tsx
~~~

Expected: 补齐 fixture 后因未加载历史、没有历史入口而行为失败。

- [ ] **Step 3: 实现历史协调状态**

初始化时使用 Promise.allSettled 同时读取 storage.load 和 client.listThreads，但分别暴露错误。选择顺序固定：

1. pendingRun。
2. activeRunId。
3. 本地 threadId 对应的服务端项。
4. 服务端第一项。

画布 effect 保留 cancelled fence；迟到 Promise 不得写入新画布。选择 Thread 时停止旧订阅、清游标、清事件与选区提交去重，采用 latestRun 并保存本地句柄。切走不会取消服务端 Run。

submit 成功后调用 reloadThreads；刷新不阻塞 Run 结果，失败只写 historyError。newThread 清当前选择但保留 threads。

- [ ] **Step 4: 实现紧凑历史列表**

组件 public props：

~~~tsx
type AgentRuntimeHistoryListProps = {
    items: AgentThreadHistoryItem[];
    selectedThreadId: string;
    loading: boolean;
    error: string;
    onSelect: (item: AgentThreadHistoryItem) => void;
    onRetry: () => void;
};
~~~

面板头部增加 History 图标，aria-label 为“历史对话”。每项展示最新 userMessage 或“尚未开始”、运行状态与 activityAt。标题只用 CSS line-clamp 截断，不在 JS 推断或改写语义。历史错误使用 role=alert 和“重试历史”按钮。

样式类统一使用 canvas-agent-runtime-history- 前缀，单层容器，390px 无横向溢出。

- [ ] **Step 5: 运行 focused GREEN 并提交**

~~~powershell
cd web
bun test test/canvas-agent-runtime-panel.test.tsx test/agent-runtime-api.test.ts
~~~

Expected: PASS，既有恢复、审批、选区与幂等测试不退化。

~~~powershell
git add web/src/components/canvas/agent-runtime-history-list.tsx web/src/components/canvas/use-agent-runtime.ts web/src/components/canvas/canvas-assistant-panel.tsx web/src/components/canvas/canvas-agent-panel.css web/test/canvas-agent-runtime-panel.test.tsx
git commit -m "feat(web): 增加 Agent 历史恢复入口"
~~~

---

### Task 4: 文档同步、一次审查与最终门禁

**Files:**
- Modify: README.md:22-31
- Modify: docs/content/docs/pending-test.mdx
- Review: Task 1–3 全部 diff

**Interfaces:** 产出当前架构说明、人工验收清单和最终门禁证据。

- [ ] **Step 1: 同步文档**

README 增加正式 GET 历史接口、服务端权威发现、本地快速缓存和显式失败说明。pending-test 增加：

- 同一用户同画布刷新恢复。
- 无本地句柄跨设备恢复最近对话。
- 选择旧对话。
- 历史失败但已有运行仍展示。
- 其他用户/画布对话不可见。
- 新 Run 后列表刷新。

- [ ] **Step 2: 执行一次显式 review**

~~~powershell
git diff 4e05674..HEAD -- backend/internal/repository/agent_runtime_history.go backend/internal/service/agent_runtime_history.go backend/internal/service/agent_runtime_transport.go backend/internal/handler/agent_runtime.go backend/internal/handler/agent_runtime_test.go web/src/services/api/agent-runtime.ts web/src/components/canvas/use-agent-runtime.ts web/src/components/canvas/agent-runtime-history-list.tsx web/src/components/canvas/canvas-assistant-panel.tsx web/src/components/canvas/canvas-agent-panel.css web/test/agent-runtime-api.test.ts web/test/canvas-agent-runtime-panel.test.tsx README.md docs/content/docs/pending-test.mdx
~~~

必须确认：

- 无 N+1、内存权限过滤、Thread 写排序或 schema 迁移。
- 无默认会话、默认 Run、静默回退或本地语义路由。
- Run/Checkpoint 冲突显式失败；空 Thread 只返回 null。
- 画布切换有迟到异步 fence；选择历史重置订阅和游标。
- 无 any、密钥、构建产物和无关文件。
- 生产改动不超过 12 文件或约 975 行净新增；超过即停止并重新拆分。

当前 diff 缺陷只集中修复一次并重跑受影响 focused tests，不扩展暂缓范围。

- [ ] **Step 3: 执行最终自动化门禁**

~~~powershell
cd backend
go test ./... -count=1
go vet ./...
go build ./...

cd ..\web
bun test
bun run build

cd ..
git diff --check
~~~

Expected: 全部 exit 0。Go 全测、vet、build 串行执行，避免共享 toolchain 缓存竞争。

- [ ] **Step 4: 安全重建本地镜像**

先只读核对：

~~~powershell
docker compose -p hmaigc-local -f docker-compose.yml config
docker inspect hmaigc-local-backend-1 --format '{{json .Mounts}}'
~~~

确认数据挂载仍指向仓库 .local/data 后执行：

~~~powershell
docker compose -p hmaigc-local -f docker-compose.yml up -d --build backend web --wait
docker compose -p hmaigc-local -f docker-compose.yml ps
curl.exe -fsS http://127.0.0.1:8080/api/health
curl.exe -fsS http://127.0.0.1:3000/
~~~

Expected: backend/web healthy，两个 URL 返回 2xx。不得删除 volume、清空数据库或启动额外测试数据库。

- [ ] **Step 5: 提交文档并核对工作区**

~~~powershell
git add README.md docs/content/docs/pending-test.mdx
git diff --cached --name-status
git diff --cached --check
git commit -m "docs(agent): 更新会话恢复验收说明"
git status --short
git log -5 --oneline
~~~

现有 .agents/memory/hmaigc/core.md、旧设计删除和 .superpowers/ 保持原样，不得混入提交。
