package remotesessions

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// recordRefresh writes one refresh's row. A carried attempt gets a step; otherwise the refresh is its own attempt.
// It reports whether a failed row was accepted, as its own attempt or as a step.
func (s *RefreshService) recordRefresh(
	ctx context.Context,
	attribution RefreshAttribution,
	sess remotesessions_repo.RemoteSession,
	client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow,
	trigger remotesessionmetrics.RefreshTrigger,
	outcome remotesessionmetrics.RefreshOutcome,
	err error,
) (standalone, step bool) {
	what, ok := refreshHappened(outcome, client, err)
	if !ok {
		return false, false
	}
	if what == mcpconnectionattempts.CodeRefreshInvalidClient {
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshReleaseTimeout)
		defer cancel()
		managed, lookupErr := remotesessions_repo.New(s.db).HasActiveEMABindingForRefresh(lookupCtx, remotesessions_repo.HasActiveEMABindingForRefreshParams{
			RemoteSessionClientID: uuid.NullUUID{UUID: sess.RemoteSessionClientID, Valid: true},
			UserSessionIssuerID:   sess.UserSessionIssuerID,
		})
		// Do not recommend re-registration unless the tenant's binding is known.
		if lookupErr != nil || managed {
			what = mcpconnectionattempts.CodeProviderOAuthInvalidClient
		}
	}
	opts := []mcpconnectionattempts.Option{
		mcpconnectionattempts.Provider(client.IssuerUrl),
		mcpconnectionattempts.RemoteSession(sess.ID),
		mcpconnectionattempts.RemoteSessionClient(sess.RemoteSessionClientID),
		mcpconnectionattempts.RemoteSessionIssuer(client.RemoteSessionIssuerID),
		mcpconnectionattempts.UserSessionIssuer(sess.UserSessionIssuerID),
		mcpconnectionattempts.Subject(&sess.SubjectUrn),
		mcpconnectionattempts.Detail(string(trigger)),
		attribution.client,
	}
	_, failed := what.(mcpconnectionattempts.Code)
	if !attribution.attempt.IsZero() {
		accepted := s.connections.Step(ctx, attribution.attempt, mcpconnectionattempts.StepProviderRefresh, what, opts...)
		return false, accepted && failed
	}
	accepted := s.connections.Once(ctx, attribution.scope, mcpconnectionattempts.StepProviderRefresh, what, opts...)
	return accepted && failed, false
}

// canReregister reports whether dynamic registration can reproduce this client.
func canReregister(clientIDMetadataURI pgtype.Text, identityProviderConnectionID uuid.NullUUID, authMethod pgtype.Text, owner string) bool {
	return !clientIDMetadataURI.Valid && !identityProviderConnectionID.Valid &&
		TokenEndpointAuthMethod(authMethod.String) != TokenEndpointAuthMethodPrivateKeyJWT &&
		CredentialOwner(owner) != CredentialOwnerSelf
}

// refreshHappened maps a metric outcome to the row; revoked mid-refresh and canceled write none.
func refreshHappened(outcome remotesessionmetrics.RefreshOutcome, client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow, err error) (mcpconnectionattempts.Happened, bool) {
	// A response body that was cut off or could not be decoded is a provider failure,
	// even through a tunnel. Do not override canceled/deferred/no-row outcomes.
	if (outcome == remotesessionmetrics.RefreshOutcomeUnreachable || outcome == remotesessionmetrics.RefreshOutcomeInternalError) && errors.Is(err, errRefreshUnusableAnswer) {
		return mcpconnectionattempts.CodeProviderServerError, true
	}
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
		if client.IssuerRegistrationEndpoint.String != "" && canReregister(client.ClientIDMetadataUri, client.IdentityProviderConnectionID, client.TokenEndpointAuthMethod, client.CredentialOwner) {
			return mcpconnectionattempts.CodeRefreshInvalidClient, true
		}
		return mcpconnectionattempts.CodeProviderOAuthInvalidClient, true
	case remotesessionmetrics.RefreshOutcomeRejected, remotesessionmetrics.RefreshOutcomeRejectedUnparsed:
		// Name provider failures without changing the response-facing refresh outcome.
		if tokenErr, ok := errors.AsType[*TokenRefreshError](err); ok {
			if tokenErr.statusCode == http.StatusRequestTimeout {
				return mcpconnectionattempts.CodeProviderUnreachable, true
			}
			switch tokenErr.code {
			case oautherr.OIDCCodeLoginRequired, oautherr.OIDCCodeInteractionRequired, oautherr.OIDCCodeConsentRequired, oautherr.OIDCCodeAccountSelectionRequired:
				return mcpconnectionattempts.CodeUpstreamReconnectRequired, true
			case oautherr.CodeServerError:
				return mcpconnectionattempts.CodeProviderServerError, true
			case oautherr.CodeTemporarilyUnavailable:
				return mcpconnectionattempts.CodeProviderUnavailable, true
			}
		}
		return mcpconnectionattempts.CodeRefreshRejected, true
	case remotesessionmetrics.RefreshOutcomeUpstreamError:
		if tokenErr, ok := errors.AsType[*TokenRefreshError](err); ok && tokenErr.code == oautherr.CodeTemporarilyUnavailable {
			return mcpconnectionattempts.CodeProviderUnavailable, true
		}
		return mcpconnectionattempts.CodeProviderServerError, true
	case remotesessionmetrics.RefreshOutcomeRateLimited:
		return mcpconnectionattempts.CodeProviderRateLimited, true
	case remotesessionmetrics.RefreshOutcomeUnreachable:
		return mcpconnectionattempts.CodeProviderUnreachable, true
	case remotesessionmetrics.RefreshOutcomeInternalError:
		if errors.Is(err, errStoredTokenUnreadable) {
			return mcpconnectionattempts.CodeStoredSignInUnreadable, true
		}
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
	case remotesessionmetrics.RefreshOutcomeSessionInactive, remotesessionmetrics.RefreshOutcomeCanceled, remotesessionmetrics.RefreshOutcomeDeferred:
		return nil, false
	}
	return nil, false
}

// refreshTunnelFailure names a refresh the tunnel stopped. A 502 the tunnel forwarded came from the provider.
func refreshTunnelFailure(outcome remotesessionmetrics.RefreshOutcome, err error) (mcpconnectionattempts.Code, bool) {
	if errors.Is(err, errRefreshTunnelTransport) || errors.Is(err, tunnelrouting.ErrTransportConfiguration) {
		return mcpconnectionattempts.CodeInternalError, true
	}
	if outcome == remotesessionmetrics.RefreshOutcomeUnreachable {
		if code, ok := tunnelrouting.FailureHappened("", err); ok {
			return code, true
		}
		return mcpconnectionattempts.CodeTunnelUnavailable, true
	}
	if tokenErr, ok := errors.AsType[*TokenRefreshError](err); ok && tokenErr.statusCode == http.StatusBadGateway {
		return tunnelrouting.FailureHappened(tokenErr.tunnelError)
	}
	var none mcpconnectionattempts.Code
	return none, false
}
