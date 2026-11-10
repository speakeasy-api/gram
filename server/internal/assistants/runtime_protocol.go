package assistants

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// runtimeProtocol is the guest wire protocol, independent of the infrastructure
// that provisions a runtime. Legacy guests use runner HTTP; Kit guests use ACP
// v2 HTTP/SSE. Admission, authorization and event claiming belong to the caller.
// Implementations must not infer the protocol from an image or feature flag.
type runtimeProtocol interface {
	RunTurn(context.Context, assistantRuntimeRecord, runTurnRequest) error
	InterruptTurn(context.Context, assistantRuntimeRecord, uuid.UUID) (bool, error)
	Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error)
}

// resolveRuntimeProtocol selects once from the immutable persisted runtime
// binding, not the assistant's desired version. A missing Kit adapter must never
// fall back to the legacy runner, including during a rollback of UI flags.
func resolveRuntimeProtocol(version int16, legacy, kit runtimeProtocol) (runtimeProtocol, error) {
	var protocol runtimeProtocol
	switch version {
	case 1, 2:
		protocol = legacy
	case 3:
		protocol = kit
	default:
		return nil, fmt.Errorf("unsupported assistant runtime version %d", version)
	}
	if protocol == nil {
		return nil, fmt.Errorf("assistant runtime protocol version %d is not configured", version)
	}
	return protocol, nil
}
