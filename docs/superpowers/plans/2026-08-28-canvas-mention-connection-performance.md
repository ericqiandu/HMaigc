# 画布引用、连线与媒体性能实施计划

> 执行方式：在 `codex/hotfix-mention-cursor` 独立工作树中使用 `superpowers:executing-plans` 与 `superpowers:test-driven-development`，每个里程碑严格 RED → GREEN → review → focused commit；不 push、不部署。

## 目标与边界

业务目标是让图片/视频节点提示词中的 `@` 成为真实资源引用入口，同时修正视频输出连线契约，并消除画布拖动、媒体首屏展示与上传持久化中的主要阻塞。所有用户动作必须显式成功或失败，不允许静默降级到本地缓存。

本轮不改 Agent 多代理架构，不引入浏览器直传 OSS，不迁移数据库。用户媒体继续由对象存储承载，画布 metadata 仅增加可选来源字段。

变更预算：14–20 个生产文件，约 700–1200 行净新增；测试与文档不计入生产预算。若跨越新的事务、计费或部署边界，先停止并重新审视范围。

## 里程碑一：统一引用与连线契约

### 契约

- `@` 菜单候选来自当前画布可用资源和 Skills；已连接资源优先，未连接资源可见。
- 选择未连接资源时，必须先通过统一连接校验并建立连接，成功后才在真实光标处插入稳定 token；失败时提示词不变。
- 视频节点作为起点时不得连接图片或音频节点；图片/音频作为输入连接视频节点仍合法。
- 拖拽连线、`@` 引用、Agent `connect_nodes` 与后端 mutation 使用同一方向语义。前端给出可展示的结构化失败原因，后端作为最终权限边界校验补丁应用后的完整连接图。
- 最终连接图只要仍含非法边，本轮 mutation 就必须拒绝；用户需删除或改正非法边后再写入，不保留历史兼容双轨。
- 从视频抽取尾帧改用节点 provenance metadata，不再制造 Video → Image 普通边。

### RED

1. 扩展 `web/test/canvas-resource-mention-textarea.test.tsx`：未连接候选可检索；连接成功后在光标处插入；连接失败不改文本；鼠标选择和输入法组合期间不误提交；预览失败显示类型图标。
2. 扩展 `web/test/canvas-connection-contract.test.ts`：验证结构化失败原因、正反方向、frame/config 规则。
3. 扩展 `web/test/canvas-agent-ops.test.ts`：Agent 非法连线显式失败，合法连线成功。
4. 扩展 `backend/internal/service/canvas_collaboration_test.go`：新建/更新 Video → Image、Video → Audio 拒绝；Image/Audio → Video 接受；含历史非法边的最终图在无关节点补丁下仍必须失败。
5. 为尾帧来源增加 focused test，先证明现有实现仍创建非法边。

预期 RED 命令：

```powershell
cd web
bun test test/canvas-resource-mention-textarea.test.tsx test/canvas-connection-contract.test.ts test/canvas-agent-ops.test.ts
cd ..\backend
go test ./internal/service -run 'Test(CanvasMutation|ProductionCanvasCommit)' -count=1
```

### GREEN

- 在 `web/src/lib/canvas/canvas-project-domain.ts` 引入纯函数连接验证结果，并让 `normalizeConnection` 复用。
- 在 `web/src/lib/canvas/canvas-agent-ops.ts` 复用相同校验；非法 Agent 操作抛出明确错误，禁止静默跳过。
- 在 `web/src/lib/canvas/canvas-resource-references.ts` 提供去重、已连接优先的候选构造。
- 在 `web/src/components/canvas/canvas-node-prompt-panel.tsx` 把全画布候选交给编辑器，并提供“先连接、后插入”回调。
- 在 `web/src/components/canvas/canvas-resource-mention-textarea.tsx` 增加选择前校验回调、鼠标/键盘/IME 与预览失败状态；保持 suffix 与 selection。
- 在 `backend/internal/service/canvas_collaboration_document.go` 对补丁应用后的全部 connections 基于合并后节点做类型契约校验。
- 在 `web/src/types/canvas.ts` 与 `web/src/pages/canvas/use-canvas-media-tools.ts` 记录尾帧派生来源，删除内部非法连线。

### 验收与提交

- 重跑上述 focused tests、Web typecheck、相关 Go tests、`git diff --check`。
- 审查前后端方向语义、历史数据处理、错误可观测性。
- focused commit：`fix(canvas): 画布资源引用 - 统一引用与连线契约`

## 里程碑二：画布拖动热路径

### 契约

- connection pointermove 不得每帧扫描全部 nodes/edges，也不得重复构造全量数组。
- node drag 仅更新被拖节点 DOM transform 和其 incident SVG path；真实节点 position 只在 drop 时提交。
- selection、自动保存、结构校验不得进入 pointermove 热路径。

