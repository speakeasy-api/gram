package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const testForwardToken = "reconciler-forward-token"

type storeOperation struct {
	kind     string
	tunnelID string
	at       time.Time
}

type recordingRouteStore struct {
	table *route.RouteTable

	mu         sync.Mutex
	snapshots  map[string]map[string][]route.Connection
	operations []storeOperation

	blockPublish       bool
	publishStarted     chan struct{}
	publishRelease     chan struct{}
	publishStartedOnce sync.Once
	publishReleaseOnce sync.Once

	blockUnpublish       bool
	unpublishFailures    int
	unpublishStarted     chan struct{}
	unpublishRelease     chan struct{}
	unpublishStartedOnce sync.Once
	unpublishReleaseOnce sync.Once
	deleteFailures       int
}

func newRecordingRouteStore(blockPublish, blockUnpublish bool) *recordingRouteStore {
	return &recordingRouteStore{
		table:                route.NewRouteTable(),
		mu:                   sync.Mutex{},
		snapshots:            make(map[string]map[string][]route.Connection),
		operations:           nil,
		blockPublish:         blockPublish,
		publishStarted:       make(chan struct{}),
		publishRelease:       make(chan struct{}),
		publishStartedOnce:   sync.Once{},
		publishReleaseOnce:   sync.Once{},
		blockUnpublish:       blockUnpublish,
		unpublishFailures:    0,
		unpublishStarted:     make(chan struct{}),
		unpublishRelease:     make(chan struct{}),
		unpublishStartedOnce: sync.Once{},
		unpublishReleaseOnce: sync.Once{},
		deleteFailures:       0,
	}
}

func (s *recordingRouteStore) Publish(ctx context.Context, tunnelID, address string, ttl time.Duration) error {
	if s.blockPublish {
		s.publishStartedOnce.Do(func() { close(s.publishStarted) })
		<-s.publishRelease
	}
	if err := s.table.Publish(ctx, tunnelID, address, ttl); err != nil {
		return err
	}
	s.record("publish", tunnelID)
	return nil
}

func (s *recordingRouteStore) Candidates(ctx context.Context, tunnelID string) ([]string, error) {
	return s.table.Candidates(ctx, tunnelID)
}

func (s *recordingRouteStore) Unpublish(ctx context.Context, tunnelID, address string) error {
	if s.blockUnpublish {
		s.unpublishStartedOnce.Do(func() { close(s.unpublishStarted) })
		<-s.unpublishRelease
	}
	s.mu.Lock()
	if s.unpublishFailures > 0 {
		s.unpublishFailures--
		s.operations = append(s.operations, storeOperation{kind: "unpublish_failed", tunnelID: tunnelID, at: time.Now()})
		s.mu.Unlock()
		return errors.New("injected unpublish failure")
	}
	s.mu.Unlock()
	if err := s.table.Unpublish(ctx, tunnelID, address); err != nil {
		return err
	}
	s.record("unpublish", tunnelID)
	return nil
}

func (s *recordingRouteStore) Delete(ctx context.Context, tunnelID string) error {
	s.mu.Lock()
	if s.deleteFailures > 0 {
		s.deleteFailures--
		s.operations = append(s.operations, storeOperation{kind: "delete_failed", tunnelID: tunnelID, at: time.Now()})
		s.mu.Unlock()
		return errors.New("injected delete failure")
	}
	s.mu.Unlock()
	if err := s.table.Delete(ctx, tunnelID); err != nil {
		return err
	}
	s.record("delete", tunnelID)
	return nil
}

func (s *recordingRouteStore) PublishConnections(
	_ context.Context,
	tunnelID string,
	owner string,
	connections []route.Connection,
	_ time.Duration,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshots[tunnelID] == nil {
		s.snapshots[tunnelID] = make(map[string][]route.Connection)
	}
	s.snapshots[tunnelID][owner] = append([]route.Connection(nil), connections...)
	s.operations = append(s.operations, storeOperation{kind: "publish_connections", tunnelID: tunnelID, at: time.Now()})
	return nil
}

