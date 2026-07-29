# HMaigc Codex 插件

该插件让 Codex 通过本地 Canvas Agent 读取和操作当前 HMaigc 画布。

## 安装准备

插件只运行当前仓库代码，不会下载上游 Canvas Agent 包。先安装 Agent 依赖：

```bash
cd canvas-agent
bun install --frozen-lockfile
```

然后从仓库根目录注册并安装本地 marketplace：

```bash
codex plugin marketplace add "$(pwd)"
codex plugin add infinite-canvas@hmaigc-local
```

Windows PowerShell 可直接把 `$(pwd)` 替换为仓库绝对路径。安装后新建 Codex 任务，让技能和 MCP 配置完整加载。

## 使用

常用提示：

```text
打开 HMaigc
读取当前画布并总结节点结构
根据选中节点创建一组生图提示词
```

插件会：

1. 核对本地 HMaigc 服务是否运行。
2. 从当前仓库启动 Canvas Agent。
3. 读取 Agent 的本机地址和连接 token。
4. 打开带连接参数的具体画布。
5. 使用 `infinite-canvas` MCP 工具读取或修改画布。

MCP 技术标识沿用 `infinite-canvas`，但启动入口已经硬切换到当前仓库的 `canvas-agent/src/index.ts`。

## 手动排查

```bash
docker compose up -d --build --wait

cd canvas-agent
CANVAS_URL=http://localhost:3000 bun run src/index.ts
```

不要把 Agent 的本机端口暴露到公网，也不要把 token 写入仓库。
