# HMaigc

HMaigc 是面向 AI 影视与短剧生产的商业化创作平台，覆盖项目、章节、角色资产、自由画布、图片/视频/音频生成、团队协作、会员计费、积分运营和管理后台。

## 代码结构

- `web/`：Vite、React、TypeScript 前端。
- `backend/`：Go、Gin、GORM 后端。
- `docker-compose.yml`：使用 SQLite 的本地一体化环境。
- `docker-compose.production.yml`：使用 PostgreSQL 和 Redis 的生产环境。

仓库只保留上述两条运行路径，不再维护旧镜像部署、重复 Compose 或上游一键安装脚本。
画布助手已硬切到单一服务端 Agent Runtime，并通过后端系统模型渠道完成鉴权、计费和请求审计；不再提供本机 Agent、Codex 插件连接或浏览器内模型循环。服务端负责冻结模型、决策循环、高层工具协调、通用交付验收和可恢复检查点；Web 只提交用户目标与真实选区事实、展示持久化事件并处理审批。

### Web 交付与页面切换

- Web HTML 随不可变 Web 镜像同源交付；哈希 JS/CSS、字体和程序图片以版本目录发布到 `https://static.hm.kunagent.com/hmaigc/web/releases/<version>/`。Actions 只构建一次 `dist`，先上传并验证完整 CDN 清单，再把同一份 `dist` 封装进 Web 镜像；HTML 中的程序资源 URL 必须精确绑定当前版本，CSP 只放行该固定静态域名，不提供同源或其他域名的静默回退。
- 用户上传与模型生成的图片、视频和音频继续保存在后端配置的对象存储中。程序包与用户媒体是两条独立生命周期：程序发布不会迁移、覆盖或删除用户媒体。
- 匿名首访只恢复鉴权外壳；画布、资产和模型配置工作区仅在登录、已登录刷新或退出边界按需加载。工作台路由只在鼠标按下、悬停或键盘聚焦形成真实导航意图时预取模块与对应数据，不再在首页加载后后台扫预取，避免与首屏程序资源和接口争抢带宽；预取失败会显式记录且不阻断当前页面。
- React Query 的共享新鲜度窗口负责页面复用；项目工作台不再在每次挂载时无条件重复请求。全局入口不预载首页专属大图，避免访问画布、后台或项目时争抢首屏带宽。

### 画布交互与媒体链路

- 图片与视频节点的 `@` 引用候选来自当前画布真实资源与公开 Skills；未连接资源必须先通过统一连线契约并建立连接，成功后才在当前光标位置插入稳定引用。拖拽连线、Agent 画布操作和服务端画布 mutation 使用同一方向语义，视频输出不能连接图片或音频输入。
- 画布拖动预览只修改被拖节点的 DOM transform 与相邻连接路径；连接目标命中使用连接开始时建立的空间索引，pointermove 热路径不扫描完整节点或连接集合，也不触发保存和网络请求。
- 用户媒体仍保存在对象存储中。画布使用受控的 `/api/resources/:id/file?direct=1` 地址直接渲染图片并流式播放视频、音频，完整 Blob 只在下载或明确复用时按需读取。登录用户上传必须成功写入后端资源，不会静默落入浏览器本地存储；访客本地模式是独立显式分支。
- 上传节点进入项目资产时只定向写入当前素材，再建立项目关联并保存关联事实；项目查询刷新不阻塞必要持久化链路，失败会留下可检索告警。

### 首页到 Agent 的创作链路

- 首页创作框先上传参考图片并创建真实画布项目，把提示词、账号级资源 ID、系统动态模型选择、平台第一方公开 Skill 目录与显式执行模式写入项目内的 `pendingAgentLaunch`；提示词和临时 Blob URL 均不进入 URL 或持久事实。
- 打开新画布后，Agent 面板用该请求创建或复用服务端 thread，并以持久化 `clientRequestId` 启动 run；只有取得运行事实后才消费启动请求。启动响应丢失时刷新或重试仍复用同一请求 ID，不重复创建运行。
- Agent 推理模型、语义规划与交付判断由服务端 Runtime 统一决定。首页与画布共用同一份草稿契约，Web 只提交用户目标、动态模型选择、已授权 Skill 版本、账号级 Resource 身份和显式执行模式；服务端重新核验并冻结这些事实。新 Run 固定使用 Runtime v5 / Policy v5 / Tool schema v6，工具调用预算固定为 24，并同时受 24 步决策上限约束；服务端还会冻结从首次启动时间计算的 30 分钟绝对截止时间。浏览器不能修改这些限制，进程重启也不会重置预算或截止时间。
- Tool schema v6 只接受 `canvas.read`、`canvas.apply_ops`、`assets.read`、`assets.publish`、`media.generate`、`skills.load` 六个原子能力及其严格参数。旧 `skill.load`、Specialist、Production Graph、`vision.analyze`、`media.assemble`、`canvas.project` 等决策不会映射或降级，而是以 `model_decision_invalid` 显式终结。六个能力均已接入当前权威数据源：读取能力只返回作用域内事实，`canvas.apply_ops` 通过画布 revision/CAS 真源原子提交，`media.generate` 复用现有 Task/BillingOrder/Resource 商业链路，`assets.publish` 在单一事务中创建 Asset、confirmed AssetVersion、ProjectAssetLink 与 AssetRepresentation。每次写入或付费执行都会重新核验租户、项目、画布、用户权限与冻结审批提案；输出只保留当前能力契约允许的已持久化事实，禁止回退旧执行图。
- 模型每轮接收精确身份与作用域、动态模型事实、按需加载的 Skill 描述，以及六原子能力的参数和结果 schema。上下文上限为 512 KiB；未加载 Skill 的 instructions 不进入上下文，`docs/`、`assets/`、`ai-metadata/`、固定工作流和固定 Specialist 顺序均不参与运行时知识装配。

