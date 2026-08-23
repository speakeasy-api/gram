package gateway

import (
	"context"
	"log/slog"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSource scripts ListEnabledIngresses and records status writes.
type fakeSource struct {
	mu       sync.Mutex
	rows     []IngressConfig
	statuses map[uuid.UUID][]string
	nulled   map[uuid.UUID]int
}

func newFakeSource(rows ...IngressConfig) *fakeSource {
	return &fakeSource{mu: sync.Mutex{}, rows: rows, statuses: map[uuid.UUID][]string{}, nulled: map[uuid.UUID]int{}}
}

func (f *fakeSource) ListEnabledIngresses(context.Context) ([]IngressConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]IngressConfig, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

func (f *fakeSource) SetStatus(_ context.Context, id uuid.UUID, status string, _ NodeStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[id] = append(f.statuses[id], status)
	return nil
}

func (f *fakeSource) NullAuthKey(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nulled[id]++
	return nil
}

func (f *fakeSource) NodeState(uuid.UUID) StateStore {
	return &memState{mu: sync.Mutex{}, m: map[string][]byte{}}
}

func (f *fakeSource) setRows(rows ...IngressConfig) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = rows
}

func (f *fakeSource) lastStatus(id uuid.UUID) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.statuses[id]
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}

type memState struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *memState) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return nil, ErrStateNotFound
	}
	return v, nil
}

func (s *memState) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}

// fakeLease grants every claim and can be told to drop claims.
type fakeLease struct {
	mu       sync.Mutex
	lost     map[uuid.UUID]bool
	released map[uuid.UUID]int
	claimed  map[uuid.UUID]int
}

func newFakeLease() *fakeLease {
	return &fakeLease{mu: sync.Mutex{}, lost: map[uuid.UUID]bool{}, released: map[uuid.UUID]int{}, claimed: map[uuid.UUID]int{}}
}

func (f *fakeLease) Claim(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimed[id]++
	return true, nil
}

func (f *fakeLease) Heartbeat(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.lost[id], nil
}

func (f *fakeLease) Release(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released[id]++
	return nil
}

// fakeProvider hands out nodes whose listeners accept on loopback.
type fakeProvider struct {
	mu    sync.Mutex
	nodes map[uuid.UUID]*supervisedFakeNode
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{mu: sync.Mutex{}, nodes: map[uuid.UUID]*supervisedFakeNode{}}
}

func (f *fakeProvider) Name() string { return "tailscale" }

func (f *fakeProvider) NewNode(_ context.Context, cfg IngressConfig, _ StateStore) (Node, error) {
	node := &supervisedFakeNode{mu: sync.Mutex{}, closed: 0, ln: nil}
	f.mu.Lock()
	f.nodes[cfg.ID] = node
	f.mu.Unlock()
	return node, nil
}

func (f *fakeProvider) node(id uuid.UUID) *supervisedFakeNode {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nodes[id]
}

type supervisedFakeNode struct {
	mu     sync.Mutex
	closed int
	ln     net.Listener
}

func (n *supervisedFakeNode) Start(context.Context) error { return nil }

func (n *supervisedFakeNode) Listener(context.Context) (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	n.mu.Lock()
	n.ln = ln
	n.mu.Unlock()
	return ln, nil
}

func (n *supervisedFakeNode) Identity(context.Context, string) (*PeerIdentity, error) {
	return nil, nil
}

func (n *supervisedFakeNode) Status(context.Context) NodeStatus {
	return NodeStatus{Online: true, NetworkName: "example.ts.net", DNSName: "gram-mcp.example.ts.net", NodeID: "n1", Err: ""}
}

func (n *supervisedFakeNode) Close(context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed++
	return nil
}

func (n *supervisedFakeNode) closeCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.closed
}

func newTestSupervisor(source IngressSource, lease LeaseManager, provider Provider) *Supervisor {
	upstream, _ := url.Parse("http://gram-server.internal")
	return NewSupervisor(SupervisorConfig{
		Source:       source,
		Lease:        lease,
		Providers:    map[string]Provider{provider.Name(): provider},
		Upstream:     upstream,
		ForwardToken: "sekrit",
		MaxNodes:     8,
		Logger:       slog.New(slog.DiscardHandler),
	})
}

func waitForStatus(t *testing.T, source *fakeSource, id uuid.UUID, want string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, want, source.lastStatus(id))
	}, 5*time.Second, 10*time.Millisecond)
}

func TestSupervisorStartsAndStopsNodes(t *testing.T) {
	t.Parallel()

	cfg := testIngressConfig(false)
	source := newFakeSource(cfg)
	lease := newFakeLease()
	provider := newFakeProvider()
	sup := newTestSupervisor(source, lease, provider)

	ctx := t.Context()
	sup.reconcile(ctx)
	waitForStatus(t, source, cfg.ID, "online")
	// The one-shot auth key is spent once the join is durable.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		source.mu.Lock()
		defer source.mu.Unlock()
		assert.Equal(c, 0, source.nulled[cfg.ID])
	}, time.Second, 10*time.Millisecond)

	// Row disappears: node stops, lease releases, status records disabled.
	source.setRows()
	sup.reconcile(ctx)
	waitForStatus(t, source, cfg.ID, "disabled")
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, provider.node(cfg.ID).closeCount())
	}, 5*time.Second, 10*time.Millisecond)
	lease.mu.Lock()
	released := lease.released[cfg.ID]
	lease.mu.Unlock()
	require.GreaterOrEqual(t, released, 1)
}

func TestSupervisorRestartsOnRowChange(t *testing.T) {
	t.Parallel()

	cfg := testIngressConfig(false)
	source := newFakeSource(cfg)
	lease := newFakeLease()
	provider := newFakeProvider()
	sup := newTestSupervisor(source, lease, provider)

	ctx := t.Context()
	sup.reconcile(ctx)
	waitForStatus(t, source, cfg.ID, "online")
	first := provider.node(cfg.ID)

	// A newer updated_at means the row changed; the node restarts.
	changed := cfg
	changed.UpdatedAt = cfg.UpdatedAt.Add(time.Second)
	source.setRows(changed)
	sup.reconcile(ctx)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, first.closeCount())
	}, 5*time.Second, 10*time.Millisecond)
	waitForStatus(t, source, cfg.ID, "online")
}

func TestSupervisorStopsNodeOnLostLease(t *testing.T) {
	t.Parallel()

	cfg := testIngressConfig(false)
	source := newFakeSource(cfg)
	lease := newFakeLease()
	provider := newFakeProvider()
	sup := newTestSupervisor(source, lease, provider)

	ctx := t.Context()
	sup.reconcile(ctx)
	waitForStatus(t, source, cfg.ID, "online")

	lease.mu.Lock()
	lease.lost[cfg.ID] = true
	lease.mu.Unlock()
	sup.reconcile(ctx)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, provider.node(cfg.ID).closeCount())
	}, 5*time.Second, 10*time.Millisecond)
}

func TestSupervisorSpendsAuthKeyAfterJoin(t *testing.T) {
	t.Parallel()

	cfg := testIngressConfig(false)
	cfg.Credential.AuthKey = "tskey-auth-test"
	source := newFakeSource(cfg)
	sup := newTestSupervisor(source, newFakeLease(), newFakeProvider())

	sup.reconcile(t.Context())
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		source.mu.Lock()
		defer source.mu.Unlock()
		assert.Equal(c, 1, source.nulled[cfg.ID])
	}, 5*time.Second, 10*time.Millisecond)
}
