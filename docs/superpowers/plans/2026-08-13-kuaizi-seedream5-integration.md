# Seedream 5.0 图片模型接入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. 本计划涉及共享模型目录、任务结果、计费与画布状态，禁止使用 `superpowers:subagent-driven-development` 拆成并行补丁。

**Goal:** 通过筷子科技统一 Base URL 与统一 Key 接入 Seedream 5.0 Lite/Pro，并让画布参数、组图结果、计费规格与后端真实能力保持一致。

**Architecture:** 在现有 provider registry 中登记 Seedream 家族，以动态 capability 同时驱动后台发布、公共模型目录和画布参数；后端新增独立异步图片 adapter，但继续复用统一凭据、任务、Resource 与计费链路。Lite 组图使用一个上游任务并顺序持久化多个 Resource，Pro 保持单图；部分成功以失败任务携带真实部分结果和 uncertain 计费事实显式返回，不引入兼容层或隐藏回退。

**Tech Stack:** Go 1.24、GORM、PostgreSQL/SQLite、React 18、TypeScript strict、Bun test、Vite、Ant Design。

## Global Constraints

- 筷子科技所有模型共用一个 Base URL 和一个活动 Key；不得新增 Seedream 专用密钥或平行渠道。
- 上游 model 值只允许 `seedream5.0lite`、`seedream5.0pro`。
- Lite 无参考图时支持 1–15 张输出；单张参考图只允许 1 张输出；2–14 张参考图时支持组图且参考图与输出图总数不超过 15。
- Pro 仅支持单图；参考图 0–10 张，输出固定 1 张。
- 分辨率只发布 `2K`、`3K`；显式尺寸总像素必须位于 `3,686,400..10,404,496`。
- 前端不展示或持久化 `seed`，后端不发送该字段，由上游执行正常随机生成。
- 前端不展示水印开关，后端始终显式发送 `watermark=false`。
- `output_format` 只允许 `jpeg`、`png`；`optimize_prompt` 与 `thinking` 必须由显式前端参数控制。
- 轮询间隔不得短于 5 秒；上游没有取消接口时必须如实展示“停止本地等待，已产生的上游结果仍可能返回”。
- 所有远程结果必须在任务终态前转存为平台 Resource；前端结果不得依赖筷子临时 URL。
- 多图不得聚合为 Base64；必须逐张下载、逐张落 Resource、逐张释放内存。
- 部分落盘失败时，任务必须 failed、已成功 Resource 必须保留并显示，计费必须进入 uncertain 等待核对。
- 不接入交互编辑 `mode=interaction`，不新增数据库迁移，不保留旧适配器双轨，不新增 `any` 或 `as any`。
- 生产改动预算分两段：A 段不超过 20 个生产文件/约 1000 行，B 段不超过 8 个生产文件/约 600 行；合计不超过 27 个生产文件/约 1600 行，任一段超出 50% 即停止并重新拆分。

---

## File Map

### 模型能力与定价

- `backend/internal/service/provider_registry.go`：登记 Seedream family/model 与图片 capability。
- `backend/internal/service/admin.go`：把图片能力安全投影到公共系统渠道 DTO。
- `backend/internal/service/channel_models.go`：按 registry 的 2K/3K 规格创建动态价格档位并执行发布门禁。
- `backend/internal/service/finance.go`：识别显式 2K/3K 图片计费规格。
- `web/src/stores/use-config-store.ts`：承载公共图片能力与 Seedream 参数状态。
- `web/src/services/api/provider-accounts.ts`：严格解析后台 registry DTO。
- `web/src/pages/admin/model-pricing/pricing-specifications.ts`：从动态 capability 生成 2K/3K 定价规格。

### 上游 adapter 与任务事实

- Create `backend/internal/service/provider_kuaizi_seedream.go`：请求、轮询、闭集状态与严格响应解析。
- Create `backend/internal/service/provider_kuaizi_seedream_test.go`：TLS mock 验证两模型、边界与失败分类。
- `backend/internal/service/provider_kuaizi_compatible_worker.go`：按 family/model 单一路径分派 Seedream。
- `backend/internal/service/provider.go`：增加严格图片参数 DTO，禁止未知字段回退。
- `backend/internal/service/resource.go`：顺序导入临时结果 URL，并构造平台 Resource 结果。
- `backend/internal/service/service.go`：保留部分结果并把任务/计费原子收口为 failed/uncertain。
- `backend/internal/repository/task_billing.go`：失败终结时一并持久化非空 `ResultJSON`。

