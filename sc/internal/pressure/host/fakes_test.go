package host_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/sc/internal/pressure/host"
)

// Persisted state keys a real tsnet node writes.
const (
	machineKeyStateKey    = "_machinekey"
	knownProfilesStateKey = "_profiles"
)

var errStateNotExist = errors.New("state does not exist")

type slotKey struct {
	ingress uuid.UUID
	slot    int32
}

type ingressRow struct {
	orgID, orgSlug, label, runtime, shard string
	enabled                               bool
}

// slotRow mirrors network_ingress_nodes.
type slotRow struct {
	status, podName, nodeID, dnsName, lastError string
	authKey                                     *string
	authKeyExpiresAt                            time.Time
	lastSeenAt, connectedSince, retiredAt       time.Time
}

type wakeRequest struct {
	orgID   string
	ingress uuid.UUID
}

// fakeDB is the gateway's view of Postgres: the ingress and slot tables,
// node state, the reconcile outbox, and organization encryption. Its
// methods reproduce the SQL the original tests relied on.
type fakeDB struct {
	mu        sync.Mutex
	ingresses map[uuid.UUID]*ingressRow
	slots     map[slotKey]*slotRow
	states    map[slotKey]map[string][]byte
	wakes     []wakeRequest
	suspended map[string]bool
	seq       int
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		ingresses: map[uuid.UUID]*ingressRow{}, slots: map[slotKey]*slotRow{},
		states: map[slotKey]map[string][]byte{}, suspended: map[string]bool{},
	}
}

var _ host.Store = (*fakeDB)(nil)

func (db *fakeDB) seedIngress(name string) (uuid.UUID, string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.seq++
	id := uuid.New()
	org := fmt.Sprintf("org-%d-%s", db.seq, name)
	db.ingresses[id] = &ingressRow{orgID: org, orgSlug: name, label: "private", runtime: "gateway", shard: host.DefaultShard, enabled: true}
	return id, org
}

func (db *fakeDB) setEnabled(id uuid.UUID, enabled bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.ingresses[id].enabled = enabled
}

func (db *fakeDB) registered(key slotKey) bool {
	raw, ok := db.states[key][knownProfilesStateKey]
	if !ok {
		return false
	}
	var byID map[string]json.RawMessage
	if json.Unmarshal(raw, &byID) == nil {
		return len(byID) > 0
	}
	var list []json.RawMessage
	return json.Unmarshal(raw, &list) == nil && len(list) > 0
}

// seedSlot is EnsureNetworkIngressNodeSlot plus, for a non-empty key,
// SetNetworkIngressNodeAuthKey with an hour to live.
func (db *fakeDB) seedSlot(id uuid.UUID, slot int32, authKey string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := slotKey{id, slot}
	if _, ok := db.slots[key]; !ok {
		db.slots[key] = &slotRow{status: string(host.NodeStatusPending)}
	}
	row := db.slots[key]
	if authKey == "" || !row.retiredAt.IsZero() || db.registered(key) {
		return
	}
	sealed := "sealed:" + authKey
	row.authKey, row.authKeyExpiresAt, row.status, row.lastError = &sealed, time.Now().Add(time.Hour), string(host.NodeStatusPending), ""
}

func (db *fakeDB) retire(id uuid.UUID, minSlot int32) {
	db.mu.Lock()
	defer db.mu.Unlock()
	for key, row := range db.slots {
		if key.ingress == id && key.slot >= minSlot && row.retiredAt.IsZero() {
			row.retiredAt = time.Now()
		}
	}
}

func (db *fakeDB) suspend(org string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.suspended[org] = true
}

func (db *fakeDB) row(id uuid.UUID, slot int32) slotRow {
	db.mu.Lock()
	defer db.mu.Unlock()
	return *db.slots[slotKey{id, slot}]
}

func (db *fakeDB) wakesFor(org string) int {
	db.mu.Lock()
	defer db.mu.Unlock()
	n := 0
	for _, w := range db.wakes {
		if w.orgID == org {
			n++
		}
	}
	return n
}

