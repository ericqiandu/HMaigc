# HMaigc 生产运行手册

生产采用“Git 标签 → GitHub Actions → SSH → 主机事务脚本”的单一路径。服务器运行预构建的不可变 Backend/Web 镜像，不拉源码、不现场构建；应用后台不承担部署控制面职责。

## 1. 上线门禁

本地或 CI 至少执行：

```bash
bash scripts/tests/validate-source-deploy-workflow.test.sh
bash scripts/tests/validate-production-compose.test.sh
bash deploy/tests/hmaigc-smoke.sh

cd backend
go test ./...
go test -race ./...
go vet ./...
go build ./...

cd ../web
bun install --frozen-lockfile
bun test
bun x tsc --noEmit
bun run build
```

发布前必须确认：

- `VERSION`、标签和 `CHANGELOG.md` 版本一致。
- GitHub `production` Environment 已配置审批、SSH 密钥、固定 host key、端口和专用部署根目录。
- `docker-compose.production.yml` 不包含 ops socket、shared-secret 或 `ops-state` 挂载。
- `production.env` 权限为 `0600`，生产环境和 CORS Origin 均显式配置。
- Backend/Web 为精确摘要，PostgreSQL、Redis 和 Backend 没有直接暴露公网。
- Web 镜像内同源启动资源、HTML 重新验证策略和哈希资源长期缓存通过验证。
- 支付、模型渠道、邮件和 OSS 密钥只存在于受保护的服务器配置或 Secrets 中。
- 当前没有跨越供应商、计费、资产写入或数据库迁移边界的运行任务。

## 2. 发布

日常发布不登录服务器执行源码命令：

1. 合并已验证的单一职责提交。
2. 创建并推送与 `VERSION` 一致的受保护标签。
3. 在 Actions 中观察质量门禁、同源 Web 发布包、镜像发布和 `deploy-production`。
4. production 审批人核对提交、目标版本和回滚点后批准。
5. 流水线传输并校验版本化 bundle，再由主机 release runner 脱离 SSH 执行 `deploy/hmaigc.sh upgrade <tag>`。
6. 成功后 runner 原子更新 `shared/deploy-state/current-release` 并输出 `status`。

流水线失败就是发布失败。禁止绕过失败步骤、改成服务器 `git pull`、用可变标签启动，或从后台再创建并行部署任务。

## 3. 发布证据

每次生产发布保留：

- 标签、Git 提交和 GitHub Environment 审批记录。
- Actions 各门禁与部署日志。
- 版本化 bundle 及其 `bundle.sha256`。
- `shared/production.env` 中的业务配置和首次切换引导摘要（它不是动态版本事实源）。
- `shared/deploy-state/state/release.env` 中的当前/上一版本、对应不可变镜像摘要和恢复点引用。
- `shared/deploy-state/backups/<timestamp>--<version>` 的归档与校验清单。
- `shared/deploy-state/actions/<version>` 的主机发布日志、PID 与耐久退出结果。
- 发布后 `/api/health`、`/canvas/` 和核心业务回归结果。

## 4. 健康检查

获授权的生产操作者可以只读检查：

```bash
export HMAIGC_ENV_FILE=/srv/hmaigc/shared/production.env
export HMAIGC_DEPLOY_STATE_DIR=/srv/hmaigc/shared/deploy-state
export HMAIGC_STATE_DIR=/srv/hmaigc/shared/deploy-state/state
export HMAIGC_BACKUP_DIR=/srv/hmaigc/shared/deploy-state/backups

cd "$(cat /srv/hmaigc/shared/deploy-state/current-release)"
bash deploy/hmaigc.sh status
bash deploy/hmaigc.sh verify
```

`verify` 必须有界完成并明确报告版本、依赖、当前 Web 容器入口和当前版本同源启动资源。生产域名验收由发布流水线的外部回归负责，不能用本机成功替代公网成功事实。

## 5. 同一恢复点备份

```bash
cd "$(cat /srv/hmaigc/shared/deploy-state/current-release)"
bash deploy/hmaigc.sh backup
```

备份成功必须同时满足：

- PostgreSQL dump 与 `backend-data` 归档都存在且非空。
- `sha256sum -c SHA256SUMS` 通过。
- `pg_restore --list postgres.dump` 可读取。
- `manifest.env` 记录版本、卷身份和创建时间；对应不可变镜像摘要由同一原子发布状态 `state/release.env` 保存。

升级脚本在停写后自动创建同类恢复点。备份失败不得继续启动目标版本。

`HMAIGC_BACKUP_RETENTION` 只清理未被当前发布状态引用的恢复点。升级、回滚或手动备份执行期间，`state/release.env` 当前引用的一键回滚点始终受保护；新状态原子提交后才允许清理被替代的旧恢复点。因此配置正整数时，受保护恢复点可能使实际目录数暂时比该数值多 1。

## 6. 回滚

回滚是高风险生产操作，必须重新确认目标恢复点和影响窗口：

```bash
cd "$(cat /srv/hmaigc/shared/deploy-state/current-release)"
bash deploy/hmaigc.sh rollback
```

回滚会先为当前版本创建安全恢复点，再恢复上一恢复点并使用已记录的上一版本摘要启动。任一恢复、启动、验活或提交步骤失败，脚本必须尝试恢复回滚前状态并明确报错。

禁止手工改数据库版本、只恢复 PostgreSQL 不恢复 `backend-data`、删除失败恢复点，或重新解析旧标签代替已持久化摘要。

## 7. 隔离恢复演练

至少在首次硬切换和重大数据库变更前执行：

1. 复制恢复点到隔离主机或隔离 Compose 项目。
2. 校验全部 checksum。
3. 恢复 PostgreSQL 和 `backend-data`。
4. 使用清单中的精确镜像摘要启动。
5. 验证登录、画布、资源读取、模型目录、账务查询和 Agent 历史事实。
6. 销毁隔离环境前保存演练时间、恢复点、版本和结果；不得触碰生产卷。

## 8. 故障处理

- 当前版本不健康：停止升级，先恢复线上健康事实。
- Agent Runtime 审计有 blocker：保持当前版本，处理真实运行事实后重试。
- SSH/host key 错误：修复 GitHub Environment 配置，禁止关闭严格校验。
- bundle 摘要不一致：停止发布；同一标签不得对应不同内容。
- 目标版本失败且自动恢复成功：线上保持旧版本，基于 Actions 与主机日志修复后发布新标签。
- 自动恢复失败：冻结现场，保留容器状态、日志、生产环境文件、发布状态与备份，不得继续点击或重复执行。

旧 `ops-controller` 数据只作为历史证据保留，不再作为当前发布状态或恢复门禁。后续清理必须作为独立高风险任务重新授权。

## 9. 安全边界

- SSH 禁止口令登录和 `StrictHostKeyChecking=no`。
- 部署用户、目录、Docker 权限遵循最小权限。
- 不在 Actions 输出私钥、生产环境文件、支付 bearer、供应商密钥或用户媒体 URL。
- 支付回调、OAuth、CORS、HTTPS 与用户媒体 CDN 配置变更必须在发布前单独验收。
- 任何失败必须保留明确错误和证据，不得以默认值、静默跳过或前端成功状态掩盖。
