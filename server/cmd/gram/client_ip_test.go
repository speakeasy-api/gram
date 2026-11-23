package gram

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/requestip"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestMCPMuxResolvesClientIP(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		header string
		want   string
	}{
		{name: "edge", header: "203.0.113.7", want: "203.0.113.7"},
		{name: "missing falls back", want: "192.0.2.1"},
		{name: "malformed falls back", header: "bad", want: "192.0.2.1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("mcp-client-ip", flag.ContinueOnError)
			flags.String("server-url", "https://app.example.com", "")
			flags.String("site-url", "https://app.example.com", "")
			flags.String("environment", "prod", "")
			origin, err := url.Parse("https://app.example.com")
			require.NoError(t, err)
			host, err := mcp.NewAuthenticationHost("https://auth.example.com", origin, "prod")
			require.NoError(t, err)
			// Platform-host classification and this route need no database or publishers.
			mux, err := newMCPServerMux(cli.NewContext(nil, flags, nil), testenv.NewLogger(t), nil, origin, host, nil, nil, &background.Publishers{})
			require.NoError(t, err)
			var handled *http.Request
			mux.Handle(http.MethodGet, "/test-client-ip", func(w http.ResponseWriter, r *http.Request) {
				handled = r
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, origin.String()+"/test-client-ip", nil)
			req.RemoteAddr = "192.0.2.1:4321"
			req.Header.Set("X-Client-IP", tt.header)
			req.Header.Set("X-Forwarded-For", "198.51.100.9, 192.0.2.1")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			require.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
			require.NotNil(t, handled)
			resolved, found := requestip.FromContext(handled.Context())
			require.True(t, found)
			require.Equal(t, tt.want, resolved.String())
			require.Equal(t, "198.51.100.9, 192.0.2.1", handled.Header.Get("X-Forwarded-For"))
		})
	}
}