func (s *recordingRouteStore) Connections(_ context.Context, tunnelID string) ([]route.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var connections []route.Connection
	for _, ownerConnections := range s.snapshots[tunnelID] {
		connections = append(connections, ownerConnections...)
	}
	return connections, nil
}

func (s *recordingRouteStore) DeleteConnectionOwner(_ context.Context, tunnelID, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.snapshots[tunnelID], owner)
	if len(s.snapshots[tunnelID]) == 0 {
		delete(s.snapshots, tunnelID)
	}
	s.operations = append(s.operations, storeOperation{kind: "delete_owner", tunnelID: tunnelID, at: time.Now()})
	return nil
}

func (s *recordingRouteStore) DeleteConnections(_ context.Context, tunnelID string) error {
	s.mu.Lock()
	delete(s.snapshots, tunnelID)
	s.operations = append(s.operations, storeOperation{kind: "delete_connections", tunnelID: tunnelID, at: time.Now()})
	s.mu.Unlock()
	return nil
}

func (s *recordingRouteStore) record(kind, tunnelID string) {
	s.mu.Lock()
	s.operations = append(s.operations, storeOperation{kind: kind, tunnelID: tunnelID, at: time.Now()})
	s.mu.Unlock()
}

func (s *recordingRouteStore) operationsFor(tunnelID string) []storeOperation {
	s.mu.Lock()
	defer s.mu.Unlock()
	var operations []storeOperation
	for _, operation := range s.operations {
		if operation.tunnelID == tunnelID {
			operations = append(operations, operation)
		}
	}
	return operations
}

func (s *recordingRouteStore) writeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.operations)
}

func (s *recordingRouteStore) operationCount(tunnelID, kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, operation := range s.operations {
		if operation.tunnelID == tunnelID && operation.kind == kind {
			count++
		}
	}
	return count
}

func (s *recordingRouteStore) hasOwner(tunnelID, owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.snapshots[tunnelID][owner]
	return ok
}

func (s *recordingRouteStore) releasePublish() {
	s.publishReleaseOnce.Do(func() { close(s.publishRelease) })
}

func (s *recordingRouteStore) releaseUnpublish() {
	s.unpublishReleaseOnce.Do(func() { close(s.unpublishRelease) })
}

func (s *recordingRouteStore) failNextUnpublish() {
	s.mu.Lock()
	s.unpublishFailures++
	s.mu.Unlock()
}

func (s *recordingRouteStore) failNextDelete() {
	s.mu.Lock()
	s.deleteFailures++
	s.mu.Unlock()
}

var _ route.RuntimeStore = (*recordingRouteStore)(nil)

type gatewayHarness struct {
	gateway *Gateway
	store   *recordingRouteStore
	public  *httptest.Server
	forward *httptest.Server
	owner   string
}

func newGatewayHarness(t *testing.T, keys map[string]string, store *recordingRouteStore) *gatewayHarness {
	t.Helper()
	return newGatewayHarnessWithResolver(t, NewStaticKeyStore(keys), store, routeTTL/2)
}

func newGatewayHarnessWithResolver(
	t *testing.T,
	keys KeyResolver,
	store *recordingRouteStore,
	refreshInterval time.Duration,
) *gatewayHarness {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw, err := New(Config{ForwardToken: testForwardToken, routeRefreshInterval: refreshInterval}, keys, store, logger)
	require.NoError(t, err)
	publicServer := httptest.NewServer(gw.PublicHandler())
	forwardServer := httptest.NewServer(gw.ForwardHandler())
	owner := forwardServer.Listener.Addr().String()
	gw.SetAdvertiseAddr(owner)

	harness := &gatewayHarness{
		gateway: gw,
		store:   store,
		public:  publicServer,
		forward: forwardServer,
		owner:   owner,
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		gw.Drain(ctx)
		gw.CloseSessions(ctx)
		_ = forwardServer.Config.Shutdown(ctx)
		_ = publicServer.Config.Shutdown(ctx)
		forwardServer.Close()
		publicServer.Close()
	})
	return harness
}

