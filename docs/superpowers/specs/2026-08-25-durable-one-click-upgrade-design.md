# HMaigc 持久化一键升级设计

## 状态

- 设计日期：2026-08-25
- 设计状态：用户已确认
- 适用范围：单机 Linux + Docker Compose 生产环境
- 迁移方式：一次性服务器引导，后续只通过后台运维升级中心操作

## 1. 背景与问题

当前生产升级由独立 `ops-controller` 接受请求，再在控制器进程内启动长时间部署脚本。部署脚本与控制器打包在同一个不可变镜像中，因此服务器仍在运行旧控制器时，即使仓库已经修复部署脚本，云端升级也继续执行旧逻辑。控制器自身又不能通过当前任务队列安全替换自己，只能由运维人员复制新的控制平面目录、修改 `.env.production` 并重建控制器。

现有实现还有以下结构性问题：

1. 控制器同时承担请求接入、任务所有权和长时间脚本执行；控制器重启会直接把 `running` 任务标记为结果未知。
2. 升级事实主要依赖控制器 SQLite 记录与脚本最终退出码，没有可恢复的阶段 checkpoint。
3. 多个 `hmaigc-control-plane-vXXXX` 目录和各自的 `.env.production` 会产生版本、配置与 Compose 工作目录漂移。
4. 业务版本、部署执行器版本和控制器版本没有形成可核验的独立事实，容易出现“代码已经修复，但云端控制器没有获得修复”的错觉。
5. 当前脚本曾把部署主机访问公网 CDN 的连接结果纳入切换验收，导致健康目标版本被误回滚；该问题已由 `3053229` 在代码层拆分，但旧控制器不会自动获得修复。
6. 浏览器、业务后端或控制器断线时，管理员只能通过服务器命令和日志人工判断真实服务状态。

本设计不继续修补控制器内长脚本，而是把长期升级执行权移交给独立、目标版本化、可恢复的 Runner。

## 2. 目标

1. 完成一次服务器引导后，常规升级只需要管理员在后台点击一次并确认目标版本。
2. 每次升级使用目标版本携带的部署逻辑，不再依赖旧控制器镜像中的脚本。
3. Runner 独立于浏览器、业务后端和控制器运行；这些组件重启不丢失任务事实。
4. 旧版本在目标镜像、配置、磁盘、备份能力和在线审计全部通过前保持在线。
5. 进入维护窗口后，任何失败都依据持久 checkpoint 恢复到确定状态，不猜测成功，不伪造恢复。
6. 业务切换只依赖本机容器健康、精确版本、数据库依赖与本机 SPA 入口。公网 CDN 校验保持严格，但作为独立环境验证，不参与业务回滚。
7. 幂等键、单实例部署锁、Runner 租约和 fencing token 共同阻止重复执行与并发写生产环境。
8. 控制器可以由 Runner 按需替换；新控制器失败时自动恢复旧控制器，不回滚已经健康的业务版本。
9. 管理后台展示业务版本、Runner 版本、控制器版本、当前阶段、心跳、服务状态、恢复动作和准确错误。
10. 首次部署与旧控制平面迁移只走一次性 bootstrap；`install` 不进入日常任务队列，避免未初始化状态与正常升级共享一套恢复语义。
11. 回滚请求必须固定已校验的上一版本恢复点；当前版本安全备份不得清理该恢复点，回滚破坏性阶段失败时必须恢复安全备份。

## 3. 非目标

1. 不实现多服务器、高可用 PostgreSQL、Kubernetes、Raft 或跨区域发布。
2. 不承诺终端用户完全零停机。单机一致性备份、数据库迁移和后端切换期间允许短时受控维护。
3. 不把真实数据冲突、磁盘损坏、Docker Engine 故障或镜像仓库不可用伪装成成功。
4. 不保留“控制器直接执行部署脚本”和“独立 Runner 执行”两条正式升级路径；迁移完成后做硬切换。
5. 不让业务后端获得 Docker socket，也不允许业务数据库成为运维事实真源。

## 4. 核心架构

### 4.1 稳定控制器

`ops-controller` 只承担以下职责：

