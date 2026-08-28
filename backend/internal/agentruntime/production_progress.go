package agentruntime

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrProductionProgressInvalid           = errors.New("production progress facts are invalid")
	ErrProductionEvidenceIncomplete        = errors.New("production evidence is incomplete")
	ErrProductionEvidenceContentDuplicated = errors.New("production evidence duplicates content truth")
)

type ProductionEvidenceLifecycle string

const (
	ProductionEvidenceCurrent ProductionEvidenceLifecycle = "current"
	ProductionEvidenceStale   ProductionEvidenceLifecycle = "stale"
)

func (status ProductionEvidenceLifecycle) Valid() bool {
	return status == ProductionEvidenceCurrent || status == ProductionEvidenceStale
}

// CharacterIdentityVersion points at exact immutable facts. Payload exists only
// as a validation guard so callers cannot accidentally create a second content
// truth beside the Character Bible Artifact revision.
type CharacterIdentityVersion struct {
	CharacterKey             string
	Version                  int64
	CharacterBibleRevisionID string
	ResourceID               string
	DependencyHash           string
	LifecycleStatus          ProductionEvidenceLifecycle
	Payload                  string
}

func ValidateCharacterIdentityVersion(version CharacterIdentityVersion) error {
	if version.Payload != "" {
		return ErrProductionEvidenceContentDuplicated
	}
	if !validProgressIdentity(version.CharacterKey, 120) || version.Version < 1 ||
		!validProgressIdentity(version.CharacterBibleRevisionID, 80) ||
		!validProgressIdentity(version.ResourceID, 80) || !validDependencyHash(version.DependencyHash) ||
		!version.LifecycleStatus.Valid() {
		return ErrProductionEvidenceIncomplete
	}
	return nil
}

// ShotBindingRevision pins one shot/character pair to exact identity,
// Artifact-revision and Resource facts. It never copies prompts or media URLs.
type ShotBindingRevision struct {
	ShotKey                string
	CharacterKey           string
	Revision               int64
	ShotArtifactRevisionID string
	IdentityVersionID      string
	ResourceID             string
	DependencyHash         string
	LifecycleStatus        ProductionEvidenceLifecycle
	Payload                string
}

func ValidateShotBindingRevision(binding ShotBindingRevision) error {
	if binding.Payload != "" {
		return ErrProductionEvidenceContentDuplicated
	}
	if !validProgressIdentity(binding.ShotKey, 120) || !validProgressIdentity(binding.CharacterKey, 120) ||
		binding.Revision < 1 || !validProgressIdentity(binding.ShotArtifactRevisionID, 80) ||
		!validProgressIdentity(binding.IdentityVersionID, 80) || !validProgressIdentity(binding.ResourceID, 80) ||
		!validDependencyHash(binding.DependencyHash) || !binding.LifecycleStatus.Valid() {
		return ErrProductionEvidenceIncomplete
	}
	return nil
}

type ProductionBlockerCode string

const (
	ProductionBlockerDependencyIncomplete  ProductionBlockerCode = "dependency_incomplete"
	ProductionBlockerTaskActive            ProductionBlockerCode = "task_active"
	ProductionBlockerTaskFailed            ProductionBlockerCode = "task_failed"
	ProductionBlockerTaskCancelled         ProductionBlockerCode = "task_cancelled"
	ProductionBlockerBillingPending        ProductionBlockerCode = "billing_pending"
	ProductionBlockerBillingUncertain      ProductionBlockerCode = "billing_uncertain"
	ProductionBlockerDeliveryIncomplete    ProductionBlockerCode = "delivery_incomplete"
	ProductionBlockerResourceNotReady      ProductionBlockerCode = "resource_not_ready"
	ProductionBlockerReviewRevisionMissing ProductionBlockerCode = "review_revision_missing"
)

type ProductionAction string

const (
	ProductionActionExecuteStage  ProductionAction = "execute_stage"
	ProductionActionSubmitReview  ProductionAction = "submit_review"
	ProductionActionReviewStage   ProductionAction = "review_stage"
	ProductionActionCompleteStage ProductionAction = "complete_stage"
)

type ProductionEvidenceRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type ProductionBlocker struct {
	Code         ProductionBlockerCode   `json:"code"`
	EvidenceRefs []ProductionEvidenceRef `json:"evidenceRefs"`
}

