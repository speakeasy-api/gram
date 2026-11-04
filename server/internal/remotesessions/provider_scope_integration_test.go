package remotesessions_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestProviderScopesUseIssuerBeforeTokenExchange(t *testing.T) {
	t.Parallel()
	var mutateIssuer func() error
	ctx, env := newSyntheticExpiryEnv(t, "issuer-snapshot", func(w http.ResponseWriter, _ *http.Request) {
		if mutateIssuer != nil {
			if err := mutateIssuer(); err != nil {
				t.Errorf("update issuer during exchange: %v", err)
				http.Error(w, "fixture failed", http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"scope-access","token_type":"Bearer","expires_in":3600,"scope":"channels:read,chat:write"}`))
	}, func(o *syntheticLoginOptions) { o.tokenResponseIssuer = "https://mcp.slack.com" })
	mutateIssuer = func() error {
		_, err := env.q.UpdateRemoteSessionIssuer(ctx, repo.UpdateRemoteSessionIssuerParams{ID: env.issuerID, ProjectID: conv.ToNullUUID(env.projectID), Issuer: conv.ToPGText("https://other.example.com")})
		if err != nil {
			return fmt.Errorf("update exchange issuer fixture: %w", err)
		}
		return nil
	}
	clients, err := env.mgr.ListClients(ctx, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	authURL, err := env.mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID: env.parentID, ProjectID: env.projectID, OrganizationID: env.organizationID,
		UserSessionIssuerID: env.session.UserSessionIssuerID, Subject: &env.subject,
		McpSlug: "synthetic-mcp-issuer-snapshot",
	}, clients[0])
	require.NoError(t, err)
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	_, err = env.callback(t, url.Values{"state": {parsed.Query().Get("state")}, "code": {"second-code"}}.Encode())
	require.NoError(t, err)
	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
	require.NoError(t, err)
	require.Equal(t, []string{"channels:read", "chat:write"}, session.Scopes)
}

func TestProviderScopesPersistThroughCallbackAndRefresh(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, issuer string
		want         []string
	}{
		{"slack", "https://mcp.slack.com", []string{"channels:read", "chat:write"}},
		{"generic", "https://idp.example.com", []string{"channels:read,chat:write"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, env := newSyntheticExpiryEnv(t, "provider-scopes-"+tc.name, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"scope-access","refresh_token":"scope-refresh","token_type":"Bearer","expires_in":3600,"scope":"channels:read,chat:write"}`))
			}, func(o *syntheticLoginOptions) { o.tokenResponseIssuer = tc.issuer })
			require.Equal(t, tc.want, env.session.Scopes)
			require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{ID: env.session.ID, ProjectID: conv.ToNullUUID(env.projectID), AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Hour))}))
			token, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
			require.NoError(t, err)
			require.Equal(t, "scope-access", token)
			refreshed, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{SubjectUrn: env.subject, RemoteSessionClientID: env.clientID})
			require.NoError(t, err)
			require.Equal(t, tc.want, refreshed.Scopes)
		})
	}
}
