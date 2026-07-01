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

	const tunnelID = "tunnel-1"
	const forwardToken = "forward-token"
	plaintext, _, err := wire.NewKey()
	require.NoError(t, err)
	routes := newSnapshotStore()
	keys := gateway.NewStaticKeyStore(map[string]string{tunnelID: plaintext})
	gw, err := gateway.New(gateway.Config{ForwardToken: forwardToken}, keys, routes, logger)
	require.NoError(t, err)

	publicServer := httptest.NewServer(gw.PublicHandler())
	defer publicServer.Close()
	forwardServer := httptest.NewServer(gw.ForwardHandler())
	defer forwardServer.Close()
	gw.SetAdvertiseAddr(forwardServer.Listener.Addr().String())

	wsURL := "ws" + strings.TrimPrefix(publicServer.URL, "http") + "/connect"
	ag, err := agent.New(agent.Config{
		GatewayURL:     wsURL,
		APIKey:         plaintext,
		LocalMCPURL:    mcp.URL,
		ServiceVersion: "0.1.0",
	}, logger)
	require.NoError(t, err)
	go func() { _ = ag.Run(ctx) }()

	requireEventually(t, 5*time.Second, func() bool {
		candidates, _ := routes.Candidates(ctx, tunnelID)
		return len(candidates) == 1 && gw.ActiveSessions() == 1
	})

	req, err := http.NewRequest(http.MethodPost, forwardServer.URL+"/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	require.NoError(t, err)
	req.Header.Set(wire.HeaderTunnelID, tunnelID)
	req.Header.Set(wire.HeaderTunnelForwardToken, forwardToken)
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
		req.Header.Set(wire.HeaderTunnelForwardToken, forwardToken)
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

	req2, err := http.NewRequest(http.MethodGet, forwardServer.URL+"/mcp/x", nil)
	require.NoError(t, err)
	req2.Header.Set(wire.HeaderTunnelID, "does-not-exist")
	req2.Header.Set(wire.HeaderTunnelForwardToken, forwardToken)
	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusBadGateway, resp2.StatusCode)
	require.Equal(t, "no-live-session", resp2.Header.Get("X-Gram-Tunnel-Error"))
}

func TestTunnelRouteSurvivesCurrentGatewayDisconnect(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := t.Context()

	const tunnelID = "tunnel-shared-route"
	const forwardToken = "forward-token"
	plaintext, _, err := wire.NewKey()
	require.NoError(t, err)

	routes := route.NewRouteTable()
	keys := gateway.NewStaticKeyStore(map[string]string{tunnelID: plaintext})

	type peer struct {
		label       string
		gateway     *gateway.Gateway
		forwardAddr string
		cancelAgent context.CancelFunc
	}

	startPeer := func(label string) peer {
		mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(label + ":" + r.URL.Path))
		}))
		t.Cleanup(mcp.Close)

		gw, err := gateway.New(gateway.Config{ForwardToken: forwardToken}, keys, routes, logger)
		require.NoError(t, err)

		publicServer := httptest.NewServer(gw.PublicHandler())
		t.Cleanup(publicServer.Close)
		forwardServer := httptest.NewServer(gw.ForwardHandler())
		t.Cleanup(forwardServer.Close)
		gw.SetAdvertiseAddr(forwardServer.Listener.Addr().String())

		ag, err := agent.New(agent.Config{
			GatewayURL:     "ws" + strings.TrimPrefix(publicServer.URL, "http") + "/connect",
			APIKey:         plaintext,
			LocalMCPURL:    mcp.URL,
			ServiceVersion: "0.1.0",
			MaxBackoff:     200 * time.Millisecond,
		}, logger)
		require.NoError(t, err)

		agentCtx, cancelAgent := context.WithCancel(ctx)
		t.Cleanup(cancelAgent)
		go func() { _ = ag.Run(agentCtx) }()

		return peer{
			label:       label,
			gateway:     gw,
			forwardAddr: forwardServer.Listener.Addr().String(),
			cancelAgent: cancelAgent,
		}
	}

	peerA := startPeer("gateway-a")
	peerB := startPeer("gateway-b")

	requireEventually(t, 5*time.Second, func() bool {
		candidates, _ := routes.Candidates(ctx, tunnelID)
		return len(candidates) == 2 && peerA.gateway.ActiveSessions() == 1 && peerB.gateway.ActiveSessions() == 1
	})

	peerA.cancelAgent()
	requireEventually(t, 5*time.Second, func() bool {
		candidates, _ := routes.Candidates(ctx, tunnelID)
		return len(candidates) == 1 &&
			candidates[0] == peerB.forwardAddr &&
			peerA.gateway.ActiveSessions() == 0 &&
			peerB.gateway.ActiveSessions() == 1
	})

	status, body := forwardThroughRoute(t, ctx, routes, tunnelID, forwardToken, "/mcp/after-owner-disconnect")
	require.Equal(t, http.StatusOK, status)
	require.Contains(t, body, peerB.label+":/mcp/after-owner-disconnect")
}

