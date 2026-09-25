package adminmcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestStaffOAuthUsesCanonicalAdminOrigin(t *testing.T) {
	t.Parallel()
	base, err := url.Parse("https://admin.example.test/?ignored=true")
	require.NoError(t, err)
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	oauth, err := NewStaffOAuth(base, &pgxpool.Pool{}, testenv.NewMemoryCache(), &fakeAdminVerifier{}, cipher, sessiontokens.NewSigner("staff-test-signing-key"))
	require.NoError(t, err)
	require.Equal(t, "https://admin.example.test/admin-mcp", oauth.Resource())
	require.Equal(t, "https://admin.example.test/admin-mcp/oauth", oauth.Issuer())
	require.Equal(t, "https://admin.example.test/.well-known/oauth-protected-resource/admin-mcp", oauth.ProtectedResourceURL())
	base.Path = "/unexpected-prefix"
	_, err = NewStaffOAuth(base, &pgxpool.Pool{}, testenv.NewMemoryCache(), &fakeAdminVerifier{}, cipher, sessiontokens.NewSigner("staff-test-signing-key"))
	require.Error(t, err)
}

func TestStaffOAuthMetadataURLs(t *testing.T) {
	t.Parallel()

	oauth := &StaffOAuth{
		issuer:               "https://admin.example.test/admin-mcp/oauth",
		resource:             "https://admin.example.test/admin-mcp",
		protectedResourceURL: "https://admin.example.test/.well-known/oauth-protected-resource/admin-mcp",
	}
	require.Equal(t, "https://admin.example.test/.well-known/oauth-protected-resource/admin-mcp", oauth.ProtectedResourceURL())

	for _, tt := range []struct {
		path    string
		handler http.Handler
		want    map[string]any
	}{
		{
			path:    "/.well-known/oauth-protected-resource/admin-mcp",
			handler: oauth.ProtectedResourceHandler(),
			want: map[string]any{
				"resource":                 oauth.Resource(),
				"authorization_servers":    []any{oauth.Issuer()},
				"bearer_methods_supported": []any{"header"},
			},
		},
		{
			path:    "/.well-known/oauth-authorization-server/admin-mcp/oauth",
			handler: oauth.AuthorizationServerHandler(),
			want: map[string]any{
				"issuer":                 oauth.Issuer(),
				"authorization_endpoint": oauth.Resource() + "/authorize",
				"token_endpoint":         oauth.Resource() + "/token",
				"registration_endpoint":  oauth.Resource() + "/register",
			},
		},
	} {
		rec := httptest.NewRecorder()
		tt.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		var metadata map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metadata))
		for key, value := range tt.want {
			require.Equal(t, value, metadata[key], key)
		}
	}
}
