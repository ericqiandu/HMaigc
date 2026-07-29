package model

import "time"

type CanvasAccessLevel string

const (
	CanvasAccessViewer  CanvasAccessLevel = "viewer"
	CanvasAccessEditor  CanvasAccessLevel = "editor"
	CanvasAccessManager CanvasAccessLevel = "manager"
)

// CanvasCollaborator 是团队画布成员的显式权限覆盖；团队所有者和管理员始终由团队角色派生为 manager。
type CanvasCollaborator struct {
	ID        string            `json:"id" gorm:"primaryKey;size:36"`
	CanvasID  string            `json:"canvasId" gorm:"uniqueIndex:idx_canvas_collaborator,priority:1;index;size:80"`
	TeamID    string            `json:"teamId" gorm:"index;size:36"`
	UserID    string            `json:"userId" gorm:"uniqueIndex:idx_canvas_collaborator,priority:2;index;size:36"`
	Access    CanvasAccessLevel `json:"access" gorm:"size:24"`
	CreatedBy string            `json:"createdBy" gorm:"index;size:36"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

// CanvasChange 是协作画布的持久化有序变更记录。Redis/WebSocket 仅负责通知，不能替代该事实记录。
type CanvasChange struct {
	ID               string    `json:"id" gorm:"primaryKey;size:36"`
	CanvasID         string    `json:"canvasId" gorm:"uniqueIndex:idx_canvas_change_revision,priority:1;uniqueIndex:idx_canvas_change_mutation,priority:1;index;size:80"`
	Revision         int64     `json:"revision" gorm:"uniqueIndex:idx_canvas_change_revision,priority:2"`
	ActorUserID      string    `json:"actorUserId" gorm:"index;size:36"`
	ClientMutationID string    `json:"clientMutationId" gorm:"uniqueIndex:idx_canvas_change_mutation,priority:2;size:80"`
	PayloadJSON      string    `json:"payloadJson" gorm:"type:text"`
	CreatedAt        time.Time `json:"createdAt" gorm:"index"`
}