type ProductionEligibleAction struct {
	Action           ProductionAction    `json:"action"`
	RequiredEvidence []DeliveryCriterion `json:"requiredEvidence,omitempty"`
}

type ProductionNextActionProjection struct {
	GraphVersionID    string                     `json:"graphVersionId"`
	GraphVersion      int64                      `json:"graphVersion"`
	CurrentStageKey   string                     `json:"currentStageKey,omitempty"`
	StageStatus       ProductionStageStatus      `json:"stageStatus,omitempty"`
	ReviewRevisionID  string                     `json:"reviewRevisionId,omitempty"`
	Blockers          []ProductionBlocker        `json:"blockers"`
	EligibleActions   []ProductionEligibleAction `json:"eligibleActions"`
	ActiveTaskRefs    []ProductionEvidenceRef    `json:"activeTaskRefs"`
	StaleRevisionRefs []ProductionEvidenceRef    `json:"staleRevisionRefs"`
	ComputedAt        time.Time                  `json:"computedAt"`
}

type ProductionTaskEvidence struct {
	TaskID         string
	Status         string
	BillingOrderID string
}

type ProductionBillingEvidence struct {
	BillingOrderID string
	Status         string
}

type ProductionProgressStageFacts struct {
	StageKey           string
	Status             ProductionStageStatus
	DependsOnStageKeys []string
	ReviewRevisionID   string
	ExpectedDelivery   ExpectedDelivery
	DeliveryEvidence   DeliveryEvidence
	Tasks              []ProductionTaskEvidence
	Billings           []ProductionBillingEvidence
	StaleRevisionIDs   []string
}

type ProductionProgressFacts struct {
	GraphVersionID string
	GraphVersion   int64
	Stages         []ProductionProgressStageFacts
	ComputedAt     time.Time
}

func BuildProductionProgress(facts ProductionProgressFacts) (ProductionNextActionProjection, error) {
	projection := ProductionNextActionProjection{
		GraphVersionID:    facts.GraphVersionID,
		GraphVersion:      facts.GraphVersion,
		Blockers:          []ProductionBlocker{},
		EligibleActions:   []ProductionEligibleAction{},
		ActiveTaskRefs:    []ProductionEvidenceRef{},
		StaleRevisionRefs: []ProductionEvidenceRef{},
		ComputedAt:        facts.ComputedAt,
	}
	if !validProgressIdentity(facts.GraphVersionID, 80) || facts.GraphVersion < 1 || facts.ComputedAt.IsZero() || len(facts.Stages) == 0 {
		return ProductionNextActionProjection{}, ErrProductionProgressInvalid
	}
	stageByKey := make(map[string]ProductionProgressStageFacts, len(facts.Stages))
	completed := make(map[string]bool, len(facts.Stages))
	for _, stage := range facts.Stages {
		if err := validateProgressStage(stage, stageByKey); err != nil {
			return ProductionNextActionProjection{}, err
		}
		stageByKey[stage.StageKey] = stage
		completed[stage.StageKey] = productionStageEvidenceCompleted(stage)
	}
	for _, stage := range facts.Stages {
		if stage.Status == StageStopped || completed[stage.StageKey] {
			continue
		}
		projection.CurrentStageKey = stage.StageKey
		projection.StageStatus = stage.Status
		projection.ReviewRevisionID = stage.ReviewRevisionID
		projection.Blockers, projection.ActiveTaskRefs, projection.StaleRevisionRefs = progressBlockers(stage, completed)
		projection.EligibleActions = progressEligibleActions(stage, projection.Blockers)
		return projection, nil
	}
	return projection, nil
}

func productionStageEvidenceCompleted(stage ProductionProgressStageFacts) bool {
	if stage.Status != StageCompleted || deliveryVerificationForProgress(stage).Status != VerificationSatisfied {
		return false
	}
	for _, task := range stage.Tasks {
		if task.Status != "succeeded" {
			return false
		}
	}
	for _, billing := range stage.Billings {
		if billing.Status != "settled" {
			return false
		}
	}
	return true
}

