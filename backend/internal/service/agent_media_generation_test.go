package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestMediaApprovalRejectsChangedParametersQuantityAndExpiredQuote(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	command := mediaGenerationTestCommand(t)

	frozen, err := svc.FreezeMediaQuote(scope, command, now)
	if err != nil {
		t.Fatal(err)
	}

	changedParameters := command
	changedParameters.ParametersJSON = replaceMediaTestQuality(t, command.ParametersJSON, "high")
	if _, err := svc.ApproveMediaAttempt(scope, *frozen, changedParameters, now.Add(time.Minute)); !errors.Is(err, ErrCostApprovalQuoteMismatch) {
		t.Fatalf("changed parameters error = %v, want ErrCostApprovalQuoteMismatch", err)
	}

	changedQuantity := command
	changedQuantity.Quantity = 2
	if _, err := svc.ApproveMediaAttempt(scope, *frozen, changedQuantity, now.Add(time.Minute)); !errors.Is(err, ErrCostApprovalQuoteMismatch) {
		t.Fatalf("changed quantity error = %v, want ErrCostApprovalQuoteMismatch", err)
	}

	if _, err := svc.ApproveMediaAttempt(scope, *frozen, command, frozen.ExpiresAt); !errors.Is(err, ErrCostApprovalQuoteMismatch) {
		t.Fatalf("expired quote error = %v, want ErrCostApprovalQuoteMismatch", err)
	}

	tamperedCapabilities := *frozen
	var capabilities PublicProviderCapabilities
	if err := json.Unmarshal([]byte(tamperedCapabilities.ProviderCapabilitiesJSON), &capabilities); err != nil {
		t.Fatal(err)
	}
	capabilities.ModelKey = "different-model"
	encodedCapabilities, err := canonicalMediaCapabilities(&capabilities)
	if err != nil {
		t.Fatal(err)
	}
	tamperedCapabilities.ProviderCapabilitiesJSON = string(encodedCapabilities)
	tamperedCapabilities.ApprovalFingerprint, err = mediaApprovalFingerprint(scope, tamperedCapabilities)
	if err != nil {
		t.Fatal(err)
	}
	tamperedCapabilities.QuoteID = mediaQuoteID(tamperedCapabilities.ApprovalFingerprint)
	if _, err := svc.ApproveMediaAttempt(scope, tamperedCapabilities, command, now.Add(time.Minute)); !errors.Is(err, ErrCostApprovalQuoteMismatch) {
		t.Fatalf("tampered capabilities error = %v, want ErrCostApprovalQuoteMismatch", err)
	}
}

func TestMediaAttemptIdentityIncludesProjectAndThreadScope(t *testing.T) {
	command := mediaGenerationTestCommand(t)
	scope := agentRuntimeServiceScope()
	identity := MediaAttemptIdentity(scope, command)
	if len(identity) != 36 {
		t.Fatalf("media attempt identity length = %d, want 36 for Task/BillingOrder persistence", len(identity))
	}

	differentProject := scope
	differentProject.DomainProjectID += "-other"
	if got := MediaAttemptIdentity(differentProject, command); got == identity {
		t.Fatal("media attempt identity ignored domain project scope")
	}
	differentThread := scope
	differentThread.ThreadID += "-other"
	if got := MediaAttemptIdentity(differentThread, command); got == identity {
		t.Fatal("media attempt identity ignored thread scope")
	}
}

func TestFreezeMediaQuoteRejectsZeroAttempt(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	command := mediaGenerationTestCommand(t)
	command.Attempt = 0

	if _, err := svc.FreezeMediaQuote(agentRuntimeServiceScope(), command, time.Now().UTC()); err == nil {
		t.Fatal("FreezeMediaQuote accepted zero attempt")
	}
}