func TestTunnelConsumerSessionSticksToAgent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := t.Context()

	const tunnelID = "tunnel-consumer-sticky"
	const forwardToken = "forward-token"
	plaintext, _, err := wire.NewKey()
	require.NoError(t, err)

	routes := route.NewRouteTable()
	keys := gateway.NewStaticKeyStore(map[string]string{tunnelID: plaintext})
	gw, err := gateway.New(gateway.Config{ForwardToken: forwardToken}, keys, routes, logger)
	require.NoError(t, err)

	publicServer := httptest.NewServer(gw.PublicHandler())
	t.Cleanup(publicServer.Close)
	forwardServer := httptest.NewServer(gw.ForwardHandler())
	t.Cleanup(forwardServer.Close)
	gw.SetAdvertiseAddr(forwardServer.Listener.Addr().String())

	startAgent := func(label string) {
		mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(label + ":" + r.URL.Path))
		}))
		t.Cleanup(mcp.Close)

		ag, err := agent.New(agent.Config{
			GatewayURL:     "ws" + strings.TrimPrefix(publicServer.URL, "http") + "/connect",
			APIKey:         plaintext,
			LocalMCPURL:    mcp.URL,
			ServiceVersion: "0.1.0",
			MaxBackoff:     200 * time.Millisecond,
		}, logger)
		require.NoError(t, err)
		go func() { _ = ag.Run(ctx) }()
	}
	startAgent("agent-a")
	startAgent("agent-b")

	requireEventually(t, 5*time.Second, func() bool {
		candidates, _ := routes.Candidates(ctx, tunnelID)
		return len(candidates) == 1 && gw.ActiveSessions() == 2
	})

	first := forwardToGateway(t, ctx, forwardServer.URL, tunnelID, forwardToken, "auth:stable-client", "/mcp/initialize")
	second := forwardToGateway(t, ctx, forwardServer.URL, tunnelID, forwardToken, "auth:stable-client", "/mcp/tools/list")

	require.Equal(t, responseAgentLabel(first), responseAgentLabel(second))
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
	routes := route.NewRouteTable()
	gw, err := gateway.New(gateway.Config{ForwardToken: "forward-token"}, gateway.NewStaticKeyStore(map[string]string{tunnelID: plaintext}), routes, logger)
	require.NoError(t, err)
	publicServer := httptest.NewServer(gw.PublicHandler())
	defer publicServer.Close()
	forwardServer := httptest.NewServer(gw.ForwardHandler())
	defer forwardServer.Close()
	gw.SetAdvertiseAddr(forwardServer.Listener.Addr().String())

	ag, err := agent.New(agent.Config{
		GatewayURL:     "ws" + strings.TrimPrefix(publicServer.URL, "http") + "/connect",
		APIKey:         plaintext,
		LocalMCPURL:    mcp.URL,
		ServiceVersion: "0.1.0",
		MaxBackoff:     200 * time.Millisecond,
	}, logger)
	require.NoError(t, err)
	go func() { _ = ag.Run(ctx) }()

	requireEventually(t, 5*time.Second, func() bool { return gw.ActiveSessions() == 1 })

	killed := gw.RevokeTunnel(ctx, tunnelID)
	require.Equal(t, 1, killed)

	candidates, err := routes.Candidates(ctx, tunnelID)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func forwardThroughRoute(t *testing.T, ctx context.Context, routes route.Store, tunnelID, forwardToken, path string) (int, string) {
	t.Helper()

	candidates, err := routes.Candidates(ctx, tunnelID)
	require.NoError(t, err)
	require.NotEmpty(t, candidates, "expected a live route for tunnel %q", tunnelID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+candidates[0]+path, nil)
	require.NoError(t, err)
	req.Header.Set(wire.HeaderTunnelID, tunnelID)
	req.Header.Set(wire.HeaderTunnelForwardToken, forwardToken)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(body)
}

