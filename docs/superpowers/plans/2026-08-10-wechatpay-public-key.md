# 微信支付公钥单一路径 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** 将微信 Native 支付硬切到包含微信支付公钥 ID、公钥验签、序列号绑定和 Request-ID 安全日志的 API v3 单一路径。

**Architecture:** 后端把微信与支付宝配置拆成不同的强类型契约，微信 connector 和 webhook 只接受微信专属配置，所有应答/回调先绑定 PUB_KEY_ID_... 再验签；provider dispatch 只传完整 paymentSettingValue，不再使用语义混杂的共享 channel。前端使用独立微信/支付宝 DTO 与纯表单映射模块，旧微信 platformPublicKey 字段被硬删除。

**Tech Stack:** Go 1.24、Gin、GORM、RSA2、微信支付 API v3、React 19、TypeScript 7、Ant Design 6、Bun test。

## Global Constraints

- 微信支付只支持新版微信支付公钥模式，不保留平台证书模式或自动回退。
- 微信公钥 ID 必须使用 PUB_KEY_ID_...，并绑定所有出站应答与入站回调验签。
- 商户私钥、微信支付公钥和 API v3 密钥继续加密保存且不回显；日志不得包含凭据、二维码、bearer、商户订单号或回调正文。
- 支付宝继续使用 platformPublicKey，其签名、配置和接口行为不得改变。
- 不新增数据库表或列；旧微信 JSON 字段不做兼容迁移，配置不完整时显式不可用。
- 所有生产代码必须先有会因缺失目标能力而失败的测试；每个 RED 必须实际运行并记录预期失败。
- 不修改版本号、不发布、不推送、不启用真实生产微信渠道。

---

### Task 1: 后端微信公钥协议硬切

**Files:**
- Modify: backend/internal/service/payment.go
- Modify: backend/internal/service/payment_connector.go
- Modify: backend/internal/service/payment_webhook.go
- Modify: backend/internal/handler/payment.go
- Modify: backend/internal/handler/payment_test.go
- Modify: backend/internal/service/commercial_membership_test.go
- Modify: backend/internal/service/payment_setting_security_test.go
- Modify: backend/internal/service/payment_connector_create_test.go
- Modify: backend/internal/service/payment_webhook_test.go
- Modify: backend/internal/service/payment_checkout_view_test.go
- Modify: backend/internal/service/payment_postgres_test.go
- Create: backend/internal/service/payment_wechat_public_key_test.go

**Interfaces:**

Produces separate request types:

~~~go
type WechatPaymentChannelSettingRequest struct {
    Enabled bool
    AppID, MerchantID, MerchantSerialNo string
    MerchantPrivateKey string
    WechatpayPublicKeyID string
    WechatpayPublicKey string
    APIv3Key string
    NotifyURL, GatewayURL string
}

type AlipayPaymentChannelSettingRequest struct {
    Enabled bool
    AppID, MerchantID string
    MerchantPrivateKey, PlatformPublicKey string
    NotifyURL, GatewayURL string
}
~~~

JSON names are appId, merchantId, merchantSerialNo, merchantPrivateKey, wechatpayPublicKeyId, wechatpayPublicKey, apiV3Key, notifyUrl and gatewayUrl. Public response types are PublicWechatPaymentChannelSetting and PublicAlipayPaymentChannelSetting; only the WeChat response exposes wechatpayPublicKeyId and hasWechatpayPublicKey, never PEM/API v3 values. Internal values are wechatPaymentChannelSettingValue and alipayPaymentChannelSettingValue.

Provider dispatch consumes the full typed setting:

~~~go
func createProviderPayment(transaction *model.PaymentTransaction, order paymentOrderReference, setting paymentSettingValue) (string, error)
func queryProviderPayment(transaction *model.PaymentTransaction, setting paymentSettingValue) (providerPaymentFact, error)
func closeProviderPayment(transaction *model.PaymentTransaction, setting paymentSettingValue) error
func doSignedWechatRequest(req *http.Request, operation string, channel wechatPaymentChannelSettingValue) ([]byte, int, http.Header, error)
~~~

Fixed operation names are native_create, query and close. WechatPaymentWebhookHeaders gains Serial, populated from Wechatpay-Serial.

- [ ] **Step 1: Write failing configuration tests**

Create payment_wechat_public_key_test.go using real RSA keys. Verify enabled WeChat rejects: missing public key ID, non-PUB_KEY_ID_ value, missing/invalid public key, invalid merchant private key, and API v3 key with wrong length or non-alphanumeric ASCII. Verify a disabled partial channel saves with Ready=false. Verify a valid save returns the public key ID and configured booleans without PEM/API v3 values.

Representative assertion:

~~~go
candidate := validWechatPublicKeyPaymentSetting(t)
candidate.Wechat.WechatpayPublicKeyID = ""
if _, err := svc.UpdatePaymentSetting(admin, candidate); err == nil {
    t.Fatal("enabled WeChat accepted a missing public key ID")
}
~~~