func TestFreezeMediaQuoteDoesNotPersistCommercialFacts(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)

	var tasksBefore, ordersBefore, ledgerBefore int64
	if err := db.Model(&model.Task{}).Count(&tasksBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&ordersBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Count(&ledgerBefore).Error; err != nil {
		t.Fatal(err)
	}

	quote, err := svc.FreezeMediaQuote(
		agentRuntimeServiceScope(),
		mediaGenerationTestCommand(t),
		time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if quote.QuoteID == "" || quote.ApprovalFingerprint == "" || quote.AmountMicrocredits < 1 {
		t.Fatalf("frozen quote facts are incomplete: %#v", quote)
	}

	var tasksAfter, ordersAfter, ledgerAfter int64
	if err := db.Model(&model.Task{}).Count(&tasksAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Count(&ordersAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).Count(&ledgerAfter).Error; err != nil {
		t.Fatal(err)
	}
	if tasksAfter != tasksBefore || ordersAfter != ordersBefore || ledgerAfter != ledgerBefore {
		t.Fatalf(
			"freezing media quote persisted commercial facts: tasks %d->%d orders %d->%d ledger %d->%d",
			tasksBefore, tasksAfter, ordersBefore, ordersAfter, ledgerBefore, ledgerAfter,
		)
	}
}

func TestEnsureMediaTaskRejectsApprovalAtQuoteExpiry(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()

	quote, err := svc.FreezeMediaQuote(
		scope,
		mediaGenerationTestCommand(t),
		time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	quote.ApprovedAt = quote.ExpiresAt
	if _, _, err := svc.EnsureMediaTask(context.Background(), scope, *quote); !errors.Is(err, ErrCostApprovalQuoteMismatch) {
		t.Fatalf("approval at quote expiry error = %v, want ErrCostApprovalQuoteMismatch", err)
	}
}

func TestRepeatedMediaApprovalCreatesOneInternalTaskAndReservation(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	createAgentRuntimeScopedRunFacts(t, svc, scope, now)
	command := mediaGenerationTestCommand(t)

	frozen, err := svc.FreezeMediaQuote(scope, command, now)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.ApproveMediaAttempt(scope, *frozen, command, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	firstTask, firstOrder, err := svc.EnsureMediaTask(context.Background(), scope, *approved)
	if err != nil {
		t.Fatal(err)
	}
	secondTask, secondOrder, err := svc.EnsureMediaTask(context.Background(), scope, *approved)
	if err != nil {
		t.Fatal(err)
	}
	if firstTask.ID != secondTask.ID || firstOrder.ID != secondOrder.ID {
		t.Fatalf("replayed media facts differ: first=%s/%s second=%s/%s", firstTask.ID, firstOrder.ID, secondTask.ID, secondOrder.ID)
	}

	storedTask, err := svc.repo.TaskForUser(scope.ActorUserID, firstTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Audience != model.TaskAudienceInternal {
		t.Fatalf("media task audience = %q, want internal", storedTask.Audience)
	}
	wantInput, err := canonicalAgentJSON([]byte(command.ParametersJSON))
	if err != nil {
		t.Fatal(err)
	}
	gotInput, err := canonicalAgentJSON([]byte(storedTask.InputJSON))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotInput) != string(wantInput) {
		t.Fatalf("stored media input = %s, want %s", gotInput, wantInput)
	}
	if err := db.Model(&model.Task{}).Where("id = ?", storedTask.ID).
		Update("status", model.TaskStatusSucceeded).Error; err != nil {
		t.Fatal(err)
	}
	completedTask, completedOrder, err := svc.EnsureMediaTask(context.Background(), scope, *approved)
	if err != nil {
		t.Fatal(err)
	}
	if completedTask.ID != firstTask.ID || completedOrder.ID != firstOrder.ID {
		t.Fatalf("completed media replay differs: first=%s/%s completed=%s/%s", firstTask.ID, firstOrder.ID, completedTask.ID, completedOrder.ID)
	}

	var taskCount, orderCount, reserveCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", firstTask.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("idempotency_key = ?", approved.BillingIdempotencyKey).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).
		Where("billing_order_id = ? AND type = ?", firstOrder.ID, model.CreditLedgerReserve).
		Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || reserveCount != 1 {
		t.Fatalf("media commercial facts tasks=%d orders=%d reserves=%d", taskCount, orderCount, reserveCount)
	}
}

func TestSimultaneousMediaAttemptCreatesOneInternalTaskAndReservation(t *testing.T) {
	svc, db, fixture := newAgentRuntimeServiceFixture(t, "https://example.com")
	createAgentRuntimeCanvas(t, db)
	createAgentRuntimeImageModel(t, db, fixture)
	scope := agentRuntimeServiceScope()
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	createAgentRuntimeScopedRunFacts(t, svc, scope, now)
	command := mediaGenerationTestCommand(t)

	frozen, err := svc.FreezeMediaQuote(scope, command, now)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.ApproveMediaAttempt(scope, *frozen, command, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	const callers = 8
	type result struct {
		taskID  string
		orderID string
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, callers)
	var wait sync.WaitGroup
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			task, order, ensureErr := svc.EnsureMediaTask(context.Background(), scope, *approved)
			if ensureErr != nil {
				results <- result{err: ensureErr}
				return
			}
			results <- result{taskID: task.ID, orderID: order.ID}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	for got := range results {
		if got.err != nil {
			t.Fatalf("simultaneous EnsureMediaTask failed: %v", got.err)
		}
		if got.taskID != approved.TaskID {
			t.Fatalf("task id = %q, want %q", got.taskID, approved.TaskID)
		}
	}

	var taskCount, orderCount, reserveCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", approved.TaskID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("idempotency_key = ?", approved.BillingIdempotencyKey).Count(&orderCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CreditLedgerEntry{}).
		Where("billing_order_id IN (?) AND type = ?", db.Model(&model.BillingOrder{}).
			Select("id").Where("idempotency_key = ?", approved.BillingIdempotencyKey), model.CreditLedgerReserve).
		Count(&reserveCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || orderCount != 1 || reserveCount != 1 {
		t.Fatalf("simultaneous media facts tasks=%d orders=%d reserves=%d", taskCount, orderCount, reserveCount)
	}
}

func mediaGenerationTestCommand(t *testing.T) MediaGenerationCommand {
	t.Helper()
	parameters, err := json.Marshal(canvasGenerationInput{
		Mode:   "image",
		Prompt: "鲜橙产品特写",
		Config: providerConfig{
			ChannelID: "runtime-image-channel",
			Model:     "kz_gpt_image2",
			Size:      "1024x1024",
			Quality:   "medium",
			Count:     "1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return MediaGenerationCommand{
		ArtifactRevisionID: "artifact-revision-image-1",
		Attempt:            1,
		TaskType:           "canvas_image",
		Operation:          agentMediaGenerationOperationForRun("runtime-run"),
		Prompt:             "鲜橙产品特写",
		Capability:         "image",
		ChannelID:          "runtime-image-channel",
		ModelKey:           "kz_gpt_image2",
		ParametersJSON:     string(parameters),
		Quantity:           1,
		ProviderCapabilities: &PublicProviderCapabilities{
			ProviderFamily: "kuaizi_gpt_image", ModelKey: "kz_gpt_image2", Capability: "image",
			Resolutions: []string{"1K", "2K", "4K"}, Ratios: []string{"1:1"},
			Qualities: []string{"low", "medium", "high"}, OutputCounts: []int{1},
		},
	}
}

func createAgentRuntimeScopedRunFacts(t *testing.T, svc *Service, scope agentruntime.Scope, now time.Time) {
	t.Helper()
	if _, err := svc.repo.CreateAgentRun(repository.CreateAgentRunInput{
		Scope: scope, ClientRequestID: "media-generation-test", Now: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func replaceMediaTestQuality(t *testing.T, raw string, quality string) string {
	t.Helper()
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		t.Fatal(err)
	}
	input.Config.Quality = quality
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
