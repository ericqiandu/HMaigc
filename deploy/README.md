# HMaigc 源码驱动生产发布

本目录服务单机 Linux + Docker Compose 生产环境。生产发布只有一条写路径：受保护的 Git 标签触发 GitHub Actions，流水线完成质量门禁、同源 Web 发布包和 Backend/Web 不可变镜像构建后，通过严格校验主机密钥的 SSH 把版本化部署包传到生产服务器，并执行仓库内的 `deploy/hmaigc.sh`。

服务器不执行 `git pull`、不现场编译源码，也不运行独立运维控制器。后台不再提供升级、回滚、停止或恢复入口；主机 release runner 会让升级脱离 SSH 会话继续运行，Actions 只跟随同一版本的持久日志和结果。

## 不变量

- 只有已发布标签可以进入生产，标签必须符合 `vX.Y.Z` 语义版本格式。
- Backend 和 Web 只使用 `repository@sha256:<64-hex>`，禁止 `latest` 或可变标签作为运行事实。
- Actions `concurrency` 与主机 `flock` 同时保证单发布。
- SSH 断线不会终止主机发布；重连只附着同一版本 operation，不会启动第二次升级。
- 当前版本验活、Agent Runtime 在线审计或停写审计失败时，不进入数据切换。
- 升级前创建并校验 PostgreSQL 与 `backend-data` 同一恢复点。
- 目标版本失败时，必须用升级前捕获的当前镜像摘要和恢复点恢复，不重新联网猜测旧版本。
- 备份保留策略不得删除 `state/release.env` 当前引用的有效回滚点；新发布状态原子提交后，才允许清理被替代的恢复点。
- `production.env` 只保存业务配置和首次切换的引导摘要；成功升级或回滚后，动态版本事实只通过单文件原子替换写入发布状态。
- 旧 `ops-controller`、旧 `ops-state` 和审计库只读保留，验收完成前不得删除。

## GitHub production Environment

仓库必须创建受保护的 `production` Environment，并按组织发布策略配置审批人。配置以下值：

Secrets：

- `HMAIGC_PRODUCTION_HOST`
- `HMAIGC_PRODUCTION_USER`
- `HMAIGC_PRODUCTION_SSH_KEY`
- `HMAIGC_PRODUCTION_SSH_HOST_KEY`：由可信通道预先取得的完整 `known_hosts` 行，禁止在流水线现场接受未知主机。

Variables：

- `HMAIGC_PRODUCTION_PORT`：默认 `22`。
- `HMAIGC_PRODUCTION_DEPLOY_ROOT`：专用绝对目录，例如 `/srv/hmaigc`。

SSH 用户必须仅具备部署目录和 Docker Compose 所需权限；若 GHCR 包为私有，必须预先为该用户配置只读拉取凭证。

## 主机目录

```text
/srv/hmaigc/
├── releases/
│   └── vX.Y.Z/
│       ├── bundle.sha256
│       ├── docker-compose.production.yml
│       └── deploy/
├── staging/
└── shared/
    ├── production.env
    └── deploy-state/
        ├── current-release
        ├── deploy.lock
        ├── actions/
        ├── state/
        └── backups/
```

`shared/production.env` 权限必须为 `0600`。备份 helper 只挂载 `shared/deploy-state/backups`，不得读取包含业务密钥的 `production.env` 或其他部署状态；部署状态与备份不依赖控制器 Unix Socket 或命名 `ops-state` 卷。

## 一次性硬切换

硬切换属于生产变更，执行前必须单独确认目标目录、当前线上版本和镜像摘要。

1. 确认当前业务版本健康，且没有正在跨越供应商、计费、资产写入或数据库迁移边界的任务。
2. 创建 `<deploy-root>/shared/production.env`，从现有生产配置复制业务参数，删除全部 `HMAIGC_OPS_*` 控制器参数。
3. 设置：

