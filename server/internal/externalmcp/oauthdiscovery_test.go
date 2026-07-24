package externalmcp

import (
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
			require.NoError(t, json.NewEncoder(w).Encode(protectedResourceMetadata{
				Resource:             server.URL + "/mcp",
				AuthorizationServers: []string{server.URL},
				ScopesSupported:      []string{"resource.read", "resource.write"},
			}))
		case "/.well-known/oauth-authorization-server":
			require.NoError(t, json.NewEncoder(w).Encode(authServerMetadata{
				Issuer:                server.URL,
				AuthorizationEndpoint: server.URL + "/authorize",
				TokenEndpoint:         server.URL + "/token",
				RegistrationEndpoint:  server.URL + "/register",
				ScopesSupported:       nil,
			}))
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
	require.Equal(t, []string{"resource.read", "resource.write"}, result.ScopesSupported)
}
