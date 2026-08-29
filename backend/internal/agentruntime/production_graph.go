package agentruntime

import (
	"errors"
	"fmt"
	"strings"
)

const maxProductionGraphStages = 64

var (
	ErrProductionGraphInvalid           = errors.New("production graph is invalid")
	ErrProductionGraphCycle             = errors.New("production graph contains a cycle")
	ErrRevisionDependencyGraphInvalid   = errors.New("revision dependency graph is invalid")
	ErrRevisionDependencyGraphCycle     = errors.New("revision dependency graph contains a cycle")
	ErrProductionStageTransitionInvalid = errors.New("production stage transition is invalid")
	ErrProductionStageVersionConflict   = errors.New("production stage version conflict")
	ErrStageApprovalRevisionMismatch    = errors.New("production stage review revision mismatch")
)

type ProductionGraphDraft struct {
	GraphKey string                 `json:"graphKey"`
	Stages   []ProductionStageDraft `json:"stages"`
}

type ProductionStageDraft struct {
	StageKey           string                `json:"stageKey"`
	SpecialistKey      SpecialistKey         `json:"specialistKey"`
	DependsOnStageKeys []string              `json:"dependsOnStageKeys"`
	InputRevisions     []ArtifactRevisionRef `json:"inputRevisions"`
	ExpectedDelivery   ExpectedDelivery      `json:"expectedDelivery"`
	ReviewPolicy       ReviewPolicy          `json:"reviewPolicy"`
	CostPolicy         CostPolicy            `json:"costPolicy"`
}

type ProductionStageState struct {
	StageKey         string                `json:"stageKey"`
	Status           ProductionStageStatus `json:"status"`
	Version          int64                 `json:"version"`
	ReviewRevisionID string                `json:"reviewRevisionId,omitempty"`
}

// RevisionDependencyNode represents one immutable Artifact revision and its
// exact upstream revision edges. Revision text and stage naming are not part
// of dependency evaluation.
type RevisionDependencyNode struct {
	Revision  ArtifactRevisionRef
	DependsOn []ArtifactRevisionRef
}

