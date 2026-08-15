# Generation Credit Quote Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** 让图片与视频节点基于后端唯一计价逻辑实时显示“闪电标志 + 预计积分”，并保证每个实际生成任务冻结的积分与用户最后确认的报价一致。

**Architecture:** 后端把现有订单计价拆成无写入的单任务报价快照，再由同一快照构造订单；只读报价接口额外汇总画布批量任务总额。前端使用可取消、防旧响应覆盖的报价 hook 展示总额，并把单任务报价版本与指纹沿现有执行链传到任务提交；没有可见面板报价的自动编排路径会在提交前取得单任务报价。

**Tech Stack:** Go、Gin、GORM、React 19、TypeScript strict、Axios、Bun Test、happy-dom、Lucide React。

## Global Constraints

- 后端是报价与冻结积分的唯一事实源；删除前端 requestCreditCost 计价公式，不保留双轨兼容。
- 报价接口只读，不创建 BillingOrder、不冻结积分、不写流水。
- 图片和视频多输出在后端汇总；每个实际任务仍独立报价、独立冻结。
- 远程图片/视频任务必须携带 quotePriceVersion 与 quoteFingerprint，失败显式返回，不使用默认价格。
- 不暴露供应商成本、利润率、价格层 ID 或内部价格快照。
- 不新增 any、as any、硬编码模型枚举、静默降级或隐式兜底。
- 保留现有节点布局和预计积分位置，只把积分图标统一成产品闪电标志。
- Agent Token 计费、后台价格编辑、历史订单和数据库结构不在本期范围。
- 生产变更预算：4 个职责、约 15 个生产文件、600–900 行净新增/修改；若超过 22 个生产文件或 1,350 行，停止实现并重新拆分。
- 定向 RED/GREEN 只跑 focused tests；全量 Go、race、Web 与构建只在最终稳定点执行一次。

---

## File Map

### Backend

- Create backend/internal/service/billing_quote.go: 报价请求/响应、规范化快照、批量汇总、指纹和价格变化错误。
- Create backend/internal/service/billing_quote_test.go: 报价领域契约与只读性测试。
- Modify backend/internal/service/finance.go: 订单和报价复用同一价格快照。
- Modify backend/internal/service/service.go: CreateTaskRequest 接收报价确认；创建与重试路径校验报价。
- Modify backend/internal/handler/finance.go: 注册 POST /api/billing/quotes。
- Modify backend/internal/handler/auth.go: 输出 HTTP 409 结构化价格变化错误。
- Create backend/internal/handler/billing_quote_test.go: 登录、只读、脱敏、匹配提交与冲突提交测试。

### Web

- Modify web/src/services/api/task-center.ts: 报价 DTO、报价请求、提交前单任务复核和价格变化错误。
- Create web/src/components/canvas/use-generation-credit-quote.ts: 250ms 防抖、取消、旧响应隔离。
- Create web/src/components/canvas/generation-credit-badge.tsx: 统一计算中、成功与失败显示。
- Modify web/src/constant/credits.tsx: CreditSymbol 改用 Zap；删除本地计价公式。
- Modify web/src/components/canvas/canvas-config-node-panel.tsx and canvas-node-prompt-panel.tsx: 使用后端报价并传确认。
- Modify web/src/pages/canvas/use-canvas-generation-executor.ts, canvas-generation-executor-types.ts, canvas-image-generation-executor.ts, canvas-media-generation-executors.ts: 透传单任务报价确认。
- Modify web/src/lib/canvas/canvas-project-generation.ts: 把确认交给 createGenerationTask。
- Replace web/test/credits.test.ts and create web/test/generation-credit-quote.test.tsx; modify web/test/canvas-project-generation.test.ts。

---

### Task 1: Backend Single-Source Quote Domain

**Files:**
- Create: backend/internal/service/billing_quote.go
- Create: backend/internal/service/billing_quote_test.go
- Modify: backend/internal/service/finance.go:356-594