```dotenv
HMAIGC_DEPLOY_STATE_DIR=/srv/hmaigc/shared/deploy-state
HMAIGC_STATE_DIR=/srv/hmaigc/shared/deploy-state/state
HMAIGC_BACKUP_DIR=/srv/hmaigc/shared/deploy-state/backups
HMAIGC_VERSION=vX.Y.Z
HMAIGC_BACKEND_IMAGE=ghcr.io/owner/hmaigc-backend@sha256:<digest>
HMAIGC_WEB_IMAGE=ghcr.io/owner/hmaigc-web@sha256:<digest>
```

4. 初始化 `shared/deploy-state/state/release.env`；`CURRENT_VERSION` 必须等于线上 `/api/health` 返回版本：

```dotenv
CURRENT_VERSION=vX.Y.Z
CURRENT_BACKEND_IMAGE=ghcr.io/owner/hmaigc-backend@sha256:<digest>
CURRENT_WEB_IMAGE=ghcr.io/owner/hmaigc-web@sha256:<digest>
PREVIOUS_VERSION=
PREVIOUS_BACKEND_IMAGE=
PREVIOUS_WEB_IMAGE=
ROLLBACK_BACKUP=
UPDATED_AT=2026-01-01T00:00:00Z
```
5. 停止旧运维控制器，但保留其容器 inspect、数据库、Journal、卷和备份证据。
6. 在当前受版本控制的部署包中运行 `status`、`verify`；只有都通过后，才允许下一个标签发布。

## 日常发布

1. 完成代码审查、测试和版本说明。
2. 更新 `VERSION` 与 `CHANGELOG.md`，创建并推送受保护标签。
3. GitHub Actions 自动完成质量门禁、同源 Web 发布包、镜像和生产部署。
4. `production` Environment 要求审批时，由审批人核对目标标签、提交、变更和回滚点后批准。
5. 流水线成功后核对 `/api/health`、`/canvas/`、核心业务请求与恢复点。

不要在服务器执行 `git pull`、复制源码目录、手改运行版本，或从后台创建第二条部署任务。

## 主机诊断与应急命令

以下命令只在生产终端由获授权人员执行，并使用当前版本部署包：

```bash
export HMAIGC_ENV_FILE=/srv/hmaigc/shared/production.env
export HMAIGC_DEPLOY_STATE_DIR=/srv/hmaigc/shared/deploy-state
export HMAIGC_STATE_DIR=/srv/hmaigc/shared/deploy-state/state
export HMAIGC_BACKUP_DIR=/srv/hmaigc/shared/deploy-state/backups

cd "$(cat /srv/hmaigc/shared/deploy-state/current-release)"
bash deploy/hmaigc.sh status
bash deploy/hmaigc.sh verify
bash deploy/hmaigc.sh backup
bash deploy/hmaigc.sh rollback
```

手工 `upgrade` 只用于流水线故障后的受控应急，必须使用已经通过同一发布门禁的版本化 bundle，并保留完整命令、操作者和输出证据。

## 失败边界

- 质量门禁、Web 发布包或镜像构建失败：不会建立生产 SSH 会话。
- SSH、host key、目录或 bundle 校验失败：不会执行部署脚本。
- 当前版本验活失败：拒绝升级。
- 在线审计失败：零停写、零备份。
- 停写审计或备份失败：恢复当前服务并明确失败。
- 目标版本验活失败：自动恢复升级前数据和不可变镜像摘要。
- 自动恢复失败：保留明确失败、日志、状态和备份；禁止伪报成功。

## Web 程序资源与用户媒体

正式标签只构建一次根路径 Web `dist`，经工作流校验后直接复制进不可变 Web 镜像。HTML、JS、CSS、字体和程序图片由同一 Web Origin 返回；哈希资源使用长期不可变缓存，HTML 使用重新验证缓存。生产 CSP 不允许 JS、CSS、字体或 Worker 从外部静态域名执行，发布不再依赖 OSS 上传、CDN 预热或 CDN 清单遍历。

用户上传及模型生成的图片、视频、音频仍由后端对象存储契约管理。用户媒体可以继续使用独立 OSS Bucket/CDN 域名和签名访问，但不得与 Web 程序资源发布路径混用；本次切换不迁移、不复制也不删除任何用户媒体对象。
