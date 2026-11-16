package remotesessions

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// ConnectionOutageHappened attributes an ErrRemoteSessionUnavailable to its known owner.
// A refresh deferred because another request holds the lock gets the zero code: that request's row covers it.
// An unclassified outage is ours, not evidence that the provider failed.
func ConnectionOutageHappened(err error) mcpconnectionattempts.Code {
	if code, ok := mcpconnectionattempts.AddressCode(OutageAddress(err)); ok {
		return code
	}
	if TokenEndpointCausedOutage(err) {
		return mcpconnectionattempts.CodeRemoteSessionUnavailable
	}
	if errors.Is(err, ErrRefreshLockHeld) {
		return mcpconnectionattempts.Code{}
	}
	refreshErr, ok := errors.AsType[*RefreshError](err)
	if !ok {
		return mcpconnectionattempts.CodeInternalError
	}
	switch refreshErr.outageCause {
	case RefreshOutageProvider:
		return mcpconnectionattempts.CodeRemoteSessionUnavailable
	case RefreshOutageTunnel:
		return mcpconnectionattempts.CodeTunnelUnavailable
	case RefreshOutageNone:
	}
	return mcpconnectionattempts.CodeInternalError
}

// OutageAddress is the configured sign-in provider address an outage never reached, AddressFine for any other outage.
func OutageAddress(err error) guardian.AddressFailure {
	if endpoint, ok := errors.AsType[*TokenEndpointError](err); ok && !endpoint.Signing {
		return endpoint.address
	}
	refreshErr, ok := errors.AsType[*RefreshError](err)
	if !ok || refreshErr.outageCause != RefreshOutageProvider {
		return guardian.AddressFine
	}
	if deferred, ok := errors.AsType[*refreshDeferredError](refreshErr.err); ok {
		return deferred.address
	}
	return guardian.AddressFailureOf(refreshErr.err, false)
}

// OutageAddressOption names the configured sign-in provider whose address an outage never reached.
// Without one, the row's own host is marked as not where it failed.
func OutageAddressOption(err error) mcpconnectionattempts.Option {
	if endpoint, ok := errors.AsType[*TokenEndpointError](err); ok && endpoint.issuer != "" {
		return mcpconnectionattempts.Provider(endpoint.issuer)
	}
	if refreshErr, ok := errors.AsType[*RefreshError](err); ok && refreshErr.IssuerURL != "" {
		return mcpconnectionattempts.Provider(refreshErr.IssuerURL)
	}
	return mcpconnectionattempts.FailedElsewhere()
}

// SignInAddressDetail says which sign-in provider address problem it is, for an administrator only.
func SignInAddressDetail(address guardian.AddressFailure) string {
	return "The sign-in provider " + address.Problem() + "."
}
