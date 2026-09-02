# HMaigc 核心记忆

## 产品目标

HMaigc 是面向真实用户、真实模型成本和长期商业运营的 AI 内容生产平台。所有方案必须优先保证正确性、安全性、可维护性、可观测性、计费一致性、数据隔离、部署升级与回滚能力，而不是追求局部页面“看起来能用”。

## 不可妥协的约束

- 只修根因，不通过复制分支、临时兼容层、默认值、假数据或静默降级掩盖错误。
- 同一能力只保留一条正式实现路径；发现历史双轨时做明确硬切换，不继续叠加第三套方案。
- 写操作和关键链路显式失败，并留下可检索的结构化证据；禁止吞异常或前端伪成功。
- 多用户、权限、积分、计费、模型调用和资产写入必须考虑并发、幂等、审计和失败补偿。
- 前端与后台遵循同一设计真源；新增页面、弹窗、节点和组件不得另起一套视觉标准。
- 视觉 Token 的唯一原始值来源是 `web/src/styles/design-tokens.css`；`Design.md` 定义语义和使用边界，`web/DESIGN.md` 定义 Web 落地映射。
- 已生成资产必须保留并可追溯；后处理不得因质检或失败删除真实产出。
- 不把密钥、数据库、本地状态、构建产物或临时审计文件提交到仓库。

## 项目边界

- 当前正式前端只有 `web/`，不得创建或恢复第二套前端目录。
- `.local/` 保存本地运行数据，清理缓存时禁止删除。
- `deploy/`、`scripts/`、`web/scripts/` 中被工作流、package scripts 或部署文档引用的脚本属于正式交付链路，不得当作污染脚本删除。
- `qa-artifacts/` 是受版本控制的视觉验收证据，不属于可随意清理的缓存。
- `.codex-audit/`、`.playwright-cli/`、`artifacts/`、`release-web-dist/`、`web/.lighthouseci/` 与 `web/dist/` 属于可再生输出；确认未被跟踪且没有正在进行的验收任务后可以清理。

## 版本基线

- 当前已发布版本：`v1.0.55`，发布事实以 Git 标签和生产健康检查交叉确认；当前开发分支仍以自身 `VERSION`、`CHANGELOG.md` 与实际差异为准。
- 当前工作区存在未发布改动时，必须以 `git status`、`git diff` 和实际验证结果判断，不得预设为某个新版本。
- 任何版本号、更新日志、镜像标签与后台升级中心显示必须来自同一发布事实，禁止分别手工宣称不同版本。

## Seedance 模型能力基线

- Seedance 2.0 Fast 仅支持 480P、720P；Seedance 2.0 Pro 支持 480P、720P、1080P、4K；Seedance 2.0 Mini 仅支持 480P、720P；Seedance 2.5 支持 480P、720P、1080P。
- 模型目录、前端参数、供应商请求与计费规格必须消费同一能力事实；不得为 Fast 1080P 虚构价格，也不得静默漏配 2.5 1080P。

## 生产域名、OSS 与商业接入边界

- `hm.kunagent.com` 是正式业务主域名：线上应用、登录 OAuth 回调、会员收银台、支付异步通知均以它为唯一公开地址。
- `hmaigc.ai` 与 `www.hmaigc.ai` 保留为品牌入口，但生产 Nginx 应以 301 将完整路径和查询参数跳转到 `https://hm.kunagent.com$request_uri`；不得让两个主域名同时承载完整应用和支付流程，避免 Cookie、OAuth state 和订单回跳分裂。
- OSS 必须按职责分 Bucket：`hmaigc-prod-static` 只存放版本化 Web 静态资源；`hmaigc-prod-assets` 只存放用户上传和生成媒体。不得混用配置或访问密钥权限。
- 静态 Bucket 的 CORS 来源必须精确匹配 Origin，不能带尾随 `/`。正式来源为 `https://hmaigc.ai`、`https://www.hmaigc.ai`、`https://hm.kunagent.com`；需要 GET 和 HEAD，允许 Header 为 `*`，并返回 Vary: Origin。发布新域名后，必须用带 Origin 的 curl 验证 `Access-Control-Allow-Origin` 后再通知用户访问，避免 JS 被浏览器拦截导致黑屏。
- 平台素材 Bucket 默认私有，通过应用鉴权读取；其 CORS 只用于已明确需要浏览器直连预览或上传的场景。RAM 密钥必须专用于目标 Bucket、按最小权限授权，禁止使用主账号 AccessKey。
- 支付和微信登录是独立能力：支付回调可使用 `/api/payments/webhooks/wechat` 与 `/api/payments/webhooks/alipay`；微信 OAuth 登录必须以独立的开放平台凭据和 HTTPS 回调实现，不能复用微信支付商户凭据。
