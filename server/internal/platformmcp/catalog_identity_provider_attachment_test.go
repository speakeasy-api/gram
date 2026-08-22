package platformmcp

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
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

	notPublished := &wellknown.ProtectedResourceDiscoveryError{ProbeURL: "https://remote.example.test/.well-known/oauth-protected-resource", Status: http.StatusNotFound}
	require.Equal(t, "not_found", notPublished.Code())

	remoteErr := identityProviderDiscoveryError(remoteURLSourceKind, notPublished)
	require.ErrorIs(t, remoteErr, ErrIdentityProviderNotDiscovered, "a remote URL source deliberately publishing no metadata is a bounded fact, not an unsupported contract")
	require.ErrorIs(t, remoteErr, notPublished)
	require.NotErrorIs(t, remoteErr, ErrIdentityProviderAttachmentUnsupported)
	require.NotErrorIs(t, remoteErr, ErrIdentityProviderAttachmentUnavailable)

	// Non-compliant catch-alls answer the well-known probe with an app page
	// (200 decoded as "malformed"), the CDN/SPA default 500, or another
	// non-transient status rather than a 404 — content-shaped answers that a
	// retry would only re-read, so they read as permanent non-publication.
	for _, catchAll := range []*wellknown.ProtectedResourceDiscoveryError{
		{ProbeURL: "https://remote.example.test/.well-known/oauth-protected-resource", Status: http.StatusOK},
		{ProbeURL: "https://remote.example.test/.well-known/oauth-protected-resource", Status: http.StatusInternalServerError},
		{ProbeURL: "https://remote.example.test/.well-known/oauth-protected-resource", Status: http.StatusForbidden},
	} {
		catchAllErr := identityProviderDiscoveryError(remoteURLSourceKind, catchAll)
		require.ErrorIs(t, catchAllErr, ErrIdentityProviderNotDiscovered, "status %d", catchAll.Status)
		require.NotErrorIs(t, catchAllErr, ErrIdentityProviderAttachmentUnavailable, "status %d", catchAll.Status)
	}

	catalogErr := identityProviderDiscoveryError("catalog", notPublished)
	require.ErrorIs(t, catalogErr, ErrIdentityProviderAttachmentUnsupported)
	require.ErrorIs(t, catalogErr, notPublished)
	require.NotErrorIs(t, catalogErr, ErrIdentityProviderNotDiscovered)
}

// Request-scoped and infrastructure-transient conditions leave publication
// unknown: no HTTP answer at all, an untyped failure, or a status that talks
// about this request rather than the path's content (408, 429, 502, 503,
// 504). Each must stay a retryable unavailable — a not-discovered
// misclassification would steer a transient failure into a terminal
// dashboard fallback.
func TestIdentityProviderDiscoveryErrorKeepsUnknownRemoteFailuresRetryable(t *testing.T) {
	t.Parallel()

	unknownFailures := []error{
		&wellknown.ProtectedResourceDiscoveryError{ProbeURL: "https://remote.example.test/.well-known/oauth-protected-resource", Status: 0},
		&wellknown.ProtectedResourceDiscoveryError{ProbeURL: "https://remote.example.test/.well-known/oauth-protected-resource", Status: http.StatusTooManyRequests},
		&wellknown.ProtectedResourceDiscoveryError{ProbeURL: "https://remote.example.test/.well-known/oauth-protected-resource", Status: http.StatusServiceUnavailable},
		errors.New("discovery failed in an untyped way"),
	}
	for _, cause := range unknownFailures {
		remoteErr := identityProviderDiscoveryError(remoteURLSourceKind, cause)
		require.ErrorIs(t, remoteErr, ErrIdentityProviderAttachmentUnavailable, "cause %v", cause)
		require.ErrorIs(t, remoteErr, cause, "cause %v", cause)
		require.NotErrorIs(t, remoteErr, ErrIdentityProviderNotDiscovered, "cause %v", cause)
	}
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
