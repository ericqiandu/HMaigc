package repository

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestFreezeProviderTaskRuntimeUsesActiveVersions(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	now := time.Now().UTC()
	seedProviderRuntime(t, db, now, "healthy")
	if err := db.Create(&model.CreditAccount{UserID: "user", AvailableMicrocredits: 100}).Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "user", Capability: "video", Status: model.TaskStatusQueued}
	order := &model.BillingOrder{ID: "order", UserID: "user", TaskID: task.ID, ChannelModelID: "channel-model", AmountMicrocredits: 10, Status: model.BillingStatusReserved}

	if err := New(db).CreateTaskWithCreditReservation(task, order, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled); err != nil {
		t.Fatal(err)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ProviderAccountID != "account" || stored.ProviderEndpointVersionID != "endpoint-v1" || stored.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("frozen runtime = %#v", stored)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.ProviderEndpointVersionID != "endpoint-v1" || storedOrder.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("billing runtime = %#v", storedOrder)
	}

	if err := db.Model(&model.ProviderEndpointVersion{}).Where("id = ?", "endpoint-v1").Update("status", "retired").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ProviderCredentialVersion{}).Where("id = ?", "key-v1").Update("status", "retired").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ProviderEndpointVersionID != "endpoint-v1" || stored.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("frozen runtime changed after rotation: %#v", stored)
	}
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.ProviderEndpointVersionID != "endpoint-v1" || storedOrder.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("billing runtime changed after rotation: %#v", storedOrder)
	}
}

