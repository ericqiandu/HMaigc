package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type CanvasAccessView struct {
	Level                  model.CanvasAccessLevel `json:"level"`
	TeamID                 string                  `json:"teamId,omitempty"`
	TeamName               string                  `json:"teamName,omitempty"`
	TeamRole               model.TeamMemberRole    `json:"teamRole,omitempty"`
	CanEdit                bool                    `json:"canEdit"`
	CanManage              bool                    `json:"canManage"`
	TeamSubscriptionActive bool                    `json:"teamSubscriptionActive"`
}

type CanvasCollaborationState struct {
	Project       json.RawMessage                       `json:"project"`
	Access        CanvasAccessView                      `json:"access"`
	Collaborators []repository.CanvasCollaboratorRecord `json:"collaborators"`
	TeamMembers   []repository.TeamMemberRecord         `json:"teamMembers"`
}

type ConfigureCanvasCollaborationRequest struct {
	TeamID        string                  `json:"teamId"`
	DefaultAccess model.CanvasAccessLevel `json:"defaultAccess"`
}

type UpdateCanvasCollaboratorRequest struct {
	UserID string                  `json:"userId"`
	Access model.CanvasAccessLevel `json:"access"`
}

type CanvasDocumentPatch struct {
	Title          *string              `json:"title,omitempty"`
	BackgroundMode *string              `json:"backgroundMode,omitempty"`
	ShowImageInfo  *bool                `json:"showImageInfo,omitempty"`
	DirectorScenes *json.RawMessage     `json:"directorScenes,omitempty"`
	Viewport       *CanvasViewportPatch `json:"viewport,omitempty"`
}

type CanvasViewportPatch struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	K float64 `json:"k"`
}

type CanvasMutationPatch struct {
	UpsertNodes         []json.RawMessage    `json:"upsertNodes,omitempty"`
	DeleteNodeIDs       []string             `json:"deleteNodeIds,omitempty"`
	UpsertConnections   []json.RawMessage    `json:"upsertConnections,omitempty"`
	DeleteConnectionIDs []string             `json:"deleteConnectionIds,omitempty"`
	Document            *CanvasDocumentPatch `json:"document,omitempty"`
}

type CanvasMutationRequest struct {
	BaseRevision     int64               `json:"baseRevision"`
	ClientMutationID string              `json:"clientMutationId"`
	Patch            CanvasMutationPatch `json:"patch"`
}

type CanvasMutationResult struct {
	CanvasID         string              `json:"canvasId"`
	Revision         int64               `json:"revision"`
	ActorUserID      string              `json:"actorUserId"`
	ClientMutationID string              `json:"clientMutationId"`
	Patch            CanvasMutationPatch `json:"patch"`
	UpdatedAt        time.Time           `json:"updatedAt"`
}

type agentCanvasMutationCompletion struct {
	Scope           agentruntime.Scope
	ToolCallID      string
	ActionVersion   int
	ProposalHash    string
	ToolReceiptJSON string
}

type CanvasMutationConflictError struct {
	Code    string
	Message string
}

func (e *CanvasMutationConflictError) Error() string {
	return e.Message
}

func (e *CanvasMutationConflictError) Unwrap() error {
	return &AuthError{Status: http.StatusConflict, Message: e.Message}
}

func (s *Service) CanvasCollaboration(user *model.User, canvasID string) (*CanvasCollaborationState, error) {
	project, access, err := s.canvasAccess(user.ID, canvasID)
	if err != nil {
		return nil, err
	}
	payload, err := canvasProjectResponse(project, access)
	if err != nil {
		return nil, err
	}
	state := &CanvasCollaborationState{
		Project:       payload,
		Access:        access,
		Collaborators: []repository.CanvasCollaboratorRecord{},
		TeamMembers:   []repository.TeamMemberRecord{},
	}
	if project.TeamID == "" {
		return state, nil
	}
	state.Collaborators, err = s.repo.CanvasCollaboratorRecords(project.ID, project.TeamID)
	if err != nil {
		return nil, err
	}
	state.TeamMembers, err = s.repo.TeamMemberRecords(project.TeamID)
	if err != nil {
		return nil, err
	}
	return state, nil
}

