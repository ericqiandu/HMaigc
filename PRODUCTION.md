# HMaigc 生产运行手册

本手册适用于单机 Docker Compose 部署。生产数据由 PostgreSQL、`backend-data` 数据卷和 Redis 共同承载，其中 PostgreSQL 与 `backend-data` 必须作为同一恢复点备份；Redis 可重建，但重建期间并发协调和实时协作会中断。

## 1. 上线前检查

```bash
CANVAS_DATA_PATH=/absolute/path/to/local-data docker compose config -q
docker compose --env-file .env.production -f docker-compose.production.yml config -q
docker compose --env-file .env.production -f deploy/docker-compose.ops.yml config -q
cd backend && go test ./... && go test -race ./... && go vet ./... && go build ./...
cd .. && bash scripts/tests/run-payment-integration.sh --all
cd web && bun install --frozen-lockfile && bun test && bun run build
HMAIGC_CHROMIUM_EXECUTABLE=/path/to/chrome bun run verify:membership-checkout-browser
cd .. && bash scripts/tests/verify-payment-checkout-nginx.test.sh
STATIC_RELEASE_URL='https://static.hm.kunagent.com/hmaigc/web/releases/vX.Y.Z'
bash scripts/verify-static-release-assets.sh web/dist "$STATIC_RELEASE_URL"
bun scripts/verify-spa-routes.mjs http://127.0.0.1:3000
```

必须确认：

- `.env.production` 未被 Git 跟踪且权限为 `600`，`POSTGRES_PASSWORD` 为独立随机强密码。
- `HMAIGC_VERSION` 是与根目录 `VERSION` 一致的不可变 `vX.Y.Z` 标签，禁止使用 `latest`。
- `HMAIGC_OPS_VERSION` 是独立控制器的不可变标签，`HMAIGC_OPS_STATE_VOLUME` 已纳入加密备份。
- 后端容器没有 Docker socket；只有独立控制器容器可以访问 Docker Engine。
- `CANVAS_ENVIRONMENT=production` 已显式配置；缺失值、未知值或其他环境值都会阻止后端启动。
- `CANVAS_CORS_ORIGINS` 只包含实际 HTTPS 站点 Origin。
- `HMAIGC_STATIC_ASSET_BASE_URL` 为 `https://static.hm.kunagent.com/hmaigc/web`；入口 JS/CSS 经中国大陆 CDN 使用 HTTP/2 或 HTTP/3、Brotli/gzip 和 365 天不可变缓存，未直接暴露香港 OSS 读取域名。
- 支付收银台地址是无 path、userinfo、query、fragment 的 HTTPS Origin；支付渠道回调地址允许 webhook path，但不得含 userinfo、query 或 fragment。
- `CANVAS_HTTP_HOST=127.0.0.1`，只有反向代理对公网开放。
- PostgreSQL、Redis 和后端端口没有映射到公网。
- 支付、模型渠道和邮件密钥只在后台或服务器环境中配置。
- 已完成 AGPL、第三方模型条款和生产依赖许可证审查。

## 2. 启动与健康检查

```bash
TARGET_VERSION='vX.Y.Z'
bash deploy/hmaigc-ops.sh install "$TARGET_VERSION"
bash deploy/hmaigc-ops.sh verify
```

必须把 `vX.Y.Z` 替换为已经完成发布提交、镜像工作流和摘要核验的实际标签。`CHANGELOG.md` 中仍位于“未发布”的功能没有可部署标签，禁止把当前 `VERSION` 或本地提交状态推测为已经发布。

`/api/health` 会实时检查 PostgreSQL、Redis 和持久化支付公开 URL 配置，并返回镜像编译时注入的版本和提交。支付运行时校验在 worker 和 HTTP listener 之前执行；生产环境出现 HTTP 收银台/回调地址、非法公开 Origin 或损坏的持久化支付 JSON 时会直接阻止启动，运行中配置异常则使 readiness 返回 `503`。部署工具必须确认实际运行版本与目标标签一致。`/canvas/` 检查用于确认 Nginx 没有把 SPA 页面路由误判成静态目录，并以有界超时验证真正启动 SPA 的入口脚本与样式；单个 CDN 请求超时会明确失败，不得长期占用运维任务。任一检查失败时禁止继续发布流量。