func forwardToGateway(t *testing.T, ctx context.Context, forwardURL, tunnelID, forwardToken, consumerSession, path string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, forwardURL+path, nil)
	require.NoError(t, err)
	req.Header.Set(wire.HeaderTunnelID, tunnelID)
	req.Header.Set(wire.HeaderTunnelForwardToken, forwardToken)
	req.Header.Set(wire.HeaderTunnelConsumerSession, consumerSession)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return string(body)
}

func responseAgentLabel(body string) string {
	label, _, _ := strings.Cut(body, ":")
	return label
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
	*route.RouteTable

	mu        sync.Mutex
	snapshots map[string]map[string][]route.Connection
}

func newSnapshotStore() *snapshotStore {
	return &snapshotStore{
		RouteTable: route.NewRouteTable(),
		mu:         sync.Mutex{},
		snapshots:  make(map[string]map[string][]route.Connection),
	}
}

func (s *snapshotStore) PublishConnections(_ context.Context, tunnelID, owner string, connections []route.Connection, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copied := make([]route.Connection, len(connections))
	copy(copied, connections)
	if s.snapshots[tunnelID] == nil {
		s.snapshots[tunnelID] = make(map[string][]route.Connection)
	}
	s.snapshots[tunnelID][owner] = copied
	return nil
}

func (s *snapshotStore) Connections(_ context.Context, tunnelID string) ([]route.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	connections := s.copyConnectionsLocked(tunnelID)
	return connections, nil
}

func (s *snapshotStore) DeleteConnectionOwner(_ context.Context, tunnelID, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.snapshots[tunnelID], owner)
	if len(s.snapshots[tunnelID]) == 0 {
		delete(s.snapshots, tunnelID)
	}
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

	return s.copyConnectionsLocked(tunnelID)
}

func (s *snapshotStore) copyConnectionsLocked(tunnelID string) []route.Connection {
	count := 0
	for _, ownerConnections := range s.snapshots[tunnelID] {
		count += len(ownerConnections)
	}
	copied := make([]route.Connection, 0, count)
	for _, ownerConnections := range s.snapshots[tunnelID] {
		copied = append(copied, ownerConnections...)
	}
	return copied
}

var _ route.Store = (*snapshotStore)(nil)
var _ route.ConnectionSnapshotStore = (*snapshotStore)(nil)

func TestTunnelGatewayShedsConnectsAtSessionCap(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := t.Context()

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mcp.Close()

	const tunnelID = "tunnel-cap"
	plaintext, _, err := wire.NewKey()
	require.NoError(t, err)
	routes := newSnapshotStore()
	gw, err := gateway.New(
		gateway.Config{ForwardToken: "forward-token", MaxSessions: 1},
		gateway.NewStaticKeyStore(map[string]string{tunnelID: plaintext}),
		routes,
		logger,
	)
	require.NoError(t, err)

	publicServer := httptest.NewServer(gw.PublicHandler())
	defer publicServer.Close()
	forwardServer := httptest.NewServer(gw.ForwardHandler())
	defer forwardServer.Close()
	gw.SetAdvertiseAddr(forwardServer.Listener.Addr().String())

	wsURL := "ws" + strings.TrimPrefix(publicServer.URL, "http") + "/connect"
	ag, err := agent.New(agent.Config{
		GatewayURL:     wsURL,
		APIKey:         plaintext,
		LocalMCPURL:    mcp.URL,
		ServiceVersion: "0.1.0",
	}, logger)
	require.NoError(t, err)
	go func() { _ = ag.Run(ctx) }()

	requireEventually(t, 5*time.Second, func() bool { return gw.ActiveSessions() == 1 })

	// At the cap, connects shed with 503 before key lookup runs.
	resp, err := http.Get(publicServer.URL + "/connect")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	// The live session keeps forwarding while new connects shed.
	req, err := http.NewRequest(http.MethodPost, forwardServer.URL+"/mcp", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set(wire.HeaderTunnelID, tunnelID)
	req.Header.Set(wire.HeaderTunnelForwardToken, "forward-token")
	forwardResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer forwardResp.Body.Close()
	require.Equal(t, http.StatusOK, forwardResp.StatusCode)
}
