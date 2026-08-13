# 账号级 AI 水印与规范发布 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 本计划涉及同一账号状态、规范版本、任务创建事务和 provider 请求事实，禁止使用 `superpowers:subagent-driven-development` 拆成并行补丁。

**Goal:** 把现有浏览器本地和节点级水印开关硬切为可审计的账号级“去 AI 水印”设置，并让后台规范发布、用户同意、任务冻结和 provider 请求使用同一事实链。

**Architecture:** 新增不可变的规范 publication、单例 publication head、账号偏好、同意历史和账号偏好事件五个持久化模型；管理员发布和用户启用分别在数据库事务中完成。媒体任务在计费预留与任务创建事务内冻结 `WatermarkDirective`，worker 和 provider adapter 只读取冻结事实，客户端不再提交水印字段。前端用后台“法律与协议”独立发布区和头像菜单大弹窗消费同一 DTO，同时删除旧设置页、Zustand 字段和节点 metadata 映射。

**Tech Stack:** Go 1.24、Gin、GORM、PostgreSQL 17/SQLite、React 18、TypeScript strict、TanStack Query、Ant Design、Bun test、Vite、Docker Compose。

## Global Constraints

- 默认 `removeWatermark=false`，即支持水印控制的模型必须显式请求带水印。
- 当前 publication 不存在时禁止开启去水印；关闭不要求 publication。
- publication 由管理规则富文本与水印规范 HTTPS 外链共同构成，二者任一为空均不可发布。
- publication 不可变；相同内容也允许创建新版本，但后台必须二次确认该动作会使旧同意失效。
- 账号启用时必须提交用户看到的 `publicationId`，事务内发现新版本时返回 HTTP 409，不写旧同意。
- 仅 registry/adapter 明确声明 `controlled` 的模型发送 `watermark=true/false`；明确 `unsupported` 的模型省略字段；媒体模型能力未知时阻止发布和任务创建。
- 图片、视频节点和任务请求不再接受水印参数；禁止保留“节点优先、全局兜底”兼容路径。
- 同一任务的 worker 重领和内部重试保持冻结事实；用户点击“重试任务”视为新请求，在重新计费入队事务内重算水印快照。
- 迁移只增量建表、建索引和加任务列，不删除、不覆盖现有用户、任务、资产、积分和支付数据。
- 富文本沿用现有法律内容白名单；水印规范 URL 只验证，不抓取、不代理、不缓存。
- 不新增 `any` / `as any`；所有 JSON 解析未知字段原地失败。
- 完整 Go/race/PostgreSQL/Docker/浏览器门禁只在稳定里程碑和最终收口执行，开发循环只跑 focused tests。
- 仓库事实确认旧水印字段横跨前后端 40 余个文件，已触发设计规格的 30 文件熔断，因此实施必须重新切成两个串行里程碑：A“领域/API/任务/provider”不超过 20 个生产文件、约 1050 行；B“Web 体验/旧逻辑硬切”不超过 23 个生产文件、约 700 行。两个里程碑共享同一契约，禁止并行，最终只能联合发布。
- 任一里程碑实际文件数或净新增超过预算 50%，或独立新增超过 500 行的生产文件，立即停止并回到模块边界设计，不进入第三轮补丁修复。

---

## File Map

### 领域与持久化

- Create `backend/internal/model/watermark_policy.go`：publication、head、preference、consent、capability 与 directive 的闭集类型。
- Modify `backend/internal/model/models.go`：给 `Task` 增加冻结水印事实字段。
- Modify `backend/internal/database/schema.go`：把五张新表加入唯一迁移清单。
- Modify `backend/internal/database/provider_schema.go`：校验 publication、consent 与账号事件的精确唯一/查询索引，防止同名错误索引被静默接受。
- Modify `backend/internal/database/schema_test.go`：验证表、任务列和唯一索引。
- Create `backend/internal/repository/watermark_policy.go`：发布、读取、账号保存和任务快照冻结事务。
- Create `backend/internal/repository/watermark_policy_test.go`：SQLite 事务、幂等、版本竞争和回滚测试。
- Modify `backend/internal/repository/finance.go`：任务创建/用户重试事务调用水印快照冻结。
- Modify `backend/internal/repository/provider_task_freeze_test.go`：证明 provider runtime、计费和水印事实同事务。

### 服务、API 与能力目录

- Create `backend/internal/service/watermark_policy.go`：输入验证、公共 DTO、管理员发布、账号读写。
- Create `backend/internal/service/watermark_policy_test.go`：业务状态、HTML/URL、冲突和审计测试。
- Create `backend/internal/handler/watermark_policy.go`：四条严格 JSON 路由。
- Create `backend/internal/handler/watermark_policy_test.go`：权限、未知字段、请求大小、409 和响应脱敏。
- Modify `backend/cmd/server/main.go`：注册水印规范路由。
- Modify `backend/internal/service/provider_registry.go`：把 `SupportsWatermark bool` 硬切为闭集 `WatermarkCapability`。
- Modify `backend/internal/service/provider_registry_test.go`：所有媒体 registry model 必须显式声明能力。
- Modify `backend/internal/service/admin.go`：公共模型 DTO 投影闭集 capability。
- Create `backend/internal/service/task_watermark.go`：按动态模型身份解析能力并构造任务快照参数。
- Create `backend/internal/service/task_watermark_test.go`：支持、不支持、未知、账号变更、发布变更和用户重试测试。

### Worker 与 provider 请求

- Modify `backend/internal/service/provider.go`：删除客户端 `VideoWatermark`，执行时从 `Task` 注入内部 directive。
- Modify `backend/internal/service/service.go`：创建/用户重试传入 capability；worker 调用改为传完整 `Task`。
- Modify `backend/internal/service/storage_quota.go`：把 capability 传入任务创建事务。
- Modify `backend/internal/service/provider_ai_open_platform_volcengine_video.go`：只按冻结参数显式发送布尔值。
- Modify `backend/internal/service/provider_minimax_h3_video.go`：只按冻结参数显式发送 `aigc_watermark`。
- Modify `backend/internal/service/provider_kling_video.go`：登记为 unsupported 后完全省略字段。
- Modify `backend/internal/service/provider_kuaizi_compatible_worker.go`：Seedance 消费任务 directive；GPT Image 2 保持 unsupported。
- Modify `backend/internal/service/provider_test.go`、`backend/internal/service/provider_kling_video_test.go`、`backend/internal/service/provider_kuaizi_compatible_worker_test.go`：精确请求体与冻结事实测试。
- Modify `backend/internal/service/canvas_share.go`：共享画布允许字段删除 `watermark`。

### Web 账号弹窗与后台发布

