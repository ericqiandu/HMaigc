package service

import "testing"

func TestProviderFailureDetailsReadsTopLevelModerationError(t *testing.T) {
	code, message := providerFailureDetails(map[string]any{
		"code":    contentModerationErrorCode,
		"message": "prompt rejected",
	})
	if code != contentModerationErrorCode {
		t.Fatalf("unexpected code: %q", code)
	}
	if message != "prompt rejected" {
		t.Fatalf("unexpected message: %q", message)
	}
}

func TestProviderFailureDetailsReadsNestedError(t *testing.T) {
	code, message := providerFailureDetails(map[string]any{
		"error": map[string]any{"code": "invalid_request", "message": "invalid size"},
	})
	if code != "invalid_request" || message != "invalid size" {
		t.Fatalf("unexpected failure details: code=%q message=%q", code, message)
	}
}

func TestProviderFailureDetailsPrefersNestedBusinessCode(t *testing.T) {
	code, message := providerFailureDetails(map[string]any{
		"code": float64(400),
		"data": map[string]any{"code": contentModerationErrorCode, "message": "prompt rejected"},
	})
	if code != contentModerationErrorCode || message != "prompt rejected" {
		t.Fatalf("unexpected wrapped failure details: code=%q message=%q", code, message)
	}
}

func TestProviderFailureDetailsReadsMiniMaxBaseResponse(t *testing.T) {
	code, message := providerFailureDetails(map[string]any{
		"base_resp": map[string]any{"status_code": float64(1004), "status_msg": "voice id not found"},
	})
	if code != "1004" || message != "voice id not found" {
		t.Fatalf("unexpected MiniMax failure details: code=%q message=%q", code, message)
	}
}

func TestContentModerationFailureRequiresExactProviderCode(t *testing.T) {
	if !isContentModerationFailure(`{"code":"sensitive_words_detected"}`) {
		t.Fatal("expected moderation error to be detected")
	}
	if isContentModerationFailure("上游 HTTP 400") {
		t.Fatal("generic HTTP 400 must remain retryable")
	}
}
