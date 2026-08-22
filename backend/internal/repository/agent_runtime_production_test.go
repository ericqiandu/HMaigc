package repository

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func TestAppendAgentProductionPlanCreatesImmutableVersionAndLedger(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)

	first, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "orange-ad", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("第一版剧本"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.Version != 1 || first.Plan.Status != model.AgentProductionPlanActive || first.Plan.Script != "第一版剧本" {
		t.Fatalf("first plan = %#v", first.Plan)
	}
	if len(first.Artifacts) != 5 {
		t.Fatalf("first artifact count = %d, want 5", len(first.Artifacts))
	}
	assertProductionArtifactShape(t, first.Artifacts)

	second, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "orange-ad", BaseVersion: 1,
		Draft: twoShotProductionPlanDraft("第二版剧本"), Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Plan.Version != 2 || second.Plan.Script != "第二版剧本" {
		t.Fatalf("second plan = %#v", second.Plan)
	}
	var storedFirst model.AgentProductionPlanVersion
	if err := db.First(&storedFirst, "id = ?", first.Plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedFirst.Status != model.AgentProductionPlanSuperseded || storedFirst.Script != "第一版剧本" || storedFirst.ShotsJSON != first.Plan.ShotsJSON {
		t.Fatalf("first immutable plan changed = %#v", storedFirst)
	}

	_, err = repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "orange-ad", BaseVersion: 1,
		Draft: twoShotProductionPlanDraft("冲突版本"), Now: now.Add(2 * time.Second),
	})
	if !errors.Is(err, ErrAgentProductionPlanVersionConflict) {
		t.Fatalf("stale base version error = %v", err)
	}
	var planCount int64
	var artifactCount int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ?", "orange-ad").Count(&planCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_key = ?", "orange-ad").Count(&artifactCount).Error; err != nil {
		t.Fatal(err)
	}
	if planCount != 2 || artifactCount != 10 {
		t.Fatalf("conflict wrote facts: plans=%d artifacts=%d", planCount, artifactCount)
	}
}

