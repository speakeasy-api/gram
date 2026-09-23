package platformmcp

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestValidDynamicClientRegistrationEndpoint(t *testing.T) {
	t.Parallel()

	require.True(t, validDynamicClientRegistrationEndpoint("https://identity.example/register"))
	require.False(t, validDynamicClientRegistrationEndpoint("http://localhost/register"))
	require.False(t, validDynamicClientRegistrationEndpoint(""))
	require.False(t, validDynamicClientRegistrationEndpoint("not a URL"))
	require.False(t, validDynamicClientRegistrationEndpoint("ftp://identity.example/register"))
	require.False(t, validDynamicClientRegistrationEndpoint("https:///register"))
	require.False(t, validDynamicClientRegistrationEndpoint("https://user:password@identity.example/register"))
}

func TestDiscoverSupportedIssuerMetadataRejectsEmptyCandidates(t *testing.T) {
	t.Parallel()

	service := &CatalogIdentityProviderAttachmentService{}
	_, err := service.discoverSupportedIssuerMetadata(t.Context(), []string{"", "  "})

	require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
}

func TestIdentityProviderDynamicRegistrationErrorTreatsTimeoutAndRateLimitAsRetryable(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError} {
		err := identityProviderDynamicRegistrationError(&registration.HTTPError{StatusCode: status})
		require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnavailable, status)
	}

	err := identityProviderDynamicRegistrationError(&registration.HTTPError{StatusCode: http.StatusBadRequest, ProviderMessage: "provider-controlled detail"})
	require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
	require.NotContains(t, err.Error(), "provider-controlled detail")
}

// A caller hanging up mid-registration is not an attachment outcome. Reporting
// it as unavailable would blame the provider for something it never did.
func TestIdentityProviderDynamicRegistrationErrorPreservesCallerCancellation(t *testing.T) {
	t.Parallel()

	err := identityProviderDynamicRegistrationError(context.Canceled)

	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrIdentityProviderAttachmentUnavailable)
	require.NotErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
}

func TestValidBrowserCatalogDynamicClientRequiresConfidentialClient(t *testing.T) {
	t.Parallel()

	require.True(t, validBrowserCatalogDynamicClient(remotesessions.ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: string(remotesessions.TokenEndpointAuthMethodBasic)}))
	require.True(t, validBrowserCatalogDynamicClient(remotesessions.ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret"}), "RFC 7591 defaults an omitted method to client_secret_basic")
	require.False(t, validBrowserCatalogDynamicClient(remotesessions.ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: string(remotesessions.TokenEndpointAuthMethodPost)}))
	require.False(t, validBrowserCatalogDynamicClient(remotesessions.ProxyRegisterResponse{ClientSecret: "secret"}))
	require.False(t, validBrowserCatalogDynamicClient(remotesessions.ProxyRegisterResponse{ClientID: "client"}))
	require.False(t, validBrowserCatalogDynamicClient(remotesessions.ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: string(remotesessions.TokenEndpointAuthMethodNone)}))
	require.False(t, validBrowserCatalogDynamicClient(remotesessions.ProxyRegisterResponse{ClientID: "client", ClientSecret: "secret", TokenEndpointAuthMethod: "private_key_jwt"}))
}
