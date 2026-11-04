package remotesessions

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

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
		mcpconnectionattempts.FromRequest(attribution.request),
	}
	if !attribution.attempt.IsZero() {
		s.connections.Step(ctx, attribution.attempt, mcpconnectionattempts.StepProviderRefresh, what, opts...)
		return
	}
	s.connections.Once(ctx, attribution.scope, mcpconnectionattempts.StepProviderRefresh, what, opts...)
}

// canReregister reports whether dynamic registration can reproduce this client.
func canReregister(clientIDMetadataURI pgtype.Text, identityProviderConnectionID uuid.NullUUID, authMethod pgtype.Text, owner string) bool {
	return !clientIDMetadataURI.Valid && !identityProviderConnectionID.Valid &&
		TokenEndpointAuthMethod(authMethod.String) != TokenEndpointAuthMethodPrivateKeyJWT &&
		CredentialOwner(owner) != CredentialOwnerSelf
}

// refreshHappened maps a metric outcome to the row; revoked mid-refresh and canceled write none.
func refreshHappened(outcome remotesessionmetrics.RefreshOutcome, client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow, err error) (mcpconnectionattempts.Happened, bool) {
	if client.TunneledMcpServerID.Valid {
		if code, ok := refreshTunnelFailure(outcome, err); ok {
			return code, true
		}
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
		if client.IssuerRegistrationEndpoint.String != "" && !client.HasActiveEmaBinding && canReregister(client.ClientIDMetadataUri, client.IdentityProviderConnectionID, client.TokenEndpointAuthMethod, client.CredentialOwner) {
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

// refreshTunnelFailure names a refresh the tunnel stopped. A 502 the tunnel forwarded came from the provider.
func refreshTunnelFailure(outcome remotesessionmetrics.RefreshOutcome, err error) (mcpconnectionattempts.Code, bool) {
	if errors.Is(err, errRefreshTunnelTransport) || outcome == remotesessionmetrics.RefreshOutcomeUnreachable {
		return mcpconnectionattempts.CodeTunnelUnavailable, true
	}
	if tokenErr, ok := errors.AsType[*TokenRefreshError](err); ok && tokenErr.statusCode == http.StatusBadGateway {
		return tunnelrouting.FailureHappened(tokenErr.tunnelError)
	}
	var none mcpconnectionattempts.Code
	return none, false
}
