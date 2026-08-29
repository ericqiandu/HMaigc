package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

var (
	ErrMediaAssemblyOutputMissing = errors.New("media assembly output is missing")
	ErrMediaAssemblyOutputInvalid = errors.New("media assembly output is invalid")
)

type MediaAssembler interface {
	Assemble(context.Context, MediaAssemblyCommand) error
}

type execMediaAssembler struct{}

func (execMediaAssembler) Assemble(ctx context.Context, command MediaAssemblyCommand) error {
	if strings.TrimSpace(command.Executable) == "" || len(command.Arguments) == 0 {
		return ErrMediaAssemblyCommandInvalid
	}
	process := exec.CommandContext(ctx, command.Executable, command.Arguments...)
	output, err := process.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg 装配失败: %w: %s", err, truncateRunes(strings.TrimSpace(string(output)), 1000))
	}
	return nil
}

type EnqueueAgentMediaAssemblyInput struct {
	Scope         agentruntime.Scope
	PlanRevision  agentruntime.ArtifactRevisionRef
	ToolCallID    string
	ActionVersion int
}

type mediaAssemblyTaskInput struct {
	Scope             agentruntime.Scope               `json:"scope"`
	PlanRevision      agentruntime.ArtifactRevisionRef `json:"planRevision"`
	PlanDigest        string                           `json:"planDigest"`
	OutputArtifactID  string                           `json:"outputArtifactId"`
	OutputArtifactKey string                           `json:"outputArtifactKey"`
	ToolCallID        string                           `json:"toolCallId,omitempty"`
	ActionVersion     int                              `json:"actionVersion,omitempty"`
}

type mediaAssemblyTaskResult struct {
	ResourceID       string                           `json:"resourceId"`
	PlanDigest       string                           `json:"planDigest"`
	ArtifactRevision agentruntime.ArtifactRevisionRef `json:"artifactRevision"`
}

func (s *Service) EnqueueAgentMediaAssembly(input EnqueueAgentMediaAssemblyInput) (*model.Task, error) {
	if err := input.Scope.Validate(); err != nil || !input.Scope.CanMutateCanvas() || input.PlanRevision.Validate() != nil {
		return nil, BadAuthRequest("媒体装配作用域或计划版本无效")
	}
	draft, err := s.approvedAssemblyPlanDraft(input.Scope, input.PlanRevision)
	if err != nil {
		return nil, err
	}
	plan, err := agentruntime.DecodeAssemblyPlanV2(draft.Payload)
	if err != nil {
		return nil, err
	}
	inputs, err := s.resolveApprovedAssemblyInputs(input.Scope, plan, validatedAssemblyPaths(plan))
	if err != nil {
		return nil, err
	}
	command, err := BuildMediaAssemblyCommand(draft, inputs, filepath.Join("validated", "output.mp4"))
	if err != nil {
		return nil, err
	}
	identity := mediaAssemblyIdentity(input.Scope, input.PlanRevision, command.PlanDigest, command.OutputArtifactKey)
	operation, err := agentruntime.MediaAssemblyOperationForRun(input.Scope.RunID)
	if err != nil {
		return nil, err
	}
	frozen := mediaAssemblyTaskInput{
		Scope: input.Scope, PlanRevision: input.PlanRevision, PlanDigest: command.PlanDigest,
		OutputArtifactID: "final-" + identity[:24], OutputArtifactKey: command.OutputArtifactKey,
		ToolCallID: input.ToolCallID, ActionVersion: input.ActionVersion,
	}
	frozenJSON, err := json.Marshal(frozen)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	task := &model.Task{
		ID: "assembly-" + identity[:24], UserID: input.Scope.ActorUserID, Audience: model.TaskAudienceInternal,
		ExecutionKind: model.TaskExecutionLocalMediaAssembly, ProjectID: input.Scope.CanvasID,
		Type: agentruntime.MediaAssemblyTaskType, Capability: "video", Status: model.TaskStatusQueued,
		Stage: "等待本地装配", Progress: 5, Operation: operation,
		InputJSON: string(frozenJSON), CreatedAt: now, UpdatedAt: now,
	}
	return s.repo.CreateInternalUnbilledTaskOnce(task)
}

