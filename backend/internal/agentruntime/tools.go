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
	case ProductionToolSchemaVersion:
		names = []ToolName{ToolSkillLoad, ToolSpecialistDelegate, ToolVisionAnalyze, ToolMediaGenerate, ToolCanvasProject, ToolMediaAssemble}
	case RetiredCloudToolSchemaVersion:
		names = []ToolName{ToolCanvasRead, ToolCanvasApplyOps, ToolAssetsRead, ToolAssetsPublish, ToolMediaGenerate, ToolSkillsLoad}
	case CurrentToolSchemaVersion:
		names = []ToolName{ToolCanvasRead, ToolCanvasApplyOps, ToolAssetsRead, ToolAssetsPublish, ToolMediaGenerate, ToolVisionAnalyze, ToolSkillsLoad}
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
	case ProductionToolSchemaVersion:
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
	case CurrentToolSchemaVersion:
		switch name {
		case ToolCanvasRead, ToolAssetsRead, ToolSkillsLoad:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskRead, RequiredAccess: AccessViewer}, true
		case ToolCanvasApplyOps, ToolAssetsPublish:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskWrite, RequiredAccess: AccessEditor}, true
		case ToolMediaGenerate, ToolVisionAnalyze:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskCost, RequiredAccess: AccessEditor}, true
		default:
			return ToolPolicy{}, false
		}
	case RetiredCloudToolSchemaVersion:
		switch name {
		case ToolCanvasRead, ToolAssetsRead, ToolSkillsLoad:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskRead, RequiredAccess: AccessViewer}, true
		case ToolCanvasApplyOps, ToolAssetsPublish:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskWrite, RequiredAccess: AccessEditor}, true
		case ToolMediaGenerate:
			return ToolPolicy{Name: name, RiskLevel: ToolRiskCost, RequiredAccess: AccessEditor}, true
		default:
			return ToolPolicy{}, false
		}
	default:
		return ToolPolicy{}, false
	}
}

func ApprovalRequiredFor(policy ToolPolicy, _ ExecutionMode) bool {
	return policy.RiskLevel == ToolRiskWrite || policy.RiskLevel == ToolRiskCost
}