- Create `web/src/services/api/watermark-policy.ts`：严格 DTO、HTTP 409 专用错误和四条 API。
- Create `web/src/components/account/ai-watermark-settings-modal.tsx`：账号弹窗状态机。
- Create `web/src/components/account/ai-watermark-settings-modal.css`：Design tokens、单层圆角、桌面/小屏和焦点样式。
- Modify `web/src/components/layout/site-account-actions.tsx`：菜单项打开弹窗而非路由跳转。
- Modify `web/src/pages/admin/settings/legal-settings-page.tsx`：挂载独立 AI 水印 publication 区。
- Create `web/src/pages/admin/settings/ai-watermark-policy-editor.tsx`：管理规则编辑、URL、版本与重新发布确认。
- Modify `web/src/pages/admin/admin-feature-workspace.css`：后台 AI 水印区的紧凑布局。
- Create `web/test/ai-watermark-settings.test.tsx`：真实 DOM 弹窗交互。
- Create `web/test/ai-watermark-policy-admin.test.tsx`：后台发布与二次确认交互。

### 旧逻辑硬切

- Delete `web/src/pages/settings/watermark-settings.tsx`。
- Modify `web/src/pages/settings/index.tsx`：删除旧水印 section。
- Modify `web/src/styles/globals.css`：删除仅服务旧设置页的 `.settings-watermark-*` 死样式。
- Modify `web/src/stores/use-config-store.ts`：删除 `videoWatermark` 和 `supportsWatermark`，改为闭集 `watermarkCapability`。
- Modify `web/src/services/api/provider-accounts.ts`：严格解析闭集 capability。
- Modify `web/src/services/api/video.ts`：删除所有 provider payload 的客户端 watermark 字段。
- Modify `web/src/lib/ai/system-provider-config.ts`：删除 `videoWatermark`。
- Modify `web/src/types/canvas.ts`：删除 `metadata.watermark`。
- Modify `web/src/components/canvas/canvas-assistant-panel.tsx`、`canvas-config-node-panel.tsx`、`canvas-node-prompt-panel.tsx`：删除输入 schema、节点优先级和 metadata 回写。
- Modify `web/src/lib/canvas/canvas-project-generation.ts`、`web/src/pages/canvas/canvas-media-generation-executors.ts`、`web/src/pages/canvas/use-canvas-generation-retry.ts`：删除创建、执行和结果 metadata 中的水印字段。
- Modify `web/src/components/canvas/canvas-image-generation-settings.tsx`、`canvas-video-settings-popover.tsx`：仅对 unsupported 模型显示只读说明。
- Modify `web/test/canvas-image-generation-settings.test.ts`、`web/test/video-model-capabilities.test.ts`、`web/test/kuaizi-provider-settings.test.tsx`、`web/test/kuaizi-provider-interactions.test.tsx`：断言生成请求、节点 metadata、动态 capability 和本地持久化均不存在客户端水印事实。

---

### Task 1: 不可变 publication、账号偏好与数据库事务

**Files:**
- Create: `backend/internal/model/watermark_policy.go`
- Modify: `backend/internal/model/models.go`
- Modify: `backend/internal/database/schema.go`
- Modify: `backend/internal/database/provider_schema.go`
- Modify: `backend/internal/database/schema_test.go`
- Create: `backend/internal/repository/watermark_policy.go`
- Create: `backend/internal/repository/watermark_policy_test.go`

**Interfaces:**
- Produces domain types:

```go
type WatermarkCapability string

const (
	WatermarkCapabilityControlled    WatermarkCapability = "controlled"
	WatermarkCapabilityUnsupported   WatermarkCapability = "unsupported"
	WatermarkCapabilityNotApplicable WatermarkCapability = "not_applicable"
)

type WatermarkDirective string

const (
	WatermarkDirectiveWithWatermark    WatermarkDirective = "with_watermark"
	WatermarkDirectiveWithoutWatermark WatermarkDirective = "without_watermark"
	WatermarkDirectiveProviderDefault  WatermarkDirective = "provider_default"
)
```

- Produces repository methods:

```go
func (r *Repository) CurrentWatermarkPolicy() (*model.PolicyPublication, error)
func (r *Repository) PublishWatermarkPolicy(publication *model.PolicyPublication, audit *model.AdminAuditEvent) error
func (r *Repository) WatermarkPreference(userID string) (*model.UserWatermarkPreference, *model.PolicyPublication, error)
func (r *Repository) SaveWatermarkPreference(userID string, remove bool, publicationID string, event *model.UserWatermarkPreferenceEvent, now time.Time) (*model.UserWatermarkPreference, *model.PolicyPublication, error)
func FreezeTaskWatermarkTx(tx *gorm.DB, task *model.Task, capability model.WatermarkCapability) error
```

- [ ] **Step 1: Write failing schema and repository tests**

```go
func TestPublishWatermarkPolicyCreatesImmutableMonotonicVersions(t *testing.T) {
	db := testDB(t)
	repo := New(db)
	first := policyPublication("publication-1", "<p>规则</p>", "https://example.com/watermark")
	require.NoError(t, repo.PublishWatermarkPolicy(first, auditFor(first.ID)))
	second := policyPublication("publication-2", "<p>规则</p>", "https://example.com/watermark")
	require.NoError(t, repo.PublishWatermarkPolicy(second, auditFor(second.ID)))
	require.Equal(t, int64(1), first.Version)
	require.Equal(t, int64(2), second.Version)
	require.Equal(t, first.ContentHash, second.ContentHash)
	require.Equal(t, int64(2), countRows[model.PolicyPublication](t, db))
}

func TestSaveWatermarkPreferenceRejectsStalePublicationWithoutWritingConsent(t *testing.T) {
	db := testDB(t)
	repo := New(db)
	publishPolicy(t, repo, "publication-1")
	publishPolicy(t, repo, "publication-2")
	_, _, err := repo.SaveWatermarkPreference("user-1", true, "publication-1", preferenceEvent("user-1", true), time.Now())
	require.ErrorIs(t, err, ErrWatermarkPolicyVersionConflict)
	require.Equal(t, int64(0), countRows[model.UserPolicyConsent](t, db))
	require.Equal(t, int64(0), countRows[model.UserWatermarkPreference](t, db))
}
```

- [ ] **Step 2: Run focused tests and confirm RED**

Run:

```powershell
cd backend
go test ./internal/database ./internal/repository -run 'WatermarkPolicy|WatermarkPreference' -count=1
```

Expected: build fails because the five models and repository methods do not exist.

- [ ] **Step 3: Add the five tables and task snapshot fields**

Use these persisted models:

