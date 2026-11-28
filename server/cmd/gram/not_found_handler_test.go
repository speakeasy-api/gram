package gram

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

// The listeners build their muxes with goahttp.NewMuxer, so the not-found
// handler must reach its router through middleware and alongside routes.
func TestSetNotFoundHandler_GoaMuxer(t *testing.T) {
	t.Parallel()

	mux := goahttp.NewMuxer()
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-Middleware", "1")
			next.ServeHTTP(w, r)
		})
	})
	mux.Handle(http.MethodGet, "/registered", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	require.NoError(t, setNotFoundHandler(mux, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	// Listeners attach more routes after installing the handler, which must
	// not reset it.
	mux.Handle(http.MethodGet, "/registered-after", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/unregistered", nil))
	require.Equal(t, http.StatusTeapot, w.Code)
	require.Equal(t, "1", w.Header().Get("X-Test-Middleware"))

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/registered", nil))
	require.Equal(t, http.StatusNoContent, w.Code)

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/registered-after", nil))
	require.Equal(t, http.StatusAccepted, w.Code)
}
