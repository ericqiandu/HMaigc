package agentruntime

const agentToolResultLimit = 256 * 1024

type ToolRiskLevel string

const (
	ToolRiskRead  ToolRiskLevel = "L0"
	ToolRiskWrite ToolRiskLevel = "L1"
	ToolRiskCost  ToolRiskLevel = "L2"
)

type ToolExecutionLocation string

const (
	ToolExecutionServer     ToolExecutionLocation = "server"
	ToolExecutionClientFact ToolExecutionLocation = "client_fact"
	ToolExecutionClient     ToolExecutionLocation = "client"
)

type ToolPolicy struct {
	Name             ToolName
	RiskLevel        ToolRiskLevel
	RequiredAccess   AccessLevel
	ApprovalRequired bool
	Execution        ToolExecutionLocation
}

func ToolPolicyFor(name ToolName) (ToolPolicy, bool) {
	switch name {
	case ToolCanvasReadState:
		return ToolPolicy{Name: name, RiskLevel: ToolRiskRead, RequiredAccess: AccessViewer, Execution: ToolExecutionServer}, true
	case ToolCanvasReadSelection:
		return ToolPolicy{Name: name, RiskLevel: ToolRiskRead, RequiredAccess: AccessViewer, Execution: ToolExecutionClientFact}, true
	case ToolCanvasApplyOps:
		return ToolPolicy{Name: name, RiskLevel: ToolRiskWrite, RequiredAccess: AccessEditor, ApprovalRequired: true, Execution: ToolExecutionClient}, true
	case ToolGenerationSubmit:
		return ToolPolicy{Name: name, RiskLevel: ToolRiskCost, RequiredAccess: AccessEditor, ApprovalRequired: true, Execution: ToolExecutionServer}, true
	case ToolGenerationWait:
		return ToolPolicy{Name: name, RiskLevel: ToolRiskRead, RequiredAccess: AccessViewer, Execution: ToolExecutionServer}, true
	default:
		return ToolPolicy{}, false
	}
}