```go
type PolicyPublicationHead struct {
	Kind                 string    `json:"kind" gorm:"primaryKey;size:32"`
	CurrentPublicationID string    `json:"currentPublicationId" gorm:"size:36;uniqueIndex"`
	CurrentVersion       int64     `json:"currentVersion"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type PolicyPublication struct {
	ID                     string    `json:"id" gorm:"primaryKey;size:36"`
	Kind                   string    `json:"kind" gorm:"size:32;uniqueIndex:idx_policy_publication_version,priority:1"`
	Version                int64     `json:"version" gorm:"uniqueIndex:idx_policy_publication_version,priority:2"`
	ManagementRuleRichText string    `json:"managementRuleRichText" gorm:"type:text"`
	WatermarkPolicyURL     string    `json:"watermarkPolicyUrl" gorm:"size:2048"`
	ContentHash            string    `json:"contentHash" gorm:"size:64;index"`
	PublishedBy            string    `json:"publishedBy" gorm:"size:36;index"`
	PublishedAt            time.Time `json:"publishedAt" gorm:"index"`
}

type UserWatermarkPreference struct {
	UserID                string    `json:"userId" gorm:"primaryKey;size:36"`
	RemoveWatermark       bool      `json:"removeWatermark"`
	AcceptedPublicationID string    `json:"acceptedPublicationId" gorm:"size:36;index"`
	AcceptedAt            *time.Time `json:"acceptedAt"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type UserPolicyConsent struct {
	ID                  string    `json:"id" gorm:"primaryKey;size:36"`
	UserID              string    `json:"userId" gorm:"size:36;uniqueIndex:idx_user_policy_consent,priority:1"`
	PolicyPublicationID string    `json:"policyPublicationId" gorm:"size:36;uniqueIndex:idx_user_policy_consent,priority:2"`
	AcceptedAt          time.Time `json:"acceptedAt"`
}

type UserWatermarkPreferenceEvent struct {
	ID                  string    `json:"id" gorm:"primaryKey;size:36"`
	UserID              string    `json:"userId" gorm:"size:36;index"`
	RemoveWatermark     bool      `json:"removeWatermark"`
	PolicyPublicationID string    `json:"policyPublicationId" gorm:"size:36;index"`
	ResultStatus        string    `json:"resultStatus" gorm:"size:32"`
	CreatedAt           time.Time `json:"createdAt" gorm:"index"`
}
```

Add these `Task` fields:

```go
WatermarkCapability          WatermarkCapability `json:"watermarkCapability,omitempty" gorm:"size:24"`
WatermarkDirective           WatermarkDirective  `json:"watermarkDirective,omitempty" gorm:"size:32"`
WatermarkParameterApplied    bool                `json:"watermarkParameterApplied"`
WatermarkParameterValue      *bool               `json:"watermarkParameterValue,omitempty"`
WatermarkPolicyPublicationID string              `json:"watermarkPolicyPublicationId,omitempty" gorm:"size:36;index"`
WatermarkPolicyVersion       int64               `json:"watermarkPolicyVersion,omitempty"`
```

- [ ] **Step 4: Implement serialized publication and preference transactions**

`PublishWatermarkPolicy` must:

1. `INSERT ... ON CONFLICT DO NOTHING` the `ai_watermark` head.
2. `SELECT ... FOR UPDATE` the head.
3. assign `CurrentVersion + 1` to the immutable publication.
4. insert publication, update head and insert the supplied audit event in one transaction.

`SaveWatermarkPreference` must lock the head before checking `publicationID`; use `ON CONFLICT DO NOTHING` for consent; update preference and append the supplied preference event in the same transaction. Disabling preserves the previous accepted publication fields but sets `RemoveWatermark=false`.

- [ ] **Step 5: Re-run focused tests and confirm GREEN**

Run the Step 2 command. Expected: all selected tests pass on SQLite.

- [ ] **Step 6: Commit the persistence milestone**

```powershell
git add backend/internal/model/watermark_policy.go backend/internal/model/models.go backend/internal/database/schema.go backend/internal/database/provider_schema.go backend/internal/database/schema_test.go backend/internal/repository/watermark_policy.go backend/internal/repository/watermark_policy_test.go
git diff --cached --check
git commit -m "feat(settings): 建立 AI 水印规范与账号偏好事实"
```

---

### Task 2: 管理员发布与账号 API

**Files:**
- Create: `backend/internal/service/watermark_policy.go`
- Create: `backend/internal/service/watermark_policy_test.go`
- Create: `backend/internal/handler/watermark_policy.go`
- Create: `backend/internal/handler/watermark_policy_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Produces service DTOs:

```go
type WatermarkPreferenceStatus string

const (
	WatermarkPreferenceDisabled          WatermarkPreferenceStatus = "disabled"
	WatermarkPreferenceActive            WatermarkPreferenceStatus = "active"
	WatermarkPreferencePolicyUpdated     WatermarkPreferenceStatus = "policy_updated"
	WatermarkPreferencePolicyUnavailable WatermarkPreferenceStatus = "policy_unavailable"
)

type WatermarkPolicySummary struct {
	ID                     string    `json:"id"`
	Version                int64     `json:"version"`
	ManagementRuleRichText string    `json:"managementRuleRichText"`
	WatermarkPolicyURL     string    `json:"watermarkPolicyUrl"`
	ContentHash            string    `json:"contentHash"`
	PublishedAt            time.Time `json:"publishedAt"`
}

type WatermarkPreferenceView struct {
	RemoveWatermark bool                       `json:"removeWatermark"`
	Status          WatermarkPreferenceStatus  `json:"status"`
	CanEnable       bool                       `json:"canEnable"`
	AcceptedAt      *time.Time                 `json:"acceptedAt"`
	CurrentPolicy   *WatermarkPolicySummary    `json:"currentPolicy"`
}
```

- Consumes the repository methods and immutable `UserWatermarkPreferenceEvent` model from Task 1.
- The service constructs the event before `SaveWatermarkPreference`, which writes it in the same transaction as preference/consent. Failed stale-version attempts are emitted as a separate structured application log without rich text, cookies, provider keys or prompts because the rejected business transaction must remain write-free.
- Produces routes:
  - `GET /api/me/watermark-preference`
  - `PUT /api/me/watermark-preference`
  - `GET /api/admin/legal/ai-watermark-policy`
  - `POST /api/admin/legal/ai-watermark-policy/publications`

- [ ] **Step 1: Write failing service tests for validation and states**

```go
func TestPublishWatermarkPolicyRequiresSafeRichTextAndStrictHTTPSURL(t *testing.T) {
	svc, admin := watermarkServiceFixture(t)
	invalid := []PublishWatermarkPolicyRequest{
		{WatermarkPolicyURL: "https://example.com/policy"},
		{ManagementRuleRichText: "<p>规则</p>"},
		{ManagementRuleRichText: "<script>alert(1)</script>", WatermarkPolicyURL: "https://example.com/policy"},
		{ManagementRuleRichText: "<p>规则</p>", WatermarkPolicyURL: "http://example.com/policy"},
		{ManagementRuleRichText: "<p>规则</p>", WatermarkPolicyURL: "https://user:pass@example.com/policy"},
		{ManagementRuleRichText: "<p>规则</p>", WatermarkPolicyURL: "https://example.com/policy#fragment"},
	}
	for _, request := range invalid {
		_, err := svc.PublishWatermarkPolicy(admin, request)
		requireAuthStatus(t, err, http.StatusBadRequest)
	}
}

func TestWatermarkPreferenceStatusChangesWhenPolicyRepublishes(t *testing.T) {
	svc, admin, user := watermarkServiceWithUsers(t)
	v1 := mustPublish(t, svc, admin, "<p>规则一</p>", "https://example.com/v1")
	active := mustSavePreference(t, svc, user, true, v1.ID)
	require.Equal(t, WatermarkPreferenceActive, active.Status)
	mustPublish(t, svc, admin, "<p>规则二</p>", "https://example.com/v2")
	updated, err := svc.WatermarkPreference(user)
	require.NoError(t, err)
	require.Equal(t, WatermarkPreferencePolicyUpdated, updated.Status)
	require.False(t, updated.RemoveWatermark)
}
```

- [ ] **Step 2: Write failing handler tests for strict requests and permissions**

```go
func TestWatermarkPolicyRoutesRejectUnknownFieldsAndStalePublication(t *testing.T) {
	fixture := watermarkHandlerFixture(t)
	fixture.request(http.MethodPost, "/api/admin/legal/ai-watermark-policy/publications", `{"managementRuleRichText":"<p>规则</p>","watermarkPolicyUrl":"https://example.com/policy","extra":true}`, fixture.adminCookie).expectStatus(http.StatusBadRequest)
	fixture.request(http.MethodPut, "/api/me/watermark-preference", `{"removeWatermark":true,"publicationId":"stale"}`, fixture.userCookie).expectStatus(http.StatusConflict)
}
```

- [ ] **Step 3: Run focused tests and confirm RED**

```powershell
cd backend
go test ./internal/service ./internal/handler -run 'WatermarkPolicy|WatermarkPreference' -count=1
```

Expected: build fails because the service DTOs and routes do not exist.

- [ ] **Step 4: Implement URL, rich-text, state and audit logic**

`validateWatermarkPolicyURL` must require:

```go
parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == "" && len(raw) <= 2048
```

Reject ASCII control characters before parsing. Reuse `validateLegalRichText("AI 生成内容水印管理规则", html)` and additionally require non-empty visible text. Compute:

```go
contentHash := sha256.Sum256([]byte(strings.TrimSpace(html) + "\n" + normalizedURL))
```

Create the admin audit template before calling the repository so publication and audit commit together. After the repository assigns the locked next version, it must set audit metadata to `publicationId`, assigned `version`, and `contentHash` before the single transaction inserts the audit; it must not contain the rich-text body.

- [ ] **Step 5: Implement strict route decoding**

Use a local generic helper in `watermark_policy.go`:

```go
func decodeWatermarkJSON[T any](c *gin.Context, limit int64) (T, error) {
	var value T
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil { return value, err }
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) { return value, errors.New("请求体只能包含一个 JSON 对象") }
	return value, nil
}
```

Use `256 << 10` for publication and `4 << 10` for preference. Map repository version conflict to `service.Conflict("水印规范已更新，请重新阅读后确认")`.

- [ ] **Step 6: Re-run focused tests and confirm GREEN**

Run the Step 3 command. Expected: all selected tests pass.

- [ ] **Step 7: Commit the API milestone**

```powershell
git add backend/internal/service/watermark_policy.go backend/internal/service/watermark_policy_test.go backend/internal/handler/watermark_policy.go backend/internal/handler/watermark_policy_test.go backend/cmd/server/main.go
git diff --cached --check
git commit -m "feat(api): 发布 AI 水印规范并同步账号偏好"
```

---

### Task 3: 闭集模型能力与任务创建冻结

**Files:**
- Modify: `backend/internal/service/provider_registry.go`
- Modify: `backend/internal/service/provider_registry_test.go`
- Modify: `backend/internal/service/admin.go`
- Create: `backend/internal/service/task_watermark.go`
- Create: `backend/internal/service/task_watermark_test.go`
- Modify: `backend/internal/repository/finance.go`
- Modify: `backend/internal/repository/provider_task_freeze_test.go`
- Modify: `backend/internal/service/storage_quota.go`
- Modify: `backend/internal/service/service.go`

**Interfaces:**
- Replaces `SupportsWatermark bool` with:

```go
WatermarkCapability model.WatermarkCapability `json:"watermarkCapability"`
```

in both `ProviderModelSpec` and `PublicProviderCapabilities`.
- Adds the same mandatory `watermarkCapability` field to each public `ChannelModel` item, so every selected image/video model has a backend-owned fact even when it is not managed by the Kuaizi registry.
- Adds:

```go
func (s *Service) taskWatermarkCapability(taskCapability string, order *model.BillingOrder) (model.WatermarkCapability, error)
```

- Extends repository transaction signatures:

```go
func (r *Repository) CreateTaskWithCreditReservation(task *model.Task, order *model.BillingOrder, policy ActiveTaskPolicy, watermark model.WatermarkCapability) error
func (r *Repository) CreateTaskWithActiveLimit(task *model.Task, policy ActiveTaskPolicy, watermark model.WatermarkCapability) error
func (r *Repository) RetryTaskWithBilling(userID, taskID string, order *model.BillingOrder, policy ActiveTaskPolicy, watermark model.WatermarkCapability) (*model.Task, error)
```

- [ ] **Step 1: Write failing closed-capability tests**

```go
func TestProviderRegistryRequiresExplicitWatermarkCapabilityForEveryMediaModel(t *testing.T) {
	descriptor := ProviderAdapterDescriptor{ProviderKind: "test", Family: "video", Models: []ProviderModelSpec{{ModelKey: "video-1", DisplayName: "Video", UpstreamMode: "video-1", Capability: "video"}}}
	_, err := NewProviderRegistry([]ProviderAdapterDescriptor{descriptor})
	require.ErrorContains(t, err, "watermark capability")
}

