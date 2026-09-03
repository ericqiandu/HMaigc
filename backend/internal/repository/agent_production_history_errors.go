package repository

import "errors"

// ErrAgentProductionArtifactConflict remains part of historical transition
// validation used by the current runtime event repository.
var ErrAgentProductionArtifactConflict = errors.New("agent production artifact state conflict")
