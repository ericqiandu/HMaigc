# 页头算力与会员图标统一设计

## 目标

项目、任务、素材、技能等工作区页头必须与首页使用同一套账户操作视觉：算力使用 16px 紫色实心闪电，会员使用 16px 金色多层钻石。会员购买页与画布编辑器保持独立，不纳入本次改动。

## 已确认事实

- `main` 中首页与工作区已经共同复用 `SiteAccountActions`，不需要新增第二套页头组件。
- `MembershipIcon` 已是首页参考图中的 1024×1024、11 层橙金色 SVG；旧工作区 `Gem` 只存在于当前 3000 端口的过期 Web 镜像，不存在于最新源码。
- 当前共享算力图标已经使用 Lucide `Zap`、16px、描边与填充同色，但颜色来自 `--brand-primary`，并非参考图的 `#5f6fff`。
- 当前 3000 端口运行的是合并前的 Web 镜像，必须重新构建并重建 Web 服务才能展示最新源码。

## 设计

1. 在全局设计 Token 中新增 `--credit-accent: #5f6fff`，表达积分/算力余额的稳定视觉语义。
2. `site-account-balance-icon` 改用 `--credit-accent`，继续保留 `fill: currentcolor`，得到参考图中的紫色实心闪电。
3. `MembershipIcon` 的 SVG 路径、填色、尺寸与可访问性保持不变；不得恢复 Lucide `Gem` 或复制首页私有实现。
4. 首页与工作区继续由同一个 `SiteAccountActions` 负责渲染，确保两处图标天然一致。
5. 只重建本地 Web 服务；不修改后端、数据库、业务数据、会员逻辑或画布编辑器。

## 验证

- 先增加源码契约测试，并确认它在实现前因缺少 `--credit-accent`、仍使用 `--brand-primary` 而失败。
- 实现后验证：算力图标为 `Zap`、16px、`fill: currentcolor`、颜色来源为 `--credit-accent`；会员图标使用共享 `MembershipIcon` 且生产源码不存在 `Gem`。
- 运行 Web 全量测试、生产构建与 Bundle Budget。
- 重建本地 Web 服务后，在真实浏览器检查 `/` 与 `/projects`：两处均使用 `.site-account-*`，算力闪电计算色为 `rgb(95, 111, 255)`，会员为同一金色多层 SVG，无控制台错误。

## 回滚边界

该变更只涉及一个语义颜色 Token、共享页头图标样式、契约测试以及本地 Web 镜像重建。源码回滚可独立完成，不影响任何业务数据。
