package provisioner_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/sc/internal/pressure/host"
	"github.com/speakeasy-api/gram/sc/internal/pressure/provisioner"
)

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
	org, hostname, runtime string
}

type slotRow struct {
	status, nodeID, dnsName, lastError, podName string
	authKey                                     *string
	authKeyExpiresAt                            time.Time
	lastSeenAt, connectedSince, retiredAt       time.Time
}

// fakeDB is the reconciler's view of Postgres, reproducing the SQL in
// queries.sql that the original tests relied on.
type fakeDB struct {
	mu        sync.Mutex
	ingresses map[uuid.UUID]*ingressRow
	slots     map[slotKey]*slotRow
	states    map[slotKey]map[string][]byte
	certs     map[uuid.UUID]map[string][]byte
	suspended map[string]bool
	seq       int
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		ingresses: map[uuid.UUID]*ingressRow{}, slots: map[slotKey]*slotRow{}, states: map[slotKey]map[string][]byte{},
		certs: map[uuid.UUID]map[string][]byte{}, suspended: map[string]bool{},
	}
}

var _ provisioner.Store = (*fakeDB)(nil)

func (db *fakeDB) seedIngress(name string) (uuid.UUID, string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.seq++
	id := uuid.New()
	org := fmt.Sprintf("org-%d-%s", db.seq, name)
	db.ingresses[id] = &ingressRow{org: org, hostname: "private", runtime: provisioner.RuntimeGateway}
	return id, org
}

func (db *fakeDB) setRuntime(id uuid.UUID, runtime string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.ingresses[id].runtime = runtime
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

// heartbeat is HeartbeatNetworkIngressNode as a pod writes it.
func (db *fakeDB) heartbeat(id uuid.UUID, slot int32, status host.NodeStatus, nodeID string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	row, ok := db.slots[slotKey{id, slot}]
	if !ok || (!row.retiredAt.IsZero() && status != host.NodeStatusDraining && status != host.NodeStatusStopped) {
		return
	}
	now := time.Now()
	row.podName, row.status, row.lastError, row.lastSeenAt, row.dnsName = "gram-tailnet-gateway-0", string(status), "", now, "private.example.ts.net"
	if nodeID != "" {
		row.nodeID = nodeID
	}
	switch {
	case status != host.NodeStatusOnline:
		row.connectedSince = time.Time{}
	case row.connectedSince.IsZero():
		row.connectedSince = now
	}
}

func (db *fakeDB) clearAuthKey(id uuid.UUID, slot int32) {
	db.mu.Lock()
	defer db.mu.Unlock()
	row := db.slots[slotKey{id, slot}]
	row.authKey, row.authKeyExpiresAt = nil, time.Time{}
}

func (db *fakeDB) suspend(org string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.suspended[org] = true
}

func (db *fakeDB) writeCert(id uuid.UUID, name string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.certs[id] == nil {
		db.certs[id] = map[string][]byte{}
	}
	db.certs[id][name] = []byte("cert")
}

func (db *fakeDB) hasCert(id uuid.UUID) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	return len(db.certs[id]) > 0
}

func (db *fakeDB) Owner(_ context.Context, id uuid.UUID) (provisioner.Owner, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	ing, ok := db.ingresses[id]
	if !ok {
		return provisioner.Owner{}, provisioner.ErrIngressNotFound
	}
	return provisioner.Owner{OrganizationID: ing.org, Hostname: ing.hostname, Runtime: ing.runtime}, nil
}

func (db *fakeDB) EnsureSlot(_ context.Context, id uuid.UUID, _ string, slot int32) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.slots[slotKey{id, slot}]; !ok {
		db.slots[slotKey{id, slot}] = &slotRow{status: string(host.NodeStatusPending)}
	}
	return nil
}

func (db *fakeDB) RetireSlots(_ context.Context, id uuid.UUID, _ string, minSlot int32) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	for key, row := range db.slots {
		if key.ingress == id && key.slot >= minSlot && row.retiredAt.IsZero() {
			row.retiredAt = time.Now()
		}
	}
	return nil
}

func (db *fakeDB) ListSlots(_ context.Context, id uuid.UUID, _ string) ([]provisioner.SlotRow, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	var out []provisioner.SlotRow
	for key, row := range db.slots {
		if key.ingress != id {
			continue
		}
		out = append(out, provisioner.SlotRow{
			Slot: key.slot, Status: row.status, NodeID: row.nodeID, DNSName: row.dnsName,
			HasAuthKey: row.authKey != nil, AuthKeyExpiresAt: row.authKeyExpiresAt,
			LastSeenAt: row.lastSeenAt, ConnectedSince: row.connectedSince, Retired: !row.retiredAt.IsZero(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out, nil
}

func (db *fakeDB) slotRows(id uuid.UUID) []provisioner.SlotRow {
	rows, _ := db.ListSlots(context.Background(), id, "")
	return rows
}

func (db *fakeDB) authKey(id uuid.UUID, slot int32) *string {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.slots[slotKey{id, slot}].authKey
}

func (db *fakeDB) SetAuthKey(_ context.Context, id uuid.UUID, _ string, slot int32, ciphertext string, expires time.Time) (int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := slotKey{id, slot}
	row, ok := db.slots[key]
	if !ok || !row.retiredAt.IsZero() || db.registered(key) {
		return 0, nil
	}
	row.authKey, row.authKeyExpiresAt, row.status, row.lastError = &ciphertext, expires, string(host.NodeStatusPending), ""
	return 1, nil
}

func (db *fakeDB) DeleteSlot(_ context.Context, id uuid.UUID, _ string, slot int32) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if row, ok := db.slots[slotKey{id, slot}]; ok && !row.retiredAt.IsZero() {
		delete(db.slots, slotKey{id, slot})
	}
	return nil
}

func (db *fakeDB) DeleteSlotStates(_ context.Context, id uuid.UUID, _ string, slot int32) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.states, slotKey{id, slot})
	return nil
}

func (db *fakeDB) DeleteStates(_ context.Context, id uuid.UUID, _ string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	for key := range db.states {
		if key.ingress == id {
			delete(db.states, key)
		}
	}
	return nil
}

func (db *fakeDB) DeleteCertificateStates(_ context.Context, id uuid.UUID, _ string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.certs, id)
	return nil
}