type testAgent struct {
	session *yamux.Session
	conn    net.Conn
	done    chan struct{}
	close   sync.Once
}

func dialTestAgent(ctx context.Context, publicURL, key string, handler http.Handler) (*testAgent, *http.Response, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+key)
	headers.Set(wire.HeaderTunnelServiceVersion, "1.0.0")
	wsURL := "ws" + strings.TrimPrefix(publicURL, "http") + "/connect"
	ws, response, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return nil, response, err
	}

	conn := websocket.NetConn(ctx, ws, websocket.MessageBinary)
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	session, err := yamux.Server(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, response, err
	}
	done := make(chan struct{})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() {
		_ = server.Serve(session)
		close(done)
	}()
	return &testAgent{session: session, conn: conn, done: done, close: sync.Once{}}, response, nil
}

func (a *testAgent) Close() {
	a.close.Do(func() {
		_ = a.session.Close()
		_ = a.conn.Close()
		select {
		case <-a.done:
		case <-time.After(time.Second):
		}
	})
}

func successfulAgentHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

type controllableActiveKeyStore struct {
	*StaticKeyStore

	mu           sync.RWMutex
	active       bool
	activeByHash map[string]bool
	err          error
}

func newControllableActiveKeyStore(keys map[string]string) *controllableActiveKeyStore {
	return &controllableActiveKeyStore{
		StaticKeyStore: NewStaticKeyStore(keys),
		mu:             sync.RWMutex{},
		active:         true,
		activeByHash:   make(map[string]bool),
		err:            nil,
	}
}

func (k *controllableActiveKeyStore) IsActive(_ context.Context, _, keyHash string) (bool, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if active, ok := k.activeByHash[keyHash]; ok {
		return active, k.err
	}
	return k.active, k.err
}

func (k *controllableActiveKeyStore) setActive(active bool, err error) {
	k.mu.Lock()
	k.active = active
	k.err = err
	k.mu.Unlock()
}

func (k *controllableActiveKeyStore) setHashActive(keyHash string, active bool) {
	k.mu.Lock()
	k.activeByHash[keyHash] = active
	k.mu.Unlock()
}

var _ ActiveTunnelChecker = (*controllableActiveKeyStore)(nil)

type blockingConnectionKeyStore struct {
	*StaticKeyStore

	markStarted chan struct{}
	markRelease chan struct{}
	started     sync.Once
	released    sync.Once
}

func newBlockingConnectionKeyStore(keys map[string]string) *blockingConnectionKeyStore {
	return &blockingConnectionKeyStore{
		StaticKeyStore: NewStaticKeyStore(keys),
		markStarted:    make(chan struct{}),
		markRelease:    make(chan struct{}),
		started:        sync.Once{},
		released:       sync.Once{},
	}
}