### 画布参数与原生批次

- `web/src/lib/image-model-capabilities.ts`：纯函数推导 2K/3K 尺寸、动态数量与参数校验。
- `web/src/components/canvas/canvas-image-generation-settings.tsx`：只渲染当前模型支持的参数。
- `web/src/components/canvas/canvas-image-settings-popover.tsx`：传入真实参考图数量。
- `web/src/components/canvas/canvas-node-prompt-panel.tsx`：将节点连接事实传给图片参数面板。
- `web/src/pages/canvas/canvas-image-generation-executor.ts`：统一 Seedream 单任务多输出与旧模型单输出执行。
- Create `web/src/pages/canvas/canvas-image-batch-execution.ts`：无 UI 的批次计划/结果分配纯协调器。
- `web/src/lib/ai/system-provider-config.ts`：只传 capability 允许的 Seedream 参数。
- `web/src/services/api/task-center.ts`：失败任务错误携带已持久化的部分结果事实。
- `web/src/lib/canvas/canvas-generation-task-sync.ts`：把 Resource 结果映射到已创建占位节点。
- `web/src/types/canvas.ts`：把图片参数写入节点任务快照。
- `web/src/lib/canvas/canvas-project-generation.ts`：创建任务时复制已验证的图片参数。
- `web/src/components/canvas/canvas-config-node-panel.tsx`：配置节点与图片节点使用同一参数契约。
- `web/src/pages/canvas/use-canvas-generation-retry.ts`：重试复用原任务快照，不读取当前全局默认值。

---

### Task 1: 动态模型能力与 2K/3K 定价门禁

**Files:**
- Modify: `backend/internal/service/provider_registry.go`
- Modify: `backend/internal/service/provider_registry_test.go`
- Modify: `backend/internal/service/admin.go`
- Modify: `backend/internal/service/channel_models.go`
- Modify: `backend/internal/service/finance.go`
- Modify: `backend/internal/service/model_access_test.go`
- Modify: `web/src/stores/use-config-store.ts`
- Modify: `web/src/services/api/provider-accounts.ts`
- Modify: `web/src/pages/admin/model-pricing/pricing-specifications.ts`
- Test: `web/test/kuaizi-provider-settings.test.tsx`
- Create: `web/test/model-pricing-specifications.test.ts`

**Interfaces:**
- Produces backend `ProviderModelSpec` fields:

```go
type ProviderImageBatchStrategy string

const (
	ProviderImageBatchSingleTask ProviderImageBatchStrategy = "single_task"
	ProviderImageBatchOneTaskPerOutput ProviderImageBatchStrategy = "one_task_per_output"
)

type ProviderModelSpec struct {
	ImageBatchStrategy        ProviderImageBatchStrategy `json:"imageBatchStrategy,omitempty"`
	ImageMaxReferenceCount    int                        `json:"imageMaxReferenceCount,omitempty"`
	ImageReferenceOutputLimit int                        `json:"imageReferenceOutputLimit,omitempty"`
	ImageGroupMinReferences   int                        `json:"imageGroupMinReferences,omitempty"`
	SupportsPromptOptimize    bool                       `json:"supportsPromptOptimize,omitempty"`
	OutputFormats             []string                   `json:"outputFormats,omitempty"`
}
```

- Produces frontend `ProviderModelCapabilities` with the same camelCase fields and no optional defaulting for managed models.

- [ ] **Step 1: Add failing registry, publication, and pricing tests**

```go
func TestProviderAdapterRegistryPublishesSeedreamCapabilities(t *testing.T) {
	registry, err := NewProviderRegistry(kuaiziProviderAdapterDescriptors())
	require.NoError(t, err)
	descriptor, ok := registry.Descriptor("kuaizi", "seedream")
	require.True(t, ok)
	require.Len(t, descriptor.Models, 2)
	lite := descriptor.Models[0]
	require.Equal(t, "image", lite.Capability)
	require.Equal(t, []string{"2K", "3K"}, lite.Resolutions)
	require.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, lite.OutputCounts)
	require.Equal(t, ProviderImageBatchSingleTask, lite.ImageBatchStrategy)
	require.Equal(t, 14, lite.ImageMaxReferenceCount)
	require.Equal(t, 15, lite.ImageReferenceOutputLimit)
	require.Equal(t, 2, lite.ImageGroupMinReferences)

	pro := descriptor.Models[1]
	require.Equal(t, []int{1}, pro.OutputCounts)
	require.Equal(t, 10, pro.ImageMaxReferenceCount)
}
```

