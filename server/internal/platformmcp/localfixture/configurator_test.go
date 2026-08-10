package localfixture

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestClientConfiguratorValidateMetadataRequiresReviewedContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cacheControl  string
		metadata      func(*Config) map[string]any
		expectErrText string
	}{
		{
			name:         "accepts reviewed metadata",
			cacheControl: "no-store",
			metadata:     fixtureMetadata,
		},
		{
			name:          "rejects cacheable metadata",
			cacheControl:  "private, max-age=60",
			metadata:      fixtureMetadata,
			expectErrText: "local fixture metadata is unavailable",
		},
		{
			name:         "rejects incompatible PKCE method",
			cacheControl: "no-store",
			metadata: func(config *Config) map[string]any {
				metadata := fixtureMetadata(config)
				metadata["code_challenge_methods_supported"] = []string{"plain"}
				return metadata
			},
			expectErrText: "local fixture metadata is incompatible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Cache-Control", tt.cacheControl)
				_ = json.NewEncoder(response).Encode(tt.metadata(configForTestServer(t, server.URL)))
			}))
			t.Cleanup(server.Close)

			config := configForTestServer(t, server.URL)
			configurator := &ClientConfigurator{config: config, policy: testPolicy(t, server)}
			err := configurator.validateMetadata(context.Background())
			if tt.expectErrText == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.expectErrText)
		})
	}
}

func TestClientConfiguratorRegisterClientRequiresPublicReviewedResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		registration  func(*Config) map[string]any
		expectErrText string
	}{
		{
			name:         "accepts public reviewed registration",
			registration: fixtureRegistration,
		},
		{
			name: "rejects client secret",
			registration: func(config *Config) map[string]any {
				registration := fixtureRegistration(config)
				registration["client_secret"] = "must-not-persist"
				return registration
			},
			expectErrText: "local fixture registration response is incompatible",
		},
		{
			name: "rejects incorrect redirect URI",
			registration: func(config *Config) map[string]any {
				registration := fixtureRegistration(config)
				registration["redirect_uris"] = []string{"https://other.example/callback"}
				return registration
			},
			expectErrText: "local fixture registration response is incompatible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var server *httptest.Server
			server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(response).Encode(tt.registration(configForTestServer(t, server.URL)))
			}))
			t.Cleanup(server.Close)

			config := configForTestServer(t, server.URL)
			configurator := &ClientConfigurator{config: config, policy: testPolicy(t, server)}
			registered, err := configurator.registerClient(context.Background())
			if tt.expectErrText == "" {
				require.NoError(t, err)
				require.Equal(t, "fixture-client-id", registered.clientID)
				return
			}
			require.ErrorContains(t, err, tt.expectErrText)
		})
	}
}

func configForTestServer(t *testing.T, rawURL string) *Config {
	t.Helper()

	origin, err := url.Parse(rawURL)
	require.NoError(t, err)
	config, err := NewConfig(origin)
	require.NoError(t, err)
	return config
}

func testPolicy(t *testing.T, server *httptest.Server) *guardian.Policy {
	t.Helper()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	return policy
}

func fixtureMetadata(config *Config) map[string]any {
	return map[string]any{
		"issuer":                                config.OAuthIssuerURL(),
		"authorization_endpoint":                config.OAuthAuthorizationURL(),
		"token_endpoint":                        config.OAuthTokenURL(),
		"registration_endpoint":                 config.OAuthRegistrationURL(),
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"tools:read"},
	}
}

func fixtureRegistration(config *Config) map[string]any {
	return map[string]any{
		"client_id":                  "fixture-client-id",
		"client_name":                OAuthClientName,
		"redirect_uris":              []string{config.RemoteLoginCallbackURL()},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
}