**Interfaces:**
- Consumes: BillingUsage, requireAccessibleChannelModel, creditPolicy, matchSuperResolutionPricingRule。
- Produces:

~~~go
type TaskBillingQuoteRequest struct {
    Type       string
    Operation  string
    BatchCount int64
    Input      TaskBillingQuoteInput
}

type TaskBillingQuoteInput struct {
    Mode                string
    ReferenceVideoCount int64
    Config              TaskBillingQuoteConfig
}

type TaskBillingQuoteConfig struct {
    ChannelID                      string
    Model                          string
    Size                           string
    Quality                        string
    VideoSeconds                   string
    VideoQuality                   string
    SuperResolutionEnabled         bool
    SuperResolutionResolution      string
    SuperResolutionVersion         string
    SuperResolutionFramesPerSecond int
}

type TaskBillingQuote struct {
    AmountMicrocredits             int64
    PerTaskAmountMicrocredits      int64
    TaskCount                      int64
    PriceVersion                  int64
    BillingMode                   string
    PricingResolution             string
    PricingInputVariant           string
    Quantity                      int64
    EnhancementAmountMicrocredits int64
    QuoteFingerprint              string
}

type TaskBillingQuoteConfirmation struct {
    PriceVersion     int64
    QuoteFingerprint string
}

func (s *Service) QuoteTaskBilling(userID string, req TaskBillingQuoteRequest) (*TaskBillingQuote, error)
~~~

- [ ] **Step 1: Write failing quote-domain tests**

Start with a complete fixed-request video batch case:

~~~go
func TestQuoteTaskBillingAggregatesFixedVideoWithoutChangingPerTaskQuantity(t *testing.T) {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if err != nil { t.Fatal(err) }
    if err := db.AutoMigrate(&model.ChannelModel{}, &model.ChannelModelPriceTier{}, &model.SystemSetting{}, &model.MembershipPlan{}, &model.MembershipSubscription{}, &model.TeamMember{}); err != nil { t.Fatal(err) }
    item := model.ChannelModel{ID: "video-model", ChannelID: "channel", ModelKey: "fixed-video", Capability: "video", BillingMode: "fixed_request", PriceStrategy: "flat", UnitPriceMicrocredits: 1_000_000, PriceConfigured: true, Enabled: true, PriceVersion: 3, AccessPolicy: model.ModelAccessAuthenticated}
    if err := db.Create(&item).Error; err != nil { t.Fatal(err) }
    svc := &Service{repo: repository.New(db)}
    quote, err := svc.QuoteTaskBilling("user", TaskBillingQuoteRequest{Type: "canvas_video", Operation: "text_to_video", BatchCount: 4, Input: TaskBillingQuoteInput{Mode: "video", Config: TaskBillingQuoteConfig{ChannelID: "channel", Model: "fixed-video", VideoSeconds: "15", VideoQuality: "720p"}}})
    if err != nil { t.Fatal(err) }
    if quote.PerTaskAmountMicrocredits != 1_000_000 || quote.AmountMicrocredits != 4_000_000 || quote.Quantity != 1 || quote.TaskCount != 4 { t.Fatalf("quote = %#v", quote) }
}
~~~

Add separate named tests with exact assertions: image 2K batch 3 gives 803,000 per task and 2,409,000 total; reference-video 720P batch 2 at 6 seconds gives 9,780,000 per task and 19,560,000 total; batchCount 0 and 16 fail; quote succeeds without a wallet balance and creates zero billing orders and ledger rows; missing price, unsupported resolution and inaccessible member-only model fail explicitly; super-resolution total is multiplied by the batch count; fingerprints are stable for identical charged facts and change for resolution, reference-video variant, duration, multiplier, price version or enhancement changes.

- [ ] **Step 2: Run focused tests and verify RED**

