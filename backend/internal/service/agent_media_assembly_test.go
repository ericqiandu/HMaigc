package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

type fakeMediaAssembler struct {
	err           error
	started       chan struct{}
	release       chan struct{}
	skipOutput    bool
	outputContent []byte
}

func (assembler *fakeMediaAssembler) Assemble(ctx context.Context, command MediaAssemblyCommand) error {
	if assembler.started != nil {
		close(assembler.started)
	}
	if assembler.release != nil {
		<-assembler.release
	} else if err := ctx.Err(); err != nil {
		return err
	}
	if assembler.err != nil {
		return assembler.err
	}
	if assembler.skipOutput {
		return nil
	}
	content := assembler.outputContent
	if content == nil {
		content = []byte("assembled-video")
	}
	return os.WriteFile(command.Arguments[len(command.Arguments)-1], content, 0o640)
}

func TestUnbilledInternalTaskEnqueueIsDeterministic(t *testing.T) {
	svc, db, scope, plan := mediaAssemblyRuntimeFixture(t)
	first, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.ExecutionKind != model.TaskExecutionLocalMediaAssembly || first.Audience != model.TaskAudienceInternal {
		t.Fatalf("deterministic internal task mismatch: first=%#v second=%#v", first, second)
	}
	if first.BillingOrderID != "" {
		t.Fatalf("local assembly billing order = %q", first.BillingOrderID)
	}
	var taskCount, billingCount int64
	if err := db.Model(&model.Task{}).Where("id = ?", first.ID).Count(&taskCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.BillingOrder{}).Where("task_id = ?", first.ID).Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if taskCount != 1 || billingCount != 0 {
		t.Fatalf("taskCount=%d billingCount=%d", taskCount, billingCount)
	}
}

