// Package e2e exercises the tunnel end to end in-process: a real yamux session
// over a real WebSocket, agent reverse-proxying to a stub MCP server, and the
// gram-server serve path forwarding through the gateway. Proves the connect /
// negotiate / forward path and the tenant-isolation 502.
package e2e

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	releaseSlowRequest := make(chan struct{})
	var slowRequest sync.Once
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/mcp/slow" {
			slowRequest.Do(func() {
				select {
				case <-releaseSlowRequest:
				case <-r.Context().Done():
				}
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"echo":"` + r.Method + " " + r.URL.Path + ":" + string(body) + `"}`))
	}))
	defer mcp.Close()

	// Test gateway with a process-local route store and one seeded tunnel key.
	const tunnelID = "tunnel-1"
	plaintext, _, err := wire.NewKey()
	require.NoError(t, err)
	routes := newSnapshotStore()
	keys := gateway.NewStaticKeyStore(map[string]string{tunnelID: plaintext})
	gw := gateway.New(gateway.Config{}, keys, routes, logger)

	publicServer := httptest.NewServer(gw.PublicHandler())
	defer publicServer.Close()
	forwardServer := httptest.NewServer(gw.ForwardHandler())
	defer forwardServer.Close()
	gw.SetAdvertiseAddr(forwardServer.Listener.Addr().String())

	// Agent dials the gateway and pins the stub MCP as its upstream.
	wsURL := "ws" + strings.TrimPrefix(publicServer.URL, "http") + "/connect"
	ag, err := agent.New(agent.Config{
		GatewayURL:     wsURL,
		APIKey:         plaintext,
		LocalMCPURL:    mcp.URL,
		ServiceID:      "stub-mcp",
		ServiceSlug:    "stub-mcp",
		ServiceVersion: "0.1.0",
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
	req, err := http.NewRequest(http.MethodPost, forwardServer.URL+"/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
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

	errCh := make(chan error, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, forwardServer.URL+"/mcp/slow", strings.NewReader(`{"jsonrpc":"2.0"}`))
		if err != nil {
			errCh <- err
			return
		}
		req.Header.Set(wire.HeaderTunnelID, tunnelID)
		req.Header.Set(wire.HeaderTunnelConsumerSession, "consumer-1")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()
		_, _ = io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			errCh <- fmt.Errorf("unexpected status: %s", resp.Status)
			return
		}
		errCh <- nil
	}()

	requireEventually(t, 5*time.Second, func() bool {
		connections := routes.connections(tunnelID)
		return len(connections) == 1 &&
			connections[0].ActiveSubstreams == 1 &&
			connections[0].ActiveConsumerSessions == 1
	})
	close(releaseSlowRequest)
	require.NoError(t, <-errCh)
	requireEventually(t, 5*time.Second, func() bool {
		connections := routes.connections(tunnelID)
		return len(connections) == 1 &&
			connections[0].ActiveSubstreams == 0 &&
			connections[0].ActiveConsumerSessions == 1
	})

	// Tenant isolation: an unknown tunnel yields the distinct 502, never a leak.
	req2, err := http.NewRequest(http.MethodGet, forwardServer.URL+"/mcp/x", nil)
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
	gw := gateway.New(gateway.Config{}, gateway.NewStaticKeyStore(map[string]string{tunnelID: plaintext}), routes, logger)
	publicServer := httptest.NewServer(gw.PublicHandler())
	defer publicServer.Close()
	forwardServer := httptest.NewServer(gw.ForwardHandler())
	defer forwardServer.Close()
	gw.SetAdvertiseAddr(forwardServer.Listener.Addr().String())

	ag, err := agent.New(agent.Config{
		GatewayURL:     "ws" + strings.TrimPrefix(publicServer.URL, "http") + "/connect",
		APIKey:         plaintext,
		LocalMCPURL:    mcp.URL,
		ServiceID:      "stub-mcp",
		ServiceSlug:    "stub-mcp",
		ServiceVersion: "0.1.0",
		MaxBackoff:     200 * time.Millisecond,
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

type snapshotStore struct {
	*route.Memory

	mu        sync.Mutex
	snapshots map[string][]route.Connection
}

func newSnapshotStore() *snapshotStore {
	return &snapshotStore{
		Memory:    route.NewMemory(),
		mu:        sync.Mutex{},
		snapshots: make(map[string][]route.Connection),
	}
}

func (s *snapshotStore) PublishConnections(_ context.Context, tunnelID string, connections []route.Connection, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]route.Connection, len(connections))
	copy(copied, connections)
	s.snapshots[tunnelID] = copied
	return nil
}

func (s *snapshotStore) DeleteConnections(_ context.Context, tunnelID string) error {
	s.mu.Lock()
	delete(s.snapshots, tunnelID)
	s.mu.Unlock()
	return nil
}

func (s *snapshotStore) connections(tunnelID string) []route.Connection {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]route.Connection, len(s.snapshots[tunnelID]))
	copy(copied, s.snapshots[tunnelID])
	return copied
}

var _ route.Store = (*snapshotStore)(nil)
var _ route.ConnectionSnapshotStore = (*snapshotStore)(nil)
