package remotesessions_test

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestValidateLoopbackRedirectURI_AcceptsSupportedHosts(t *testing.T) {
	t.Parallel()

	valid := []string{
		"http://localhost:49152/callback",
		"http://127.0.0.1:49153/callback",
		"http://[::1]:49154/callback",
		"http://localhost:49155/callback?agent_state=opaque",
	}
	for _, candidate := range valid {
		require.NoError(t, remotesessions.ValidateLoopbackRedirectURI(candidate), candidate)
	}
}

func TestValidateLoopbackRedirectURI_RejectsUnsafeTargets(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"https://localhost:49152/callback",
		"http://example.com:49152/callback",
		"http://localhost/callback",
		"http://user@localhost:49152/callback",
		"http://localhost:49152/callback#fragment",
		"http://localhost:0/callback",
		"http://localhost:65536/callback",
		"/callback",
	}
	for _, candidate := range invalid {
		require.Error(t, remotesessions.ValidateLoopbackRedirectURI(candidate), candidate)
	}
}

func TestRemoteLoginDance_LoopbackRedirectOnAuthorizeAndExchange(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "", "loopback", &spy)
	const callbackURI = "http://localhost:49152/callback"
	fx.parent.RemoteOAuthRedirectURI = callbackURI

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, callbackURI, parsed.Query().Get("redirect_uri"))

	// Simulate the local agent relaying the upstream code+state to Gram.
	runCallback(t, ctx, fx, authURL)
	require.NoError(t, spy.handlerErr)
	require.Equal(t, callbackURI, spy.form.Get("redirect_uri"))
}

func TestRemoteLoginDance_HostedCallbackRemainsDefault(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "", "hosted-default", &spy)

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, "http://localhost/mcp/remote_login_callback", parsed.Query().Get("redirect_uri"))
}

func TestRemoteLoginDance_LegacyClientRejectsLoopbackRedirect(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "", "legacy-loopback", &spy)
	fx.parent.RemoteOAuthRedirectURI = "http://localhost:49152/callback"
	fx.clients[0].LegacyCallbackUrl = true

	_, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
	require.ErrorContains(t, err, "legacy remote session clients do not support loopback redirects")
}