func (k *blockingConnectionKeyStore) MarkConnected(ctx context.Context, _, _, _ string) error {
	k.started.Do(func() { close(k.markStarted) })
	select {
	case <-k.markRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (k *blockingConnectionKeyStore) releaseMarkConnected() {
	k.released.Do(func() { close(k.markRelease) })
}

var _ ConnectionRecorder = (*blockingConnectionKeyStore)(nil)

func TestGatewayDrainRemovesRoutesAndSnapshots(t *testing.T) {
	t.Parallel()

	keys := map[string]string{
		"tunnel-drain-a": "gram_tunnel_drain_key_a",
		"tunnel-drain-b": "gram_tunnel_drain_key_b",
	}
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarness(t, keys, store)

	agents := make([]*testAgent, 0, len(keys))
	for _, key := range keys {
		agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
		require.NoError(t, err)
		agents = append(agents, agent)
		t.Cleanup(agent.Close)
	}
	require.Eventually(t, func() bool {
		for tunnelID := range keys {
			candidates, _ := store.Candidates(t.Context(), tunnelID)
			if len(candidates) != 1 || !store.hasOwner(tunnelID, harness.owner) {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)

	harness.gateway.Drain(t.Context())
	for tunnelID := range keys {
		candidates, err := store.Candidates(t.Context(), tunnelID)
		require.NoError(t, err)
		require.Empty(t, candidates)
		require.False(t, store.hasOwner(tunnelID, harness.owner))
	}
	for _, agent := range agents {
		require.False(t, agent.session.IsClosed())
	}
	writesAtDrain := store.writeCount()
	require.Never(t, func() bool { return store.writeCount() != writesAtDrain }, 100*time.Millisecond, 5*time.Millisecond)

	harness.gateway.CloseSessions(t.Context())
	require.Eventually(t, func() bool { return harness.gateway.ActiveSessions() == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestGatewayRejectsConnectWhileDrainIsInProgress(t *testing.T) {
	t.Parallel()

	const key = "gram_tunnel_admission_key"
	store := newRecordingRouteStore(false, true)
	harness := newGatewayHarness(t, map[string]string{"tunnel-admission": key}, store)
	t.Cleanup(store.releaseUnpublish)
	healthResponse, err := http.Get(harness.public.URL + "/healthz")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, healthResponse.StatusCode)
	require.NoError(t, healthResponse.Body.Close())

	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	require.Eventually(t, func() bool {
		candidates, _ := store.Candidates(t.Context(), "tunnel-admission")
		return len(candidates) == 1
	}, 5*time.Second, 10*time.Millisecond)

	drainDone := make(chan struct{})
	go func() {
		harness.gateway.Drain(t.Context())
		close(drainDone)
	}()
	require.Eventually(t, func() bool { return channelClosed(store.unpublishStarted) }, 5*time.Second, 10*time.Millisecond)

	healthResponse, err = http.Get(harness.public.URL + "/healthz")
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, healthResponse.StatusCode)
	require.NoError(t, healthResponse.Body.Close())

	_, response, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.Error(t, err)
	require.NotNil(t, response)
	require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	if response.Body != nil {
		_ = response.Body.Close()
	}

	store.releaseUnpublish()
	require.Eventually(t, func() bool { return channelClosed(drainDone) }, 5*time.Second, 10*time.Millisecond)
}

func TestRouteReconcilerRetriesFailedGlobalRevokeOnTicker(t *testing.T) {
	t.Parallel()

	const (
		tunnelID = "tunnel-revoke-retry"
		key      = "gram_tunnel_revoke_retry_key"
	)
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarnessWithResolver(
		t,
		NewStaticKeyStore(map[string]string{tunnelID: key}),
		store,
		500*time.Millisecond,
	)
	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	require.Eventually(t, func() bool {
		candidates, _ := store.Candidates(t.Context(), tunnelID)
		return len(candidates) == 1
	}, time.Second, 5*time.Millisecond)
	require.NoError(t, store.Publish(t.Context(), tunnelID, "other-gateway", routeTTL))

	store.failNextDelete()
	require.Equal(t, 1, harness.gateway.RevokeTunnel(t.Context(), tunnelID))
	require.Equal(t, 1, store.operationCount(tunnelID, "delete_failed"))
	require.Equal(t, 1, store.operationCount(tunnelID, "delete_connections"))
	require.Eventually(t, func() bool {
		return store.operationCount(tunnelID, "delete") == 1 &&
			store.operationCount(tunnelID, "delete_connections") == 2
	}, 2*time.Second, 5*time.Millisecond)
	candidates, err := store.Candidates(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestRouteReconcilerRegistersRevokeWithCanceledContext(t *testing.T) {
	t.Parallel()

	const tunnelID = "tunnel-canceled-revoke"
	store := newRecordingRouteStore(false, true)
	harness := newGatewayHarnessWithResolver(
		t,
		NewStaticKeyStore(map[string]string{tunnelID: "gram_tunnel_canceled_revoke_key"}),
		store,
		20*time.Millisecond,
	)
	t.Cleanup(store.releaseUnpublish)
	require.NoError(t, store.table.Publish(t.Context(), tunnelID, "stale-owner", routeTTL))
	store.failNextDelete()
	harness.gateway.reconciler.nudge("blocked-cleanup")
	require.Eventually(t, func() bool { return channelClosed(store.unpublishStarted) }, time.Second, 5*time.Millisecond)

	canceledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	revokeStarted := make(chan struct{})
	revokeDone := make(chan struct{})
	killed := 0
	go func() {
		close(revokeStarted)
		killed = harness.gateway.RevokeTunnel(canceledCtx, tunnelID)
		close(revokeDone)
	}()
	<-revokeStarted
	require.Never(t, func() bool { return channelClosed(revokeDone) }, 50*time.Millisecond, 5*time.Millisecond)
	store.releaseUnpublish()
	require.Eventually(t, func() bool { return channelClosed(revokeDone) }, time.Second, 5*time.Millisecond)
	require.Zero(t, killed)
	require.Eventually(t, func() bool {
		candidates, candidatesErr := store.Candidates(t.Context(), tunnelID)
		return candidatesErr == nil && len(candidates) == 0 &&
			store.operationCount(tunnelID, "delete_failed") == 1 &&
			store.operationCount(tunnelID, "delete") == 1 &&
			store.operationCount(tunnelID, "delete_connections") == 2
	}, time.Second, 5*time.Millisecond)
}

func TestRouteReconcilerSuppressesConnectPublishAfterRevoke(t *testing.T) {
	t.Parallel()

	const (
		tunnelID = "tunnel-revoke-connect-race"
		key      = "gram_tunnel_revoke_connect_race_key"
	)
	keys := newBlockingConnectionKeyStore(map[string]string{tunnelID: key})
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarnessWithResolver(t, keys, store, time.Second)
	t.Cleanup(keys.releaseMarkConnected)
	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	require.Eventually(t, func() bool { return channelClosed(keys.markStarted) }, time.Second, 5*time.Millisecond)

	require.Zero(t, harness.gateway.RevokeTunnel(t.Context(), tunnelID))
	keys.releaseMarkConnected()
	require.Eventually(t, func() bool { return harness.gateway.ActiveSessions() == 1 }, time.Second, 5*time.Millisecond)
	require.Never(t, func() bool {
		candidates, _ := store.Candidates(t.Context(), tunnelID)
		return len(candidates) != 0
	}, 250*time.Millisecond, 5*time.Millisecond)
	require.Zero(t, store.operationCount(tunnelID, "publish"))
}

func TestGatewayShutdownLetsInflightForwardFinish(t *testing.T) {
	t.Parallel()

	const key = "gram_tunnel_inflight_key"
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarness(t, map[string]string{"tunnel-inflight": key}, store)
	forwardStarted := make(chan struct{})
	releaseForward := make(chan struct{})
	var startOnce sync.Once
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseForward) }) })
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			startOnce.Do(func() { close(forwardStarted) })
			<-releaseForward
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("complete"))
	})
	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, handler)
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	require.Eventually(t, func() bool {
		candidates, _ := store.Candidates(t.Context(), "tunnel-inflight")
		return len(candidates) == 1
	}, 5*time.Second, 10*time.Millisecond)

	type forwardResult struct {
		status int
		body   string
		err    error
	}
	forwardDone := make(chan forwardResult, 1)
	go func() {
		req, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, harness.forward.URL+"/slow", nil)
		if requestErr != nil {
			forwardDone <- forwardResult{status: 0, body: "", err: requestErr}
			return
		}
		req.Header.Set(wire.HeaderTunnelID, "tunnel-inflight")
		req.Header.Set(wire.HeaderTunnelForwardToken, testForwardToken)
		response, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			forwardDone <- forwardResult{status: 0, body: "", err: requestErr}
			return
		}
		defer response.Body.Close()
		body, requestErr := io.ReadAll(response.Body)
		forwardDone <- forwardResult{status: response.StatusCode, body: string(body), err: requestErr}
	}()
	require.Eventually(t, func() bool { return channelClosed(forwardStarted) }, 5*time.Second, 10*time.Millisecond)

	shutdownCtx, cancelShutdown := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelShutdown()
	shutdownDone := make(chan struct{})
	go func() {
		harness.gateway.Drain(shutdownCtx)
		_ = harness.forward.Config.Shutdown(shutdownCtx)
		harness.gateway.CloseSessions(shutdownCtx)
		_ = harness.public.Config.Shutdown(shutdownCtx)
		close(shutdownDone)
	}()
	require.Never(t, func() bool { return channelClosed(shutdownDone) }, 50*time.Millisecond, 5*time.Millisecond)
	releaseOnce.Do(func() { close(releaseForward) })

	select {
	case result := <-forwardDone:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.status)
		require.Equal(t, "complete", result.body)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "forward did not finish")
	}
	require.Eventually(t, func() bool { return channelClosed(shutdownDone) }, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return harness.gateway.ActiveSessions() == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestGatewayDrainConvergesDuringConcurrentChurn(t *testing.T) {
	t.Parallel()

	keys := map[string]string{
		"tunnel-churn-a": "gram_tunnel_churn_key_a",
		"tunnel-churn-b": "gram_tunnel_churn_key_b",
		"tunnel-churn-c": "gram_tunnel_churn_key_c",
		"tunnel-churn-d": "gram_tunnel_churn_key_d",
	}
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarness(t, keys, store)
	startChurn := make(chan struct{})
	ready := make(chan struct{}, len(keys))
	results := make(chan error, len(keys))

	for tunnelID, key := range keys {
		go func() {
			for iteration := range 8 {
				agent, response, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
				if err != nil {
					if response != nil && response.StatusCode == http.StatusServiceUnavailable {
						if response.Body != nil {
							_ = response.Body.Close()
						}
						results <- nil
						return
					}
					results <- fmt.Errorf("dial churn agent: %w", err)
					return
				}
				if iteration == 0 {
					if !waitUntil(t.Context(), func() bool {
						connections, _ := store.Connections(t.Context(), tunnelID)
						return len(connections) > 0
					}) {
						agent.Close()
						results <- fmt.Errorf("initial route for %s was not published", tunnelID)
						return
					}
					ready <- struct{}{}
					<-startChurn
				}

				req, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, harness.forward.URL+"/churn", nil)
				if requestErr == nil {
					req.Header.Set(wire.HeaderTunnelID, tunnelID)
					req.Header.Set(wire.HeaderTunnelForwardToken, testForwardToken)
					if forwardResponse, forwardErr := http.DefaultClient.Do(req); forwardErr == nil {
						_ = forwardResponse.Body.Close()
					}
				}
				agent.Close()
			}
			results <- nil
		}()
	}
	for range keys {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			require.FailNow(t, "churn worker did not become ready")
		}
	}
	close(startChurn)
	harness.gateway.Drain(t.Context())
	writesAtDrain := store.writeCount()

	for range keys {
		select {
		case err := <-results:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "churn worker did not finish")
		}
	}
	harness.gateway.CloseSessions(t.Context())
	for tunnelID := range keys {
		candidates, err := store.Candidates(t.Context(), tunnelID)
		require.NoError(t, err)
		require.Empty(t, candidates)
		require.False(t, store.hasOwner(tunnelID, harness.owner))
	}
	require.Never(t, func() bool { return store.writeCount() != writesAtDrain }, 100*time.Millisecond, 5*time.Millisecond)
}

