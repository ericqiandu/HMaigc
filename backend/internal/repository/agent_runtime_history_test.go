package repository

import (
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAgentTimelineEventsAfterReturnsScopedEventsAndAssociatedItems(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "history-user", ActorUserID: "history-user",
		CanvasID: "history-canvas", ThreadID: "history-thread", RunID: "history-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record", ModelKey: "model-key", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage: "生成短片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	records, err := repo.AgentTimelineEventsAfter(scope, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Event.Kind != agentruntime.EventRunCreated || records[0].Item != nil ||
		records[1].Event.Kind != agentruntime.EventUserMessageAdded || records[1].Item == nil ||
		records[1].Item.Kind != model.AgentTimelineItemUserMessage || records[1].Item.SourceEventSequence != 2 {
		t.Fatalf("timeline event records = %#v", records)
	}
}

func TestAppendAgentMessageDeltaPersistsEventAndItemAtomicallyForReplay(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "delta-user", ActorUserID: "delta-user",
		CanvasID: "delta-canvas", ThreadID: "delta-thread", RunID: "delta-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record", ModelKey: "model-key", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
		PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "生成短片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	itemID := agentruntime.AgentMessageItemID(scope.RunID, 0)
	first, err := repo.AppendAgentMessageDelta(AppendAgentMessageDeltaInput{Scope: scope, ItemID: itemID, PayloadJSON: `{"itemId":"` + itemID + `","delta":"你","userVisible":true,"started":true}`, Message: "你", Started: true, Now: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.AppendAgentMessageDelta(AppendAgentMessageDeltaInput{Scope: scope, ItemID: itemID, PayloadJSON: `{"itemId":"` + itemID + `","delta":"好","userVisible":true}`, Message: "你好", Now: now.Add(2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 3 || second.Sequence != 4 {
		t.Fatalf("delta sequences = %d, %d", first.Sequence, second.Sequence)
	}
	records, err := repo.AgentTimelineEventsAfter(scope, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("delta replay records = %#v", records)
	}
	for _, record := range records {
		if record.Item == nil || record.Item.ID != itemID || record.Item.Kind != model.AgentTimelineItemAgentMessage || record.Item.Status != model.AgentTimelineItemInProgress || record.Item.SourceEventSequence != record.Event.Sequence {
			t.Fatalf("delta replay item = %#v", record)
		}
	}
}

func TestAgentMessageStreamRejectsLateFactsAfterRunInterrupt(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "stopped-stream-user", ActorUserID: "stopped-stream-user",
		CanvasID: "stopped-stream-canvas", ThreadID: "stopped-stream-thread", RunID: "stopped-stream-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record", ModelKey: "model-key", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
		PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "生成短片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	itemID := agentruntime.AgentMessageItemID(scope.RunID, 0)
	if _, err := repo.AppendAgentMessageDelta(AppendAgentMessageDeltaInput{
		Scope: scope, ItemID: itemID,
		PayloadJSON: `{"itemId":"` + itemID + `","delta":"第一段","userVisible":true,"started":true}`,
		Message:     "第一段", Started: true, Now: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InterruptAgentRun(scope, 1, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendAgentMessageDelta(AppendAgentMessageDeltaInput{
		Scope: scope, ItemID: itemID,
		PayloadJSON: `{"itemId":"` + itemID + `","delta":"第二段","userVisible":true}`,
		Message:     "第一段第二段", Now: now.Add(3 * time.Second),
	}); err == nil {
		t.Fatal("interrupted run accepted a late visible delta")
	}
	if _, err := repo.FailAgentMessageStream(FailAgentMessageStreamInput{
		Scope: scope, ItemID: itemID, Message: "第一段", FailureCode: "agent_provider_stream_failed", Now: now.Add(4 * time.Second),
	}); err == nil {
		t.Fatal("interrupted run accepted a late message failure")
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != agentruntime.RunCancelled || run.LastEventSequence != 4 {
		t.Fatalf("interrupted run changed after late stream facts = %#v", run)
	}
	var item model.AgentTimelineItem
	if err := db.First(&item, "id = ?", itemID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != model.AgentTimelineItemInterrupted || item.CompletedAt == nil ||
		item.ContentJSON != `{"message":"第一段"}` || item.SourceEventSequence != 4 {
		t.Fatalf("message projection changed after interruption = %#v", item)
	}
	records, err := repo.AgentTimelineEventsAfter(scope, 4, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("late stream facts persisted after interruption = %#v", records)
	}
}

func TestAppendAgentMessageDeltaRestartsOneInterruptedMessageProjection(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "restart-user", ActorUserID: "restart-user",
		CanvasID: "restart-canvas", ThreadID: "restart-thread", RunID: "restart-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record", ModelKey: "model-key", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
		PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "生成短片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	itemID := agentruntime.AgentMessageItemID(scope.RunID, 0)
	if _, err := repo.AppendAgentMessageDelta(AppendAgentMessageDeltaInput{
		Scope: scope, ItemID: itemID,
		PayloadJSON: `{"itemId":"` + itemID + `","delta":"旧的半截","userVisible":true,"started":true}`,
		Message:     "旧的半截", Started: true, Now: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AppendAgentMessageDelta(AppendAgentMessageDeltaInput{
		Scope: scope, ItemID: itemID,
		PayloadJSON: `{"itemId":"` + itemID + `","delta":"重新开始","userVisible":true,"started":true}`,
		Message:     "重新开始", Started: true, Now: now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("restarted stream could not replace the interrupted projection: %v", err)
	}
	var item model.AgentTimelineItem
	if err := db.First(&item, "id = ?", itemID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != model.AgentTimelineItemInProgress || item.ContentJSON != `{"message":"重新开始"}` || item.SourceEventSequence != 4 {
		t.Fatalf("restarted message projection = %#v", item)
	}
	var itemCount int64
	if err := db.Model(&model.AgentTimelineItem{}).Where("id = ?", itemID).Count(&itemCount).Error; err != nil {
		t.Fatal(err)
	}
	if itemCount != 1 {
		t.Fatalf("restarted stream created %d message items", itemCount)
	}
}

func TestFailAgentMessageStreamPersistsFailedTerminalItemForReplay(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "failed-message-user", ActorUserID: "failed-message-user",
		CanvasID: "failed-message-canvas", ThreadID: "failed-message-thread", RunID: "failed-message-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record", ModelKey: "model-key", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion, RuntimeVersion: agentruntime.CurrentRuntimeVersion,
		PolicyVersion: agentruntime.CurrentPolicyVersion, UserMessage: "生成短片",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	itemID := agentruntime.AgentMessageItemID(scope.RunID, 0)
	if _, err := repo.AppendAgentMessageDelta(AppendAgentMessageDeltaInput{
		Scope: scope, ItemID: itemID,
		PayloadJSON: `{"itemId":"` + itemID + `","delta":"未完成正文","userVisible":true,"started":true}`,
		Message:     "未完成正文", Started: true, Now: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := repo.FailAgentMessageStream(FailAgentMessageStreamInput{
		Scope: scope, ItemID: itemID, Message: "未完成正文",
		FailureCode: "agent_provider_stream_truncated", Now: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("failed stream did not terminate its message item: %v", err)
	}
	if failed.Kind != agentruntime.EventAgentMessageFailed || failed.Sequence != 4 {
		t.Fatalf("failed message event = %#v", failed)
	}
	var item model.AgentTimelineItem
	if err := db.First(&item, "id = ?", itemID).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != model.AgentTimelineItemFailed || item.CompletedAt == nil || item.SourceEventSequence != failed.Sequence ||
		item.ContentJSON != `{"message":"未完成正文","failureCode":"agent_provider_stream_truncated"}` {
		t.Fatalf("failed message item = %#v", item)
	}
	records, err := repo.AgentTimelineEventsAfter(scope, 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Event.Kind != agentruntime.EventAgentMessageFailed || records[0].Item == nil || records[0].Item.Status != model.AgentTimelineItemFailed {
		t.Fatalf("failed message replay = %#v", records)
	}
}

func TestAgentTimelineEventsAfterRejectsStoredItemOutsideRunScope(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "history-user", ActorUserID: "history-user",
		CanvasID: "history-canvas", ThreadID: "history-thread", RunID: "history-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, scope)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record", ModelKey: "model-key", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage: "生成短片", Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ? AND source_event_sequence = ?", scope.RunID, 2).
		Update("tenant_id", "other-tenant").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AgentTimelineEventsAfter(scope, 0, 100); err == nil {
		t.Fatal("timeline item outside the scoped run was silently omitted")
	}
}

func TestAgentThreadHistoryReturnsAllTurnsAndItemsInStableOrder(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	baseScope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "history-user", ActorUserID: "history-user",
		CanvasID: "history-canvas", ThreadID: "history-thread", RunID: "history-run-1",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, baseScope)
	now := time.Now().UTC()
	initializeHistoryRun(t, repo, baseScope, "第一轮", now)
	secondScope := baseScope
	secondScope.RunID = "history-run-2"
	createAgentRunForTest(t, repo, secondScope)
	initializeHistoryRun(t, repo, secondScope, "第二轮", now.Add(time.Second))

	records, err := repo.AgentThreadHistory(baseScope, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Thread.ID != baseScope.ThreadID || len(records[0].Turns) != 2 {
		t.Fatalf("thread history = %#v", records)
	}
	if records[0].Turns[0].Run.ID != baseScope.RunID || records[0].Turns[1].Run.ID != secondScope.RunID {
		t.Fatalf("turn order = %#v", records[0].Turns)
	}
	for _, turn := range records[0].Turns {
		if len(turn.Items) != 1 || turn.Items[0].Kind != model.AgentTimelineItemUserMessage || turn.Items[0].Ordinal != 1 {
			t.Fatalf("turn items = %#v", turn.Items)
		}
	}
}

func TestAgentThreadHistoryRebuildsMissingTimelineOnlyForTerminalRuns(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "history-user", ActorUserID: "history-user",
		CanvasID: "history-canvas", ThreadID: "history-terminal-thread", RunID: "history-terminal-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC()
	initializeHistoryRun(t, repo, scope, "旧终态任务", now)
	if _, err := repo.InterruptAgentRun(scope, 1, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("run_id = ?", scope.RunID).Delete(&model.AgentTimelineItem{}).Error; err != nil {
		t.Fatal(err)
	}
	records, err := repo.AgentThreadHistory(scope, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0].Turns) != 1 || len(records[0].Turns[0].Items) != 2 {
		t.Fatalf("rebuilt terminal history = %#v", records)
	}
	items := records[0].Turns[0].Items
	if items[0].Kind != model.AgentTimelineItemUserMessage || items[0].Status != model.AgentTimelineItemCompleted ||
		items[1].Kind != model.AgentTimelineItemStatusKind || items[1].Status != model.AgentTimelineItemInterrupted {
		t.Fatalf("rebuilt terminal items = %#v", items)
	}
	var persistedCount int64
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ?", scope.RunID).Count(&persistedCount).Error; err != nil {
		t.Fatal(err)
	}
	if persistedCount != 0 {
		t.Fatalf("read-only terminal rebuild wrote %d timeline items", persistedCount)
	}
}

func TestAgentThreadHistoryRejectsMissingTimelineForActiveRun(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := agentruntime.Scope{
		TenantKind: agentruntime.TenantPersonal, TenantID: "history-user", ActorUserID: "history-user",
		CanvasID: "history-canvas", ThreadID: "history-active-thread", RunID: "history-active-run",
		Access: agentruntime.AccessGrant{Level: agentruntime.AccessManager, SubscriptionActive: true},
	}
	createAgentRunForTest(t, repo, scope)
	initializeHistoryRun(t, repo, scope, "旧活动任务", time.Now().UTC())
	if err := db.Where("run_id = ?", scope.RunID).Delete(&model.AgentTimelineItem{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AgentThreadHistory(scope, 20); err == nil {
		t.Fatal("active run without a timeline projection was silently accepted")
	}
}

func initializeHistoryRun(t *testing.T, repo *Repository, scope agentruntime.Scope, message string, now time.Time) {
	t.Helper()
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-record", ModelKey: "model-key", MaxSteps: 4,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage: message, Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionGuided}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
}
