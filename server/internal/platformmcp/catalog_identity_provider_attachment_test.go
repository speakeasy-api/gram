package platformmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

func TestCatalogIssuerIdentityIsExact(t *testing.T) {
	t.Parallel()
	require.True(t, sameIssuerURL("https://issuer.example/tenant/", "https://issuer.example/tenant/"))
	require.False(t, sameIssuerURL("https://issuer.example/tenant/", "https://issuer.example/tenant"))
	require.False(t, sameIssuerURL("https://issuer.example/tenant", "https://issuer.example/tenant/"))
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 server.URL + "/tenant/",
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"registration_endpoint":  "https://issuer.example/register",
		}))
	}))
	defer server.Close()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	service := &CatalogIdentityProviderAttachmentService{policy: policy}
	metadata, err := service.discoverSupportedIssuerMetadata(t.Context(), []string{server.URL + "/tenant/"})
	require.NoError(t, err)
	require.Equal(t, server.URL+"/tenant/", metadata.Issuer)
	for _, issuer := range []string{server.URL + "/tenant", " " + server.URL + "/tenant/ "} {
		_, err := service.discoverSupportedIssuerMetadata(t.Context(), []string{issuer})
		require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
	}
}