func TestRouteReconcilerOrdersFinalCleanupAfterBlockedPublish(t *testing.T) {
	t.Parallel()

	const key = "gram_tunnel_ordering_key"
	store := newRecordingRouteStore(true, false)
	harness := newGatewayHarness(t, map[string]string{"tunnel-ordering": key}, store)
	t.Cleanup(store.releasePublish)
	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	require.Eventually(t, func() bool { return channelClosed(store.publishStarted) }, 5*time.Second, 10*time.Millisecond)

	drainDone := make(chan struct{})
	go func() {
		harness.gateway.Drain(t.Context())
		close(drainDone)
	}()
	require.Never(t, func() bool { return channelClosed(drainDone) }, 50*time.Millisecond, 5*time.Millisecond)
	store.releasePublish()
	require.Eventually(t, func() bool { return channelClosed(drainDone) }, 5*time.Second, 10*time.Millisecond)

	operations := store.operationsFor("tunnel-ordering")
	require.NotEmpty(t, operations)
	require.Equal(t, "delete_owner", operations[len(operations)-1].kind)
	var publishAt, cleanupAt time.Time
	for _, operation := range operations {
		if operation.kind == "publish" {
			publishAt = operation.at
		}
		if operation.kind == "delete_owner" {
			cleanupAt = operation.at
		}
	}
	require.False(t, publishAt.IsZero())
	require.True(t, cleanupAt.After(publishAt) || cleanupAt.Equal(publishAt))
}