```ts
test("Seedream 定价规格只来自动态 2K/3K capability", () => {
  expect(specificationsForModel(seedreamLiteChannel)).toEqual([
    { key: "2K", label: "2K", group: "base" },
    { key: "3K", label: "3K", group: "base" },
  ]);
});
```

- [ ] **Step 2: Run focused tests and confirm RED**

Run:

```powershell
cd backend
go test ./internal/service -run 'TestProviderAdapterRegistryPublishesSeedreamCapabilities|TestManagedImagePricing' -count=1
cd ..\web
bun test test/kuaizi-provider-settings.test.tsx test/model-pricing-specifications.test.ts
```

Expected: Go fails because Seedream and image capability fields do not exist; Web fails because managed image pricing still emits fixed 1K/2K/4K.

- [ ] **Step 3: Add Seedream registry facts and dynamic pricing**

Use exact registry values:

```go
ProviderAdapterDescriptor{
	ProviderKind: "kuaizi",
	Family:       "seedream",
	Models: []ProviderModelSpec{
		{
			ModelKey: "seedream5.0lite", DisplayName: "Seedream 5.0 Lite",
			UpstreamMode: "seedream5.0lite", Capability: "image",
			Resolutions: []string{"2K", "3K"},
			Ratios: []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "21:9", "9:21"},
			Qualities: []string{}, OutputCounts: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
			ImageBatchStrategy: ProviderImageBatchSingleTask,
			ImageMaxReferenceCount: 14,
			ImageReferenceOutputLimit: 15, ImageGroupMinReferences: 2,
			SupportsPromptOptimize: true, OutputFormats: []string{"jpeg", "png"},
		},
		{
			ModelKey: "seedream5.0pro", DisplayName: "Seedream 5.0 Pro",
			UpstreamMode: "seedream5.0pro", Capability: "image",
			Resolutions: []string{"2K", "3K"},
			Ratios: []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3", "21:9", "9:21"},
			Qualities: []string{}, OutputCounts: []int{1},
			ImageBatchStrategy: ProviderImageBatchSingleTask,
			ImageMaxReferenceCount: 10,
			SupportsPromptOptimize: true,
			OutputFormats: []string{"jpeg", "png"},
		},
	},
}
```

Managed image publication is ready only when every registry resolution has a positive price. Missing 2K or 3K remains unpublished with an explicit warning; warning does not prevent an administrator from saving prices.

- [ ] **Step 4: Re-run focused tests and confirm GREEN**

Run the Step 2 commands. Expected: all selected tests pass.

- [ ] **Step 5: Commit the capability milestone**

```powershell
git add backend/internal/service/provider_registry.go backend/internal/service/provider_registry_test.go backend/internal/service/admin.go backend/internal/service/channel_models.go backend/internal/service/finance.go backend/internal/service/model_access_test.go web/src/stores/use-config-store.ts web/src/services/api/provider-accounts.ts web/src/pages/admin/model-pricing/pricing-specifications.ts web/test/kuaizi-provider-settings.test.tsx web/test/model-pricing-specifications.test.ts
git diff --cached --check
git commit -m "feat(provider): 登记 Seedream 图片能力与定价规格"
```

---

### Task 2: Seedream 异步 adapter、顺序资源持久化与部分结果

**Files:**
- Create: `backend/internal/service/provider_kuaizi_seedream.go`
- Create: `backend/internal/service/provider_kuaizi_seedream_test.go`
- Modify: `backend/internal/service/provider_kuaizi_compatible_worker.go`
- Modify: `backend/internal/service/provider.go`
- Modify: `backend/internal/service/resource.go`
- Modify: `backend/internal/service/service.go`
- Modify: `backend/internal/repository/task_billing.go`
- Test: `backend/internal/service/provider_kuaizi_compatible_worker_test.go`
- Test: `backend/internal/service/task_billing_finalization_test.go`

