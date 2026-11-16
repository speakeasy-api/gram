package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/middleware"
)

func TestDeprecatedEndpoint_SetsHeadersOnSuccessAndError(t *testing.T) {
	t.Parallel()

	deprecatedAt := time.Date(2026, time.October, 26, 0, 0, 0, 0, time.UTC)
	sunset := time.Date(2026, time.November, 25, 0, 0, 0, 0, time.UTC)
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		handler := middleware.DeprecatedEndpoint(deprecatedAt, sunset)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/example", nil))

		require.Equal(t, status, rec.Code)
		require.Equal(t, "@1792972800", rec.Header().Get("Deprecation"))
		require.Equal(t, "Wed, 25 Nov 2026 00:00:00 GMT", rec.Header().Get("Sunset"))
	}
}
