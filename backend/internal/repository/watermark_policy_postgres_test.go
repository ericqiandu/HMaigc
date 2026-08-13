package repository

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/database"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/testsupport"

	"gorm.io/gorm"
)

func TestPostgresWatermarkPublicationSerializesConcurrentVersions(t *testing.T) {
	db := openPostgresWatermarkSchema(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	start := make(chan struct{})
	versions := make([]int64, 2)
	errorsByIndex := make([]error, 2)
	var wait sync.WaitGroup
	for index := range versions {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			publication := watermarkPublication(fmt.Sprintf("postgres-publication-%d", index), "<p>并发规则</p>", "https://example.com/watermark")
			publication.PublishedAt = now.Add(time.Duration(index) * time.Second)
			errorsByIndex[index] = New(db.Session(&gorm.Session{})).PublishWatermarkPolicy(publication, watermarkPublicationAudit(fmt.Sprintf("postgres-audit-%d", index), publication.ID))
			versions[index] = publication.Version
		}(index)
	}
	close(start)
	wait.Wait()
	for index, err := range errorsByIndex {
		if err != nil {
			t.Fatalf("concurrent publication %d failed: %v", index, err)
		}
	}
	sort.Slice(versions, func(left int, right int) bool { return versions[left] < versions[right] })
	if versions[0] != 1 || versions[1] != 2 {
		t.Fatalf("concurrent publication versions = %v, want [1 2]", versions)
	}
}

func TestPostgresWatermarkPreferenceAndConsentRollbackTogether(t *testing.T) {
	db := openPostgresWatermarkSchema(t)
	repo := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	publication := watermarkPublication("postgres-rollback-publication", "<p>回滚规则</p>", "https://example.com/watermark")
	publication.PublishedAt = now
	if err := repo.PublishWatermarkPolicy(publication, watermarkPublicationAudit("postgres-rollback-audit", publication.ID)); err != nil {
		t.Fatal(err)
	}
	functionName := "reject_watermark_event_insert"
	if err := db.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected event failure'; END; $$`, functionName)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON user_watermark_preference_events FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, functionName)).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SaveWatermarkPreference("postgres-user", true, publication.ID, watermarkPreferenceEvent("postgres-event", "postgres-user", true, publication.ID), now); err == nil {
		t.Fatal("injected event failure unexpectedly committed")
	}
	assertWatermarkRowCount(t, db, &model.UserPolicyConsent{}, 0)
	assertWatermarkRowCount(t, db, &model.UserWatermarkPreference{}, 0)
}

func TestPostgresTaskCreateFreezesWatermarkWithBillingAndProviderRuntime(t *testing.T) {
	db := openPostgresWatermarkSchema(t)
	repo := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedProviderRuntime(t, db, now, "healthy")
	publishAndEnableWatermark(t, repo, "postgres-user", "postgres-task-publication", now)
	if err := db.Create(&model.CreditAccount{UserID: "postgres-user", AvailableMicrocredits: 100}).Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "postgres-watermark-task", UserID: "postgres-user", Capability: "video", Status: model.TaskStatusQueued}
	order := &model.BillingOrder{ID: "postgres-watermark-order", UserID: task.UserID, TaskID: task.ID, ChannelModelID: "channel-model", AmountMicrocredits: 10, Status: model.BillingStatusReserved}
	if err := repo.CreateTaskWithCreditReservation(task, order, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled); err != nil {
		t.Fatal(err)
	}
	var stored model.Task
	if err := db.First(&stored, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.WatermarkDirective != model.WatermarkDirectiveWithoutWatermark || stored.WatermarkParameterValue == nil || *stored.WatermarkParameterValue || stored.WatermarkPolicyPublicationID != "postgres-task-publication" || stored.ProviderEndpointVersionID != "endpoint-v1" || stored.ProviderCredentialVersionID != "key-v1" {
		t.Fatalf("stored task facts = %#v", stored)
	}
	assertFrozenWatermarkLog(t, db, task.ID, model.WatermarkDirectiveWithoutWatermark, false, "postgres-task-publication", 1)
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 90 || account.ReservedMicrocredits != 10 {
		t.Fatalf("reserved account = %#v", account)
	}
}