func (s *Service) ConfigureCanvasCollaboration(
	user *model.User,
	canvasID string,
	req ConfigureCanvasCollaborationRequest,
) (*CanvasCollaborationState, error) {
	canvas, err := s.repo.CanvasProject(strings.TrimSpace(canvasID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &AuthError{Status: http.StatusNotFound, Message: "画布不存在"}
	}
	if err != nil {
		return nil, err
	}
	targetTeamID := strings.TrimSpace(req.TeamID)
	now := time.Now()

	if targetTeamID == "" {
		if canvas.TeamID == "" {
			return nil, BadAuthRequest("当前画布不是团队画布")
		}
		if canvas.UserID != user.ID {
			return nil, Forbidden("只有画布创建者可以将画布移回个人空间")
		}
		_, actor, accessErr := s.teamAccess(user.ID, canvas.TeamID)
		if accessErr != nil {
			return nil, accessErr
		}
		if !canManageTeam(actor.Role) {
			return nil, Forbidden("只有团队所有者或管理员可以解除团队协作")
		}
		audit := newTeamAuditEvent(canvas.TeamID, user.ID, "canvas.detached", canvasAuditMetadata(canvas.ID, canvas.Title), now)
		if err := s.repo.ConfigureCanvasTeam(canvas.ID, canvas.UserID, "", "", user.ID, audit, now); err != nil {
			return nil, err
		}
		return s.CanvasCollaboration(user, canvas.ID)
	}

	if !validCollaboratorAccess(req.DefaultAccess) {
		return nil, BadAuthRequest("团队默认权限必须是 viewer 或 editor")
	}
	_, actor, err := s.teamAccess(user.ID, targetTeamID)
	if err != nil {
		return nil, err
	}
	if !canManageTeam(actor.Role) {
		return nil, Forbidden("只有团队所有者或管理员可以配置协作画布")
	}
	if _, err := s.repo.ActiveTeamSubscription(targetTeamID, now); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &AuthError{Status: http.StatusConflict, Message: "团队尚未开通有效团队会员，不能启用多人协作"}
	} else if err != nil {
		return nil, err
	}
	if canvas.TeamID == "" {
		if canvas.UserID != user.ID {
			return nil, Forbidden("只有画布所有者可以将个人画布加入团队")
		}
	} else if canvas.TeamID != targetTeamID {
		return nil, &AuthError{Status: http.StatusConflict, Message: "画布已属于其他团队，不能直接转移"}
	} else {
		_, access, accessErr := s.canvasAccess(user.ID, canvas.ID)
		if accessErr != nil {
			return nil, accessErr
		}
		if !access.CanManage {
			return nil, Forbidden("当前用户不能修改画布协作设置")
		}
	}
	action := "canvas.access_updated"
	if canvas.TeamID == "" {
		action = "canvas.attached"
	}
	audit := newTeamAuditEvent(targetTeamID, user.ID, action, canvasAuditMetadata(canvas.ID, canvas.Title), now)
	if err := s.repo.ConfigureCanvasTeam(canvas.ID, canvas.UserID, targetTeamID, req.DefaultAccess, user.ID, audit, now); err != nil {
		return nil, err
	}
	return s.CanvasCollaboration(user, canvas.ID)
}

func (s *Service) UpdateCanvasCollaborator(
	user *model.User,
	canvasID string,
	req UpdateCanvasCollaboratorRequest,
) (*CanvasCollaborationState, error) {
	canvas, access, err := s.canvasAccess(user.ID, canvasID)
	if err != nil {
		return nil, err
	}
	if canvas.TeamID == "" || !access.CanManage {
		return nil, Forbidden("当前用户不能管理画布协作者")
	}
	if !validCollaboratorAccess(req.Access) {
		return nil, BadAuthRequest("协作者权限必须是 viewer 或 editor")
	}
	targetUserID := strings.TrimSpace(req.UserID)
	targetMember, err := s.repo.TeamMemberForUser(canvas.TeamID, targetUserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, BadAuthRequest("目标用户不是当前团队的有效成员")
	}
	if err != nil {
		return nil, err
	}
	if canManageTeam(targetMember.Role) {
		return nil, BadAuthRequest("团队所有者和管理员已拥有管理权限，无需单独配置")
	}
	now := time.Now()
	collaborator := &model.CanvasCollaborator{
		ID: newID(), CanvasID: canvas.ID, TeamID: canvas.TeamID, UserID: targetUserID,
		Access: req.Access, CreatedBy: user.ID, CreatedAt: now, UpdatedAt: now,
	}
	metadata, _ := json.Marshal(struct {
		CanvasID string                  `json:"canvasId"`
		Access   model.CanvasAccessLevel `json:"access"`
	}{CanvasID: canvas.ID, Access: req.Access})
	audit := newTeamAuditEvent(canvas.TeamID, user.ID, "canvas.collaborator_updated", string(metadata), now)
	audit.TargetUserID = targetUserID
	if err := s.repo.UpsertCanvasCollaborator(collaborator, audit); err != nil {
		return nil, err
	}
	return s.CanvasCollaboration(user, canvas.ID)
}