func TestFreezeTaskWatermarkUsesCurrentAccountFactInsideCreateTransaction(t *testing.T) {
	db, repo := providerFreezeFixture(t)
	publishAndEnable(t, repo, "user-1", "publication-1")
	task, order := billableTask("user-1")
	require.NoError(t, repo.CreateTaskWithCreditReservation(task, order, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled))
	require.Equal(t, model.WatermarkDirectiveWithoutWatermark, task.WatermarkDirective)
	require.NotNil(t, task.WatermarkParameterValue)
	require.False(t, *task.WatermarkParameterValue)
	require.Equal(t, "publication-1", task.WatermarkPolicyPublicationID)
}
```

- [ ] **Step 2: Write failing retry semantics tests**

```go
func TestUserRetryRecomputesWatermarkWhileWorkerRetryKeepsSnapshot(t *testing.T) {
	svc, task := failedControlledTask(t, true)
	disableWatermarkPreference(t, svc.repo, task.UserID)
	retried, err := svc.RetryTask(task.UserID, task.ID)
	require.NoError(t, err)
	require.Equal(t, model.WatermarkDirectiveWithWatermark, retried.WatermarkDirective)
	require.True(t, *retried.WatermarkParameterValue)

	claimed := mustClaimSameTask(t, svc.repo, retried.ID)
	require.Equal(t, model.WatermarkDirectiveWithWatermark, claimed.WatermarkDirective)
}
```

- [ ] **Step 3: Run focused tests and confirm RED**

```powershell
cd backend
go test ./internal/service ./internal/repository -run 'WatermarkCapability|FreezeTaskWatermark|UserRetryRecomputesWatermark' -count=1
```

Expected: build fails on the old bool capability and old repository signatures.

- [ ] **Step 4: Implement exact capability registry facts**

Set registry values:

```go
// controlled
Seedance 2.0 Fast, Seedance 2.0 Pro, Seedance 2.0 Mini, Seedance 2.5

