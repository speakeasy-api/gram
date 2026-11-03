package remotesessions

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestRefreshHappened_InvalidClientFollowsWhoCanRepairIt(t *testing.T) {
	t.Parallel()

	withEndpoint := func(change func(*remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow)) remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow {
		client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{IssuerRegistrationEndpoint: pgtype.Text{String: "https://idp.example.com/register", Valid: true}}
		change(&client)
		return client
	}
	for _, tc := range []struct {
		name   string
		client remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow
		want   mcpconnectionattempts.Code
	}{
		{name: "dynamic", client: withEndpoint(func(*remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {}), want: mcpconnectionattempts.CodeRefreshInvalidClient},
		{name: "static", client: remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{}, want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "cimd", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.ClientIDMetadataUri = pgtype.Text{String: "https://client.example.com/metadata", Valid: true}
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "managed", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.IdentityProviderConnectionID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "private key", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.TokenEndpointAuthMethod = pgtype.Text{String: string(TokenEndpointAuthMethodPrivateKeyJWT), Valid: true}
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "active managed binding", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.HasActiveEmaBinding = true
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
		{name: "self", client: withEndpoint(func(c *remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow) {
			c.CredentialOwner = string(CredentialOwnerSelf)
		}), want: mcpconnectionattempts.CodeProviderOAuthInvalidClient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			what, ok := refreshHappened(remotesessionmetrics.RefreshOutcomeInvalidClient, tc.client, nil)
			require.True(t, ok)
			require.Equal(t, tc.want, what)
		})
	}
}

func TestRefreshHappened_DownTunnelIsTheTunnel(t *testing.T) {
	t.Parallel()

	client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	for _, tc := range []struct {
		name    string
		outcome remotesessionmetrics.RefreshOutcome
		err     error
	}{
		{name: "no transport", outcome: remotesessionmetrics.RefreshOutcomeInternalError, err: newTokenRefreshError("no transport", errRefreshTunnelTransport, refreshRemedyRetry)},
		{name: "unanswered", outcome: remotesessionmetrics.RefreshOutcomeUnreachable, err: errors.New("post refresh")},
		{name: "gateway 502", outcome: remotesessionmetrics.RefreshOutcomeUpstreamError, err: &TokenRefreshError{statusCode: http.StatusBadGateway}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			what, ok := refreshHappened(tc.outcome, client, tc.err)
			require.True(t, ok)
			require.Equal(t, mcpconnectionattempts.CodeTunnelUnavailable, what)
		})
	}
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

// One member's outage must not read as another's on the same issuer.
func TestSkippedClients_DoNotLeakBetweenClients(t *testing.T) {
	t.Parallel()

	projectA, projectB, issuer, clientA, clientB := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	skipped := new(SkippedClients)
	skipped.Note(projectA, issuer, clientA, ErrRemoteSessionUnavailable)

	none := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	require.ErrorIs(t, skipped.Reason(projectA, issuer, none), ErrRemoteSessionUnavailable)
	require.NoError(t, skipped.Reason(projectB, issuer, none), "another project's member")
	require.NoError(t, skipped.Reason(projectA, issuer, uuid.NullUUID{UUID: clientB, Valid: true}), "another chosen client")
	require.ErrorIs(t, skipped.Reason(projectA, issuer, uuid.NullUUID{UUID: clientA, Valid: true}), ErrRemoteSessionUnavailable)
}