func validateProgressStage(stage ProductionProgressStageFacts, preceding map[string]ProductionProgressStageFacts) error {
	if !validProgressIdentity(stage.StageKey, 120) || !stage.Status.Valid() ||
		strings.TrimSpace(stage.ReviewRevisionID) != stage.ReviewRevisionID || len(stage.ReviewRevisionID) > 80 ||
		stage.ExpectedDelivery.Validate() != nil {
		return ErrProductionProgressInvalid
	}
	if _, duplicated := preceding[stage.StageKey]; duplicated {
		return ErrProductionProgressInvalid
	}
	seenDependencies := make(map[string]struct{}, len(stage.DependsOnStageKeys))
	for _, dependency := range stage.DependsOnStageKeys {
		if !validProgressIdentity(dependency, 120) || dependency == stage.StageKey {
			return ErrProductionProgressInvalid
		}
		if _, duplicated := seenDependencies[dependency]; duplicated {
			return ErrProductionProgressInvalid
		}
		if _, found := preceding[dependency]; !found {
			return ErrProductionProgressInvalid
		}
		seenDependencies[dependency] = struct{}{}
	}
	for _, task := range stage.Tasks {
		if !validProgressIdentity(task.TaskID, 80) || !validTaskEvidenceStatus(task.Status) ||
			strings.TrimSpace(task.BillingOrderID) != task.BillingOrderID || len(task.BillingOrderID) > 80 {
			return ErrProductionProgressInvalid
		}
	}
	for _, billing := range stage.Billings {
		if !validProgressIdentity(billing.BillingOrderID, 80) || !validBillingEvidenceStatus(billing.Status) {
			return ErrProductionProgressInvalid
		}
	}
	for _, revisionID := range stage.StaleRevisionIDs {
		if !validProgressIdentity(revisionID, 80) {
			return ErrProductionProgressInvalid
		}
	}
	return nil
}

func progressBlockers(stage ProductionProgressStageFacts, completed map[string]bool) ([]ProductionBlocker, []ProductionEvidenceRef, []ProductionEvidenceRef) {
	blockers := make([]ProductionBlocker, 0)
	activeTasks := make([]ProductionEvidenceRef, 0)
	staleRevisions := make([]ProductionEvidenceRef, 0, len(stage.StaleRevisionIDs))
	for _, dependency := range stage.DependsOnStageKeys {
		if !completed[dependency] {
			blockers = appendProductionBlocker(blockers, ProductionBlockerDependencyIncomplete, ProductionEvidenceRef{Kind: "stage", ID: dependency})
		}
	}
	for _, task := range stage.Tasks {
		ref := ProductionEvidenceRef{Kind: "task", ID: task.TaskID}
		switch task.Status {
		case "queued", "running":
			activeTasks = append(activeTasks, ref)
			blockers = appendProductionBlocker(blockers, ProductionBlockerTaskActive, ref)
		case "failed":
			if stage.Status != StageFailed && stage.Status != StageStale {
				blockers = appendProductionBlocker(blockers, ProductionBlockerTaskFailed, ref)
			}
		case "cancelled":
			if stage.Status != StageFailed && stage.Status != StageStale {
				blockers = appendProductionBlocker(blockers, ProductionBlockerTaskCancelled, ref)
			}
		}
	}
	for _, billing := range stage.Billings {
		ref := ProductionEvidenceRef{Kind: "billing_order", ID: billing.BillingOrderID}
		switch billing.Status {
		case "reserved", "running":
			blockers = appendProductionBlocker(blockers, ProductionBlockerBillingPending, ref)
		case "uncertain":
			blockers = appendProductionBlocker(blockers, ProductionBlockerBillingUncertain, ref)
		}
	}
	for _, revisionID := range stage.StaleRevisionIDs {
		staleRevisions = append(staleRevisions, ProductionEvidenceRef{Kind: "artifact_revision", ID: revisionID})
	}
	if requiresDeliveryEvidence(stage.Status) {
		verification := deliveryVerificationForProgress(stage)
		for _, criterion := range verification.MissingCriteria {
			code := ProductionBlockerDeliveryIncomplete
			refs := []ProductionEvidenceRef{{Kind: "stage", ID: stage.StageKey}}
			if criterion.Fact == DeliveryFactResource {
				code = ProductionBlockerResourceNotReady
				refs = resourceEvidenceRefs(stage.StageKey, stage.DeliveryEvidence, criterion.Artifact)
			}
			for _, ref := range refs {
				blockers = appendProductionBlocker(blockers, code, ref)
			}
		}
		if verification.Status == VerificationFailed {
			blockers = appendProductionBlocker(blockers, ProductionBlockerDeliveryIncomplete, ProductionEvidenceRef{Kind: "stage", ID: stage.StageKey})
		}
	}
	if stage.Status == StageAwaitingReview && stage.ReviewRevisionID == "" {
		blockers = appendProductionBlocker(blockers, ProductionBlockerReviewRevisionMissing, ProductionEvidenceRef{Kind: "stage", ID: stage.StageKey})
	}
	sortEvidenceRefs(activeTasks)
	sortEvidenceRefs(staleRevisions)
	return blockers, activeTasks, staleRevisions
}

