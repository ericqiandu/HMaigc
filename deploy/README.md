# HMaigc 一键发布

本目录只服务单机 Linux + Docker Compose 生产环境。发布单位是 GitHub Container Registry 中的不可变 `vX.Y.Z` 镜像标签，不在生产服务器现场编译源码，也不使用 `latest` 作为可回滚版本。

## 首次准备

1. 安装 Docker Engine、Docker Compose 插件、`curl`、`tar`、`sha256sum` 和 `flock`。
2. 在 GitHub 仓库中配置 `VITE_TLDRAW_LICENSE_KEY`，推送与根目录 `VERSION` 完全一致的 `vX.Y.Z` 标签，等待镜像工作流通过。
3. 私有 GHCR 包需先执行一次 `docker login ghcr.io`。
4. 在服务器仓库根目录执行：

```bash
cp .env.production.example .env.production
chmod 600 .env.production
```

填写真实镜像仓库、版本、数据库密码、HTTPS Origin 和本机监听端口。卷名启用后不得随意改变，否则部署工具会找不到生产恢复点。

外层 Nginx 可参考 `deploy/nginx/hmaigc.conf.example`，替换域名后再由服务器现有证书方案启用 HTTPS。PostgreSQL、Redis 和后端均不得映射到公网。

## 一条命令操作

```bash
# 首次安装
./deploy/hmaigc.sh install v1.0.10

# 升级：拉取镜像、停写、创建一致性备份、先验活后端、再启动 Web
./deploy/hmaigc.sh upgrade v1.0.11

# 回滚：恢复上一版本代码、PostgreSQL 和 backend-data 同一恢复点
./deploy/hmaigc.sh rollback

# 独立备份、验收与状态
./deploy/hmaigc.sh backup
./deploy/hmaigc.sh verify
./deploy/hmaigc.sh status
```

升级失败会在 Web 重新开放前自动恢复升级前的数据与版本。回滚前也会先为当前版本创建安全恢复点；若目标旧版本验收失败，会恢复到回滚前状态。

## 数据与审计

- 状态、锁和操作日志默认写入 `.deploy/`，备份默认写入 `.deploy/backups/`，均被 Git 忽略。
- 每个恢复点包含 PostgreSQL custom dump、`backend-data` 压缩包、版本清单和 SHA-256 校验文件。
- 备份归档本身不负责加密；生产环境必须把 `HMAIGC_BACKUP_DIR` 指向独立的加密磁盘，并复制到异地对象存储。
- 默认不自动删除任何备份。只有在 `.env.production` 显式设置正整数 `HMAIGC_BACKUP_RETENTION` 后，工具才会删除超出数量的最旧恢复点。
- Redis 是可重建的协调层，不进入业务恢复点；升级与回滚会重新启动 Redis，但不会把 Redis 当作业务事实源。
- 回滚会覆盖目标恢复点之后产生的数据。执行 `rollback` 命令本身即表示运维人员确认这一影响。

## 发布前提

一键发布只解决可重复部署和回滚，不替代发布质量门禁。版本标签必须先通过 GitHub Actions 中的 Go 全量测试、Web 测试、生产构建和 Compose 校验。涉及不可逆数据库变更时，即使脚本具备备份恢复能力，也必须先在隔离环境完成恢复演练。