func (db *fakeDB) ListDesired(_ context.Context, slot int32, shard string) ([]host.Ingress, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var out []host.Ingress
	for id, ing := range db.ingresses {
		row, ok := db.slots[slotKey{id, slot}]
		if !ok || !row.retiredAt.IsZero() || !ing.enabled || ing.runtime != "gateway" || ing.shard != shard {
			continue
		}
		out = append(out, host.Ingress{ID: id, OrganizationID: ing.orgID, OrganizationSlug: ing.orgSlug, Label: ing.label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.String() < out[j].ID.String() })
	return out, nil
}

func (db *fakeDB) GetSlot(_ context.Context, id uuid.UUID, org string, slot int32) (host.SlotRow, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	row, ok := db.slots[slotKey{id, slot}]
	if !ok || db.ingresses[id].orgID != org {
		return host.SlotRow{}, host.ErrSlotNotFound
	}
	out := host.SlotRow{Status: row.status, HasAuthKey: row.authKey != nil, AuthKeyExpiresAt: row.authKeyExpiresAt, Retired: !row.retiredAt.IsZero()}
	if row.authKey != nil {
		out.AuthKeyEncrypted = *row.authKey
	}
	return out, nil
}

func (db *fakeDB) ClearAuthKey(_ context.Context, id uuid.UUID, _ string, slot int32) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if row, ok := db.slots[slotKey{id, slot}]; ok {
		row.authKey, row.authKeyExpiresAt = nil, time.Time{}
	}
	return nil
}

// Heartbeat reproduces HeartbeatNetworkIngressNode: node id and DNS name are
// kept when the beat has none, connected_since is set while online, and a
// retired row only takes draining and stopped beats.
func (db *fakeDB) Heartbeat(_ context.Context, b host.Heartbeat) (int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	row, ok := db.slots[slotKey{b.IngressID, b.Slot}]
	if !ok || (!row.retiredAt.IsZero() && b.Status != host.NodeStatusDraining && b.Status != host.NodeStatusStopped) {
		return 0, nil
	}
	now := time.Now()
	row.podName, row.status, row.lastError, row.lastSeenAt = b.PodName, string(b.Status), b.LastError, now
	if b.NodeID != "" {
		row.nodeID = b.NodeID
	}
	if b.DNSName != "" {
		row.dnsName = b.DNSName
	}
	switch {
	case b.Status != host.NodeStatusOnline:
		row.connectedSince = time.Time{}
	case row.connectedSince.IsZero():
		row.connectedSince = now
	}
	return 1, nil
}

func (db *fakeDB) EnqueueReconcile(_ context.Context, org string, id uuid.UUID) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.wakes = append(db.wakes, wakeRequest{orgID: org, ingress: id})
	return nil
}

func (db *fakeDB) NodeStateStore(id uuid.UUID, org string, slot int32) (host.NodeStateStore, error) {
	return &fakeStateStore{db: db, key: slotKey{id, slot}, org: org}, nil
}

func (db *fakeDB) DecryptAuthKey(_ context.Context, org, ciphertext string) (string, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.suspended[org] {
		return "", fmt.Errorf("decrypt auth key: %w", host.ErrEncryptionKeyUnavailable)
	}
	plain, ok := strings.CutPrefix(ciphertext, "sealed:")
	if !ok {
		return "", errors.New("decrypt auth key: invalid ciphertext")
	}
	return plain, nil
}

// fakeStateStore is one slot's network_ingress_node_states rows.
type fakeStateStore struct {
	db  *fakeDB
	key slotKey
	org string
}

func (s *fakeStateStore) ReadState(key string) ([]byte, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	value, ok := s.db.states[s.key][key]
	if !ok {
		return nil, errStateNotExist
	}
	return value, nil
}

func (s *fakeStateStore) WriteState(key string, value []byte) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	if s.db.states[s.key] == nil {
		s.db.states[s.key] = map[string][]byte{}
	}
	s.db.states[s.key][key] = value
	return nil
}

func (s *fakeStateStore) Registered(context.Context) (bool, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	if s.db.suspended[s.org] {
		if _, ok := s.db.states[s.key][knownProfilesStateKey]; ok {
			return false, fmt.Errorf("decrypt tailnet login profiles: %w", host.ErrEncryptionKeyUnavailable)
		}
	}
	return s.db.registered(s.key), nil
}

func (s *fakeStateStore) Reset(context.Context) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	delete(s.db.states, s.key)
	return nil
}

type fakeNode struct {
	mu            sync.Mutex
	spec          host.NodeSpec
	online        bool
	unapproved    bool
	uncertified   string
	certExpiresAt time.Time
	needsLogin    bool
	statusErr     error
	closeDelay    time.Duration
	fqdn          string
	nodeID        string
	handler       http.Handler
	closed        bool
}

func (n *fakeNode) Serve(handler http.Handler) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handler = handler
	return nil
}

func (n *fakeNode) Status(context.Context) (host.NodeState, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.statusErr != nil {
		return host.NodeState{}, n.statusErr
	}
	state := "Starting"
	switch {
	case n.needsLogin:
		state = "NeedsLogin"
	case n.online:
		state = "Running"
	}
	running := n.online && !n.needsLogin
	certified := n.uncertified == ""
	return host.NodeState{
		Online: running && !n.unapproved && certified, BackendState: state, NeedsLogin: n.needsLogin,
		AwaitingApproval:    running && n.unapproved,
		AwaitingCertificate: running && !n.unapproved && !certified, CertificateStatus: n.uncertified, CertificateExpiresAt: n.certExpiresAt,
		NodeID: n.nodeID, FQDN: n.fqdn,
	}, nil
}

