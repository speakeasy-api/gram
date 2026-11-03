package remotesessions

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// RefreshAttribution says where a refresh's connection row goes. Every caller
// that can trigger a refresh names it, so no path files a row by accident.
type RefreshAttribution struct {
	scope   mcpconnectionattempts.Scope
	attempt mcpconnectionattempts.Attempt
	skipped *SkippedClients
}

// RefreshIn files a refresh as its own attempt under scope.
func RefreshIn(scope mcpconnectionattempts.Scope) RefreshAttribution {
	return RefreshAttribution{scope: scope, attempt: mcpconnectionattempts.NoAttempt, skipped: nil}
}

// InAttempt makes the refresh a step of attempt. A zero attempt changes nothing.
func (a RefreshAttribution) InAttempt(attempt mcpconnectionattempts.Attempt) RefreshAttribution {
	a.attempt = attempt
	return a
}

// NotingSkips collects why partial resolution left a client out.
func (a RefreshAttribution) NotingSkips(skipped *SkippedClients) RefreshAttribution {
	a.skipped = skipped
	return a
}

// SkippedClients holds why clients were left out of a partial resolution, keyed
// by remote session issuer. It sits beside the token map because an entry in that
// map changes how members route. Nil ignores everything.
type SkippedClients struct {
	mu      sync.Mutex
	reasons map[uuid.UUID]error
}

// Note records why the issuer's client was left out.
func (s *SkippedClients) Note(issuerID uuid.UUID, reason error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reasons == nil {
		s.reasons = map[uuid.UUID]error{}
	}
	s.reasons[issuerID] = reason
}

// Reason is why the issuer's client was left out, or nil.
func (s *SkippedClients) Reason(issuerID uuid.UUID) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reasons[issuerID]
}

// recordRefresh writes one refresh's row. A carried attempt gets a step; otherwise the refresh is its own attempt.
func (s *RefreshService) recordRefresh(
	ctx context.Context,
	attribution RefreshAttribution,
	sess remotesessions_repo.RemoteSession,
	client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow,
	trigger remotesessionmetrics.RefreshTrigger,
	outcome remotesessionmetrics.RefreshOutcome,
	err error,
) {
	what, ok := refreshHappened(outcome, client, err)
	if !ok {
		return
	}
	opts := []mcpconnectionattempts.Option{
		mcpconnectionattempts.Provider(client.IssuerUrl),
		mcpconnectionattempts.RemoteSession(sess.ID),
		mcpconnectionattempts.RemoteSessionClient(sess.RemoteSessionClientID),
		mcpconnectionattempts.RemoteSessionIssuer(client.RemoteSessionIssuerID),
		mcpconnectionattempts.UserSessionIssuer(sess.UserSessionIssuerID),
		mcpconnectionattempts.Subject(&sess.SubjectUrn),
		mcpconnectionattempts.Detail(string(trigger)),
	}
	if !attribution.attempt.IsZero() {
		s.connections.Step(ctx, attribution.attempt, mcpconnectionattempts.StepProviderRefresh, what, opts...)
		return
	}
	s.connections.Once(ctx, attribution.scope, mcpconnectionattempts.StepProviderRefresh, what, opts...)
}

// canReregister reports whether a registration can be replaced by dynamic
// registration: a CIMD client's client_id is its document URL, a managed client's
// registration belongs to its connection, a private_key_jwt client's is bound to a
// key set, and a self client's uses a grant the replacement does not request.
func canReregister(clientIDMetadataURI pgtype.Text, identityProviderConnectionID uuid.NullUUID, authMethod pgtype.Text, owner string) bool {
	return !clientIDMetadataURI.Valid && !identityProviderConnectionID.Valid &&
		TokenEndpointAuthMethod(authMethod.String) != TokenEndpointAuthMethodPrivateKeyJWT &&
		CredentialOwner(owner) != CredentialOwnerSelf
}

// refreshHappened maps a metric outcome to what the row says. A session revoked
// mid-refresh and a caller that went away are neither success nor failure.
func refreshHappened(outcome remotesessionmetrics.RefreshOutcome, client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow, err error) (mcpconnectionattempts.Happened, bool) {
	if client.TunneledMcpServerID.Valid && refreshReachedNoTunnel(outcome, err) {
		return mcpconnectionattempts.CodeTunnelUnavailable, true
	}
	switch outcome {
	case remotesessionmetrics.RefreshOutcomeRefreshed:
		return mcpconnectionattempts.ResultRefreshed, true
	case remotesessionmetrics.RefreshOutcomeAdoptedConcurrentWinner:
		return mcpconnectionattempts.ResultAdoptedConcurrentWinner, true
	case remotesessionmetrics.RefreshOutcomeNoGrant:
		return mcpconnectionattempts.CodeNoGrant, true
	case remotesessionmetrics.RefreshOutcomeInvalidGrant:
		return mcpconnectionattempts.CodeInvalidGrant, true
	case remotesessionmetrics.RefreshOutcomeInvalidClient:
		// Only a client that can re-register is repaired by the person reconnecting.
		if client.IssuerRegistrationEndpoint.String != "" && canReregister(client.ClientIDMetadataUri, client.IdentityProviderConnectionID, client.TokenEndpointAuthMethod, client.CredentialOwner) {
			return mcpconnectionattempts.CodeRefreshInvalidClient, true
		}
		return mcpconnectionattempts.CodeProviderOAuthInvalidClient, true
	case remotesessionmetrics.RefreshOutcomeRejected, remotesessionmetrics.RefreshOutcomeRejectedUnparsed:
		return mcpconnectionattempts.CodeRefreshRejected, true
	case remotesessionmetrics.RefreshOutcomeUpstreamError:
		return mcpconnectionattempts.CodeProviderServerError, true
	case remotesessionmetrics.RefreshOutcomeRateLimited:
		return mcpconnectionattempts.CodeProviderRateLimited, true
	case remotesessionmetrics.RefreshOutcomeUnreachable:
		return mcpconnectionattempts.CodeProviderUnreachable, true
	case remotesessionmetrics.RefreshOutcomeInternalError:
		if tokenErr, ok := errors.AsType[*TokenRefreshError](err); ok {
			switch tokenErr.remedy {
			case refreshRemedyAdministrator:
				return mcpconnectionattempts.CodeCredentialMisconfigured, true
			case refreshRemedyReconnect:
				return mcpconnectionattempts.CodeUpstreamReconnectRequired, true
			case refreshRemedyRetry, refreshRemedyUnset:
			}
		}
		return mcpconnectionattempts.CodeInternalError, true
	case remotesessionmetrics.RefreshOutcomeSessionInactive, remotesessionmetrics.RefreshOutcomeCanceled:
		return nil, false
	}
	return nil, false
}

// refreshReachedNoTunnel reports a refresh through a tunnel that is down: no
// transport for it, the POST unanswered, or the tunnel gateway's 502.
func refreshReachedNoTunnel(outcome remotesessionmetrics.RefreshOutcome, err error) bool {
	if errors.Is(err, errRefreshTunnelTransport) || outcome == remotesessionmetrics.RefreshOutcomeUnreachable {
		return true
	}
	tokenErr, ok := errors.AsType[*TokenRefreshError](err)
	return ok && tokenErr.statusCode == http.StatusBadGateway
}
