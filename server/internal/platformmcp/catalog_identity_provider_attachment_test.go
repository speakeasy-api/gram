package platformmcp

import (
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

func TestIdentityProviderRegistrationErrorTreatsTimeoutAndRateLimitAsRetryable(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError} {
		err := identityProviderRegistrationError(failedRegistration(&registration.HTTPError{StatusCode: status, ProviderMessage: ""}))
		require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnavailable, status)
	}

	err := identityProviderRegistrationError(failedRegistration(&registration.HTTPError{StatusCode: http.StatusBadRequest, ProviderMessage: "provider-controlled detail"}))
	require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
	require.NotContains(t, err.Error(), "provider-controlled detail")
}

// A provider without dynamic registration cannot serve this flow unchanged.
func TestIdentityProviderRegistrationErrorTreatsManualSetupAsUnsupported(t *testing.T) {
	t.Parallel()

	var reg remotesessions.Registration
	reg.ManualSetupRequired = true
	require.ErrorIs(t, identityProviderRegistrationError(reg), ErrIdentityProviderAttachmentUnsupported)
}

func failedRegistration(err error) remotesessions.Registration {
	failure := registration.ClassifyDCR(err)
	var reg remotesessions.Registration
	reg.Method = remotesessions.RegistrationDCR
	reg.Failure = &failure
	return reg
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
