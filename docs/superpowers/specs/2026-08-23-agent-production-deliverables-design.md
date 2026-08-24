# Agent Production Deliverables Design

## 背景

HMaigc Agent 的 `production.plan` 当前要求每个镜头同时提供 `imagePrompt` 与 `videoPrompt`，仓储据此无条件创建 `storyboard_image` 和 `video_clip` 两个 Artifact。即使用户明确要求纯文生视频，计划仍会产生未生成的分镜 Artifact；`canvas.commit` 会把它投影为空图片节点，delivery evidence 随后因该已提交图片没有真实 Resource 而拒绝整轮交付。

真实测试已经证明供应商视频、Task、BillingOrder、Resource 和视频 Artifact 均成功，失败发生在计划交付物与真实证据不一致之后。修复必须统一计划、Artifact Ledger、画布投影与 delivery verifier 的事实来源，不能给纯视频增加 verifier 特判，也不能跳过或伪造未生成资产。

## 目标

- 让每个正式镜头显式声明本计划版本需要交付的媒体 Artifact。
- 支持纯分镜图、纯视频、分镜图加视频三种结构化计划。
- 纯视频计划只创建脚本和视频 Artifact，画布只投影脚本与视频节点。
- 保持现有付费审批、Task、BillingOrder、Resource、Artifact 状态机、revision/CAS 和 delivery verifier 单一路径。
- 历史计划保留审计事实，不通过推断或迁移篡改历史意图。

## 非目标

- 不改变 Agent 对话框视觉样式或真实流式协议。
- 不改变媒体供应商、模型能力、报价、扣费或审批流程。
- 不自动删除真实测试已经写入的空图片节点或历史计划。
- 不新增模型自动降级、隐式默认交付物或 Prompt 关键词判断。
- 验收不再次调用付费媒体供应商。

## 领域契约

### 镜头交付物

`ShotPlanDraft` 增加必填的 `deliverables`：

```json
{
  "shotKey": "shot-1",
  "order": 1,
  "durationMs": 5000,
  "scriptText": "抽象光带汇聚并消散",
  "deliverables": ["video_clip"],
  "videoPrompt": "原创抽象光影，镜头缓慢推进",
  "dependencies": []
}
```

允许值只有：

- `storyboard_image`
- `video_clip`

每个镜头必须声明一到两个不重复值。数组是交付意图的唯一真源，顺序不改变 Artifact 身份或业务含义。

### 内容校验

- `scriptText` 始终必填。
- 声明 `storyboard_image` 时 `imagePrompt` 必填；未声明时 `imagePrompt` 必须为空。
- 声明 `video_clip` 时 `videoPrompt` 必填；未声明时 `videoPrompt` 必须为空。
- 未知值、重复值、空数组以及 Prompt 与交付物不一致都显式返回 `production_plan_invalid`。
- `referenceKeys` 仅可用于包含 `storyboard_image` 的镜头；纯视频渲染不会静默忽略参考图。
- 标题、时长、顺序、依赖、引用作用域和总时长继续使用现有结构校验。

### Artifact Ledger

每个计划版本始终创建一个已成功的 `script` Artifact。`references` 继续逐项创建 `reference_image` Artifact。正式镜头只按 `deliverables` 创建对应 Artifact：

| deliverables | 镜头 Artifact |
| --- | --- |
| `["storyboard_image"]` | `storyboard_image` |
| `["video_clip"]` | `video_clip` |
| `["storyboard_image", "video_clip"]` | 两者 |

`ExpectedDeliveryJSON` 的数量从实际声明派生，不再使用镜头数同时填充图片与视频数量。Artifact ID 继续由计划版本、镜头和 Kind 确定，CAS 重放比较仍以完整不可变内容和 Artifact 身份为准。

### 画布投影

`canvas.commit` 仍要求 `artifactIds` 与计划版本的完整 Ledger 精确相等。投影按镜头实际 Artifact 动态构造：

- 纯图片：脚本行连接图片节点。
- 纯视频：脚本行直接连接视频节点。
- 图片加视频：脚本行连接图片节点，再连接视频节点。
- 参考图只连接实际存在的分镜图片节点。

Storyboard 行中不存在的 `imageNodeId` 或 `videoNodeId` 省略，不创建空媒体节点、不创建悬空连接。所有被提交的媒体 Artifact 仍必须由现有 Resource 事实决定节点内容和最终 delivery evidence。

### 交付验证

delivery verifier 不增加纯视频分支。它继续只消费成功工具调用、已提交 Artifact、ready Resource 和画布 revision。由于计划 Ledger 只包含声明的交付物，纯视频交付自然形成 `text + video + canvas_revision` 证据，不再要求图片证据。

流式 `final.message` 的失败语义保持现有规格：若最终严格决策或交付证据无效，消息 Item 标记失败并保留已收到正文，但不得认定为最终交付。

## 历史数据与硬切换

`deliverables` 存入现有 `shots_json`，无需数据库迁移。历史计划和 Artifact 保持原样用于审计。缺少 `deliverables` 的旧计划不得被新 `canvas.commit` 当成可执行计划，也不得根据 Prompt 或已有 Artifact 猜测交付意图；Agent 必须用当前 `planKey` 和 `baseVersion` 创建包含显式交付物的新版本后继续。

## 错误与可观测性

- 所有结构冲突沿现有 `production_plan_invalid` 或 `production_canvas_invalid` 返回具体 reason。
- 不吞掉缺失 Prompt、无交付物、旧契约计划、缺 Artifact、缺 Resource 或 revision 冲突。
- 计费、供应商请求和历史 Artifact 事实不因计划更新而删除或覆盖。

## 测试策略

1. RED：纯视频 Draft 在旧实现中因空 `imagePrompt` 被拒绝。
2. GREEN：纯视频 Draft 创建 `script + video_clip`，交付数量为一条视频、零分镜图。
3. 验证纯图片与图片加视频分别创建精确 Artifact 集合。
4. 验证空、重复、未知 deliverable，以及缺失/多余 Prompt 和纯视频引用参考图被拒绝。
5. 验证纯视频画布只有脚本和视频节点，连接为脚本到视频，Storyboard 行没有图片节点 ID。
6. 验证纯视频 commit 后 delivery evidence 只包含真实文本与视频证据，不要求图片 Resource。
7. 回归现有参考图、图生视频、文生视频、Artifact CAS、Canvas revision/CAS 和幂等测试。
8. 最终执行后端 focused tests、全量 Go tests/build、受影响 race、Web tests/typecheck/build 和 `git diff --check`；不进行新的付费供应商调用。

## 变更预算

- 生产文件：4–7 个；render 与 commit 必须复用同一持久计划校验时允许触及第 6 个文件。
- 文档：本规格、实施计划和根 README。
- 净新增生产代码：约 180–320 行。
- 若必须增加数据库迁移、前端状态机、新的付费链路或超过 7 个生产文件，视为范围变化，停止实现并重新设计。