### 单一 Agent Runtime

#### 当前云能力状态机（Runtime v5）

当前唯一可创建的新 Run 契约是 Runtime v5 / Policy v5 / Tool schema v6 / UI protocol v5。模型在单一循环中依据真实项目上下文、动态模型目录、工具结果和按需加载的 Skills 自主决策；后端只注入权限、协议、计费、幂等与交付验收硬约束，不维护固定意图路由、Specialist 顺序或 Production Graph。

Tool schema v6 只提供六个原子能力：

- `canvas.read`、`assets.read`、`skills.load`：只读并立即执行。
- `canvas.apply_ops`、`assets.publish`：写入能力，执行前必须获得对精确不可变提案的批准。
- `media.generate`：复用现有 Task、BillingOrder、积分和 Resource 商业链路的付费能力，批准前不创建任务或预留积分。

工具身份固定为 `runId + toolCallId + actionVersion`；写入与付费提案额外冻结小写 SHA-256 `proposalHash`。已完成工具只重放持久结果，拒绝、过期、哈希不匹配、模型/价格/输入变化和结算不确定都作为显式事实返回，不会自动换模型、降低交付目标或伪造成功。

写入批准与付费批准相互独立，批准只对当前不可变提案在有效期内生效；一次批准不能授权后续新价格、新输入或另一类副作用。用户界面只展示简洁且可操作的错误说明，完整运维证据则通过 `runId`、`toolCallId`、`actionVersion`、`taskId`、`billingOrderId` 与供应商请求身份关联，既能定位真实失败，也不会向用户暴露供应商原始响应、内部堆栈或密钥。

首个模型决策必须声明 `expectedDelivery`，Runtime 将其冻结为整条 Run 的交付合同。每轮基于真实 ToolCall、Task、BillingOrder、ready Resource、资产发布和画布 revision 构造 `deliveryEvidence`，再生成 `deliveryVerification`；只有全部 completion criteria 已满足时才允许 final。只有文本、计划、Task 已提交或节点占位均不构成交付完成。

#### 当前 Web 与传输投影

Web 只提交用户目标、真实画布/选区事实和显式配置，展示服务端持久化 Turn/Item、结构化追问、审批提案与结果。SSE 使用 UI protocol v5 和连续持久 sequence 断线补发；旧 `stage_review_resolution`、`artifact_review`、`media_assembly` 等事件不会映射成当前交互卡。

刷新、断线重连或服务进程重启后，客户端都从持久化 Run、审批提案、连续事件序列和权威工具结果恢复；内存状态、浏览器计时器和前端乐观文案均不构成完成事实。同一批准或恢复请求重复到达时只复用原身份与结果，不重复修改画布、创建媒体任务或扣减积分。

`canvas.apply_ops` 成功后由服务端权威 revision/CAS 和协作通道传播画布变化，浏览器不自行执行 Agent 写入。`media.generate` 终态由 Outbox 唤醒同一 Run，成功必须能重读 succeeded Task、settled BillingOrder 与归属正确的 ready Resource；失败、取消和账务不确定保持可审计状态。已经由供应商成功产出的 Resource 不因后续步骤失败被删除、覆盖或回滚。

#### Skills 与模型事实

Skill 目录、版本和 SHA-256 在 Run 创建时冻结，`skills.load` 只能按需加载本轮已授权且仍发布的精确版本。`docs/`、`assets/`、`ai-metadata/`、固定工作流和固定子代理顺序不进入运行时知识装配。所有可选模型来自系统模型目录中当前用户真实可调用、已定价且凭据健康的动态事实；缺失能力或参数时显式失败，不使用默认模型或隐式回退。

