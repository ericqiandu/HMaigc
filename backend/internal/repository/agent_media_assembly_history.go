package repository

import (
	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
)

func mediaAssemblyTimelineItemStatus(content agentruntime.MediaAssemblyTimelineContent) (model.AgentTimelineItemStatus, error) {
	switch content.TaskStatus {
	case agentruntime.MediaAssemblyTaskQueued, agentruntime.MediaAssemblyTaskRunning:
		return model.AgentTimelineItemInProgress, nil
	case agentruntime.MediaAssemblyTaskSucceeded:
		return model.AgentTimelineItemCompleted, nil
	case agentruntime.MediaAssemblyTaskFailed:
		return model.AgentTimelineItemFailed, nil
	case agentruntime.MediaAssemblyTaskCancelled:
		return model.AgentTimelineItemInterrupted, nil
	default:
		return "", ErrAgentTimelineConflict
	}
}