~~~powershell
cd backend
go test ./internal/service -run 'TestQuoteTaskBilling' -count=1
~~~

Expected: compile failure because TaskBillingQuoteRequest and QuoteTaskBilling do not exist.

- [ ] **Step 3: Implement the immutable quote snapshot**

Create this internal owner type:

~~~go
type billingQuoteSnapshot struct {
    ChannelModelID                    string
    Model                             string
    Capability                        string
    BillingMode                       string
    PriceVersion                     int64
    PriceTierID                      string
    PricingResolution                string
    PricingInputVariant              string
    UnitPriceMicrocredits            int64
    MultiplierBasisPoints            int64
    Quantity                         int64
    AmountMicrocredits               int64
    EnhancementPricingRuleID         string
    EnhancementUnitPriceMicrocredits int64
    EnhancementAmountMicrocredits    int64
    EnhancementPricingSnapshotJSON   string
}
~~~

Move price matching and arithmetic out of newBillingOrder into:

~~~go
func (s *Service) calculateBillingQuote(userID, channelID, modelKey, capability string, usage BillingUsage) (*billingQuoteSnapshot, error)
func billingQuoteFingerprint(snapshot billingQuoteSnapshot) (string, error)
func publicTaskBillingQuote(snapshot billingQuoteSnapshot, batchCount int64) (*TaskBillingQuote, error)
~~~

The fingerprint is lowercase SHA-256 hex over JSON of charged fields. Exclude order identity, Scene, supplier cost ranges and timestamps. Use checked multiplication for total amount.

- [ ] **Step 4: Make order construction consume the snapshot**

Reduce newBillingOrder to:

~~~go
snapshot, err := s.calculateBillingQuote(userID, channelID, modelKey, capability, usage)
if err != nil {
    return nil, err
}
teamID, err := s.billingTeamID(userID, time.Now())
if err != nil {
    return nil, err
}
return billingOrderFromQuote(snapshot, billingOrderIdentity{
    ID: newID(), UserID: userID, TeamID: teamID,
    TaskID: taskID, IdempotencyKey: idempotencyKey, Scene: scene,
}), nil
~~~

QuoteTaskBilling maps the precise request DTO into the same internal priced facts used by taskBillingOrder, forces one actual task, validates BatchCount from 1 through 15, then aggregates in the backend. Add explicit json tags matching the approved camelCase wire names when implementing these structs.

- [ ] **Step 5: Run focused tests and verify GREEN**

~~~powershell
cd backend
go test ./internal/service -run 'TestQuoteTaskBilling|TestNewBillingOrder|TestTaskBillingOrder|TestNormalize.*Pricing' -count=1
~~~

Expected: PASS and all quote/order comparisons match.

- [ ] **Step 6: Commit**

~~~powershell
git add backend/internal/service/billing_quote.go backend/internal/service/billing_quote_test.go backend/internal/service/finance.go
git commit -m "refactor(billing): 统一生成任务报价内核"
~~~

---

### Task 2: Quote API and Submission Consistency Gate

**Files:**
- Modify: backend/internal/service/service.go:67-78,338-410,520-565
- Modify: backend/internal/handler/finance.go:18-110
- Modify: backend/internal/handler/auth.go:906-914
- Create: backend/internal/handler/billing_quote_test.go

**Interfaces:**
- Consumes: QuoteTaskBilling and TaskBillingQuoteConfirmation from Task 1.
- Produces:

~~~go
type BillingQuoteChangedError struct {
    CurrentQuote TaskBillingQuote
}
~~~

Add QuotePriceVersion int64 with json name quotePriceVersion and QuoteFingerprint string with json name quoteFingerprint to the existing CreateTaskRequest; do not replace its existing fields.

- [ ] **Step 1: Write failing HTTP and service consistency tests**

