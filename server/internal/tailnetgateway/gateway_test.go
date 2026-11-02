package tailnetgateway_test

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn"
	"tailscale.com/tailcfg"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	nirepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type fakeNode struct {
	mu      sync.Mutex
	spec    tailnetgateway.NodeSpec
	online  bool
	fqdn    string
	nodeID  string
	handler http.Handler
	closed  bool
	who     *apitype.WhoIsResponse
}

func (n *fakeNode) Serve(handler http.Handler) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handler = handler
	return nil
}

func (n *fakeNode) Status(context.Context) (tailnetgateway.NodeState, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	state := "Starting"
	if n.online {
		state = "Running"
	}
	return tailnetgateway.NodeState{Online: n.online, BackendState: state, NodeID: n.nodeID, FQDN: n.fqdn}, nil
}

func (n *fakeNode) WhoIs(context.Context, netip.Addr) (*apitype.WhoIsResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.who == nil {
		return nil, errors.New("no such peer")
	}
	return n.who, nil
}

func (n *fakeNode) Close(context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed = true
	return nil
}

func (n *fakeNode) setOnline(nodeID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.online = true
	n.nodeID = nodeID
}

func (n *fakeNode) isClosed() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.closed
}

type fakeRuntime struct {
	mu        sync.Mutex
	nodes     []*fakeNode
	failStart error
	// writePartial makes a failing start leave a machine key behind, like a
	// real login that timed out after tsnet persisted its keys.
	writePartial bool
}

func (r *fakeRuntime) Start(_ context.Context, spec tailnetgateway.NodeSpec) (tailnetgateway.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failStart != nil {
		if r.writePartial {
			if err := spec.Store.WriteState(ipn.MachineKeyStateKey, []byte("machine-key")); err != nil {
				return nil, fmt.Errorf("persist partial fake node state: %w", err)
			}
		}
		return nil, r.failStart
	}
	// A real node writes its machine key and then, once login completes, its
	// profile list into the store.
	if err := spec.Store.WriteState(ipn.MachineKeyStateKey, []byte("machine-key")); err != nil {
		return nil, fmt.Errorf("persist fake node state: %w", err)
	}
	if err := spec.Store.WriteState(ipn.KnownProfilesStateKey, []byte(`{"profile-1":{"ID":"profile-1","Name":"speakeasy-gw-0"}}`)); err != nil {
		return nil, fmt.Errorf("persist fake node profile: %w", err)
	}
	node := &fakeNode{mu: sync.Mutex{}, spec: spec, online: false, fqdn: "speakeasy.example.ts.net", nodeID: "", handler: nil, closed: false, who: nil}
	r.nodes = append(r.nodes, node)
	return node, nil
}

func (r *fakeRuntime) started() []*fakeNode {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*fakeNode(nil), r.nodes...)
}

type gatewayFixture struct {
	db       *pgxpool.Pool
	enc      *encryption.Client
	runtime  *fakeRuntime
	gateway  *tailnetgateway.Gateway
	upstream *upstreamCapture
}

type upstreamCapture struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (u *upstreamCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.requests = append(u.requests, r.Clone(context.Background()))
	u.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (u *upstreamCapture) seen() []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]*http.Request(nil), u.requests...)
}

