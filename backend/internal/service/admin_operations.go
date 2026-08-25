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
	if input.Action == opsprotocol.ActionInstall {
		return nil, Forbidden("首次安装只能通过服务器命令行引导，后台不能执行安装")
	}
	client, err := s.requireOperationsClient()
	if err != nil {
		return nil, err
	}
	displayName := adminOperationDisplayName(actor)
	result, err := client.StartOperation(ctx, opsprotocol.StartOperationRequest{
		Action: input.Action, TargetVersion: strings.TrimSpace(input.TargetVersion),
		ActorUserID: actor.ID, ActorDisplayName: displayName,
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), Confirmation: strings.TrimSpace(input.Confirmation),
	})
	if err != nil {
		return nil, mapOperationsClientError(err)
	}
	return result, nil
}

func (s *Service) CancelAdminOperation(ctx context.Context, actor *model.User, id string, input AdminControlOperationRequest) (*opsprotocol.Operation, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	client, err := s.requireOperationsClient()
	if err != nil {
		return nil, err
	}
	result, err := client.CancelOperation(ctx, strings.TrimSpace(id), opsprotocol.CancelOperationRequest{
		ActorUserID: actor.ID, ActorDisplayName: adminOperationDisplayName(actor),
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), Confirmation: strings.TrimSpace(input.Confirmation),
	})
	if err != nil {
		return nil, mapOperationsClientError(err)
	}
	return result, nil
}

func (s *Service) RecoverAdminOperation(ctx context.Context, actor *model.User, id string, input AdminControlOperationRequest) (*opsprotocol.Operation, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	client, err := s.requireOperationsClient()
	if err != nil {
		return nil, err
	}
	result, err := client.RecoverOperation(ctx, strings.TrimSpace(id), opsprotocol.RecoverOperationRequest{
		ActorUserID: actor.ID, ActorDisplayName: adminOperationDisplayName(actor),
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), Confirmation: strings.TrimSpace(input.Confirmation),
	})
	if err != nil {
		return nil, mapOperationsClientError(err)
	}
	return result, nil
}

func adminOperationDisplayName(actor *model.User) string {
	displayName := strings.TrimSpace(actor.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(actor.Username)
	}
	return displayName
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
