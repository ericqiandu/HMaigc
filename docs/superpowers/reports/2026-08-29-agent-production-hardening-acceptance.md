# Agent 生产级执行加固验收（2026-08-29）

## 验收范围

- 基线：`c11114a`（`v1.0.69`）。
- 验收 HEAD：`9d862bb`（`feat(tasks): close settlement outbox audit`）。
- 后续收口复验 HEAD：`2b415e0`（`fix(media): 统一模型能力与计费契约`）。
- 覆盖 Runtime v4 / Policy v4 / Tool schema v5 / UI protocol v4 / Production schema v2，以及从多 Specialist、Artifact Ledger、逐阶段审核、定向重生成、真实媒体装配到 Task 执行信封、取消/租约 fencing、原子结算和耐久 Outbox 的完整提交链。
- 本报告只记录本次实际执行的门禁和可复核事实；没有运行的真实 PostgreSQL、付费供应商生成或跨进程故障注入不写成成功。

## 契约验收

| 目标 | 实现与证据 | 结论 |
| --- | --- | --- |
| 付费副作用前授权 | Task 执行信封使用规范 JSON 与 HMAC-SHA256，绑定租户、请求人、项目、画布、Run、Artifact revision、Task 身份/类型和载荷摘要；worker 从持久 Task 重读租约后、调用供应商前校验，缺失、篡改、过期、跨作用域重放和尾随/未知 JSON 均拒绝 | 通过 |
| 取消与旧 worker 隔离 | `cancel_requested_at`、`lease_generation`、`lease_token` 与供应商请求身份持久化；取消会推进租约代际，旧 worker 即使复用 owner 也不能写终态；迟到成功只经核对租约保存为未采纳结果 | 通过 |
| 资金与结果原子性 | 成功路径把 Task、BillingOrder、Session、Message、Result 和唤醒 Outbox 放入同一事务；失败/取消路径把退款或 `uncertain`、Task 终态、迟到结果和 Outbox 放入对应事务。Outbox 无法落库时整个终态事务回滚 | 通过 |
| 崩溃恢复与幂等投递 | `task_outboxes` 使用唯一幂等键、状态、尝试次数、可用时间和独立租约；提交后投递，失败重排，处理实例崩溃后可重领；终态 Task 作用域与当前 Run 待处理 Task 身份会再次核验 | 通过 |
| 只展示持久事实 | Agent 文本 delta、工具、审批、Artifact、装配与 Run 状态先持久化 sequence/Item，再经 SSE 投影；停止后的正文 delta 被边界拒绝，历史恢复按服务端 Turn/Item 和 sequence 去重 | 通过 |
| 内部文本 Task 隔离 | Agent 文本 Task 固定为 `internal`；普通用户列表、详情、日志、取消和重试只访问 `customer` Task，管理员、账务与 Runtime 保留内部事实读取 | 通过 |

## 自动化门禁

### Backend

以下命令均在 `backend/` 执行并返回退出码 `0`：

```text
go test ./... -count=1
go test -race ./internal/agentruntime ./internal/repository ./internal/service -count=1
go vet ./...
go build ./...
```

- 全量 Go 测试全部通过；race 门禁覆盖 Agent Runtime、Repository 与 Service。
- M10 定向复核通过：执行信封 canonical/绑定/篡改/过期/重放/密钥选择；租约代际和取消意图；迟到媒体不推进 Artifact head；Task/账务/Result/Outbox 原子提交与回滚；Outbox 崩溃恢复、重复投递、时间线重试、作用域冲突和过期唤醒；Token 供应商费用不确定审计。
- PostgreSQL 专项测试未执行：当前环境没有配置 `DATABASE_URL`。SQLite 事务与全量测试通过不能替代真实 PostgreSQL 锁、隔离级别和并发验收。

### Web

为避免把用户尚未提交的视频能力重构混入 Agent 验收，从 `9d862bb` 导出干净 HEAD 并复用已安装依赖执行：

```text
bun test
bun x tsc --noEmit
bun run build
```

