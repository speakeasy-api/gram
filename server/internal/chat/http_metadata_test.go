package chat

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/middleware"
)

func TestHTTPMetadataClientIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		missingContext bool
		xff            string
		real           string
		remote         string
		header         string
		want           string
	}{
		{name: "trusted", header: "203.0.113.7", xff: "198.51.100.9, 192.0.2.1", real: "198.51.100.9", remote: "192.0.2.1:4321", want: "203.0.113.7"},
		{name: "missing trusted", xff: "198.51.100.9", real: "198.51.100.9", remote: "192.0.2.1:4321", want: "192.0.2.1"},
		{name: "unresolved trusted", xff: "198.51.100.9", real: "198.51.100.9", remote: "bad"},
		{name: "direct request uses transport", remote: "192.0.2.1:4321", want: "192.0.2.1"},
		{name: "missing middleware ignores raw headers", missingContext: true, header: "203.0.113.7", xff: "198.51.100.9", real: "198.51.100.8", remote: "192.0.2.1:4321"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wrap := middleware.PublicHTTPTracing("test")
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.RemoteAddr = tt.remote
			req.Header.Set("X-Client-IP", tt.header)
			req.Header.Set("X-Forwarded-For", tt.xff)
			req.Header.Set("X-Real-IP", tt.real)
			req.Header.Set("Origin", "https://app.example.com")
			req.Header.Set("User-Agent", "test-client")
			var result httpMetadata
			if tt.missingContext {
				result = extractHTTPMetadata(req)
			} else {
				wrap(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					result = extractHTTPMetadata(r)
				})).ServeHTTP(httptest.NewRecorder(), req)
			}
			require.Equal(t, tt.want, result.IPAddress)
			require.Equal(t, "https://app.example.com", result.Origin)
			require.Equal(t, "test-client", result.UserAgent)
		})
	}
}