func (n *fakeNode) Close(ctx context.Context) error {
	n.mu.Lock()
	delay := n.closeDelay
	n.mu.Unlock()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed = true
	return nil
}

func (n *fakeNode) setApproved(approved bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.unapproved = !approved
}

func (n *fakeNode) setUncertified(reason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.uncertified = reason
}

func (n *fakeNode) failStatus(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.statusErr = err
}

func (n *fakeNode) setNeedsLogin() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.needsLogin = true
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
	mu           sync.Mutex
	nodes        []*fakeNode
	failStart    error
	writePartial bool
	writeLogin   bool
	closeDelay   time.Duration
}

const profileList = `{"profile-1":{"ID":"profile-1","Name":"speakeasy-gw-0"}}`

func (r *fakeRuntime) Start(_ context.Context, spec host.NodeSpec) (host.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failStart != nil {
		if r.writePartial || r.writeLogin {
			if err := spec.Store.WriteState(machineKeyStateKey, []byte("machine-key")); err != nil {
				return nil, fmt.Errorf("persist partial fake node state: %w", err)
			}
		}
		if r.writeLogin {
			if err := spec.Store.WriteState(knownProfilesStateKey, []byte(profileList)); err != nil {
				return nil, fmt.Errorf("persist fake node profile: %w", err)
			}
		}
		return nil, r.failStart
	}
	if err := spec.Store.WriteState(machineKeyStateKey, []byte("machine-key")); err != nil {
		return nil, fmt.Errorf("persist fake node state: %w", err)
	}
	if err := spec.Store.WriteState(knownProfilesStateKey, []byte(profileList)); err != nil {
		return nil, fmt.Errorf("persist fake node profile: %w", err)
	}
	node := &fakeNode{spec: spec, closeDelay: r.closeDelay, fqdn: "speakeasy.example.ts.net"}
	r.nodes = append(r.nodes, node)
	return node, nil
}

func (r *fakeRuntime) setFailStart(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failStart = err
}

func (r *fakeRuntime) started() []*fakeNode {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*fakeNode(nil), r.nodes...)
}

type gaugeKey struct {
	org    string
	status host.NodeStatus
}

type certRequest struct{ operation, result string }

// fakeMetrics records what the gateway reports.
type fakeMetrics struct {
	mu        sync.Mutex
	hosts     map[gaugeKey]int
	lifecycle map[string]int
	certs     []certRequest
	observe   func() []host.CertificateObservation
}

func newFakeMetrics() *fakeMetrics {
	return &fakeMetrics{hosts: map[gaugeKey]int{}, lifecycle: map[string]int{}}
}

func (m *fakeMetrics) HostStatusChanged(_ context.Context, org string, _ int32, previous, next host.NodeStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if previous == next {
		return
	}
	if previous != "" {
		m.hosts[gaugeKey{org, previous}]--
	}
	if next != "" {
		m.hosts[gaugeKey{org, next}]++
	}
}

func (m *fakeMetrics) Lifecycle(_ context.Context, _ string, op host.Operation, result host.Result) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lifecycle[string(op)+"/"+string(result)]++
}

func (m *fakeMetrics) lifecycleCount(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lifecycle[key]
}

func (m *fakeMetrics) StartDuration(context.Context, string, bool, time.Duration) {}

func (m *fakeMetrics) Poll(context.Context, time.Duration) {}

func (m *fakeMetrics) CertificateRequest(_ context.Context, _, operation, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.certs = append(m.certs, certRequest{operation, result})
}

func (m *fakeMetrics) ObserveCertificates(list func() []host.CertificateObservation) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observe = list
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.observe = nil
	}, nil
}

// collect is one metrics collection of the certificate gauge.
func (m *fakeMetrics) collect() []host.CertificateObservation {
	m.mu.Lock()
	list := m.observe
	m.mu.Unlock()
	if list == nil {
		return nil
	}
	return list()
}

func (m *fakeMetrics) gauge(org string) map[host.NodeStatus]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[host.NodeStatus]int)
	for k, v := range m.hosts {
		if k.org == org && v != 0 {
			out[k.status] = v
		}
	}
	return out
}

// fakeClock is a settable clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now.IsZero() {
		return time.Now()
	}
	return c.now
}

func (c *fakeClock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func newTestStateStore(t *testing.T, db *fakeDB, id uuid.UUID, org string, slot int32) host.NodeStateStore {
	t.Helper()
	store, err := db.NodeStateStore(id, org, slot)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