**Interfaces:**
- Consumes `ProviderModelSpec` from Task 1.
- Produces:

```go
type kuaiziSeedreamInput struct {
	Prompt          string
	Model           string
	Size            string
	ReferenceURLs   []string
	OptimizePrompt  bool
	SequentialMode  string
	MaxImages       int
	OutputFormat    string
}

type storedImageResult struct {
	URL        string `json:"url"`
	StorageKey string `json:"storageKey"`
	MediaType  string `json:"mediaType"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
}

type seedreamPartialFailure struct {
	RequestedCount int    `json:"requestedCount"`
	CompletedCount int    `json:"completedCount"`
	Message        string `json:"message"`
}

type seedreamGenerationResult struct {
	Mode           string                   `json:"mode"`
	Images         []storedImageResult      `json:"images"`
	RequestedCount int                      `json:"requestedCount"`
	CompletedCount int                      `json:"completedCount"`
	PartialFailure *seedreamPartialFailure  `json:"partialFailure,omitempty"`
}

type partialGenerationError struct {
	Result seedreamGenerationResult
	Cause  error
}
```

Extend the existing request config instead of introducing a parallel payload path:

```go
type providerConfig struct {
	ImageResolution     string `json:"imageResolution"`
	ImageOptimizePrompt string `json:"imageOptimizePrompt"`
	ImageOutputFormat   string `json:"imageOutputFormat"`
}
```

- [ ] **Step 1: Write failing adapter contract tests**

Create local TLS tests that assert the exact payload and closed response states:

```go
func TestKuaiziSeedreamLiteCreateAndPollGroupImages(t *testing.T) {
	// TLS server asserts ApiKey, create path, model, size and absence of seed,
	// sequential_image_generation, max_images, output_format and image order.
	// First poll returns running; second returns succeeded with three image_urls.
	// Assert poll timestamps are at least five seconds apart using an injected clock.
}

func TestKuaiziSeedreamRejectsReferenceAndOutputOverflow(t *testing.T) {
	// 14 references + 2 outputs must fail before an HTTP request.
}

func TestKuaiziSeedreamProRejectsMultipleOutputs(t *testing.T) {
	// max_images=2 must return a structural validation error.
}
```

Add table cases for: unknown status, HTTP 401/403/429/5xx, HTTP 200 with non-zero business code, missing task_id, missing/empty image_urls, invalid URL scheme, and response text that echoes Key/prompt.

- [ ] **Step 2: Run adapter tests and confirm RED**

Run:

```powershell
cd backend
go test ./internal/service -run 'TestKuaiziSeedream' -count=1
```

Expected: build fails because the Seedream adapter and input types do not exist.

- [ ] **Step 3: Implement the smallest strict adapter**

Create and status endpoints:

```go
const (
	kuaiziSeedreamCreatePath = "/ai-open-platform-api/v1/seedream/image/task/create"
	kuaiziSeedreamStatusPath = "/ai-open-platform-api/v1/seedream/image/task/status"
	kuaiziSeedreamPollInterval = 5 * time.Second
)
```

Rules:

1. Validate every field before network I/O.
2. Use the existing frozen unified Kuaizi credential and strict outbound transport.
3. Accept only `running`, `succeeded`, `failed`; unknown states fail explicitly.
4. Redact Key, prompt, references and upstream echoed content before persisting errors.
5. Treat create response loss after request write as uncertain; never create a second upstream task automatically.
6. Poll by the original `task_id`; no implicit model fallback.

- [ ] **Step 4: Write failing persistence and partial-result tests**

```go
func TestSeedreamGroupStoresResourcesSequentiallyWithoutBase64Aggregation(t *testing.T) {
	// Three TLS image URLs are downloaded one at a time.
	// Assert max in-flight download count == 1, result contains three platform
	// storage keys, and ResultJSON contains no data:image or upstream host.
}

