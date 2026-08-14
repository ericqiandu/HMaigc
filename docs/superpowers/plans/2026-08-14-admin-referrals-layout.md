# 后台邀请奖励页面排版实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把邀请奖励页统一为“运营概览 → 活动配置 → 套餐奖励 → 邀请关系”的后台数据页结构，同时保持现有奖励、分页和资格处置行为不变。

**Architecture:** 页面继续独占请求、草稿、分页和弹窗状态，只把成功内容接入现有无状态 `AdminDataLayout`、`AdminMetricBand`、`AdminMetric` 与 `AdminContentSection`。样式修改限定在邀请奖励现有语义类，删除被共享组件替代的指标卡样式，不引入新依赖或平行组件。

**Tech Stack:** React 19、TypeScript strict、Ant Design 6、现有后台数据页组件、Bun Test、Vite。

## Global Constraints

- 不改变后台路由、接口、权限、分页、奖励换算、活动开关或取消资格行为。
- 不引入 ProComponents、Headless UI 或新布局依赖。
- 生产职责不超过 3 项，生产文件不超过 4 个，净新增生产代码不超过 200 行。
- 禁止 `any`、旧行为兼容层、静默回退、默认数据和前端伪成功。
- 首次加载、首次失败、刷新失败、空关系和成功内容继续基于真实服务端事实展示。
- 390px、1024px、1440px 都不得出现页面级横向溢出。
- 不改后端、数据库、DTO、会员套餐、积分计算或计费。

## 文件结构

- Modify: `web/src/pages/admin/referrals/referral-program-page.tsx` — 接入共享排版组件，保持全部业务状态与请求逻辑。
- Modify: `web/src/pages/admin/admin-domain-workspace.css` — 删除重复指标卡外观并统一邀请奖励桌面/响应式布局。
- Create: `web/test/admin-referrals-layout.test.tsx` — 邀请奖励结构、真实状态和 CSS 契约测试。

---

### Task 1: 邀请奖励页接入共享结构

**Files:**
- Modify: `web/src/pages/admin/referrals/referral-program-page.tsx`
- Create: `web/test/admin-referrals-layout.test.tsx`

**Interfaces:**
- Consumes: `AdminDataLayout`、`AdminMetricBand`、`AdminMetric`、`AdminContentSection`。
- Preserves: `getAdminReferralProgram`、`updateAdminReferralProgram`、`updateAdminReferralRewardRule`、`disqualifyAdminReferralInvitation` 与现有分页状态。

- [ ] **Step 1: 写结构 RED 测试**

测试读取页面源文件并断言成功内容依次包含 `AdminDataLayout`、标题为“邀请活动概览”的 `AdminMetricBand`，以及“活动配置”“套餐奖励”“邀请关系”三个 `AdminContentSection`；同时保留三项指标、活动开关、逐行保存、分页和取消资格弹窗关键契约。

- [ ] **Step 2: 运行 focused RED**

Run: `cd web && bun test test/admin-referrals-layout.test.tsx`

Expected: FAIL，因为邀请奖励页尚未导入并使用共享数据页组件。

- [ ] **Step 3: 最小接入共享组件**

在 `referral-program-page.tsx` 中：

- 使用 `AdminDataLayout` 包裹现有成功内容。
- 使用一个 `AdminMetricBand title="邀请活动概览"` 和三个 `AdminMetric` 展示现有真实摘要。
- 使用三个 `AdminContentSection` 承载活动配置、套餐奖励和邀请关系。
- 把关系总数放入邀请关系分区 `actions`，不重复显示装饰卡片状态。
- 删除页面私有 `AdminMetric` 函数和不再需要的 `ReactNode` 导入。
- 不改任何请求、状态、比较、换算、分页或弹窗代码。

- [ ] **Step 4: 运行 focused GREEN**

Run: `cd web && bun test test/admin-referrals-layout.test.tsx test/admin-data-layout.test.tsx`

Expected: PASS，共享组件原有测试继续通过。

---

### Task 2: 收口页面样式与响应式行为