Create TestBillingQuoteRouteRequiresLoginAndDoesNotExposeCostFacts, TestCreateTaskRejectsMissingImageVideoQuote, TestCreateTaskRejectsChangedQuoteWithoutWritingCommercialFacts, TestCreateTaskFreezesExactlyTheConfirmedQuote, and TestRetryTaskObtainsCurrentInternalQuoteBeforeReservation. Use the repository's existing Gin/session fixture pattern. Each test must query Task, BillingOrder and CreditLedgerEntry counts after the request; the conflict case asserts all three remain zero and the matching case asserts one task, one reserved order and one reserve ledger entry.

For mismatch, assert HTTP 409:

~~~json
{
  "code": 409,
  "data": {
    "errorCode": "PRICE_CHANGED",
    "currentQuote": {}
  },
  "msg": "预计积分已变化，请确认新报价后重试"
}
~~~

Assert public JSON contains none of supplier, cost, margin, priceTierId, snapshot or credential fields.

- [ ] **Step 2: Run focused tests and verify RED**

~~~powershell
cd backend
go test ./internal/handler ./internal/service -run 'TestBillingQuoteRoute|TestCreateTask.*Quote|TestRetryTaskObtainsCurrentInternalQuote' -count=1
~~~

Expected: compile or route-not-found failures.

- [ ] **Step 3: Register the authenticated read-only route**

At the start of RegisterFinanceRoutes:

~~~go
r.POST("/billing/quotes", func(c *gin.Context) {
    user, err := currentUser(c, svc)
    if err != nil {
        failService(c, err)
        return
    }
    c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
    var req service.TaskBillingQuoteRequest
    decoder := json.NewDecoder(c.Request.Body)
    decoder.DisallowUnknownFields()
    if err := decoder.Decode(&req); err != nil {
        fail(c, http.StatusBadRequest, err)
        return
    }
    quote, err := svc.QuoteTaskBilling(user.ID, req)
    if err != nil {
        failService(c, err)
        return
    }
    ok(c, quote)
})
~~~

- [ ] **Step 4: Enforce confirmation before image/video writes**

CreateTask recomputes the single-task quote and compares both facts before createTaskWithinStorageQuota:

~~~go
confirmation := TaskBillingQuoteConfirmation{
    PriceVersion: req.QuotePriceVersion,
    QuoteFingerprint: strings.TrimSpace(req.QuoteFingerprint),
}
order, err := s.taskBillingOrder(userID, &task, normalizedInput, confirmation)
~~~

Canvas image/video tasks reject empty confirmation. Internal retry obtains the current confirmation first and feeds the same path. Agent/token tasks keep their existing token billing path.

- [ ] **Step 5: Return a structured conflict**

Extend failService before AuthError:

~~~go
var changed *service.BillingQuoteChangedError
if errors.As(err, &changed) {
    c.JSON(http.StatusConflict, gin.H{
        "code": http.StatusConflict,
        "data": gin.H{
            "errorCode": "PRICE_CHANGED",
            "currentQuote": changed.CurrentQuote,
        },
        "msg": "预计积分已变化，请确认新报价后重试",
    })
    return
}
~~~

- [ ] **Step 6: Run focused tests and verify GREEN**

~~~powershell
cd backend
go test ./internal/handler ./internal/service -run 'TestBillingQuoteRoute|TestCreateTask.*Quote|TestRetryTaskObtainsCurrentInternalQuote|TestTaskBillingOrder' -count=1
~~~

Expected: PASS; quote and mismatch paths write no Task, BillingOrder or CreditLedgerEntry.

- [ ] **Step 7: Commit**

~~~powershell
git add backend/internal/service/service.go backend/internal/handler/finance.go backend/internal/handler/auth.go backend/internal/handler/billing_quote_test.go
git commit -m "feat(billing): 校验生成任务报价"
~~~

---

### Task 3: Web Quote Client and Race-Safe State