func TestAgentProductionArtifactTransitionUsesStatusAndAttemptCAS(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "artifact-cas", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("CAS 剧本"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, created.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)

	queued, err := repo.TransitionAgentProductionArtifact(scope, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactPlanned,
		NextStatus: model.AgentProductionArtifactQueued, ExpectedAttempt: 0, NextAttempt: 1,
		TaskID: "task-image-1", Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Status != model.AgentProductionArtifactQueued || queued.Attempt != 1 || queued.TaskID != "task-image-1" {
		t.Fatalf("queued artifact = %#v", queued)
	}

	_, err = repo.TransitionAgentProductionArtifact(scope, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactPlanned,
		NextStatus: model.AgentProductionArtifactQueued, ExpectedAttempt: 0, NextAttempt: 1,
		TaskID: "task-stale", Now: now.Add(2 * time.Second),
	})
	if !errors.Is(err, ErrAgentProductionArtifactConflict) {
		t.Fatalf("stale artifact transition error = %v", err)
	}

	succeeded, err := repo.TransitionAgentProductionArtifact(scope, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactQueued,
		NextStatus: model.AgentProductionArtifactSucceeded, ExpectedAttempt: 1, NextAttempt: 1,
		TaskID: "task-image-1", BillingOrderID: "billing-image-1", ResourceID: "resource-image-1",
		Now: now.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if succeeded.Status != model.AgentProductionArtifactSucceeded || succeeded.ResourceID != "resource-image-1" || succeeded.BillingOrderID != "billing-image-1" {
		t.Fatalf("succeeded artifact = %#v", succeeded)
	}
}

func TestAgentProductionArtifactSuccessAppendsTimelineAfterRunInterruptedAndReplaysIdempotently(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := repo.InitializeAgentRun(InitializeAgentRunInput{
		Scope: scope, ModelRecordID: "model-1", ModelKey: "gpt-5.5", MaxSteps: 8,
		ToolSchemaVersion: agentruntime.CurrentToolSchemaVersion,
		RuntimeVersion:    agentruntime.CurrentRuntimeVersion, PolicyVersion: agentruntime.CurrentPolicyVersion,
		UserMessage:   "生成分镜图",
		Configuration: agentruntime.RunConfiguration{ExecutionMode: agentruntime.ExecutionAutomatic}, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "late-artifact", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("迟到资产剧本"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	artifact := firstProductionArtifact(t, created.Artifacts, "shot-1", model.AgentProductionArtifactStoryboardImage)
	if _, err := repo.TransitionAgentProductionArtifact(scope, ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactPlanned,
		NextStatus: model.AgentProductionArtifactQueued, ExpectedAttempt: 0, NextAttempt: 1,
		TaskID: "task-late-image", BillingOrderID: "billing-late-image", Now: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.InterruptAgentRun(scope, 1, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	transition := ArtifactTransition{
		ArtifactID: artifact.ID, ExpectedStatus: model.AgentProductionArtifactQueued,
		NextStatus: model.AgentProductionArtifactSucceeded, ExpectedAttempt: 1, NextAttempt: 1,
		TaskID: "task-late-image", BillingOrderID: "billing-late-image", ResourceID: "resource-late-image",
		Now: now.Add(3 * time.Second),
	}
	succeeded, err := repo.TransitionAgentProductionArtifact(scope, transition)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded.Status != model.AgentProductionArtifactSucceeded || succeeded.ResourceID != transition.ResourceID {
		t.Fatalf("late succeeded artifact = %#v", succeeded)
	}
	var run model.AgentRun
	if err := db.First(&run, "id = ?", scope.RunID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Status != agentruntime.RunCancelled || run.StateVersion != 2 || run.LastEventSequence != 4 {
		t.Fatalf("late artifact changed terminal runtime facts = %#v", run)
	}
	var checkpoint model.AgentCheckpoint
	if err := db.Where("run_id = ?", scope.RunID).Order("sequence DESC").Take(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	if checkpoint.Sequence != 3 {
		t.Fatalf("late artifact rewrote runtime checkpoint = %#v", checkpoint)
	}
	var event model.AgentRunEvent
	if err := db.Where("run_id = ? AND sequence = ?", scope.RunID, 4).Take(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Kind != agentruntime.EventArtifactAvailable || !strings.Contains(event.PayloadJSON, transition.ResourceID) || strings.Contains(event.PayloadJSON, "Signature=") {
		t.Fatalf("late artifact event = %#v", event)
	}
	var item model.AgentTimelineItem
	if err := db.Where("run_id = ? AND kind = ?", scope.RunID, model.AgentTimelineItemArtifact).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Status != model.AgentTimelineItemCompleted || item.SourceEventSequence != 4 || !strings.Contains(item.ContentJSON, transition.ResourceID) {
		t.Fatalf("late artifact timeline item = %#v", item)
	}
	if _, err := repo.TransitionAgentProductionArtifact(scope, transition); err != nil {
		t.Fatalf("identical late artifact callback replay = %v", err)
	}
	var eventCount, itemCount int64
	if err := db.Model(&model.AgentRunEvent{}).Where("run_id = ?", scope.RunID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentTimelineItem{}).Where("run_id = ?", scope.RunID).Count(&itemCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 4 || itemCount != 3 {
		t.Fatalf("late artifact replay duplicated facts: events=%d items=%d", eventCount, itemCount)
	}
}

func TestAgentProductionPlanReadIsScopeIsolated(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "isolated-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("隔离剧本"), Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AgentProductionPlanVersionForScope(scope, created.Plan.PlanKey, created.Plan.Version); err != nil {
		t.Fatal(err)
	}
	other := scope
	other.TenantID = "other-user"
	other.ActorUserID = "other-user"
	if _, err := repo.AgentProductionPlanVersionForScope(other, created.Plan.PlanKey, created.Plan.Version); err == nil {
		t.Fatal("cross-tenant plan read succeeded")
	}
	sameTenantOtherActor := scope
	sameTenantOtherActor.ActorUserID = "other-user"
	if _, err := repo.AgentProductionPlanVersionForScope(sameTenantOtherActor, created.Plan.PlanKey, created.Plan.Version); err == nil {
		t.Fatal("same-tenant cross-actor plan read succeeded")
	}
	if _, err := repo.ActiveAgentProductionPlanForThread(sameTenantOtherActor); err == nil {
		t.Fatal("same-tenant cross-actor active plan read succeeded")
	}
	wrongProject := scope
	wrongProject.DomainProjectID = "another-project"
	if _, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: wrongProject, RunID: wrongProject.RunID, PlanKey: "wrong-project-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("越权剧本"), Now: time.Now().UTC(),
	}); err == nil {
		t.Fatal("cross-project plan append succeeded")
	}
}

func TestAgentProductionPlanIdentityIsScopedAcrossTenants(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	firstScope := repositoryAgentScope()
	secondScope := repositoryAgentScope()
	secondScope.TenantID = "agent-user-2"
	secondScope.ActorUserID = "agent-user-2"
	secondScope.CanvasID = "agent-canvas-2"
	secondScope.ThreadID = "agent-thread-2"
	secondScope.RunID = "agent-run-2"
	createAgentRunForTest(t, repo, firstScope)
	createAgentRunForTest(t, repo, secondScope)

	first, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{Scope: firstScope, RunID: firstScope.RunID, PlanKey: "shared-plan-key", BaseVersion: 0, Draft: twoShotProductionPlanDraft("租户一"), Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{Scope: secondScope, RunID: secondScope.RunID, PlanKey: "shared-plan-key", BaseVersion: 0, Draft: twoShotProductionPlanDraft("租户二"), Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.ID == second.Plan.ID || first.Artifacts[0].ID == second.Artifacts[0].ID {
		t.Fatalf("cross-tenant production identities collided: first=%s second=%s", first.Plan.ID, second.Plan.ID)
	}
	var planCount int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ? AND version = 1", "shared-plan-key").Count(&planCount).Error; err != nil {
		t.Fatal(err)
	}
	if planCount != 2 {
		t.Fatalf("scoped plan count = %d, want 2", planCount)
	}
}

func TestActiveAgentProductionPlanForThreadFollowsThreadAndScope(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	created, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "active-run-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("活动计划"), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "latest-run-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("最新活动计划"), Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := repo.ActiveAgentProductionPlanForThread(scope)
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.Plan.ID != latest.Plan.ID || active.Plan.ID == created.Plan.ID || len(active.Artifacts) != len(latest.Artifacts) {
		t.Fatalf("active run plan = %#v", active)
	}

	otherRun := scope
	otherRun.RunID = "another-run"
	createAgentRunForTest(t, repo, otherRun)
	active, err = repo.ActiveAgentProductionPlanForThread(otherRun)
	if err != nil || active == nil || active.Plan.ID != latest.Plan.ID {
		t.Fatalf("other run active plan = %#v, err = %v", active, err)
	}

	otherThread := scope
	otherThread.RunID = "another-thread-run"
	otherThread.ThreadID = "another-thread"
	createAgentRunForTest(t, repo, otherThread)
	active, err = repo.ActiveAgentProductionPlanForThread(otherThread)
	if err != nil || active != nil {
		t.Fatalf("other thread active plan = %#v, err = %v", active, err)
	}

	otherTenant := scope
	otherTenant.TenantID = "another-user"
	otherTenant.ActorUserID = "another-user"
	active, err = repo.ActiveAgentProductionPlanForThread(otherTenant)
	if err == nil || active != nil {
		t.Fatalf("other tenant active plan = %#v, err = %v", active, err)
	}
}

func TestAppendAgentProductionPlanRejectsStructurallyInvalidDraft(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*agentruntime.ProductionPlanDraft)
	}{
		{name: "duplicate shot key", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.Shots[1].ShotKey = draft.Shots[0].ShotKey }},
		{name: "non-contiguous order", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.Shots[1].Order = 3 }},
		{name: "missing dependency", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.Shots[1].Dependencies = []string{"missing-shot"} }},
		{name: "future dependency", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.Shots[0].Dependencies = []string{"shot-2"} }},
		{name: "duration mismatch", mutate: func(draft *agentruntime.ProductionPlanDraft) { draft.TargetDurationMS = 9_000 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo, _ := openAgentRuntimeRepositorySQLite(t)
			scope := repositoryAgentScope()
			createAgentRunForTest(t, repo, scope)
			draft := twoShotProductionPlanDraft("无效剧本")
			test.mutate(&draft)
			if _, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
				Scope: scope, RunID: scope.RunID, PlanKey: "invalid-plan", BaseVersion: 0,
				Draft: draft, Now: time.Now().UTC(),
			}); err == nil {
				t.Fatal("structurally invalid production plan was accepted")
			}
		})
	}
}