func newGatewayFixture(t *testing.T, name string, ordinal int32) *gatewayFixture {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	enc := testenv.NewEncryptionClient(t)

	capture := &upstreamCapture{mu: sync.Mutex{}, requests: nil}
	upstream := httptest.NewTLSServer(capture)
	t.Cleanup(upstream.Close)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw, Headers: nil})
	transport, err := netingress.NewAttestorTransport(caPEM)
	require.NoError(t, err)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	tokenPath := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("projected-token"), 0o600))

	runtime := &fakeRuntime{mu: sync.Mutex{}, nodes: nil, failStart: nil, writePartial: false}
	gateway, err := tailnetgateway.New(testenv.NewLogger(t), testenv.NewMeterProvider(t), db, enc, tailnetgateway.Config{
		Ordinal: ordinal, PodName: "gram-tailnet-gateway-0", Shard: tailnetgateway.DefaultShard, StateDir: t.TempDir(),
		Tag: tailnetgateway.DefaultTag, Upstream: upstreamURL, UpstreamTransport: transport, TokenPath: tokenPath,
		PollInterval: tailnetgateway.DefaultPollInterval, DrainTimeout: time.Second, StartTimeout: 5 * time.Second,
		MaxHosts: 10, ReconcileTaskQueue: "test-tailnet-gateway", ControlURL: "",
	}, runtime)
	require.NoError(t, err)
	return &gatewayFixture{db: db, enc: enc, runtime: runtime, gateway: gateway, upstream: capture}
}

func (f *gatewayFixture) seedSlot(t *testing.T, ingressID uuid.UUID, orgID string, slot int32, authKey string) {
	t.Helper()
	ctx := t.Context()
	queries := repo.New(f.db)
	require.NoError(t, queries.EnsureNetworkIngressNodeSlot(ctx, repo.EnsureNetworkIngressNodeSlotParams{NetworkIngressID: ingressID, OrganizationID: orgID, Slot: slot}))
	if authKey == "" {
		return
	}
	ciphertext, err := f.enc.Encrypt([]byte(authKey))
	require.NoError(t, err)
	rows, err := queries.SetNetworkIngressNodeAuthKey(ctx, repo.SetNetworkIngressNodeAuthKeyParams{
		AuthKeyEncrypted: conv.ToPGText(ciphertext), AuthKeyExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		NetworkIngressID: ingressID, OrganizationID: orgID, Slot: slot,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
}

func (f *gatewayFixture) slotRow(t *testing.T, ingressID uuid.UUID, orgID string, slot int32) repo.NetworkIngressNode {
	t.Helper()
	row, err := repo.New(f.db).GetNetworkIngressNodeSlot(t.Context(), repo.GetNetworkIngressNodeSlotParams{NetworkIngressID: ingressID, OrganizationID: orgID, Slot: slot})
	require.NoError(t, err)
	return row
}

func (f *gatewayFixture) waitForHosts(t *testing.T, count int) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Len(c, f.gateway.Hosts(), count)
	}, 10*time.Second, 20*time.Millisecond)
}

func TestGatewayHostsIngressAtOrdinalAndHeartbeats(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, "tailnetgatewayhost", 0)
	ingressID, orgID := seedIngress(t, f.db, "gateway-host")
	f.seedSlot(t, ingressID, orgID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)

	nodes := f.runtime.started()
	require.Len(t, nodes, 1)
	require.Equal(t, "speakeasy-gw-0", nodes[0].spec.Hostname)
	require.Equal(t, "svc:private", nodes[0].spec.ServiceName, "service name follows the ingress label")
	require.Equal(t, "tag:speakeasy", nodes[0].spec.Tag)
	require.Equal(t, "tskey-auth-one-shot", nodes[0].spec.AuthKey)
	require.EqualValues(t, 0, nodes[0].spec.Slot)

	row := f.slotRow(t, ingressID, orgID, 0)
	require.Equal(t, string(tailnetgateway.NodeStatusStarting), row.Status)
	require.True(t, row.AuthKeyEncrypted.Valid, "key stays until the node is online")
	require.Equal(t, "speakeasy.example.ts.net", row.DnsName.String)
	require.Equal(t, "gram-tailnet-gateway-0", row.PodName.String)

	nodes[0].setOnline("nSTABLE")
	f.gateway.Poll(ctx)
	row = f.slotRow(t, ingressID, orgID, 0)
	require.Equal(t, string(tailnetgateway.NodeStatusOnline), row.Status)
	require.Equal(t, "nSTABLE", row.NodeID.String)
	require.True(t, row.ConnectedSince.Valid)
	require.True(t, row.LastSeenAt.Valid)
	require.False(t, row.AuthKeyEncrypted.Valid, "one-shot key is cleared once state exists")

	messages, err := nirepo.New(f.db).ListNetworkIngressReconcileRequests(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, messages, 1, "coming online wakes the reconciler once")

	f.gateway.Poll(ctx)
	messages, err = nirepo.New(f.db).ListNetworkIngressReconcileRequests(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, messages, 1, "steady state does not wake the reconciler again")
	require.Len(t, f.runtime.started(), 1, "a hosted ingress is not started twice")
}

