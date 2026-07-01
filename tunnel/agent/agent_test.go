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
		GatewayURL:     "wss://example.test/connect",
		APIKey:         "gram_tunnel_test",
		LocalMCPURL:    upstream.URL + "/mcp",
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

func TestNormalizeGatewayURLRejectsInsecureNonLocalHosts(t *testing.T) {
	t.Parallel()

	_, err := normalizeGatewayURL("ws://example.test/connect")
	require.Error(t, err)
}

func TestNormalizeGatewayURLAllowsLocalInsecureHosts(t *testing.T) {
	t.Parallel()

	got, err := normalizeGatewayURL("ws://127.0.0.1:8090/connect")
	require.NoError(t, err)
	require.Equal(t, "ws://127.0.0.1:8090/connect", got)
}

func TestNormalizeGatewayURLConvertsHTTPS(t *testing.T) {
	t.Parallel()

	got, err := normalizeGatewayURL("https://tunnel.example.test/connect")
	require.NoError(t, err)
	require.Equal(t, "wss://tunnel.example.test/connect", got)
}

func TestNormalizeGatewayURLRejectsMissingHost(t *testing.T) {
	t.Parallel()

	_, err := normalizeGatewayURL("wss:///connect")
	require.Error(t, err)
}

func TestNormalizeGatewayURLRejectsEmptyHostname(t *testing.T) {
	t.Parallel()

	_, err := normalizeGatewayURL("wss://:443/connect")
	require.Error(t, err)
}