- 干净 HEAD：`708 pass / 0 fail`，150 个测试文件；类型检查与生产构建通过。
- 生产构建预算全部通过：应用入口、首页、项目、技能、画布、后台、FFmpeg 静态闭包和导演台 3D 按需包均在预算内；构建只保留一个超过 900 KiB 的警告，预算检查仍为通过。
- 后续媒体能力与计费契约已由 `2b415e0` 收口；在该 HEAD 重新执行当前共享工作区的完整 Web 门禁为 `741 pass / 0 fail`、`2918 expect()`、156 个测试文件，TypeScript 检查、生产构建与全部 bundle 预算通过。原先的两个动态视频能力契约失败不再存在。

### 静态完整性

- `git diff c11114a..9d862bb --check`：通过。
- 当前工作区 `git diff --check`：通过。
- 未发现密钥、构建产物或本地临时目录进入 Agent 提交。

## 浏览器真实流程证据

本次使用内置浏览器连接本地已登录运行时，没有触发付费模型：

1. 画布 `Q7T5CS18n_TIYaFqCjqSp`：打开 Agent 历史中的已取消 Run，服务端历史恢复了原用户消息与冻结模型配置，状态稳定为“已停止”；刷新恢复期间先显示真实“正在恢复运行事实”，恢复完成后没有继续输出、没有伪造运行中状态。历史选择的 `kz_gpt_image2` 当前不可用于 Agent 时，页面明确报出该事实，没有自动替换模型；浏览器控制台无 error/warn。
2. 画布 `D6gkP85uOI4uC7m573_Dj`：最终视频节点播放器读取真实资源端点，`readyState=4`、`duration=3.041667`、无媒体错误，播放器可见 `0:00 / 0:03`。
3. Web 自动化同时覆盖：引用顺序变化仍保持稳定资产键、节点提示词 `@` mention 编辑、冻结审批身份、单次阶段修改、定向重生成只推进受影响 attempt、真实装配卡合并、停止后丢弃迟到正文、迟到 Artifact 保留但不复活 Run、刷新后按持久 Turn/Item/sequence 重放。

没有执行的浏览器门禁：真实付费图片/视频/音频生成、供应商长流断线、跨进程 worker 强杀、数据库重启、跨设备并发审核。它们会产生真实费用或需要独立故障注入环境，本轮没有新的费用授权，故明确保留为上线前运维验收项。

## 独立审查与定向复审

独立审查按 `c11114a..9d862bb` 对照需求、计划、实际 diff、前后端协议、数据库模型、计费、权限、幂等、取消、恢复、文档和测试逐项核验，重点复读 `taskruntime/envelope.go`、Task claim/finalizer、`task_billing.go`、`agent_task_outbox.go` 和 worker 终态路径。

- 未发现当前交付中的 Critical 或 Important 缺陷，因此没有为了制造“修复轮次”而改动生产代码。
- 定向复审确认：固定价成功结算在原子终态事务内完成，后续通用结算调用只能命中幂等已结算状态；Token usage 成功先进入 `uncertain`，只能由供应商账单核对收口，不能按估算结果直接结算。
- Outbox 投递失败会保留错误并持续重试；其副作用是唤醒可幂等协调器，而不是再次调用供应商。终态 Task 和账务事实不依赖投递成功才可见。
- 数据库新增 `task_outboxes` 由统一 schema 注册并带唯一幂等索引；Task 新增执行信封、取消意图和租约代际字段。上线必须先让目标镜像完成数据库迁移门禁，再启动 worker。

## 验收结论

已执行的代码、类型、构建、race、静态检查和无费用浏览器门禁全部通过；干净提交态不存在已知阻断项。当前结论是“Agent 生产级执行加固代码可进入下一发布候选”，不是“真实付费供应商和 PostgreSQL 故障演练已完成”。上线前仍必须在受控生产副本完成 PostgreSQL 专项、付费最小样本和进程/网络故障注入，并记录对应 Task、BillingOrder、Resource、Artifact revision、Outbox 与 sequence 身份。
