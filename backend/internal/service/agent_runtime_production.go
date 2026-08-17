package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

var errAgentRuntimeProductionPlanInput = errors.New("agent production plan arguments are invalid")

type agentSkillLoadArguments struct {
	Dir string `json:"dir"`
}

type agentSkillLoadResult struct {
	Dir          string `json:"dir"`
	Name         string `json:"name"`
	Version      int    `json:"version"`
	Instructions string `json:"instructions"`
}

type agentProductionPlanArguments struct {
	PlanKey     string                           `json:"planKey"`
	BaseVersion int                              `json:"baseVersion"`
	Draft       agentruntime.ProductionPlanDraft `json:"draft"`
}

type agentProductionPlanResult struct {
	PlanKey          string                          `json:"planKey"`
	PlanVersion      int                             `json:"planVersion"`
	Artifacts        []agentProductionArtifactResult `json:"artifacts"`
	TargetDurationMS int                             `json:"targetDurationMs"`
}

type agentProductionArtifactResult struct {
	ArtifactID string                              `json:"artifactId"`
	Kind       model.AgentProductionArtifactKind   `json:"kind"`
	ShotKey    string                              `json:"shotKey"`
	Status     model.AgentProductionArtifactStatus `json:"status"`
}

func executeAgentSkillLoad(configuration agentruntime.RunConfiguration, raw json.RawMessage) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentSkillLoadArguments
	if err := decoder.Decode(&arguments); err != nil {
		return nil, errors.New("agent skill load arguments are invalid")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("agent skill load arguments are invalid")
	}
	arguments.Dir = strings.TrimSpace(arguments.Dir)
	for _, skill := range configuration.Skills {
		if skill.Dir != arguments.Dir {
			continue
		}
		return json.Marshal(agentSkillLoadResult{
			Dir: skill.Dir, Name: skill.Name, Version: skill.Version, Instructions: skill.Instructions,
		})
	}
	return nil, errors.New("agent skill load selection is unavailable")
}

func (s *Service) executeAgentProductionPlan(scope agentruntime.Scope, raw json.RawMessage) ([]byte, error) {
	arguments, err := decodeAgentProductionPlanArguments(raw)
	if err != nil {
		return nil, err
	}
	if arguments.PlanKey == "" {
		arguments.PlanKey = agentRuntimeProductionPlanKey(scope.RunID)
	}
	record, err := s.repo.AppendAgentProductionPlanVersion(repository.AppendAgentProductionPlanInput{
		Scope: scope, RunID: scope.RunID, PlanKey: arguments.PlanKey,
		BaseVersion: arguments.BaseVersion, Draft: arguments.Draft, Now: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	result := agentProductionPlanResult{
		PlanKey: record.Plan.PlanKey, PlanVersion: record.Plan.Version,
		Artifacts: make([]agentProductionArtifactResult, 0, len(record.Artifacts)), TargetDurationMS: record.Plan.TargetDurationMS,
	}
	for _, artifact := range record.Artifacts {
		result.Artifacts = append(result.Artifacts, agentProductionArtifactResult{
			ArtifactID: artifact.ID,
			Kind:       artifact.Kind,
			ShotKey:    artifact.ShotKey,
			Status:     artifact.Status,
		})
	}
	return json.Marshal(result)
}

func decodeAgentProductionPlanArguments(raw json.RawMessage) (agentProductionPlanArguments, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var arguments agentProductionPlanArguments
	if err := decoder.Decode(&arguments); err != nil {
		return agentProductionPlanArguments{}, fmt.Errorf("%w: json", errAgentRuntimeProductionPlanInput)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return agentProductionPlanArguments{}, fmt.Errorf("%w: trailing json", errAgentRuntimeProductionPlanInput)
	}
	arguments.PlanKey = strings.TrimSpace(arguments.PlanKey)
	if len(arguments.PlanKey) > 120 || arguments.BaseVersion < 0 {
		return agentProductionPlanArguments{}, fmt.Errorf("%w: version identity", errAgentRuntimeProductionPlanInput)
	}
	if (arguments.BaseVersion == 0 && arguments.PlanKey != "") || (arguments.BaseVersion > 0 && arguments.PlanKey == "") {
		return agentProductionPlanArguments{}, fmt.Errorf("%w: plan identity ownership", errAgentRuntimeProductionPlanInput)
	}
	if err := arguments.Draft.Validate(); err != nil {
		return agentProductionPlanArguments{}, fmt.Errorf("%w: %v", errAgentRuntimeProductionPlanInput, err)
	}
	return arguments, nil
}

func agentRuntimeProductionPlanKey(runID string) string {
	digest := sha256.Sum256([]byte("agent-production-plan\x00" + strings.TrimSpace(runID)))
	return fmt.Sprintf("plan_%x", digest[:16])
}
