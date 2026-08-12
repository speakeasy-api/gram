package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// flushRecorder records whether Flush reached the innermost writer.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flushRecorder) Flush() {
	f.flushed = true
	f.ResponseRecorder.Flush()
}

// Every ResponseWriter wrapper in this package must forward Flush to the
// writer it wraps. A wrapper that hides Flush silently disables streaming
// (SSE events sit in the server's write buffer until it fills) for every
// handler behind it — and the failure is invisible in httptest-based
// handler tests, which never exercise the real middleware chain.
func TestResponseWriterWrappersForwardFlush(t *testing.T) {
	t.Parallel()

	inner := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: false}
	wrappers := map[string]http.ResponseWriter{
		"logging.responseWriter":              newResponseWriter(inner),
		"admin_security.adminCookieRewriter":  &adminCookieRewriter{ResponseWriter: inner, domain: "example.com", wroteHeader: false},
		"clear_site_data.clearSiteDataWriter": &clearSiteDataWriter{ResponseWriter: inner, wroteHeader: false},
	}

	for name, w := range wrappers {
		inner.flushed = false
		flusher, ok := w.(http.Flusher)
		require.True(t, ok, "%s must implement http.Flusher", name)
		flusher.Flush()
		require.True(t, inner.flushed, "%s must forward Flush to the wrapped writer", name)

		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		require.True(t, ok, "%s must implement Unwrap for http.ResponseController", name)
		require.Equal(t, http.ResponseWriter(inner), unwrapper.Unwrap(), "%s must unwrap to the wrapped writer", name)
	}
}
