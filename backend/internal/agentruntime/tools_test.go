package agentruntime_test

import (
	"reflect"
	"testing"

	"infinite-canvas/backend/internal/agentruntime"
)

func TestToolPolicyForSchemaFreezesProductionRiskAndAccess(t *testing.T) {
	cases := []struct {
		name   agentruntime.ToolName
		risk   agentruntime.ToolRiskLevel
		access agentruntime.AccessLevel
	}{
		{name: agentruntime.ToolSkillLoad, risk: agentruntime.ToolRiskRead, access: agentruntime.AccessViewer},
		{name: agentruntime.ToolSpecialistDelegate, risk: agentruntime.ToolRiskWrite, access: agentruntime.AccessEditor},
		{name: agentruntime.ToolVisionAnalyze, risk: agentruntime.ToolRiskCost, access: agentruntime.AccessEditor},
		{name: agentruntime.ToolMediaGenerate, risk: agentruntime.ToolRiskCost, access: agentruntime.AccessEditor},
		{name: agentruntime.ToolCanvasProject, risk: agentruntime.ToolRiskWrite, access: agentruntime.AccessEditor},
	}
	for _, testCase := range cases {
		policy, ok := agentruntime.ToolPolicyForSchema(testCase.name, agentruntime.ProductionToolSchemaVersion)
		if !ok {
			t.Fatalf("missing production policy for %q", testCase.name)
		}
		if policy.RiskLevel != testCase.risk || policy.RequiredAccess != testCase.access {
			t.Fatalf("policy for %q = %#v", testCase.name, policy)
		}
	}

	if _, ok := agentruntime.ToolPolicyForSchema(agentruntime.ToolProductionPlan, agentruntime.ProductionToolSchemaVersion); ok {
		t.Fatal("production schema exposed retired production.plan")
	}
	if _, ok := agentruntime.ToolPolicyForSchema(agentruntime.ToolSpecialistDelegate, agentruntime.CurrentToolSchemaVersion); !ok {
		t.Fatal("current schema did not expose specialist.delegate")
	}
	if _, ok := agentruntime.ToolPolicyForSchema(agentruntime.ToolSpecialistDelegate, agentruntime.LegacyToolSchemaVersion); ok {
		t.Fatal("legacy history schema exposed specialist.delegate")
	}
}

func TestToolPoliciesForSchemaExposeExactFrozenVocabulary(t *testing.T) {
	t.Parallel()

	production, ok := agentruntime.ToolPoliciesForSchema(agentruntime.ProductionToolSchemaVersion)
	if !ok {
		t.Fatal("production tool schema is unavailable")
	}
	productionNames := make([]agentruntime.ToolName, 0, len(production))
	for _, policy := range production {
		productionNames = append(productionNames, policy.Name)
	}
	wantProduction := []agentruntime.ToolName{
		agentruntime.ToolSkillLoad,
		agentruntime.ToolSpecialistDelegate,
		agentruntime.ToolVisionAnalyze,
		agentruntime.ToolMediaGenerate,
		agentruntime.ToolCanvasProject,
		agentruntime.ToolMediaAssemble,
	}
	if !reflect.DeepEqual(productionNames, wantProduction) {
		t.Fatalf("production tools = %#v, want %#v", productionNames, wantProduction)
	}

	legacy, ok := agentruntime.ToolPoliciesForSchema(agentruntime.LegacyToolSchemaVersion)
	if !ok {
		t.Fatal("legacy tool schema is unavailable")
	}
	legacyNames := make([]agentruntime.ToolName, 0, len(legacy))
	for _, policy := range legacy {
		legacyNames = append(legacyNames, policy.Name)
	}
	wantLegacy := []agentruntime.ToolName{
		agentruntime.ToolSkillLoad,
		agentruntime.ToolProductionPlan,
		agentruntime.ToolProductionRender,
		agentruntime.ToolCanvasCommit,
	}
	if !reflect.DeepEqual(legacyNames, wantLegacy) {
		t.Fatalf("legacy tools = %#v, want %#v", legacyNames, wantLegacy)
	}

	if _, ok := agentruntime.ToolPoliciesForSchema(999); ok {
		t.Fatal("unknown tool schema was accepted")
	}
}

func TestToolSchemaV5AddsOnlyMediaAssembly(t *testing.T) {
	t.Parallel()

	policies, ok := agentruntime.ToolPoliciesForSchema(agentruntime.CurrentToolSchemaVersion)
	if !ok {
		t.Fatal("tool schema v5 is unavailable")
	}
	names := make([]agentruntime.ToolName, 0, len(policies))
	for _, policy := range policies {
		names = append(names, policy.Name)
	}
	want := []agentruntime.ToolName{
		agentruntime.ToolSkillLoad,
		agentruntime.ToolSpecialistDelegate,
		agentruntime.ToolVisionAnalyze,
		agentruntime.ToolMediaGenerate,
		agentruntime.ToolCanvasProject,
		agentruntime.ToolMediaAssemble,
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("v5 tools = %#v, want %#v", names, want)
	}

	policy, ok := agentruntime.ToolPolicyForSchema(agentruntime.ToolMediaAssemble, agentruntime.CurrentToolSchemaVersion)
	if !ok || policy.RiskLevel != agentruntime.ToolRiskWrite || policy.RequiredAccess != agentruntime.AccessEditor {
		t.Fatalf("media.assemble policy = %#v, found=%v", policy, ok)
	}
	if !agentruntime.ApprovalRequiredFor(policy, agentruntime.ExecutionGuided) {
		t.Fatal("guided media.assemble did not require approval")
	}
	if agentruntime.ApprovalRequiredFor(policy, agentruntime.ExecutionAutomatic) {
		t.Fatal("automatic media.assemble required a second tool approval")
	}
	if _, ok := agentruntime.ToolPolicyForSchema(agentruntime.ToolMediaAssemble, 4); ok {
		t.Fatal("tool schema v4 exposed media.assemble")
	}
	if _, ok := agentruntime.ToolPolicyForSchema(agentruntime.ToolName("media.assemble.v2"), agentruntime.CurrentToolSchemaVersion); ok {
		t.Fatal("tool schema v5 accepted an unknown tool")
	}
}
