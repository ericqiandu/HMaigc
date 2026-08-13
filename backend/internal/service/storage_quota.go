package service

import (
	"fmt"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func structuredBytes(usage repository.UserStorageUsage) int64 {
	return usage.AssetBytes + usage.CanvasBytes + usage.SessionBytes
}

func validateStructuredStorageQuotaWithPolicy(usage repository.UserStorageUsage, kind string, creating bool, deltaBytes int64, policy RuntimeResourcePolicy) error {
	if structuredBytes(usage)+deltaBytes > megabytes(policy.StructuredDataMB) {
		return BadAuthRequest(fmt.Sprintf("账号画布、素材和会话数据已达到 %dMB 上限，请先删除不需要的内容", policy.StructuredDataMB))
	}
	if !creating {
		return nil
	}
	switch kind {
	case "asset":
		if usage.AssetCount >= policy.AssetCount {
			return BadAuthRequest(fmt.Sprintf("账号素材数量已达到 %d 个上限", policy.AssetCount))
		}
	case "canvas":
		if usage.CanvasCount >= policy.CanvasCount {
			return BadAuthRequest(fmt.Sprintf("账号画布数量已达到 %d 个上限", policy.CanvasCount))
		}
	case "session":
		if usage.SessionCount >= policy.SessionCount {
			return BadAuthRequest(fmt.Sprintf("账号 Agent 会话数量已达到 %d 个上限", policy.SessionCount))
		}
	}
	return nil
}

func validateTaskStorageQuotaWithPolicy(usage repository.UserStorageUsage, incomingBytes int64, policy RuntimeResourcePolicy) error {
	if usage.TaskCount >= policy.TaskCount {
		return BadAuthRequest(fmt.Sprintf("账号任务历史已达到 %d 条上限，请联系管理员归档", policy.TaskCount))
	}
	return validateTaskDataGrowthQuotaWithPolicy(usage, incomingBytes, policy)
}

func validateTaskDataGrowthQuotaWithPolicy(usage repository.UserStorageUsage, incomingBytes int64, policy RuntimeResourcePolicy) error {
	if usage.TaskBytes+incomingBytes > gigabytes(policy.TaskDataGB) {
		return BadAuthRequest(fmt.Sprintf("账号任务历史数据已达到 %dGB 上限，请联系管理员归档", policy.TaskDataGB))
	}
	return nil
}

func validateStructuredReplacementQuotaWithPolicy(usage repository.UserStorageUsage, kind string, count int, bytes int64, policy RuntimeResourcePolicy) error {
	deltaBytes := bytes
	switch kind {
	case "asset":
		if int64(count) > policy.AssetCount {
			return BadAuthRequest(fmt.Sprintf("账号素材数量不能超过 %d 个", policy.AssetCount))
		}
		deltaBytes -= usage.AssetBytes
	case "canvas":
		if int64(count) > policy.CanvasCount {
			return BadAuthRequest(fmt.Sprintf("账号画布数量不能超过 %d 个", policy.CanvasCount))
		}
		deltaBytes -= usage.CanvasBytes
	}
	return validateStructuredStorageQuotaWithPolicy(usage, kind, false, deltaBytes, policy)
}

func (s *Service) createTaskWithinStorageQuota(task *model.Task, billingOrder *model.BillingOrder, runtimePolicy RuntimePolicySetting, activeTaskPolicy repository.ActiveTaskPolicy) error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	usage, err := s.repo.UserStorageUsage(task.UserID)
	if err != nil {
		return err
	}
	incomingBytes := int64(len([]byte(task.Prompt)) + len([]byte(task.InputJSON)) + len([]byte(task.Error)))
	if err := validateTaskStorageQuotaWithPolicy(usage, incomingBytes, runtimePolicy.Resource); err != nil {
		return err
	}
	if billingOrder != nil {
		return s.repo.CreateTaskWithCreditReservation(task, billingOrder, activeTaskPolicy)
	}
	return s.repo.CreateTaskWithActiveLimit(task, activeTaskPolicy)
}

// saveTaskCompletion 在供应商已产出后无条件提交结果事实。
// 存储配额只允许在任务创建/上传前拦截，不能在已经产生供应商成本后丢弃资产。
func (s *Service) saveTaskCompletion(task *model.Task, resultJSON []byte, opsJSON []byte, hasCanvasOps bool) error {
	publicInputJSON := publicTaskInputJSON(task.InputJSON)

	var session *model.Session
	var message *model.Message
	results := make([]model.Result, 0, 2)
	if task.SessionID != "" {
		var err error
		session, err = s.repo.SessionForUser(task.UserID, task.SessionID)
		if err != nil {
			return err
		}
		if hasCanvasOps {
			session.CanvasOpsJSON = string(opsJSON)
		}
		session.Status = model.SessionStatusCompleted
		message = &model.Message{
			ID: newID(), UserID: task.UserID, SessionID: task.SessionID, Role: "assistant",
			Content: "已生成影视级工作流分镜和画布回写操作。", Payload: string(resultJSON),
		}
		results = append(results, model.Result{ID: newID(), UserID: task.UserID, TaskID: task.ID, SessionID: task.SessionID, Kind: "generation_result", Payload: string(resultJSON)})
	}
	if hasCanvasOps {
		results = append(results, model.Result{ID: newID(), UserID: task.UserID, TaskID: task.ID, SessionID: task.SessionID, Kind: "canvas_ops", Payload: string(opsJSON)})
	}
	completed := *task
	completed.Status = model.TaskStatusSucceeded
	completed.Stage = "任务完成"
	completed.Progress = 100
	completed.ResultJSON = string(resultJSON)
	completed.InputJSON = publicInputJSON
	completed.CompletedAt = ptr(time.Now())
	if err := s.repo.SaveTaskCompletion(&completed, session, message, results); err != nil {
		return err
	}
	*task = completed
	return nil
}

func (s *Service) saveCancelledTaskResult(task *model.Task, resultJSON []byte, billingError string) error {
	publicInputJSON := publicTaskInputJSON(task.InputJSON)
	result := model.Result{
		ID: newID(), UserID: task.UserID, TaskID: task.ID, SessionID: task.SessionID,
		Kind: "generation_result", Payload: string(resultJSON),
	}
	stored := *task
	stored.Stage = "任务已取消（已保留生成结果）"
	stored.Progress = 100
	stored.ResultJSON = string(resultJSON)
	stored.InputJSON = publicInputJSON
	if err := s.repo.SaveCancelledTaskResult(&stored, result, billingError); err != nil {
		return err
	}
	*task = stored
	return nil
}
