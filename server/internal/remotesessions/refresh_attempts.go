package remotesessions

import (
	"context"
	"errors"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

type connectionScopeKey struct{}

type connectionAttemptKey struct{}

// WithConnectionScope names the server a request reached, for any refresh it triggers.
func WithConnectionScope(ctx context.Context, scope mcpconnectionattempts.Scope) context.Context {
	return context.WithValue(ctx, connectionScopeKey{}, scope)
}

// WithConnectionAttempt makes a refresh a step of attempt instead of its own attempt.
func WithConnectionAttempt(ctx context.Context, attempt mcpconnectionattempts.Attempt) context.Context {
	return context.WithValue(ctx, connectionAttemptKey{}, attempt)
}

func connectionScopeFrom(ctx context.Context) mcpconnectionattempts.Scope {
	scope, _ := ctx.Value(connectionScopeKey{}).(mcpconnectionattempts.Scope)
	return scope
}

// recordRefresh writes one refresh's row. A carried attempt gets a step; otherwise the refresh is its own attempt.
func (s *RefreshService) recordRefresh(
	ctx context.Context,
	scope mcpconnectionattempts.Scope,
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
	if attempt, carried := ctx.Value(connectionAttemptKey{}).(mcpconnectionattempts.Attempt); carried && !attempt.IsZero() {
		s.connections.Step(ctx, attempt, mcpconnectionattempts.StepProviderRefresh, what, opts...)
		return
	}
	if scope == (mcpconnectionattempts.Scope{}) {
		// A platform client has no owner, so nothing says whose row this would be.
		if client.OwnerOrganizationID == "" {
			return
		}
		scope = mcpconnectionattempts.ProviderSessionScope(client.OwnerOrganizationID, client.OwnerProjectID.UUID, mcpconnectionattempts.AppUnverified)
	}
	s.connections.Once(ctx, scope, mcpconnectionattempts.StepProviderRefresh, what, opts...)
}

// refreshHappened maps a metric outcome to what the row says. A session revoked
// mid-refresh and a caller that went away are neither success nor failure.
func refreshHappened(outcome remotesessionmetrics.RefreshOutcome, client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow, err error) (mcpconnectionattempts.Happened, bool) {
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
		if client.IssuerRegistrationEndpoint.String != "" {
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