func TestMediaAssemblySuccessMaterializesChecksummedResourceAndArtifact(t *testing.T) {
	svc, db, scope, plan := mediaAssemblyRuntimeFixture(t)
	svc.mediaAssembler = &fakeMediaAssembler{}
	svc.mediaFileProbe = func(context.Context, string) (mediaFileMetadata, error) {
		return mediaFileMetadata{DurationMS: 1000, MimeType: "video/mp4"}, nil
	}
	task, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusSucceeded {
		t.Fatalf("task status = %s, error=%s", stored.Status, stored.Error)
	}
	var result mediaAssemblyTaskResult
	if err := json.Unmarshal([]byte(stored.ResultJSON), &result); err != nil {
		t.Fatal(err)
	}
	resource, err := svc.repo.Resource(result.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	expectedChecksum := sha256.Sum256([]byte("assembled-video"))
	if resource.Status != model.ResourceStatusReady || resource.ETag != hex.EncodeToString(expectedChecksum[:]) || resource.DurationMs != 1000 {
		t.Fatalf("output resource = %#v", resource)
	}
	revision, err := svc.repo.ArtifactRevisionInScope(scope, result.ArtifactRevision.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if revision.ResourceID != resource.ID || revision.LifecycleStatus != model.AgentArtifactRevisionAwaitingReview {
		t.Fatalf("output revision = %#v", revision)
	}
	var billingCount int64
	if err := db.Model(&model.BillingOrder{}).Where("task_id = ?", task.ID).Count(&billingCount).Error; err != nil {
		t.Fatal(err)
	}
	if billingCount != 0 {
		t.Fatalf("billing count = %d", billingCount)
	}
}

func TestMediaAssemblyCancellationStopsQueuedTask(t *testing.T) {
	svc, _, scope, plan := mediaAssemblyRuntimeFixture(t)
	task, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.CancelAgentMediaAssembly(scope, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != model.TaskStatusCancelled {
		t.Fatalf("status = %s", cancelled.Status)
	}
}

func TestMediaAssemblyExpiredLeaseIsRecoveredAfterWorkerRestart(t *testing.T) {
	svc, _, scope, plan := mediaAssemblyRuntimeFixture(t)
	svc.mediaAssembler = &fakeMediaAssembler{}
	svc.mediaFileProbe = func(context.Context, string) (mediaFileMetadata, error) {
		return mediaFileMetadata{DurationMS: 1000, MimeType: "video/mp4"}, nil
	}
	task, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repo.ClaimNextTask("stopped-worker", -time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.ID != task.ID || claimed.Status != model.TaskStatusRunning {
		t.Fatalf("initial claim = %#v", claimed)
	}
	if err := svc.ProcessNextTask(); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusSucceeded || stored.Attempts != 2 {
		t.Fatalf("recovered task = %#v", stored)
	}
}

func TestMediaAssemblyLateSuccessAfterCancellationIsUnadopted(t *testing.T) {
	svc, db, scope, plan := mediaAssemblyRuntimeFixture(t)
	assembler := &fakeMediaAssembler{started: make(chan struct{}), release: make(chan struct{})}
	svc.mediaAssembler = assembler
	svc.mediaFileProbe = func(context.Context, string) (mediaFileMetadata, error) {
		return mediaFileMetadata{DurationMS: 1000, MimeType: "video/mp4"}, nil
	}
	task, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- svc.processClaimedTask(claimed) }()
	<-assembler.started
	if _, err := svc.CancelAgentMediaAssembly(scope, task.ID); err != nil {
		t.Fatal(err)
	}
	close(assembler.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusCancelled {
		t.Fatalf("task status = %s", stored.Status)
	}
	var revisions []model.AgentArtifactRevision
	if err := db.Where("created_by_run_id = ? AND lifecycle_status = ?", scope.RunID, model.AgentArtifactRevisionUnadopted).Find(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("unadopted revisions = %d, want 1", len(revisions))
	}
	if revisions[0].ResourceID == "" {
		t.Fatal("late successful output resource was not preserved")
	}
	var artifact model.AgentArtifact
	if err := db.First(&artifact, "id = ?", revisions[0].ArtifactID).Error; err != nil {
		t.Fatal(err)
	}
	if artifact.HeadRevision != 0 || artifact.LifecycleStatus != model.AgentArtifactLifecycleUnadopted {
		t.Fatalf("late successful artifact was adopted: %#v", artifact)
	}
}

func TestMediaAssemblyLateSuccessAfterPlanBecomesStaleIsUnadopted(t *testing.T) {
	svc, db, scope, plan := mediaAssemblyRuntimeFixture(t)
	assembler := &fakeMediaAssembler{started: make(chan struct{}), release: make(chan struct{})}
	svc.mediaAssembler = assembler
	svc.mediaFileProbe = func(context.Context, string) (mediaFileMetadata, error) {
		return mediaFileMetadata{DurationMS: 1000, MimeType: "video/mp4"}, nil
	}
	task, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- svc.processClaimedTask(claimed) }()
	<-assembler.started
	if err := db.Model(&model.AgentArtifactRevision{}).Where("id = ?", plan.RevisionID).
		Update("lifecycle_status", model.AgentArtifactRevisionStale).Error; err != nil {
		t.Fatal(err)
	}
	close(assembler.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusSucceeded || stored.Stage != "装配完成（计划已过期，产物未采纳）" {
		t.Fatalf("task after stale plan = %#v", stored)
	}
	var revisions []model.AgentArtifactRevision
	if err := db.Where("created_by_run_id = ? AND lifecycle_status = ?", scope.RunID, model.AgentArtifactRevisionUnadopted).Find(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || revisions[0].ResourceID == "" {
		t.Fatalf("unadopted revisions = %#v", revisions)
	}
	var artifact model.AgentArtifact
	if err := db.First(&artifact, "id = ?", revisions[0].ArtifactID).Error; err != nil {
		t.Fatal(err)
	}
	if artifact.HeadRevision != 0 || artifact.LifecycleStatus != model.AgentArtifactLifecycleUnadopted {
		t.Fatalf("stale-plan output was adopted: %#v", artifact)
	}
}

func TestUnbilledInternalTaskDoesNotExemptProviderExecution(t *testing.T) {
	svc, db, scope, _ := mediaAssemblyRuntimeFixture(t)
	now := time.Now().UTC()
	task := model.Task{
		ID: "provider-without-billing", UserID: scope.ActorUserID, Audience: model.TaskAudienceInternal,
		ExecutionKind: model.TaskExecutionProvider, ProjectID: scope.CanvasID, Type: "canvas_video",
		Capability: "video", Status: model.TaskStatusQueued, InputJSON: `{}`, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ProcessNextTask(); err == nil {
		t.Fatal("provider task without billing order error = nil")
	}
	stored, err := svc.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusFailed || stored.Stage != "任务执行事实无效" {
		t.Fatalf("provider task = %#v", stored)
	}
}

func TestMediaAssemblyFailureAndLeaseLossAreExplicit(t *testing.T) {
	t.Run("assembler failure", func(t *testing.T) {
		svc, _, scope, plan := mediaAssemblyRuntimeFixture(t)
		svc.mediaAssembler = &fakeMediaAssembler{err: errors.New("ffmpeg exit 1")}
		if _, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan}); err != nil {
			t.Fatal(err)
		}
		if err := svc.ProcessNextTask(); err == nil {
			t.Fatal("ProcessNextTask() error = nil")
		}
	})
	t.Run("missing output", func(t *testing.T) {
		svc, _, scope, plan := mediaAssemblyRuntimeFixture(t)
		svc.mediaAssembler = &fakeMediaAssembler{skipOutput: true}
		if _, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan}); err != nil {
			t.Fatal(err)
		}
		if err := svc.ProcessNextTask(); !errors.Is(err, ErrMediaAssemblyOutputMissing) {
			t.Fatalf("missing output error = %v", err)
		}
	})
	t.Run("corrupt output", func(t *testing.T) {
		svc, _, scope, plan := mediaAssemblyRuntimeFixture(t)
		svc.mediaAssembler = &fakeMediaAssembler{}
		svc.mediaFileProbe = func(context.Context, string) (mediaFileMetadata, error) {
			return mediaFileMetadata{}, errors.New("invalid mp4")
		}
		if _, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan}); err != nil {
			t.Fatal(err)
		}
		if err := svc.ProcessNextTask(); !errors.Is(err, ErrMediaAssemblyOutputInvalid) {
			t.Fatalf("corrupt output error = %v", err)
		}
	})
	t.Run("output exceeds runtime policy", func(t *testing.T) {
		svc, _, scope, plan := mediaAssemblyRuntimeFixture(t)
		policy := defaultRuntimePolicy()
		policy.Resource.GeneratedFileMB = 1
		if _, err := svc.UpdateRuntimePolicySetting(providerAdmin(), policy); err != nil {
			t.Fatal(err)
		}
		svc.mediaAssembler = &fakeMediaAssembler{outputContent: make([]byte, 1024*1024+1)}
		if _, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan}); err != nil {
			t.Fatal(err)
		}
		if err := svc.ProcessNextTask(); !errors.Is(err, ErrMediaAssemblyOutputInvalid) {
			t.Fatalf("oversized output error = %v", err)
		}
	})
	t.Run("lease loss", func(t *testing.T) {
		svc, db, scope, plan := mediaAssemblyRuntimeFixture(t)
		assembler := &fakeMediaAssembler{started: make(chan struct{}), release: make(chan struct{})}
		svc.mediaAssembler = assembler
		svc.mediaFileProbe = func(context.Context, string) (mediaFileMetadata, error) {
			return mediaFileMetadata{DurationMS: 1000, MimeType: "video/mp4"}, nil
		}
		if _, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan}); err != nil {
			t.Fatal(err)
		}
		claimed, err := svc.repo.ClaimNextTask(svc.workerID, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- svc.processClaimedTask(claimed) }()
		<-assembler.started
		if err := db.Model(&model.Task{}).Where("id = ?", claimed.ID).Update("lease_owner", "other-worker").Error; err != nil {
			t.Fatal(err)
		}
		close(assembler.release)
		if err := <-done; !errors.Is(err, repository.ErrTaskCompletionStateConflict) {
			t.Fatalf("lease loss error = %v", err)
		}
	})
}