func TestSeedreamPartialStorageFailurePreservesSuccessfulResources(t *testing.T) {
	// First two downloads/store operations succeed, third fails.
	// Assert Task=failed, BillingOrder=uncertain, two Resources remain ready,
	// ResultJSON contains those two results and an explicit partialFailure fact.
}
```

- [ ] **Step 5: Run persistence tests and confirm RED**

Run:

```powershell
go test ./internal/service -run 'TestSeedreamGroupStores|TestSeedreamPartialStorage' -count=1
```

Expected: results are not persisted sequentially and failed tasks cannot currently keep ResultJSON.

- [ ] **Step 6: Wire the single execution path**

Dispatch only by the family returned from the registry lookup:

```go
family, _, ok := kuaiziProviderFamilyForModel(task.Model)
if !ok {
	return nil, fmt.Errorf("筷子科技模型未登记：%s", task.Model)
}
switch family {
case "gpt-image2":
	return s.runKuaiziGPTImageTask(ctx, task, input, runtime)
case "seedream":
	return s.runKuaiziSeedreamTask(ctx, task, input, runtime)
default:
	return nil, fmt.Errorf("unsupported kuaizi image family %q", runtime.ModelSpec.Family)
}
```

`runKuaiziSeedreamTask` downloads and stores one result at a time. It returns:

```json
{
  "type": "image",
  "images": [{"url":"/api/resources/...","storageKey":"...","mediaType":"image/png"}],
  "requestedCount": 3,
  "completedCount": 3
}
```

On partial failure it returns `partialGenerationError` with the same result plus:

```json
{"partialFailure":{"requestedCount":3,"completedCount":2,"message":"第 3 张图片持久化失败"}}
```

`processClaimedTask` copies this non-empty ResultJSON into the existing atomic failure finalizer; the repository update must persist `result_json` in the same transaction that sets failed/uncertain.

- [ ] **Step 7: Run backend focused and package tests**

Run:

```powershell
go test ./internal/service ./internal/repository -run 'Seedream|ProviderRegistry|ManagedImagePricing|TaskBilling' -count=1
```

Expected: all selected tests pass with no real upstream call.

- [ ] **Step 8: Commit the backend milestone**

```powershell
git add backend/internal/service/provider_kuaizi_seedream.go backend/internal/service/provider_kuaizi_seedream_test.go backend/internal/service/provider_kuaizi_compatible_worker.go backend/internal/service/provider.go backend/internal/service/resource.go backend/internal/service/service.go backend/internal/repository/task_billing.go backend/internal/service/provider_kuaizi_compatible_worker_test.go backend/internal/service/task_billing_finalization_test.go
git diff --cached --check
git commit -m "feat(media): 接入 Seedream 异步组图生成"
```

---

### Task 3: 画布动态参数、原生批次与部分结果回填

**Files:**
- Modify: `web/src/lib/image-model-capabilities.ts`
- Modify: `web/src/stores/use-config-store.ts`
- Modify: `web/src/components/canvas/canvas-image-generation-settings.tsx`
- Modify: `web/src/components/canvas/canvas-image-settings-popover.tsx`
- Modify: `web/src/components/canvas/canvas-node-prompt-panel.tsx`
- Modify: `web/src/pages/canvas/canvas-image-generation-executor.ts`
- Create: `web/src/pages/canvas/canvas-image-batch-execution.ts`
- Modify: `web/src/lib/ai/system-provider-config.ts`
- Modify: `web/src/services/api/task-center.ts`
- Modify: `web/src/lib/canvas/canvas-generation-task-sync.ts`
- Modify: `web/src/types/canvas.ts`
- Modify: `web/src/lib/canvas/canvas-project-generation.ts`
- Modify: `web/src/components/canvas/canvas-config-node-panel.tsx`
- Modify: `web/src/pages/canvas/use-canvas-generation-retry.ts`
- Modify: `web/test/canvas-image-generation-settings.test.ts`
- Create: `web/test/canvas-image-batch-execution.test.ts`
- Create: `web/test/canvas-generation-task-sync.test.ts`

**Interfaces:**
- Consumes Task 1 capability fields and Task 2 result contract.
- Produces pure functions:

```ts
export type ImageBatchPlan = {
  strategy: "single_task" | "one_task_per_output";
  requestedCount: number;
  taskCount: number;
};

export function availableImageOutputCounts(
  capabilities: ProviderModelCapabilities,
  referenceCount: number,
): readonly number[];

