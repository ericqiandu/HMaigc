# HMaigc 独立运维控制器与一键发布

本目录只服务单机 Linux + Docker Compose 生产环境。管理员选择已经发布的 `vX.Y.Z`，控制器在本机拉取后把 Backend、Web、备份 helper、Runner 与候选控制器解析为不可变镜像摘要并持久化；生产切换不在服务器现场编译源码，也不以标签或 `latest` 作为执行事实。

正式部署由两个独立 Compose 项目和一个按任务启动的一次性 Runner 组成：

- `hmaigc-ops`：稳定控制器，只负责鉴权、排队、签名命令、状态投影和恢复协调。
- `hmaigc`：Web、业务后端、PostgreSQL 与 Redis。业务后端没有 Docker socket，只能通过只读挂载的 Unix socket 和共享密钥向控制器提交已验签请求。
- `hmaigc-ops-runner-*`：按目标版本镜像摘要启动的 detached Runner。它持有阶段 checkpoint、租约、心跳和最终结果；浏览器、业务后端或控制器重启都不会令这些事实丢失。

控制器在业务后端停止、升级或启动失败时仍然运行，因此不能把控制器并入业务 Compose，也不能让业务后端直接执行 Docker 命令或重启自身。

## 一次性引导

旧控制平面必须只迁移一次。该操作会把配置真源切换到 `ops-state/config`，属于高风险生产变更：先在同构隔离环境演练，并在生产执行前重新确认源配置、源控制器数据库、目标控制器摘要和维护窗口。

1. 安装 Docker Engine 与 Docker Compose 插件，登录私有 GHCR。
2. 在旧后台确认没有 `queued`、`running`、`cancelling` 或 `recovering` 运维任务；停止旧 `ops-controller`，但不得停止业务服务、删除旧目录或删除卷。
3. 解析已发布控制器镜像的不可变 digest，禁止使用标签或 `latest`。
4. 从包含本版本脚本的可信目录执行：

```bash
bash deploy/hmaigc-bootstrap.sh \
  --source-env /absolute/legacy/.env.production \
  --source-db /absolute/legacy/controller.db \
  --state-volume hmaigc-ops-state \
  --controller-image 'ghcr.io/owner/hmaigc-ops-controller@sha256:<64-hex>' \
  --controller-version 'vX.Y.Z' \
  --protocol-version 1
```

引导会拒绝非终态历史任务，先创建并校验源数据库备份，再原子写入权限为 `0600` 的 `config/production.env` 与 `config/control.env`，导入终态审计事实并启动稳定控制器。相同输入可安全重试；不同输入会显式冲突。引导失败不会删除或改写旧目录。引导成功后，宿主机版本目录只作为人工证据保留，不再是日常配置或控制器真源。

规范配置必须包含摘要固定的 `HMAIGC_BACKEND_IMAGE`、`HMAIGC_WEB_IMAGE`、`BACKUP_HELPER_IMAGE` 和 `HMAIGC_OPS_IMAGE`。控制器 Compose 只挂载 Docker socket 与命名 `ops-state` 卷，不挂载宿主机版本目录或 `.env.production`。业务卷和 `HMAIGC_OPS_STATE_VOLUME` 启用后不得随意改变。

私有 GitHub 仓库若要在后台检查最新 Release，填写只读的 `HMAIGC_RELEASES_API_TOKEN`。不配置时版本检查会明确显示“未配置”，不会假装已经是最新版本。

外层 Nginx 必须参考 `deploy/nginx/hmaigc.conf.example`，替换域名和证书路径，并保持 80 端口到 HTTPS 443 的硬重定向。PostgreSQL、Redis 和后端均不得映射到公网。

收银台页面 `/pay/` 与能力 API `/api/payments/checkout/` 携带 bearer token。示例仅在这两个精确前缀关闭 access/error log；同时清除上游 Referer，强制 `Cache-Control: private, no-store`、`Pragma: no-cache` 与 `Referrer-Policy: no-referrer`。inner Nginx 既有的 `/api/public/canvas-shares/` bearer 前缀继续保持 error-log 隔离。这些位置的代理错误由后端脱敏结构化日志与 `/api/health` 提供诊断。

stock Nginx upstream error 格式无法移除 incoming Referer，所以示例仅在实际 reverse-proxy location 关闭该原生行，并用条件 access log 为普通 4xx/5xx 写入不含 Referer、query 或上游正文的 safe proxy-error（时间、客户端、方法、URI、状态和 upstream status）；普通 2xx access 仍可观测。server/main/global 的配置、启动和静态错误日志继续保留，禁止把静默范围扩到整个 server。

