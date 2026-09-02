package agentruntime_test

import (
	"encoding/json"
	"errors"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestDecodeMediaCandidateContentRequiresExactCommercialFacts(t *testing.T) {
	valid := agentruntime.MediaCandidateContent{
		CandidateKey:            "character-hero-candidate-1",
		MediaKind:               agentruntime.ArtifactImage,
		ProviderRequestIdentity: "provider-request-1:01",
		ResourceID:              "resource-1",
		SourceTaskID:            "task-1",
	}
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeMediaCandidateContent(encoded)
	if err != nil {
		t.Fatalf("valid media candidate rejected: %v", err)
	}
	if decoded != valid {
		t.Fatalf("decoded media candidate = %#v, want %#v", decoded, valid)
	}

	invalidPayloads := map[string]string{
		"missing source task": `{"candidateKey":"character-hero-candidate-1","mediaKind":"image","providerRequestIdentity":"provider-request-1:01","resourceId":"resource-1"}`,
		"unknown media kind":  `{"candidateKey":"character-hero-candidate-1","mediaKind":"document","providerRequestIdentity":"provider-request-1:01","resourceId":"resource-1","sourceTaskId":"task-1"}`,
		"unknown field":       `{"candidateKey":"character-hero-candidate-1","mediaKind":"image","providerRequestIdentity":"provider-request-1:01","resourceId":"resource-1","sourceTaskId":"task-1","fallback":true}`,
		"trailing document":   `{"candidateKey":"character-hero-candidate-1","mediaKind":"image","providerRequestIdentity":"provider-request-1:01","resourceId":"resource-1","sourceTaskId":"task-1"}{}`,
	}
	for name, payload := range invalidPayloads {
		t.Run(name, func(t *testing.T) {
			if _, err := agentruntime.DecodeMediaCandidateContent([]byte(payload)); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
				t.Fatalf("invalid media candidate error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
			}
		})
	}
}

func TestDecodeMediaGenerationToolResultRequiresUniqueCandidateLineage(t *testing.T) {
	first := agentruntime.ArtifactRevisionRef{ArtifactID: "candidate-1", RevisionID: "revision-1"}
	second := agentruntime.ArtifactRevisionRef{ArtifactID: "candidate-2", RevisionID: "revision-2"}
	valid := agentruntime.MediaGenerationToolResult{
		TaskID:         "task-1",
		BillingOrderID: "billing-order-1",
		AudioMode:      agentruntime.MediaAudioNone,
		Candidates:     []agentruntime.ArtifactRevisionRef{first, second},
	}
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := agentruntime.DecodeMediaGenerationToolResult(encoded)
	if err != nil {
		t.Fatalf("valid media generation result rejected: %v", err)
	}
	if decoded.TaskID != valid.TaskID || decoded.BillingOrderID != valid.BillingOrderID ||
		decoded.AudioMode != valid.AudioMode || len(decoded.Candidates) != len(valid.Candidates) {
		t.Fatalf("decoded media generation result = %#v, want %#v", decoded, valid)
	}

	invalidResults := map[string]agentruntime.MediaGenerationToolResult{
		"missing task": {
			BillingOrderID: "billing-order-1", AudioMode: agentruntime.MediaAudioNone,
			Candidates: []agentruntime.ArtifactRevisionRef{first},
		},
		"missing billing order": {
			TaskID: "task-1", AudioMode: agentruntime.MediaAudioNone,
			Candidates: []agentruntime.ArtifactRevisionRef{first},
		},
		"invalid audio mode": {
			TaskID: "task-1", BillingOrderID: "billing-order-1", AudioMode: agentruntime.MediaAudioMode("fallback"),
			Candidates: []agentruntime.ArtifactRevisionRef{first},
		},
		"duplicate candidate": {
			TaskID: "task-1", BillingOrderID: "billing-order-1", AudioMode: agentruntime.MediaAudioNone,
			Candidates: []agentruntime.ArtifactRevisionRef{first, first},
		},
	}
	for name, result := range invalidResults {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := agentruntime.DecodeMediaGenerationToolResult(encoded); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
				t.Fatalf("invalid media generation result error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
			}
		})
	}

	unknown := `{"taskId":"task-1","billingOrderId":"billing-order-1","audioMode":"none","candidates":[{"artifactId":"candidate-1","revisionId":"revision-1"}],"legacyTaskId":"task-old"}`
	if _, err := agentruntime.DecodeMediaGenerationToolResult([]byte(unknown)); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
		t.Fatalf("unknown media generation result field error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
	}
}

func TestDecodeMediaTaskResultResourcesRequiresUsableUniqueResources(t *testing.T) {
	resources, err := agentruntime.DecodeMediaTaskResultResources([]byte(`{
		"images":[{"resourceId":"image-resource"},{"resourceId":"image-resource"}],
		"audio":{"resourceId":"audio-resource"},
		"providerMetadata":{"requestId":"supplier-request"}
	}`))
	if err != nil {
		t.Fatalf("valid media task result rejected: %v", err)
	}
	want := []agentruntime.MediaTaskResultResource{
		{Kind: agentruntime.ArtifactImage, ResourceID: "image-resource"},
		{Kind: agentruntime.ArtifactAudio, ResourceID: "audio-resource"},
	}
	if len(resources) != len(want) {
		t.Fatalf("resources = %#v, want %#v", resources, want)
	}
	for index := range want {
		if resources[index] != want[index] {
			t.Fatalf("resources[%d] = %#v, want %#v", index, resources[index], want[index])
		}
	}

	invalid := map[string]string{
		"no media":           `{"providerMetadata":{"requestId":"supplier-request"}}`,
		"missing resource":   `{"images":[{"url":"https://example.com/image.png"}]}`,
		"invalid collection": `{"images":"image-resource"}`,
		"trailing document":  `{"image":{"resourceId":"image-resource"}}{}`,
	}
	for name, payload := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := agentruntime.DecodeMediaTaskResultResources([]byte(payload)); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
				t.Fatalf("invalid task result error = %v, want %v", err, agentruntime.ErrArtifactPayloadInvalid)
			}
		})
	}
}

func TestMediaGenerationOperationForRunRequiresValidRunIdentity(t *testing.T) {
	operation, err := agentruntime.MediaGenerationOperationForRun("run-1")
	if err != nil || operation != "media_generation:run-1" {
		t.Fatalf("operation = %q, err = %v", operation, err)
	}
	for _, runID := range []string{"", " run-1 ", string(make([]byte, 81))} {
		if _, err := agentruntime.MediaGenerationOperationForRun(runID); !errors.Is(err, agentruntime.ErrArtifactPayloadInvalid) {
			t.Fatalf("run %q error = %v, want %v", runID, err, agentruntime.ErrArtifactPayloadInvalid)
		}
	}
}