外层 Nginx 必须按 `deploy/nginx/hmaigc.conf.example` 配置证书并将 80 端口永久重定向到 HTTPS 443。`/pay/` 和 `/api/payments/checkout/` 是 bearer capability 精确前缀：两层 Nginx 均关闭这两个位置的 access/error log，并强制 `private, no-store`、`no-cache` 和 `no-referrer`；inner Nginx 既有的 `/api/public/canvas-shares/` bearer 前缀继续保持 error-log 隔离。该局部可观测性由后端不含敏感值的结构化错误日志和 `/api/health` 替代。

stock Nginx upstream error 行会原样附带 incoming Referer，无法自定义格式。因此仅在实际 reverse-proxy location 关闭 stock upstream error 行，改由条件 access log 为普通 4xx/5xx 写入 safe proxy-error 事实（时间、客户端、方法、不含 query 的 URI、状态与 upstream status）；普通 2xx access log 同样不含 Referer。server/main/global 的配置、启动与静态文件错误日志继续保留，不得把该替换扩到整个 server。

从旧版本升级前必须先暂停新的下单/checkout 创建，在旧版后台把 checkout base 修正为无 path 的 HTTPS Origin，把全部渠道 notify URL 修正为 HTTPS，并核验生产 CORS Origin。当前没有后台“一键暂停支付”开关；切换窗口必须在外层 Nginx 或上游网关按 method + path 临时拒绝以下五类写请求：`POST /api/membership/orders`、`POST /api/credit-store/orders`、`POST /api/membership/orders/:id/checkout`、`POST /api/credit-store/orders/:id/checkout`、`POST /api/payments/checkout/:token/transactions`。规则必须继续放行 `GET /api/payments/checkout/:token` 只读查询以及 `POST /api/payments/webhooks/wechat`、`POST /api/payments/webhooks/alipay` 渠道回调，不得用整个 `/api/payments/` 前缀封禁代替精确规则。维护规则必须经过 `nginx -t`，逐条请求验证拒绝/放行结果，并由运维记录开始、恢复时间和实际命中状态；无法建立这条隔离边界时不得继续升级。暂停后至少等待最长 `15 分钟` checkout TTL，使旧 HTTP/pathful 加密链接自然失效；不得清库或隐式轮换 bearer。若新版本因支付运行时门禁拒绝启动，应保持发布工具的自动回滚，回到旧版修正持久化配置后重新升级；禁止临时改回 development、HTTP 或关闭门禁。

会员收银台只支持当前微信与支付宝的一次性扫码支付，不存在自动续费、分期或前端人工标记已支付路径。活动收银台的 bearer 只以 SHA-256 哈希查询，并依赖生产 `/data/.settings-key` 加密保存的 `enc:v1` envelope 为订单所有者恢复同一链接；该文件属于 `backend-data` 命名卷，必须与 PostgreSQL 进入同一恢复点并在恢复演练中验证权限与解密能力。历史 hash-only 会话仍可凭原 bearer 只读访问，但无法反向恢复或生成替代 token。迁移或轮换 `.settings-key` 前必须确认没有仍处于有效期内的活动收银台。

后台支付交易出现 `review_required` 时，只能从“支付交易”执行“渠道对账”：渠道确认已支付后进入统一履约，确认未支付或不存在且远端关单成功后才关闭本地交易，查询结果不确定时继续保持待复核。禁止直接修改会员订单为已支付。验签成功的迟到、第二笔或履约失败事实会先独立持久化，再由运营核对；不得删除事件、重建商户单或把未知状态改成失败以释放付款槽位。