func TestResumeTaskWithUncertainBillingPreservesExistingProviderRequest(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	now := time.Now().UTC()
	task := model.Task{
		ID: "resume-image", UserID: "user", Type: "canvas_image", Capability: "image", Status: model.TaskStatusFailed,
		Stage: "生成失败", Progress: 35, Error: "HTTP 500", BillingOrderID: "resume-order",
		ProviderRequestID: "kz-cgt-existing", ProviderAccountID: "account", ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1",
		CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: "resume-order", UserID: "user", TaskID: task.ID, IdempotencyKey: "resume-order", Status: model.BillingStatusUncertain,
		ProviderRequestID: task.ProviderRequestID, AmountMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}

	resumed, err := New(db).ResumeTaskWithUncertainBilling("user", task.ID, ActiveTaskPolicy{Unlimited: true})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != model.TaskStatusQueued || resumed.ProviderRequestID != task.ProviderRequestID || resumed.BillingOrderID != order.ID || resumed.PollStage != "provider_resume" {
		t.Fatalf("resumed task = %#v", resumed)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.Status != model.BillingStatusUncertain || storedOrder.ProviderRequestID != task.ProviderRequestID {
		t.Fatalf("billing order = %#v", storedOrder)
	}
}

func TestFailedKuaiziMediaTaskSchedulesFrozenBillingReconciliation(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	now := time.Now().UTC()
	task := model.Task{
		ID: "failed-media", UserID: "user", Status: model.TaskStatusRunning, Stage: "生成失败", Error: "upstream failed",
		BillingOrderID: "failed-media-order", ProviderRequestID: "provider-media-task", LeaseOwner: "worker", LeaseGeneration: 1,
		LeaseToken: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", CreatedAt: now, UpdatedAt: now,
	}
	order := model.BillingOrder{
		ID: task.BillingOrderID, UserID: task.UserID, TaskID: task.ID, IdempotencyKey: task.BillingOrderID,
		BillingMode: "fixed_request", Status: model.BillingStatusRunning, ProviderRequestID: task.ProviderRequestID,
		ProviderEndpointVersionID: "endpoint-v1", ProviderCredentialVersionID: "key-v1", AmountMicrocredits: 1_000_000,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := New(db).FinalizeFailedTaskAndBilling(&task, FailedTaskBillingUncertain, "上游结果待核对"); err != nil {
		t.Fatal(err)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusUncertain || stored.NextReconcileAt == nil || !stored.NextReconcileAt.After(now) {
		t.Fatalf("scheduled billing = %#v", stored)
	}
}

func TestMarkBillingUncertainSchedulesFrozenProviderFact(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	now := time.Now().UTC()
	order := model.BillingOrder{
		ID: "uncertain-media-order", UserID: "user", IdempotencyKey: "uncertain-media-order", BillingMode: "fixed_request",
		Status: model.BillingStatusRunning, ProviderRequestID: "provider-media-task", ProviderEndpointVersionID: "endpoint-v1",
		ProviderCredentialVersionID: "key-v1", AmountMicrocredits: 1_000_000, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatal(err)
	}
	if err := New(db).MarkBillingUncertain(order.ID, "结果待核对"); err != nil {
		t.Fatal(err)
	}
	var stored model.BillingOrder
	if err := db.First(&stored, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.BillingStatusUncertain || stored.NextReconcileAt == nil || !stored.NextReconcileAt.After(now) {
		t.Fatalf("scheduled billing = %#v", stored)
	}
}

func TestFreezeProviderTaskRuntimeRejectsUnhealthyCredential(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	seedProviderRuntime(t, db, time.Now().UTC(), "unavailable")
	if err := db.Create(&model.CreditAccount{UserID: "user", AvailableMicrocredits: 100}).Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task", UserID: "user", Capability: "video", Status: model.TaskStatusQueued}
	order := &model.BillingOrder{ID: "order", UserID: "user", TaskID: task.ID, ChannelModelID: "channel-model", AmountMicrocredits: 10, Status: model.BillingStatusReserved}

	if err := New(db).CreateTaskWithCreditReservation(task, order, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled); err == nil {
		t.Fatal("unhealthy credential was accepted")
	}
	if task.ProviderAccountID != "" || task.ProviderEndpointVersionID != "" || task.ProviderCredentialVersionID != "" {
		t.Fatalf("failed freeze mutated task: %#v", task)
	}
	var taskCount, orderCount int64
	if err := db.Model(&model.Task{}).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", "user").Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || orderCount != 0 || account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != 0 {
		t.Fatalf("failed freeze left persistent facts: tasks=%d orders=%d account=%#v", taskCount, orderCount, account)
	}
}

func TestRetryTaskFreezesCurrentProviderRuntimeIntoTaskAndNewBilling(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	now := time.Now().UTC()
	seedProviderRuntime(t, db, now, "healthy")
	if err := db.Create(&model.CreditAccount{UserID: "retry-user", AvailableMicrocredits: 100, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "retry-task", UserID: "retry-user", Capability: "video", Status: model.TaskStatusFailed, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	order := &model.BillingOrder{
		ID: "retry-order", UserID: task.UserID, TaskID: task.ID, IdempotencyKey: "retry-order", ChannelModelID: "channel-model",
		BillingMode: "per_second", AmountMicrocredits: 10, Status: model.BillingStatusReserved, CreatedAt: now, UpdatedAt: now,
	}
	retried, err := New(db).RetryTaskWithBilling(task.UserID, task.ID, order, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ProviderAccountID != "account" || retried.ProviderEndpointVersionID != "endpoint-v1" || retried.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("retried runtime = %#v", retried)
	}
	var storedOrder model.BillingOrder
	if err := db.First(&storedOrder, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedOrder.ProviderEndpointVersionID != "endpoint-v1" || storedOrder.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("retry billing runtime = %#v", storedOrder)
	}
}

func TestFreezeTaskWatermarkUsesCurrentAccountFactInsideCreateTransaction(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	publishAndEnableWatermark(t, repo, "user-1", "publication-1", now)
	task := &model.Task{ID: "task-watermark", UserID: "user-1", Capability: "video", Status: model.TaskStatusQueued}

	if err := repo.CreateTaskWithActiveLimit(task, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled); err != nil {
		t.Fatal(err)
	}
	if task.WatermarkCapability != model.WatermarkCapabilityControlled || task.WatermarkDirective != model.WatermarkDirectiveWithoutWatermark {
		t.Fatalf("frozen watermark identity = %#v", task)
	}
	if !task.WatermarkParameterApplied || task.WatermarkParameterValue == nil || *task.WatermarkParameterValue {
		t.Fatalf("frozen watermark parameter = applied %v value %v", task.WatermarkParameterApplied, task.WatermarkParameterValue)
	}
	if task.WatermarkPolicyPublicationID != "publication-1" || task.WatermarkPolicyVersion != 1 {
		t.Fatalf("frozen publication = %q v%d", task.WatermarkPolicyPublicationID, task.WatermarkPolicyVersion)
	}
	assertFrozenWatermarkLog(t, db, task.ID, model.WatermarkDirectiveWithoutWatermark, false, "publication-1", 1)
}

func TestUserRetryRecomputesWatermarkWhileWorkerClaimKeepsSnapshot(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	publishAndEnableWatermark(t, repo, "user-1", "publication-1", now)
	task := &model.Task{ID: "task-retry-watermark", UserID: "user-1", Capability: "video", Status: model.TaskStatusQueued, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateTaskWithActiveLimit(task, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Updates(map[string]any{"status": model.TaskStatusFailed, "error": "failed"}).Error; err != nil {
		t.Fatal(err)
	}
	event := &model.UserWatermarkPreferenceEvent{ID: "disable-event"}
	if _, _, err := repo.SaveWatermarkPreference("user-1", false, "", event, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	retried, err := repo.RetryTaskWithBilling("user-1", task.ID, nil, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled)
	if err != nil {
		t.Fatal(err)
	}
	if retried.WatermarkDirective != model.WatermarkDirectiveWithWatermark || retried.WatermarkParameterValue == nil || !*retried.WatermarkParameterValue {
		t.Fatalf("retried watermark = %#v", retried)
	}
	if retried.WatermarkPolicyPublicationID != "" || retried.WatermarkPolicyVersion != 0 {
		t.Fatalf("retried stale publication facts = %#v", retried)
	}
	assertFrozenWatermarkLog(t, db, task.ID, model.WatermarkDirectiveWithWatermark, true, "", 0)
	claimed, err := repo.ClaimNextTask("worker-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.WatermarkDirective != retried.WatermarkDirective || claimed.WatermarkParameterValue == nil || *claimed.WatermarkParameterValue != *retried.WatermarkParameterValue {
		t.Fatalf("worker claim changed watermark snapshot: retried=%#v claimed=%#v", retried, claimed)
	}
}

func assertFrozenWatermarkLog(t *testing.T, db *gorm.DB, taskID string, directive model.WatermarkDirective, parameterValue bool, publicationID string, publicationVersion int64) {
	t.Helper()
	var logs []model.TaskLog
	if err := db.Where("task_id = ? AND message = ?", taskID, "水印执行指令已冻结").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	for _, log := range logs {
		var payload struct {
			Capability        model.WatermarkCapability `json:"capability"`
			Directive         model.WatermarkDirective  `json:"directive"`
			ParameterApplied  bool                      `json:"parameterApplied"`
			ParameterValue    *bool                     `json:"parameterValue"`
			PolicyPublication string                    `json:"policyPublicationId"`
			PolicyVersion     int64                     `json:"policyVersion"`
		}
		if err := json.Unmarshal([]byte(log.Payload), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Capability == model.WatermarkCapabilityControlled && payload.Directive == directive && payload.ParameterApplied && payload.ParameterValue != nil && *payload.ParameterValue == parameterValue && payload.PolicyPublication == publicationID && payload.PolicyVersion == publicationVersion {
			return
		}
	}
	t.Fatalf("missing frozen watermark log directive=%q parameter=%v publication=%q version=%d in %#v", directive, parameterValue, publicationID, publicationVersion, logs)
}

func TestFreezeTaskWatermarkRejectsBrokenCurrentPublication(t *testing.T) {
	db := openProviderRepositorySQLite(t)
	repo := New(db)
	now := time.Now().UTC()
	publishAndEnableWatermark(t, repo, "user-1", "publication-broken", now)
	if err := db.Delete(&model.PolicyPublication{}, "id = ?", "publication-broken").Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task-broken-publication", UserID: "user-1", Capability: "video", Status: model.TaskStatusQueued}
	if err := repo.CreateTaskWithActiveLimit(task, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled); err == nil {
		t.Fatal("broken current publication was accepted")
	}
	var count int64
	if err := db.Model(&model.Task{}).Where("id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("broken publication left %d task rows", count)
	}
}

func publishAndEnableWatermark(t *testing.T, repo *Repository, userID string, publicationID string, now time.Time) {
	t.Helper()
	publication := &model.PolicyPublication{ID: publicationID, ManagementRuleRichText: "<p>管理规则</p>", WatermarkPolicyURL: "https://example.com/policy", ContentHash: "hash", PublishedBy: "admin", PublishedAt: now}
	audit := &model.AdminAuditEvent{ID: "audit-" + publicationID, ActorUserID: "admin", Action: "watermark_policy.publish", TargetType: "policy_publication", TargetID: publicationID, Summary: "发布规则", MetadataJSON: `{}`, CreatedAt: now}
	if err := repo.PublishWatermarkPolicy(publication, audit); err != nil {
		t.Fatal(err)
	}
	event := &model.UserWatermarkPreferenceEvent{ID: "enable-" + publicationID}
	if _, _, err := repo.SaveWatermarkPreference(userID, true, publicationID, event, now); err != nil {
		t.Fatal(err)
	}
}

func seedProviderRuntime(t *testing.T, db *gorm.DB, now time.Time, health string) {
	t.Helper()
	rows := []any{
		&model.ProviderAccount{ID: "account", ProviderKind: "kuaizi", Name: "筷子科技", Enabled: true, CreatedAt: now, UpdatedAt: now},
		&model.ProviderEndpointVersion{ID: "endpoint-v1", ProviderAccountID: "account", BaseURL: "https://example.com", Status: "active", Version: 1, CreatedAt: now},
		&model.ProviderCredential{ID: "credential", ProviderAccountID: "account", Family: "seedance", HealthStatus: health, Enabled: true, CreatedAt: now, UpdatedAt: now},
		&model.ProviderCredentialVersion{ID: "key-v1", ProviderCredentialID: "credential", KeyCipher: "cipher", KeyFingerprint: "fingerprint", Status: "active", Version: 1, CreatedAt: now},
		&model.ChannelModel{ID: "channel-model", ChannelID: "channel", ProviderCredentialID: "credential", ModelKey: "doubao-seedance-2-5-260628", Capability: "video", Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
}
