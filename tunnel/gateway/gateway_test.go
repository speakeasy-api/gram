package gateway

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

func TestPublicHandlerDoesNotForward(t *testing.T) {
	t.Parallel()

	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")

	gw.PublicHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}

func newForwardTestGateway(t *testing.T, cfg Config) *Gateway {
	t.Helper()
	gw, err := New(cfg, NewStaticKeyStore(map[string]string{}), route.NewRouteTable(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	return gw
}

func TestNewRejectsMissingForwardToken(t *testing.T) {
	t.Parallel()

	_, err := New(Config{}, NewStaticKeyStore(map[string]string{}), route.NewRouteTable(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.ErrorIs(t, err, errMissingForwardToken)
}

func TestForwardHandlerRejectsMissingOrWrongToken(t *testing.T) {
	t.Parallel()

	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret"})

	for _, token := range []string{"", "wrong"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
		req.Header.Set(wire.HeaderTunnelID, "tunnel-1")
		if token != "" {
			req.Header.Set(wire.HeaderTunnelForwardToken, token)
		}

		gw.ForwardHandler().ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
	}
}

func TestForwardHandlerAcceptsValidTokenAndStripsIt(t *testing.T) {
	t.Parallel()

	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")
	req.Header.Set(wire.HeaderTunnelForwardToken, "s3cret")

	gw.ForwardHandler().ServeHTTP(rec, req)

	// 502 (not 403) means the token passed the gate and reached the no-session lookup.
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "no-live-session", rec.Header().Get("X-Gram-Tunnel-Error"))
	require.Empty(t, req.Header.Get(wire.HeaderTunnelForwardToken))
}

func TestForwardHandlerRejectsMissingForwardTokenConfig(t *testing.T) {
	t.Parallel()

	gw := &Gateway{
		cfg:    Config{},
		keys:   NewStaticKeyStore(map[string]string{}),
		routes: route.NewRouteTable(),
		reg:    newRegistry(),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")

	gw.ForwardHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestParseServiceMetadata(t *testing.T) {
	t.Parallel()

	metadata, err := parseServiceMetadata(`{"environment":"prod","blank":"","empty_key":"ok"," ":"ignored"}`)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"environment": "prod",
		"empty_key":   "ok",
	}, metadata)
}

func TestParseServiceMetadataRejectsOversizedMetadata(t *testing.T) {
	t.Parallel()

	_, err := parseServiceMetadata(`{"value":"` + strings.Repeat("a", wire.MaxServiceMetadataBytes) + `"}`)
	require.ErrorIs(t, err, errServiceMetadataTooLarge)
}

func TestRegistryBeginForwardRoundRobinsWithoutConsumerSession(t *testing.T) {
	reg := newRegistry()
	sessionA := newYamuxSession(t)
	sessionB := newYamuxSession(t)
	removeA := reg.add("tunnel-1", "session-a", sessionA, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-a", Metadata: map[string]string{}})
	removeB := reg.add("tunnel-1", "session-b", sessionB, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-b", Metadata: map[string]string{}})
	t.Cleanup(removeA)
	t.Cleanup(removeB)

	first, failure := reg.beginForward("tunnel-1", "", time.Now().UTC(), 0)
	require.Equal(t, forwardReserved, failure)
	second, failure := reg.beginForward("tunnel-1", "", time.Now().UTC(), 0)
	require.Equal(t, forwardReserved, failure)

	require.NotEqual(t, first.id, second.id)
}

func TestRegistryBeginForwardSticksStableConsumerSession(t *testing.T) {
	reg := newRegistry()
	sessionA := newYamuxSession(t)
	sessionB := newYamuxSession(t)
	removeA := reg.add("tunnel-1", "session-a", sessionA, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-a", Metadata: map[string]string{}})
	removeB := reg.add("tunnel-1", "session-b", sessionB, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-b", Metadata: map[string]string{}})
	t.Cleanup(removeA)
	t.Cleanup(removeB)

	first, failure := reg.beginForward("tunnel-1", "consumer-1", time.Now().UTC(), 0)
	require.Equal(t, forwardReserved, failure)
	for range 5 {
		entry, failure := reg.beginForward("tunnel-1", "consumer-1", time.Now().UTC(), 0)
		require.Equal(t, forwardReserved, failure)
		require.Equal(t, first.id, entry.id)
	}
}

func TestRegistryBeginForwardUsesNextRankedEligibleSession(t *testing.T) {
	reg := newRegistry()
	sessionA := newYamuxSession(t)
	sessionB := newYamuxSession(t)
	removeA := reg.add("tunnel-1", "session-a", sessionA, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-a", Metadata: map[string]string{}})
	removeB := reg.add("tunnel-1", "session-b", sessionB, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-b", Metadata: map[string]string{}})
	t.Cleanup(removeA)
	t.Cleanup(removeB)

	first, failure := reg.beginForward("tunnel-1", "consumer-1", time.Now().UTC(), 1)
	require.Equal(t, forwardReserved, failure)
	second, failure := reg.beginForward("tunnel-1", "consumer-1", time.Now().UTC(), 1)
	require.Equal(t, forwardReserved, failure)

	require.NotEqual(t, first.id, second.id)
}

// TestRegistryBeginForwardDistinguishesBusyFromNoSession: a healthy session
// at its substream cap must report forwardBusy, not forwardNoSession — the
// server-side retry policy unpublishes routes on no-live-session, and a
// capacity blip must not de-list a healthy gateway.
func TestRegistryBeginForwardDistinguishesBusyFromNoSession(t *testing.T) {
	reg := newRegistry()

	_, failure := reg.beginForward("tunnel-1", "", time.Now().UTC(), 1)
	require.Equal(t, forwardNoSession, failure)

	session := newYamuxSession(t)
	remove := reg.add("tunnel-1", "session-a", session, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-a", Metadata: map[string]string{}})
	t.Cleanup(remove)

	entry, failure := reg.beginForward("tunnel-1", "", time.Now().UTC(), 1)
	require.Equal(t, forwardReserved, failure)

	_, failure = reg.beginForward("tunnel-1", "", time.Now().UTC(), 1)
	require.Equal(t, forwardBusy, failure)

	reg.finishForward(entry, time.Now().UTC())
	_, failure = reg.beginForward("tunnel-1", "", time.Now().UTC(), 1)
	require.Equal(t, forwardReserved, failure)
}

func TestForwardHandlerReportsTunnelBusyAtCap(t *testing.T) {
	t.Parallel()

	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret", MaxStreamsPerTunnel: 1})
	session := newYamuxSession(t)
	remove := gw.reg.add("tunnel-1", "session-a", session, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-a", Metadata: map[string]string{}})
	t.Cleanup(remove)

	// Occupy the single substream slot so the next forward hits the cap.
	_, failure := gw.reg.beginForward("tunnel-1", "", time.Now().UTC(), 1)
	require.Equal(t, forwardReserved, failure)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(wire.HeaderTunnelID, "tunnel-1")
	req.Header.Set(wire.HeaderTunnelForwardToken, "s3cret")

	gw.ForwardHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, wire.TunnelErrorTunnelBusy, rec.Header().Get("X-Gram-Tunnel-Error"))
}

// unpublishHookStore wraps a route.Store and runs a hook just before the
// underlying Unpublish executes — used to interleave a concurrent connect at
// the worst possible moment of the disconnect cleanup.
type unpublishHookStore struct {
	route.Store
	beforeUnpublish func()
}

func (s *unpublishHookStore) Unpublish(ctx context.Context, tunnelID, addr string) error {
	if s.beforeUnpublish != nil {
		s.beforeUnpublish()
	}
	return s.Store.Unpublish(ctx, tunnelID, addr)
}

// TestCleanupSessionStateHealsReconnectRace reproduces the disconnect/connect
// race: session A's cleanup reads count==0, a replacement session B registers
// (its Publish already happened), then A's Unpublish deletes B's fresh route.
// The cleanup must detect the survivor and republish.
func TestCleanupSessionStateHealsReconnectRace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	table := route.NewRouteTable()
	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret", AdvertiseAddr: "gw-1:8091"})

	hooked := &unpublishHookStore{Store: table, beforeUnpublish: nil}
	gw.routes = hooked

	// Replacement session B "connected" earlier in the race: its Publish has
	// already landed; its registry add slips in during A's cleanup below.
	require.NoError(t, table.Publish(ctx, "tunnel-1", "gw-1:8091", time.Minute))
	sessionB := newYamuxSession(t)
	hooked.beforeUnpublish = func() {
		remove := gw.reg.add("tunnel-1", "session-b", sessionB, http.NotFoundHandler(), route.Connection{GatewaySessionID: "session-b", Metadata: map[string]string{}})
		t.Cleanup(remove)
	}

	// Session A already removed itself from the registry; count reads 0.
	gw.cleanupSessionState("tunnel-1")

	candidates, err := table.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Equal(t, []string{"gw-1:8091"}, candidates, "route for the live replacement session must survive the stale cleanup")
}

// TestCleanupSessionStateRemovesRouteWhenLastSessionCloses covers the normal
// path: no survivors, route and snapshot removed.
func TestCleanupSessionStateRemovesRouteWhenLastSessionCloses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	table := route.NewRouteTable()
	gw := newForwardTestGateway(t, Config{ForwardToken: "s3cret", AdvertiseAddr: "gw-1:8091"})
	gw.routes = table

	require.NoError(t, table.Publish(ctx, "tunnel-1", "gw-1:8091", time.Minute))

	gw.cleanupSessionState("tunnel-1")

	candidates, err := table.Candidates(ctx, "tunnel-1")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

// recordingKeyStore wraps StaticKeyStore with a MarkConnected spy.
type recordingKeyStore struct {
	*StaticKeyStore
	markConnectedCalls int
}

func (r *recordingKeyStore) MarkConnected(context.Context, string, string, string) error {
	r.markConnectedCalls++
	return nil
}

// TestConnectProbeDoesNotMarkConnected: a valid-key plain-HTTP request (no
// websocket upgrade) must not record durable activation — status='active' and
// last_seen_at advancing for a tunnel that never held a session misleads
// operators debugging an agent that never connected.
func TestConnectProbeDoesNotMarkConnected(t *testing.T) {
	t.Parallel()

	keys := &recordingKeyStore{StaticKeyStore: NewStaticKeyStore(map[string]string{"tunnel-1": "gram_tunnel_testkey"}), markConnectedCalls: 0}
	gw, err := New(Config{ForwardToken: "s3cret"}, keys, route.NewRouteTable(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/connect", nil)
	req.Header.Set("Authorization", "Bearer gram_tunnel_testkey")
	req.Header.Set(wire.HeaderTunnelServiceVersion, "1.0.0")

	gw.PublicHandler().ServeHTTP(rec, req)

	// The upgrade fails (no websocket headers) — activation must not have run.
	require.Equal(t, 0, keys.markConnectedCalls)
}

func newYamuxSession(t *testing.T) *yamux.Session {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	client, err := yamux.Client(clientConn, yamux.DefaultConfig())
	require.NoError(t, err)
	server, err := yamux.Server(serverConn, yamux.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
		require.NoError(t, server.Close())
	})
	return client
}
