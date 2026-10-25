package remotesessions

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RemoteLoginPhase is how far a provider callback got before it ended.
type RemoteLoginPhase string

const (
	// RemoteLoginPhaseValidating: the state was consumed and the callback was
	// being checked against it.
	RemoteLoginPhaseValidating RemoteLoginPhase = "validating"
	// RemoteLoginPhaseDenied: the provider answered with an error.
	RemoteLoginPhaseDenied RemoteLoginPhase = "denied"
	// RemoteLoginPhaseRetrying: the provider refused the resource and a
	// sibling leg without it was being minted.
	RemoteLoginPhaseRetrying RemoteLoginPhase = "retrying"
	// RemoteLoginPhaseExchanging: the code was being exchanged at the
	// provider's token endpoint.
	RemoteLoginPhaseExchanging RemoteLoginPhase = "exchanging"
	// RemoteLoginPhaseStoring: tokens were received and being stored.
	RemoteLoginPhaseStoring RemoteLoginPhase = "storing"
	// RemoteLoginPhaseStored: the credential was committed.
	RemoteLoginPhaseStored RemoteLoginPhase = "stored"
	// RemoteLoginPhaseRetried: a sibling leg was sent back to the provider;
	// no credential was stored yet.
	RemoteLoginPhaseRetried RemoteLoginPhase = "retried"
)

// RemoteLoginObservation is what one provider callback did, known only once
// its single-use state was consumed: a callback whose state is missing has
// none, because nothing trustworthy names its gateway.
type RemoteLoginObservation struct {
	// ProjectID, OrganizationID, MetaMcpServerID and RouteBase are copied
	// from the consumed state.
	ProjectID       uuid.UUID
	OrganizationID  string
	MetaMcpServerID uuid.NullUUID
	RouteBase       mcproute.Base
	// Subject is the canonical subject the state was stamped with, unset when
	// the state carries none.
	Subject *urn.SessionSubject
	// InboundUserSessionClientID is the registered gateway client the login
	// started from, when the state recorded one.
	InboundUserSessionClientID uuid.NullUUID
	// RemoteSessionClientID is the provider client the login was started
	// for. It locates the provider when ProviderIssuerID is unset and is
	// never recorded itself.
	RemoteSessionClientID uuid.UUID
	// ProviderIssuerID is the provider's remote session issuer, set once the
	// callback loaded its client.
	ProviderIssuerID uuid.NullUUID
	// Phase is where the callback ended.
	Phase RemoteLoginPhase
	// Failure is what stopped the code exchange or the credential's storing,
	// where it is known; unset otherwise.
	Failure RemoteLoginFailure
	// ProviderErrorCode is the registered OAuth error code the provider
	// answered with, unset when it sent none or one outside the registry.
	ProviderErrorCode string
}

// RemoteLoginFailure is what stopped a code exchange or storing its result,
// told apart where the error is built so a local fault is never read as the
// provider's.
type RemoteLoginFailure string