**Files:**
- Modify: web/src/services/api/task-center.ts:1-205
- Create: web/src/components/canvas/use-generation-credit-quote.ts
- Create: web/src/components/canvas/generation-credit-badge.tsx
- Create: web/test/generation-credit-quote.test.tsx

**Interfaces:**
- Consumes: POST /api/billing/quotes and HTTP 409 from Task 2.
- Produces:

~~~ts
export type TaskBillingQuoteConfirmation = {
    priceVersion: number;
    quoteFingerprint: string;
};

export type TaskBillingQuote = TaskBillingQuoteConfirmation & {
    amountMicrocredits: number;
    perTaskAmountMicrocredits: number;
    taskCount: number;
    billingMode: "fixed_request" | "per_second";
    pricingResolution: string;
    pricingInputVariant: string;
    quantity: number;
    enhancementAmountMicrocredits: number;
};

export type GenerationCreditQuoteState =
    | { status: "idle" | "loading"; quote: null; error: null }
    | { status: "ready"; quote: TaskBillingQuote; error: null }
    | { status: "error"; quote: null; error: string };
~~~

- [ ] **Step 1: Write failing API, state and badge tests**

Use happy-dom and an injected deferred quote function. Define a QuoteProbe component in the test file that renders data-status, data-amount and data-fingerprint from useGenerationCreditQuote, mount it with createRoot, and then run this ordering:

~~~tsx
test("a slow old quote cannot overwrite the latest parameters", async () => {
    const oneK = deferred<TaskBillingQuote>();
    const twoK = deferred<TaskBillingQuote>();
    const requestQuote: QuoteTaskBilling = (request) => request.input.config.size === "1024x1024" ? oneK.promise : twoK.promise;
    await act(async () => root.render(<QuoteProbe size="1024x1024" requestQuote={requestQuote} />));
    await advanceQuoteDebounce();
    await act(async () => root.render(<QuoteProbe size="2048x2048" requestQuote={requestQuote} />));
    await advanceQuoteDebounce();
    await act(async () => twoK.resolve(quote({ amountMicrocredits: 2_000_000, quoteFingerprint: "two-k" })));
    await act(async () => oneK.resolve(quote({ amountMicrocredits: 1_000_000, quoteFingerprint: "one-k" })));
    expect(host.querySelector("output")?.getAttribute("data-amount")).toBe("2000000");
    expect(host.querySelector("output")?.getAttribute("data-fingerprint")).toBe("two-k");
});
~~~

In the same file, define deferred, quote, advanceQuoteDebounce and QuoteProbe completely. Add four further tests: rejection after a prior success clears amount and renders the real error; disabled/local modes call the injected function zero times; the badge markup contains lucide-zap for loading/ready/error; createGenerationTask given an older confirmation throws BillingPriceChangedError and the injected task POST call count remains zero. Tests inject request functions through typed options; they do not patch global Axios and do not add a production fallback.

- [ ] **Step 2: Run focused tests and verify RED**

~~~powershell
cd web
bun test test/generation-credit-quote.test.tsx
~~~

Expected: module and type failures.

- [ ] **Step 3: Add quote DTOs and API functions**

Implement:

~~~ts
export function quoteTaskBilling(input: TaskBillingQuoteRequest, signal?: AbortSignal) {
    return request<TaskBillingQuote>(api.post("/billing/quotes", input, { signal }));
}

export class BillingPriceChangedError extends Error {
    constructor(readonly currentQuote: TaskBillingQuote) {
        super("预计积分已变化，请确认新报价后重试");
    }
}
~~~

Change createGenerationTask(input, expectedQuote?) so image/video requests first obtain a current single-task quote with batchCount 1. If expectedQuote differs in version or fingerprint, throw BillingPriceChangedError without posting /tasks. Otherwise POST /tasks with current quotePriceVersion and quoteFingerprint. Text and Agent tasks skip media pre-quote.

