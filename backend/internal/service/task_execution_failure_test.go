package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestNormalizeTaskExecutionFailureOnlyLabelsOuterDeadlineAsTaskTimeout(t *testing.T) {
	outer, cancel := context.WithCancel(context.Background())
	defer cancel()
	nested := errors.Join(errors.New("OSS 读取请求超时"), context.DeadlineExceeded)

	if got := normalizeTaskExecutionFailure(outer, "canvas_image", nested); got != nested {
		t.Fatalf("normalizeTaskExecutionFailure() = %v, want original nested dependency failure", got)
	}

	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()
	got := normalizeTaskExecutionFailure(expired, "canvas_image", nested)
	if got == nil || got.Error() != taskTimeoutMessage("canvas_image") {
		t.Fatalf("normalizeTaskExecutionFailure() = %v, want task timeout message", got)
	}
}

func TestPreProviderDispatchFailureMarksBillingAsNotStarted(t *testing.T) {
	failure := newPreProviderDispatchFailure(errors.New("reference hydration failed"))
	if !billableProviderDispatchNotStarted(failure) {
		t.Fatal("pre-provider failure must prove that no billable provider request started")
	}
	if failure.Error() != "reference hydration failed" {
		t.Fatalf("failure.Error() = %q", failure.Error())
	}
}

func TestClassifyProviderDispatchFailureMarksDialFailureAsNotStarted(t *testing.T) {
	dialFailure := &url.Error{
		Op:  "Post",
		URL: "https://provider.example/v1/images/generations",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded},
	}

	got := classifyProviderDispatchFailure(dialFailure)
	if !billableProviderDispatchNotStarted(got) {
		t.Fatal("dial failure must prove that no billable provider request started")
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatal("classified dial failure must preserve its original cause")
	}
}

func TestClassifyProviderDispatchFailureDoesNotMarkResponseFailureAsNotStarted(t *testing.T) {
	responseFailure := &url.Error{
		Op:  "Post",
		URL: "https://provider.example/v1/images/generations",
		Err: &net.OpError{Op: "read", Net: "tcp", Err: io.ErrUnexpectedEOF},
	}

	if got := classifyProviderDispatchFailure(responseFailure); billableProviderDispatchNotStarted(got) {
		t.Fatal("response failure may occur after provider dispatch and must remain reviewable")
	}
}