// unsupported
GPT Image 2

// not_applicable
GPT 5.5, DeepSeek V4 Pro
```

For non-Kuaizi media channels use an exhaustive `(task capability, interface type)` switch in `task_watermark.go`:

```go
case capability == "video" && interfaceType == model.ChannelInterfaceNewAPIVideo,
	capability == "video" && interfaceType == model.ChannelInterfaceAIOpenVideoVolcengine,
	capability == "video" && interfaceType == model.ChannelInterfaceMiniMaxVideo:
	return model.WatermarkCapabilityControlled, nil
case capability == "image" && interfaceType == model.ChannelInterfaceOpenAIImage,
	capability == "image" && interfaceType == model.ChannelInterfaceAPIMartImage,
	capability == "video" && interfaceType == model.ChannelInterfaceXAIVideo,
	capability == "video" && interfaceType == model.ChannelInterfaceKlingVideo:
	return model.WatermarkCapabilityUnsupported, nil
default:
	return "", fmt.Errorf("媒体模型 %s 缺少明确的水印能力契约", channelModel.Model)
```

Do not infer from model names. Managed Kuaizi models must resolve through `kuaiziProviderModelSpec` and match the `ChannelModel` model key.

- [ ] **Step 5: Freeze account and publication facts in the task transaction**

Call `FreezeTaskWatermarkTx` after provider runtime freeze and before credit reservation/task insert. For unsupported models set `provider_default`, `Applied=false`, `Value=nil`. For controlled models set `Applied=true`; only an active preference sets value false and publication facts, all other statuses set value true with empty publication facts.

`RetryTaskWithBilling` recomputes and overwrites the six watermark fields before requeue. Worker claims and polling paths never modify them.

- [ ] **Step 6: Re-run focused tests and confirm GREEN**

Run the Step 3 command. Expected: all selected tests pass.

- [ ] **Step 7: Commit the frozen-task milestone**

```powershell
git add backend/internal/service/provider_registry.go backend/internal/service/provider_registry_test.go backend/internal/service/admin.go backend/internal/service/task_watermark.go backend/internal/service/task_watermark_test.go backend/internal/repository/finance.go backend/internal/repository/provider_task_freeze_test.go backend/internal/service/storage_quota.go backend/internal/service/service.go
git diff --cached --check
git commit -m "feat(task): 冻结账号级 AI 水印指令"
```

---

### Task 4: Provider 请求只消费冻结 directive

**Files:**
- Modify: `backend/internal/service/provider.go`
- Modify: `backend/internal/service/provider_ai_open_platform_volcengine_video.go`
- Modify: `backend/internal/service/provider_minimax_h3_video.go`
- Modify: `backend/internal/service/provider_kling_video.go`
- Modify: `backend/internal/service/provider_kuaizi_compatible_worker.go`
- Modify: `backend/internal/service/canvas_share.go`
- Modify tests: `backend/internal/service/provider_test.go`, `provider_kling_video_test.go`, `provider_kuaizi_compatible_worker_test.go`, `task_security_test.go`

**Interfaces:**
- `providerConfig` no longer has JSON `videoWatermark`.
- `canvasGenerationInput` gets an execution-only field:

```go
Watermark taskWatermarkRuntime `json:"-"`

type taskWatermarkRuntime struct {
	Capability model.WatermarkCapability
	Directive  model.WatermarkDirective
	Parameter  *bool
}
```

- `processCanvasGenerationTask` changes to:

```go
func (s *Service) processCanvasGenerationTask(ctx context.Context, task model.Task) (map[string]interface{}, error)
```

- [ ] **Step 1: Write failing request-body tests**

```go
func TestControlledProviderSendsFrozenWatermarkValue(t *testing.T) {
	for _, value := range []bool{true, false} {
		task := controlledVideoTask(value)
		body := runAgainstTLSRecorder(t, task)
		require.Equal(t, value, body["watermark"])
	}
}

func TestUnsupportedProviderOmitsWatermarkField(t *testing.T) {
	task := unsupportedKlingTask()
	body := runAgainstTLSRecorder(t, task)
	_, exists := body["watermark"]
	require.False(t, exists)
}