支付完整性迁移先增加可承载历史数据的列与默认值，再扫描错误索引谓词、同订单多笔可支付交易、重复渠道交易号、缺少关联交易事实的已处理回调和非正价付费套餐，最后才创建唯一约束。任一冲突都会让迁移非零退出且不删除、不挑选或改写财务事实。升级前必须先执行下一节的同一恢复点备份，并在隔离的生产数据副本运行迁移与 `scripts/tests/run-payment-integration.sh --all`；生产冲突只能依据审计事实人工核对后重新演练，禁止在迁移脚本中自动修正。

## 3. 同一恢复点备份

正式备份统一执行：

```bash
bash deploy/hmaigc-ops.sh backup
```

工具会先停止 Web 与后端写入，再生成同一恢复点。备份成功必须同时满足：

- 两个归档文件均存在且非空。
- `sha256sum -c SHA256SUMS` 通过。
- `pg_restore --list postgres.dump` 能读取归档目录。

## 4. 恢复演练

恢复必须先在隔离项目中验证，禁止直接覆盖生产数据库。

1. 复制 `.env.production` 为 `.env.restore`，把 `HMAIGC_VERSION` 改为本次准备发布的目标标签，并修改业务/运维 Compose 项目名、三个业务卷名、`HMAIGC_OPS_STATE_VOLUME`、HTTP 端口和数据库密码。将 `HMAIGC_HOST_ENV_FILE` 设为 `../.env.restore`，确保隔离控制器读取隔离配置。目标业务标签和 `HMAIGC_OPS_VERSION` 都必须已存在于镜像仓库且摘要已核验；不得沿用生产 ops-state、使用 `latest` 或让隔离控制器读取 `.env.production`。
2. 使用独立项目名启动 PostgreSQL 与 Redis：

```bash
docker compose -p hmaigc-restore --env-file .env.restore \
  -f docker-compose.production.yml up -d postgres redis --wait
```

3. 把 `postgres.dump` 复制进隔离 PostgreSQL 容器，恢复到新建数据库。
4. 使用只读源归档恢复新的 `hmaigc-restore_backend-data` 数据卷。
5. 恢复 PostgreSQL 与 `backend-data` 后，用目标版本启动隔离后端并保留迁移日志，再启动 Web：

```bash
set -Eeuo pipefail
umask 077

TARGET_VERSION='vX.Y.Z'
RESTORE_LOG_DIR="/var/tmp/hmaigc-restore-logs-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -m 700 "$RESTORE_LOG_DIR"

sed -i "s/^HMAIGC_VERSION=.*/HMAIGC_VERSION=${TARGET_VERSION}/" .env.restore
grep -Fx "HMAIGC_VERSION=${TARGET_VERSION}" .env.restore
grep -Fx 'HMAIGC_OPS_STATE_VOLUME=hmaigc-restore-ops-state' .env.restore
grep -Fx 'HMAIGC_HOST_ENV_FILE=../.env.restore' .env.restore

if ! docker compose -p hmaigc-restore-ops --env-file .env.restore \
  -f deploy/docker-compose.ops.yml up -d ops-controller --wait; then
  docker compose -p hmaigc-restore-ops --env-file .env.restore \
    -f deploy/docker-compose.ops.yml ps --all \
    > "$RESTORE_LOG_DIR/ops-ps.txt" 2>&1 || true
  docker compose -p hmaigc-restore-ops --env-file .env.restore \
    -f deploy/docker-compose.ops.yml logs --no-color --no-log-prefix ops-controller \
    > "$RESTORE_LOG_DIR/ops-controller.log" 2>&1 || true
  exit 1
fi

if ! docker compose -p hmaigc-restore --env-file .env.restore \
  -f docker-compose.production.yml up -d backend --wait; then
  docker compose -p hmaigc-restore --env-file .env.restore \
    -f docker-compose.production.yml ps --all \
    > "$RESTORE_LOG_DIR/business-ps.txt" 2>&1 || true
  docker compose -p hmaigc-restore --env-file .env.restore \
    -f docker-compose.production.yml logs --no-color --no-log-prefix backend \
    > "$RESTORE_LOG_DIR/backend.log" 2>&1 || true
  exit 1
fi

docker compose -p hmaigc-restore --env-file .env.restore \
  -f docker-compose.production.yml logs --no-color --no-log-prefix backend \
  > "$RESTORE_LOG_DIR/backend.log" 2>&1

if ! docker compose -p hmaigc-restore --env-file .env.restore \
  -f docker-compose.production.yml up -d web --wait; then
  docker compose -p hmaigc-restore --env-file .env.restore \
    -f docker-compose.production.yml ps --all \
    > "$RESTORE_LOG_DIR/business-ps.txt" 2>&1 || true
  docker compose -p hmaigc-restore --env-file .env.restore \
    -f docker-compose.production.yml logs --no-color --no-log-prefix backend web \
    > "$RESTORE_LOG_DIR/backend-web.log" 2>&1 || true
  exit 1
fi
```