- 验证业务后端通过 Unix socket 发来的 HMAC 请求、管理员身份、时间窗、nonce、确认短语和幂等键。
- 以幂等键的 SHA-256 派生稳定 operation ID，先将不可变操作请求原子写入 `operations/<operation-id>/request.json`，再创建 SQLite 投影、签发 Runner generation 与 fencing token，并保证同一时间最多存在一个活动操作。同一幂等键的重试必须落到同一路径并核对 request hash；请求文件落盘失败时不得创建可执行任务。
- 拉取目标版本 Runner 镜像，解析并持久化本机实际使用的镜像 digest。
- 先写入权限为 `0600` 的签名 `commands/launch.json`，再以 detached 容器启动 Runner；容器参数只包含 operation ID 与 generation，持续导入 Runner 事件、日志、心跳、checkpoint 和最终结果。`docker run` 返回错误后必须重新读取带 operation/generation/digest 标签的容器事实：只有匹配且仍在运行的唯一实例可收敛为已启动；容器已停止、实例不匹配或 Docker 状态无法读取时必须进入 `recovery_required`，禁止写入猜测性的启动失败终态。
- 控制器重启后，根据 Docker 标签、Runner 心跳和持久文件重新附着现有 Runner，或在确认旧 Runner 已退出后启动恢复 Runner。
- 为后台提供操作、日志、备份、恢复状态与版本概览。

控制器不再用 `exec.CommandContext` 直接持有部署脚本进程。迁移完成后，控制器启动时也不再把所有 `running` 操作直接判为失败，而是先执行 Runner reconciliation。

### 4.2 目标版本 Runner

每个正式发布标签继续构建一个运维镜像。该镜像除控制器与 CLI 外，新增独立 Runner 入口。控制器拉取目标标签后记录精确 digest，并使用该 digest 启动一次性 Runner；后续执行不再引用可变化的标签。Runner 在进入维护窗口前同样拉取目标 Backend、Web 和备份 helper 镜像，记录各自的本机实际 digest；启动目标服务时向 Compose 注入摘要固定的镜像引用，禁止重新按标签解析。

Runner：

- 使用只读镜像、`no-new-privileges`、临时 `/tmp`、Docker socket 和现有 `ops-state` 命名卷。
- 读取 `ops-state` 中的规范化生产配置，不依赖宿主机某个版本目录。
- 持有现有 `deploy.lock` 的整个执行生命周期。
- 在每个生产写动作前校验 operation ID、generation、fencing token 和锁所有权。
- 执行在线预检、静止审计、备份、目标启动、本机验活、自动恢复和控制器交接。
- 将事件、日志、心跳、checkpoint 和结果先持久化，再执行下一阶段或向控制器投影。
- 完成后退出并由控制器清理容器；历史事实留在 `ops-state`。

### 4.3 运维状态卷

现有 `HMAIGC_OPS_STATE_VOLUME` 保持不变，并成为唯一运维事实根。建议布局：

```text
/var/lib/hmaigc-ops/
  config/
    production.env
    control.env
  controller.db
  shared-secret
  operations/<operation-id>/
    request.json
    commands/
      launch.json
      cancel.json
    lease.json
    heartbeat.json
    checkpoint.json
    result.json
    events/<generation>-<sequence>.json
  release/release.env
  release/logs/
  backups/
```

Runner 事件采用临时文件写入、`fsync`、原子 rename 的逐事件文件，不使用进程内缓冲作为事实源。Runner 是 `lease.json`、`heartbeat.json`、`checkpoint.json` 和执行期事件文件的唯一写入者，并负责所有已经进入执行阶段的结果；控制器是 `request.json`、`commands/*.json` 与 SQLite 投影的唯一写入者。仅在 Runner 尚未开始执行的排队取消、已确认启动失败，或需要用同 generation 的 `recovery_required` 结果阻止启动事实不明的 Runner 继续执行时，控制器可以写入 `result.json`；Runner 启动后会先读取该结果并在 generation 不高于结果时无副作用退出，后续恢复只能用更高 generation 覆盖 `recovery_required`。除此之外控制器不得修改 Runner 已落盘事实，Runner 也不得直接修改投影数据库。命令文件统一采用“schema version + 原始 JSON payload + HMAC-SHA256 signature”信封；读取方只能先解析信封，必须对原始 payload 字节完成常量时间验签后，才允许把 payload 解码为可信命令，并继续校验 operation ID、generation/nonce、时间窗和重放。原始 fencing token 仅存在于权限为 `0600` 的签名 `launch.json`，不得放入容器环境、标签、参数、inspect 输出、日志、事件或 SQLite；Runner 的 lease/checkpoint 只保存不可逆摘要。控制器将事件幂等投影到 SQLite；唯一键为 `operation_id + generation + sequence`。控制器停机期间 Runner 仍可继续落盘，新控制器启动后按 sequence 补录。