```

- [ ] **Step 2: Run focused tests and confirm RED**

```powershell
cd backend
go test ./internal/service -run 'ControlledProviderSendsFrozenWatermark|UnsupportedProviderOmitsWatermark|TaskInputRejectsClientWatermark' -count=1
```

Expected: controlled requests still read `VideoWatermark`, and unsupported paths reject or emit old values.

- [ ] **Step 3: Remove the client field and inject the task snapshot**

At the beginning of both legacy and Kuaizi execution, call:

```go
func taskWatermarkRuntimeFromTask(task model.Task) (taskWatermarkRuntime, error)
```

The function rejects inconsistent snapshots such as controlled+nil parameter, unsupported+non-nil parameter, or a directive/capability mismatch. It must not look up current account preference or current publication.

Removing `providerConfig.VideoWatermark` makes the existing Web field inert during this backend milestone; final strict rejection is intentionally deferred to Task 6 so the intermediate commit remains runnable. The field is never consulted after this step, so there is still only one behavioral decision path.

- [ ] **Step 4: Map exact provider fields**

Use conditional map insertion, never a fallback value:

```go
if input.Watermark.Parameter != nil {
	body["watermark"] = *input.Watermark.Parameter
}
```

MiniMax uses `aigc_watermark`. Kling and GPT Image 2 omit all watermark fields. Kuaizi Seedance uses `watermark`. Provider rejection remains a normal explicit provider failure; do not retry without the field.

- [ ] **Step 5: Re-run focused tests and confirm GREEN**

Run the Step 2 command. Expected: all selected tests pass and recorded JSON matches exact field presence/value.

- [ ] **Step 6: Commit the provider milestone**

```powershell
git add backend/internal/service/provider.go backend/internal/service/provider_ai_open_platform_volcengine_video.go backend/internal/service/provider_minimax_h3_video.go backend/internal/service/provider_kling_video.go backend/internal/service/provider_kuaizi_compatible_worker.go backend/internal/service/canvas_share.go backend/internal/service/provider_test.go backend/internal/service/provider_kling_video_test.go backend/internal/service/provider_kuaizi_compatible_worker_test.go backend/internal/service/task_security_test.go
git diff --cached --check
git commit -m "refactor(provider): 统一消费冻结水印指令"
```

---

### Task 5: 账号弹窗与后台 publication 编辑器

**Files:**
- Create: `web/src/services/api/watermark-policy.ts`
- Create: `web/src/components/account/ai-watermark-settings-modal.tsx`
- Create: `web/src/components/account/ai-watermark-settings-modal.css`
- Modify: `web/src/components/layout/site-account-actions.tsx`
- Create: `web/src/pages/admin/settings/ai-watermark-policy-editor.tsx`
- Modify: `web/src/pages/admin/settings/legal-settings-page.tsx`
- Modify: `web/src/pages/admin/admin-feature-workspace.css`
- Create: `web/test/ai-watermark-settings.test.tsx`
- Create: `web/test/ai-watermark-policy-admin.test.tsx`

**Interfaces:**
- Produces frontend types mirroring Task 2 exactly:

```ts
export type WatermarkPreferenceStatus = "disabled" | "active" | "policy_updated" | "policy_unavailable";

export type WatermarkPreferenceView = {
  removeWatermark: boolean;
  status: WatermarkPreferenceStatus;
  canEnable: boolean;
  acceptedAt: string | null;
  currentPolicy: {
    id: string;
    version: number;
    managementRuleRichText: string;
    watermarkPolicyUrl: string;
    contentHash: string;
    publishedAt: string;
  } | null;
};
```

- [ ] **Step 1: Write failing account modal DOM tests**

```tsx
test("头像菜单打开账号级弹窗并保存当前 publication", async () => {
  const api = watermarkApiFixture({ status: "disabled", canEnable: true, currentPolicy: policyV1 });
  renderAccountActions({ watermarkApi: api });
  await clickByText("AI 水印设置");
  expect(screen.getByRole("dialog", { name: "AI 生成内容水印管理规则" })).toBeTruthy();
  expect(screen.getByRole("link", { name: "水印规范" }).getAttribute("rel")).toBe("noopener noreferrer");
  await clickByLabel("去 AI 水印");
  await clickByText("保存设置");
  expect(api.puts).toEqual([{ removeWatermark: true, publicationId: policyV1.id }]);
});

