package externalmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestBuildWellKnownURL_OriginOnly(t *testing.T) {
	t.Parallel()
	result := buildWellKnownURL("https://auth.example.com")
	require.Equal(t, "https://auth.example.com/.well-known/oauth-authorization-server", result)
}

func TestBuildWellKnownURL_WithPath(t *testing.T) {
	t.Parallel()
	result := buildWellKnownURL("https://example.com/mcp/my-server")
	require.Equal(t, "https://example.com/.well-known/oauth-authorization-server/mcp/my-server", result)
}

func TestBuildWellKnownURL_WithTrailingSlash(t *testing.T) {
	t.Parallel()
	result := buildWellKnownURL("https://example.com/mcp/my-server/")
	require.Equal(t, "https://example.com/.well-known/oauth-authorization-server/mcp/my-server", result)
}

func TestBuildWellKnownResourceURL_OriginOnly(t *testing.T) {
	t.Parallel()
	result := buildWellKnownResourceURL("https://auth.example.com")
	require.Equal(t, "https://auth.example.com/.well-known/oauth-protected-resource", result)
}

func TestBuildWellKnownResourceURL_WithPath(t *testing.T) {
	t.Parallel()
	result := buildWellKnownResourceURL("https://example.com/mcp/my-server")
	require.Equal(t, "https://example.com/.well-known/oauth-protected-resource/mcp/my-server", result)
}

func TestParseWWWAuthenticate_Empty(t *testing.T) {
	t.Parallel()
	params := parseWWWAuthenticate("")
	require.Empty(t, params)
}

func TestParseWWWAuthenticate_WithParams(t *testing.T) {
	t.Parallel()
	header := `Bearer realm="OAuth", resource_metadata="https://example.com/.well-known/oauth-protected-resource/mcp/test"`
	params := parseWWWAuthenticate(header)
	require.Equal(t, "OAuth", params["realm"])
	require.Equal(t, "https://example.com/.well-known/oauth-protected-resource/mcp/test", params["resource_metadata"])
}

func TestDiscoverOAuthMetadata_PreservesProtectedResourceScopes(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/resource-metadata":
			if err := json.NewEncoder(w).Encode(protectedResourceMetadata{
				Resource:             server.URL + "/mcp",
				AuthorizationServers: []string{server.URL},
				ScopesSupported:      []string{"resource.read", "resource.write"},
			}); err != nil {
				t.Errorf("encode protected resource metadata: %v", err)
			}
		case "/.well-known/oauth-authorization-server":
			if err := json.NewEncoder(w).Encode(authServerMetadata{
				Issuer:                server.URL,
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
				RegistrationEndpoint:  server.URL + "/register",
				ScopesSupported:       nil,
			}); err != nil {
				t.Errorf("encode authorization server metadata: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	result, err := DiscoverOAuthMetadata(
		t.Context(),
		testenv.NewLogger(t),
		policy,
		`Bearer resource_metadata="`+server.URL+`/resource-metadata"`,
		server.URL+"/mcp",
	)
	require.NoError(t, err)
	require.Equal(t, OAuthVersion21, result.Version)
	require.Equal(t, server.URL, result.Issuer)
	require.Equal(t, []string{"resource.read", "resource.write"}, result.ScopesSupported)
}

func TestDiscoverOAuthMetadataRejectsIssuerMismatch(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/resource-metadata":
			_ = json.NewEncoder(w).Encode(protectedResourceMetadata{
				Resource:             server.URL + "/mcp",
				AuthorizationServers: []string{server.URL + "/expected"},
				ScopesSupported:      nil,
			})
		case "/.well-known/oauth-authorization-server/expected":
			_ = json.NewEncoder(w).Encode(authServerMetadata{
				Issuer:                server.URL + "/different",
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
				RegistrationEndpoint:  server.URL + "/register",
				ScopesSupported:       nil,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	result, err := DiscoverOAuthMetadata(
		t.Context(),
		testenv.NewLogger(t),
		policy,
		`Bearer resource_metadata="`+server.URL+`/resource-metadata"`,
		server.URL+"/mcp",
	)
	require.ErrorContains(t, err, "issuer mismatch")
	require.Nil(t, result)
}

func TestDiscoverOAuthMetadataUsesLaterAdvertisedAuthorizationServer(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/resource-metadata":
			_ = json.NewEncoder(w).Encode(protectedResourceMetadata{
				Resource: server.URL + "/mcp",
				AuthorizationServers: []string{
					server.URL + "/unavailable",
					server.URL + "/mismatched",
					server.URL + "/valid",
				},
				ScopesSupported: []string{"resource.read"},
			})
		case "/.well-known/oauth-authorization-server/mismatched":
			_ = json.NewEncoder(w).Encode(authServerMetadata{
				Issuer:                server.URL + "/different",
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
				RegistrationEndpoint:  server.URL + "/register",
			})
		case "/.well-known/oauth-authorization-server/valid":
			_ = json.NewEncoder(w).Encode(authServerMetadata{
				Issuer:                server.URL + "/valid",
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
				RegistrationEndpoint:  server.URL + "/register",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	result, err := DiscoverOAuthMetadata(
		t.Context(),
		testenv.NewLogger(t),
		policy,
		`Bearer resource_metadata="`+server.URL+`/resource-metadata"`,
		server.URL+"/mcp",
	)
	require.NoError(t, err)
	require.Equal(t, OAuthVersion21, result.Version)
	require.Equal(t, server.URL+"/valid", result.Issuer)
	require.Equal(t, []string{"resource.read"}, result.ScopesSupported)
}

func TestDiscoverOAuthMetadataRejectsInsecureMetadataURL(t *testing.T) {
	t.Parallel()

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := DiscoverOAuthMetadata(
		ctx,
		testenv.NewLogger(t),
		policy,
		`Bearer auth_server_metadata="http://auth.example.com/.well-known/oauth-authorization-server"`,
		"https://mcp.example.com",
	)
	require.ErrorContains(t, err, "must use HTTPS")
	require.Nil(t, result)
}

func TestDiscoverOAuthMetadataRejectsInsecureMetadataRedirect(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://auth.example.com/.well-known/oauth-authorization-server", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)

	result, err := DiscoverOAuthMetadata(
		t.Context(),
		testenv.NewLogger(t),
		policy,
		`Bearer auth_server_metadata="`+server.URL+`"`,
		"https://mcp.example.com",
	)
	require.ErrorContains(t, err, "must use HTTPS")
	require.Nil(t, result)
}

func TestValidateOAuthIssuerRequiresHTTPS(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, validateOAuthIssuer("http://auth.example.com"), "HTTPS")
	require.ErrorContains(t, validateOAuthIssuer("https://auth.example.com?tenant=test"), "query or fragment")
	require.NoError(t, validateOAuthIssuer("https://auth.example.com/tenant"))
}