const (
	RemoteLoginFailureNone RemoteLoginFailure = ""
	// RemoteLoginFailureSigning: the gateway could not sign its client
	// assertion. Nothing was sent to the provider.
	RemoteLoginFailureSigning RemoteLoginFailure = "assertion_signing"
	// RemoteLoginFailureAssertionKey: the client's assertion key is not
	// configured, which an administrator repairs. Nothing was sent.
	RemoteLoginFailureAssertionKey RemoteLoginFailure = "assertion_key_unconfigured"
	// RemoteLoginFailureInternal: the request could not be built.
	RemoteLoginFailureInternal RemoteLoginFailure = "internal"
	// RemoteLoginFailureUnreachable: the token endpoint did not answer.
	RemoteLoginFailureUnreachable RemoteLoginFailure = "provider_unreachable"
	// RemoteLoginFailureTunnel: the MCP tunnel could not reach the provider.
	RemoteLoginFailureTunnel RemoteLoginFailure = "tunnel_unreachable"
	// RemoteLoginFailureProviderOrTunnel: a bare tunneled 502 cannot identify
	// whether the provider or the tunnel agent failed.
	RemoteLoginFailureProviderOrTunnel RemoteLoginFailure = "provider_or_tunnel"
	// RemoteLoginFailureRateLimited: the provider refused the exchange for now.
	RemoteLoginFailureRateLimited RemoteLoginFailure = "provider_rate_limited"
	// RemoteLoginFailureUnavailable: the provider answered 503 or
	// temporarily_unavailable.
	RemoteLoginFailureUnavailable RemoteLoginFailure = "provider_unavailable"
	// RemoteLoginFailureServerError: the provider answered another 5xx or
	// server_error.
	RemoteLoginFailureServerError RemoteLoginFailure = "provider_server_error"
	// RemoteLoginFailureInvalidResponse: the provider answered success with
	// a body the gateway could not use.
	RemoteLoginFailureInvalidResponse RemoteLoginFailure = "invalid_response"
	// RemoteLoginFailureRejected: the provider refused the exchange.
	RemoteLoginFailureRejected RemoteLoginFailure = "provider_rejected"
	// RemoteLoginFailureUnrenewableToken: the provider issued an access token
	// that is expired or about to expire and no refresh token.
	RemoteLoginFailureUnrenewableToken RemoteLoginFailure = "unrenewable_token" //nolint:gosec // G101 false positive: a reason label.
	// RemoteLoginFailureConnectionGone: the provider connection or sign-in
	// the login was started from was removed before it was stored.
	RemoteLoginFailureConnectionGone RemoteLoginFailure = "connection_changed"
	// RemoteLoginFailureLoginExpired: the login can no longer be stored and
	// must be started again.
	RemoteLoginFailureLoginExpired RemoteLoginFailure = "login_expired"
)

// exchangeFailureKind is the failure exchangeFailure reports, with the
// provider's registered error code when it sent one.
func exchangeFailureKind(err error, tunneled bool) (RemoteLoginFailure, string) {
	if _, ok := errors.AsType[*tokenEndpointSigningError](err); ok {
		if clientAssertionUnconfigured(err) {
			return RemoteLoginFailureAssertionKey, ""
		}
		return RemoteLoginFailureSigning, ""
	}
	if _, ok := errors.AsType[*tokenExchangeUnavailableError](err); ok {
		if errors.Is(err, tunnelrouting.ErrNoTunnelRoute) {
			return RemoteLoginFailureTunnel, ""
		}
		return RemoteLoginFailureUnreachable, ""
	}
	endpointErr, ok := errors.AsType[*tokenEndpointError](err)
	if !ok {
		return RemoteLoginFailureInternal, ""
	}
	code := ""
	if oauthErr, ok := errors.AsType[oautherr.RFC6749Error](err); ok {
		code = tokenEndpointErrorCode(oauthErr.Code)
	}
	registered := registeredErrorCode(code)
	switch {
	case tunneled && endpointErr.tunnelError != "":
		return RemoteLoginFailureTunnel, registered
	case tunneled && endpointErr.statusCode == http.StatusBadGateway && code == "":
		return RemoteLoginFailureProviderOrTunnel, registered
	case endpointErr.statusCode == http.StatusTooManyRequests:
		return RemoteLoginFailureRateLimited, registered
	case code == "" && endpointErr.statusCode/100 == 2:
		// A success status is only malformed when it carries no OAuth error.
		return RemoteLoginFailureInvalidResponse, ""
	}
	switch exchangeFailureCode(endpointErr.statusCode, code) {
	case oops.CodeUnavailable:
		return RemoteLoginFailureUnavailable, registered
	case oops.CodeGatewayError:
		return RemoteLoginFailureServerError, registered
	default:
		return RemoteLoginFailureRejected, registered
	}
}

// registeredErrorCode keeps an OAuth error code only when it is registered,
// so a provider's own string never becomes a dimension.
func registeredErrorCode(code string) string {
	if oautherr.IsIETFRegisteredCode(code) {
		return code
	}
	return ""
}