func deliveryVerificationForProgress(stage ProductionProgressStageFacts) DeliveryVerification {
	evidence := stage.DeliveryEvidence
	if stage.Status == StageRunning || stage.Status == StageAwaitingReview {
		evidence.Artifacts = append([]DeliveryArtifact(nil), evidence.Artifacts...)
		for index := range evidence.Artifacts {
			if evidence.Artifacts[index].ArtifactID != "" && evidence.Artifacts[index].RevisionID != "" {
				evidence.Artifacts[index].Approved = true
			}
		}
	}
	return VerifyDelivery(stage.ExpectedDelivery, evidence)
}

func progressEligibleActions(stage ProductionProgressStageFacts, blockers []ProductionBlocker) []ProductionEligibleAction {
	if len(blockers) != 0 {
		return []ProductionEligibleAction{}
	}
	var action ProductionAction
	switch stage.Status {
	case StagePlanned, StageFailed, StageStale:
		action = ProductionActionExecuteStage
	case StageRunning:
		action = ProductionActionSubmitReview
	case StageAwaitingReview:
		action = ProductionActionReviewStage
	case StageApproved:
		action = ProductionActionCompleteStage
	default:
		return []ProductionEligibleAction{}
	}
	return []ProductionEligibleAction{{Action: action, RequiredEvidence: append([]DeliveryCriterion(nil), stage.ExpectedDelivery.CompletionCriteria...)}}
}

func requiresDeliveryEvidence(status ProductionStageStatus) bool {
	return status == StageRunning || status == StageAwaitingReview || status == StageApproved || status == StageCompleted
}

func resourceEvidenceRefs(stageKey string, evidence DeliveryEvidence, kind ArtifactKind) []ProductionEvidenceRef {
	refs := make([]ProductionEvidenceRef, 0)
	for _, artifact := range evidence.Artifacts {
		if artifact.Kind != kind {
			continue
		}
		if artifact.ResourceID != "" {
			refs = append(refs, ProductionEvidenceRef{Kind: "resource", ID: artifact.ResourceID})
		} else if artifact.RevisionID != "" {
			refs = append(refs, ProductionEvidenceRef{Kind: "artifact_revision", ID: artifact.RevisionID})
		}
	}
	if len(refs) == 0 {
		refs = append(refs, ProductionEvidenceRef{Kind: "stage", ID: stageKey})
	}
	return refs
}

func appendProductionBlocker(blockers []ProductionBlocker, code ProductionBlockerCode, ref ProductionEvidenceRef) []ProductionBlocker {
	for index := range blockers {
		if blockers[index].Code != code {
			continue
		}
		for _, existing := range blockers[index].EvidenceRefs {
			if existing == ref {
				return blockers
			}
		}
		blockers[index].EvidenceRefs = append(blockers[index].EvidenceRefs, ref)
		sortEvidenceRefs(blockers[index].EvidenceRefs)
		return blockers
	}
	return append(blockers, ProductionBlocker{Code: code, EvidenceRefs: []ProductionEvidenceRef{ref}})
}

func sortEvidenceRefs(refs []ProductionEvidenceRef) {
	sort.Slice(refs, func(left, right int) bool {
		if refs[left].Kind == refs[right].Kind {
			return refs[left].ID < refs[right].ID
		}
		return refs[left].Kind < refs[right].Kind
	})
}

func validTaskEvidenceStatus(status string) bool {
	switch status {
	case "queued", "running", "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func validBillingEvidenceStatus(status string) bool {
	switch status {
	case "reserved", "running", "settled", "refunded", "uncertain":
		return true
	default:
		return false
	}
}

func validProgressIdentity(value string, maximum int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= maximum
}

func validDependencyHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
