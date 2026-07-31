# HMaigc

HMaigc 是面向 AI 影视与短剧生产的商业化创作平台，覆盖项目、章节、角色资产、自由画布、图片/视频/音频生成、团队协作、会员计费、积分运营和管理后台。

## 代码结构

- `web/`：Vite、React、TypeScript 前端。
- `backend/`：Go、Gin、GORM 后端。
- `docker-compose.yml`：使用 SQLite 的本地一体化环境。
- `docker-compose.production.yml`：使用 PostgreSQL 和 Redis 的生产环境。

仓库只保留上述两条运行路径，不再维护旧镜像部署、重复 Compose 或上游一键安装脚本。
画布助手统一使用 Web 内置网站 Agent，并通过后端系统模型渠道完成鉴权、计费和请求审计；不再提供本机 Agent 或 Codex 插件连接路径。

### 首页到 Agent 的创作链路

- 首页创作框先创建真实画布项目，并把提示词与执行模式写入项目内的 `pendingAgentLaunch`；提示词不进入 URL。
- 打开新画布后，Agent 面板只消费一次启动请求。后端会话 ID、执行模式和来源请求 ID 会随聊天会话持久化，刷新页面后继续查询同一任务，不重复创建任务。
- `执行前确认` 模式只允许 Agent 完成方案推演；返回的画布操作会形成待确认卡片，用户批准后才写入节点并触发媒体生成。
- `自动执行` 模式在后端方案完成后直接写回画布。两种模式都只展示后端任务返回的真实阶段与进度，失败会明确记录且不会静默降级。

## 本地开发

```bash
cd backend
go run ./cmd/server

# 另开终端
cd web
bun install --frozen-lockfile
bun run dev
```

也可以直接启动本地 Docker 环境：

```bash
docker compose up -d --build --wait
```

默认访问地址为 `http://localhost:3000`。本地业务数据位于 `.local/data`，不得提交到 Git。

### MiniMax Speech 配置

1. 在管理后台“AI 模型”中新建 `MiniMax Speech` 系统渠道，Base URL 使用 `https://api.minimaxi.com/v1`，API Key 只保存在后端。
2. 为渠道添加实际开通的音频模型和积分价格，例如 `speech-2.8-hd` 或 `speech-2.8-turbo`；用户端模型列表不会使用硬编码候选。
3. 打开“音色管理”，同步供应商音色，或在确认已获得声音本人授权后上传 10 秒至 5 分钟、20MB 以内的 MP3、M4A、WAV 样本进行克隆。
4. 为音色配置兼容模型、会员权限和发布状态。画布只展示后台已发布且与当前模型兼容的音色，任务创建前后端会再次校验权限并按模型价格扣除积分。

MiniMax 同步语音当前支持 MP3、WAV、FLAC，语速范围为 0.5–2.0。克隆源文件不会保存在本系统，只保留文件名、大小、SHA-256 和授权确认时间用于审计。没有真实 API Key 时只能完成本地契约测试，不能将模拟响应视为供应商验收。

## 生产部署

生产服务器必须安装 Docker Engine 与 Docker Compose。先从私有仓库取得代码，然后创建生产配置：

```bash
cp .env.production.example .env.production
chmod 600 .env.production
openssl rand -hex 32
```

把生成结果写入 `.env.production` 的 `POSTGRES_PASSWORD`，并至少配置：

- `HMAIGC_IMAGE_REGISTRY`：例如 `ghcr.io/ericqiandu`。
- `HMAIGC_VERSION`：与 Git 标签一致的不可变版本，例如 `v1.0.13`。
- `HMAIGC_OPS_VERSION`：独立运维控制器的不可变版本。
- `HMAIGC_RELEASES_API_URL`：用于后台检查最新 GitHub Release。
- `CANVAS_CORS_ORIGINS`：实际 HTTPS 站点 Origin。
- `CANVAS_HTTP_HOST`：有反向代理时保持 `127.0.0.1`。
- `CANVAS_HTTP_PORT`：反向代理连接的本机端口。

首次安装：

```bash
bash deploy/hmaigc-ops.sh install v1.0.13
```

生产环境应由 Caddy、Nginx 或云负载均衡器提供 HTTPS。不要直接把后端、PostgreSQL 或 Redis 暴露到公网。

后续升级与回滚：

```bash
bash deploy/hmaigc-ops.sh upgrade v1.0.13
bash deploy/hmaigc-ops.sh rollback
```

业务后端不持有 Docker socket，也不会重启自己；后台运维升级中心和服务器命令行都把任务提交给独立控制器。完整契约见 [独立控制器与一键发布说明](deploy/README.md) 与 [生产运行手册](PRODUCTION.md)。

## 上线门禁

- 在受控网络注册首个管理员后，保持公开注册关闭。
- 后台配置正式模型渠道、供应商预算、模型积分售价和并发策略。
- 配置支付商户资料并完成签名、异步通知、退款、关单和对账验收后，才能开放自动支付。
- 配置站点名称、Logo、版权、用户协议、隐私政策及备案信息。
- 对 PostgreSQL、后端资源卷和 `.settings-key` 建立加密备份与恢复演练。
- 完成前端构建、Go 全量测试、生产镜像构建和关键业务回归。
- 运行 `bun scripts/verify-spa-routes.mjs http://127.0.0.1:3000`，确认首页、画布、项目和管理后台深链接都由当前 SPA 入口提供。
- GitHub Actions 质量门禁通过后才允许发布生产镜像。

## 数据与升级

- PostgreSQL 是生产业务数据真源，Redis 只承担队列和实时广播。
- `backend-data` 保存上传文件及服务端密钥材料；数据库备份不能替代文件备份。
- 发布镜像由 GitHub Actions 构建并以不可变版本标签推送；生产服务器禁止现场构建或使用 `latest` 升级。
- 升级前同时备份 PostgreSQL 与 `backend-data`，新后端完成迁移和版本验活后才允许启动 Web。
- 禁止把 `.env`、数据库、日志、备份、上传文件或真实密钥提交到仓库。
- 页面路由名称不得复用于 `web/public` 静态目录；画布人脸模型统一位于 `/runtime-assets/canvas-models/`，避免静态目录抢占 `/canvas` 页面路由。

## 许可证与上游

本项目基于 `ddcat-ai/open-ai-canvas` 与 `basketikun/infinite-canvas` 继续开发。上游署名和版权信息保留在 [NOTICE](NOTICE) 中。

当前代码采用 [GNU Affero General Public License v3.0](LICENSE)。通过网络向用户提供修改后的程序时，需要履行 AGPL-3.0 对应的源码提供义务；正式商业上线前应由法务确认开源合规、第三方模型条款和素材版权。
