package agentruntime

const agentToolResultLimit = 256 * 1024

type ToolRiskLevel string

const (
	ToolRiskRead  ToolRiskLevel = "L0"
	ToolRiskWrite ToolRiskLevel = "L1"
	ToolRiskCost  ToolRiskLevel = "L2"
)

type ToolPolicy struct {
	Name           ToolName
	RiskLevel      ToolRiskLevel
	RequiredAccess AccessLevel
}

func ToolPolicyFor(name ToolName) (ToolPolicy, bool) {
	return ToolPolicyForSchema(name, CurrentToolSchemaVersion)
}

func ToolPoliciesForSchema(toolSchemaVersion int) ([]ToolPolicy, bool) {
	var names []ToolName
	switch toolSchemaVersion {
	case LegacyToolSchemaVersion:
		names = []ToolName{ToolSkillLoad, ToolProductionPlan, ToolProductionRender, ToolCanvasCommit}
	case CurrentToolSchemaVersion:
		names = []ToolName{ToolSkillLoad, ToolSpecialistDelegate, ToolVisionAnalyze, ToolMediaGenerate, ToolCanvasProject, ToolMediaAssemble}
	default:
		return nil, false
	}
	policies := make([]ToolPolicy, 0, len(names))
	for _, name := range names {
		policy, ok := ToolPolicyForSchema(name, toolSchemaVersion)
		if !ok {
			return nil, false
		}
		policies = append(policies, policy)
	}
	return policies, true
}

func ToolPolicyForSchema(name ToolName, toolSchemaVersion int) (ToolPolicy, bool) {
	switch toolSchemaVersion {
	case LegacyToolSchemaVersion:
		switch name {
		case ToolSkillLoad:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskRead, RequiredAccess: AccessViewer}, true
		case ToolProductionPlan:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskWrite, RequiredAccess: AccessEditor}, true
		case ToolProductionRender:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskCost, RequiredAccess: AccessEditor}, true
		case ToolCanvasCommit:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskWrite, RequiredAccess: AccessEditor}, true
		default:
			return ToolPolicy{}, false
		}
	case CurrentToolSchemaVersion:
		switch name {
		case ToolSkillLoad:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskRead, RequiredAccess: AccessViewer}, true
		case ToolSpecialistDelegate, ToolCanvasProject, ToolMediaAssemble:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskWrite, RequiredAccess: AccessEditor}, true
		case ToolVisionAnalyze, ToolMediaGenerate:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskCost, RequiredAccess: AccessEditor}, true
		default:
			return ToolPolicy{}, false
		}
	default:
		return ToolPolicy{}, false
	}
}

func ApprovalRequiredFor(policy ToolPolicy, mode ExecutionMode) bool {
	if policy.RiskLevel == ToolRiskCost {
		return true
	}
	return mode == ExecutionGuided && policy.RiskLevel == ToolRiskWrite
}