func ValidateProductionGraph(draft ProductionGraphDraft) error {
	if strings.TrimSpace(draft.GraphKey) != draft.GraphKey || draft.GraphKey == "" || len(draft.GraphKey) > 120 {
		return fmt.Errorf("%w: graph key is invalid", ErrProductionGraphInvalid)
	}
	if len(draft.Stages) == 0 || len(draft.Stages) > maxProductionGraphStages {
		return fmt.Errorf("%w: stage count is invalid", ErrProductionGraphInvalid)
	}

	stageByKey := make(map[string]ProductionStageDraft, len(draft.Stages))
	for _, stage := range draft.Stages {
		if strings.TrimSpace(stage.StageKey) != stage.StageKey || stage.StageKey == "" || len(stage.StageKey) > 120 {
			return fmt.Errorf("%w: stage key is invalid", ErrProductionGraphInvalid)
		}
		if _, duplicated := stageByKey[stage.StageKey]; duplicated {
			return fmt.Errorf("%w: stage key %q is duplicated", ErrProductionGraphInvalid, stage.StageKey)
		}
		if !stage.SpecialistKey.Valid() {
			return fmt.Errorf("%w: stage %q specialist is invalid", ErrProductionGraphInvalid, stage.StageKey)
		}
		if !stage.ReviewPolicy.Valid() || !stage.CostPolicy.Valid() {
			return fmt.Errorf("%w: stage %q policies are invalid", ErrProductionGraphInvalid, stage.StageKey)
		}
		if err := stage.ExpectedDelivery.Validate(); err != nil {
			return fmt.Errorf("%w: stage %q delivery: %v", ErrProductionGraphInvalid, stage.StageKey, err)
		}
		if err := validateArtifactRevisionRefs(stage.InputRevisions); err != nil {
			return fmt.Errorf("%w: stage %q input revisions: %v", ErrProductionGraphInvalid, stage.StageKey, err)
		}
		stageByKey[stage.StageKey] = stage
	}

	indegree := make(map[string]int, len(draft.Stages))
	dependents := make(map[string][]string, len(draft.Stages))
	for _, stage := range draft.Stages {
		seenDependencies := make(map[string]struct{}, len(stage.DependsOnStageKeys))
		for _, dependency := range stage.DependsOnStageKeys {
			if strings.TrimSpace(dependency) != dependency || dependency == "" || dependency == stage.StageKey {
				return fmt.Errorf("%w: stage %q dependency is invalid", ErrProductionGraphInvalid, stage.StageKey)
			}
			if _, found := stageByKey[dependency]; !found {
				return fmt.Errorf("%w: stage %q dependency %q is missing", ErrProductionGraphInvalid, stage.StageKey, dependency)
			}
			if _, duplicated := seenDependencies[dependency]; duplicated {
				return fmt.Errorf("%w: stage %q dependency %q is duplicated", ErrProductionGraphInvalid, stage.StageKey, dependency)
			}
			seenDependencies[dependency] = struct{}{}
			indegree[stage.StageKey]++
			dependents[dependency] = append(dependents[dependency], stage.StageKey)
		}
	}

	queue := make([]string, 0, len(draft.Stages))
	for _, stage := range draft.Stages {
		if indegree[stage.StageKey] == 0 {
			queue = append(queue, stage.StageKey)
		}
	}
	visited := 0
	for len(queue) > 0 {
		stageKey := queue[0]
		queue = queue[1:]
		visited++
		for _, dependent := range dependents[stageKey] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	if visited != len(draft.Stages) {
		return ErrProductionGraphCycle
	}
	return nil
}

func TransitionProductionStage(current ProductionStageState, command StageReviewCommand) (ProductionStageState, error) {
	if strings.TrimSpace(current.StageKey) != current.StageKey || current.StageKey == "" ||
		!current.Status.Valid() || current.Version < 1 {
		return ProductionStageState{}, ErrProductionStageTransitionInvalid
	}
	if command.StageVersion != current.Version {
		return ProductionStageState{}, ErrProductionStageVersionConflict
	}
	if current.Status != StageAwaitingReview || strings.TrimSpace(current.ReviewRevisionID) == "" {
		return ProductionStageState{}, ErrProductionStageTransitionInvalid
	}
	if strings.TrimSpace(command.RevisionID) != command.RevisionID || command.RevisionID != current.ReviewRevisionID {
		return ProductionStageState{}, ErrStageApprovalRevisionMismatch
	}
	if strings.TrimSpace(command.ClientRequestID) != command.ClientRequestID || command.ClientRequestID == "" || len(command.ClientRequestID) > 120 ||
		strings.TrimSpace(command.Comment) != command.Comment || len(command.Comment) > 4*1024 || !command.Decision.Valid() ||
		!validPublicationIntentForDecision(command.PublicationIntent, command.Decision) {
		return ProductionStageState{}, ErrProductionStageTransitionInvalid
	}

	next := current
	next.Version++
	switch command.Decision {
	case StageReviewApprove:
		next.Status = StageApproved
	case StageReviewRequestRevision:
		if command.Comment == "" {
			return ProductionStageState{}, ErrProductionStageTransitionInvalid
		}
		next.Status = StageRunning
		next.ReviewRevisionID = ""
	case StageReviewStop:
		next.Status = StageStopped
	}
	return next, nil
}

func ValidateProductionStageStatusTransition(current ProductionStageStatus, next ProductionStageStatus) error {
	if !current.Valid() || !next.Valid() || current == next {
		return ErrProductionStageTransitionInvalid
	}

	allowed := false
	switch current {
	case StagePlanned:
		allowed = next == StageRunning || next == StageStopped
	case StageRunning:
		allowed = next == StageAwaitingReview || next == StageFailed || next == StageStopped
	case StageAwaitingReview:
		allowed = next == StageApproved || next == StageRunning || next == StageStopped
	case StageApproved:
		allowed = next == StageCompleted || next == StageStale || next == StageStopped
	case StageCompleted:
		allowed = next == StageStale
	case StageFailed, StageStale:
		allowed = next == StageRunning || next == StageStopped
	case StageStopped:
		allowed = false
	}
	if !allowed {
		return ErrProductionStageTransitionInvalid
	}
	return nil
}

func StaleDependentStages(draft ProductionGraphDraft, changedStageKey string) ([]string, error) {
	if err := ValidateProductionGraph(draft); err != nil {
		return nil, err
	}
	if strings.TrimSpace(changedStageKey) != changedStageKey || changedStageKey == "" {
		return nil, fmt.Errorf("%w: changed stage key is invalid", ErrProductionGraphInvalid)
	}

	dependents := make(map[string][]string, len(draft.Stages))
	foundChangedStage := false
	for _, stage := range draft.Stages {
		if stage.StageKey == changedStageKey {
			foundChangedStage = true
		}
		for _, dependency := range stage.DependsOnStageKeys {
			dependents[dependency] = append(dependents[dependency], stage.StageKey)
		}
	}
	if !foundChangedStage {
		return nil, fmt.Errorf("%w: changed stage is missing", ErrProductionGraphInvalid)
	}

	staleSet := make(map[string]struct{})
	queue := append([]string(nil), dependents[changedStageKey]...)
	for len(queue) > 0 {
		stageKey := queue[0]
		queue = queue[1:]
		if _, visited := staleSet[stageKey]; visited {
			continue
		}
		staleSet[stageKey] = struct{}{}
		queue = append(queue, dependents[stageKey]...)
	}
	stale := make([]string, 0, len(staleSet))
	for _, stage := range draft.Stages {
		if _, found := staleSet[stage.StageKey]; found {
			stale = append(stale, stage.StageKey)
		}
	}
	return stale, nil
}

func StaleDependentRevisions(
	nodes []RevisionDependencyNode,
	changed []ArtifactRevisionRef,
) ([]ArtifactRevisionRef, error) {
	if len(nodes) == 0 || len(changed) == 0 {
		return nil, ErrRevisionDependencyGraphInvalid
	}
	nodeByRef := make(map[ArtifactRevisionRef]RevisionDependencyNode, len(nodes))
	for _, node := range nodes {
		if node.Revision.Validate() != nil || node.DependsOn == nil || validateArtifactRevisionRefs(node.DependsOn) != nil {
			return nil, ErrRevisionDependencyGraphInvalid
		}
		if _, duplicated := nodeByRef[node.Revision]; duplicated {
			return nil, ErrRevisionDependencyGraphInvalid
		}
		nodeByRef[node.Revision] = node
	}
	changedSet := make(map[ArtifactRevisionRef]struct{}, len(changed))
	for _, reference := range changed {
		if reference.Validate() != nil {
			return nil, ErrRevisionDependencyGraphInvalid
		}
		if _, duplicated := changedSet[reference]; duplicated {
			return nil, ErrRevisionDependencyGraphInvalid
		}
		if _, exists := nodeByRef[reference]; !exists {
			return nil, ErrRevisionDependencyGraphInvalid
		}
		changedSet[reference] = struct{}{}
	}
	dependents := make(map[ArtifactRevisionRef][]ArtifactRevisionRef, len(nodes))
	for _, node := range nodes {
		for _, dependency := range node.DependsOn {
			if _, exists := nodeByRef[dependency]; !exists {
				return nil, ErrRevisionDependencyGraphInvalid
			}
			dependents[dependency] = append(dependents[dependency], node.Revision)
		}
	}
	if revisionDependencyGraphContainsCycle(nodes, nodeByRef) {
		return nil, ErrRevisionDependencyGraphCycle
	}

	staleSet := make(map[ArtifactRevisionRef]struct{})
	queue := append([]ArtifactRevisionRef(nil), changed...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, dependent := range dependents[current] {
			if _, changedRoot := changedSet[dependent]; changedRoot {
				continue
			}
			if _, visited := staleSet[dependent]; visited {
				continue
			}
			staleSet[dependent] = struct{}{}
			queue = append(queue, dependent)
		}
	}
	stale := make([]ArtifactRevisionRef, 0, len(staleSet))
	for _, node := range nodes {
		if _, exists := staleSet[node.Revision]; exists {
			stale = append(stale, node.Revision)
		}
	}
	return stale, nil
}

func StaleStagesForRevisionChanges(draft ProductionGraphDraft, changed []ArtifactRevisionRef) ([]string, error) {
	if err := ValidateProductionGraph(draft); err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, ErrRevisionDependencyGraphInvalid
	}
	changedSet := make(map[ArtifactRevisionRef]struct{}, len(changed))
	for _, reference := range changed {
		if reference.Validate() != nil {
			return nil, ErrRevisionDependencyGraphInvalid
		}
		if _, duplicated := changedSet[reference]; duplicated {
			return nil, ErrRevisionDependencyGraphInvalid
		}
		changedSet[reference] = struct{}{}
	}

	staleSet := make(map[string]struct{})
	queue := make([]string, 0)
	dependents := make(map[string][]string, len(draft.Stages))
	for _, stage := range draft.Stages {
		for _, dependency := range stage.DependsOnStageKeys {
			dependents[dependency] = append(dependents[dependency], stage.StageKey)
		}
		for _, reference := range stage.InputRevisions {
			if _, changedInput := changedSet[reference]; changedInput {
				if _, found := staleSet[stage.StageKey]; !found {
					staleSet[stage.StageKey] = struct{}{}
					queue = append(queue, stage.StageKey)
				}
				break
			}
		}
	}
	for len(queue) > 0 {
		stageKey := queue[0]
		queue = queue[1:]
		for _, dependent := range dependents[stageKey] {
			if _, found := staleSet[dependent]; found {
				continue
			}
			staleSet[dependent] = struct{}{}
			queue = append(queue, dependent)
		}
	}
	stale := make([]string, 0, len(staleSet))
	for _, stage := range draft.Stages {
		if _, found := staleSet[stage.StageKey]; found {
			stale = append(stale, stage.StageKey)
		}
	}
	return stale, nil
}

func revisionDependencyGraphContainsCycle(
	nodes []RevisionDependencyNode,
	nodeByRef map[ArtifactRevisionRef]RevisionDependencyNode,
) bool {
	const (
		visiting = iota + 1
		visited
	)
	states := make(map[ArtifactRevisionRef]int, len(nodes))
	var visit func(ArtifactRevisionRef) bool
	visit = func(reference ArtifactRevisionRef) bool {
		switch states[reference] {
		case visiting:
			return true
		case visited:
			return false
		}
		states[reference] = visiting
		for _, dependency := range nodeByRef[reference].DependsOn {
			if visit(dependency) {
				return true
			}
		}
		states[reference] = visited
		return false
	}
	for _, node := range nodes {
		if visit(node.Revision) {
			return true
		}
	}
	return false
}
