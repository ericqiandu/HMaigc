# HMaigc

HMaigc 是面向 AI 影视与短剧生产的商业化创作平台，覆盖项目、章节、角色资产、自由画布、图片/视频/音频生成、团队协作、会员计费、积分运营和管理后台。

## 代码结构

- `web/`：Vite、React、TypeScript 前端。
- `backend/`：Go、Gin、GORM 后端。
- `docker-compose.yml`：使用 SQLite 的本地一体化环境。
- `docker-compose.production.yml`：使用 PostgreSQL 和 Redis 的生产环境。

仓库只保留上述两条运行路径，不再维护旧镜像部署、重复 Compose 或上游一键安装脚本。
画布助手已硬切到单一服务端 Agent Runtime，并通过后端系统模型渠道完成鉴权、计费和请求审计；不再提供本机 Agent、Codex 插件连接或浏览器内模型循环。服务端负责冻结模型、决策循环、五类工具协调、通用交付验收和可恢复检查点；Web 只提交用户目标与真实选区事实、展示持久化事件并处理审批。

### 首页到 Agent 的创作链路

- 首页创作框先上传参考图片并创建真实画布项目，把提示词、账号级资源 ID、系统动态模型选择、公开 Skill 目录与显式执行模式写入项目内的 `pendingAgentLaunch`；提示词和临时 Blob URL 均不进入 URL 或持久事实。
- 打开新画布后，Agent 面板用该请求创建或复用服务端 thread，并以持久化 `clientRequestId` 启动 run；只有取得运行事实后才消费启动请求。启动响应丢失时刷新或重试仍复用同一请求 ID，不重复创建运行。
- Agent 推理模型、工具规划与交付判断由服务端 Runtime 统一决定。首页与画布共用同一份草稿契约，允许用户从系统动态目录显式指定本次运行的图片/视频模型、公开 Skills、参考图片与执行模式；Web 只提交 `channelId + model`、Skill 目录标识、账号级 `resourceId + name` 和模式枚举。服务端重新校验模型可调用且已定价、读取 Skill 详情、核对资源归属与可用状态，并将模型、Skill 指令、资源 MIME/尺寸及执行模式冻结进 run checkpoint。引导模式继续审批所有有副作用工具；自动模式只免除可逆 `canvas.apply_ops` 的审批，付费生成仍必须确认。需要确认时，前端展示已冻结的 `toolCallId + actionVersion + arguments`，用户批准后由后端按权限、revision CAS、计费和幂等事实执行。
- 选区读取是唯一需要浏览器回传的当前画布事实；前端提交真实 revision 与节点 ID。图片、视频和音频生成结果只以服务端 Task、BillingOrder、Resource 和交付验收事实为准。

### 单一 Agent Runtime

- 运行作用域固定绑定租户、用户、项目、画布、会话和运行记录；每次工具执行都会重新读取真实画布权限，不信任浏览器缓存的权限声明。
- `stateVersion` 独立承担审批、工具结果和恢复操作的并发控制，`stepNumber` 只在模型作出下一次决策时递增，避免工具恢复被重复计费为模型步骤。
- 首个模型决策必须声明结构化 `expectedDelivery`，Runtime 将其冻结为整条 run 的不可变交付合同；后续每个工具调用和最终答复必须逐字段保持一致。工具失败、审批拒绝或证据不足不能把图片、视频或画布交付降级成文字回答，合同漂移会作为显式修复事实回灌同一条有界执行链。
- 工具调用按 `runId + toolCallId + actionVersion` 冻结并幂等登记。`canvas.read_state`、`canvas.read_selection` 已由服务端协调；选择事实必须匹配当前画布 revision 和真实节点。
- `canvas.apply_ops` 使用画布 revision CAS 与稳定 `clientMutationId` 幂等提交；`generation.submit` 在服务端按同一计价内核取得当前单任务报价并携带版本与指纹，随后复用正式 Task、BillingOrder、积分预留和冻结供应商版本，禁止绕过媒体报价校验；`generation.wait` 只接受同一运行创建的任务与真实终态资产。后台 worker 会定期核对持久化等待事实，进程中断后不依赖浏览器重复提交生成任务。
- 每个新工具动作必须使用未出现过的 `toolCallId + actionVersion`；模型误复用历史身份时，Runtime 记录显式 `tool_identity_reused` 修复事实并继续同一执行链，不会再次写入冲突记录。最后一个模型步骤不得再开启新工具调用；历史运行若已进入该状态，拒绝或完成工具后会保存结果并以 `step_budget_exhausted` 明确终结。图片生成公共契约要求规范字符串字段 `size/count` 并强制匹配本次运行冻结的图片模型；`quality` 仅在动态 `providerCapabilities.qualities` 发布非空候选时才允许从候选中填写，候选为空则必须省略，禁止默认画质、`ratio/resolution` 或其他未知字段绕开正式任务契约。
- 模型每轮只接收当前用户真正可调用、已定价且凭据健康的图片、视频和音频模型事实；用户显式选择图片或视频模型时，本次 run 的对应能力目录只保留该模型，未选择的能力仍使用完整可调用目录。模型和 Skill 配置随 run checkpoint 冻结，不暴露 Base URL、Key 或凭据密文，也不会用硬编码候选兜底。
- Agent 模型调用统一声明 Chat Completions 的 `response_format=json_object`，使 DeepSeek 与 GPT 共用同一结构化决策契约；模型仍必须通过 Runtime 的严格单 JSON 校验。决策结构无效时，Runtime 记录受控的 `model_decision_invalid` 事实并在同一有界 run 内回灌下一模型步骤自修，绝不提取文本、伪造默认决策或切换模型；达到步骤上限仍显式失败。
- 参考图片只接受当前账号已就绪的图片 Resource；服务端冻结资源 ID、显示名称、MIME 与尺寸，拒绝浏览器 Blob URL、跨账号资源和失效资源。执行模式是必填运行事实，幂等重放若更换模型、Skill、附件或模式会显式冲突，禁止静默采用新配置。
- 正式传输入口为 `GET /api/agent/threads?canvasId=...&limit=...`、`POST /api/agent/threads`、`POST /api/agent/threads/:threadId/runs`、`GET /api/agent/runs/:runId`、`GET /api/agent/runs/:runId/events?afterSequence=N`、审批和工具结果提交。历史查询只投影当前用户、当前租户与当前画布最近 20 个 Thread 及各自最新 Run/checkpoint；SSE 仅发送已持久化事件。Web 对历史与本地恢复句柄分别报告错误，按最后确认的 sequence 续接，未知状态、未知事件、Run/checkpoint 冲突或非法 DTO 均显式失败。
- 服务端历史是会话发现与跨设备恢复的权威来源；浏览器 `localForage` 只保存当前 Thread、活动 Run、事件游标或尚未确认的 `clientRequestId`，用于快速恢复和启动幂等，不构成会话事实。没有本地句柄时采用服务端最近活动会话；选择旧会话只切换观察和恢复目标，不取消服务端仍在运行的 Run。
- 浏览器不再调用 Agent 模型、不拼 system prompt、不维护 tool loop，也不再创建固定影视 Session。旧会话事实仅保留在历史项目数据中用于审计，不进入新运行链。

