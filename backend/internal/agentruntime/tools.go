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
}

func ApprovalRequiredFor(policy ToolPolicy, mode ExecutionMode) bool {
	if policy.RiskLevel == ToolRiskCost {
		return true
	}
	return mode == ExecutionGuided && policy.RiskLevel == ToolRiskWrite
}