func TestGatewayWaitsForAuthKeyAndReconnectsWithState(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, "tailnetgatewaykey", 0)
	ingressID, orgID := seedIngress(t, f.db, "gateway-key")
	f.seedSlot(t, ingressID, orgID, 0, "")

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.slotRow(t, ingressID, orgID, 0)
		assert.Equal(c, string(tailnetgateway.NodeStatusPending), row.Status)
		assert.Equal(c, "awaiting auth key", row.LastError.String)
	}, 5*time.Second, 20*time.Millisecond)
	require.Empty(t, f.runtime.started())
	require.Empty(t, f.gateway.Hosts())

	store, err := tailnetgateway.NewPostgresStateStore(f.db, f.enc, ingressID, orgID, 0)
	require.NoError(t, err)
	require.NoError(t, store.WriteState(ipn.MachineKeyStateKey, []byte("existing")))
	require.NoError(t, store.WriteState(ipn.KnownProfilesStateKey, []byte(`{"profile-1":{"ID":"profile-1","Name":"speakeasy-gw-0"}}`)))

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	nodes := f.runtime.started()
	require.Len(t, nodes, 1)
	require.Empty(t, nodes[0].spec.AuthKey, "persisted state reconnects without a key")
}

func TestGatewayStopsHostWhenIngressIsDisabled(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, "tailnetgatewaystop", 0)
	ingressID, orgID := seedIngress(t, f.db, "gateway-stop")
	f.seedSlot(t, ingressID, orgID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	node := f.runtime.started()[0]
	node.setOnline("nSTABLE")
	f.gateway.Poll(ctx)

	require.NoError(t, testrepo.New(f.db).SetNetworkIngressEnabledFixture(ctx, testrepo.SetNetworkIngressEnabledFixtureParams{Enabled: false, ID: ingressID}))
	f.gateway.Poll(ctx)
	require.Empty(t, f.gateway.Hosts())
	require.True(t, node.isClosed())
	row := f.slotRow(t, ingressID, orgID, 0)
	require.Equal(t, string(tailnetgateway.NodeStatusStopped), row.Status)
	require.False(t, row.ConnectedSince.Valid)

	messages, err := nirepo.New(f.db).ListNetworkIngressReconcileRequests(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, messages, 2, "going online and offline each wake the reconciler")
}

func TestGatewaySkipsRetiredSlotsAndForeignOrdinals(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, "tailnetgatewayretired", 2)
	ingressID, orgID := seedIngress(t, f.db, "gateway-retired")
	f.seedSlot(t, ingressID, orgID, 0, "tskey-auth-slot0")

	f.gateway.Poll(ctx)
	require.Never(t, func() bool { return len(f.gateway.Hosts()) > 0 }, 200*time.Millisecond, 20*time.Millisecond)
	require.Empty(t, f.runtime.started(), "ordinal 2 has no slot row")

	f.seedSlot(t, ingressID, orgID, 2, "tskey-auth-slot2")
	require.NoError(t, repo.New(f.db).RetireNetworkIngressNodes(ctx, repo.RetireNetworkIngressNodesParams{NetworkIngressID: ingressID, OrganizationID: orgID, MinSlot: 2}))
	f.gateway.Poll(ctx)
	require.Never(t, func() bool { return len(f.gateway.Hosts()) > 0 }, 200*time.Millisecond, 20*time.Millisecond)
	require.Empty(t, f.runtime.started(), "retired slots are never hosted")
}

