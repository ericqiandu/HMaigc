# HMaigc 生产运行手册

本手册适用于单机 Docker Compose 部署。生产数据由 PostgreSQL、`backend-data` 数据卷和 Redis 共同承载，其中 PostgreSQL 与 `backend-data` 必须作为同一恢复点备份；Redis 可重建，但重建期间并发协调和实时协作会中断。

## 1. 上线前检查

```bash
docker compose --env-file .env.production -f docker-compose.production.yml config -q
docker compose --env-file .env.production -f deploy/docker-compose.ops.yml config -q
cd backend && go test ./...
cd ../web && bun install --frozen-lockfile && bun test && bun run build
cd .. && bun scripts/verify-spa-routes.mjs http://127.0.0.1:3000
```

必须确认：

- `.env.production` 未被 Git 跟踪且权限为 `600`，`POSTGRES_PASSWORD` 为独立随机强密码。
- `HMAIGC_VERSION` 是与根目录 `VERSION` 一致的不可变 `vX.Y.Z` 标签，禁止使用 `latest`。
- `HMAIGC_OPS_VERSION` 是独立控制器的不可变标签，`HMAIGC_OPS_STATE_VOLUME` 已纳入加密备份。
- 后端容器没有 Docker socket；只有独立控制器容器可以访问 Docker Engine。
- `CANVAS_CORS_ORIGINS` 只包含实际 HTTPS 站点 Origin。
- `CANVAS_HTTP_HOST=127.0.0.1`，只有反向代理对公网开放。
- PostgreSQL、Redis 和后端端口没有映射到公网。
- 支付、模型渠道和邮件密钥只在后台或服务器环境中配置。
- 已完成 AGPL、第三方模型条款和生产依赖许可证审查。

## 2. 启动与健康检查

```bash
bash deploy/hmaigc-ops.sh install v1.0.13
bash deploy/hmaigc-ops.sh verify
```

`/api/health` 会实时检查 PostgreSQL 与 Redis，并返回镜像编译时注入的版本和提交；部署工具必须确认实际运行版本与目标标签一致。`/canvas/` 检查用于确认 Nginx 没有把 SPA 页面路由误判成静态目录。任一检查失败时禁止继续发布流量。

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
bash deploy/hmaigc-ops.sh upgrade v1.0.13
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