把 `vX.Y.Z` 替换为目标发布标签；命令中的三个 `grep` 值也必须与 `.env.restore` 实际设置完全一致。隔离 ops-state 只为隔离后端提供独立 socket/共享密钥，不得接入生产 ops-state，也不得在演练中提交发布任务。若 ops-controller/backend 非零退出或 backend 未达到 healthy，立即保存对应 Compose 项目的 `ps` 与完整日志，不得启动 Web 或改用旧镜像继续验收。`/var/tmp` 证据目录只允许交给授权运维人员，问题关闭后按本机留存策略处理，不得提交 Git。随后验证管理员登录、会员、钱包、模型目录、任务与资源文件。
6. 核对表数量、核心业务记录数量和文件校验值后，恢复演练才算完成。

隔离后端必须使用目标版本镜像启动并等待健康检查；这个启动动作仍是支付完整性等写迁移的唯一真实预检，不存在通用的“只看不改”伪 dry-run。目标镜像自带的 Agent Runtime 命令只读审计旧活跃 Run 的升级兼容性，不执行 schema migration，也不能证明支付迁移或目标后端必然启动成功。迁移非零退出时保留目标容器日志、隔离 PostgreSQL 与恢复归档，核对冲突事实后重新创建隔离恢复点演练。若恢复验证失败，不得转而覆盖生产环境尝试。

## 5. 升级与回滚

升级前：

1. 完成第 3 节的同一恢复点备份。
2. 记录当前 Git 提交和镜像摘要。
3. 在隔离环境验证新版本构建、数据库迁移和关键业务回归。

升级时：

```bash
TARGET_VERSION='vX.Y.Z'
bash deploy/hmaigc-ops.sh upgrade "$TARGET_VERSION"
```

工具先拉取新镜像，并在当前服务在线时运行目标镜像的全活跃 Run 只读升级审计；失败时当前版本保持在线且不创建新备份。首次通过后停止 Web 与后端写入，再对静止数据库运行同一审计；失败时恢复当前版本，不创建新备份或启动目标后端。两次审计均通过后才创建 PostgreSQL 与 `backend-data` 同一恢复点，并启动目标后端执行正式原子迁移、依赖检查和版本校验；全部通过后才启动 Web。目标启动或验活失败会在 Web 重新开放前自动恢复上一版本的数据与镜像。

人工主动回滚：

```bash
bash deploy/hmaigc-ops.sh rollback
```

回滚会覆盖升级恢复点之后产生的数据；命令执行前会先为当前版本生成安全恢复点。禁止只回滚代码而保留不兼容的数据状态。

## 6. 禁止操作

- 禁止在生产执行 `docker compose down -v`。
- 禁止删除或复用未核验的 PostgreSQL、Redis、`backend-data` 数据卷。
- 禁止把 `.env`、数据库归档、上传文件、日志或密钥提交到 Git。
- 禁止在没有备份恢复证据时执行数据库迁移或版本升级。
- 禁止把 PostgreSQL、Redis 或后端服务直接暴露到公网。
- 禁止把 Docker socket 挂载到业务后端，或绕过独立控制器直接从后台进程执行部署脚本。