export function buildImageDimensions(
  resolution: "2K" | "3K",
  ratio: string,
): { width: number; height: number; size: string };

export function planImageBatch(
  capabilities: ProviderModelCapabilities,
  referenceCount: number,
  requestedCount: number,
): ImageBatchPlan;
```

- [ ] **Step 1: Write failing capability and UI tests**

```ts
test("Lite 参考图与输出图总数不超过 15", () => {
  expect(availableImageOutputCounts(seedreamLiteCapabilities, 14)).toEqual([1]);
  expect(() => planImageBatch(seedreamLiteCapabilities, 14, 2)).toThrow(
    "参考图与输出图总数不能超过 15",
  );
});

test("Lite 单张参考图不启用组图", () => {
  expect(availableImageOutputCounts(seedreamLiteCapabilities, 1)).toEqual([1]);
  expect(availableImageOutputCounts(seedreamLiteCapabilities, 0)).toHaveLength(15);
});

test("Pro 始终只允许单图", () => {
  expect(availableImageOutputCounts(seedreamProCapabilities, 10)).toEqual([1]);
});

test("Seedream 参数面板只登记模型支持的显式参数", () => {
  // Read the real component contract and assert 2K/3K, prompt optimization,
  // jpeg/png and current-reference-aware output count are wired.
  // Assert seed, 1K/4K and unsupported interaction controls are absent.
});
```

- [ ] **Step 2: Run focused Web tests and confirm RED**

Run:

```powershell
cd web
bun test test/canvas-image-generation-settings.test.ts test/canvas-image-batch-execution.test.ts test/canvas-generation-task-sync.test.ts
```

Expected: capability helpers and Seedream controls do not exist; executor still creates one backend task per output.

- [ ] **Step 3: Implement deterministic dimensions and strict validation**

Use target pixel areas `4,194,304` for 2K and `9,437,184` for 3K. For ratio `w:h`, calculate:

```ts
const width = roundToEven(Math.sqrt(targetPixels * (w / h)));
const height = roundToEven(targetPixels / width);
```

Then reject unless `width * height` is within `3_686_400..10_404_496` and ratio is in the capability list. Do not silently switch ratio, resolution, count, format or optimization mode when validation fails.

Render these controls only when declared by capability:

- 分辨率: 2K / 3K
- 比例: capability ratios
- 数量: Lite 使用单个紧凑步进控件，最小 1，最大值由 `availableImageOutputCounts` 决定；Pro 不显示
- 提示词优化: 关闭 / 开启；开启时后端发送 `optimize_prompt=true` 与 `options.thinking=auto`
- 格式: jpeg / png

- [ ] **Step 4: Implement native single-task batch coordination**

`planImageBatch` returns `taskCount=1` for Seedream and `taskCount=requestedCount` for models whose strategy is `one_task_per_output`.

For Seedream:

1. Create all placeholder nodes before the request.
2. Bind the same backend task ID to all placeholders.
3. Send one task with `count=requestedCount` and exact Seedream parameters.
4. Map returned `images[n]` to placeholder `n` using its platform URL/storageKey; do not upload it again.
5. If `GenerationTaskFailure` contains a partial result, fill the successful placeholders and mark only the missing placeholders failed.
6. Keep the overall operation failed and show the backend partial-failure message.

Use a typed error rather than parsing message text:

```ts
export class GenerationTaskFailure extends Error {
  constructor(
    message: string,
    readonly task: GenerationTask,
  ) {
    super(message);
    this.name = "GenerationTaskFailure";
  }
}
```

- [ ] **Step 5: Run focused Web tests and confirm GREEN**

Run the Step 2 command. Expected: all selected tests pass and assert exactly one backend create call for a 4-image Lite request.

- [ ] **Step 6: Run the complete Web gate**

```powershell
bun test
bun run build
```

Expected: all tests pass, TypeScript/Vite build passes, and bundle budgets pass.

- [ ] **Step 7: Commit the canvas milestone**

```powershell
git add web/src/lib/image-model-capabilities.ts web/src/stores/use-config-store.ts web/src/components/canvas/canvas-image-generation-settings.tsx web/src/components/canvas/canvas-image-settings-popover.tsx web/src/components/canvas/canvas-node-prompt-panel.tsx web/src/pages/canvas/canvas-image-generation-executor.ts web/src/pages/canvas/canvas-image-batch-execution.ts web/src/lib/ai/system-provider-config.ts web/src/services/api/task-center.ts web/src/lib/canvas/canvas-generation-task-sync.ts web/src/types/canvas.ts web/src/lib/canvas/canvas-project-generation.ts web/src/components/canvas/canvas-config-node-panel.tsx web/src/pages/canvas/use-canvas-generation-retry.ts web/test/canvas-image-generation-settings.test.ts web/test/canvas-image-batch-execution.test.ts web/test/canvas-generation-task-sync.test.ts
git diff --cached --check
git commit -m "feat(canvas): 按 Seedream 能力渲染图片参数"
```

---

### Task 4: 商业级总验收、真实预览与交付记录

**Files:**
- Modify: `CHANGELOG.md`
- Verify only: all files changed by Tasks 1–3

**Interfaces:**
- Consumes all previous milestones.
- Produces one independently deployable Seedream release candidate without publishing or pushing.

- [ ] **Step 1: Run backend complete gates once at the stable milestone**

```powershell
cd backend
go test ./... -count=1
go test -race ./internal/service ./internal/repository -count=1
go vet ./...
go build ./...
```

Expected: all commands exit 0. No real Kuaizi Key or production database is used.

- [ ] **Step 2: Run frontend complete gates once**

```powershell
cd ..\web
bun test
bun run build
```

Expected: all tests, TypeScript, Vite build and bundle budgets pass.

- [ ] **Step 3: Build the local images and preserve the current data bind**

Before rebuild, record the resolved data mount and file metadata. Rebuild only backend/web services using the existing local compose project; do not create another database volume.

```powershell
& .\scripts\local-compose.ps1 config
& .\scripts\local-compose.ps1 up -d --build --wait backend web
& .\scripts\local-compose.ps1 ps
```

Expected: backend and web are healthy; the resolved data bind is unchanged; existing data file path, size and modification time remain unchanged.

- [ ] **Step 4: Execute browser acceptance without a real paid generation**

Verify in the production build:

1. Admin Kuaizi page lists `seedream` under the same unified endpoint/key.
2. Unpriced Seedream models show a warning and are not user-visible.
3. After entering both 2K and 3K prices and publishing, Lite/Pro appear in the image selector.
4. Lite with 14 references only offers one output; Pro always offers one output.
5. Selecting Seedream shows only 2K/3K, supported ratios, optimize and jpeg/png; seed is absent.
6. Switching back to GPT Image 2 removes Seedream-only controls.
7. No control uses a hardcoded fallback model when dynamic catalog is empty.

Use local TLS mock fixtures for the create/poll/result path; do not consume the user's balance.

- [ ] **Step 5: Perform the mandatory review against requirement, plan and diff**

Review exactly these axes:

- Requirement: Lite/Pro facts, frontend params, unified credential, pricing warning semantics.
- Contract: registry → public DTO → UI → request → adapter → Resource → task result → billing.
- Security: no Key/prompt/reference/upstream URL in JSON, errors, logs or task result.
- Consistency: single upstream task for Lite group, no duplicate create, no Base64 aggregation.
- Failure: partial Resource preservation, failed task, uncertain billing, explicit user-visible reason.
- Deployment: no migration, no local data-path drift, no build artifacts or secrets staged.

If any current-diff defect is found, fix it once in a concentrated round, run only the affected focused gates, then repeat Steps 1–3 once. A new cross-module Important after that triggers architecture stop instead of another patch loop.

- [ ] **Step 6: Update the changelog and commit final verification metadata**

Add a Chinese `Pending` entry covering Seedream Lite/Pro, native group images, frontend dynamic parameters, Resource persistence and 2K/3K pricing.

```powershell
git add CHANGELOG.md
git diff --cached --check
git commit -m "docs(media): 记录 Seedream 图片模型接入"
```

- [ ] **Step 7: Confirm final Git scope**

```powershell
git status --short
git log -4 --oneline
git diff HEAD~4..HEAD --stat
```

Expected: only intended source/tests/changelog commits are present; the user's deleted canvas polish spec and unrelated `.superpowers/` files remain untouched and uncommitted. Do not push, tag, merge or publish a release without a new explicit user instruction.
