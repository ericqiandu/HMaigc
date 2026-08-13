# 筷子科技 Seedream 5.0 图片模型接入设计

## 目标

在现有“筷子科技统一账号”渠道中新增 Seedream 5.0 图片模型系列，使管理员继续只维护一套 Base URL 和 Key，并可分别发布、定价和启用 `seedream5.0lite` 与 `seedream5.0pro`。画布图片节点根据后台发布的能力契约动态展示参数，Lite 使用上游原生组图，Pro 保持单图。

## 业务边界

### 本次交付

- 新增 `seedream` 模型系列：
  - `seedream5.0lite`：0–14 张参考图；单图或原生组图；参考图数量与输出数量之和不得超过 15。
  - `seedream5.0pro`：0–10 张参考图；每次只生成 1 张。
- 支持文生图、单图参考和多图参考。
- 支持 2K、3K、常用比例预设、提示词优化、JPEG/PNG 输出。
- Lite 在无参考图时支持 1–15 张输出；单张参考图只允许 1 张输出；2–14 张参考图时允许组图且可选数量必须实时收窄。
- 生成结果及时下载并进入现有平台资产链路，不依赖上游 URL 长期有效。
- 沿用现有图片模型的按张计费语义；Lite 一次上游任务返回多张时，计费数量仍等于实际请求的输出张数。

### 明确暂缓

- 不实现 Seedream 5.0 Pro 的坐标、框选、箭头等交互编辑，因为当前画布没有对应的结构化编辑输入契约。
- 不实现联网搜索、流式输出和上游取消；接口文档明确当前不支持。
- 不接入 4.0、4.5 或其他未在本次文档模型枚举中的 Seedream 版本。
- 不增加旧字段兼容层、默认模型回退或第二套筷子渠道配置。

## 架构设计

### 1. 模型注册与统一凭据

在后端 provider adapter registry 中增加 `kuaizi/seedream` 描述，模型标识严格使用上游枚举：

| 模型 | 能力 | 参考图上限 | 输出上限 | 原生批次 |
| --- | --- | ---: | ---: | --- |
| `seedream5.0lite` | image | 14 | 15 | 是 |
| `seedream5.0pro` | image | 10 | 1 | 否 |

两个模型与现有 Seedance、GPT Image 2、Agent 模型一样，共用已激活的筷子账号 endpoint 与统一健康凭据。管理员在筷子科技页面单独发布 `seedream` 系列；发布只登记模型，不自动启用、不自动填价格。

公共能力 DTO 增加图片模型需要的结构字段，前端不得按模型名猜测：

- `imageBatchStrategy`: `single_task` 或 `one_task_per_output`；Seedream Lite 为 `single_task`。
- `supportsPromptOptimization`
- `outputFormats`
- 既有 `resolutions`、`ratios`、`outputCounts`、`maxImages` 继续作为动态真源。

未知或缺失的批次策略必须显式报错，不做隐式回退。

### 2. 参数契约

画布生成配置增加明确字段：

- `resolution`: `2K` / `3K`
- `optimizePrompt`: `true` / `false`
- `outputFormat`: `jpeg` / `png`
- `count`: 请求输出张数

尺寸由“比例 + resolution”生成明确的 `宽x高`，并在前后端共同验证总像素位于 `[3686400, 10404496]`。常用比例只是平台提供的确定性尺寸预设，不伪装成上游独立枚举。后端必须再次验证尺寸，不能信任浏览器。

Seedream Lite 的约束为：

```text
referenceImageCount <= 14
outputCount >= 1
outputCount > 1 且 referenceImageCount > 0 时，referenceImageCount >= 2
referenceImageCount + outputCount <= 15
```

当 `count == 1` 时发送 `sequential_image_generation=disabled`；当 `count > 1` 时发送 `sequential_image_generation=auto` 与 `max_images=count`。Pro 始终拒绝 `count != 1`，且不发送组图字段。

参考图只接受当前用户有权访问并已经过平台素材链路解析的图片资源；发往上游时必须是可访问的 HTTP(S) URL。禁止由浏览器直接传任意外部 URL 绕过资源权限。

### 3. 上游适配器与状态

新增独立 Seedream 适配器，复用现有筷子请求安全边界和冻结运行时：

1. `POST /ai-open-platform-api/v1/seedream/image/task/create`
2. 仅当 HTTP 200、业务 `code == 0` 且 `task_id` 合法时视为创建成功。
3. `POST /ai-open-platform-api/v1/seedream/image/task/status`，轮询间隔不低于 5 秒。
4. 只接受 `running`、`succeeded`、`failed` 三种状态；未知状态显式失败并进入现有待核对链路。
5. `succeeded` 必须返回非空 `image_urls`，数量必须与请求输出数量一致；少图、多图或非法 URL 都不得伪装成功。
6. 下载每个结果并验证 HTTPS、重定向、特殊地址、内容类型、响应体上限和敏感信息反射，然后交给现有资产保存流程。

Seedream 组图不得先把全部图片编码为 Base64 聚合在内存中。后端按 URL 顺序逐张下载、逐张写入现有 `Resource` 存储，任务结果只保存 `/api/resources/{id}/file`、`resource:{id}` 与尺寸、字节数、MIME 等资源事实。该路径不新增资源表或兼容字段。

创建请求已经写出但响应不确定时，复用现有 `KuaiziCompatibleCreateError` 与 provider task fact，禁止再次创建导致重复扣费。Key、prompt、上游错误正文及签名 URL 不进入任务错误、API 日志或前端错误消息。

