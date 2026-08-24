package repository

import (
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/agentruntime"
	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

const adminAgentRunStalledAfter = 10 * time.Minute

var ErrAdminAgentRunQueryInvalid = errors.New("admin agent run query is invalid")

type AdminAgentRunActivityClassification string

const (
	AdminAgentRunActive          AdminAgentRunActivityClassification = "active"
	AdminAgentRunAwaitingUser    AdminAgentRunActivityClassification = "awaiting_user"
	AdminAgentRunPossiblyStalled AdminAgentRunActivityClassification = "possibly_stalled"
)

type AdminAgentRunControlDisposition string

const (
	AdminAgentRunInterruptibleNow           AdminAgentRunControlDisposition = "interruptible_now"
	AdminAgentRunCancelRequestRequired      AdminAgentRunControlDisposition = "cancel_request_required"
	AdminAgentRunBlockedByUnresolvedBilling AdminAgentRunControlDisposition = "blocked_by_unresolved_billing"
	AdminAgentRunAlreadyTerminal            AdminAgentRunControlDisposition = "already_terminal"
)

type AdminAgentRunQuery struct {
	Status        agentruntime.RunStatus
	Activity      AdminAgentRunActivityClassification
	User          string
	Scope         string
	UpdatedBefore *time.Time
	Page          int
	PageSize      int
}

type AdminAgentRunRecord struct {
	RunID                  string                              `json:"runId" gorm:"column:run_id"`
	ThreadID               string                              `json:"threadId" gorm:"column:thread_id"`
	ActorUserID            string                              `json:"actorUserId" gorm:"column:actor_user_id"`
	ActorDisplayName       string                              `json:"actorDisplayName" gorm:"column:actor_display_name"`
	DomainProjectID        string                              `json:"domainProjectId" gorm:"column:domain_project_id"`
	CanvasID               string                              `json:"canvasId" gorm:"column:canvas_id"`
	Status                 agentruntime.RunStatus              `json:"status" gorm:"column:status"`
	StateVersion           int                                 `json:"stateVersion" gorm:"column:state_version"`
	StepNumber             int                                 `json:"stepNumber" gorm:"column:step_number"`
	MaxSteps               int                                 `json:"maxSteps" gorm:"column:max_steps"`
	ToolSchemaVersion      int                                 `json:"toolSchemaVersion" gorm:"column:tool_schema_version"`
	RuntimeVersion         int                                 `json:"runtimeVersion" gorm:"column:runtime_version"`
	PolicyVersion          int                                 `json:"policyVersion" gorm:"column:policy_version"`
	PendingKind            string                              `json:"pendingKind" gorm:"-"`
	PendingToolName        string                              `json:"pendingToolName" gorm:"-"`
	UpdatedAt              time.Time                           `json:"updatedAt" gorm:"column:updated_at"`
	InactiveSeconds        int64                               `json:"inactiveSeconds" gorm:"-"`
	ActivityClassification AdminAgentRunActivityClassification `json:"activityClassification" gorm:"-"`
	LinkedModelTaskStatus  string                              `json:"linkedModelTaskStatus" gorm:"-"`
	LinkedMediaTaskStatus  string                              `json:"linkedMediaTaskStatus" gorm:"-"`
	BillingState           string                              `json:"billingState" gorm:"-"`
	ProviderRequestState   string                              `json:"providerRequestState" gorm:"-"`
	ControlDisposition     AdminAgentRunControlDisposition     `json:"controlDisposition" gorm:"-"`
	ControlBlockedReason   string                              `json:"controlBlockedReason" gorm:"-"`
	ConfirmationPhrase     string                              `json:"confirmationPhrase,omitempty" gorm:"-"`
}

type AdminAgentRunPage struct {
	Items    []AdminAgentRunRecord `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"pageSize"`
}

func (r *Repository) AdminAgentRuns(query AdminAgentRunQuery, now time.Time) (AdminAgentRunPage, error) {
	normalized, err := normalizeAdminAgentRunQuery(query)
	if err != nil {
		return AdminAgentRunPage{}, err
	}
	if now.IsZero() {
		return AdminAgentRunPage{}, ErrAdminAgentRunQueryInvalid
	}
	db := r.adminAgentRunBaseQuery(false)
	db = applyAdminAgentRunFilters(db, normalized, now)
	var total int64
	if err := db.Distinct("runs.id").Count(&total).Error; err != nil {
		return AdminAgentRunPage{}, err
	}
	items := make([]AdminAgentRunRecord, 0, normalized.PageSize)
	offset := (normalized.Page - 1) * normalized.PageSize
	if err := db.Select(adminAgentRunSelect).
		Order("runs.updated_at ASC, runs.id ASC").
		Offset(offset).
		Limit(normalized.PageSize).
		Scan(&items).Error; err != nil {
		return AdminAgentRunPage{}, err
	}
	for index := range items {
		finalizeAdminAgentRunRecord(&items[index], now)
	}
	if err := r.hydrateAdminAgentRunFacts(items); err != nil {
		return AdminAgentRunPage{}, err
	}
	return AdminAgentRunPage{Items: items, Total: total, Page: normalized.Page, PageSize: normalized.PageSize}, nil
}

func (r *Repository) AdminAgentRun(runID string, now time.Time) (*AdminAgentRunRecord, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" || now.IsZero() {
		return nil, ErrAdminAgentRunQueryInvalid
	}
	var record AdminAgentRunRecord
	result := r.adminAgentRunBaseQuery(true).
		Select(adminAgentRunSelect).
		Where("runs.id = ?", runID).
		Limit(1).
		Scan(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	finalizeAdminAgentRunRecord(&record, now)
	records := []AdminAgentRunRecord{record}
	if err := r.hydrateAdminAgentRunFacts(records); err != nil {
		return nil, err
	}
	record = records[0]
	record.ConfirmationPhrase = adminAgentRunConfirmationPhrase(record.RunID)
	return &record, nil
}

type adminAgentRunToolFact struct {
	RunID    string                      `gorm:"column:run_id"`
	ToolName string                      `gorm:"column:tool_name"`
	Status   agentruntime.ToolCallStatus `gorm:"column:status"`
}

type adminAgentRunTaskFact struct {
	RunID             string           `gorm:"column:run_id"`
	Status            model.TaskStatus `gorm:"column:status"`
	ProviderRequestID string           `gorm:"column:provider_request_id"`
}

type adminAgentRunDirectBillingFact struct {
	IdempotencyKey string              `gorm:"column:idempotency_key"`
	Status         model.BillingStatus `gorm:"column:status"`
}

type adminAgentRunBillingFact struct {
	RunID  string              `gorm:"column:run_id"`
	Status model.BillingStatus `gorm:"column:status"`
}

func (r *Repository) hydrateAdminAgentRunFacts(records []AdminAgentRunRecord) error {
	if len(records) == 0 {
		return nil
	}
	runIDs := make([]string, 0, len(records))
	recordByRunID := make(map[string]*AdminAgentRunRecord, len(records))
	for index := range records {
		record := &records[index]
		record.LinkedModelTaskStatus = "none"
		record.LinkedMediaTaskStatus = "none"
		record.BillingState = "none"
		record.ProviderRequestState = "none"
		runIDs = append(runIDs, record.RunID)
		recordByRunID[record.RunID] = record
	}

	if err := r.hydrateAdminAgentRunToolFacts(runIDs, recordByRunID); err != nil {
		return err
	}
	if err := r.hydrateAdminAgentRunModelTaskFacts(runIDs, recordByRunID); err != nil {
		return err
	}
	if err := r.hydrateAdminAgentRunMediaTaskFacts(runIDs, recordByRunID); err != nil {
		return err
	}
	if err := r.hydrateAdminAgentRunBillingFacts(runIDs, recordByRunID); err != nil {
		return err
	}
	for index := range records {
		finalizeAdminAgentRunControlFacts(&records[index])
	}
	return nil
}

func (r *Repository) hydrateAdminAgentRunToolFacts(runIDs []string, records map[string]*AdminAgentRunRecord) error {
	var facts []adminAgentRunToolFact
	if err := r.db.Model(&model.AgentToolCall{}).
		Select("run_id, tool_name, status").
		Where("run_id IN ? AND status IN ?", runIDs, []agentruntime.ToolCallStatus{
			agentruntime.ToolCallPending,
			agentruntime.ToolCallWaitingApproval,
			agentruntime.ToolCallRunning,
		}).
		Order("updated_at DESC, id DESC").
		Scan(&facts).Error; err != nil {
		return err
	}
	for _, fact := range facts {
		if record := records[fact.RunID]; record != nil && record.PendingToolName == "" {
			record.PendingToolName = fact.ToolName
		}
	}
	return nil
}

func (r *Repository) hydrateAdminAgentRunModelTaskFacts(runIDs []string, records map[string]*AdminAgentRunRecord) error {
	operations := make([]string, 0, len(runIDs))
	for _, runID := range runIDs {
		operations = append(operations, "agent_model:"+runID)
	}
	var facts []adminAgentRunTaskFact
	if err := r.db.Model(&model.Task{}).
		Select("SUBSTR(operation, 13) AS run_id, status, provider_request_id").
		Where("operation IN ?", operations).
		Order("updated_at DESC, id DESC").
		Scan(&facts).Error; err != nil {
		return err
	}
	for _, fact := range facts {
		record := records[fact.RunID]
		if record == nil {
			continue
		}
		record.LinkedModelTaskStatus = preferredAdminAgentRunTaskStatus(record.LinkedModelTaskStatus, fact.Status)
		if strings.TrimSpace(fact.ProviderRequestID) != "" {
			record.ProviderRequestState = "submitted"
		} else if record.ProviderRequestState == "none" {
			record.ProviderRequestState = "not_submitted"
		}
	}
	return nil
}

func (r *Repository) hydrateAdminAgentRunMediaTaskFacts(runIDs []string, records map[string]*AdminAgentRunRecord) error {
	var facts []adminAgentRunTaskFact
	if err := r.db.Table("tasks AS tasks").
		Select("plans.created_by_run_id AS run_id, tasks.status AS status, tasks.provider_request_id AS provider_request_id").
		Joins("JOIN agent_production_artifacts AS artifacts ON artifacts.task_id = tasks.id").
		Joins("JOIN agent_production_plan_versions AS plans ON plans.id = artifacts.plan_version_id").
		Where("plans.created_by_run_id IN ?", runIDs).
		Order("tasks.updated_at DESC, tasks.id DESC").
		Scan(&facts).Error; err != nil {
		return err
	}
	for _, fact := range facts {
		record := records[fact.RunID]
		if record == nil {
			continue
		}
		record.LinkedMediaTaskStatus = preferredAdminAgentRunTaskStatus(record.LinkedMediaTaskStatus, fact.Status)
		if strings.TrimSpace(fact.ProviderRequestID) != "" {
			record.ProviderRequestState = "submitted"
		} else if record.ProviderRequestState == "none" {
			record.ProviderRequestState = "not_submitted"
		}
	}
	return nil
}

func (r *Repository) hydrateAdminAgentRunBillingFacts(runIDs []string, records map[string]*AdminAgentRunRecord) error {
	directQuery := r.db.Model(&model.BillingOrder{}).Select("idempotency_key, status")
	for index, runID := range runIDs {
		condition := "idempotency_key LIKE ? OR idempotency_key LIKE ?"
		directPattern := "agent-runtime:" + runID + ":%"
		proxyPattern := "proxy-token:agent-runtime:" + runID + ":%"
		if index == 0 {
			directQuery = directQuery.Where(condition, directPattern, proxyPattern)
		} else {
			directQuery = directQuery.Or(condition, directPattern, proxyPattern)
		}
	}
	var directFacts []adminAgentRunDirectBillingFact
	if err := directQuery.Order("updated_at DESC, id DESC").Scan(&directFacts).Error; err != nil {
		return err
	}
	for _, fact := range directFacts {
		for _, runID := range runIDs {
			if adminAgentRunBillingKeyMatches(fact.IdempotencyKey, runID) {
				records[runID].BillingState = preferredAdminAgentRunBillingStatus(records[runID].BillingState, fact.Status)
				break
			}
		}
	}

	var artifactFacts []adminAgentRunBillingFact
	if err := r.db.Table("billing_orders AS billing").
		Select("plans.created_by_run_id AS run_id, billing.status AS status").
		Joins("JOIN agent_production_artifacts AS artifacts ON artifacts.billing_order_id = billing.id").
		Joins("JOIN agent_production_plan_versions AS plans ON plans.id = artifacts.plan_version_id").
		Where("plans.created_by_run_id IN ?", runIDs).
		Order("billing.updated_at DESC, billing.id DESC").
		Scan(&artifactFacts).Error; err != nil {
		return err
	}
	for _, fact := range artifactFacts {
		if record := records[fact.RunID]; record != nil {
			record.BillingState = preferredAdminAgentRunBillingStatus(record.BillingState, fact.Status)
		}
	}
	return nil
}

func preferredAdminAgentRunTaskStatus(current string, candidate model.TaskStatus) string {
	if current == "none" || adminAgentRunTaskStatusPriority(candidate) > adminAgentRunTaskStatusPriority(model.TaskStatus(current)) {
		return string(candidate)
	}
	return current
}

func adminAgentRunTaskStatusPriority(status model.TaskStatus) int {
	switch status {
	case model.TaskStatusRunning:
		return 5
	case model.TaskStatusQueued:
		return 4
	case model.TaskStatusFailed:
		return 3
	case model.TaskStatusCancelled:
		return 2
	case model.TaskStatusSucceeded:
		return 1
	default:
		return 0
	}
}

func preferredAdminAgentRunBillingStatus(current string, candidate model.BillingStatus) string {
	if current == "none" || adminAgentRunBillingStatusPriority(candidate) > adminAgentRunBillingStatusPriority(model.BillingStatus(current)) {
		return string(candidate)
	}
	return current
}

func adminAgentRunBillingStatusPriority(status model.BillingStatus) int {
	switch status {
	case model.BillingStatusUncertain:
		return 5
	case model.BillingStatusRunning:
		return 4
	case model.BillingStatusReserved:
		return 3
	case model.BillingStatusSettled:
		return 2
	case model.BillingStatusRefunded:
		return 1
	default:
		return 0
	}
}

func adminAgentRunBillingKeyMatches(key string, runID string) bool {
	return strings.HasPrefix(key, "agent-runtime:"+runID+":") || strings.HasPrefix(key, "proxy-token:agent-runtime:"+runID+":")
}

func finalizeAdminAgentRunControlFacts(record *AdminAgentRunRecord) {
	if !adminAgentRunNonTerminalStatus(record.Status) {
		record.ControlDisposition = AdminAgentRunAlreadyTerminal
		return
	}
	if record.BillingState == string(model.BillingStatusReserved) || record.BillingState == string(model.BillingStatusRunning) || record.BillingState == string(model.BillingStatusUncertain) {
		record.ControlDisposition = AdminAgentRunBlockedByUnresolvedBilling
		record.ControlBlockedReason = "billing_unresolved"
		return
	}
	if adminAgentRunTaskStatusActive(record.LinkedModelTaskStatus) || adminAgentRunTaskStatusActive(record.LinkedMediaTaskStatus) {
		record.ControlDisposition = AdminAgentRunCancelRequestRequired
		return
	}
	record.ControlDisposition = AdminAgentRunInterruptibleNow
}

func adminAgentRunTaskStatusActive(status string) bool {
	return status == string(model.TaskStatusQueued) || status == string(model.TaskStatusRunning)
}

const adminAgentRunSelect = `
	runs.id AS run_id,
	runs.thread_id AS thread_id,
	runs.actor_user_id AS actor_user_id,
	COALESCE(NULLIF(users.display_name, ''), NULLIF(users.username, ''), runs.actor_user_id) AS actor_display_name,
	threads.domain_project_id AS domain_project_id,
	threads.canvas_id AS canvas_id,
	runs.status AS status,
	runs.state_version AS state_version,
	runs.step_number AS step_number,
	runs.max_steps AS max_steps,
	runs.tool_schema_version AS tool_schema_version,
	runs.runtime_version AS runtime_version,
	runs.policy_version AS policy_version,
	runs.updated_at AS updated_at`

func (r *Repository) adminAgentRunBaseQuery(includeTerminal bool) *gorm.DB {
	db := r.db.Table("agent_runs AS runs").
		Joins("JOIN agent_threads AS threads ON threads.id = runs.thread_id").
		Joins("LEFT JOIN users ON users.id = runs.actor_user_id")
	if includeTerminal {
		return db
	}
	return db.Where("runs.status IN ?", adminAgentRunNonTerminalStatuses())
}

func applyAdminAgentRunFilters(db *gorm.DB, query AdminAgentRunQuery, now time.Time) *gorm.DB {
	if query.Status != "" {
		db = db.Where("runs.status = ?", query.Status)
	}
	switch query.Activity {
	case AdminAgentRunAwaitingUser:
		db = db.Where("runs.status IN ?", []agentruntime.RunStatus{agentruntime.RunWaitingInput, agentruntime.RunWaitingApproval})
	case AdminAgentRunPossiblyStalled:
		db = db.Where(
			"runs.status IN ? AND runs.updated_at <= ?",
			[]agentruntime.RunStatus{agentruntime.RunQueued, agentruntime.RunRunning, agentruntime.RunWaitingTool},
			now.Add(-adminAgentRunStalledAfter),
		)
	case AdminAgentRunActive:
		db = db.Where(
			"runs.status NOT IN ? AND NOT (runs.status IN ? AND runs.updated_at <= ?)",
			[]agentruntime.RunStatus{agentruntime.RunWaitingInput, agentruntime.RunWaitingApproval},
			[]agentruntime.RunStatus{agentruntime.RunQueued, agentruntime.RunRunning, agentruntime.RunWaitingTool},
			now.Add(-adminAgentRunStalledAfter),
		)
	}
	if query.User != "" {
		like := "%" + strings.ToLower(query.User) + "%"
		db = db.Where(
			"LOWER(runs.actor_user_id) LIKE ? OR LOWER(COALESCE(users.email, '')) LIKE ? OR LOWER(COALESCE(users.display_name, '')) LIKE ? OR LOWER(COALESCE(users.username, '')) LIKE ?",
			like, like, like, like,
		)
	}
	if query.Scope != "" {
		like := "%" + strings.ToLower(query.Scope) + "%"
		db = db.Where(
			"LOWER(runs.id) LIKE ? OR LOWER(threads.domain_project_id) LIKE ? OR LOWER(threads.canvas_id) LIKE ?",
			like, like, like,
		)
	}
	if query.UpdatedBefore != nil {
		db = db.Where("runs.updated_at <= ?", query.UpdatedBefore.UTC())
	}
	return db
}

func normalizeAdminAgentRunQuery(query AdminAgentRunQuery) (AdminAgentRunQuery, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	if query.PageSize != 20 && query.PageSize != 50 && query.PageSize != 100 {
		return AdminAgentRunQuery{}, ErrAdminAgentRunQueryInvalid
	}
	if query.Status != "" && !adminAgentRunNonTerminalStatus(query.Status) {
		return AdminAgentRunQuery{}, ErrAdminAgentRunQueryInvalid
	}
	if query.Activity != "" && query.Activity != AdminAgentRunActive && query.Activity != AdminAgentRunAwaitingUser && query.Activity != AdminAgentRunPossiblyStalled {
		return AdminAgentRunQuery{}, ErrAdminAgentRunQueryInvalid
	}
	query.User = strings.TrimSpace(query.User)
	query.Scope = strings.TrimSpace(query.Scope)
	return query, nil
}

func finalizeAdminAgentRunRecord(record *AdminAgentRunRecord, now time.Time) {
	inactive := now.Sub(record.UpdatedAt.UTC())
	if inactive < 0 {
		inactive = 0
	}
	record.InactiveSeconds = int64(inactive / time.Second)
	record.ActivityClassification = classifyAdminAgentRunActivity(record.Status, record.UpdatedAt, now)
	switch record.Status {
	case agentruntime.RunWaitingInput:
		record.PendingKind = "clarification"
	case agentruntime.RunWaitingApproval:
		record.PendingKind = "approval"
	case agentruntime.RunWaitingTool:
		record.PendingKind = "tool"
	}
	if adminAgentRunNonTerminalStatus(record.Status) {
		record.ControlDisposition = AdminAgentRunInterruptibleNow
	} else {
		record.ControlDisposition = AdminAgentRunAlreadyTerminal
	}
}

func classifyAdminAgentRunActivity(status agentruntime.RunStatus, updatedAt time.Time, now time.Time) AdminAgentRunActivityClassification {
	if status == agentruntime.RunWaitingInput || status == agentruntime.RunWaitingApproval {
		return AdminAgentRunAwaitingUser
	}
	if (status == agentruntime.RunQueued || status == agentruntime.RunRunning || status == agentruntime.RunWaitingTool) && !updatedAt.After(now.Add(-adminAgentRunStalledAfter)) {
		return AdminAgentRunPossiblyStalled
	}
	return AdminAgentRunActive
}

func adminAgentRunNonTerminalStatus(status agentruntime.RunStatus) bool {
	for _, candidate := range adminAgentRunNonTerminalStatuses() {
		if status == candidate {
			return true
		}
	}
	return false
}

func adminAgentRunNonTerminalStatuses() []agentruntime.RunStatus {
	return []agentruntime.RunStatus{
		agentruntime.RunQueued,
		agentruntime.RunRunning,
		agentruntime.RunWaitingInput,
		agentruntime.RunWaitingApproval,
		agentruntime.RunWaitingTool,
	}
}

func adminAgentRunConfirmationPhrase(runID string) string {
	prefix := strings.TrimSpace(runID)
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return "STOP " + prefix
}
