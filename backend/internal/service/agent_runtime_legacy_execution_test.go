package service

import "testing"

func skipRetiredAgentExecutionGraph(t *testing.T) {
	t.Helper()
	t.Skip("retired v4/v5 production and specialist execution graph; historical persistence remains covered until the hard-cut deletion task removes this test")
}

func skipPendingAtomicSkillAdapter(t *testing.T) {
	t.Helper()
	t.Skip("legacy skill.load adapter test; the v6 skills.load adapter is implemented and tested in the next capability-adapter milestone")
}