`production.env` 是业务 Compose 的规范化配置真源，明确保存摘要固定的 `HMAIGC_BACKEND_IMAGE`、`HMAIGC_WEB_IMAGE`、`BACKUP_HELPER_IMAGE` 与业务版本；`control.env` 保存摘要固定的 `HMAIGC_OPS_IMAGE`、控制器版本和协议版本。稳定控制器 Compose 只挂载 Docker socket 与现有 `ops-state` 命名卷，不再挂载宿主机版本目录或 `.env.production`。宿主机引导目录只保留灾难恢复入口，不再承担日常版本真源。

### 4.4 单一配置路径

一次性引导会在确认没有活动运维任务后：

1. 备份并校验当前 `ops-state`。
2. 把当前有效生产配置规范化复制到 `ops-state/config/production.env`，权限为 `0600`。
3. 将当前控制器版本、digest 与协议版本写入 `control.env`。
4. 将历史终态操作导入为不可变的终态 request/checkpoint/result/event 文件，保留原 operation ID、时间、操作者、日志路径和结果；再由新控制器重建 SQLite 投影。若发现任何 `queued`、`running` 或结果未知的历史操作，引导必须拒绝继续并要求先人工核验，禁止把活动任务直接标成失败或完成。
5. 启动首个支持 Runner 的稳定控制器，并验证它读取的是同一个命名卷。

引导失败时保持旧控制器和当前业务版本，不删除任何原目录。引导成功并经用户确认后，旧目录只作为人工留存证据，不再参与后续升级。该引导会改变生产运维配置真源，属于一次性高风险迁移；正式执行前必须在生产同构隔离环境演练，并再次获得用户对目标服务器、配置来源和维护影响的明确批准。

## 5. 升级状态机

### 5.1 阶段

| 阶段 | 业务状态 | 主要动作 | 失败处理 |
|---|---|---|---|
| `accepted` | 当前版本在线 | 校验权限、目标版本、幂等键和确认短语 | 拒绝请求 |
| `runner_preparing` | 当前版本在线 | 拉取 Runner、记录 digest、创建租约并启动容器 | 当前版本保持在线 |
| `online_preflight` | 当前版本在线 | 校验镜像、配置、磁盘、Docker、备份 helper 和 Agent 在线审计 | 当前版本保持在线 |
| `public_verifying` | 当前版本在线 | 仅用于独立 `verify`：校验公网 CDN、主域入口和跨网络可达性 | 记录环境校验失败，不改变业务发布状态 |
| `quiescing` | 进入维护 | 停止新写入、停止 Web/后端、等待进程退出 | 重启当前版本 |
| `quiesced_audit` | 维护中 | 静止数据库二次 Agent 审计 | 重启当前版本，不创建新恢复点 |
| `backing_up` | 维护中 | 创建并校验 PostgreSQL、业务卷和发布状态恢复点 | 重启当前版本 |
| `starting_target` | 维护中 | 启动目标后端、执行迁移、校验健康和版本 | 恢复数据与当前版本 |
| `verifying_target` | 维护中 | 启动 Web，校验本机健康、SPA 根和本机入口资源 | 恢复数据与当前版本 |
| `restoring_current` | 维护中 | 未改写数据时重新启动当前业务版本 | 进入人工恢复态 |
| `restoring_backup` | 维护中 | 恢复已验证的数据库与资源卷恢复点并启动当前版本 | 进入人工恢复态 |
| `committing_release` | 目标版本在线 | 原子写入当前版本、上一版本和恢复点 | 按 checkpoint 核对后恢复或进入人工恢复态 |
| `controller_handoff` | 目标版本在线 | 校验并按需替换控制器 | 恢复旧控制器，业务版本不回滚 |
| `completed` | 目标版本在线 | 写入最终结果、关闭租约、清理 Runner | 无 |