Add a pure taskBillingQuoteRequestFromTask(input, batchCount) mapper that accepts only canvas_image and canvas_video, reads the system channel/model and priced config fields from the existing task input, counts the actual referenceVideos array, and returns the precise TaskBillingQuoteRequest DTO. It returns null for text/Agent tasks and throws a structural error for malformed media input; it never inspects prompt semantics.

- [ ] **Step 4: Implement the race-safe hook**

useGenerationCreditQuote accepts mode, operation, config, batchCount, referenceVideoCount, enabled, and an optional injected request function for tests.

~~~ts
useEffect(() => {
    if (!enabled || (mode !== "image" && mode !== "video")) {
        setState(IDLE_QUOTE_STATE);
        return;
    }
    const controller = new AbortController();
    const requestNumber = ++requestNumberRef.current;
    setState({ status: "loading", quote: null, error: null });
    const timer = window.setTimeout(async () => {
        try {
            const quote = await requestQuote(buildRequest(), controller.signal);
            if (!controller.signal.aborted && requestNumber === requestNumberRef.current) {
                setState({ status: "ready", quote, error: null });
            }
        } catch (error) {
            if (!controller.signal.aborted && requestNumber === requestNumberRef.current) {
                setState({ status: "error", quote: null, error: errorMessage(error) });
            }
        }
    }, 250);
    return () => {
        window.clearTimeout(timer);
        controller.abort();
    };
}, [enabled, mode, operation, channelId, model, count, size, quality, videoSeconds, vquality, referenceVideoCount, superResolutionEnabled, superResolutionResolution, superResolutionVersion, superResolutionFPS]);
~~~

The request contains price-relevant config only. referenceVideoCount is explicit; backend converts it to the same standard/reference_video variant as real task references.

- [ ] **Step 5: Implement the shared badge**

~~~tsx
export function GenerationCreditBadge({ state }: { state: GenerationCreditQuoteState }) {
    const label = state.status === "loading" ? "计算中" : state.status === "ready" ? formatCredits(state.quote.amountMicrocredits) : state.status === "error" ? "报价失败" : "";
    if (!label) return null;
    return (
        <span className="generation-credit-badge inline-flex items-center gap-1" role="status" title={state.status === "error" ? state.error : "预计消耗积分"}>
            <CreditSymbol aria-hidden />
            <span>{label}</span>
        </span>
    );
}
~~~

- [ ] **Step 6: Run focused tests and verify GREEN**

~~~powershell
cd web
bun test test/generation-credit-quote.test.tsx
~~~

Expected: PASS including out-of-order resolution, abort and error clearing.

- [ ] **Step 7: Commit**

~~~powershell
git add web/src/services/api/task-center.ts web/src/components/canvas/use-generation-credit-quote.ts web/src/components/canvas/generation-credit-badge.tsx web/test/generation-credit-quote.test.tsx
git commit -m "feat(web): 增加生成积分后端报价"
~~~

---

### Task 4: Canvas Wiring and Lightning Presentation

**Files:**
- Modify: web/src/constant/credits.tsx
- Modify: web/src/components/canvas/canvas-config-node-panel.tsx:1-240
- Modify: web/src/components/canvas/canvas-node-prompt-panel.tsx:1-110,330-370,648-658
- Modify: web/src/pages/canvas/use-canvas-generation-executor.ts:35-210
- Modify: web/src/pages/canvas/canvas-generation-executor-types.ts
- Modify: web/src/pages/canvas/canvas-image-generation-executor.ts:15-155
- Modify: web/src/pages/canvas/canvas-media-generation-executors.ts:15-155
- Modify: web/src/lib/canvas/canvas-project-generation.ts:1-75
- Replace: web/test/credits.test.ts
- Modify: web/test/canvas-project-generation.test.ts

**Interfaces:**
- Consumes: GenerationCreditQuoteState, TaskBillingQuoteConfirmation and createGenerationTask(input, expectedQuote) from Task 3.
- Produces: user-clicked image/video paths pass the displayed single-task confirmation to every actual task; automated paths obtain a current single-task quote.

