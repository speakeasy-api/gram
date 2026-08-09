package usersessions

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The tombstone reads no toolset — the whole proxy is retired, so every caller
// is a retired-proxy client — so a Service with only a logger is enough.
func newRetiredProxyMux(t *testing.T) goahttp.Muxer {
	t.Helper()

	mux := goahttp.NewMuxer()
	attachRetiredProxy(mux, &Service{logger: testenv.NewLogger(t)})
	return mux
}

// A client holding a proxy refresh token for a retired server must be told
// invalid_grant, which is the signal it acts on by discarding the token and
// re-running authorization against the issuer — not a 404, which would strand
// it on a dead token.
func TestRetiredProxyTokenEndpointReturnsInvalidGrant(t *testing.T) {
	t.Parallel()

	mux := newRetiredProxyMux(t)

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", "a-stale-proxy-refresh-token")
	form.Set("client_id", "some-client")

	req := httptest.NewRequest(http.MethodPost, "/oauth/any-mcp/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.Contains(t, rec.Body.String(), `"error":"invalid_grant"`)
	require.Contains(t, rec.Body.String(), "Re-authorize to continue")
}
