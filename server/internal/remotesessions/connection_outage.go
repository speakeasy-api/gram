package remotesessions

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
)

// ConnectionOutageHappened attributes an ErrRemoteSessionUnavailable to its known owner.
// A refresh that waited on another request gets the zero code: that request's row covers it.
// An unclassified outage is ours, not evidence that the provider failed.
func ConnectionOutageHappened(err error) mcpconnectionattempts.Code {
	if TokenEndpointCausedOutage(err) {
		return mcpconnectionattempts.CodeRemoteSessionUnavailable
	}
	refreshErr, ok := errors.AsType[*RefreshError](err)
	if !ok {
		return mcpconnectionattempts.CodeInternalError
	}
	switch refreshErr.outage {
	case RefreshOutageProvider:
		return mcpconnectionattempts.CodeRemoteSessionUnavailable
	case RefreshOutageTunnel:
		return mcpconnectionattempts.CodeTunnelUnavailable
	case RefreshOutageNone:
	}
	if refreshErr.Outcome == remotesessionmetrics.RefreshOutcomeDeferred {
		return mcpconnectionattempts.Code{}
	}
	return mcpconnectionattempts.CodeInternalError
}