- [ ] **Step 2: Run configuration RED**

Run:

~~~powershell
cd backend
go test ./internal/service -run 'TestWechatPaymentSettingRequiresPublicKeyMode|TestWechatPaymentSettingPublicProjection' -count=1
~~~

Expected: compile/assertion failure because the new typed fields and validation do not exist.

- [ ] **Step 3: Write failing connector and log tests**

Exercise native_create, query and close with the real RoundTripper fixture. Assert every request contains Wechatpay-Serial: PUB_KEY_ID_3000000001. A correctly signed response carrying another serial must fail. Capture the standard logger in a non-parallel test and assert it contains only provider=wechat, fixed operation, status, quoted request_id and request_id_missing; assert absence of Authorization, merchant order, code_url, bearer and response body. Cover valid 2xx, signed non-2xx, and missing Request-ID.

- [ ] **Step 4: Run connector RED**

~~~powershell
cd backend
go test ./internal/service -run 'TestWechat(RequestsBindConfiguredPublicKeyID|ResponseRejectsMismatchedPublicKeyID|ResponseLogsOnlySafeRequestMetadata)' -count=1
~~~

Expected: failure because requests omit Wechatpay-Serial, responses do not bind the serial, and no Request-ID fact is logged.

- [ ] **Step 5: Write and run failing webhook serial tests**

Add a service test proving a correctly signed webhook with empty/foreign Wechatpay-Serial fails before any PaymentWebhookEvent, subscription, credit ledger or paid transaction is created. Add a handler test proving the incoming header reaches WechatPaymentWebhookHeaders.Serial.

Run:

~~~powershell
cd backend
go test ./internal/service ./internal/handler -run 'TestWechatWebhookRequiresConfiguredPublicKeyID|Test.*Wechat.*Webhook' -count=1
~~~

Expected: failure because handler and service currently ignore the serial.

- [ ] **Step 6: Implement typed configuration and strict validation**

Replace shared request/public/internal structs with the typed interfaces above. Add explicit merge/readiness functions for WeChat and Alipay. Enabled WeChat requires every field; disabled WeChat validates each non-empty field but permits incomplete entry. Enforce PUB_KEY_ID_ plus non-empty suffix, RSA-parsable private/public PEM, and exactly 32 ASCII letters/digits for API v3. Encrypt the WeChat private key, public key and API v3 key. Remove old WeChat platformPublicKey decoding and fallback.

- [ ] **Step 7: Implement request/response serial binding and safe logs**

Set the request header:

~~~go
req.Header.Set("Wechatpay-Serial", channel.WechatpayPublicKeyID)
~~~

After receiving HTTP headers, log exactly one safe fact:

~~~go
requestID := strings.TrimSpace(headers.Get("Request-ID"))
log.Printf(
    "payment_provider_response provider=wechat operation=%s status=%d request_id=%q request_id_missing=%t",
    operation,
    status,
    truncateRunes(requestID, 160),
    requestID == "",
)
~~~

Require response Wechatpay-Serial to exactly equal the configured public key ID before RSA verification. Never log URI, response body, provider error text or merchant order.

- [ ] **Step 8: Implement webhook serial binding**

Map c.GetHeader("Wechatpay-Serial") in the Gin handler. Check exact serial equality before timestamp/signature/decryption/identity/amount/fulfillment. Do not infer a key from the incoming header and do not fall back.

- [ ] **Step 9: Update backend fixtures to the hard-cut contract**

All WeChat fixtures use:

~~~go
WechatpayPublicKeyID: "PUB_KEY_ID_3000000001",
WechatpayPublicKey: publicKeyPEM,
APIv3Key: strings.Repeat("k", 32),
~~~

Alipay fixtures remain PlatformPublicKey. Signed WeChat response/webhook helpers set the configured serial. No helper may accept the old WeChat field.

- [ ] **Step 10: Run backend GREEN gates**

~~~powershell
cd backend
go test ./internal/service ./internal/handler -run 'Test.*(Wechat|PaymentSetting)' -count=1
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
~~~

Expected: all exit 0 and targeted tests are not skipped.

- [ ] **Step 11: Commit Task 1**

Stage only Task 1 backend files, run staged diff/secret checks, and commit:

~~~text
feat(payment): 微信支付公钥 - 硬切新版验签与 Request-ID 审计
~~~

---

### Task 2: 管理后台微信公钥配置硬切

**Files:**
- Modify: web/src/services/api/payment.ts
- Create: web/src/pages/admin/settings/payment-settings-form.ts
- Modify: web/src/pages/admin/settings/payment-settings-page.tsx
- Create: web/test/payment-settings-public-key-contract.test.ts

**Interfaces:**

