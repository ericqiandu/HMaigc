# HMaigc Canvas Agent

Canvas Agent 连接 HMaigc 网页画布与用户电脑上的 Codex。它只监听 `127.0.0.1`，不应部署为公网服务。

## 安装与启动

```bash
cd canvas-agent
bun install --frozen-lockfile
bun run build
bun run start
```

开发时可以直接运行 TypeScript 源码：

```bash
CANVAS_URL=http://localhost:3000 bun run src/index.ts
```

启动后会输出本机地址与一次性连接信息：

```text
Local URL: http://127.0.0.1:17371
Connect token: xxxxxx
```

网页首次使用正确 token 连接后，Agent 会记录该 Origin；其他 Origin 不能复用同一 Agent。

## Codex MCP

本地源码方式：

```bash
cd canvas-agent
bun install --frozen-lockfile
codex mcp add infinite-canvas -- bun run src/index.ts mcp
```

`infinite-canvas` 是现有 MCP 协议标识，暂时保留以维持工具契约；产品名称和运行代码均为 HMaigc。

可用工具包括读取画布、读取选区、创建文本/图片/视频/音频节点、创建生成流程、批量修改和触发生成。所有网页写操作仍由前端确认。

## Codex 插件

仓库内的 `plugins/infinite-canvas` 插件直接运行当前仓库的 Canvas Agent 源码，不再下载上游 npm 包。安装插件前必须先在 `canvas-agent/` 执行：

```bash
bun install --frozen-lockfile
```

## 安全边界

- Agent 默认仅监听本机回环地址。
- 图片附件只写入操作系统临时目录并作为本地 Codex 输入。
- 网页写操作必须经过确认。
- 不要把 Agent token、配置目录或诊断日志提交到 Git。
- 商业发行前如需发布独立 npm 包，应先确定自有 npm scope，再解除当前 `private` 标记。
