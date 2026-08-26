package service

import (
	"context"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/opsprotocol"
)

type AdminStartOperationRequest struct {
	Action         opsprotocol.Action `json:"action"`
	TargetVersion  string             `json:"targetVersion"`
	IdempotencyKey string             `json:"idempotencyKey"`
	Confirmation   string             `json:"confirmation"`
}

type AdminControlOperationRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Confirmation   string `json:"confirmation"`
}

const sourceDeploymentWriteMessage = "生产发布由 GitHub Actions 源码流水线管理，后台不再执行运维写操作"

func (s *Service) AdminOperationsOverview(ctx context.Context, actor *model.User) (*opsprotocol.Overview, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	client, err := s.requireOperationsClient()
	if err != nil {
		return nil, err
	}
	result, err := client.Overview(ctx)
	if err != nil {
		return nil, mapOperationsClientError(err)
	}
	return result, nil
}

func (s *Service) AdminOperations(ctx context.Context, actor *model.User, limit int) (*opsprotocol.OperationPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	client, err := s.requireOperationsClient()
	if err != nil {
		return nil, err
	}
	result, err := client.Operations(ctx, limit)
	if err != nil {
		return nil, mapOperationsClientError(err)
	}
	return result, nil
}

func (s *Service) AdminOperation(ctx context.Context, actor *model.User, id string) (*opsprotocol.Operation, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	client, err := s.requireOperationsClient()
	if err != nil {
		return nil, err
	}
	result, err := client.Operation(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, mapOperationsClientError(err)
	}
	return result, nil
}

func (s *Service) AdminOperationLogs(ctx context.Context, actor *model.User, id string, after uint64, limit int) (*opsprotocol.OperationLogPage, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	client, err := s.requireOperationsClient()
	if err != nil {
		return nil, err
	}
	result, err := client.OperationLogs(ctx, strings.TrimSpace(id), after, limit)
	if err != nil {
		return nil, mapOperationsClientError(err)
	}
	return result, nil
}

func (s *Service) AdminOperationBackups(ctx context.Context, actor *model.User, limit int) ([]opsprotocol.Backup, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	client, err := s.requireOperationsClient()
	if err != nil {
		return nil, err
	}
	result, err := client.Backups(ctx, limit)
	if err != nil {
		return nil, mapOperationsClientError(err)
	}
	return result, nil
}

func (s *Service) StartAdminOperation(ctx context.Context, actor *model.User, input AdminStartOperationRequest) (*opsprotocol.Operation, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return nil, Forbidden(sourceDeploymentWriteMessage)
}

func (s *Service) CancelAdminOperation(ctx context.Context, actor *model.User, id string, input AdminControlOperationRequest) (*opsprotocol.Operation, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return nil, Forbidden(sourceDeploymentWriteMessage)
}

func (s *Service) RecoverAdminOperation(ctx context.Context, actor *model.User, id string, input AdminControlOperationRequest) (*opsprotocol.Operation, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return nil, Forbidden(sourceDeploymentWriteMessage)
}

func (s *Service) requireOperationsClient() (opsprotocol.Client, error) {
	if s.operationsClient == nil {
		return nil, ServiceUnavailable("独立运维控制器尚未配置或未安装")
	}
	return s.operationsClient, nil
}

func mapOperationsClientError(err error) error {
	var remote *opsprotocol.RemoteError
	if !errors.As(err, &remote) {
		return ServiceUnavailable("独立运维控制器不可用: " + err.Error())
	}
	switch remote.Status {
	case 400:
		return BadAuthRequest(remote.Message)
	case 401, 403:
		return Forbidden(remote.Message)
	case 409:
		return Conflict(remote.Message)
	case 503:
		return ServiceUnavailable(remote.Message)
	case 404:
		return NotFound(remote.Message)
	default:
		return ServiceUnavailable(remote.Message)
	}
}