公网 CDN、主域入口和跨网络性能检查不属于上表的事务阶段。升级成功后管理员可运行独立 `verify`；其失败会产生明确环境故障记录，但不会停止或回滚已通过本机验活的业务版本。未执行公网校验时只能显示 `not_run`，不得推断为成功、失败或警告。

独立 `verify` 必须有确定的时间上限：清单资源最多八路并发，每个请求连接超时 5 秒、总超时 15 秒、最多两次尝试，整轮校验总期限 120 秒。任一资源在期限内无法验证即显式失败并记录具体 URL 与错误事实，禁止用无限重试、长时间串行等待或跳过失败资源制造成功。

### 5.2 Checkpoint

每个 checkpoint 至少包含：

- operation ID、action、目标版本、Runner digest。
- generation、fencing token 的不可逆摘要、最后事件 sequence。
- 当前阶段、上一完整阶段、阶段开始与完成时间。
- 当前业务版本、期望业务版本和服务状态。
- 是否已停写、是否已有已验证恢复点、恢复点路径和 checksum 状态。
- 数据迁移是否开始、目标后端是否健康、目标 Web 是否健康。
- 当前控制器版本和待切换控制器 digest。
- 已请求的取消、下一安全动作和失败原因。

checkpoint 只在阶段事实已经完成后推进，禁止预写“成功”。阶段内部的破坏性动作先写 `intent` 事件，动作完成后写 `fact` 事件；恢复逻辑据此区分“尚未执行”“已执行”和“结果未知”。

### 5.3 服务状态

运维协议新增结构化 `serviceState`：

- `current_online`
- `maintenance`
- `target_online`
- `current_restored`
- `unknown`

只有本机健康、精确版本和发布状态一致时才能写入 `current_online`、`target_online` 或 `current_restored`。证据不足时必须写 `unknown`，并阻止新的升级、回滚或备份任务。

## 6. 中断、恢复与取消

### 6.1 控制器重启

控制器启动时先扫描活动操作和带 HMaigc 运维标签的 Runner：

1. Runner 存活且 token/generation 匹配：重新附着，补录事件并继续展示。
2. Runner 已退出且存在完整 `result.json`：幂等投影最终结果。
3. Runner 心跳过期但容器仍在运行：先检查容器状态和锁，不启动第二个 Runner。
4. Runner 已确认退出且操作未终结：根据 checkpoint 启动相同 digest 的恢复 Runner，并增加 generation。
5. 无法确认旧 Runner 是否仍能修改生产环境：进入 `recovery_required`，禁止自动并发恢复。

### 6.2 Runner 中断

恢复 Runner 不从头盲目重跑，而是读取最后完整 checkpoint：

- 停写前中断：当前版本保持在线，操作失败或按策略重试预检。
- 已停写、尚未创建恢复点：重新启动当前版本并结束失败。
- 恢复点已验证、目标尚未提交：恢复该恢复点并启动当前版本。
- 目标已经本机验活但发布状态尚未提交：核对实际容器版本、迁移结果与恢复点后完成提交；证据冲突则恢复当前版本。
- 发布已经提交：保持目标业务版本，继续控制器交接或完成操作。
- 恢复过程本身中断：从恢复 checkpoint 继续；不得启动新的业务升级。

### 6.3 取消

新增取消请求，但取消不是强制杀进程：

- `accepted`、`runner_preparing`、`online_preflight`：立即取消。
- `quiescing`、`quiesced_audit`：先恢复当前版本，再标记取消。
- `backing_up`：允许当前不可分割的备份动作结束，然后恢复当前版本。
- `starting_target`、`verifying_target`：执行自动恢复后标记取消。
- `committing_release`、`controller_handoff`：先完成当前原子动作并确认业务服务状态，再结束；禁止留下半提交版本。

