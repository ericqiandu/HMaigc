# HMaigc 独立运维控制器与一键发布

本目录只服务单机 Linux + Docker Compose 生产环境。发布单位是 GitHub Container Registry 中的不可变 `vX.Y.Z` 镜像标签，不在生产服务器现场编译源码，也不使用 `latest` 作为可回滚版本。

正式部署由两个独立 Compose 项目组成：

- `hmaigc-ops`：独立运维控制器。它独占 Docker socket、发布锁、备份、发布状态和运维审计数据库。
- `hmaigc`：Web、业务后端、PostgreSQL 与 Redis。业务后端没有 Docker socket，只能通过只读挂载的 Unix socket 和共享密钥向控制器提交已验签请求。

控制器在业务后端停止、升级或启动失败时仍然运行，因此不能把控制器并入业务 Compose，也不能让业务后端直接执行 Docker 命令或重启自身。

## 首次准备

1. 安装 Docker Engine 与 Docker Compose 插件。
2. 发布人员创建与根目录 `VERSION` 完全一致的不可变 `vX.Y.Z` 标签并等待镜像工作流通过；服务器运维人员只选择已经存在且摘要核验通过的标签，不在生产机创建或推送标签。
3. 私有 GHCR 包需先执行一次 `docker login ghcr.io`。
4. 在服务器仓库根目录执行：

```bash
cp .env.production.example .env.production
chmod 600 .env.production
```

填写真实镜像仓库、业务版本、控制器版本、数据库密码、`CANVAS_ENVIRONMENT=production`、HTTPS Origin 和本机监听端口。`HMAIGC_BACKEND_GID` 必须保持与发布镜像中的固定值 `101` 一致。控制器启动时会把只读挂载的生产配置复制到独立运维卷并收紧为 `600`，避免 Docker Desktop 与不同宿主机的 bind mount 权限语义影响发布校验。业务卷和 `HMAIGC_OPS_STATE_VOLUME` 启用后不得随意改变，否则控制器会失去发布状态、审计日志或生产恢复点。后端会在启动 worker/listener 前校验持久化支付公开 URL；checkout base 必须是无 path 的 HTTPS Origin，渠道 notify URL 可保留 webhook path。生产环境中的 HTTP 收银台、HTTP 支付回调、非法 Origin 或损坏 JSON 都会显式阻止启动。

私有 GitHub 仓库若要在后台检查最新 Release，填写只读的 `HMAIGC_RELEASES_API_TOKEN`。不配置时版本检查会明确显示“未配置”，不会假装已经是最新版本。

外层 Nginx 必须参考 `deploy/nginx/hmaigc.conf.example`，替换域名和证书路径，并保持 80 端口到 HTTPS 443 的硬重定向。PostgreSQL、Redis 和后端均不得映射到公网。

收银台页面 `/pay/` 与能力 API `/api/payments/checkout/` 携带 bearer token。示例仅在这两个精确前缀关闭 access/error log；同时清除上游 Referer，强制 `Cache-Control: private, no-store`、`Pragma: no-cache` 与 `Referrer-Policy: no-referrer`。inner Nginx 既有的 `/api/public/canvas-shares/` bearer 前缀继续保持 error-log 隔离。这些位置的代理错误由后端脱敏结构化日志与 `/api/health` 提供诊断。

stock Nginx upstream error 格式无法移除 incoming Referer，所以示例仅在实际 reverse-proxy location 关闭该原生行，并用条件 access log 为普通 4xx/5xx 写入不含 Referer、query 或上游正文的 safe proxy-error（时间、客户端、方法、URI、状态和 upstream status）；普通 2xx access 仍可观测。server/main/global 的配置、启动和静态错误日志继续保留，禁止把静默范围扩到整个 server。

