package agent

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestAgentPreservesPinnedRootTargetPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	agent, err := New(Config{
		GatewayURL:     "wss://example.test/connect",
		APIKey:         "gram_tunnel_test",
		LocalMCPURL:    upstream.URL + "/mcp?api_key=x",
		ServiceVersion: "1.0.0",
		Metadata:       map[string]string{},
		MinBackoff:     0,
		MaxBackoff:     0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/?tenant=y", nil)
	agent.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/mcp", gotPath)
	require.Equal(t, "api_key=x&tenant=y", gotQuery)
}

func TestAgentPreservesNonRootInboundPathAndQuery(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
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
	req := httptest.NewRequest(http.MethodPost, "/oauth/token?tenant=x", nil)
	agent.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/oauth/token", gotPath)
	require.Equal(t, "tenant=x", gotQuery)
	require.NotEqual(t, "/mcp/oauth/token", gotPath)
}

func TestAgentKeepsPinnedTargetQueryOnNonRootInboundPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	agent, err := New(Config{
		GatewayURL:     "wss://example.test/connect",
		APIKey:         "gram_tunnel_test",
		LocalMCPURL:    upstream.URL + "/mcp?api_key=x",
		ServiceVersion: "1.0.0",
		Metadata:       map[string]string{},
		MinBackoff:     0,
		MaxBackoff:     0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token?tenant=y", nil)
	agent.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/oauth/token", gotPath)
	require.Equal(t, "api_key=x&tenant=y", gotQuery,
		"a tunneled OAuth call must keep the credentials pinned in TUNNEL_LOCAL_MCP_URL")
}

func TestAgentKeepsPinnedTargetQueryWhenInboundHasNone(t *testing.T) {
	t.Parallel()

	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	agent, err := New(Config{
		GatewayURL:     "wss://example.test/connect",
		APIKey:         "gram_tunnel_test",
		LocalMCPURL:    upstream.URL + "/mcp?api_key=x",
		ServiceVersion: "1.0.0",
		Metadata:       map[string]string{},
		MinBackoff:     0,
		MaxBackoff:     0,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	agent.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "api_key=x", gotQuery)
}

func TestAgentPreservesNonRootInboundRawPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotRawPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawPath = r.URL.RawPath
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
	req := httptest.NewRequest(http.MethodPost, "/oauth/%74oken", nil)
	agent.handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/oauth/token", gotPath)
	require.Equal(t, "/oauth/%74oken", gotRawPath)
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

// connectHeaders runs one connect attempt against a gateway that rejects the
// WebSocket upgrade and returns the headers the agent announced.
func connectHeaders(t *testing.T, cfg Config) http.Header {
	t.Helper()

	received := make(chan http.Header, 1)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(gateway.Close)

	cfg.GatewayURL = "ws" + strings.TrimPrefix(gateway.URL, "http") + "/connect"
	agent, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	var rejected *dialError
	require.ErrorAs(t, agent.connectOnce(t.Context()), &rejected)
	return <-received
}

func TestAgentAnnouncesDiagnosticsWithHostOnlyTarget(t *testing.T) {
	t.Parallel()

	header := connectHeaders(t, Config{
		APIKey:         "gram_tunnel_test",
		LocalMCPURL:    "https://user:pass@mcp.internal:8443/mcp/secret-token?api_key=private#fragment",
		ServiceVersion: "1.0.0",
	})

	capabilities := header.Get(wire.HeaderCapabilities)
	require.Equal(t, wire.DiagnosticsCapability, capabilities)
	require.True(t, wire.SupportsDiagnostics(capabilities, header.Get(wire.HeaderControlToken)))
	require.False(t, wire.DiagnosticsDisabled(capabilities))
	require.Equal(t, "https://mcp.internal:8443", header.Get(wire.HeaderTargetDisplay))
}

func TestAgentWithDiagnosticsTurnedOffAnnouncesIt(t *testing.T) {
	t.Parallel()

	header := connectHeaders(t, Config{
		APIKey:             "gram_tunnel_test",
		LocalMCPURL:        "https://mcp.internal:8443/mcp/secret-token",
		ServiceVersion:     "1.0.0",
		DisableDiagnostics: true,
	})

	capabilities := header.Get(wire.HeaderCapabilities)
	require.Equal(t, wire.DiagnosticsDisabledCapability, capabilities)
	require.True(t, wire.DiagnosticsDisabled(capabilities))
	require.False(t, wire.SupportsDiagnostics(capabilities, header.Get(wire.HeaderControlToken)))
	require.Empty(t, header.Values(wire.HeaderControlToken), "a turned-off agent serves no control path")
	require.Empty(t, header.Values(wire.HeaderTargetDisplay), "a turned-off agent does not reveal its target")
}
