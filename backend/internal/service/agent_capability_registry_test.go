package service

import (
	"encoding/json"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestAgentCapabilityRegistryDeclaresExactlyCurrentCapabilities(t *testing.T) {
	t.Parallel()

	registry, err := newAgentCapabilityRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptors := registry.Descriptors()
	want := []agentruntime.ToolName{
		agentruntime.ToolCanvasRead,
		agentruntime.ToolCanvasApplyOps,
		agentruntime.ToolAssetsRead,
		agentruntime.ToolAssetsPublish,
		agentruntime.ToolMediaGenerate,
		agentruntime.ToolVisionAnalyze,
		agentruntime.ToolSkillsLoad,
	}
	if len(descriptors) != len(want) {
		t.Fatalf("capability descriptors = %#v", descriptors)
	}
	for index, name := range want {
		descriptor := descriptors[index]
		policy, found := agentruntime.ToolPolicyForSchema(name, agentruntime.CurrentToolSchemaVersion)
		if !found {
			t.Fatalf("policy unavailable for %q", name)
		}
		if descriptor.Name != name || descriptor.ActionVersion != 1 || descriptor.RiskLevel != policy.RiskLevel || descriptor.RequiredAccess != policy.RequiredAccess {
			t.Fatalf("descriptor[%d] = %#v, policy = %#v", index, descriptor, policy)
		}
		if !validCapabilitySchema(descriptor.ArgumentsSchema) || !validCapabilitySchema(descriptor.ResultSchema) {
			t.Fatalf("descriptor[%d] schemas are invalid: %#v", index, descriptor)
		}
	}
}

func TestVisionAnalyzeCapabilityDeclaresClosedInputAndUsageResult(t *testing.T) {
	t.Parallel()

	registry, err := newAgentCapabilityRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	var visionDescriptor *agentCapabilityDescriptor
	for _, descriptor := range registry.Descriptors() {
		if descriptor.Name == agentruntime.ToolVisionAnalyze {
			copy := descriptor
			visionDescriptor = &copy
			break
		}
	}
	if visionDescriptor == nil {
		t.Fatal("vision.analyze descriptor is missing")
	}
	var arguments struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(visionDescriptor.ArgumentsSchema, &arguments); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"modelRecordId", "modelKey", "sourceResourceIds", "prompt", "detail", "clientRequestId"} {
		if _, found := arguments.Properties[field]; !found {
			t.Fatalf("vision arguments schema is missing %q", field)
		}
	}
	if len(arguments.Required) != 6 {
		t.Fatalf("vision required arguments = %#v", arguments.Required)
	}
	var result struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(visionDescriptor.ResultSchema, &result); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"taskId", "billingOrderId", "modelRecordId", "modelKey", "clientRequestId", "sourceResourceIds", "detail", "analysis", "usage"} {
		if _, found := result.Properties[field]; !found {
			t.Fatalf("vision result schema is missing %q", field)
		}
	}
	if len(result.Required) != 9 {
		t.Fatalf("vision required result fields = %#v", result.Required)
	}
}

func TestMediaGenerateCapabilityDeclaresExactParameterVariants(t *testing.T) {
	t.Parallel()

	registry, err := newAgentCapabilityRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	var mediaDescriptor *agentCapabilityDescriptor
	for _, descriptor := range registry.Descriptors() {
		if descriptor.Name == agentruntime.ToolMediaGenerate {
			copy := descriptor
			mediaDescriptor = &copy
			break
		}
	}
	if mediaDescriptor == nil {
		t.Fatal("media.generate descriptor is missing")
	}
	var document struct {
		Properties map[string]struct {
			MinLength int `json:"minLength"`
			OneOf     []struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"oneOf"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(mediaDescriptor.ArgumentsSchema, &document); err != nil {
		t.Fatal(err)
	}
	variants := document.Properties["parameters"].OneOf
	if len(variants) != 3 {
		t.Fatalf("media parameter variants = %#v", variants)
	}
	if document.Properties["targetCanvasNodeId"].MinLength != 1 {
		t.Fatalf("media target canvas node schema = %#v", document.Properties["targetCanvasNodeId"])
	}
	wantFields := [][]string{
		{"prompt", "aspectRatio", "resolution", "quality", "count", "transparentBackground"},
		{"prompt", "aspectRatio", "resolution", "durationSeconds", "generateAudio"},
		{"prompt", "voice", "format", "speed", "volume", "pitch", "emotion", "languageBoost", "sampleRate", "bitrate", "channel", "instructions"},
	}
	for _, required := range variants[0].Required {
		if required == "quality" {
			t.Fatal("image quality must remain optional when callable model capabilities declare no quality variants")
		}
	}
	for index, fields := range wantFields {
		for _, field := range fields {
			if _, found := variants[index].Properties[field]; !found {
				t.Fatalf("media parameter variant %d is missing %q: %#v", index, field, variants[index])
			}
		}
		if len(variants[index].Required) == 0 {
			t.Fatalf("media parameter variant %d has no required fields", index)
		}
	}
}

func TestCanvasApplyOpsCapabilityDeclaresExactOperationVariants(t *testing.T) {
	t.Parallel()

	registry, err := newAgentCapabilityRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	var canvasDescriptor *agentCapabilityDescriptor
	for _, descriptor := range registry.Descriptors() {
		if descriptor.Name == agentruntime.ToolCanvasApplyOps {
			copy := descriptor
			canvasDescriptor = &copy
			break
		}
	}
	if canvasDescriptor == nil {
		t.Fatal("canvas.apply_ops descriptor is missing")
	}
	var document struct {
		Properties map[string]struct {
			MinItems int `json:"minItems"`
			MaxItems int `json:"maxItems"`
			Items    struct {
				OneOf []struct {
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				} `json:"oneOf"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(canvasDescriptor.ArgumentsSchema, &document); err != nil {
		t.Fatal(err)
	}
	operations := document.Properties["operations"]
	if operations.MinItems != 1 || operations.MaxItems != 100 || len(operations.Items.OneOf) != 7 {
		t.Fatalf("canvas operation schema = %#v", operations)
	}
	want := []string{"add_node", "update_node", "delete_node", "connect_nodes", "delete_connections", "set_viewport", "select_nodes"}
	for index, variant := range operations.Items.OneOf {
		var discriminator struct {
			Const string `json:"const"`
		}
		if err := json.Unmarshal(variant.Properties["type"], &discriminator); err != nil {
			t.Fatal(err)
		}
		if discriminator.Const != want[index] || len(variant.Required) == 0 {
			t.Fatalf("canvas operation variant %d = %#v", index, variant)
		}
	}
	var addNode struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(operations.Items.OneOf[0].Properties["node"], &addNode); err != nil {
		t.Fatal(err)
	}
	var nodeType struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(addNode.Properties["type"], &nodeType); err != nil {
		t.Fatal(err)
	}
	wantNodeTypes := []string{"image", "text", "script", "skill", "config", "video", "audio", "frame"}
	if got := nodeType.Enum; !equalStrings(got, wantNodeTypes) {
		t.Fatalf("canvas node type enum = %#v, want %#v", got, wantNodeTypes)
	}
}

func equalStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func validCapabilitySchema(schema json.RawMessage) bool {
	var document struct {
		Type                 string                     `json:"type"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
	}
	if json.Unmarshal(schema, &document) != nil || document.Type != "object" || document.AdditionalProperties == nil || *document.AdditionalProperties || len(document.Properties) == 0 || len(document.Required) == 0 {
		return false
	}
	return true
}
