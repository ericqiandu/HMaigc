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
