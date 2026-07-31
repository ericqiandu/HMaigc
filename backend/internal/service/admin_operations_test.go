package service

import (
	"errors"
	"testing"

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
