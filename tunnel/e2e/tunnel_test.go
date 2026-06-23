// Package e2e exercises the tunnel end to end in-process: a real yamux session
// over a real WebSocket, agent reverse-proxying to a stub MCP server, and the
// gram-server serve path forwarding through the gateway. Proves the connect /
// negotiate / forward path and the tenant-isolation 502.
package e2e

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/agent"
	"github.com/speakeasy-api/gram/tunnel/gateway"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestTunnelEndToEnd(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := t.Context()

	// Stub local MCP server (stands in for the customer's MCP server).
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"echo":"` + r.Method + " " + r.URL.Path + ":" + string(body) + `"}`))
	}))
	defer mcp.Close()

	// Gateway with an in-memory route store and one seeded tunnel key.
	const tunnelID = "tunnel-1"
	plaintext, _, err := wire.NewKey()
	require.NoError(t, err)
	routes := route.NewMemory()
	keys := gateway.NewKeyStore(map[string]string{tunnelID: plaintext})
	gw := gateway.New(gateway.Config{}, keys, routes, logger)

	gwServer := httptest.NewServer(gw.Handler())
	defer gwServer.Close()
	gw.SetAdvertiseAddr(gwServer.Listener.Addr().String())

	// Agent dials the gateway and pins the stub MCP as its upstream.
	wsURL := "ws" + strings.TrimPrefix(gwServer.URL, "http") + "/connect"
	ag, err := agent.New(agent.Config{
		GatewayURL:  wsURL,
		APIKey:      plaintext,
		LocalMCPURL: mcp.URL,
	}, logger)
	require.NoError(t, err)
	go func() { _ = ag.Run(ctx) }()

	// Wait for the agent to connect and publish its route.
	requireEventually(t, 5*time.Second, func() bool {
		_, ok, _ := routes.Lookup(ctx, tunnelID)
		return ok && gw.ActiveSessions() == 1
	})

	// Forward through the gateway (as gram-server does, pod-to-pod) by setting
	// the tunnel-ID header. Must round-trip to the stub MCP via the substream.
	req, err := http.NewRequest(http.MethodPost, gwServer.URL+"/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	require.NoError(t, err)
	req.Header.Set(wire.HeaderTunnelID, tunnelID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(out), "POST /mcp/initialize")
	require.Contains(t, string(out), `{"jsonrpc":"2.0"}`)

	// Tenant isolation: an unknown tunnel yields the distinct 502, never a leak.
	req2, err := http.NewRequest(http.MethodGet, gwServer.URL+"/mcp/x", nil)
	require.NoError(t, err)
	req2.Header.Set(wire.HeaderTunnelID, "does-not-exist")
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusBadGateway, resp2.StatusCode)
	require.Equal(t, "no-live-session", resp2.Header.Get("X-Gram-Tunnel-Error"))
}

func TestTunnelRevoke(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := t.Context()

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mcp.Close()

	const tunnelID = "tunnel-revoke"
	plaintext, _, err := wire.NewKey()
	require.NoError(t, err)
	routes := route.NewMemory()
	gw := gateway.New(gateway.Config{}, gateway.NewKeyStore(map[string]string{tunnelID: plaintext}), routes, logger)
	gwServer := httptest.NewServer(gw.Handler())
	defer gwServer.Close()
	gw.SetAdvertiseAddr(gwServer.Listener.Addr().String())

	ag, err := agent.New(agent.Config{
		GatewayURL:  "ws" + strings.TrimPrefix(gwServer.URL, "http") + "/connect",
		APIKey:      plaintext,
		LocalMCPURL: mcp.URL,
		MaxBackoff:  200 * time.Millisecond,
	}, logger)
	require.NoError(t, err)
	go func() { _ = ag.Run(ctx) }()

	requireEventually(t, 5*time.Second, func() bool { return gw.ActiveSessions() == 1 })

	killed := gw.RevokeTunnel(ctx, tunnelID)
	require.Equal(t, 1, killed)

	// Route is gone after revoke.
	_, ok, _ := routes.Lookup(ctx, tunnelID)
	require.False(t, ok)
}

func requireEventually(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}