- [ ] **Step 1: Replace local-formula tests with failing presentation tests**

~~~tsx
test("CreditSymbol uses the product lightning icon", () => {
    const markup = renderToStaticMarkup(createElement(CreditSymbol));
    expect(markup).toContain("lucide-zap");
    expect(markup).not.toContain("lucide-coins");
});

test("formatCredits only converts microcredits for display", () => {
    expect(formatCredits(8_150_000)).toBe("8.15");
});
~~~

Extend canvas-project-generation.test.ts with an injected task API seam that asserts quotePriceVersion and quoteFingerprint reach user-visible task submission, while an automated path quotes before submitting.

- [ ] **Step 2: Run focused tests and verify RED**

~~~powershell
cd web
bun test test/credits.test.ts test/canvas-project-generation.test.ts test/generation-credit-quote.test.tsx
~~~

Expected: lightning and propagation assertions fail.

- [ ] **Step 3: Delete local pricing and switch the symbol**

~~~tsx
import { Zap } from "lucide-react";

export function CreditSymbol({ className, ...props }: ComponentProps<"span">) {
    return (
        <span {...props} className={"inline-flex items-center justify-center " + (className || "")}>
            <Zap className="size-[1em] fill-current" strokeWidth={2.2} />
        </span>
    );
}
~~~

Keep formatCredits. Delete ModelCreditCost, resolution normalization and requestCreditCost.

- [ ] **Step 4: Wire both panels to shared quote state**

For system image/video models, call useGenerationCreditQuote with actual output task count and reference video count. Replace local credits with GenerationCreditBadge.

~~~ts
const quoteReady = quoteState.status === "ready";
const paidMediaBlocked = isRemoteMedia && !quoteReady;
const confirmation = quoteReady
    ? {
          priceVersion: quoteState.quote.priceVersion,
          quoteFingerprint: quoteState.quote.quoteFingerprint,
      }
    : undefined;
~~~

Disable generation while paidMediaBlocked. Pass confirmation in CanvasNodeGenerationOptions. Local/text/audio behavior remains unchanged.

- [ ] **Step 5: Carry confirmation through the execution chain**

~~~ts
export type CanvasNodeGenerationOptions = {
    controller?: AbortController;
    waitForTaskCapacity?: boolean;
    billingQuote?: TaskBillingQuoteConfirmation;
};

export type CanvasGenerationExecution = CanvasGenerationExecutorDependencies & {
    billingQuote?: TaskBillingQuoteConfirmation;
};
~~~

The implementation adds this property to the existing CanvasGenerationExecution intersection without removing nodeId, sourceNode, prompt, effectivePrompt, generationConfig, generationContext, controller, editingTextNode or registerPendingNodeIds.

useCanvasGenerationExecutor copies options.billingQuote into the execution object. Image and video executors pass it to each runBackendCanvasGenerationTask. Audio/text paths do not fabricate media quotes.

- [ ] **Step 6: Submit every actual task with confirmation**

Extend runBackendCanvasGenerationTask with a billingQuote optional parameter and call:

~~~ts
const task = await createGenerationTask(
    {
        projectId,
        type: "canvas_" + mode,
        operation,
        prompt,
        model: config.model,
        input,
    },
    billingQuote,
);
~~~

Image/video executors already force count 1 for each actual task. Therefore Task 3's single-task pre-submit quote and backend billing input are identical; the panel aggregate remains informational and the sum of actual reservations equals its total when all outputs start.

- [ ] **Step 7: Run focused canvas tests and verify GREEN**

~~~powershell
cd web
bun test test/credits.test.ts test/generation-credit-quote.test.tsx test/canvas-project-generation.test.ts test/canvas-node-ui-standard.test.tsx test/canvas-image-generation-settings.test.ts test/video-output-batch.test.ts
~~~

