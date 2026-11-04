package remotesessions

import "github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"

// ConnectionOutageHappened attributes an ErrRemoteSessionUnavailable to its known owner.
// An unclassified outage is ours, not evidence that the provider failed.
func ConnectionOutageHappened(err error) mcpconnectionattempts.Code {
	switch OutageKindOf(err) {
	case RefreshOutageProvider:
		return mcpconnectionattempts.CodeRemoteSessionUnavailable
	case RefreshOutageTunnel:
		return mcpconnectionattempts.CodeTunnelUnavailable
	default:
		return mcpconnectionattempts.CodeInternalError
	}
}