func TestGatewayStopsRunningHostWhenSlotIsRetired(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, "tailnetgatewayretire", 0)
	ingressID, orgID := seedIngress(t, f.db, "gateway-retire")
	f.seedSlot(t, ingressID, orgID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	node := f.runtime.started()[0]

	require.NoError(t, repo.New(f.db).RetireNetworkIngressNodes(ctx, repo.RetireNetworkIngressNodesParams{NetworkIngressID: ingressID, OrganizationID: orgID, MinSlot: 0}))
	f.gateway.Poll(ctx)
	require.Empty(t, f.gateway.Hosts(), "a retired slot is no longer desired")
	require.True(t, node.isClosed())
	row := f.slotRow(t, ingressID, orgID, 0)
	require.Equal(t, string(tailnetgateway.NodeStatusStopped), row.Status)
}

func TestGatewayResetsPartialStateAfterFailedRegistration(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, "tailnetgatewaypartial", 0)
	f.runtime.failStart = errors.New("login timed out")
	f.runtime.writePartial = true
	ingressID, orgID := seedIngress(t, f.db, "gateway-partial")
	f.seedSlot(t, ingressID, orgID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.slotRow(t, ingressID, orgID, 0)
		assert.Equal(c, string(tailnetgateway.NodeStatusError), row.Status)
		assert.False(c, row.AuthKeyEncrypted.Valid, "a consumed one-shot key is cleared so the reconciler mints another")
	}, 5*time.Second, 20*time.Millisecond)
	store, err := tailnetgateway.NewPostgresStateStore(f.db, f.enc, ingressID, orgID, 0)
	require.NoError(t, err)
	_, err = store.ReadState(ipn.MachineKeyStateKey)
	require.ErrorIs(t, err, ipn.ErrStateNotExist, "partial state is reset")
	messages, err := nirepo.New(f.db).ListNetworkIngressReconcileRequests(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, messages, 1, "the reconciler is woken to mint a new key")
}

func TestGatewayRecordsStartFailures(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, "tailnetgatewayfail", 0)
	f.runtime.failStart = errors.New("control plane unreachable")
	ingressID, orgID := seedIngress(t, f.db, "gateway-fail")
	f.seedSlot(t, ingressID, orgID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		row := f.slotRow(t, ingressID, orgID, 0)
		assert.Equal(c, string(tailnetgateway.NodeStatusError), row.Status)
		assert.Contains(c, row.LastError.String, "control plane unreachable")
	}, 5*time.Second, 20*time.Millisecond)
	require.Empty(t, f.gateway.Hosts())
}

