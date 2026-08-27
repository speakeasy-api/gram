// tokenservice_expiry_skew_test.go pins where AccessTokenExpirySkew applies on
// the lazy request path. The skew trades one refresh grant for a token that
// would otherwise be rejected upstream mid-request, so it only applies when
// there is a refresh grant to spend.

package remotesessions_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestResolveAccessToken_NoRefreshToken_InsideSkew_ServedUntilDeadline(t *testing.T) {
	t.Parallel()

	const upstreamAccessToken = "access-short-lived-no-refresh"
	expiresIn := strconv.Itoa(int((remotesessions.AccessTokenExpirySkew - 5*time.Second) / time.Second))
	ctx, env := newSyntheticExpiryEnv(t, "no-refresh-skew", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + upstreamAccessToken + `","token_type":"Bearer","expires_in":` + expiresIn + `}`))
	})

	require.True(t, env.session.AccessExpiresAt.Valid)
	require.False(t, env.session.RefreshTokenEncrypted.Valid, "no refresh token should be stored")
	require.True(t, env.session.AccessExpiresAt.Time.Before(time.Now().Add(remotesessions.AccessTokenExpirySkew)),
		"fixture must land inside the skew window")

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
