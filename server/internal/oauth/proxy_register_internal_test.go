package oauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProxyRegisterRedirectURIs_RemoteSession(t *testing.T) {
	t.Parallel()

	uris, err := proxyRegisterRedirectURIs("https://gram.example.com", ProxyRegisterFlowRemoteSession)
	require.NoError(t, err)
	require.Equal(t, []string{"https://gram.example.com/mcp/remote_login_callback"}, uris)
}

func TestProxyRegisterRedirectURIs_OAuthProxy(t *testing.T) {
	t.Parallel()

	uris, err := proxyRegisterRedirectURIs("https://gram.example.com", ProxyRegisterFlowOAuthProxy)
	require.NoError(t, err)
	require.Equal(t, []string{"https://gram.example.com/oauth/callback"}, uris)
}

func TestProxyRegisterRedirectURIs_EmptyFlowRegistersSuperset(t *testing.T) {
	t.Parallel()

	uris, err := proxyRegisterRedirectURIs("https://gram.example.com", "")
	require.NoError(t, err)
	require.Equal(t, []string{
		"https://gram.example.com/oauth/callback",
		"https://gram.example.com/mcp/remote_login_callback",
		"https://gram.example.com/x/mcp/remote_login_callback",
	}, uris)
}

func TestProxyRegisterRedirectURIs_UnknownFlow(t *testing.T) {
	t.Parallel()

	_, err := proxyRegisterRedirectURIs("https://gram.example.com", "bogus")
	require.ErrorContains(t, err, `unknown flow "bogus"`)
}