func TestAppendAgentProductionPlanAllowsOnlyOneConcurrentNextVersion(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLiteFile(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// SQLite has no row-level FOR UPDATE semantics. Keep this focused unit test
	// on one connection; the PostgreSQL gate below proves cross-connection CAS.
	sqlDB.SetMaxOpenConns(1)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err = repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "concurrent-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("基础版本"), Now: now,
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, script := range []string{"并发版本 A", "并发版本 B"} {
		script := script
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
				Scope: scope, RunID: scope.RunID, PlanKey: "concurrent-plan", BaseVersion: 1,
				Draft: twoShotProductionPlanDraft(script), Now: now.Add(time.Second),
			})
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	succeeded := 0
	conflicted := 0
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAgentProductionPlanVersionConflict):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent append error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent append results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
	var planCount int64
	var artifactCount int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ?", "concurrent-plan").Count(&planCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_key = ?", "concurrent-plan").Count(&artifactCount).Error; err != nil {
		t.Fatal(err)
	}
	if planCount != 2 || artifactCount != 10 {
		t.Fatalf("concurrent append facts: plans=%d artifacts=%d", planCount, artifactCount)
	}
}

func TestAppendAgentProductionPlanReplaysIdenticalVersionWithoutNewFacts(t *testing.T) {
	repo, db := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	input := AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "replay-plan", BaseVersion: 0,
		Draft: twoShotProductionPlanDraft("可重放剧本"), Now: time.Now().UTC().Truncate(time.Second),
	}
	first, err := repo.AppendAgentProductionPlanVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repo.AppendAgentProductionPlanVersion(input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Plan.ID != first.Plan.ID || len(replayed.Artifacts) != len(first.Artifacts) {
		t.Fatalf("replayed production plan = %#v", replayed)
	}
	for index := range first.Artifacts {
		if replayed.Artifacts[index].ID != first.Artifacts[index].ID {
			t.Fatalf("replayed artifact %d = %s, want %s", index, replayed.Artifacts[index].ID, first.Artifacts[index].ID)
		}
	}
	conflict := input
	conflict.Draft.Script = "不同剧本不得冒充重放"
	if _, err := repo.AppendAgentProductionPlanVersion(conflict); !errors.Is(err, ErrAgentProductionPlanVersionConflict) {
		t.Fatalf("different replay error = %v", err)
	}
	var plans int64
	var artifacts int64
	if err := db.Model(&model.AgentProductionPlanVersion{}).Where("plan_key = ?", input.PlanKey).Count(&plans).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AgentProductionArtifact{}).Where("plan_key = ?", input.PlanKey).Count(&artifacts).Error; err != nil {
		t.Fatal(err)
	}
	if plans != 1 || artifacts != 5 {
		t.Fatalf("replay duplicated facts: plans=%d artifacts=%d", plans, artifacts)
	}
}

