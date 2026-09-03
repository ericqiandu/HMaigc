package repository

import (
	"errors"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

// ErrAgentSpecialistRunConflict is retained for read-only run-tree and
// historical publication validation. Specialist execution is no longer a
// repository capability.
var ErrAgentSpecialistRunConflict = errors.New("agent specialist run conflict")

func agentSpecialistScopeQuery(query *gorm.DB, scope agentruntime.Scope) *gorm.DB {
	return query.Model(&model.AgentSpecialistRun{}).Where(
		"agent_specialist_runs.tenant_kind = ? AND agent_specialist_runs.tenant_id = ? AND agent_specialist_runs.actor_user_id = ? AND agent_specialist_runs.domain_project_id = ? AND agent_specialist_runs.canvas_id = ? AND agent_specialist_runs.thread_id = ? AND agent_specialist_runs.run_id = ?",
		scope.TenantKind,
		scope.TenantID,
		scope.ActorUserID,
		scope.DomainProjectID,
		scope.CanvasID,
		scope.ThreadID,
		scope.RunID,
	)
}

func agentStageReviewTimelineItemID(runID string, stageID string, revisionID string) string {
	return agentFactID("timeline", runID, "stage-review", stageID, revisionID)
}

func agentArtifactReviewTimelineItemID(runID string, content agentruntime.ArtifactReviewContent) string {
	return agentFactID("timeline", runID, "artifact-review", content.StageID, content.RevisionID)
}