### Agent 模型计费链路

- 网站 Agent 的每次模型请求分别创建计费订单；工具调用本身不计费，图片、视频等媒体生成继续使用各自独立订单。
- Agent 模型任务创建前若确认账号余额不足或团队月额度耗尽，运行会以 `insufficient_credits` 或 `team_credit_limit_reached` 明确终结，不创建下一任务或账单，也不会由后台驱动器无限重试；并发与暂时性额度错误仍保持原有显式错误语义。
- 托管的筷子 DeepSeek 模型使用 `token_usage`：系统代理和服务端 Agent Runtime 共用同一预留/结算内核；请求发出前按后台发布的输入/缓存命中/输出单价和最大输出 Token 原子预留积分，并把同一最大输出值写入真实请求，同时冻结当时的服务地址版本和凭据版本；首版倍率固定为 1.0。
- 流式请求会强制开启 `stream_options.include_usage`，响应 usage 会先持久化；缺失或无效 usage 分别标记为 `missing` / `invalid`，但资金结算仍以筷子账单的订单号、任务状态、总 Token 和实扣金额为准。账单 pending 时只异步核对，不重复调用模型。
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

生产服务器必须安装 Docker Engine 与 Docker Compose。先从私有仓库取得代码，然后创建生产配置：

```bash
cp .env.production.example .env.production
chmod 600 .env.production
openssl rand -hex 32
```

把生成结果写入 `.env.production` 的 `POSTGRES_PASSWORD`，并至少配置：

- `HMAIGC_IMAGE_REGISTRY`：例如 `ghcr.io/ericqiandu`。
- `HMAIGC_VERSION`：与 Git 标签一致的不可变版本，例如 `v1.0.14`。
- `HMAIGC_OPS_VERSION`：独立运维控制器的不可变版本。
- `HMAIGC_RELEASES_API_URL`：用于后台检查最新 GitHub Release。
- `CANVAS_CORS_ORIGINS`：实际 HTTPS 站点 Origin。
- `CANVAS_HTTP_HOST`：有反向代理时保持 `127.0.0.1`。
- `CANVAS_HTTP_PORT`：反向代理连接的本机端口。

首次安装：

```bash
bash deploy/hmaigc-ops.sh install v1.0.14
```

生产环境应由 Caddy、Nginx 或云负载均衡器提供 HTTPS。不要直接把后端、PostgreSQL 或 Redis 暴露到公网。

后续升级与回滚：

```bash
bash deploy/hmaigc-ops.sh upgrade v1.0.14
bash deploy/hmaigc-ops.sh rollback
```

业务后端不持有 Docker socket，也不会重启自己；后台运维升级中心和服务器命令行都把任务提交给独立控制器。完整契约见 [独立控制器与一键发布说明](deploy/README.md) 与 [生产运行手册](PRODUCTION.md)。

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
- 升级前同时备份 PostgreSQL 与 `backend-data`，新后端完成迁移和版本验活后才允许启动 Web。
- 禁止把 `.env`、数据库、日志、备份、上传文件或真实密钥提交到仓库。
- 页面路由名称不得复用于 `web/public` 静态目录；画布人脸模型统一位于 `/runtime-assets/canvas-models/`，避免静态目录抢占 `/canvas` 页面路由。

## 许可证与上游

本项目基于 `ddcat-ai/open-ai-canvas` 与 `basketikun/infinite-canvas` 继续开发。上游署名和版权信息保留在 [NOTICE](NOTICE) 中。

当前代码采用 [GNU Affero General Public License v3.0](LICENSE)。通过网络向用户提供修改后的程序时，需要履行 AGPL-3.0 对应的源码提供义务；正式商业上线前应由法务确认开源合规、第三方模型条款和素材版权。
