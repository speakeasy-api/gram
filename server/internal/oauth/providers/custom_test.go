package providers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauth/providers"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func newProvider(t *testing.T) *providers.CustomProvider {
	t.Helper()
	return providers.NewCustomProvider(testenv.NewLogger(t), nil)
}

func baseProvider(tokenEndpoint string) oauth_repo.OauthProxyProvider {
	return oauth_repo.OauthProxyProvider{
		TokenEndpoint:                     pgtype.Text{String: tokenEndpoint, Valid: true},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
		Secrets:                           []byte(`{"client_id":"cid","client_secret":"csec"}`),
	}
}

func TestCustomProvider_RefreshToken_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "rotated-refresh",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	prov := baseProvider(srv.URL + "/token")
	result, err := newProvider(t).RefreshToken(t.Context(), "old-refresh", prov, &toolsets_repo.Toolset{})

	require.NoError(t, err)
	require.Equal(t, "new-access", result.AccessToken)
	require.Equal(t, "rotated-refresh", result.RefreshToken)
	require.NotNil(t, result.ExpiresAt)
}

func TestCustomProvider_RefreshToken_NoRotation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	prov := baseProvider(srv.URL + "/token")
	result, err := newProvider(t).RefreshToken(t.Context(), "old-refresh", prov, &toolsets_repo.Toolset{})

	require.NoError(t, err)
	require.Equal(t, "new-access", result.AccessToken)
	require.Empty(t, result.RefreshToken, "caller is responsible for preserving the original refresh token")
}

func TestCustomProvider_RefreshToken_BasicAuth(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var formHasClientID bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = r.ParseForm()
		_, formHasClientID = r.PostForm["client_id"]

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	prov := baseProvider(srv.URL + "/token")
	prov.TokenEndpointAuthMethodsSupported = []string{"client_secret_basic"}

	_, err := newProvider(t).RefreshToken(t.Context(), "rt", prov, &toolsets_repo.Toolset{})
	require.NoError(t, err)

	require.Contains(t, gotAuth, "Basic ", "should use Basic auth header")
	require.False(t, formHasClientID, "client_id should NOT be in form body for basic auth")
}

func TestCustomProvider_RefreshToken_PostAuth(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var formClientID, formClientSecret string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = r.ParseForm()
		formClientID = r.PostFormValue("client_id")
		formClientSecret = r.PostFormValue("client_secret")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new",
			"expires_in":   3600,
		})
	}))
	defer srv.Close()

	prov := baseProvider(srv.URL + "/token")
	prov.TokenEndpointAuthMethodsSupported = []string{"client_secret_post"}

	_, err := newProvider(t).RefreshToken(t.Context(), "rt", prov, &toolsets_repo.Toolset{})
	require.NoError(t, err)

	require.Empty(t, gotAuth, "should NOT use Basic auth header for post auth")
	require.Equal(t, "cid", formClientID)
	require.Equal(t, "csec", formClientSecret)
}

func TestCustomProvider_RefreshToken_UpstreamError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer srv.Close()

	prov := baseProvider(srv.URL + "/token")
	_, err := newProvider(t).RefreshToken(t.Context(), "rt", prov, &toolsets_repo.Toolset{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "401")
}

func TestCustomProvider_RefreshToken_CamelCaseResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Some providers return camelCase field names
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessToken":  "new-camel",
			"refreshToken": "rotated-camel",
			"expiresIn":    3600,
		})
	}))
	defer srv.Close()

	prov := baseProvider(srv.URL + "/token")
	result, err := newProvider(t).RefreshToken(t.Context(), "rt", prov, &toolsets_repo.Toolset{})

	require.NoError(t, err)
	require.Equal(t, "new-camel", result.AccessToken)
	require.Equal(t, "rotated-camel", result.RefreshToken)
	require.NotNil(t, result.ExpiresAt)
}
