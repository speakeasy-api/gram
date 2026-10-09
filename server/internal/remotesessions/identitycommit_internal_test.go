package remotesessions

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestRegisteredAuthMethodRequiresConfidentialClient(t *testing.T) {
	t.Parallel()

	policy := RegistrationPolicy{Scope: nil, Audience: nil, TokenEndpointAuthMethod: conv.PtrEmpty(string(TokenEndpointAuthMethodBasic)), RequireClientSecret: true, AllowCIMD: false}
	accepted := func(response ProxyRegisterResponse) bool {
		_, ok := registeredAuthMethod(response, policy)
		return ok
	}

	require.True(t, accepted(ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: string(TokenEndpointAuthMethodBasic)}))
	require.True(t, accepted(ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret"}), "an omitted method defaults to the requested client_secret_basic")
	require.False(t, accepted(ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: string(TokenEndpointAuthMethodPost)}))
	require.False(t, accepted(ProxyRegisterResponse{ClientSecret: "secret"}))
	require.False(t, accepted(ProxyRegisterResponse{ClientID: "client"}))
	require.False(t, accepted(ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: string(TokenEndpointAuthMethodNone)}))
	require.False(t, accepted(ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: "private_key_jwt"}))
}

func TestRegisteredAuthMethodAcceptsPublicClientUnlessRequired(t *testing.T) {
	t.Parallel()

	policy := RegistrationPolicy{Scope: nil, Audience: nil, TokenEndpointAuthMethod: nil, RequireClientSecret: false, AllowCIMD: true}

	method, ok := registeredAuthMethod(ProxyRegisterResponse{ClientID: "client"}, policy)
	require.True(t, ok)
	require.Equal(t, string(TokenEndpointAuthMethodNone), method)

	method, ok = registeredAuthMethod(ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret"}, policy)
	require.True(t, ok)
	require.Equal(t, string(TokenEndpointAuthMethodBasic), method)

	_, ok = registeredAuthMethod(ProxyRegisterResponse{ClientID: "client", TokenEndpointAuthMethod: string(TokenEndpointAuthMethodPost)}, policy)
	require.False(t, ok, "a secret method without a secret is refused")
}

func committerAt(t *testing.T, serverURL string) *IdentityCommitter {
	t.Helper()
	u, err := url.Parse(serverURL)
	require.NoError(t, err)
	return &IdentityCommitter{serverURL: u}
}

func TestCIMDFetchableFromAPublicServerURL(t *testing.T) {
	t.Parallel()

	c := committerAt(t, "https://app.example.com")

	require.True(t, c.cimdFetchableBy("https://mcp.example.com"))
	require.True(t, c.cimdFetchableBy("http://127.0.0.1:35291"))
}

func TestCIMDNotFetchableByAPublicProviderFromAPrivateServerURL(t *testing.T) {
	t.Parallel()

	// A public provider resolves localhost to itself, or refuses private
	// addresses outright, so it can never read the document from these.
	for _, serverURL := range []string{
		"https://localhost:8080",
		"https://gram.localhost",
		"https://127.0.0.1:8080",
		"https://[::1]:8080",
		"https://10.0.0.5",
		"https://192.168.1.20:8443",
	} {
		require.False(t, committerAt(t, serverURL).cimdFetchableBy("https://mcp.example.com"), serverURL)
	}
}

func TestCIMDFetchableByAProviderOnThePrivateNetwork(t *testing.T) {
	t.Parallel()

	// The dev-idp harness and httptest providers run beside a local server, so
	// they can fetch its document.
	c := committerAt(t, "https://localhost:8080")

	require.True(t, c.cimdFetchableBy("http://127.0.0.1:35291"))
	require.True(t, c.cimdFetchableBy("http://localhost:35291/oauth"))
}

func TestCIMDFetchableWithNoServerURL(t *testing.T) {
	t.Parallel()

	c := &IdentityCommitter{serverURL: nil}

	require.True(t, c.cimdFetchableBy("https://mcp.example.com"))
}
