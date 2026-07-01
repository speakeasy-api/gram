package httpcache_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/httpcache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestWriteCacheableJSON_OK(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := []byte(`{"issuer":"https://example.test/mcp/foo"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/foo", nil)

	require.NoError(t, httpcache.WriteCacheableJSON(t.Context(), w, r, logger, "application/json", 60, body))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=60", w.Header().Get("Cache-Control"))
	require.NotEmpty(t, w.Header().Get("ETag"))
	require.Equal(t, body, w.Body.Bytes())
}

func TestWriteCacheableJSON_PreservesCharsetContentType(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-client/abc", nil)

	require.NoError(t, httpcache.WriteCacheableJSON(t.Context(), w, r, logger, "application/json; charset=utf-8", 3600, []byte(`{}`)))

	require.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))
}

func TestWriteCacheableJSON_NotModifiedOnMatchingIfNoneMatch(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := []byte(`{"issuer":"https://example.test/mcp/foo"}`)

	// First request to learn the ETag the server assigns to this body.
	first := httptest.NewRecorder()
	require.NoError(t, httpcache.WriteCacheableJSON(t.Context(), first, httptest.NewRequest(http.MethodGet, "/x", nil), logger, "application/json", 60, body))
	etag := first.Header().Get("ETag")
	require.NotEmpty(t, etag)

	// Re-request with the learned ETag: expect a bodyless 304 that still carries
	// the validators but no Content-Type.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("If-None-Match", etag)
	require.NoError(t, httpcache.WriteCacheableJSON(t.Context(), w, r, logger, "application/json", 60, body))

	require.Equal(t, http.StatusNotModified, w.Code)
	require.Empty(t, w.Body.Bytes())
	require.Empty(t, w.Header().Get("Content-Type"))
	require.Equal(t, etag, w.Header().Get("ETag"))
	require.Equal(t, "public, max-age=60", w.Header().Get("Cache-Control"))
}

func TestWriteCacheableJSON_NotModifiedOnStar(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("If-None-Match", "*")

	require.NoError(t, httpcache.WriteCacheableJSON(t.Context(), w, r, logger, "application/json", 60, []byte(`{}`)))

	require.Equal(t, http.StatusNotModified, w.Code)
	require.Empty(t, w.Body.Bytes())
}

func TestWriteCacheableJSON_ServesBodyOnMismatchedIfNoneMatch(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	body := []byte(`{"issuer":"https://example.test/mcp/foo"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("If-None-Match", `"some-other-etag"`)

	require.NoError(t, httpcache.WriteCacheableJSON(t.Context(), w, r, logger, "application/json", 60, body))

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, body, w.Body.Bytes())
}

func TestWriteCacheableJSON_ETagVariesWithBody(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)

	etagFor := func(body string) string {
		w := httptest.NewRecorder()
		require.NoError(t, httpcache.WriteCacheableJSON(t.Context(), w, httptest.NewRequest(http.MethodGet, "/x", nil), logger, "application/json", 60, []byte(body)))
		return w.Header().Get("ETag")
	}

	firstCall := etagFor(`{"a":1}`)
	secondCall := etagFor(`{"a":1}`)
	require.Equal(t, firstCall, secondCall, "identical bodies must share an ETag")
	require.NotEqual(t, firstCall, etagFor(`{"a":2}`), "different bodies must get different ETags")
}