func (s *Service) DeleteCanvasCollaborator(user *model.User, canvasID string, targetUserID string) (*CanvasCollaborationState, error) {
	canvas, access, err := s.canvasAccess(user.ID, canvasID)
	if err != nil {
		return nil, err
	}
	if canvas.TeamID == "" || !access.CanManage {
		return nil, Forbidden("当前用户不能管理画布协作者")
	}
	now := time.Now()
	audit := newTeamAuditEvent(canvas.TeamID, user.ID, "canvas.collaborator_removed", canvasAuditMetadata(canvas.ID, canvas.Title), now)
	audit.TargetUserID = strings.TrimSpace(targetUserID)
	if err := s.repo.DeleteCanvasCollaborator(canvas.ID, audit.TargetUserID, audit); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &AuthError{Status: http.StatusNotFound, Message: "协作者权限记录不存在"}
	} else if err != nil {
		return nil, err
	}
	return s.CanvasCollaboration(user, canvas.ID)
}

func (s *Service) CommitCanvasMutation(
	user *model.User,
	canvasID string,
	req CanvasMutationRequest,
) (*CanvasMutationResult, error) {
	return s.commitCanvasMutation(user, canvasID, req, nil)
}

func (s *Service) commitCanvasMutation(
	user *model.User,
	canvasID string,
	req CanvasMutationRequest,
	agentCompletion *agentCanvasMutationCompletion,
) (*CanvasMutationResult, error) {
	canvas, access, err := s.canvasAccess(user.ID, canvasID)
	if err != nil {
		return nil, err
	}
	if !access.CanEdit {
		if !access.TeamSubscriptionActive {
			return nil, &AuthError{Status: http.StatusPaymentRequired, Message: "团队会员已失效，画布暂时为只读"}
		}
		return nil, Forbidden("当前用户只有查看权限")
	}
	if req.BaseRevision < 0 {
		return nil, BadAuthRequest("画布基础版本无效")
	}
	req.ClientMutationID = strings.TrimSpace(req.ClientMutationID)
	if req.ClientMutationID == "" || len(req.ClientMutationID) > 80 {
		return nil, BadAuthRequest("客户端变更 ID 不能为空且最多 80 个字符")
	}
	if err := validateCanvasMutationPatch(req.Patch); err != nil {
		return nil, err
	}
	if err := s.validateCanvasMutationResources(user.ID, canvas.PayloadJSON, req.Patch); err != nil {
		return nil, err
	}
	req.Patch, err = canonicalizeCanvasMutationResourceURLs(req.Patch, canvas.ID)
	if err != nil {
		return nil, err
	}
	changePayload, err := json.Marshal(req.Patch)
	if err != nil {
		return nil, err
	}
	if len(changePayload) > maxCanvasMutationBytes {
		return nil, BadAuthRequest("单次画布变更不能超过 2MB")
	}
	now := time.Now()
	apply := func(current *model.CanvasProject) (string, string, error) {
		return applyCanvasMutationPatch(current.PayloadJSON, current.Title, req.Patch)
	}
	var commit *repository.CanvasChangeCommit
	if agentCompletion == nil {
		commit, err = s.repo.CommitCanvasChange(
			canvas.ID, newID(), user.ID, req.BaseRevision, req.ClientMutationID, string(changePayload), now, apply,
		)
	} else {
		commit, err = s.repo.CommitAgentCanvasChange(repository.AgentCanvasChangeInput{
			Scope: agentCompletion.Scope, ToolCallID: agentCompletion.ToolCallID, ActionVersion: agentCompletion.ActionVersion,
			ProposalHash: agentCompletion.ProposalHash, CanvasID: canvas.ID, ChangeID: newID(), ActorUserID: user.ID,
			BaseRevision: req.BaseRevision, ClientMutationID: req.ClientMutationID, ChangePayloadJSON: string(changePayload),
			ToolReceiptJSON: agentCompletion.ToolReceiptJSON, Now: now, Apply: apply,
		})
	}
	if errors.Is(err, repository.ErrCanvasRevisionConflict) {
		return nil, &CanvasMutationConflictError{Code: "canvas_revision_conflict", Message: "画布版本已更新，请同步最新内容后重试"}
	}
	if errors.Is(err, repository.ErrCanvasMutationMismatch) {
		return nil, &CanvasMutationConflictError{Code: "canvas_mutation_idempotency_conflict", Message: "客户端变更 ID 已被其他内容使用"}
	}
	if errors.Is(err, repository.ErrCanvasPlanInactive) {
		return nil, &AuthError{Status: http.StatusPaymentRequired, Message: "团队会员已失效，画布暂时为只读"}
	}
	if errors.Is(err, repository.ErrCanvasWriteForbidden) {
		return nil, Forbidden("当前用户没有团队画布编辑权限")
	}
	if errors.Is(err, repository.ErrAgentRuntimeStepConflict) {
		return nil, &CanvasMutationConflictError{Code: "canvas_tool_receipt_conflict", Message: "画布提交与 Agent 工具回执冲突"}
	}
	if err != nil {
		return nil, err
	}
	var committedPatch CanvasMutationPatch
	if err := json.Unmarshal([]byte(commit.Change.PayloadJSON), &committedPatch); err != nil {
		return nil, err
	}
	committedPatch, err = rewriteCanvasMutationResourceURLs(committedPatch, canvas.ID)
	if err != nil {
		return nil, err
	}
	return &CanvasMutationResult{
		CanvasID: commit.Project.ID, Revision: commit.Change.Revision,
		ActorUserID: commit.Change.ActorUserID, ClientMutationID: commit.Change.ClientMutationID,
		Patch: committedPatch, UpdatedAt: commit.Project.UpdatedAt,
	}, nil
}

