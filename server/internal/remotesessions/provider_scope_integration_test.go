package remotesessions_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

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