从旧版本切换到本安全边界前，先在外层 Nginx/网关精确拒绝 `POST /api/membership/orders`、`POST /api/credit-store/orders`、两类 `POST /api/*/orders/:id/checkout` 和 `POST /api/payments/checkout/:token/transactions`；必须继续放行 checkout GET 与微信/支付宝 webhook POST。完整验证步骤以 `PRODUCTION.md` 第 2 节为准。随后在旧版后台把 checkout base 改为无 path 的 HTTPS Origin、把 notify URL 改为 HTTPS并核验 CORS，至少等待 `15 分钟` checkout TTL，使旧 HTTP/pathful 加密链接自然失效。不得清库、轮换 bearer 或关闭门禁。若升级被支付运行时门禁拒绝，保持自动回滚，回旧版修正配置后再升级。

## 唯一正式操作入口

```bash
TARGET_VERSION='vX.Y.Z'

# 状态与环境验收
bash deploy/hmaigc-ops.sh status
bash deploy/hmaigc-ops.sh verify

# 命令行升级、备份和回滚（后台页面通过同一控制器队列执行）
bash deploy/hmaigc-ops.sh upgrade "$TARGET_VERSION"
bash deploy/hmaigc-ops.sh backup
bash deploy/hmaigc-ops.sh rollback

# 浏览器不可用时的安全停止与恢复
bash deploy/hmaigc-ops.sh cancel '<operation-id>'
bash deploy/hmaigc-ops.sh recover '<operation-id>'
```

首次部署或旧控制平面迁移只能使用上一节的一次性 `hmaigc-bootstrap.sh`，不能提交 `install` 运维任务。必须把 `vX.Y.Z` 替换为已经发布且镜像摘要可核对的实际标签；仍在 `CHANGELOG.md`“未发布”小节中的功能没有可用于生产升级的版本号。

bootstrap 会在写入任何目标事实前保存同源 `in-progress` 标记；进程中断后，使用完全相同的源数据库、源环境、控制器摘要和协议参数重试时，会逐项校验已写文件并继续。来源、摘要或已有内容不一致时必须显式失败，不会覆盖未知状态。

`deploy/hmaigc-stage.sh` 是 Runner 调用的确定性阶段适配器，`deploy/hmaigc.sh` 是旧编排器；两者都不是生产人员的正式入口。`hmaigc-ops.sh` 只从 `ops-state/config/control.env` 读取规范配置，启动对应摘要的稳定控制器，再由 `hmaigc-opsctl` 创建任务、持续读取持久日志并等待明确终态。它不依赖当前 shell 所在的版本目录。

升级执行器会使用目标后端镜像对现有数据库执行两次只读 Agent Runtime 升级审计：第一次在当前 Web/后端仍在线时运行，失败则零停机、零新备份；第一次通过后停止 Web/后端，再在静止数据库运行同一审计，失败则恢复当前版本且不创建新备份、不启动目标后端。报告一次列出全部不兼容 `queued/running/waiting_input/waiting_approval/waiting_tool` Run 的脱敏 blocker。只有两次审计均通过，才创建升级恢复点并启动目标版本；正式启动迁移仍是最终原子门禁。

升级失败会在 Web 重新开放前自动恢复升级前的数据与版本。目标版本与自动恢复版本的切换验收只依据本机容器健康、精确版本、SPA 根节点和本机入口资源，避免部署主机到 CDN 的瞬时网络抖动把健康版本误判失败。不可变公网 JS/CSS 已由标签发布门禁逐文件校验；独立 `verify` 命令会在统一的 120 秒上限内严格探测 `CANVAS_CORS_ORIGINS` 中每个生产 HTTPS Origin 的 `/canvas/` SPA 根节点，并按当前版本清单并发校验全部公网 CDN 资源。任一入口或资源不可访问都会显式失败，但不会停止或回滚健康服务。回滚请求会把已校验的上一版本恢复点固定到不可变请求中；进入维护窗口后先为当前版本创建并校验安全恢复点，且备份保留策略不得删除固定的回滚源。随后才恢复上一版本数据并启动旧版本。任一恢复、启动、验活或提交步骤失败，系统都会用当前版本安全恢复点还原回滚前状态。

