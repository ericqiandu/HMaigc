---
name: open-canvas
description: 打开 HMaigc 网页画布并自动连接本地 Canvas Agent。用户要求打开、启动、进入、使用 HMaigc 或画布时使用。
---

# 打开 HMaigc 画布

当用户要求打开、启动、进入或使用 HMaigc 时，先确认当前仓库和服务身份，再连接本地 Canvas Agent。不要把 URL、token 或 JSON 交给用户手工拼装。

## 画布地址

- 新建画布：`<站点地址>/canvas?mode=new&agentUrl=<Local URL>&agentToken=<Connect token>`
- 最近画布：`<站点地址>/canvas?mode=recent&agentUrl=<Local URL>&agentToken=<Connect token>`
- 用户选择：`<站点地址>/canvas?mode=choose&agentUrl=<Local URL>&agentToken=<Connect token>`

## 工作流

1. 确认当前仓库包含 `docker-compose.yml`、`web/` 和 `canvas-agent/`。
2. 检查 `http://localhost:3000/api/health`；端口存在但健康接口或项目身份不符时必须显式失败。
3. 本地服务未运行时，在仓库根目录执行 `docker compose up -d --build --wait`。
4. 确认 `canvas-agent/node_modules` 已安装；缺失时提示先执行 `cd canvas-agent && bun install --frozen-lockfile`，不下载上游 npm 包。
5. 从 `canvas-agent/` 启动 `CANVAS_URL=<真实站点地址> bun run src/index.ts`。
6. 从 Agent 输出或本机配置读取 `Local URL` 和 token。
7. 构造最终画布 URL 并直接打开；不要通过页面模拟点击新建画布。
8. 使用 `canvas_get_state` 验证连接。连接失败时返回真实错误，不切换线上站点或其他 Agent。

本插件的 MCP 技术标识仍为 `infinite-canvas`，但所有运行代码必须来自当前 HMaigc 仓库。