func TestGatewayProxiesWithIdentityAndAttestation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newGatewayFixture(t, "tailnetgatewayproxy", 0)
	ingressID, orgID := seedIngress(t, f.db, "gateway-proxy")
	f.seedSlot(t, ingressID, orgID, 0, "tskey-auth-one-shot")

	f.gateway.Poll(ctx)
	f.waitForHosts(t, 1)
	node := f.runtime.started()[0]
	node.mu.Lock()
	handler := node.handler
	node.who = &apitype.WhoIsResponse{Node: &tailcfg.Node{}, UserProfile: &tailcfg.UserProfile{LoginName: "alice@example.com", DisplayName: "Alice"}}
	node.mu.Unlock()
	require.NotNil(t, handler)

	request := httptest.NewRequest(http.MethodGet, "https://speakeasy.example.ts.net/mcp/demo", nil)
	request.RemoteAddr = "127.0.0.1:40001"
	request.Header.Set("X-Forwarded-For", "100.64.0.9")
	request.Header.Set(netingress.TailscaleUserLoginHeader, "spoofed@example.com")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code)

	seen := f.upstream.seen()
	require.Len(t, seen, 1)
	require.Equal(t, "speakeasy.example.ts.net", seen[0].Host)
	require.Equal(t, "Bearer projected-token", seen[0].Header.Get(netingress.AttestationHeader))
	require.Equal(t, "alice@example.com", seen[0].Header.Get(netingress.TailscaleUserLoginHeader))
	require.Equal(t, "Alice", seen[0].Header.Get(netingress.TailscaleUserNameHeader))

	// A request for another host is refused before reaching the listener.
	other := httptest.NewRequest(http.MethodGet, "https://other.example.ts.net/mcp/demo", nil)
	other.RemoteAddr = "127.0.0.1:40002"
	other.Header.Set("X-Forwarded-For", "100.64.0.9")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, other)
	require.Equal(t, http.StatusNotFound, recorder.Code)

	// A peer WhoIs cannot resolve is refused.
	node.mu.Lock()
	node.who = nil
	node.mu.Unlock()
	unknown := httptest.NewRequest(http.MethodGet, "https://speakeasy.example.ts.net/mcp/demo", nil)
	unknown.RemoteAddr = "127.0.0.1:40003"
	unknown.Header.Set("X-Forwarded-For", "100.64.0.10")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, unknown)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Len(t, f.upstream.seen(), 1)
}

func TestGatewayRunDrainsOnCancel(t *testing.T) {
	t.Parallel()
	f := newGatewayFixture(t, "tailnetgatewaydrain", 0)
	ingressID, orgID := seedIngress(t, f.db, "gateway-drain")
	f.seedSlot(t, ingressID, orgID, 0, "tskey-auth-one-shot")

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.gateway.Run(ctx) }()
	f.waitForHosts(t, 1)
	require.True(t, f.gateway.Ready())
	require.NoError(t, f.gateway.Healthy())

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("gateway did not drain")
	}
	require.False(t, f.gateway.Ready())
	require.True(t, f.runtime.started()[0].isClosed())
	row := f.slotRow(t, ingressID, orgID, 0)
	require.Equal(t, string(tailnetgateway.NodeStatusStopped), row.Status)
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()
	valid := tailnetgateway.Config{
		Ordinal: 0, PodName: "", Shard: "shared", StateDir: "/tmp/x", Tag: "tag:speakeasy",
		Upstream: &url.URL{Scheme: "https", Host: "listener"}, UpstreamTransport: http.DefaultTransport, TokenPath: "/token",
		PollInterval: time.Second, DrainTimeout: time.Second, StartTimeout: time.Second, MaxHosts: 1, ReconcileTaskQueue: "", ControlURL: "",
	}
	require.NoError(t, valid.Validate())
	cases := map[string]func(c *tailnetgateway.Config){
		"negative ordinal": func(c *tailnetgateway.Config) { c.Ordinal = -1 },
		"empty shard":      func(c *tailnetgateway.Config) { c.Shard = "" },
		"bad tag":          func(c *tailnetgateway.Config) { c.Tag = "speakeasy" },
		"http upstream":    func(c *tailnetgateway.Config) { c.Upstream = &url.URL{Scheme: "http", Host: "listener"} },
		"upstream path":    func(c *tailnetgateway.Config) { c.Upstream = &url.URL{Scheme: "https", Host: "listener", Path: "/mcp"} },
		"upstream query": func(c *tailnetgateway.Config) {
			c.Upstream = &url.URL{Scheme: "https", Host: "listener", RawQuery: "x=1"}
		},
		"no transport": func(c *tailnetgateway.Config) { c.UpstreamTransport = nil },
		"no token":     func(c *tailnetgateway.Config) { c.TokenPath = "" },
		"zero hosts":   func(c *tailnetgateway.Config) { c.MaxHosts = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := valid
			mutate(&cfg)
			require.Error(t, cfg.Validate())
		})
	}
}