后台必须展示“已收到停止请求，正在到达安全点”，不得把延迟取消伪装成已停止。

## 7. 控制器交接

控制器镜像仍与正式版本一起发布，但并非每次业务升级都必须重建控制器。目标 Runner 比较当前控制器 digest 与目标 digest：

1. 相同则跳过交接。
2. 不同则先用目标镜像执行只读 `controller validate`，验证配置、审计 schema、共享密钥、状态卷和协议兼容性。该命令禁止启动调度循环、修改 SQLite、写事件或执行 `FailInterruptedOperations` 一类启动副作用。
3. 校验通过后，Runner 先保存旧 `control.env`，再原子更新 `control.env` 并重建 `ops-controller` 服务。
4. Runner 独立于该 Compose 服务，因此旧控制器退出不会终止升级。
5. 新控制器必须通过健康、版本、commit、socket 和活动操作重新附着检查。
6. 新控制器失败时，Runner 原子恢复旧 `control.env`，用已记录的旧 digest 恢复旧控制器，并把交接失败写入操作结果；目标业务版本保持在线。

业务发布在 `committing_release` 完成后已经提交。若新控制器失败但旧控制器恢复成功，操作总状态仍为 `succeeded`，同时返回 `warningCode=controller_handoff_failed` 和 `controllerHandoff=restored_previous`，避免把已经上线的业务版本伪报成失败。只有新旧控制器都无法恢复且运维入口状态不能确认时，操作才进入 `recovery_required`。控制器交接结果只能是 `unchanged`、`updated` 或 `restored_previous`。

稳定控制器必须理解当前 Runner 事件协议。若未来必须提升最低协议版本，发布门禁需在升级前明确声明并由当前控制器拒绝不兼容版本，不能静默猜测。

## 8. 协议与数据契约

### 8.1 操作状态

现有粗粒度状态扩展为：

- `queued`
- `running`
- `cancelling`
- `recovering`
- `succeeded`
- `failed`
- `cancelled`
- `recovery_required`

`phase` 不再承载机器判断，新增类型化 `stage`。操作响应还应包含：

- `runnerVersion`、`runnerDigest`、`runnerGeneration`
- `heartbeatAt`
- `serviceState`
- `checkpointSequence`
- `cancelRequestedAt`
- `recoveryAction`
- `controllerVersionAtStart`、`resultControllerVersion`
- `controllerHandoff`
- `resultVersion`
- `warnings`（结构化 warning code 与事实说明）
- 精确 `errorCode` 与 `error`

`Overview` 另行返回最近一次独立公网校验的 `publicVerification`，状态只能是 `not_run`、`succeeded` 或 `failed`，并包含对应 operation ID、检查时间和错误事实；不得从最近一次业务升级推导公网状态。

### 8.2 API

保留现有查询和创建接口，并新增：

- `POST /v1/operations/{id}/cancel`
- `POST /v1/operations/{id}/recover`

`recover` 只允许 `recovery_required`，并要求管理员重新确认实际服务影响。正常的控制器或 Runner 重启恢复由 reconciliation 自动完成，不要求用户点击。

### 8.3 错误分类

至少区分：

- `preflight_failed`
- `public_verify_failed`
- `runner_start_failed`
- `lease_lost`
- `quiesce_failed`
- `backup_failed`
- `migration_failed`
- `target_health_failed`
- `restore_failed`
- `controller_handoff_failed`
- `state_conflict`
- `cancelled_at_safe_point`

错误记录必须包含阶段、服务状态、目标版本、当前版本、Runner digest、最后 checkpoint 和建议的下一步；不得只返回“脚本退出码 1”。日志不得包含生产密钥、数据库密码、供应商密钥或共享密钥。

## 9. 管理后台

沿用现有运维升级页面视觉体系，只调整数据契约和必要交互：