接口不支持上游取消。用户取消已提交任务时，界面必须说明“已停止本地等待，上游任务可能仍在执行，费用状态待核对”，不能宣称上游已终止或自动退款。

### 4. 原生组图数据流

当前图片执行器对 `count > 1` 会发起多个后端任务。Seedream Lite 必须改为通用能力驱动：

```text
选择 Seedream Lite + N 张输出
  -> 创建 1 个本地生成任务
  -> 预留 N 张的积分
  -> 创建 1 个 Seedream 上游任务（max_images=N）
  -> 获取 N 个 image_urls
  -> 创建/回填 N 个画布图片节点
  -> 所有资产成功落库后结算一次订单
```

非原生批次模型仍按其后台 `imageBatchStrategy` 执行，前端不按 `seedream` 字符串分支。一个批次部分下载失败时，任务以 `failed + partial result` 收口：`ResultJSON` 保存已落库图片及结构化 `partialFailure`，订单标记为待核对。前端识别该事实后回填已成功节点并将其余节点置为失败；禁止删除成功资产、重复创建上游任务或把整个批次伪装成全部失败。

### 5. 计费与发布

- 两个模型默认 `fixed_request`，按图片数量计费。
- 支持按 2K / 3K 配置图片分辨率阶梯价格。
- `BillingOrder.quantity` 等于本次请求输出张数，而不是上游任务数。
- 未定价时后台允许发布但显示警告；前台模型目录保持不可用，不能绕过商业门禁。
- 成功数量与请求不一致、创建不确定、结果下载不完整时不得直接结算，进入现有人工核对状态并保留所有事实。

## 前端交互

图片节点只展示所选模型支持的参数：

- Lite：清晰度、比例、生成数量、提示词优化、输出格式。
- Pro：清晰度、比例、提示词优化、输出格式；不显示生成数量。

参考图数量变化后，Lite 的输出数量选项即时收窄。例如已有 10 张参考图时，只显示 1–5 张；若当前值变得非法，界面要求用户重新选择，不静默改成默认值。Pro 超过 10 张参考图时在提交前明确报错。

前端不展示、不保存种子，后端也不发送 `seed`，由上游执行正常随机生成。提示词优化默认关闭；输出格式默认 JPEG。默认值必须在前后端契约中一致，并在任务快照中可追溯。

前端也不展示水印开关；后端始终显式发送 `watermark=false`，避免供应商默认值变化影响用户资产。

## 安全与可观测性

- 只从服务端冻结凭据解密 Key，浏览器永远不接触明文 Key。
- 创建与轮询记录结构化 `providerRequestId`、上游 `task_id`、安全 trace ID、模型、请求类型、状态码和耗时。
- 日志只记录参数摘要与数量，不记录 prompt 原文、Key、参考图签名参数或上游错误正文。
- 下载结果沿用严格 HTTPS、逐跳 DNS/IP 校验和大小限制。
- 所有返回数组、状态和数值字段采用严格解析；缺字段、错类型和未知枚举原地失败。

## 测试与验收

### 后端 focused tests

- registry 能精确发布 Lite/Pro 能力且不影响现有四个系列。
- Lite/Pro 参数边界、参考图上限、参考图与输出总量、尺寸、格式和 prompt 优化映射。
- 一次 Lite 组图只创建一次上游任务，轮询返回 N 张并完整下载。
- HTTP/业务错误、未知状态、缺 task ID、创建不确定、数量不一致、非法 URL、非图片内容、超限响应和敏感信息反射。
- 冻结 endpoint/credential 仍被使用，Key 不进入任务输入和日志。
- Billing quantity 与输出数一致，2K/3K 阶梯价格正确，未定价模型不可调用。

### 前端 focused tests

- 两个模型的参数均来自动态能力 DTO。
- Lite 原生批次只发一个后端任务并创建 N 个结果节点。
- Pro 不显示批次选项并拒绝多输出。
- 参考图变化正确收窄 Lite 输出数量，非法已选值显式阻止提交。
- 部分资产成功时保留成功节点并展示待核对事实。

### 最终门禁

- 后端定向测试、全量 `go test ./...`、`go vet ./...`、`go build ./...`。
- Web 定向测试、全量测试、TypeScript/Vite 生产构建。
- 使用本地 TLS mock 完成创建、轮询、组图和失败流程；不使用真实 Key、不产生真实上游费用。
- 复用 `scripts/local-compose.ps1` 显式绑定主项目 `.local/data` 做本地构建启动，只读核对管理员发布与画布动态参数；不新建或清理用户数据库。

## 变更预算

- 生产职责：模型能力与发布、Seedream 异步适配、图片节点原生批次与动态参数，共 3 项。
- 分两段执行但只在全部通过后发布：A 段为模型能力、定价、单图 adapter 与前端参数，预算不超过 20 个生产文件/约 1000 行；B 段为 Lite 原生组图和部分结果，预算不超过 8 个生产文件/约 600 行。
- 两段合计不超过 27 个生产文件、约 1600 行净新增生产代码；文件数来自前后端共享 DTO、任务快照、重试与部分结果契约的必要同步，禁止用模型名硬编码压缩表面文件数。
- 不新增数据库表或字段，不引入新依赖，不修改部署结构。
- 若任一段超过自己的预算 50%、出现超过 500 行的新生产文件，或需要修改 provider task 状态机/数据库结构，则立即停止并重新评估，不继续堆补丁。