从旧版本切换到本安全边界前，先在外层 Nginx/网关精确拒绝 `POST /api/membership/orders`、`POST /api/credit-store/orders`、两类 `POST /api/*/orders/:id/checkout` 和 `POST /api/payments/checkout/:token/transactions`；必须继续放行 checkout GET 与微信/支付宝 webhook POST。完整验证步骤以 `PRODUCTION.md` 第 2 节为准。随后在旧版后台把 checkout base 改为无 path 的 HTTPS Origin、把 notify URL 改为 HTTPS并核验 CORS，至少等待 `15 分钟` checkout TTL，使旧 HTTP/pathful 加密链接自然失效。不得清库、轮换 bearer 或关闭门禁。若升级被支付运行时门禁拒绝，保持自动回滚，回旧版修正配置后再升级。

## 唯一正式操作入口

```bash
TARGET_VERSION='vX.Y.Z'

# 首次安装
bash deploy/hmaigc-ops.sh install "$TARGET_VERSION"

# 状态与环境验收
bash deploy/hmaigc-ops.sh status
bash deploy/hmaigc-ops.sh verify

# 命令行升级、备份和回滚（后台页面也通过同一控制器队列执行）
bash deploy/hmaigc-ops.sh upgrade "$TARGET_VERSION"
bash deploy/hmaigc-ops.sh backup
bash deploy/hmaigc-ops.sh rollback
```

必须把 `vX.Y.Z` 替换为已经发布且镜像摘要可核对的实际标签；仍在 `CHANGELOG.md`“未发布”小节中的功能没有可用于生产安装的版本号。

`deploy/hmaigc.sh` 是控制器镜像内部的执行器，不是生产人员的正式入口。`hmaigc-ops.sh` 会先启动独立控制器，再由 `hmaigc-opsctl` 创建任务、持续读取独立审计库中的日志，并等待明确成功或失败。

升级失败会在 Web 重新开放前自动恢复升级前的数据与版本。回滚前也会先为当前版本创建安全恢复点；若目标旧版本验收失败，会恢复到回滚前状态。

后台 `/admin/operations` 提供版本检查、升级、回滚、实时日志、备份状态和审计记录。每次调用都由业务后端重新校验管理员身份；首次安装不会开放给后台页面，只允许服务器命令行引导。

## 静态资源 OSS/CDN 发布

从 `v1.0.13` 起，正式标签发布会先把 Web 构建产物写入独立静态资源 Bucket，再构建引用该不可变目录的 Web 镜像。用户媒体 Bucket 保持私有且通过业务接口鉴权读取，禁止与公开静态资源 Bucket 混用。

当前正式静态资源直接使用 `hmaigc-prod-static` Bucket 的 HTTPS 读取域名，对象前缀为 `hmaigc/web`。仓库需要配置：

| 类型 | 名称 | 示例 |
| --- | --- | --- |
| Repository variable | `HMAIGC_STATIC_ASSET_BASE_URL` | `https://hmaigc-prod-static.oss-cn-hongkong.aliyuncs.com/hmaigc/web` |
| Repository variable | `HMAIGC_STATIC_OSS_ENDPOINT` | `https://oss-cn-hongkong.aliyuncs.com` |
| Repository variable | `HMAIGC_STATIC_OSS_BUCKET` | `hmaigc-prod-static` |
| Repository variable | `HMAIGC_STATIC_OSS_PREFIX` | `hmaigc/web` |
| Repository secret | `HMAIGC_STATIC_OSS_ACCESS_KEY_ID` | 仅允许该 Bucket 写入与读取元数据的 RAM AccessKey |
| Repository secret | `HMAIGC_STATIC_OSS_ACCESS_KEY_SECRET` | 对应 Secret |

`HMAIGC_STATIC_ASSET_BASE_URL` 末尾不能带 `/`，并且 URL 路径必须与 CDN 回源后的 `HMAIGC_STATIC_OSS_PREFIX` 一致。Bucket/CDN 还必须满足：