### RED

1. 为连接命中索引新增纯函数测试：快照建索引、邻域查询、隐藏节点/自身/非法目标过滤。
2. 为 incident connection path 更新新增测试：只返回与拖动节点相邻的连接，非相邻连接引用保持稳定。
3. 增加渲染模型测试，证明 drag preview 不再重建全部 display connections。

### GREEN

- 新增 `web/src/lib/canvas/canvas-connection-hit-index.ts`：在 connect start 构建空间网格快照，pointermove 只查相邻桶。
- 调整 `web/src/pages/canvas/use-canvas-connection-controller.ts` 使用快照查询与统一连接 validator。
- 调整 `web/src/pages/canvas/use-canvas-selection-controller.ts` 移除 50ms 全局 drag preview 状态同步，RAF 中仅处理局部 DOM/路径。
- 调整 `web/src/components/canvas/canvas-connections.tsx` 暴露可稳定定位的 connection path 元素。
- 调整 `web/src/pages/canvas/use-canvas-render-model.ts`，连接派生不再依赖高频 drag preview。
- 将纯几何与 incident 选择放入 `web/src/lib/canvas/canvas-drag-performance.ts` 或单一职责新模块。

### 验收与提交

- focused tests + Web typecheck/build；浏览器验证单节点、多选、框内节点、连线吸附与取消。
- 对性能热路径做代码级 O(1)/O(k) 审查，确认没有网络/序列化/全量 map/filter。
- focused commit：`perf(canvas): 画布拖动 - 收敛连线命中与路径更新`

## 里程碑三：媒体直出与目标持久化

### 契约

- 图片、视频、音频优先使用受控 `/api/resources/:id/file?direct=1` URL 直接渲染/流式播放，不因 IndexedDB 全量 blob 下载阻塞首帧。
- blob 缓存只在下载、离线或明确复用场景按需执行；禁止图片首次渲染等待整个文件。
- 已登录上传失败必须显式失败，禁止静默落到本地 IndexedDB；访客本地模式必须是显式分支。
- 上传完成只持久化本次 asset 与项目关联，不做两次全局用户数据扫描；分类更新与查询失效不阻塞必要成功链路。
- 保留后端代理上传、对象存储、权限与审计边界。

### RED

1. 扩展 resource URL/cache 测试：远程资源立即返回直达 URL，不先触发 metadata GET/blob download。
2. 扩展 image/file storage 测试：登录态远程上传失败抛错；访客显式本地模式可保存。
3. 扩展 project asset sync 测试：只提交目标 asset；项目 link 顺序正确；失败不伪成功；不触发全量 sync 两次。
4. 扩展上传 hook 测试：必要持久化成功后即可完成，query invalidation 后台执行且错误可记录。

### GREEN

- 重构 `web/src/services/resource-blob-cache.ts` 与 `web/src/components/canvas/canvas-node.tsx`，分离 direct display URL 与 on-demand blob cache。
- 保持 `web/src/components/canvas/canvas-media-node-content.tsx` 使用流式 URL 与合适 preload。
- 重构 `web/src/services/image-storage.ts`、`web/src/services/file-storage.ts` 的登录态/访客模式与错误契约。
- 在 `web/src/services/user-data-sync.ts` 增加单资产目标同步原语。
- 在 `web/src/services/project-asset-sync.ts` 使用目标同步并去除重复全量同步。
- 调整 `web/src/pages/canvas/use-canvas-upload.ts`，必要写入与非阻塞失效分层。

### 验收与提交

- focused tests + Web tests/typecheck/build；浏览器验证上传成功/失败、首图展示、视频 seek/range、刷新后资产仍在。
- 检查权限、幂等、失败态、缓存生命周期和对象 URL 释放。
- focused commit：`perf(media): 画布媒体 - 直出资源并收敛上传持久化`

## 最终审查与门禁

1. 对照原始需求、本文、三次 diff，核验引用、连接、Agent、服务端、provenance、性能与媒体契约。
2. 核验数据库：无 schema migration；metadata 只记录派生来源事实，读取路径单一且不保留旧非法边双轨。
3. 核验计费与权限：本轮不改计费；媒体资源仍经过既有鉴权入口；失败不回退。
4. 运行 Web 全量测试、typecheck/build、Go 全量测试/build、受影响 race、`git diff --check`。
5. 有条件使用本地浏览器完成真实回归：`@` 搜索/选择、非法连线、拖动、上传、刷新与媒体播放。
6. 一次独立 review → 一次集中修复 → 一次定向复审；同步 `CHANGELOG.md` 与 `docs/content/docs/pending-test.mdx`。
