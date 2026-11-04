package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/wire"

	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
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
		{name: "unanswered", outcome: remotesessionmetrics.RefreshOutcomeUnreachable, err: errors.New("post refresh")},
		{name: "gateway 502", outcome: remotesessionmetrics.RefreshOutcomeUpstreamError, err: &TokenRefreshError{statusCode: http.StatusBadGateway, tunnelError: wire.TunnelErrorNoRoute}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			what, ok := refreshHappened(tc.outcome, client, tc.err)
			require.True(t, ok)
			require.Equal(t, mcpconnectionattempts.CodeTunnelUnavailable, what)
		})
	}
}

// A 502 the tunnel forwarded came from the provider, and a busy tunnel is also a tunnel outage.
func TestRefreshHappened_BadGatewayFollowsTheTunnelStatus(t *testing.T) {
	t.Parallel()

	client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	for tunnelError, want := range map[string]mcpconnectionattempts.Code{
		"":                                mcpconnectionattempts.CodeProviderServerError,
		wire.TunnelErrorTunnelBusy:        mcpconnectionattempts.CodeTunnelUnavailable,
		wire.TunnelErrorRouteLookupFailed: mcpconnectionattempts.CodeInternalError,
	} {
		what, ok := refreshHappened(remotesessionmetrics.RefreshOutcomeUpstreamError, client, &TokenRefreshError{statusCode: http.StatusBadGateway, tunnelError: tunnelError})
		require.True(t, ok)
		require.Equal(t, want, what, tunnelError)
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
	for _, outcome := range []remotesessionmetrics.RefreshOutcome{remotesessionmetrics.RefreshOutcomeSessionInactive, remotesessionmetrics.RefreshOutcomeCanceled, remotesessionmetrics.RefreshOutcomeDeferred} {
		_, ok := refreshHappened(outcome, client, context.Canceled)
		require.False(t, ok, outcome)
	}
}

func TestRefreshHappened_LocalTunnelTransportIsInternal(t *testing.T) {
	t.Parallel()
	client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	for _, err := range []error{newTokenRefreshError("no transport", errRefreshTunnelTransport, refreshRemedyRetry), tunnelrouting.ErrTransportConfiguration} {
		what, ok := refreshHappened(refreshOutcomeForError(context.Background(), err), client, err)
		require.True(t, ok)
		require.Equal(t, mcpconnectionattempts.CodeInternalError, what)
		require.False(t, refreshOutagePaced(remotesessionmetrics.RefreshOutcomeInternalError, err))
	}
}

func TestRefreshHappened_TemporaryOAuthErrorsPreserveResponse(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"server_error", "temporarily_unavailable"} {
		for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusTooManyRequests} {
			err := newTokenRefreshErrorFromHTTP(status, http.StatusText(status), []byte(`{"error":"`+code+`"}`))
			outcome := refreshOutcomeForError(context.Background(), err)
			want := remotesessionmetrics.RefreshOutcomeRejected
			if status == http.StatusTooManyRequests {
				want = remotesessionmetrics.RefreshOutcomeRateLimited
			}
			require.Equal(t, want, outcome)
			require.Equal(t, status == http.StatusTooManyRequests, refreshOutagePaced(outcome, err))
		}
	}
}

func TestRefreshHappened_MissingTokenDoesNotStartCooldown(t *testing.T) {
	t.Parallel()
	err := newTokenRefreshError("no access token", errRefreshUnusableAnswer, refreshRemedyAdministrator)
	require.False(t, refreshOutagePaced(refreshOutcomeForError(context.Background(), err), err))
}

func TestRefreshHappened_TruncatedBodyPrecedesTunnelOutage(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("read refresh response: %w: %w: %w", errRefreshUpstreamUnreachable, errRefreshUnusableAnswer, io.ErrUnexpectedEOF)
	for _, tunneled := range []bool{false, true} {
		client := remotesessions_repo.GetRemoteSessionClientWithIssuerByIDRow{TunneledMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: tunneled}}
		outcome := refreshOutcomeForError(context.Background(), err)
		require.Equal(t, remotesessionmetrics.RefreshOutcomeUnreachable, outcome)
		require.ErrorIs(t, &RefreshError{Outcome: outcome, err: err}, ErrRemoteSessionUnavailable)
		what, ok := refreshHappened(outcome, client, err)
		require.True(t, ok)
		require.Equal(t, mcpconnectionattempts.CodeProviderServerError, what)
		for _, suppressed := range []remotesessionmetrics.RefreshOutcome{remotesessionmetrics.RefreshOutcomeCanceled, remotesessionmetrics.RefreshOutcomeDeferred, remotesessionmetrics.RefreshOutcomeSessionInactive} {
			_, ok := refreshHappened(suppressed, client, err)
			require.False(t, ok)
		}
	}
}
