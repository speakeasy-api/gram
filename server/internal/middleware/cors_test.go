package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCORSMiddleware_PlatformOrigins(t *testing.T) {
	t.Parallel()

	const serverURL = "https://app.getgram.ai"
	platformOrigins := []string{"https://ai.speakeasy.com", serverURL}

	tests := []struct {
		name         string
		origin       string
		expected     string
		expectedVary string
	}{
		{name: "platform origin is echoed", origin: "https://ai.speakeasy.com", expected: "https://ai.speakeasy.com", expectedVary: "Origin"},
		{name: "server origin", origin: serverURL, expected: serverURL, expectedVary: "Origin"},
		{name: "unknown origin", origin: "https://evil.example", expected: serverURL, expectedVary: "Origin"},
		{name: "subdomain of a platform origin", origin: "https://dev.ai.speakeasy.com", expected: serverURL, expectedVary: "Origin"},
		{name: "no origin", origin: "", expected: serverURL, expectedVary: "Origin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := CORSMiddleware("prod", serverURL, platformOrigins, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "https://mcp.customer.example/rpc/x", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			require.Equal(t, tt.expected, rec.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, tt.expectedVary, rec.Header().Get("Vary"))
		})
	}
}

func TestCORSMiddleware_VaryOrigin(t *testing.T) {
	t.Parallel()

	const serverURL = "https://app.getgram.ai"
	serve := func(platformOrigins []string, path string) http.Header {
		handler := CORSMiddleware("prod", serverURL, platformOrigins, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "https://app.getgram.ai"+path, nil)
		req.Header.Set("Origin", "https://ai.speakeasy.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Header()
	}

	t.Run("no platform origins keeps today's headers", func(t *testing.T) {
		t.Parallel()
		h := serve(nil, "/rpc/x")
		require.Equal(t, serverURL, h.Get("Access-Control-Allow-Origin"))
		require.Empty(t, h.Get("Vary"))
	})

	t.Run("open well-known routes answer any origin without Vary", func(t *testing.T) {
		t.Parallel()
		h := serve([]string{"https://ai.speakeasy.com"}, "/.well-known/oauth-protected-resource/mcp/x")
		require.Equal(t, "*", h.Get("Access-Control-Allow-Origin"))
		require.Empty(t, h.Get("Vary"))
	})
}
