package remotesessions

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestRefreshHappened_InvalidClientFollowsWhoCanRepairIt(t *testing.T) {
	t.Parallel()

	withEndpoint := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{IssuerRegistrationEndpoint: pgtype.Text{String: "https://idp.example.com/register", Valid: true}}
	what, ok := refreshHappened(remotesessionmetrics.RefreshOutcomeInvalidClient, withEndpoint, nil)
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeRefreshInvalidClient, what)

	what, ok = refreshHappened(remotesessionmetrics.RefreshOutcomeInvalidClient, remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{}, nil)
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.CodeProviderOAuthInvalidClient, what)
}

func TestRefreshHappened_InternalErrorFollowsTheRemedy(t *testing.T) {
	t.Parallel()

	var client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow
	for _, tc := range []struct {
		name string
		err  error
		want mcpconnectionattempts.Code
	}{
		{name: "administrator", err: newTokenRefreshError("no token endpoint", nil, refreshRemedyAdministrator), want: mcpconnectionattempts.CodeCredentialMisconfigured},
		{name: "reconnect", err: newTokenRefreshError("unreadable token", nil, refreshRemedyReconnect), want: mcpconnectionattempts.CodeUpstreamReconnectRequired},
		{name: "retry", err: newTokenRefreshError("lost the race", nil, refreshRemedyRetry), want: mcpconnectionattempts.CodeInternalError},
		{name: "plain", err: errors.New("database"), want: mcpconnectionattempts.CodeInternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			what, ok := refreshHappened(remotesessionmetrics.RefreshOutcomeInternalError, client, tc.err)
			require.True(t, ok)
			require.Equal(t, tc.want, what)
		})
	}
}

func TestRefreshHappened_NoRow(t *testing.T) {
	t.Parallel()

	var client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow
	for _, outcome := range []remotesessionmetrics.RefreshOutcome{remotesessionmetrics.RefreshOutcomeSessionInactive, remotesessionmetrics.RefreshOutcomeCanceled} {
		_, ok := refreshHappened(outcome, client, context.Canceled)
		require.False(t, ok, outcome)
	}
}
