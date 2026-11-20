package fixture_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture"
)

func TestAttachServesUnderPrefix(t *testing.T) {
	t.Parallel()
	server, err := url.Parse("https://speakeasy.example.test")
	require.NoError(t, err)
	base := fixture.BaseURL(server)
	require.Equal(t, "https://speakeasy.example.test/rpc/demoseed", base)

	mux := goahttp.NewMuxer()
	fixture.Attach(mux, fixture.New(fixture.Config{BaseURL: base}))

	for path, want := range map[string]int{
		"/rpc/demoseed/healthz":                                           http.StatusOK,
		"/rpc/demoseed/.well-known/oauth-authorization-server":            http.StatusOK,
		"/rpc/demoseed/dcr/.well-known/openid-configuration":              http.StatusOK,
		"/rpc/demoseed/.well-known/oauth-protected-resource/mcp/oauth":    http.StatusOK,
		"/.well-known/oauth-authorization-server/rpc/demoseed":            http.StatusOK,
		"/.well-known/openid-configuration/rpc/demoseed/dcr":              http.StatusOK,
		"/.well-known/oauth-protected-resource/rpc/demoseed/mcp/oauth":    http.StatusOK,
		"/.well-known/oauth-protected-resource/rpc/demoseed/dcr/mcp/auto": http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		require.Equal(t, want, rec.Code, path)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/rpc/demoseed/.well-known/oauth-authorization-server", nil))
	require.Contains(t, rec.Body.String(), `"issuer":"https://speakeasy.example.test/rpc/demoseed"`)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/.well-known/oauth-authorization-server/rpc/demoseed/dcr", nil))
	require.Contains(t, rec.Body.String(), `"issuer":"https://speakeasy.example.test/rpc/demoseed/dcr"`)
}