#### 历史审计边界

Runtime v4/v5 的 Production Graph、Stage、Specialist、Artifact Ledger、视觉分析与媒体装配模型、仓储和数据库表只为读取既有终态审计事实保留。当前 HTTP 路由、依赖注入、协调器、Task Outbox、UI protocol 和模型工具 schema 均不再执行这些旧能力；非终态旧 Run 只能通过既有原子退役审计收口，历史记录不会删除、迁移成新决策或恢复执行。

### Agent 模型计费链路

- 网站 Agent 的每次模型请求分别创建计费订单；工具调用本身不计费，图片、视频等媒体生成继续使用各自独立订单。
- Agent 模型任务创建前若确认账号余额不足或团队月额度耗尽，运行会以 `insufficient_credits` 或 `team_credit_limit_reached` 明确终结，不创建下一任务或账单，也不会由后台驱动器无限重试；并发与暂时性额度错误仍保持原有显式错误语义。
- 托管的筷子 DeepSeek 模型使用 `token_usage`：系统代理和服务端 Agent Runtime 共用同一预留/结算内核；`expectedOutputTokens` 只表达运营成本估算中的平均输出量，`maxOutputTokens` 独立承担单次请求硬上限和最坏成本预留。请求发出前按后台发布的输入/缓存命中/输出单价和最大输出 Token 原子预留积分，并把同一最大输出值写入真实请求，同时冻结当时的服务地址版本和凭据版本；首版倍率固定为 1.0。升级迁移只会把历史 `expected_output_tokens` 一次性复制到新增上限字段，保留原估算事实，管理员随后可分别维护两者。
- 筷子 Kling 3 Omni 的普通无声、普通有声和参考视频价格按后台能力目录分别配置：`std/pro` 可发布同步音频档位，`4k` 只允许无声普通生成，参考视频只允许 `std/pro` 且禁止同步音频。画布参数规范化、Agent 渲染门禁、供应商请求和计费档位消费同一份分辨率能力事实，不会把不受支持的组合发送到上游或隐式改用其他档位。
- 筷子 GPT Image 2 的后台价格矩阵由模型动态能力目录展开为 `1K/2K/4K × low/medium/high` 九个必配档位。普通画布询价、Agent `media.generate` 冻结报价、Task/BillingOrder 预留与结算、供应商调用审计和成本统计都使用同一个 `resolution + quality` 规格身份；任一参数缺失、组合未发布或价格未配置都会在供应商请求前显式失败，不再把画质映射成分辨率，也不使用默认档位。
- Agent 流式请求强制开启 `stream=true` 与 `stream_options.include_usage=true`，响应 usage、Chat Completion request ID、finish reason 和断流事实会在 Task 终结前保留；缺失或无效 usage 分别标记为 `missing` / `invalid`，但资金结算仍以筷子账单的订单号、任务状态、总 Token 和实扣金额为准。账单 pending 时只异步核对，不重复调用模型。
- 筷子图片、视频与 Token 任务共用同一条账单核对 worker。新建付费任务会把供应商服务地址版本和凭据版本同时冻结到任务与账单；已取得上游任务号但本地结果不明确时，worker 只查询对应筷子账单：上游确认成功则结算本地冻结报价，明确失败且未扣费则退款，pending、矛盾或无法取得可复现运行时的账单继续保留人工核对，禁止猜测扣费结果。
- Chat Completion 响应 ID 按筷子契约从 `chatcmpl-<task_id>` 提取唯一内部任务 ID，写入计费订单后再通过任务账单接口取得真实扣费金额。成功账单原子消费实际积分并释放差额；待生成、缺失、重复或不可判定账单进入有租约、有限次数的后台核对，绝不重复发送模型请求。
- 上游账单事实不足时不会回退到固定 1 分或估算终值。超过预留、达到核对上限或凭据事实损坏都会保留冻结积分并进入显式人工核对。

## 本地开发

```bash
cd backend
go run ./cmd/server

# 另开终端
cd web
bun install --frozen-lockfile
bun run dev
```

也可以直接启动本地 Docker 环境：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/local-compose.ps1
```

默认访问地址为 `http://localhost:3000`。本地业务数据只允许位于 Git 主项目的 `.local/data`，不得提交到 Git。脚本会从 Git 公共目录解析主项目根；从 worktree 直接执行 `docker compose` 且未显式设置 `CANVAS_DATA_PATH` 会拒绝启动，防止误建第二份数据库。

### MiniMax Speech 配置