正式 Web CSP 必须允许 `HMAIGC_STATIC_ASSET_BASE_URL` 的精确 Origin。若以后切换自定义 CDN 域名，必须在同一版本同步修改仓库变量和 `nginx.conf`；Nginx 与 Chromium 发布门禁会以仓库变量为准拒绝域名漂移，禁止先改变量、后补 CSP。

- CDN 对 `hmaigc/web/releases/*` 缓存 365 天；对象名与版本目录不可变，不执行覆盖发布；
- 只允许 `https://hmaigc.ai`、`https://www.hmaigc.ai` 与正式业务主域 `https://hm.kunagent.com` 跨域 GET/HEAD，禁止开放写方法；
- `index.html` 不上传 OSS，继续由版本化 Web 镜像提供且不得配置长期缓存；
- 发布器逐文件 PUT 后执行 HEAD 大小和 ETag 校验，最后才写 `manifest.json`；清单经 CDN 无法读取时镜像发布被阻止；
- 旧版本目录不自动删除，因此镜像回滚可继续读取同版本静态资源。

生产服务器不需要保存静态 Bucket 的 AccessKey。密钥只存在于 GitHub Actions secrets，业务服务器仍只配置用户媒体 OSS。

## 历史媒体迁移

升级到 `v1.0.13` 后，数据库会新增迁移任务与迁移明细表。管理员先在“系统配置 → 存储服务”确认平台 OSS 已启用，再由“历史资源迁移”创建快照任务：

1. 系统只选择创建任务时仍为 `local + ready` 的资源；
2. Worker 复制文件、校验源大小和 SHA-256，再核对 OSS HEAD 大小与 ETag；
3. 校验通过后才在数据库事务中切换该资源引用；
4. 失败资源保持本地引用并记录原因，可由管理员重试；
5. 原数据卷文件不会自动删除，升级与回滚备份仍包含它们。

## 控制器自身升级

控制器不能通过自身任务队列替换自己。发布新的控制器版本时：

1. 先确认当前没有 `queued` 或 `running` 运维任务。
2. 修改 `.env.production` 中 `HMAIGC_OPS_VERSION` 为新的不可变标签。
3. 执行 `bash deploy/hmaigc-ops.sh status`，外层 Compose 会拉取并重建控制器。
4. 在后台确认控制器版本、历史审计与备份列表仍完整。

控制器启动时若发现上次任务处于运行中，会把它显式标记为“控制器重启中断”，要求管理员核对实际服务状态，不会自动猜测成功或继续执行。

## 数据与审计

- 发布状态、锁、脚本日志、控制器 SQLite 审计库和备份统一保存在独立 `ops-state` 命名卷。
- 控制器日志按游标持久化；业务后端升级重启后，后台页面可以从上次游标继续读取。
- 每个恢复点包含 PostgreSQL custom dump、`backend-data` 压缩包、版本清单和 SHA-256 校验文件。
- `ops-state` 必须纳入主机磁盘加密与异地备份。当前恢复点本身不做应用层加密。
- 默认不自动删除任何备份。只有在 `.env.production` 显式设置正整数 `HMAIGC_BACKUP_RETENTION` 后，工具才会删除超出数量的最旧恢复点。
- Redis 是可重建的协调层，不进入业务恢复点；升级与回滚会重新启动 Redis，但不会把 Redis 当作业务事实源。
- 回滚会覆盖目标恢复点之后产生的数据。后台必须输入 `ROLLBACK`，命令行也会记录执行人、确认短语对应动作和幂等键。

## 发布前提

一键发布只解决可重复部署和回滚，不替代发布质量门禁。版本标签必须先通过 GitHub Actions 中的 Go 全量测试、真实 PostgreSQL 17/Redis 7 支付矩阵、Web 测试与生产构建、`bash scripts/tests/verify-payment-checkout-nginx.test.sh`、生产 Nginx Chromium 收银台矩阵、控制器镜像构建和三份 Compose 配置校验。涉及不可逆数据库变更时，即使脚本具备备份恢复能力，也必须先在隔离环境完成恢复演练。