func (db *fakeDB) DeleteSlots(_ context.Context, id uuid.UUID, _ string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	for key := range db.slots {
		if key.ingress == id {
			delete(db.slots, key)
		}
	}
	return nil
}

func (db *fakeDB) NodeStateStore(id uuid.UUID, org string, slot int32) (host.NodeStateStore, error) {
	return &fakeStateStore{db: db, key: slotKey{id, slot}, org: org}, nil
}

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
	if _, ok := s.db.states[s.key][knownProfilesStateKey]; ok && s.db.suspended[s.org] {
		return false, fmt.Errorf("decrypt tailnet login profiles: %w", host.ErrEncryptionKeyUnavailable)
	}
	return s.db.registered(s.key), nil
}

func (s *fakeStateStore) Reset(context.Context) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	delete(s.db.states, s.key)
	return nil
}

// fakeKeys is organization encryption: each organization's envelope names
// it, so a key sealed for one cannot be opened as another's.
type fakeKeys struct {
	db      *fakeDB
	mu      sync.Mutex
	ensured []string
}

func (k *fakeKeys) EnsureUseCase(_ context.Context, org string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.ensured = append(k.ensured, org)
	return nil
}

func (k *fakeKeys) EncryptAuthKey(_ context.Context, org, authKey string) (string, error) {
	k.db.mu.Lock()
	defer k.db.mu.Unlock()
	if k.db.suspended[org] {
		return "", fmt.Errorf("seal: %w", host.ErrEncryptionKeyUnavailable)
	}
	return "envelope:" + org + ":" + authKey, nil
}

func openAuthKey(org, ciphertext string) (string, error) {
	plain, ok := strings.CutPrefix(ciphertext, "envelope:"+org+":")
	if !ok {
		return "", errors.New("invalid ciphertext")
	}
	return plain, nil
}

// fakeTailscale is tailscaleapi.Fake.
type fakeTailscale struct {
	mu       sync.Mutex
	Reject   error
	keys     []provisioner.AuthKey
	services map[string]provisioner.Service
	deleted  []string
}

var _ provisioner.TailscaleAPI = (*fakeTailscale)(nil)

func newFakeTailscale() *fakeTailscale {
	return &fakeTailscale{services: map[string]provisioner.Service{}}
}

func (f *fakeTailscale) reject(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Reject = err
}

func (f *fakeTailscale) check(creds provisioner.Credentials) error {
	if f.Reject != nil {
		return f.Reject
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return provisioner.ErrTailscaleUnauthorized
	}
	return nil
}

func (f *fakeTailscale) CreateAuthKey(_ context.Context, creds provisioner.Credentials, req provisioner.AuthKeyRequest) (provisioner.AuthKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(creds); err != nil {
		return provisioner.AuthKey{}, err
	}
	if req.Tag == "" || req.TTL < time.Second {
		return provisioner.AuthKey{}, errors.New("auth key request requires a tag and a ttl of at least one second")
	}
	key := provisioner.AuthKey{ID: fmt.Sprintf("k%d", len(f.keys)+1), Key: fmt.Sprintf("tskey-auth-fake-%d-%s", len(f.keys)+1, req.Tag), Expires: time.Now().Add(req.TTL)}
	f.keys = append(f.keys, key)
	return key, nil
}

func (f *fakeTailscale) DeleteDevice(_ context.Context, creds provisioner.Credentials, nodeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(creds); err != nil {
		return err
	}
	f.deleted = append(f.deleted, nodeID)
	return nil
}

func (f *fakeTailscale) UpsertService(_ context.Context, creds provisioner.Credentials, svc provisioner.Service) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(creds); err != nil {
		return err
	}
	if !strings.HasPrefix(svc.Name, "svc:") || len(svc.Ports) == 0 {
		return errors.New("service definition requires a svc: name and at least one port")
	}
	f.services[svc.Name] = provisioner.Service{Name: svc.Name, Ports: slices.Clone(svc.Ports), Tags: slices.Clone(svc.Tags), Comment: svc.Comment}
	return nil
}

func (f *fakeTailscale) DeleteService(_ context.Context, creds provisioner.Credentials, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(creds); err != nil {
		return err
	}
	delete(f.services, name)
	return nil
}

func (f *fakeTailscale) Keys() []provisioner.AuthKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.keys)
}

func (f *fakeTailscale) DeletedDevices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleted)
}

func (f *fakeTailscale) Services() map[string]provisioner.Service {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]provisioner.Service, len(f.services))
	maps.Copy(out, f.services)
	return out
}