- 概览分别展示业务版本、控制器版本和活动 Runner 版本。
- 活动任务展示类型化阶段、最近心跳、服务状态和是否正在恢复。
- 运行中提供停止按钮；进入非立即取消阶段后显示安全停止说明。
- `recovery_required` 时禁用升级、回滚和备份，只开放查看证据与人工恢复入口。
- 公网环境校验与业务升级结果分开展示，避免“业务升级成功但 CDN 校验失败”被合并成一个模糊失败。
- 浏览器刷新、换浏览器或业务后端重启后，页面从控制器持久事实恢复，不依赖前端本地状态。

## 10. 安全边界

1. Docker socket 仍只挂载给控制器和一次性 Runner，业务后端只读访问 Unix socket 与共享密钥。
2. Runner 必须按目标镜像 digest 启动，容器使用固定标签记录 operation、generation 和 digest。
3. 生产配置存入权限为 `0600` 的 ops-state 文件，不写入操作事件或日志。
4. 每个破坏性动作前验证部署锁和 fencing token；恢复 Runner 接管前必须确认旧 Runner 已退出。
5. 操作请求继续使用 HMAC、时间窗、nonce、管理员鉴权、确认短语和幂等键。
6. 备份、恢复点和发布状态保持校验和与受控路径校验；不得接受 ops-state 之外的恢复路径。

## 11. 测试与验收

### 11.1 单元与协议测试

- 操作状态转换和非法转换。
- 幂等请求与请求哈希冲突。
- generation、租约、fencing token 和过期心跳。
- Runner 事件按 sequence 幂等导入和断点补录。
- checkpoint 原子写入、截断文件拒绝和结果投影。
- 取消在各阶段的安全动作。
- 错误分类、脱敏和管理员权限。

### 11.2 Docker 故障注入

现有 smoke harness 扩展为至少覆盖：

1. 正常一次性 bootstrap、升级、回滚和再次升级；日常任务入口必须拒绝 `install`。
2. 重复点击与并发请求。
3. 目标镜像不存在、拉取失败和磁盘空间不足。
4. Controller 在各阶段重启。
5. Runner 在每个 checkpoint 前后被终止。
6. Backend/Web 启动失败和版本不匹配。
7. Agent 在线审计与静止审计失败。
8. 备份失败、checksum 损坏、迁移失败和自动恢复。
9. 恢复过程再次中断。
10. CDN 完全不可访问时业务升级仍能完成；独立 `verify` 必须失败。
11. 目标控制器校验失败、启动失败和旧控制器自动恢复。
12. 浏览器、业务后端和控制器同时发生连接中断后，操作事实仍可查询。

### 11.3 最终门禁

- Go focused tests、全量 tests、build 和受影响 race。
- Shell 语法、ShellCheck（如当前 CI 可用）与部署 smoke。
- Web tests、typecheck 和 production build。
- 三份 Compose 配置校验。
- 隔离恢复点上的旧版本到目标版本真实升级、自动恢复和回滚演练。
- 一次性引导在生产同构隔离环境中的演练。
- `git diff --check` 和逐项需求/计划/diff/协议/数据库/权限/幂等/错误/文档/测试复核。

## 12. 文档与运维交付

实现时同步更新：

- `deploy/README.md`：日常一键升级、Runner、取消、恢复和灾难引导。
- `PRODUCTION.md`：维护窗口、故障证据、控制器交接和回滚边界。
- `CHANGELOG.md`：未发布的升级体系硬切换说明。
- `.env.production.example`：首次引导所需参数；日常版本不再由宿主机目录手工维护。
- 管理后台运维页面说明和 API 类型。

## 13. 实施边界

该设计是一个架构里程碑，但应按以下顺序形成可验证的小提交：

1. Runner 事件、checkpoint、租约与状态机契约。
2. 独立 Runner 启动、恢复和故障注入。
3. 控制器 reconciliation、取消与 API。
4. 业务升级脚本接入 Runner 并硬切换旧执行路径。
5. 控制器交接与旧控制器自动恢复。
6. 管理后台和文档。
7. 一次性引导演练与最终发布。

每个里程碑都必须保留当前生产可回退提交；最终发布前不得让生产控制器进入一半新、一半旧的双轨状态。