func (s *Service) CancelAgentMediaAssembly(scope agentruntime.Scope, taskID string) (*model.Task, error) {
	task, err := s.repo.CancelInternalMediaAssemblyTask(scope, taskID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	s.cancelActiveTask(task.ID)
	return task, nil
}

func (s *Service) processClaimedMediaAssembly(ctx context.Context, task *model.Task) error {
	input, err := decodeMediaAssemblyTaskInput(task.InputJSON)
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	if task.UserID != input.Scope.ActorUserID || task.ProjectID != input.Scope.CanvasID ||
		task.Audience != model.TaskAudienceInternal || task.ExecutionKind != model.TaskExecutionLocalMediaAssembly ||
		task.Type != agentruntime.MediaAssemblyTaskType || task.Capability != "video" || task.BillingOrderID != "" {
		return s.failClaimedMediaAssembly(task, repository.ErrInternalTaskFactConflict)
	}
	if task.Status == model.TaskStatusCancelled {
		return nil
	}
	draft, err := s.approvedAssemblyPlanDraft(input.Scope, input.PlanRevision)
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	plan, err := agentruntime.DecodeAssemblyPlanV2(draft.Payload)
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	workspaceRoot := filepath.Join(s.dataDir, "agent-media-assembly")
	if err := os.MkdirAll(workspaceRoot, 0o750); err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	workspace, err := os.MkdirTemp(workspaceRoot, task.ID+"-")
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	paths := assemblyWorkspacePaths(plan, workspace)
	inputs, err := s.resolveApprovedAssemblyInputs(input.Scope, plan, paths)
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	for index := range inputs {
		if err := s.stageAssemblyResource(ctx, input.Scope, inputs[index].Resource, inputs[index].InputPath); err != nil {
			return s.failClaimedMediaAssembly(task, err)
		}
	}
	outputPath := filepath.Join(workspace, "output.mp4")
	command, err := BuildMediaAssemblyCommand(draft, inputs, outputPath)
	if err != nil || command.PlanDigest != input.PlanDigest || command.OutputArtifactKey != input.OutputArtifactKey {
		if err == nil {
			err = repository.ErrInternalTaskFactConflict
		}
		return s.failClaimedMediaAssembly(task, err)
	}
	identity := mediaAssemblyIdentity(input.Scope, input.PlanRevision, command.PlanDigest, command.OutputArtifactKey)
	if task.ID != "assembly-"+identity[:24] || input.OutputArtifactID != "final-"+identity[:24] {
		return s.failClaimedMediaAssembly(task, repository.ErrInternalTaskFactConflict)
	}
	assembler := s.mediaAssembler
	if assembler == nil {
		assembler = execMediaAssembler{}
	}
	if err := assembler.Assemble(ctx, command); err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = ErrMediaAssemblyOutputMissing
		}
		return s.failClaimedMediaAssembly(task, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return s.failClaimedMediaAssembly(task, ErrMediaAssemblyOutputInvalid)
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	maxOutputBytes := policy.Resource.GeneratedFileMB * 1024 * 1024
	if info.Size() > maxOutputBytes {
		return s.failClaimedMediaAssembly(task, fmt.Errorf("%w: 装配输出超过单个生成资源上限 %dMB", ErrMediaAssemblyOutputInvalid, policy.Resource.GeneratedFileMB))
	}
	probe := s.mediaFileProbe
	if probe == nil {
		probe = ffprobeMediaFile
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	metadata, probeErr := probe(probeCtx, outputPath)
	cancel()
	if probeErr != nil || metadata.DurationMS <= 0 || metadata.MimeType != "video/mp4" {
		if probeErr != nil {
			return s.failClaimedMediaAssembly(task, fmt.Errorf("%w: %v", ErrMediaAssemblyOutputInvalid, probeErr))
		}
		return s.failClaimedMediaAssembly(task, ErrMediaAssemblyOutputInvalid)
	}
	checksum, err := sha256File(outputPath)
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	body, err := os.Open(outputPath)
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	resourceID := "assembled-" + task.ID[len("assembly-"):]
	teamID := ""
	if input.Scope.TenantKind == agentruntime.TenantTeam {
		teamID = input.Scope.TenantID
	}
	resource, storeErr := s.storeScopedResourceWithIdentity(resourceID, input.Scope.ActorUserID, teamID, "video", "final.mp4", "video/mp4", info.Size(), *plan.Output.Width, *plan.Output.Height, body)
	closeErr := body.Close()
	if storeErr != nil {
		return s.failClaimedMediaAssembly(task, storeErr)
	}
	if closeErr != nil {
		return s.failClaimedMediaAssembly(task, closeErr)
	}
	if err := s.verifyStoredAssemblyChecksum(input.Scope, resource.ID, checksum); err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	resource.DurationMs = metadata.DurationMS
	resource.ETag = checksum
	if err := s.repo.SaveResource(resource); err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	payload, err := json.Marshal(agentruntime.MediaCandidateContent{
		CandidateKey: input.OutputArtifactKey, MediaKind: agentruntime.ArtifactVideo,
		ProviderRequestIdentity: "local-assembly:" + input.PlanDigest, ResourceID: resource.ID, SourceTaskID: task.ID,
	})
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	finalDraft := agentruntime.ArtifactDraft{
		ArtifactKey: input.OutputArtifactKey, Kind: "media_candidate", SchemaVersion: 1, Payload: payload,
		ResourceID: resource.ID, ModelRequestIdentity: "local-assembly:" + input.PlanDigest,
		UpstreamRevisions: []agentruntime.ArtifactRevisionRef{input.PlanRevision},
	}
	_, err = s.repo.FinalizeMediaAssembly(repository.MediaAssemblyFinalization{
		TaskID: task.ID, UserID: task.UserID, LeaseOwner: task.LeaseOwner, Scope: input.Scope,
		ArtifactID: input.OutputArtifactID, Draft: finalDraft, ResourceID: resource.ID,
		PlanDigest: input.PlanDigest, PlanRevision: input.PlanRevision, CompletedAt: time.Now().UTC(),
	})
	if err != nil {
		return s.failClaimedMediaAssembly(task, err)
	}
	latest, loadErr := s.repo.Task(task.ID)
	if loadErr != nil {
		return loadErr
	}
	if latest.Status == model.TaskStatusCancelled {
		if timelineErr := s.appendPersistedMediaAssemblyTaskTimeline(input.Scope, *latest); timelineErr != nil {
			return timelineErr
		}
	}
	_ = s.log(task.UserID, task.ID, "info", "本地媒体装配完成", "")
	return nil
}

func (s *Service) failClaimedMediaAssembly(task *model.Task, failure error) error {
	latest, loadErr := s.repo.Task(task.ID)
	if loadErr == nil && latest.Status == model.TaskStatusCancelled {
		_ = s.log(task.UserID, task.ID, "warn", "本地媒体装配已取消", taskFailureMessage(failure))
		return nil
	}
	if err := s.repo.FailMediaAssemblyTask(task.ID, task.UserID, task.LeaseOwner, failure, time.Now().UTC()); err != nil {
		return errors.Join(failure, err)
	}
	_ = s.log(task.UserID, task.ID, "error", "本地媒体装配失败", taskFailureMessage(failure))
	return failure
}

func (s *Service) approvedAssemblyPlanDraft(scope agentruntime.Scope, reference agentruntime.ArtifactRevisionRef) (agentruntime.ArtifactDraft, error) {
	revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
	if err != nil {
		return agentruntime.ArtifactDraft{}, err
	}
	approved, err := s.repo.ApprovedArtifactRevisionIDsForScope(scope)
	if err != nil {
		return agentruntime.ArtifactDraft{}, err
	}
	if _, ok := approved[revision.ID]; !ok {
		return agentruntime.ArtifactDraft{}, errors.New("装配计划版本尚未批准")
	}
	if revision.LifecycleStatus == model.AgentArtifactRevisionStale {
		return agentruntime.ArtifactDraft{}, errors.New("装配计划版本已过期")
	}
	draft, err := artifactDraftFromRevision(*revision)
	if err != nil {
		return agentruntime.ArtifactDraft{}, err
	}
	if draft.SchemaID() != agentruntime.ArtifactSchemaAssemblyPlanV2 {
		return agentruntime.ArtifactDraft{}, ErrMediaAssemblyPlanVersionUnsupported
	}
	return draft, nil
}

func (s *Service) resolveApprovedAssemblyInputs(scope agentruntime.Scope, plan agentruntime.AssemblyPlanV2, paths []string) ([]MediaAssemblyInput, error) {
	references := make([]agentruntime.ArtifactRevisionRef, 0, len(plan.Clips)+len(plan.AudioTracks))
	for _, clip := range plan.Clips {
		references = append(references, clip.SourceRevision)
	}
	for _, track := range plan.AudioTracks {
		references = append(references, track.SourceRevision)
	}
	if len(paths) != len(references) {
		return nil, ErrMediaAssemblyCommandInvalid
	}
	approved, err := s.repo.ApprovedArtifactRevisionIDsForScope(scope)
	if err != nil {
		return nil, err
	}
	inputs := make([]MediaAssemblyInput, 0, len(references))
	for index, reference := range references {
		revision, err := s.repo.ArtifactRevisionForArtifactInScope(scope, reference.ArtifactID, reference.RevisionID)
		if err != nil {
			return nil, err
		}
		if _, ok := approved[revision.ID]; !ok || revision.LifecycleStatus == model.AgentArtifactRevisionStale || revision.ResourceID == "" {
			return nil, errors.New("装配输入版本未批准、已过期或缺少资源")
		}
		kind := agentruntime.ArtifactKind(revision.Kind)
		if revision.Kind == "media_candidate" {
			candidate, decodeErr := agentruntime.DecodeMediaCandidateContent([]byte(revision.PayloadJSON))
			if decodeErr != nil || candidate.ResourceID != revision.ResourceID {
				return nil, errors.New("装配媒体候选事实无效")
			}
			kind = candidate.MediaKind
		}
		resource, err := s.repo.Resource(revision.ResourceID)
		if err != nil || !assemblyResourceInScope(scope, resource) {
			return nil, errors.New("装配资源作用域无效")
		}
		inputs = append(inputs, MediaAssemblyInput{
			Evidence: agentruntime.DeliveryArtifact{Kind: kind, ArtifactID: revision.ArtifactID, RevisionID: revision.ID, ResourceID: resource.ID, ResourceReady: resource.Status == model.ResourceStatusReady, Approved: true},
			Resource: *resource, InputPath: paths[index],
		})
	}
	return inputs, nil
}

func (s *Service) stageAssemblyResource(ctx context.Context, scope agentruntime.Scope, resource model.Resource, targetPath string) error {
	_, body, err := s.openAssemblyResource(scope, resource.ID)
	if err != nil {
		return err
	}
	defer body.Close()
	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(contextReader{ctx: ctx, reader: body}, resource.Size+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != resource.Size {
		return errors.New("装配输入资源大小与持久事实不一致")
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(buffer)
	}
}

func (s *Service) openAssemblyResource(scope agentruntime.Scope, resourceID string) (*model.Resource, io.ReadCloser, error) {
	if scope.TenantKind == agentruntime.TenantTeam {
		stream, err := s.OpenTeamResourceRange(scope.ActorUserID, scope.TenantID, resourceID, "")
		if err != nil {
			return nil, nil, err
		}
		return stream.Resource, stream.Body, nil
	}
	return s.OpenResource(scope.ActorUserID, resourceID)
}

func artifactDraftFromRevision(revision model.AgentArtifactRevision) (agentruntime.ArtifactDraft, error) {
	var upstream []agentruntime.ArtifactRevisionRef
	var skills []agentruntime.SkillSelection
	if err := json.Unmarshal([]byte(revision.UpstreamRevisionsJSON), &upstream); err != nil {
		return agentruntime.ArtifactDraft{}, err
	}
	if err := json.Unmarshal([]byte(revision.SkillVersionsJSON), &skills); err != nil {
		return agentruntime.ArtifactDraft{}, err
	}
	return agentruntime.ArtifactDraft{ArtifactKey: revision.ArtifactKey, Kind: revision.Kind, SchemaVersion: revision.SchemaVersion, Payload: json.RawMessage(revision.PayloadJSON), ResourceID: revision.ResourceID, UpstreamRevisions: upstream, ModelRequestIdentity: revision.ModelRequestIdentity, SkillVersions: skills}, nil
}

func decodeMediaAssemblyTaskInput(value string) (mediaAssemblyTaskInput, error) {
	var input mediaAssemblyTaskInput
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return mediaAssemblyTaskInput{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return mediaAssemblyTaskInput{}, repository.ErrInternalTaskFactConflict
		}
		return mediaAssemblyTaskInput{}, err
	}
	toolIdentityAbsent := input.ToolCallID == "" && input.ActionVersion == 0
	toolIdentityPresent := input.ToolCallID != "" && input.ActionVersion > 0
	if input.Scope.Validate() != nil || input.PlanRevision.Validate() != nil || len(input.PlanDigest) != 64 || input.OutputArtifactID == "" || input.OutputArtifactKey == "" ||
		(!toolIdentityAbsent && !toolIdentityPresent) {
		return mediaAssemblyTaskInput{}, repository.ErrInternalTaskFactConflict
	}
	return input, nil
}

func decodeMediaAssemblyTaskResult(value string) (mediaAssemblyTaskResult, error) {
	var result mediaAssemblyTaskResult
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return mediaAssemblyTaskResult{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return mediaAssemblyTaskResult{}, repository.ErrInternalTaskFactConflict
		}
		return mediaAssemblyTaskResult{}, err
	}
	if result.ResourceID == "" || len(result.PlanDigest) != 64 || result.ArtifactRevision.Validate() != nil {
		return mediaAssemblyTaskResult{}, repository.ErrInternalTaskFactConflict
	}
	return result, nil
}

func (s *Service) appendPersistedMediaAssemblyTaskTimeline(scope agentruntime.Scope, task model.Task) error {
	input, err := decodeMediaAssemblyTaskInput(task.InputJSON)
	if err != nil {
		return err
	}
	if input.ToolCallID == "" {
		return nil
	}
	call := &agentruntime.ToolCallDecision{
		ToolCallID: input.ToolCallID, ToolName: agentruntime.ToolMediaAssemble,
		ActionVersion: input.ActionVersion,
	}
	content, err := s.mediaAssemblyTimelineContent(
		scope,
		call,
		MediaAssembleArguments{PlanRevision: input.PlanRevision},
		task,
	)
	if err != nil {
		return err
	}
	_, err = s.repo.AppendAgentMediaAssemblyTimeline(scope, content)
	return err
}

func mediaAssemblyIdentity(scope agentruntime.Scope, revision agentruntime.ArtifactRevisionRef, planDigest string, outputKey string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{string(scope.TenantKind), scope.TenantID, scope.ActorUserID, scope.DomainProjectID, scope.CanvasID, scope.ThreadID, scope.RunID, revision.ArtifactID, revision.RevisionID, planDigest, outputKey}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func validatedAssemblyPaths(plan agentruntime.AssemblyPlanV2) []string {
	return assemblyWorkspacePaths(plan, "validated")
}

func assemblyWorkspacePaths(plan agentruntime.AssemblyPlanV2, root string) []string {
	paths := make([]string, 0, len(plan.Clips)+len(plan.AudioTracks))
	for index := range plan.Clips {
		paths = append(paths, filepath.Join(root, fmt.Sprintf("video-%03d.mp4", index)))
	}
	for index := range plan.AudioTracks {
		paths = append(paths, filepath.Join(root, fmt.Sprintf("audio-%03d.m4a", index)))
	}
	return paths
}

func assemblyResourceInScope(scope agentruntime.Scope, resource *model.Resource) bool {
	if resource == nil {
		return false
	}
	if scope.TenantKind == agentruntime.TenantTeam {
		return resource.TeamID == scope.TenantID
	}
	return resource.UserID == scope.ActorUserID && resource.TeamID == ""
}

func sha256File(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return sha256Reader(file)
}

func sha256Reader(reader io.Reader) (string, error) {
	digest := sha256.New()
	if _, err := io.Copy(digest, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (s *Service) verifyStoredAssemblyChecksum(scope agentruntime.Scope, resourceID string, expected string) error {
	resource, body, err := s.openAssemblyResource(scope, resourceID)
	if err != nil {
		return err
	}
	if !assemblyResourceInScope(scope, resource) {
		_ = body.Close()
		return errors.New("装配输出资源作用域无效")
	}
	actual, hashErr := sha256Reader(body)
	closeErr := body.Close()
	if hashErr != nil {
		return hashErr
	}
	if closeErr != nil {
		return closeErr
	}
	if actual != expected {
		return errors.New("确定性装配输出与已存资源校验和冲突")
	}
	return nil
}
