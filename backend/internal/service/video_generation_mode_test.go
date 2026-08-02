package service

import "testing"

func TestValidateVideoGenerationModeInput(t *testing.T) {
	valid := []map[string]any{
		{"mode": "video", "metadata": map[string]any{"videoGenerationMode": "text"}},
		{"mode": "video", "referenceImages": []any{map[string]any{"id": "first"}}, "metadata": map[string]any{"videoGenerationMode": "image", "videoStartFrameNodeId": "first"}},
		{"mode": "video", "referenceImages": []any{map[string]any{"id": "first"}, map[string]any{"id": "last"}}, "metadata": map[string]any{"videoGenerationMode": "first_last_frame", "videoStartFrameNodeId": "first", "videoEndFrameNodeId": "last"}},
		{"mode": "video", "referenceImages": []any{map[string]any{"id": "reference"}}, "metadata": map[string]any{"videoGenerationMode": "image_reference"}},
		{"mode": "video", "referenceVideos": []any{map[string]any{"id": "video"}}, "referenceAudios": []any{map[string]any{"id": "audio"}}, "metadata": map[string]any{"videoGenerationMode": "omni_reference"}},
	}
	for index, input := range valid {
		if err := validateVideoGenerationModeInput(input); err != nil {
			t.Fatalf("valid case %d failed: %v", index, err)
		}
	}

	invalid := []map[string]any{
		{"mode": "video", "referenceImages": []any{map[string]any{"id": "unexpected"}}, "metadata": map[string]any{"videoGenerationMode": "text"}},
		{"mode": "video", "metadata": map[string]any{"videoGenerationMode": "image"}},
		{"mode": "video", "referenceImages": []any{map[string]any{"id": "first"}}, "metadata": map[string]any{"videoGenerationMode": "first_last_frame", "videoStartFrameNodeId": "first"}},
		{"mode": "video", "referenceAudios": []any{map[string]any{"id": "audio"}}, "metadata": map[string]any{"videoGenerationMode": "image_reference"}},
		{"mode": "video", "metadata": map[string]any{"videoGenerationMode": "omni_reference"}},
		{"mode": "video", "metadata": map[string]any{"videoGenerationMode": "unknown"}},
	}
	for index, input := range invalid {
		if err := validateVideoGenerationModeInput(input); err == nil {
			t.Fatalf("invalid case %d was accepted", index)
		}
	}
}