func TestMediaAssemblyTaskInputRejectsTrailingJSON(t *testing.T) {
	svc, _, scope, plan := mediaAssemblyRuntimeFixture(t)
	task, err := svc.EnqueueAgentMediaAssembly(EnqueueAgentMediaAssemblyInput{Scope: scope, PlanRevision: plan})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeMediaAssemblyTaskInput(task.InputJSON + `{}`); err == nil {
		t.Fatal("trailing JSON error = nil")
	}
}

func TestMediaAssemblyUsesVideoTimeoutPolicy(t *testing.T) {
	policy := RuntimeTaskPolicy{VideoTimeoutMinutes: 17, DefaultTimeoutMinutes: 3}
	if got := taskExecutionTimeoutWithPolicy(agentMediaAssemblyTaskType, policy); got != 17*time.Minute {
		t.Fatalf("assembly timeout = %s, want %s", got, 17*time.Minute)
	}
}

func TestMediaAssemblyStagingHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := io.ReadAll(contextReader{ctx: ctx, reader: bytes.NewReader([]byte("video"))})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("context reader error = %v, want context canceled", err)
	}
}

func TestMediaAssemblyStagesTeamResourceOwnedByAnotherMember(t *testing.T) {
	svc, db := newMembershipTestService(t)
	svc.mediaDurationProbe = func(context.Context, io.Reader) (int64, error) { return 1000, nil }
	owner := createTeamTestUser(t, db, "assembly-team-owner", "assembly-team-owner@example.com")
	member := createTeamTestUser(t, db, "assembly-team-member", "assembly-team-member@example.com")
	team, err := svc.CreateTeam(owner, teamCreateRequest("装配团队"))
	if err != nil {
		t.Fatal(err)
	}
	activateTeamTestSubscription(t, db, team, owner, 2)
	now := time.Now().UTC()
	if err := db.Create(&model.TeamMember{
		ID: newID(), TeamID: team.ID, UserID: member.ID, Role: model.TeamMemberRoleMember,
		Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	resource, err := svc.UploadTeamResource(owner.ID, team.ID, multipartFileHeader(t, "clip.mp4", "team-video"), "video", 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "clip.mp4")
	scope := agentruntime.Scope{TenantKind: agentruntime.TenantTeam, TenantID: team.ID, ActorUserID: member.ID}
	if err := svc.stageAssemblyResource(context.Background(), scope, *resource, target); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "team-video" {
		t.Fatalf("staged content = %q", string(content))
	}
}

func TestMediaAssemblyAdapterRunsFFmpegFixture(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	output := filepath.Join(t.TempDir(), "fixture.mp4")
	command := MediaAssemblyCommand{Executable: ffmpeg, Arguments: []string{
		"-hide_banner", "-nostdin", "-f", "lavfi", "-i", "color=c=black:s=16x16:d=0.1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-f", "mp4", output,
	}}
	if err := (execMediaAssembler{}).Assemble(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		t.Fatalf("fixture output invalid: info=%v err=%v", info, err)
	}
	invalidOutput := filepath.Join(t.TempDir(), "invalid.mp4")
	invalid := MediaAssemblyCommand{Executable: ffmpeg, Arguments: []string{"-hide_banner", "-nostdin", "-i", "missing-input.mp4", invalidOutput}}
	if err := (execMediaAssembler{}).Assemble(context.Background(), invalid); err == nil {
		t.Fatal("invalid ffmpeg command error = nil")
	}
}

func mediaAssemblyRuntimeFixture(t *testing.T) (*Service, *gorm.DB, agentruntime.Scope, agentruntime.ArtifactRevisionRef) {
	t.Helper()
	svc, db, _ := newAgentRuntimeServiceFixture(t, "https://provider.invalid")
	scope := agentRuntimeServiceScope()
	now := time.Now().UTC()
	if err := db.Create(&model.AgentThread{ID: scope.ThreadID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, CreatedByUserID: scope.ActorUserID, DomainProjectID: scope.DomainProjectID, CanvasID: scope.CanvasID, Status: agentruntime.ThreadActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentRun{ID: scope.RunID, ThreadID: scope.ThreadID, ActorUserID: scope.ActorUserID, ClientRequestID: "assembly-run", Status: agentruntime.RunRunning, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	svc.mediaDurationProbe = func(context.Context, io.Reader) (int64, error) { return 1000, nil }
	resource, err := svc.storeScopedResourceWithIdentity("assembly-input-resource", scope.ActorUserID, "", "video", "input.mp4", "video/mp4", 5, 16, 16, bytes.NewReader([]byte("video")))
	if err != nil {
		t.Fatal(err)
	}
	resource.ETag = "assembly-input-etag"
	if err := svc.repo.SaveResource(resource); err != nil {
		t.Fatal(err)
	}
	candidatePayload, _ := json.Marshal(agentruntime.MediaCandidateContent{CandidateKey: "clip", MediaKind: agentruntime.ArtifactVideo, ProviderRequestIdentity: "provider-request", ResourceID: resource.ID, SourceTaskID: "source-task"})
	candidate, err := svc.repo.AppendMediaCandidateRevision(scope, "assembly-input-artifact", agentruntime.ArtifactDraft{ArtifactKey: "clip", Kind: "media_candidate", SchemaVersion: 1, Payload: candidatePayload, ResourceID: resource.ID, ModelRequestIdentity: "provider-request", UpstreamRevisions: []agentruntime.ArtifactRevisionRef{}})
	if err != nil {
		t.Fatal(err)
	}
	approveMediaAssemblyRevision(t, db, scope, candidate.ID, 1)
	planPayload := json.RawMessage(`{"planKey":"assembly-plan","audioMode":"none","clips":[{"clipKey":"clip","sourceRevision":{"artifactId":"assembly-input-artifact","revisionId":"` + candidate.ID + `"},"trimStartMs":0,"trimEndMs":1000,"nativeAudioGainMilliDb":null,"transitionToNext":{"kind":"cut","durationMs":0}}],"audioTracks":[],"output":{"artifactKey":"final-video","container":"mp4","videoCodec":"h264","audioCodec":"none","width":16,"height":16,"frameRate":24}}`)
	planRevision, err := svc.repo.AppendArtifactRevisionOnce(scope, "assembly-plan-artifact", agentruntime.ArtifactDraft{ArtifactKey: "assembly-plan", Kind: "assembly_plan", SchemaVersion: 2, Payload: planPayload, UpstreamRevisions: []agentruntime.ArtifactRevisionRef{{ArtifactID: candidate.ArtifactID, RevisionID: candidate.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	approveMediaAssemblyRevision(t, db, scope, planRevision.ID, 2)
	return svc, db, scope, agentruntime.ArtifactRevisionRef{ArtifactID: planRevision.ArtifactID, RevisionID: planRevision.ID}
}

func approveMediaAssemblyRevision(t *testing.T, db *gorm.DB, scope agentruntime.Scope, revisionID string, ordinal int64) {
	t.Helper()
	now := time.Now().UTC()
	content, err := json.Marshal(agentruntime.StageReviewResolutionContent{ContentType: agentruntime.StageReviewContentType, StageID: "assembly-stage-" + revisionID, StageVersion: 1, RevisionID: revisionID, Decision: agentruntime.StageReviewApprove, ClientRequestID: "approve-" + revisionID, ResultStageVersion: 2, ResultStatus: agentruntime.StageApproved, ResultReviewRevisionID: revisionID, ResultUpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AgentTimelineItem{ID: "approval-" + revisionID, TenantKind: scope.TenantKind, TenantID: scope.TenantID, ThreadID: scope.ThreadID, RunID: scope.RunID, Kind: model.AgentTimelineItemApproval, Status: model.AgentTimelineItemCompleted, Ordinal: ordinal, SourceEventSequence: ordinal, ContentJSON: string(content), StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
}
