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

## 生产部署

生产服务器必须安装 Docker Engine 与 Docker Compose。先从私有仓库取得代码，然后创建生产配置：

```bash
cp .env.example .env
openssl rand -hex 32
```

把生成结果写入 `.env` 的 `POSTGRES_PASSWORD`，并至少配置：

- `CANVAS_CORS_ORIGINS`：实际 HTTPS 站点 Origin。
- `CANVAS_HTTP_HOST`：有反向代理时保持 `127.0.0.1`。
- `CANVAS_HTTP_PORT`：反向代理连接的本机端口。
- `VITE_TLDRAW_LICENSE_KEY`：获得正式商业许可后填写。

启动：

```bash
docker compose --env-file .env -f docker-compose.production.yml up -d --build --wait
docker compose --env-file .env -f docker-compose.production.yml ps
```

生产环境应由 Caddy、Nginx 或云负载均衡器提供 HTTPS。不要直接把后端、PostgreSQL 或 Redis 暴露到公网。

备份、恢复演练、升级与回滚必须遵循 [生产运行手册](PRODUCTION.md)。

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
- 升级前同时备份 PostgreSQL 与 `backend-data`，再构建新镜像并执行回归。
- 禁止把 `.env`、数据库、日志、备份、上传文件或真实密钥提交到仓库。
- 页面路由名称不得复用于 `web/public` 静态目录；画布人脸模型统一位于 `/runtime-assets/canvas-models/`，避免静态目录抢占 `/canvas` 页面路由。

## 许可证与上游

本项目基于 `ddcat-ai/open-ai-canvas` 与 `basketikun/infinite-canvas` 继续开发。上游署名和版权信息保留在 [NOTICE](NOTICE) 中。

当前代码采用 [GNU Affero General Public License v3.0](LICENSE)。通过网络向用户提供修改后的程序时，需要履行 AGPL-3.0 对应的源码提供义务；正式商业上线前应由法务确认开源合规、第三方模型条款、素材版权和 tldraw 商业许可。