1. 在管理后台“AI 模型”中新建 `MiniMax Speech` 系统渠道，Base URL 使用 `https://api.minimaxi.com/v1`，API Key 只保存在后端。
2. 为渠道添加实际开通的音频模型和积分价格，例如 `speech-2.8-hd` 或 `speech-2.8-turbo`；用户端模型列表不会使用硬编码候选。
3. 打开“音色管理”，同步供应商音色，或在确认已获得声音本人授权后上传 10 秒至 5 分钟、20MB 以内的 MP3、M4A、WAV 样本进行克隆。
4. 为音色配置兼容模型、会员权限和发布状态。画布只展示后台已发布且与当前模型兼容的音色，任务创建前后端会再次校验权限并按模型价格扣除积分。

MiniMax 同步语音当前支持 MP3、WAV、FLAC，语速范围为 0.5–2.0。克隆源文件不会保存在本系统，只保留文件名、大小、SHA-256 和授权确认时间用于审计。没有真实 API Key 时只能完成本地契约测试，不能将模拟响应视为供应商验收。

## 生产部署

生产服务器必须安装 Docker Engine 与 Docker Compose。正式生产只有“受保护 Git 标签 → GitHub Actions → 严格 SSH 主机校验 → 版本化 bundle → `deploy/hmaigc.sh`”这一条发布写路径；服务器不执行 `git pull`、不现场编译，也不运行独立运维控制器。

业务配置保存在部署根目录的 `shared/production.env`，动态版本和回滚事实保存在独立部署状态目录。GitHub `production` Environment 必须配置审批、专用部署用户、SSH 私钥、预置 `known_hosts` 行、端口和绝对部署根目录。Backend/Web 镜像只接受 `repository@sha256:<digest>`，不使用 `latest` 或其他可变标签。

发布由正式标签自动触发；Actions 完成测试、版本化 Web 程序资源上传与 CDN 验证、同一 `dist` 的镜像构建和摘要解析后，把经过 SHA-256 校验的 bundle 上传到服务器，并由脱离 SSH 会话的主机 release runner 执行。日常升级不在管理后台或服务器命令行创建第二条任务；手工命令只用于基于已验证 bundle 的受控应急。生产入口应由 Caddy、Nginx 或云负载均衡器提供 HTTPS，后端、PostgreSQL 和 Redis 不得直接暴露公网。

业务后端不持有 Docker socket，也不能重启自己。主机事务脚本在在线态与停写态分别运行目标后端镜像自带的只读 Agent Runtime 升级审计；审计失败不会带病迁移。审计、同一恢复点备份和目标版本验活全部通过后才原子提交新版本状态，失败则按升级前镜像摘要与已校验恢复点恢复。完整安装、首次硬切、发布与应急契约见 [源码驱动生产发布说明](deploy/README.md) 与 [生产运行手册](PRODUCTION.md)。

## 上线门禁

- 在受控网络注册首个管理员后，保持公开注册关闭。
- 后台配置正式模型渠道、供应商预算、模型积分售价和并发策略。
- 配置支付商户资料并完成签名、异步通知、退款、关单和对账验收后，才能开放自动支付。
- 配置站点名称、Logo、版权、用户协议、隐私政策及备案信息。
- 对 PostgreSQL、后端资源卷和 `.settings-key` 建立加密备份与恢复演练。
- 完成前端构建、Go 全量测试、生产镜像构建和关键业务回归。
- 运行 `bun scripts/verify-spa-routes.mjs http://127.0.0.1:3000`，确认首页、画布、项目和管理后台深链接都由当前 SPA 入口提供。
- GitHub Actions 质量门禁通过后才允许发布生产镜像。

## 数据与升级

- PostgreSQL 是生产业务数据真源，Redis 只承担队列和实时广播。
- `backend-data` 保存上传文件及服务端密钥材料；数据库备份不能替代文件备份。
- 发布镜像由 GitHub Actions 构建并以不可变版本标签推送；生产服务器禁止现场构建或使用 `latest` 升级。
- 升级前先在隔离副本完成恢复演练并保留既有安全恢复点；正式升级中的目标镜像双阶段只读审计通过后，执行器才为 PostgreSQL 与 `backend-data` 创建同一升级恢复点。新后端完成原子迁移和版本验活后才允许启动 Web。
- 禁止把 `.env`、数据库、日志、备份、上传文件或真实密钥提交到仓库。
- 页面路由名称不得复用于 `web/public` 静态目录；画布人脸模型统一位于 `/runtime-assets/canvas-models/`，避免静态目录抢占 `/canvas` 页面路由。

## 许可证与上游

本项目基于 `ddcat-ai/open-ai-canvas` 与 `basketikun/infinite-canvas` 继续开发。上游署名和版权信息保留在 [NOTICE](NOTICE) 中。

当前代码采用 [GNU Affero General Public License v3.0](LICENSE)。通过网络向用户提供修改后的程序时，需要履行 AGPL-3.0 对应的源码提供义务；正式商业上线前应由法务确认开源合规、第三方模型条款和素材版权。
