package service

import (
	"context"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/opsprotocol"
)

func TestMapOperationsClientError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		input          error
		expectedStatus int
	}{
		{name: "bad request", input: &opsprotocol.RemoteError{Status: 400, Message: "invalid"}, expectedStatus: 400},
		{name: "forbidden", input: &opsprotocol.RemoteError{Status: 403, Message: "forbidden"}, expectedStatus: 403},
		{name: "not found", input: &opsprotocol.RemoteError{Status: 404, Message: "missing"}, expectedStatus: 404},
		{name: "conflict", input: &opsprotocol.RemoteError{Status: 409, Message: "busy"}, expectedStatus: 409},
		{name: "controller failure", input: &opsprotocol.RemoteError{Status: 500, Message: "failed"}, expectedStatus: 503},
		{name: "transport failure", input: errors.New("dial unix failed"), expectedStatus: 503},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mapped := mapOperationsClientError(test.input)
			var authError *AuthError
			if !errors.As(mapped, &authError) {
				t.Fatalf("expected AuthError, got %T", mapped)
			}
			if authError.Status != test.expectedStatus {
				t.Fatalf("expected status %d, got %d", test.expectedStatus, authError.Status)
			}
			if authError.Message == "" {
				t.Fatal("expected explicit error message")
			}
		})
	}
}

func TestAdminOperationControlRequiresAdmin(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	user := &model.User{ID: "user-1", Username: "user", Role: model.UserRoleUser, Status: model.UserStatusActive}
	input := AdminControlOperationRequest{IdempotencyKey: "cancel-service-0001", Confirmation: "STOP op-1"}

	_, err := svc.CancelAdminOperation(context.Background(), user, "op-1", input)
	var authError *AuthError
	if !errors.As(err, &authError) || authError.Status != 403 {
		t.Fatalf("expected non-admin forbidden, got %v", err)
	}
}

func TestAdminOperationWritesAreOwnedBySourceDeploymentPipeline(t *testing.T) {
	t.Parallel()
	client := &operationsClientStub{}
	svc := &Service{}
	svc.ConfigureOperationsClient(client)
	admin := &model.User{
		ID: "admin-1", Username: "admin", DisplayName: "管理员",
		Role: model.UserRoleAdmin, Status: model.UserStatusActive,
	}

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "start",
			call: func() error {
				_, err := svc.StartAdminOperation(context.Background(), admin, AdminStartOperationRequest{Action: opsprotocol.ActionUpgrade})
				return err
			},
		},
		{
			name: "cancel",
			call: func() error {
				_, err := svc.CancelAdminOperation(context.Background(), admin, "op-1", AdminControlOperationRequest{})
				return err
			},
		},
		{
			name: "recover",
			call: func() error {
				_, err := svc.RecoverAdminOperation(context.Background(), admin, "op-1", AdminControlOperationRequest{})
				return err
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			var authError *AuthError
			if !errors.As(err, &authError) || authError.Status != 403 {
				t.Fatalf("expected source deployment rejection, got %v", err)
			}
			if authError.Message != "生产发布由 GitHub Actions 源码流水线管理，后台不再执行运维写操作" {
				t.Fatalf("unexpected rejection message: %s", authError.Message)
			}
		})
	}

	if client.writeCalls != 0 {
		t.Fatalf("operations client received %d write calls", client.writeCalls)
	}
}

type operationsClientStub struct {
	writeCalls int
}

func (s *operationsClientStub) Overview(context.Context) (*opsprotocol.Overview, error) {
	return &opsprotocol.Overview{}, nil
}

func (s *operationsClientStub) Operations(context.Context, int) (*opsprotocol.OperationPage, error) {
	return &opsprotocol.OperationPage{}, nil
}

func (s *operationsClientStub) Operation(context.Context, string) (*opsprotocol.Operation, error) {
	return &opsprotocol.Operation{}, nil
}

func (s *operationsClientStub) OperationLogs(context.Context, string, uint64, int) (*opsprotocol.OperationLogPage, error) {
	return &opsprotocol.OperationLogPage{}, nil
}

func (s *operationsClientStub) Backups(context.Context, int) ([]opsprotocol.Backup, error) {
	return []opsprotocol.Backup{}, nil
}

func (s *operationsClientStub) StartOperation(context.Context, opsprotocol.StartOperationRequest) (*opsprotocol.Operation, error) {
	s.writeCalls++
	return &opsprotocol.Operation{}, nil
}

func (s *operationsClientStub) CancelOperation(context.Context, string, opsprotocol.CancelOperationRequest) (*opsprotocol.Operation, error) {
	s.writeCalls++
	return &opsprotocol.Operation{}, nil
}

func (s *operationsClientStub) RecoverOperation(context.Context, string, opsprotocol.RecoverOperationRequest) (*opsprotocol.Operation, error) {
	s.writeCalls++
	return &opsprotocol.Operation{}, nil
}
