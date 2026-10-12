package remotesessions

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// A legacy_callback_url client's upstream redirects to /oauth/callback; the
// shim must forward the query string verbatim to /mcp/remote_login_callback so
// the remote-session flow can finish the exchange on the pinned outbound
// origin, even after the server URL moves. The forwarder only reads the
// callback origins, so a bare ChallengeManager is enough.
func TestHandleLegacyProxyCallbackForwardsToRemoteLogin(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://moved.example.com")
	require.NoError(t, err)
	outbound, err := url.Parse("https://api.example.com")
	require.NoError(t, err)
	m := &ChallengeManager{serverURL: serverURL, origins: CallbackOrigins{Outbound: outbound, Registration: nil}}

	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=abc123&code=xyz789", nil)
	rec := httptest.NewRecorder()

	require.NoError(t, m.HandleLegacyProxyCallback(rec, req))

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t,
		"https://api.example.com/mcp/remote_login_callback?state=abc123&code=xyz789",
		rec.Header().Get("Location"))
}

// Upstream errors ride /oauth/callback too and must reach the remote-login
// handler unchanged so it can surface the failure.
func TestHandleLegacyProxyCallbackForwardsError(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://api.example.com")
	require.NoError(t, err)
	m := &ChallengeManager{serverURL: serverURL, origins: DefaultCallbackOrigins(serverURL)}

	req := httptest.NewRequest(http.MethodGet,
		"/oauth/callback?state=s1&error=access_denied&error_description=nope", nil)
	rec := httptest.NewRecorder()

	require.NoError(t, m.HandleLegacyProxyCallback(rec, req))

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t,
		"https://api.example.com/mcp/remote_login_callback?state=s1&error=access_denied&error_description=nope",
		rec.Header().Get("Location"))
}
