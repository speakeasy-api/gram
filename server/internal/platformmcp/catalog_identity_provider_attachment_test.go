package platformmcp

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

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

func TestIdentityProviderDiscoveryErrorTypesPerRegistrationSource(t *testing.T) {
	t.Parallel()

	cause := errors.New("no protected resource metadata")

	remoteErr := identityProviderDiscoveryError(remoteURLSourceKind, cause)
	require.ErrorIs(t, remoteErr, ErrIdentityProviderNotDiscovered, "a remote URL source with no discoverable metadata is a bounded fact, not an unsupported contract")
	require.ErrorIs(t, remoteErr, cause)
	require.NotErrorIs(t, remoteErr, ErrIdentityProviderAttachmentUnsupported)

	catalogErr := identityProviderDiscoveryError("catalog", cause)
	require.ErrorIs(t, catalogErr, ErrIdentityProviderAttachmentUnsupported)
	require.ErrorIs(t, catalogErr, cause)
	require.NotErrorIs(t, catalogErr, ErrIdentityProviderNotDiscovered)
}

func TestIdentityProviderDynamicRegistrationErrorTreatsTimeoutAndRateLimitAsRetryable(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError} {
		err := identityProviderDynamicRegistrationError(&remotesessions.DynamicClientRegistrationError{StatusCode: status})
		require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnavailable, status)
	}

	err := identityProviderDynamicRegistrationError(&remotesessions.DynamicClientRegistrationError{StatusCode: http.StatusBadRequest})
	require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
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