func TestRouteReconcilerRefreshesLiveRouteOnTicker(t *testing.T) {
	t.Parallel()

	const (
		tunnelID = "tunnel-refresh"
		key      = "gram_tunnel_refresh_key"
	)
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarnessWithResolver(
		t,
		NewStaticKeyStore(map[string]string{tunnelID: key}),
		store,
		20*time.Millisecond,
	)
	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)

	require.Eventually(t, func() bool {
		return store.operationCount(tunnelID, "publish") >= 2
	}, time.Second, 5*time.Millisecond)

	operations := store.operationsFor(tunnelID)
	var publishes []time.Time
	for _, operation := range operations {
		if operation.kind == "publish" {
			publishes = append(publishes, operation.at)
		}
	}
	require.GreaterOrEqual(t, len(publishes), 2)
	require.Less(t, publishes[1].Sub(publishes[0]), routeTTL)
}

func TestRouteReconcilerGatesRefreshOnActiveState(t *testing.T) {
	t.Parallel()

	const (
		tunnelID = "tunnel-active-gate"
		key      = "gram_tunnel_active_gate_key"
	)
	keys := newControllableActiveKeyStore(map[string]string{tunnelID: key})
	keys.setActive(true, errors.New("active state unavailable"))
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarnessWithResolver(t, keys, store, 20*time.Millisecond)
	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	require.Eventually(t, func() bool {
		candidates, _ := store.Candidates(t.Context(), tunnelID)
		return len(candidates) == 1
	}, time.Second, 5*time.Millisecond)

	require.NoError(t, store.Delete(t.Context(), tunnelID))
	publishesAfterDelete := store.operationCount(tunnelID, "publish")
	require.Never(t, func() bool {
		return store.operationCount(tunnelID, "publish") != publishesAfterDelete
	}, 75*time.Millisecond, 5*time.Millisecond)

	keys.setActive(false, nil)
	require.Eventually(t, func() bool {
		return harness.gateway.ActiveSessions() == 0
	}, time.Second, 5*time.Millisecond)
	candidates, err := store.Candidates(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestRouteReconcilerValidatesEachSessionKey(t *testing.T) {
	t.Parallel()

	const (
		tunnelID = "tunnel-key-rotation"
		oldKey   = "gram_tunnel_old_rotation_key"
		newKey   = "gram_tunnel_new_rotation_key"
	)
	keys := newControllableActiveKeyStore(map[string]string{tunnelID: oldKey})
	keys.Add(tunnelID, newKey)
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarnessWithResolver(t, keys, store, 20*time.Millisecond)
	oldAgent, _, err := dialTestAgent(t.Context(), harness.public.URL, oldKey, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "old")
	}))
	require.NoError(t, err)
	t.Cleanup(oldAgent.Close)
	require.Eventually(t, func() bool { return harness.gateway.ActiveSessions() == 1 }, time.Second, 5*time.Millisecond)

	newAgent, _, err := dialTestAgent(t.Context(), harness.public.URL, newKey, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "new")
	}))
	require.NoError(t, err)
	t.Cleanup(newAgent.Close)
	require.Eventually(t, func() bool { return harness.gateway.ActiveSessions() == 2 }, time.Second, 5*time.Millisecond)
	keys.setHashActive(wire.HashKey(oldKey), false)

	require.Eventually(t, func() bool {
		return oldAgent.session.IsClosed() && harness.gateway.ActiveSessions() == 1
	}, time.Second, 5*time.Millisecond)
	require.False(t, newAgent.session.IsClosed())
	candidates, err := store.Candidates(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, harness.forward.URL+"/probe", nil)
	require.NoError(t, err)
	request.Header.Set(wire.HeaderTunnelID, tunnelID)
	request.Header.Set(wire.HeaderTunnelForwardToken, testForwardToken)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "new", string(body))
}

