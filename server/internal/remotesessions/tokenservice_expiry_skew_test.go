// tokenservice_expiry_skew_test.go pins where AccessTokenExpirySkew applies on
// the lazy request path. The skew trades one refresh grant for a token that
// would otherwise be rejected upstream mid-request, so it only applies when
// there is a refresh grant to spend.

package remotesessions_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestResolveAccessToken_NoRefreshToken_InsideSkew_ServedUntilDeadline(t *testing.T) {
	t.Parallel()

	const upstreamAccessToken = "access-no-refresh"
	ctx, env := newSyntheticExpiryEnv(t, "no-refresh-skew", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + upstreamAccessToken + `","token_type":"Bearer","expires_in":3600}`))
	})
	require.False(t, env.session.RefreshTokenEncrypted.Valid, "no refresh token should be stored")

	// Stand in for the stored token reaching the skew window.
	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(remotesessions.AccessTokenExpirySkew / 2)),
	}))

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, upstreamAccessToken, resolved,
		"with no refresh grant the token is forwarded until its stated deadline, not turned into a reconnect prompt early")

	// The consent UI reports the same window: connected, nothing to refresh.
	states, err := env.mgr.RemoteSessionStatuses(ctx, env.subject, env.projectID, env.organizationID, env.session.UserSessionIssuerID)
	require.NoError(t, err)
	require.Equal(t, remotesessions.RemoteSessionActive, states[env.clientID].Status)
	require.False(t, states[env.clientID].CanRefresh)
}

func TestResolveAccessToken_RefreshFailsInsideSkew_ServesStoredTokenUntilDeadline(t *testing.T) {
	t.Parallel()

	const upstreamAccessToken = "access-refresh-refused"
	var refreshAttempts atomic.Int32
	ctx, env := newSyntheticExpiryEnv(t, "refresh-fails-inside-skew", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshAttempts.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + upstreamAccessToken + `","refresh_token":"refresh-initial","token_type":"Bearer","expires_in":3600}`))
	})

	// Inside the window the request path spends the refresh grant first. The
	// upstream refusing it must not cost the caller a token that still works.
	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(remotesessions.AccessTokenExpirySkew / 2)),
	}))

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Equal(t, upstreamAccessToken, resolved,
		"a failed early refresh must fall back to the stored token while it is inside its stated lifetime")
	require.EqualValues(t, 1, refreshAttempts.Load(), "the refresh must still be attempted first")
}

func TestResolveAccessToken_RefreshFailsPastDeadline_IsUnusable(t *testing.T) {
	t.Parallel()

	var refreshAttempts atomic.Int32
	ctx, env := newSyntheticExpiryEnv(t, "refresh-fails-past-deadline", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			refreshAttempts.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-expired","refresh_token":"refresh-initial","token_type":"Bearer","expires_in":3600}`))
	})

	require.NoError(t, env.q.SetRemoteSessionAccessExpiresAt(ctx, repo.SetRemoteSessionAccessExpiresAtParams{
		ID:              env.session.ID,
		ProjectID:       conv.ToNullUUID(env.projectID),
		AccessExpiresAt: conv.ToPGTimestamptz(time.Now().Add(-time.Minute)),
	}))

	resolved, err := env.mgr.ResolveAccessToken(ctx, env.clientID, env.subject, "")
	require.NoError(t, err)
	require.Empty(t, resolved, "past the deadline there is no token to fall back to")
	require.EqualValues(t, 1, refreshAttempts.Load())
}
