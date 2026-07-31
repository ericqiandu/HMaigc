# HMaigc 独立运维控制器与一键发布

本目录只服务单机 Linux + Docker Compose 生产环境。发布单位是 GitHub Container Registry 中的不可变 `vX.Y.Z` 镜像标签，不在生产服务器现场编译源码，也不使用 `latest` 作为可回滚版本。

正式部署由两个独立 Compose 项目组成：

- `hmaigc-ops`：独立运维控制器。它独占 Docker socket、发布锁、备份、发布状态和运维审计数据库。
- `hmaigc`：Web、业务后端、PostgreSQL 与 Redis。业务后端没有 Docker socket，只能通过只读挂载的 Unix socket 和共享密钥向控制器提交已验签请求。

控制器在业务后端停止、升级或启动失败时仍然运行，因此不能把控制器并入业务 Compose，也不能让业务后端直接执行 Docker 命令或重启自身。

## 首次准备

1. 安装 Docker Engine 与 Docker Compose 插件。
2. 推送与根目录 `VERSION` 完全一致的 `vX.Y.Z` 标签，等待镜像工作流通过。
3. 私有 GHCR 包需先执行一次 `docker login ghcr.io`。
4. 在服务器仓库根目录执行：

```bash
cp .env.production.example .env.production
chmod 600 .env.production
```

填写真实镜像仓库、业务版本、控制器版本、数据库密码、HTTPS Origin 和本机监听端口。`HMAIGC_BACKEND_GID` 必须保持与发布镜像中的固定值 `101` 一致。控制器启动时会把只读挂载的生产配置复制到独立运维卷并收紧为 `600`，避免 Docker Desktop 与不同宿主机的 bind mount 权限语义影响发布校验。业务卷和 `HMAIGC_OPS_STATE_VOLUME` 启用后不得随意改变，否则控制器会失去发布状态、审计日志或生产恢复点。

私有 GitHub 仓库若要在后台检查最新 Release，填写只读的 `HMAIGC_RELEASES_API_TOKEN`。不配置时版本检查会明确显示“未配置”，不会假装已经是最新版本。

外层 Nginx 可参考 `deploy/nginx/hmaigc.conf.example`，替换域名后再由服务器现有证书方案启用 HTTPS。PostgreSQL、Redis 和后端均不得映射到公网。

## 唯一正式操作入口

```bash
# 首次安装
bash deploy/hmaigc-ops.sh install v1.0.12

# 状态与环境验收
bash deploy/hmaigc-ops.sh status
bash deploy/hmaigc-ops.sh verify

# 命令行升级、备份和回滚（后台页面也通过同一控制器队列执行）
bash deploy/hmaigc-ops.sh upgrade v1.0.12
bash deploy/hmaigc-ops.sh backup
bash deploy/hmaigc-ops.sh rollback
```

`deploy/hmaigc.sh` 是控制器镜像内部的执行器，不是生产人员的正式入口。`hmaigc-ops.sh` 会先启动独立控制器，再由 `hmaigc-opsctl` 创建任务、持续读取独立审计库中的日志，并等待明确成功或失败。

升级失败会在 Web 重新开放前自动恢复升级前的数据与版本。回滚前也会先为当前版本创建安全恢复点；若目标旧版本验收失败，会恢复到回滚前状态。

后台 `/admin/operations` 提供版本检查、升级、回滚、实时日志、备份状态和审计记录。每次调用都由业务后端重新校验管理员身份；首次安装不会开放给后台页面，只允许服务器命令行引导。

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

一键发布只解决可重复部署和回滚，不替代发布质量门禁。版本标签必须先通过 GitHub Actions 中的 Go 全量测试、Web 测试、生产构建、控制器镜像构建和两个 Compose 文件校验。涉及不可逆数据库变更时，即使脚本具备备份恢复能力，也必须先在隔离环境完成恢复演练。