后台 `/admin/operations` 提供版本检查、升级、回滚、实时日志、阶段、心跳、服务状态、停止、恢复、备份状态和审计记录。停止请求先持久化签名命令，只会在安全点生效；界面显示“已收到停止请求，正在到达安全点”时不得强杀容器。同一任务的停止或恢复重试复用稳定控制命令身份，容器启动故障修复后可以重放同一已验签恢复命令。若 `docker run` 已返回错误但控制器无法确认容器是否创建，任务必须进入 `recovery_required`；只有检查到相同 operation、generation 与 digest 的运行中 Runner 才可继续附着，停止容器或检查失败都不得伪造终态。`recovery_required` 会阻止新的升级、回滚、备份和 verify：若持久事实已经确定安全恢复动作，后台允许确认后启动更高 generation 的唯一恢复 Runner；若恢复动作为 `require_operator`，界面只展示证据且不提供不可执行的恢复按钮。每次调用都由业务后端重新校验管理员身份；首次引导不开放给后台页面。

## 静态资源 OSS/CDN 发布

从 `v1.0.13` 起，正式标签发布会先把 Web 构建产物写入独立静态资源 Bucket，再构建引用该不可变目录的 Web 镜像。用户媒体 Bucket 保持私有且通过业务接口鉴权读取，禁止与公开静态资源 Bucket 混用。

正式静态资源由已备案的 `static.hm.kunagent.com` 中国大陆 CDN 域名交付，`hmaigc-prod-static` 香港 Bucket 只作为回源和发布写入端点，对象前缀为 `hmaigc/web`。用户浏览器禁止直接读取 OSS 域名。仓库需要配置：

| 类型 | 名称 | 示例 |
| --- | --- | --- |
| Repository variable | `HMAIGC_STATIC_ASSET_BASE_URL` | `https://static.hm.kunagent.com/hmaigc/web` |
| Repository variable | `HMAIGC_STATIC_OSS_ENDPOINT` | `https://oss-cn-hongkong.aliyuncs.com` |
| Repository variable | `HMAIGC_STATIC_OSS_BUCKET` | `hmaigc-prod-static` |
| Repository variable | `HMAIGC_STATIC_OSS_PREFIX` | `hmaigc/web` |
| Repository secret | `HMAIGC_STATIC_OSS_ACCESS_KEY_ID` | 仅允许该 Bucket 写入与读取元数据的 RAM AccessKey |
| Repository secret | `HMAIGC_STATIC_OSS_ACCESS_KEY_SECRET` | 对应 Secret |

`HMAIGC_STATIC_ASSET_BASE_URL` 固定为 `https://static.hm.kunagent.com/hmaigc/web`，末尾不能带 `/`；`HMAIGC_STATIC_OSS_ENDPOINT` 仍是香港 OSS 写入端点，两者不得填成同一个地址。CDN 回源路径必须与 `HMAIGC_STATIC_OSS_PREFIX` 一致。Bucket/CDN 还必须满足：

正式 Web CSP 必须允许 `HMAIGC_STATIC_ASSET_BASE_URL` 的精确 Origin。若以后切换自定义 CDN 域名，必须在同一版本同步修改仓库变量和 `nginx.conf`；Nginx 与 Chromium 发布门禁会以仓库变量为准拒绝域名漂移，禁止先改变量、后补 CSP。

- 发布器为 `hmaigc/web/releases/*` 对象写入 `Cache-Control: public,max-age=31536000,immutable`；CDN 不配置会覆盖该响应头的“缓存过期时间”规则，直接遵循源站策略；对象名与版本目录不可变，不执行覆盖发布；
- `static.hm.kunagent.com` 配置有效 HTTPS 证书并开启 HTTP/2；HTTP/3 可按客户端覆盖情况选开，同时开启 Brotli 与 gzip；入口 JS/CSS 必须返回 `Content-Encoding: br` 或 `gzip`；
- CDN CORS 精确允许正式业务主域 `https://hm.kunagent.com` 的 GET/HEAD/OPTIONS，禁止通配 Origin 和写方法；品牌跳转域不承载正式应用，不加入静态模块 CORS；
- `index.html` 不上传 OSS，继续由版本化 Web 镜像提供且不得配置长期缓存；
- 发布器逐文件 PUT 后执行 HEAD 大小和 ETag 校验，最后才写 `manifest.json`；清单经 CDN 无法读取时镜像发布被阻止；
- 发布门禁读取真正启动 SPA 的入口脚本与样式，要求 HTTP/2 或 HTTP/3、正确 MIME、压缩、365 天不可变缓存与正式 Origin CORS；构建发布器仍逐文件校验全部产物。公网读取使用有界连接、传输和总重试时间，任一入口失败都会明确阻止镜像发布，不会因单个 CDN 请求无限挂起；
- 旧版本目录不自动删除，因此镜像回滚可继续读取同版本静态资源。

阿里云控制台一次性配置顺序：