Create AdminWechatPaymentChannelSetting and WechatPaymentChannelSettingInput with wechatpayPublicKeyId, wechatpayPublicKey/hasWechatpayPublicKey. Create AdminAlipayPaymentChannelSetting and AlipayPaymentChannelSettingInput with platformPublicKey/hasPlatformPublicKey and no WeChat-only fields. payment-settings-form.ts exports PaymentFormValues, toPaymentFormValues, toPaymentSettingRequest and paymentFormValuesEqual.

- [ ] **Step 1: Write failing DTO and pure mapping tests**

Import the real API types and mapping module. Assert the WeChat form emits merchantSerialNo, wechatpayPublicKeyId, wechatpayPublicKey and apiV3Key and has no platformPublicKey own property. Assert Alipay emits platformPublicKey and no WeChat-only property. Assert configured secrets map to blank inputs and unchanged blanks do not mark the form dirty.

- [ ] **Step 2: Run mapping RED**

~~~powershell
cd web
bun test test/payment-settings-public-key-contract.test.ts
~~~

Expected: compile failure because the separate DTOs and mapping module do not exist.

- [ ] **Step 3: Add a failing page wiring contract**

Parse the actual page source. Assert a required “微信支付公钥 ID” input with PUB_KEY_ID_... placeholder, a secret “微信支付公钥” input, and the exact merchant-platform source hint. Assert the WeChat subtree has no platformPublicKey while Alipay retains it. The test must also execute the pure mapping so source text alone cannot pass.

- [ ] **Step 4: Run page RED**

Run the focused Bun test again. Expected: failure specifically identifies the old shared platform-public-key UI.

- [ ] **Step 5: Implement separate DTOs and form mapping**

Hard-cut payment.ts to provider-specific types. The pure mapping module trims strings and emits only valid provider fields, with no index signature and no any. Preserve secret blank-after-save and dirty-state behavior.

- [ ] **Step 6: Implement the administrator form**

Add the WeChat public-key ID input and WeChat public-key secret input, using hasWechatpayPublicKey. Update the description to “账户中心 → API安全 → 微信支付公钥”. Keep Alipay on its platform-public-key field. Preserve descriptive class names, validation-on-enable and Ant Design behavior.

- [ ] **Step 7: Run frontend GREEN gates**

~~~powershell
cd web
bun test test/payment-settings-public-key-contract.test.ts
bun test
bun run build
bunx prettier --check src/services/api/payment.ts src/pages/admin/settings/payment-settings-form.ts src/pages/admin/settings/payment-settings-page.tsx test/payment-settings-public-key-contract.test.ts
~~~

Expected: zero failures; TypeScript, Vite and bundle budgets pass.

- [ ] **Step 8: Commit Task 2**

Stage only the four Task 2 files, inspect staged diff/check, and commit:

~~~text
feat(admin): 微信支付配置 - 增加公钥 ID 与新版公钥字段
~~~

---

### Task 3: 文档与资金链路最终验收

**Files:**
- Modify: CHANGELOG.md
- Modify: docs/content/docs/pending-test.mdx
- Verify: .github/workflows/publish-images.yml
- Verify: scripts/tests/run-payment-integration.sh

- [ ] **Step 1: Update delivery facts**

Add one concise Unreleased “支付可靠性” bullet covering public-key ID binding, response/webhook serial verification and safe Request-ID logs. Update pending-test to require visible PUB_KEY_ID_..., secret non-disclosure, Wechatpay-Serial on create/query/close, foreign serial rejection, and a real low-value payment with usable Request-ID but no credentials/QR/bearer in logs.

- [ ] **Step 2: Run mandatory PostgreSQL/Redis payment matrix**

~~~powershell
& 'C:\Program Files\Git\bin\bash.exe' scripts/tests/run-payment-integration.sh --all
~~~

Expected: PostgreSQL 17 and Redis 7 healthy; database/repository/service/handler payment suites pass without skip; containers, volumes and network are cleaned.

- [ ] **Step 3: Run fresh final gates**

~~~powershell
Push-Location backend
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
Pop-Location

Push-Location web
bun install --frozen-lockfile
bun test
bun run build
Pop-Location
~~~

Expected: every command exits 0, no lockfile drift, no skipped payment tests.

- [ ] **Step 4: Perform explicit final review**

Compare the approved spec, this plan, actual diff and API JSON. Confirm: no production WeChat platformPublicKey; no WeChat API request omits Wechatpay-Serial; webhook checks serial before fulfillment; logs contain no URI/body/order/QR/bearer/credential; Alipay remains unchanged; no schema migration, secret, build output or unrelated file exists.

- [ ] **Step 5: Commit documentation**

Stage only CHANGELOG.md and docs/content/docs/pending-test.mdx, run staged diff/secret checks, and commit:

~~~text
docs(payment): 微信支付公钥 - 同步配置与真实商户验收边界
~~~

- [ ] **Step 6: Report external acceptance boundary**

Report commits and fresh command evidence. State that real WeChat Native payment remains unverified until merchant certificate, AppID binding, public key ID, public key and API v3 key are configured and the user authorizes a controlled low-value production payment. Never call the channel connected before that evidence exists.
