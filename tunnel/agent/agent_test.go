package agent

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentPreservesPinnedRootTargetPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	agent, err := New(Config{
		GatewayURL:     "ws://example.test/connect",
		APIKey:         "gram_tunnel_test",
		LocalMCPURL:    upstream.URL + "/mcp",
		ServiceID:      "postgres-mcp",
		ServiceSlug:    "postgres-mcp",
		ServiceVersion: "1.0.0",
		Metadata:       map[string]string{},
		MinBackoff:     0,
		MaxBackoff:     0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	agent.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/mcp", gotPath)
}
