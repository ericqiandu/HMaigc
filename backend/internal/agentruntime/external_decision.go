package agentruntime

import "errors"

var ErrExternalDecisionConflict = errors.New("agent external decision conflict")

type ExternalDecisionInput struct {
	ExpectedStateVersion int
	Decision             ModelDecision
	Evidence             DeliveryEvidence
}

// AdvanceExternalDecision applies one decision produced by an external
// reasoning host to the same bounded runtime used by managed reasoning.
// Idempotency is enforced by the persistence layer, where complete tool and
// request identity history is available.
func AdvanceExternalDecision(current RuntimeState, input ExternalDecisionInput) (RuntimeTransition, error) {
	if err := ValidateRuntimeState(current); err != nil {
		return RuntimeTransition{}, err
	}
	if input.ExpectedStateVersion < 1 || current.StateVersion != input.ExpectedStateVersion || current.Status != RunRunning {
		return RuntimeTransition{}, ErrExternalDecisionConflict
	}
	return AdvanceForToolSchema(current, RuntimeInput{
		Decision: input.Decision,
		Evidence: input.Evidence,
	}, CurrentToolSchemaVersion)
}