Expected: PASS and rg finds no requestCreditCost or ModelCreditCost import.

- [ ] **Step 8: Commit**

~~~powershell
git add web/src/constant/credits.tsx web/src/components/canvas/canvas-config-node-panel.tsx web/src/components/canvas/canvas-node-prompt-panel.tsx web/src/pages/canvas/use-canvas-generation-executor.ts web/src/pages/canvas/canvas-generation-executor-types.ts web/src/pages/canvas/canvas-image-generation-executor.ts web/src/pages/canvas/canvas-media-generation-executors.ts web/src/lib/canvas/canvas-project-generation.ts web/test/credits.test.ts web/test/canvas-project-generation.test.ts
git commit -m "feat(canvas): 显示统一生成积分报价"
~~~

---

### Task 5: Final Review and Commercial Gate

**Files:**
- Verification only. Production changes are permitted only for a current-diff defect within the stated budget.

**Interfaces:**
- Consumes: Tasks 1–4.
- Produces: evidence that quote, display and reservation are one consistent path.

- [ ] **Step 1: Format and run static checks**

~~~powershell
gofmt -w backend/internal/service/billing_quote.go backend/internal/service/billing_quote_test.go backend/internal/service/finance.go backend/internal/service/service.go backend/internal/handler/finance.go backend/internal/handler/auth.go backend/internal/handler/billing_quote_test.go
cd web
bunx prettier --check src/services/api/task-center.ts src/components/canvas/use-generation-credit-quote.ts src/components/canvas/generation-credit-badge.tsx src/constant/credits.tsx src/components/canvas/canvas-config-node-panel.tsx src/components/canvas/canvas-node-prompt-panel.tsx src/pages/canvas/use-canvas-generation-executor.ts src/pages/canvas/canvas-generation-executor-types.ts src/pages/canvas/canvas-image-generation-executor.ts src/pages/canvas/canvas-media-generation-executors.ts src/lib/canvas/canvas-project-generation.ts test/credits.test.ts test/generation-credit-quote.test.tsx test/canvas-project-generation.test.ts
cd ..
git diff --check
~~~

Expected: all exit 0.

- [ ] **Step 2: Run final Backend gates once**

~~~powershell
cd backend
go test ./... -count=1
go test -race ./internal/service ./internal/handler -count=1
go vet ./...
go build ./...
~~~

Expected: PASS. No PostgreSQL/Docker gate is required because there is no schema change and quote is read-only; reservation transaction coverage remains in the full suite.

- [ ] **Step 3: Run final Web gates once**

~~~powershell
cd web
bun test
bun run build
~~~

Expected: all tests, TypeScript, Vite and bundle budget pass.

- [ ] **Step 4: Perform the mandatory fixed-point review**

Verify every statement:

1. Every displayed amount comes from POST /api/billing/quotes.
2. No frontend file contains price matching or multiplication.
3. Batch total equals the sum of actual task reservations.
4. Resolution, reference-video variant, seconds, model and enhancement change the fingerprint.
5. Slow or failed quotes cannot reuse an older amount.
6. Price changes return 409 and write no Task, BillingOrder or ledger facts.
7. Automatic media paths quote before submit.
8. Public responses contain no supplier cost, margin, credential or internal snapshot.
9. Existing layout is unchanged and CreditSymbol renders Zap.
10. Agent token billing remains unchanged.

If review finds a current-diff defect, make one concentrated fix wave and rerun affected focused tests, then Steps 1–3 once. If the directed re-review finds a new cross-module Critical/Important or the implementation exceeds budget, stop and report instead of starting another patch loop.

- [ ] **Step 5: Close the worktree**

If Step 4 made no changes, create no empty commit. If it made a bounded correction, stage only the files named by git diff --name-only and commit them as fix(billing): 收口生成报价一致性. Finish with git status --short and require an empty worktree before reporting completion.
