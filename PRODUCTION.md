# HMaigc 生产运行手册

本手册适用于单机 Docker Compose 部署。生产数据由 PostgreSQL、`backend-data` 数据卷和 Redis 共同承载，其中 PostgreSQL 与 `backend-data` 必须作为同一恢复点备份；Redis 可重建，但重建期间并发协调和实时协作会中断。

## 1. 上线前检查

```bash
docker compose --env-file .env -f docker-compose.production.yml config -q
cd backend && go test ./...
cd ../web && bun install --frozen-lockfile && bun test && bun run build
```

必须确认：

- `.env` 未被 Git 跟踪，`POSTGRES_PASSWORD` 为独立随机强密码。
- `CANVAS_CORS_ORIGINS` 只包含实际 HTTPS 站点 Origin。
- `CANVAS_HTTP_HOST=127.0.0.1`，只有反向代理对公网开放。
- PostgreSQL、Redis 和后端端口没有映射到公网。
- 支付、模型渠道和邮件密钥只在后台或服务器环境中配置。
- 已取得适用的 tldraw 商业许可，并完成 AGPL 与第三方模型条款审查。

## 2. 启动与健康检查

```bash
docker compose --env-file .env -f docker-compose.production.yml up -d --build --wait
docker compose --env-file .env -f docker-compose.production.yml ps
curl --fail http://127.0.0.1:3000/api/health
```

`/api/health` 会实时检查 PostgreSQL 与 Redis。返回非 200 时禁止继续发布流量。

## 3. 同一恢复点备份

以下命令应在服务器仓库根目录执行。备份目录必须位于加密磁盘，并同步到独立存储。

```bash
stamp="$(date +%Y%m%d-%H%M%S)"
backup_dir="$(pwd)/backups/$stamp"
mkdir -p "$backup_dir"

docker compose --env-file .env -f docker-compose.production.yml exec -T postgres \
  sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' \
  > "$backup_dir/postgres.dump"

docker run --rm \
  --mount type=volume,src=hmaigc_backend-data,dst=/source,readonly \
  --mount type=bind,src="$backup_dir",dst=/backup \
  alpine:3.22 \
  tar -czf /backup/backend-data.tgz -C /source .

sha256sum "$backup_dir/postgres.dump" "$backup_dir/backend-data.tgz" \
  > "$backup_dir/SHA256SUMS"
```

备份成功必须同时满足：

- 两个归档文件均存在且非空。
- `sha256sum -c SHA256SUMS` 通过。
- `pg_restore --list postgres.dump` 能读取归档目录。

## 4. 恢复演练

恢复必须先在隔离项目中验证，禁止直接覆盖生产数据库。

1. 复制 `.env` 为隔离配置，修改 HTTP 端口和数据库密码。
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
docker compose --env-file .env -f docker-compose.production.yml up -d --build --wait
curl --fail http://127.0.0.1:3000/api/health
```

如新版本失败，停止流量，恢复上一提交构建的镜像；若数据库结构或数据已经变化，必须按同一恢复点同时恢复 PostgreSQL 与 `backend-data`。禁止只回滚代码而保留不兼容的数据状态。

## 6. 禁止操作

- 禁止在生产执行 `docker compose down -v`。
- 禁止删除或复用未核验的 PostgreSQL、Redis、`backend-data` 数据卷。
- 禁止把 `.env`、数据库归档、上传文件、日志或密钥提交到 Git。
- 禁止在没有备份恢复证据时执行数据库迁移或版本升级。
- 禁止把 PostgreSQL、Redis 或后端服务直接暴露到公网。