1. 新增 CDN 加速域名 `static.hm.kunagent.com`，加速区域选择“中国内地”，业务类型选择图片小文件；
2. 源站类型选择 OSS 域名，指向 `hmaigc-prod-static.oss-cn-hongkong.aliyuncs.com`，回源 Host 保持该 Bucket 域名；
3. 将 DNS CNAME 指向 CDN 分配的 CNAME，配置 `static.hm.kunagent.com` HTTPS 证书并开启 HTTP/2；HTTP/3 可选；
4. 开启 Brotli 与智能压缩；不要为 `hmaigc/web/releases/*` 新增控制台“缓存过期时间”规则，避免覆盖发布器写入的完整 `Cache-Control`；
5. 配置响应头 CORS 后，先用临时版本目录验证，再修改 GitHub Repository variable；禁止先切变量、后补 CDN 配置。

配置完成后的人工核验：

```bash
curl --http2 --compressed -I \
  -H 'Origin: https://hm.kunagent.com' \
  'https://static.hm.kunagent.com/hmaigc/web/releases/vX.Y.Z/assets/<entry>.js'
```

必须同时看到 HTTP/2 或 HTTP/3、`Content-Encoding: br|gzip`、`Cache-Control: public,max-age=31536000,immutable`、`Access-Control-Allow-Origin: https://hm.kunagent.com` 和 `Access-Control-Allow-Methods: GET,HEAD,OPTIONS`。

生产服务器不需要保存静态 Bucket 的 AccessKey。密钥只存在于 GitHub Actions secrets，业务服务器仍只配置用户媒体 OSS。

## 历史媒体迁移

升级到 `v1.0.13` 后，数据库会新增迁移任务与迁移明细表。管理员先在“系统配置 → 存储服务”确认平台 OSS 已启用，再由“历史资源迁移”创建快照任务：

1. 系统只选择创建任务时仍为 `local + ready` 的资源；
2. Worker 复制文件、校验源大小和 SHA-256，再核对 OSS HEAD 大小与 ETag；
3. 校验通过后才在数据库事务中切换该资源引用；
4. 失败资源保持本地引用并记录原因，可由管理员重试；
5. 原数据卷文件不会自动删除，升级与回滚备份仍包含它们。

## 控制器交接

业务升级由 detached Runner 执行，因此控制器可以在操作中安全重建。Runner 先验证目标控制器镜像、协议、只读状态和 socket，再原子切换 `config/control.env`。新控制器必须重新附着同一 operation/generation；验证失败时 Runner 会恢复旧 `control.env` 和旧控制器，业务目标版本保持在线，任务以 `succeeded + controller_handoff_failed` 警告结束。

禁止在宿主机手改版本目录中的 `.env.production` 来升级控制器。若新旧控制器都无法确认，任务进入 `recovery_required`，应保留 Runner、lease、checkpoint、容器 inspect 和日志证据，再从后台或灾难 CLI 执行恢复；不得启动第二条升级链。

## 数据与审计

- 规范配置、不可变请求、签名命令、Runner lease/heartbeat/checkpoint/result、事件、控制器 SQLite 投影、发布状态和备份统一保存在独立 `ops-state` 命名卷。
- 控制器日志按游标持久化；业务后端升级重启后，后台页面可以从上次游标继续读取。
- 每个事实先落盘再发布；SQLite 是可重建投影，不是 Runner 执行真源。
- 每个恢复点包含 PostgreSQL custom dump、`backend-data` 压缩包、版本清单和 SHA-256 校验文件。
- `ops-state` 必须纳入主机磁盘加密与异地备份。当前恢复点本身不做应用层加密。
- 默认不自动删除任何备份。只有在 `.env.production` 显式设置正整数 `HMAIGC_BACKUP_RETENTION` 后，工具才会删除超出数量的最旧恢复点。
- Redis 是可重建的协调层，不进入业务恢复点；升级与回滚会重新启动 Redis，但不会把 Redis 当作业务事实源。
- 回滚会覆盖目标恢复点之后产生的数据。后台必须输入 `ROLLBACK`，命令行也会记录执行人、确认短语对应动作和幂等键。

## 发布前提

一键发布只解决可重复部署和回滚，不替代发布质量门禁。版本标签必须先通过 GitHub Actions 中的 Go 全量测试、真实 PostgreSQL 17/Redis 7 支付矩阵、Web 测试与生产构建、`bash scripts/tests/verify-payment-checkout-nginx.test.sh`、生产 Nginx Chromium 收银台矩阵、控制器镜像构建和三份 Compose 配置校验。涉及不可逆数据库变更时，即使脚本具备备份恢复能力，也必须先在隔离环境完成恢复演练。
