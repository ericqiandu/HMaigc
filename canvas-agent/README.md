# HMaigc Canvas Agent

`canvas-agent` 是 HMaigc 画布“本机”模式的回环服务。它在用户电脑上调用已登录的 Codex，接收语义规划和工具提案；所有画布写入、资产发布、媒体生成、审批、计费、幂等与审计仍由 HMaigc 后端执行。

本机服务不是第二套执行器，也不会读取 HMaigc Cookie、网站登录态、模型渠道密钥或 OSS 凭据。网站模式与本机模式必须由用户显式切换，连接失败时不会自动回退到另一模式。

## 环境要求

- Node.js 22 或更高版本。
- Bun。
- 本机 Codex 已登录，并且配置中指定的模型可用。
- 当前画布已配置一个存在的绝对工作目录。

## 构建

在仓库根目录执行：

```powershell
bun --cwd canvas-agent install
bun --cwd canvas-agent run build
bun --cwd canvas-agent run test
```

## 配置

配置文件固定为：

- Windows：`%USERPROFILE%\.hmaigc\canvas-agent\config.json`
- Linux/macOS：`~/.hmaigc/canvas-agent/config.json`

先生成至少 256 bit 的随机令牌：

```powershell
node -e "console.log(require('node:crypto').randomBytes(32).toString('base64url'))"
```

创建配置文件。`allowedOrigins` 只能填写将要打开 HMaigc 的精确 Origin，不能使用 `*`、`null`、路径或通配域名；`canvases` 的键是画布页面使用的项目/画布身份，工作区必须是本机绝对路径。

```json
{
    "url": "http://127.0.0.1:17371",
    "token": "替换为刚生成的随机令牌",
    "allowedOrigins": ["http://127.0.0.1:3000", "https://hm.kunagent.com"],
    "allowedAttachmentOrigins": ["https://hm.kunagent.com"],
    "codex": {
        "model": "gpt-5.6-sol",
        "modelReasoningEffort": "high"
    },
    "canvases": {
        "替换为当前画布身份": {
            "workspaceRoot": "E:\\path\\to\\workspace"
        }
    }
}
```

非 Windows 系统会强制要求配置文件权限为 `0600`：

```bash
chmod 600 ~/.hmaigc/canvas-agent/config.json
```

附件下载只允许配置中的公网 HTTPS Origin，单个附件最大 25MB、每轮总计最大 30MB，并在 turn 结束后删除临时文件。localhost、内网地址、未列入 allowlist 的重定向和 MIME 不一致都会显式失败。

## 启动与连接

启动服务：

```powershell
node canvas-agent/dist/index.js serve
```

启动成功会在标准输出记录 `canvas_agent_listening`，默认地址是 `http://127.0.0.1:17371`。随后：

1. 打开对应 HMaigc 画布的 Agent 面板。
2. 显式选择“本机”。
3. 粘贴回环地址和配置文件中的令牌。
4. 点击“连接”。连接成功后才会读取本机 thread 列表。

浏览器只把令牌保存在当前标签会话的 `sessionStorage`，请求通过 `X-HMaigc-Agent-Token` header 发送；令牌不得放入 URL、日志或长期 `localStorage`。

## 运行与计费边界

- 网站模式：HMaigc 托管 Agent 的文本推理通过网站模型渠道计费。
- 本机模式：Codex 推理由用户本机 Codex 身份承担，不记入 HMaigc 文本模型账单。
- 两种模式都只使用后端发布的动态模型目录；本机 Agent 不能选择隐藏或未定价模型。
- `canvas.apply_ops`、`assets.publish` 和 `media.generate` 的每一个不可变提案都必须单独确认。
- 人工审批有效期为 15 分钟；回环 MCP 连接会发送心跳，并额外保留 1 分钟结果交付余量，避免网页审批仍有效而本机 Codex 已提前超时。
- `media.generate` 仍由 HMaigc 后端冻结模型、价格和积分报价，并负责预留、结算、释放、退款与审计；批准前不会创建付费媒体任务。
- 媒体请求必须在断线重试时复用原 `clientRequestId` 与 `targetCanvasNodeId`，并保持全部生成参数完全一致。后端会重读原始 Task、账单与 Resource：已成功则直接回放结果且不再次审批/扣费，仍在执行则明确返回“正在运行”，同一请求身份参数不一致则明确报冲突。客户端不得通过生成新请求 ID 绕过这些结果。
- 六种 MCP 工具分别发布与后端规范契约一致的闭合输入 Schema；媒体身份、画布操作联合类型与 Skill 校验字段必须由 Codex 按 Schema 提交，未知字段和缺失字段都会显式失败。
- 网站 Agent 专属的 `vision.analyze` 不在本机 MCP 能力面中；本机 Run 不依赖管理员默认视觉模型配置。
- `canvas_get_state` 的权威结果同时包含当前 `domainProjectId` 与服务器生成的可调用媒体模型事实；`assets_*` 和 `media_generate` 必须复用这些返回值，禁止把 canvas ID 当作项目 ID、猜测模型记录或读取本地静态模型表。
- 回环令牌只通过 Codex MCP 的环境变量白名单传入 MCP 子进程，不写入 Codex MCP 配置值、命令行参数或日志。
- 网站历史与本机 Codex 历史分别保存和展示，不伪造合并历史。
- 后端通用交付校验要求纠偏时，浏览器会在同一 Codex thread 和同一审计 Run 中发起临时续接 turn；该内部纠偏消息与事件不写入本机用户历史，纠偏成功后才释放执行链。

## 撤销或轮换令牌

1. 停止 `canvas-agent` 进程。
2. 生成新的 256 bit 随机令牌并替换配置文件中的 `token`。
3. 重新启动服务。
4. 在画布面板断开旧连接，再使用新令牌连接。

旧令牌不会继续有效。不要通过聊天、截图、URL 或日志分享配置文件内容。

## 故障排查

- `请求 Origin 未获授权`：确认浏览器地址的 `scheme://host:port` 与 `allowedOrigins` 完全一致。
- `Agent token 无效`：重新复制配置文件中的完整令牌；不要附带空格。
- `当前画布未配置本机工作区`：把当前画布身份加入 `canvases`，使用存在的绝对路径，然后重启服务。
- `浏览器事件连接不可用`：同一服务只允许一个活动事件连接；关闭旧标签或先断开旧连接。
- Codex thread 或模型错误：确认 Codex 已登录、模型可用、工作区存在；服务不会静默换模型或创建默认工作区。

## 上游与许可证

本包的 Codex thread 生命周期与本机 Canvas Agent 结构基于 `ddcat-ai/open-ai-canvas` 的 `canvas-agent` 改造，固定来源版本为 `e8c6b5a2d977c96a539923df6e68f37c509b0392`。上游和本包均受 GNU Affero General Public License v3.0 约束；详细归属见仓库根目录 `THIRD_PARTY_NOTICES.md` 与 `LICENSE`。