**Files:**
- Modify: `web/src/pages/admin/admin-domain-workspace.css`
- Modify: `web/test/admin-referrals-layout.test.tsx`

**Interfaces:**
- Consumes: 共享 `.admin-data-*`、`.admin-metric-*` 与 `.admin-content-section` 样式。
- Preserves: 邀请奖励规则行、表格、分页和取消资格 Modal 的现有业务类。

- [ ] **Step 1: 写 CSS RED 契约**

断言邀请奖励不再定义 `.referral-admin-overview`、`.referral-admin-metric` 私有指标表面；规则行在 1280px 以下重新排布，在 639px 以下使用单列字段与 44px 保存/刷新热区；表格横向滚动继续局限于 Ant Table 内容区。

- [ ] **Step 2: 运行 focused RED**

Run: `cd web && bun test test/admin-referrals-layout.test.tsx`

Expected: FAIL，因为旧私有指标和旧响应式规则仍存在。

- [ ] **Step 3: 实施最小样式清理**

- 删除 `.referral-admin-overview`、`.referral-admin-metric*` 及其响应式规则。
- 保留并整理活动配置、奖励规则、表格、分页和 Modal 现有语义类。
- 在 `max-width: 1279px` 让奖励行降为可读的两列/单列组合。
- 在 `max-width: 639px` 让字段、启停和保存操作纵向排列，操作热区不低于 44px。
- 不增加页面级 `overflow-x: hidden` 掩盖布局问题。

- [ ] **Step 4: 运行 focused GREEN**

Run: `cd web && bun test test/admin-referrals-layout.test.tsx test/admin-data-layout.test.tsx`

Expected: PASS，旧私有指标选择器无生产匹配。

---

### Task 3: 集中验收、提交与镜像重建

**Files:**
- Verify all changed files; only fix current-diff defects within the announced budget.

**Interfaces:**
- Validates the complete design against source, tests, production build and the running localhost environment.

- [ ] **Step 1: 运行 Web 门禁**

Run:

```powershell
Set-Location web
bun test test/admin-referrals-layout.test.tsx test/admin-data-layout.test.tsx
bun test
bun run build
```

Expected: focused tests、全量测试、类型检查、Vite 构建和 bundle budgets 全部 PASS。

- [ ] **Step 2: 核对范围和代码质量**

Run:

```powershell
git diff --check 5ab2c93..HEAD
git diff --stat 5ab2c93..HEAD
rg -n "as any|: any\b|TODO|FIXME" web/src/pages/admin/referrals web/src/pages/admin/admin-domain-workspace.css web/test/admin-referrals-layout.test.tsx
```

Expected: 禁止项无匹配；生产文件不超过 4 个；净新增生产代码不超过 300 行（含 50% 熔断线）。

- [ ] **Step 3: 一次集中 review**

对照用户需求、设计、计划、实际 diff、前端契约和测试证据；只集中修复当前 diff 缺陷。若发现新的跨模块 Critical/Important 或超出预算 50%，停止补丁循环并回到设计决策。

- [ ] **Step 4: 提交单一交付意图**

仅暂存邀请奖励生产文件与定向测试，执行 `git diff --cached --check` 后提交：

```powershell
git commit -m "refactor(admin): 统一邀请奖励页面结构"
```

- [ ] **Step 5: 安全重建本地镜像**

从主仓根目录运行：

```powershell
pwsh -File .\scripts\local-compose.ps1 up -d --build --wait
```

重建前后核对主仓 `E:/新版短剧制作/open-ai-canvas/.local/data` 的文件路径、大小和修改时间，确认后端/Web 健康、`/api/health` 与 `/admin/referrals` 返回 200，禁止创建 worktree 本地数据目录。

- [ ] **Step 6: 真实浏览器响应式验收**

使用已登录的本地后台分别验证 1440×900、1024×768、390×844：区块顺序正确、活动开关和规则保存可操作、关系表格滚动局部、取消资格弹窗完整、页面无横向溢出、控制台无错误。