func TestRouteReconcilerRetriesFailedCleanupOnTicker(t *testing.T) {
	t.Parallel()

	const (
		tunnelID = "tunnel-cleanup-retry"
		key      = "gram_tunnel_cleanup_retry_key"
	)
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarnessWithResolver(
		t,
		NewStaticKeyStore(map[string]string{tunnelID: key}),
		store,
		20*time.Millisecond,
	)
	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	require.Eventually(t, func() bool {
		candidates, _ := store.Candidates(t.Context(), tunnelID)
		return len(candidates) == 1
	}, time.Second, 5*time.Millisecond)

	store.failNextUnpublish()
	agent.Close()
	require.Eventually(t, func() bool {
		return harness.gateway.ActiveSessions() == 0 && store.operationCount(tunnelID, "unpublish_failed") == 1
	}, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		candidates, candidatesErr := store.Candidates(t.Context(), tunnelID)
		return candidatesErr == nil && len(candidates) == 0 && store.operationCount(tunnelID, "unpublish") == 1
	}, time.Second, 5*time.Millisecond)

	writesAfterRetry := store.writeCount()
	harness.gateway.Drain(t.Context())
	require.Equal(t, writesAfterRetry, store.writeCount())
}

func TestGatewayDrainCleanupIgnoresCanceledFirstCaller(t *testing.T) {
	t.Parallel()

	const (
		tunnelID = "tunnel-canceled-drain"
		key      = "gram_tunnel_canceled_drain_key"
	)
	store := newRecordingRouteStore(false, false)
	harness := newGatewayHarness(t, map[string]string{tunnelID: key}, store)
	agent, _, err := dialTestAgent(t.Context(), harness.public.URL, key, http.HandlerFunc(successfulAgentHandler))
	require.NoError(t, err)
	t.Cleanup(agent.Close)
	require.Eventually(t, func() bool {
		candidates, _ := store.Candidates(t.Context(), tunnelID)
		return len(candidates) == 1
	}, time.Second, 5*time.Millisecond)

	canceledCtx, cancel := context.WithCancel(t.Context())
	cancel()
	harness.gateway.Drain(canceledCtx)
	waitCtx, waitCancel := context.WithTimeout(t.Context(), time.Second)
	defer waitCancel()
	harness.gateway.Drain(waitCtx)

	candidates, err := store.Candidates(t.Context(), tunnelID)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func waitUntil(ctx context.Context, condition func() bool) bool {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}

func channelClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