func TestPostgresTaskCreateRollsBackWhenWatermarkLogFails(t *testing.T) {
	db := openPostgresWatermarkSchema(t)
	repo := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	seedProviderRuntime(t, db, now, "healthy")
	if err := db.Create(&model.CreditAccount{UserID: "postgres-log-user", AvailableMicrocredits: 100}).Error; err != nil {
		t.Fatal(err)
	}
	functionName := "reject_watermark_task_log_insert"
	if err := db.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.message = '水印执行指令已冻结' THEN RAISE EXCEPTION 'injected watermark log failure'; END IF; RETURN NEW; END; $$`, functionName)).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON task_logs FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, functionName)).Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "postgres-log-task", UserID: "postgres-log-user", Capability: "video", Status: model.TaskStatusQueued}
	order := &model.BillingOrder{ID: "postgres-log-order", UserID: task.UserID, TaskID: task.ID, ChannelModelID: "channel-model", AmountMicrocredits: 10, Status: model.BillingStatusReserved}
	if err := repo.CreateTaskWithCreditReservation(task, order, ActiveTaskPolicy{Unlimited: true}, model.WatermarkCapabilityControlled); err == nil {
		t.Fatal("injected watermark log failure unexpectedly committed")
	}
	assertWatermarkRowCount(t, db, &model.Task{}, 0)
	assertWatermarkRowCount(t, db, &model.BillingOrder{}, 0)
	assertWatermarkRowCount(t, db, &model.TaskLog{}, 0)
	var account model.CreditAccount
	if err := db.First(&account, "user_id = ?", task.UserID).Error; err != nil {
		t.Fatal(err)
	}
	if account.AvailableMicrocredits != 100 || account.ReservedMicrocredits != 0 {
		t.Fatalf("failed task log left reserved credits: %#v", account)
	}
}

func TestPostgresWatermarkTaskFreezeAllowsConcurrentReaders(t *testing.T) {
	db := openPostgresWatermarkSchema(t)
	repo := New(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	publishAndEnableWatermark(t, repo, "postgres-reader-user", "postgres-reader-publication", now)

	first := db.Begin()
	if first.Error != nil {
		t.Fatal(first.Error)
	}
	firstClosed := false
	t.Cleanup(func() {
		if !firstClosed {
			_ = first.Rollback().Error
		}
	})
	firstTask := &model.Task{UserID: "postgres-reader-user"}
	if err := freezeTaskWatermarkTx(first, firstTask, model.WatermarkCapabilityControlled); err != nil {
		t.Fatal(err)
	}

	secondResult := make(chan error, 1)
	go func() {
		second := db.Begin()
		if second.Error != nil {
			secondResult <- second.Error
			return
		}
		defer second.Rollback()
		secondTask := &model.Task{UserID: "postgres-reader-user"}
		secondResult <- freezeTaskWatermarkTx(second, secondTask, model.WatermarkCapabilityControlled)
	}()

	select {
	case err := <-secondResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(750 * time.Millisecond):
		_ = first.Rollback().Error
		firstClosed = true
		if err := <-secondResult; err != nil {
			t.Fatal(err)
		}
		t.Fatal("concurrent watermark task freeze blocked behind an exclusive publication lock")
	}
	if err := first.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	firstClosed = true
}

func openPostgresWatermarkSchema(t *testing.T) *gorm.DB {
	t.Helper()
	db := testsupport.OpenPaymentIntegrationPostgres(t)
	if err := database.MigrateBaseSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := database.EnsureProviderIntegritySchema(db); err != nil {
		t.Fatal(err)
	}
	return db
}
