# HMaigc 生产运行手册

本手册适用于单机 Docker Compose 部署。生产数据由 PostgreSQL、`backend-data` 数据卷和 Redis 共同承载，其中 PostgreSQL 与 `backend-data` 必须作为同一恢复点备份；Redis 可重建，但重建期间并发协调和实时协作会中断。

## 1. 上线前检查

```bash
docker compose --env-file .env.production -f docker-compose.production.yml config -q
docker compose --env-file .env.production -f deploy/docker-compose.ops.yml config -q
cd backend && go test ./...
cd ../web && bun install --frozen-lockfile && bun test && bun run build
cd .. && bash scripts/tests/verify-payment-checkout-nginx.test.sh
bun scripts/verify-spa-routes.mjs http://127.0.0.1:3000
```

必须确认：

- `.env.production` 未被 Git 跟踪且权限为 `600`，`POSTGRES_PASSWORD` 为独立随机强密码。
- `HMAIGC_VERSION` 是与根目录 `VERSION` 一致的不可变 `vX.Y.Z` 标签，禁止使用 `latest`。
- `HMAIGC_OPS_VERSION` 是独立控制器的不可变标签，`HMAIGC_OPS_STATE_VOLUME` 已纳入加密备份。
- 后端容器没有 Docker socket；只有独立控制器容器可以访问 Docker Engine。
- `CANVAS_ENVIRONMENT=production` 已显式配置；缺失值、未知值或其他环境值都会阻止后端启动。
- `CANVAS_CORS_ORIGINS` 只包含实际 HTTPS 站点 Origin。
- 支付收银台地址是无 path、userinfo、query、fragment 的 HTTPS Origin；支付渠道回调地址允许 webhook path，但不得含 userinfo、query 或 fragment。
- `CANVAS_HTTP_HOST=127.0.0.1`，只有反向代理对公网开放。
- PostgreSQL、Redis 和后端端口没有映射到公网。
- 支付、模型渠道和邮件密钥只在后台或服务器环境中配置。
- 已完成 AGPL、第三方模型条款和生产依赖许可证审查。

## 2. 启动与健康检查

```bash
bash deploy/hmaigc-ops.sh install v1.0.14
bash deploy/hmaigc-ops.sh verify
```

`/api/health` 会实时检查 PostgreSQL、Redis 和持久化支付公开 URL 配置，并返回镜像编译时注入的版本和提交。支付运行时校验在 worker 和 HTTP listener 之前执行；生产环境出现 HTTP 收银台/回调地址、非法公开 Origin 或损坏的持久化支付 JSON 时会直接阻止启动，运行中配置异常则使 readiness 返回 `503`。部署工具必须确认实际运行版本与目标标签一致。`/canvas/` 检查用于确认 Nginx 没有把 SPA 页面路由误判成静态目录。任一检查失败时禁止继续发布流量。

外层 Nginx 必须按 `deploy/nginx/hmaigc.conf.example` 配置证书并将 80 端口永久重定向到 HTTPS 443。`/pay/` 和 `/api/payments/checkout/` 是 bearer capability 精确前缀：两层 Nginx 均关闭这两个位置的 access/error log，并强制 `private, no-store`、`no-cache` 和 `no-referrer`；inner Nginx 既有的 `/api/public/canvas-shares/` bearer 前缀继续保持 error-log 隔离。该局部可观测性由后端不含敏感值的结构化错误日志和 `/api/health` 替代。

stock Nginx upstream error 行会原样附带 incoming Referer，无法自定义格式。因此仅在实际 reverse-proxy location 关闭 stock upstream error 行，改由条件 access log 为普通 4xx/5xx 写入 safe proxy-error 事实（时间、客户端、方法、不含 query 的 URI、状态与 upstream status）；普通 2xx access log 同样不含 Referer。server/main/global 的配置、启动与静态文件错误日志继续保留，不得把该替换扩到整个 server。

从旧版本升级前必须先暂停新的下单/checkout 创建，在旧版后台把 checkout base 修正为无 path 的 HTTPS Origin，把全部渠道 notify URL 修正为 HTTPS，并核验生产 CORS Origin。暂停后至少等待最长 `15 分钟` checkout TTL，使旧 HTTP/pathful 加密链接自然失效；不得清库或隐式轮换 bearer。若新版本因支付运行时门禁拒绝启动，应保持发布工具的自动回滚，回到旧版修正持久化配置后重新升级；禁止临时改回 development、HTTP 或关闭门禁。

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

1. 复制 `.env.production` 为隔离配置，修改 Compose 项目名、三个卷名、HTTP 端口和数据库密码。
2. 使用独立项目名启动 PostgreSQL 与 Redis：

```bash
docker compose -p hmaigc-restore --env-file .env.restore \
  -f docker-compose.production.yml up -d postgres redis --wait
```

3. 把 `postgres.dump` 复制进隔离 PostgreSQL 容器，恢复到新建数据库。
4. 使用只读源归档恢复新的 `hmaigc-restore_backend-data` 数据卷。
5. 启动隔离后端和 Web，验证管理员登录、会员、钱包、模型目录、任务与资源文件。
6. 核对表数量、核心业务记录数量和文件校验值后，恢复演练才算完成。

若恢复验证失败，必须保留日志和归档，不得转而覆盖生产环境尝试。

## 5. 升级与回滚

升级前：

1. 完成第 3 节的同一恢复点备份。
2. 记录当前 Git 提交和镜像摘要。
3. 在隔离环境验证新版本构建、数据库迁移和关键业务回归。

升级时：

```bash
bash deploy/hmaigc-ops.sh upgrade v1.0.14
```

工具先拉取新镜像，再停止 Web 与后端写入并创建恢复点；只在新后端完成数据库迁移、依赖检查和版本校验后才启动 Web。任一步失败都会在 Web 重新开放前自动恢复上一版本的 PostgreSQL、`backend-data` 与镜像。

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