func (s *Service) canvasAccess(userID string, canvasID string) (*model.CanvasProject, CanvasAccessView, error) {
	canvas, err := s.repo.CanvasProject(strings.TrimSpace(canvasID))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, CanvasAccessView{}, &AuthError{Status: http.StatusNotFound, Message: "画布不存在"}
	}
	if err != nil {
		return nil, CanvasAccessView{}, err
	}
	if canvas.TeamID == "" {
		if canvas.UserID != userID {
			return nil, CanvasAccessView{}, &AuthError{Status: http.StatusNotFound, Message: "画布不存在"}
		}
		return canvas, CanvasAccessView{Level: model.CanvasAccessManager, CanEdit: true, CanManage: true, TeamSubscriptionActive: true}, nil
	}
	team, member, err := s.teamAccess(userID, canvas.TeamID)
	if err != nil {
		return nil, CanvasAccessView{}, &AuthError{Status: http.StatusNotFound, Message: "画布不存在"}
	}
	subscriptionActive := true
	if _, err := s.repo.ActiveTeamSubscription(canvas.TeamID, time.Now()); errors.Is(err, gorm.ErrRecordNotFound) {
		subscriptionActive = false
	} else if err != nil {
		return nil, CanvasAccessView{}, err
	}
	level := canvas.DefaultTeamAccess
	if canManageTeam(member.Role) {
		level = model.CanvasAccessManager
	} else {
		override, overrideErr := s.repo.CanvasCollaboratorForUser(canvas.ID, userID)
		if overrideErr == nil {
			level = override.Access
		} else if !errors.Is(overrideErr, gorm.ErrRecordNotFound) {
			return nil, CanvasAccessView{}, overrideErr
		}
	}
	if level != model.CanvasAccessViewer && level != model.CanvasAccessEditor && level != model.CanvasAccessManager {
		return nil, CanvasAccessView{}, errors.New("团队画布权限配置无效")
	}
	canManage := level == model.CanvasAccessManager
	canEdit := subscriptionActive && (level == model.CanvasAccessEditor || canManage)
	return canvas, CanvasAccessView{
		Level: level, TeamID: canvas.TeamID, TeamName: team.Name, TeamRole: member.Role,
		CanEdit: canEdit, CanManage: canManage, TeamSubscriptionActive: subscriptionActive,
	}, nil
}

func validCollaboratorAccess(value model.CanvasAccessLevel) bool {
	return value == model.CanvasAccessViewer || value == model.CanvasAccessEditor
}

func canvasAuditMetadata(canvasID string, title string) string {
	payload, _ := json.Marshal(struct {
		CanvasID string `json:"canvasId"`
		Title    string `json:"title"`
	}{CanvasID: canvasID, Title: title})
	return string(payload)
}
