package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResponsesStreamKeepsDistinctTextParts(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"first"}`,
		``,
		`data: {"type":"response.output_text.done","output_index":0,"content_index":0,"text":"first"}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":1,"delta":"second"}`,
		``,
		`data: {"type":"response.output_text.done","output_index":0,"content_index":1,"text":"second"}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":"second"}]}]}}`,
		``, ``,
	}, "\n")
	var emitted strings.Builder
	result, err := parseResponsesSSE(context.Background(), strings.NewReader(stream), func(text string) error { emitted.WriteString(text); return nil })
	if err != nil {
		t.Fatalf("valid multipart stream rejected: %v", err)
	}
	if result.Text != "firstsecond" || emitted.String() != "firstsecond" {
		t.Fatalf("text=%q emitted=%q", result.Text, emitted.String())
	}
}

func TestResponsesTextPartCompletionIsIdempotentAndRejectsConflict(t *testing.T) {
	for _, final := range []string{"second", "conflict"} {
		t.Run(final, func(t *testing.T) {
			stream := "data: {\"type\":\"response.output_text.done\",\"output_index\":0,\"content_index\":0,\"text\":\"first\"}\n\n" +
				"data: {\"type\":\"response.output_text.done\",\"output_index\":1,\"content_index\":0,\"text\":\"second\"}\n\n" +
				"data: {\"type\":\"response.output_text.done\",\"output_index\":1,\"content_index\":0,\"text\":\"" + final + "\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
			var emitted strings.Builder
			result, err := parseResponsesSSE(context.Background(), strings.NewReader(stream), func(value string) error { emitted.WriteString(value); return nil })
			if final == "conflict" {
				if !errors.Is(err, errAgentProviderStreamProtocolInvalid) {
					t.Fatalf("conflict accepted: %v", err)
				}
			} else if err != nil || result.Text != "firstsecond" {
				t.Fatalf("result=%q error=%v", result.Text, err)
			}
			want := "firstsecond"
			if final == "conflict" {
				want = "first"
			}
			if emitted.String() != want {
				t.Fatalf("duplicate or conflicting text emitted: %q", emitted.String())
			}
		})
	}
}

func TestResponsesInterleavedPartsUseCanonicalOrder(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":1,"item_id":"m","delta":"second"}`,
		``,
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"m","delta":"fi"}`,
		``,
		`data: {"type":"response.output_text.done","output_index":0,"content_index":1,"item_id":"m","text":"second"}`,
		``,
		`data: {"type":"response.output_text.done","output_index":0,"content_index":0,"item_id":"m","text":"first"}`,
		``,
		`data: {"type":"response.completed","response":{"status":"completed","output":[{"id":"m","type":"message","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":"second"}]}]}}`, ``, ``,
	}, "\n")
	var emitted strings.Builder
	result, err := parseResponsesSSE(context.Background(), strings.NewReader(stream), func(value string) error { emitted.WriteString(value); return nil })
	if err != nil || result.Text != "firstsecond" || emitted.String() != "firstsecond" {
		t.Fatalf("text=%q emitted=%q err=%v", result.Text, emitted.String(), err)
	}
}

func TestResponsesRejectsPartIdentityAndLifecycleConflicts(t *testing.T) {
	for _, next := range []string{
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"item_id":"other","text":"first"}`,
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"m","delta":"late"}`,
		`{"type":"response.output_text.done","output_index":-1,"content_index":0,"text":"first"}`,
		`{"type":"response.output_text.done","output_index":0,"content_index":0,"item_id":"m","text":"first extended"}`,
	} {
		stream := "data: {\"type\":\"response.output_text.done\",\"output_index\":0,\"content_index\":0,\"item_id\":\"m\",\"text\":\"first\"}\n\ndata: " + next + "\n\n"
		_, err := parseResponsesSSE(context.Background(), strings.NewReader(stream), func(string) error { return nil })
		if !errors.Is(err, errAgentProviderStreamProtocolInvalid) {
			t.Fatalf("identity/lifecycle conflict accepted: %s: %v", next, err)
		}
	}
}

func TestResponsesCompletedSnapshotCannotMoveTextBetweenParts(t *testing.T) {
	stream := "data: {\"type\":\"response.output_text.done\",\"output_index\":0,\"content_index\":0,\"text\":\"ab\"}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"output_index\":0,\"content_index\":1,\"text\":\"c\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"a\"},{\"type\":\"output_text\",\"text\":\"bc\"}]}]}}\n\n"
	_, err := parseResponsesSSE(context.Background(), strings.NewReader(stream), func(string) error { return nil })
	if !errors.Is(err, errAgentProviderStreamProtocolInvalid) {
		t.Fatalf("part boundaries changed without rejection: %v", err)
	}
}