func twoShotProductionPlanDraft(script string) agentruntime.ProductionPlanDraft {
	return agentruntime.ProductionPlanDraft{
		Title: "10 秒橙子广告", TargetDurationMS: 10_000, Script: script,
		Shots: []agentruntime.ShotPlanDraft{
			{ShotKey: "shot-1", Order: 1, DurationMS: 5_000, ScriptText: "鲜橙落水", ImagePrompt: "橙子产品特写", VideoPrompt: "慢镜头水花", Dependencies: []string{}},
			{ShotKey: "shot-2", Order: 2, DurationMS: 5_000, ScriptText: "果汁收尾", ImagePrompt: "果汁英雄镜头", VideoPrompt: "镜头推进", Dependencies: []string{"shot-1"}},
		},
	}
}

func TestAppendAgentProductionPlanCreatesDurableReferenceArtifacts(t *testing.T) {
	repo, _ := openAgentRuntimeRepositorySQLite(t)
	scope := repositoryAgentScope()
	createAgentRunForTest(t, repo, scope)
	draft := twoShotProductionPlanDraft("带角色参考的广告")
	draft.References = []agentruntime.ReferenceAssetDraft{{
		ReferenceKey: "hero", Role: "character", Title: "主角", ImagePrompt: "主角角色参考图",
	}}
	draft.Shots[0].ReferenceKeys = []string{"hero"}
	draft.Shots[1].ReferenceKeys = []string{"hero"}

	record, err := repo.AppendAgentProductionPlanVersion(AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: "reference-plan", BaseVersion: 0, Draft: draft, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Plan.ReferencesJSON == "" {
		t.Fatal("reference plan facts were not persisted")
	}
	var referenceArtifact model.AgentProductionArtifact
	for _, artifact := range record.Artifacts {
		if artifact.Kind == model.AgentProductionArtifactReferenceImage {
			referenceArtifact = artifact
			break
		}
	}
	if referenceArtifact.ID == "" || referenceArtifact.ReferenceKey != "hero" || referenceArtifact.ShotKey != "" || referenceArtifact.Status != model.AgentProductionArtifactPlanned {
		t.Fatalf("reference artifact = %#v", referenceArtifact)
	}
}

func assertProductionArtifactShape(t *testing.T, artifacts []model.AgentProductionArtifact) {
	t.Helper()
	want := map[string]bool{
		"/script":                 false,
		"shot-1/storyboard_image": false,
		"shot-1/video_clip":       false,
		"shot-2/storyboard_image": false,
		"shot-2/video_clip":       false,
	}
	ids := map[string]bool{}
	for _, artifact := range artifacts {
		key := artifact.ShotKey + "/" + string(artifact.Kind)
		if _, ok := want[key]; !ok {
			t.Fatalf("unexpected production artifact %q", key)
		}
		want[key] = true
		if ids[artifact.ID] {
			t.Fatalf("duplicate production artifact id %s", artifact.ID)
		}
		ids[artifact.ID] = true
		wantStatus := model.AgentProductionArtifactPlanned
		if artifact.Kind == model.AgentProductionArtifactScript {
			wantStatus = model.AgentProductionArtifactSucceeded
		}
		if artifact.Status != wantStatus || artifact.Attempt != 0 {
			t.Fatalf("initial artifact = %#v", artifact)
		}
	}
	for key, found := range want {
		if !found {
			t.Fatalf("missing production artifact %q", key)
		}
	}
}

func firstProductionArtifact(t *testing.T, artifacts []model.AgentProductionArtifact, shotKey string, kind model.AgentProductionArtifactKind) model.AgentProductionArtifact {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.ShotKey == shotKey && artifact.Kind == kind {
			return artifact
		}
	}
	t.Fatalf("missing artifact %s/%s", shotKey, kind)
	return model.AgentProductionArtifact{}
}
