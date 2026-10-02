package remotesessions

import (
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