test("保存期间 publication 变化时重新读取并保留开启草稿", async () => {
  const api = conflictWatermarkApiFixture(policyV1, policyV2);
  renderWatermarkModal({ api });
  await enableAndSave();
  expect(await screen.findByText("水印规范已更新，请重新阅读并确认")).toBeTruthy();
  expect(screen.getByRole("switch", { name: "去 AI 水印" }).getAttribute("aria-checked")).toBe("true");
});
```

- [ ] **Step 2: Write failing admin publication DOM tests**

```tsx
test("相同正文和 URL 重新发布前必须二次确认", async () => {
  const api = watermarkAdminApiFixture(policyV1);
  renderPolicyEditor({ api });
  await clickByText("发布新版本");
  expect(screen.getByText("继续发布会要求所有已开启账号重新确认")).toBeTruthy();
  await clickByText("确认发布新版本");
  expect(api.publications).toEqual([{ managementRuleRichText: policyV1.managementRuleRichText, watermarkPolicyUrl: policyV1.watermarkPolicyUrl }]);
});
```

- [ ] **Step 3: Run focused tests and confirm RED**

```powershell
cd web
bun test test/ai-watermark-settings.test.tsx test/ai-watermark-policy-admin.test.tsx
```

Expected: modules and UI do not exist.

- [ ] **Step 4: Implement strict API parsing and conflict error**

`watermark-policy.ts` must reject unknown statuses and missing fields. Export:

```ts
export class WatermarkPolicyConflictError extends Error {}
export const watermarkPreferenceQueryKey = ["me", "watermark-preference"] as const;
export const adminWatermarkPolicyQueryKey = ["admin", "legal", "ai-watermark-policy"] as const;
```

Only map HTTP 409 to `WatermarkPolicyConflictError`; other failures retain the backend `msg`.

- [ ] **Step 5: Build the user modal with the approved layout**

Use Ant `Modal` with `destroyOnHidden`, `keyboard`, close button and default focus trap. Render backend HTML only through existing `LegalRichTextViewer`; never use raw `dangerouslySetInnerHTML`. The external link is:

```tsx
<a target="_blank" rel="noopener noreferrer" href={policy.watermarkPolicyUrl}>水印规范</a>
```

The modal must implement loading, load failure, disabled, active, policy_updated and policy_unavailable. A first-load error disables switch/save. A failed save preserves the draft. A successful save updates the query cache and closes only after the server response.

- [ ] **Step 6: Build the independent admin editor**

Keep the existing three legal documents on their current joint save flow. Mount `AIWatermarkPolicyEditor` as a separate `SettingsSectionCard` with its own query/mutation, rich-text draft, HTTPS URL field, current version tag, publication metadata and preview link. Same-content publish opens an Ant confirmation modal; changed content publishes directly.

- [ ] **Step 7: Re-run focused tests and confirm GREEN**

Run the Step 3 command. Expected: both files pass without React `act` warnings.

- [ ] **Step 8: Commit the UI milestone**

```powershell
git add web/src/services/api/watermark-policy.ts web/src/components/account/ai-watermark-settings-modal.tsx web/src/components/account/ai-watermark-settings-modal.css web/src/components/layout/site-account-actions.tsx web/src/pages/admin/settings/ai-watermark-policy-editor.tsx web/src/pages/admin/settings/legal-settings-page.tsx web/src/pages/admin/admin-feature-workspace.css web/test/ai-watermark-settings.test.tsx web/test/ai-watermark-policy-admin.test.tsx
git diff --cached --check
git commit -m "feat(web): 增加账号级 AI 水印设置弹窗"
```

---

### Task 6: 删除本地/节点双轨并展示 unsupported 事实

**Files:**
- Delete: `web/src/pages/settings/watermark-settings.tsx`
- Modify: `web/src/pages/settings/index.tsx`
- Modify: `web/src/styles/globals.css`
- Modify: `web/src/stores/use-config-store.ts`
- Modify: `web/src/services/api/provider-accounts.ts`
- Modify: `web/src/services/api/video.ts`
- Modify: `web/src/lib/ai/system-provider-config.ts`
- Modify: `web/src/types/canvas.ts`
- Modify: `web/src/components/canvas/canvas-assistant-panel.tsx`
- Modify: `web/src/components/canvas/canvas-config-node-panel.tsx`
- Modify: `web/src/components/canvas/canvas-node-prompt-panel.tsx`
- Modify: `web/src/lib/canvas/canvas-project-generation.ts`
- Modify: `web/src/pages/canvas/canvas-media-generation-executors.ts`
- Modify: `web/src/pages/canvas/use-canvas-generation-retry.ts`
- Modify: `web/src/components/canvas/canvas-image-generation-settings.tsx`
- Modify: `web/src/components/canvas/canvas-video-settings-popover.tsx`
- Modify: `web/test/canvas-image-generation-settings.test.ts`
- Modify: `web/test/video-model-capabilities.test.ts`
- Modify: `web/test/kuaizi-provider-settings.test.tsx`
- Modify: `web/test/kuaizi-provider-interactions.test.tsx`
- Modify: `backend/internal/service/task_watermark.go`
- Modify: `backend/internal/service/task_security_test.go`
- Modify tests: `web/test/kuaizi-provider-settings.test.tsx`, `kuaizi-provider-interactions.test.tsx`, `video-model-capabilities.test.ts`, `canvas-image-generation-settings.test.ts`, plus directly affected canvas tests.

**Interfaces:**
- Replaces frontend `supportsWatermark: boolean` with:

```ts
export type WatermarkCapability = "controlled" | "unsupported" | "not_applicable";
watermarkCapability: WatermarkCapability;
```

- Removes `AiConfig.videoWatermark` and `CanvasNodeMetadata.watermark`.

- [ ] **Step 1: Change tests first to assert the hard cut**

```ts
test("系统任务请求和节点 metadata 均不包含水印字段", () => {
  const request = buildCanvasTaskRequest(videoNodeFixture());
  expect(JSON.stringify(request)).not.toContain("watermark");
  expect(JSON.stringify(request)).not.toContain("videoWatermark");
});

test("unsupported 模型只展示供应商决定说明", () => {
  renderVideoSettings({ watermarkCapability: "unsupported" });
  expect(screen.getByText("该模型不支持水印控制，结果由模型服务商决定")).toBeTruthy();
  expect(screen.queryByRole("switch", { name: /水印/ })).toBeNull();
});
```

Add the backend structural hard-cut test in this same milestone:

```go
func TestTaskInputRejectsClientWatermarkFields(t *testing.T) {
	for _, input := range []map[string]any{
		{"watermark": true},
		{"config": map[string]any{"videoWatermark": "false"}},
		{"metadata": map[string]any{"watermark": "true"}},
	} {
		require.Error(t, validateTaskWatermarkInput(input))
	}
}
```

- [ ] **Step 2: Run affected tests and confirm RED**

```powershell
cd web
bun test test/kuaizi-provider-settings.test.tsx test/kuaizi-provider-interactions.test.tsx test/video-model-capabilities.test.ts test/canvas-image-generation-settings.test.ts
cd ..\backend
go test ./internal/service -run 'TaskInputRejectsClientWatermarkFields' -count=1
```

Expected: DTO fixtures still use the bool, and request/metadata builders still emit old watermark fields.

- [ ] **Step 3: Delete every production read/write of the old facts**

Delete the settings section, file and its `.settings-watermark-*` global CSS; remove persisted default, normalization and selectors; remove assistant tool schema input; remove node metadata transforms; remove direct video API fields. Do not migrate historical canvas JSON and do not read it.

Use this repository scan as a completion gate:

```powershell
rg -n "videoWatermark|metadata\?\.watermark|metadata\.watermark|watermark: stringOptional\(input\.watermark\)|section=watermark" web/src backend/internal/service
```

Expected: no output. Allowed remaining matches are domain/API names under `watermark-policy`, task frozen fields, provider request mapping and the unsupported explanatory copy.

- [ ] **Step 4: Add unsupported-only explanation**

Read `watermarkCapability` from the selected dynamic `providerCapabilities`. Render the read-only sentence only for `unsupported`; `controlled` adds no node control. Missing/unknown capability must remain a parser or model-catalog error, not a hidden default.

- [ ] **Step 5: Re-run affected tests and confirm GREEN**

Run the Step 2 command plus the scan in Step 3. Expected: tests pass and the scan returns no forbidden production path.

- [ ] **Step 6: Commit the hard-cut milestone**

```powershell
git add -A web/src/pages/settings/watermark-settings.tsx web/src/pages/settings/index.tsx web/src/styles/globals.css web/src/stores/use-config-store.ts web/src/services/api/provider-accounts.ts web/src/services/api/video.ts web/src/lib/ai/system-provider-config.ts web/src/types/canvas.ts web/src/components/canvas/canvas-assistant-panel.tsx web/src/components/canvas/canvas-config-node-panel.tsx web/src/components/canvas/canvas-node-prompt-panel.tsx web/src/lib/canvas/canvas-project-generation.ts web/src/pages/canvas/canvas-media-generation-executors.ts web/src/pages/canvas/use-canvas-generation-retry.ts web/src/components/canvas/canvas-image-generation-settings.tsx web/src/components/canvas/canvas-video-settings-popover.tsx web/test/canvas-image-generation-settings.test.ts web/test/video-model-capabilities.test.ts web/test/kuaizi-provider-settings.test.tsx web/test/kuaizi-provider-interactions.test.tsx backend/internal/service/task_watermark.go backend/internal/service/task_security_test.go
git add web/test/kuaizi-provider-settings.test.tsx web/test/kuaizi-provider-interactions.test.tsx web/test/video-model-capabilities.test.ts web/test/canvas-image-generation-settings.test.ts
git diff --cached --check
git commit -m "refactor(web): 删除节点级水印配置双轨"
```

---

### Task 7: PostgreSQL、全量门禁、真实浏览器与本地镜像

**Files:**
- Modify if required by final release notes: `CHANGELOG.md`
- No permanent test script or browser fixture may be added in this task.

**Interfaces:**
- Consumes all prior tasks.
- Produces one commercially verified release candidate and a rebuilt local image using the canonical `.local/data` database only.

- [ ] **Step 1: Add exact PostgreSQL repository cases to the existing runner gate**

The existing Go tests must include exact names:

```go
func TestPostgresWatermarkPublicationSerializesConcurrentVersions(t *testing.T)
func TestPostgresWatermarkPreferenceAndConsentRollbackTogether(t *testing.T)
func TestPostgresTaskCreateFreezesWatermarkWithBillingAndProviderRuntime(t *testing.T)
```

Use `scripts/tests/run-payment-integration.sh --require` for exact discovery; do not create another integration script.

- [ ] **Step 2: Run focused and full backend gates**

```powershell
cd backend
go test ./internal/database ./internal/repository ./internal/service ./internal/handler -run 'Watermark' -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

