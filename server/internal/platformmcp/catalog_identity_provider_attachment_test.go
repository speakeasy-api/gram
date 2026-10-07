package platformmcp

import (
	"encoding/json"
	"maps"
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

func TestDiscoverSupportedIssuerMetadataRejectsEmptyCandidates(t *testing.T) {
	t.Parallel()

	service := &CatalogIdentityProviderAttachmentService{}
	_, err := service.discoverSupportedIssuerMetadata(t.Context(), []string{"", "  "})

	require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
}

// Attachment accepts a provider the dashboard's automatic setup accepts: one
// with dynamic registration, or one with only a Client ID Metadata Document.
// A provider offering neither still needs manual setup.
func TestDiscoverSupportedIssuerMetadataAcceptsAutomaticRegistrationPaths(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		extra     map[string]any
		supported bool
	}{
		{name: "dynamic registration", extra: map[string]any{"registration_endpoint": "https://issuer.example.com/register"}, supported: true},
		{name: "client ID metadata document only", extra: map[string]any{"client_id_metadata_document_supported": true, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}}, supported: true},
		{name: "client ID metadata document refusing public clients", extra: map[string]any{"client_id_metadata_document_supported": true, "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}}, supported: false},
		{name: "neither", extra: map[string]any{"token_endpoint_auth_methods_supported": []string{"none"}}, supported: false},
		{name: "dynamic registration refusing client_secret_basic", extra: map[string]any{"registration_endpoint": "https://issuer.example.com/register", "token_endpoint_auth_methods_supported": []string{"none"}}, supported: false},
		{name: "dynamic registration refusing client_secret_basic with CIMD", extra: map[string]any{"registration_endpoint": "https://issuer.example.com/register", "client_id_metadata_document_supported": true, "token_endpoint_auth_methods_supported": []string{"none"}}, supported: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				document := map[string]any{
					"issuer":                 server.URL,
					"authorization_endpoint": server.URL + "/authorize",
					"token_endpoint":         server.URL + "/token",
				}
				maps.Copy(document, test.extra)
				w.Header().Set("Content-Type", "application/json")
				assert.NoError(t, json.NewEncoder(w).Encode(document))
			}))
			defer server.Close()
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)
			service := &CatalogIdentityProviderAttachmentService{policy: policy}

			metadata, err := service.discoverSupportedIssuerMetadata(t.Context(), []string{server.URL})
			if !test.supported {
				require.ErrorIs(t, err, ErrIdentityProviderAttachmentUnsupported)
				return
			}
			require.NoError(t, err)
			require.Equal(t, server.URL, metadata.Issuer)
		})
	}
}

// An issuer whose dynamic registration cannot issue the client_secret_basic
// client attachment requires is skipped, so a later CIMD issuer is chosen
// instead of registering an upstream client attachment would then refuse.
func TestDiscoverSupportedIssuerMetadataSkipsUnusableDynamicRegistration(t *testing.T) {
	t.Parallel()

	issuer := func(extra map[string]any) *httptest.Server {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			document := map[string]any{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
			}
			maps.Copy(document, extra)
			w.Header().Set("Content-Type", "application/json")
			assert.NoError(t, json.NewEncoder(w).Encode(document))
		}))
		t.Cleanup(server.Close)
		return server
	}
	unusable := issuer(map[string]any{"registration_endpoint": "https://issuer.example.com/register", "token_endpoint_auth_methods_supported": []string{"none"}})
	cimd := issuer(map[string]any{"client_id_metadata_document_supported": true, "token_endpoint_auth_methods_supported": []string{"none"}})
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	service := &CatalogIdentityProviderAttachmentService{policy: policy}

	metadata, err := service.discoverSupportedIssuerMetadata(t.Context(), []string{unusable.URL, cimd.URL})
	require.NoError(t, err)
	require.Equal(t, cimd.URL, metadata.Issuer)
}

// A provider offering only public-client dynamic registration is reported as
// automatic by the inspector, because the dashboard's automatic setup can
// register a public client there, but attachment skips it: it requires a
// client_secret_basic client and must not leave a refused client upstream.
func TestPublicClientOnlyDynamicRegistrationInspectorVersusAttachment(t *testing.T) {
	t.Parallel()

	metadata := map[string]any{"registration_endpoint": "https://issuer.example.com/register", "token_endpoint_auth_methods_supported": []any{"none"}}
	require.Equal(t, oauthDiscoveryAvailableDCR, directRemoteAutomaticRegistration(remotesessions.RegistrationCapabilities{RegistrationEndpoint: "https://issuer.example.com/register", TokenEndpointAuthMethodsSupported: []string{"none"}, ClientIDMetadataDocumentSupported: false}))

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		document := map[string]any{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
		}
		maps.Copy(document, metadata)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(document))
	}))
	defer server.Close()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	service := &CatalogIdentityProviderAttachmentService{policy: policy}

	_, err = service.discoverSupportedIssuerMetadata(t.Context(), []string{server.URL})
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