Expected: all commands exit 0.

- [ ] **Step 3: Run the isolated PostgreSQL gate and confirm cleanup**

From the repository root in Git Bash:

```bash
scripts/tests/run-payment-integration.sh \
  --run '^(TestPostgresWatermarkPublicationSerializesConcurrentVersions|TestPostgresWatermarkPreferenceAndConsentRollbackTogether|TestPostgresTaskCreateFreezesWatermarkWithBillingAndProviderRuntime)$' \
  --require TestPostgresWatermarkPublicationSerializesConcurrentVersions \
  --require TestPostgresWatermarkPreferenceAndConsentRollbackTogether \
  --require TestPostgresTaskCreateFreezesWatermarkWithBillingAndProviderRuntime
```

Expected: all three exact tests pass and the runner removes its PostgreSQL/Redis containers, network and volumes. It must not point to `.local/data`.

- [ ] **Step 4: Run Web tests, build, formatting and bundle budgets**

```powershell
cd web
bun test
bun run build
bunx prettier --check src test
cd ..
git diff --check
```

Expected: all tests and all three Vite bundle budgets pass; formatting and diff checks exit 0.

- [ ] **Step 5: Run real-browser acceptance without adding scripts**

Use the local authenticated test account and in-app browser/Playwright tool to verify these exact flows:

1. Admin publishes v1 with rich text and HTTPS link; page shows v1, publisher and timestamp.
2. User menu opens the dark centered modal; link opens a new tab with safe attributes; Escape and close work; 390px viewport keeps switch/save reachable.
3. User enables v1, reloads, and another browser session reads `active`.
4. Admin republishes identical content after the warning; user modal becomes `policy_updated` and visually off.
5. A stale v1 save receives 409, refetches v2 and preserves the user draft.
6. Image and video node parameter panels contain no watermark switch; unsupported model shows the supplier-decision copy.
7. Create controlled tasks in both enabled and disabled states, inspect task detail/log and recorded TLS test facts for exact false/true values; inspect an unsupported task for omitted field.

No screenshots, fixture JSON or temporary scripts are committed. Save optional QA screenshots under ignored `qa-artifacts/` only.

- [ ] **Step 6: Rebuild the local images against the canonical demo database**

Before rebuild, snapshot the exact file list, sizes and UTC mtimes under the main repository `.local/data`. Then run from this worktree:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/local-compose.ps1 up -d --build --wait
docker compose -p hmaigc-local ps
curl.exe -fsS http://127.0.0.1:3000/api/health
curl.exe -fsS http://127.0.0.1:3000/
```

Expected: backend and web are healthy, both HTTP checks succeed, bind mount resolves to the main repository `.local/data`, and the pre/post data file list, sizes and mtimes are unchanged except normal application writes caused by the explicit browser acceptance steps. Do not run `down -v`.

- [ ] **Step 7: Perform the single review and concentrated repair allowance**

Review, in order:

1. `docs/superpowers/specs/2026-08-13-account-ai-watermark-policy-design.md`.
2. this plan.
3. `git diff` from the plan parent.
4. backend DTO ↔ Web parser field-by-field equality.
5. publication/preference/task transaction order.
6. provider field presence/value and secret/log boundaries.
7. migration-only database impact and `.local/data` mount evidence.

One independent review plus one concentrated fix and one focused re-review is the maximum. If the re-review finds a new cross-module Critical/Important, or production files exceed 30, stop and return to architecture instead of continuing patch rounds.

- [ ] **Step 8: Update changelog and create the final delivery commit only if needed**

If Tasks 1–6 already formed complete reviewed commits, update `CHANGELOG.md` with the user-visible account watermark behavior and commit only the changelog:

```powershell
git add CHANGELOG.md
git diff --cached --check
git commit -m "docs(release): 记录账号级 AI 水印设置"
```

If review required code fixes, stage only those focused files plus `CHANGELOG.md`, run the affected focused gate and one final complete confirmation, then commit:

```powershell
git diff --cached --check
git commit -m "fix(settings): 收口账号级 AI 水印事实链"
```

Do not push, tag, merge, rebase or publish a GitHub Release without a new explicit user instruction.

---

## Final Acceptance Checklist

- [ ] 管理规则富文本与水印规范 HTTPS URL 共同发布、不可变且版本单调。
- [ ] 同内容重新发布有二次确认并使旧同意失效。
- [ ] 账号默认关闭；开启、关闭、重复保存、未发布、版本冲突均符合 API 契约。
- [ ] preference 与 consent 同事务，任务水印与计费/provider runtime 同事务。
- [ ] user retry 重算快照，worker/internal retry 不漂移。
- [ ] controlled provider 精确发送 true/false；unsupported 严格省略；unknown 显式失败。
- [ ] 本地 store、旧 settings 页面、节点 metadata、assistant schema 和客户端 provider 请求均无水印输入。
- [ ] 账号弹窗符合截图参考的层级但不复制第三方品牌，桌面/小屏/键盘可用。
- [ ] 后台 publication 编辑器与现有三份法律文档职责独立。
- [ ] Go、race、vet、build、Web test/build/format/budget、隔离 PostgreSQL 与浏览器验收全绿。
- [ ] 本地镜像已重建，使用主仓唯一 `.local/data`，没有遗留测试容器或卷。
- [ ] 最终 diff 无密钥、cookie、完整规范正文日志、构建产物或临时脚本。
