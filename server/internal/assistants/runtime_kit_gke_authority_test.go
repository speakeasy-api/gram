package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The test store models the durable row independently from stale caller records.
type kitGKEAuthorityTestStore struct {
	mu   sync.Mutex
	rows map[uuid.UUID][]byte
}

func newKitGKEAuthorityTestStore() *kitGKEAuthorityTestStore {
	return &kitGKEAuthorityTestStore{rows: make(map[uuid.UUID][]byte)}
}
func (s *kitGKEAuthorityTestStore) Load(_ context.Context, _ pgx.Tx, r assistantRuntimeRecord) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if raw, ok := s.rows[r.ID]; ok {
		return append([]byte(nil), raw...), nil
	}
	return append([]byte(nil), r.BackendMetadataJSON...), nil
}
func (s *kitGKEAuthorityTestStore) Save(_ context.Context, _ pgx.Tx, r assistantRuntimeRecord, expected, next []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.rows[r.ID]
	if !ok {
		current = r.BackendMetadataJSON
	}
	if !kitGKEEqualMetadata(current, expected) {
		return fmt.Errorf("Kit GKE checkpoint authority changed")
	}
	s.rows[r.ID] = append([]byte(nil), next...)
	return nil
}

func TestKitGKEAuthorityIgnoresConfigMap(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	k.config.AuthorityStore = newKitGKEAuthorityTestStore()
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	fake := kitObject("ConfigMap", "v1", kitGKEName(r)+"-authority", k.identity(r))
	fake.Object["data"] = map[string]any{"authority.json": `{"phase":"retired"}`}
	_, err = client.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace(k.config.GKE.Namespace).Create(t.Context(), fake, metav1.CreateOptions{})
	require.NoError(t, err)
	state, handle, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "active", state.Phase)
	state.Phase = "checkpointing"
	_, err = k.saveAuthority(t.Context(), handle, state)
	require.NoError(t, err)
	// Stale caller metadata cannot undo the persisted fence.
	state, _, err = k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "checkpointing", state.Phase)
	_, err = k.saveAuthority(t.Context(), handle, state)
	require.ErrorContains(t, err, "authority changed")
	for _, action := range client.Actions() {
		if action.GetResource().Resource == "configmaps" {
			require.Equal(t, "create", action.GetVerb())
		}
	}
}
func TestKitGKEAuthorityEnvelopeDoesNotNestMetadata(t *testing.T) {
	t.Parallel()
	m, err := kitGKEAuthorityMetadata(kitGKEAuthority{Phase: "stopped", Metadata: kitGKEBackendMetadata{Checkpoint: json.RawMessage(`{"phase":"active"}`)}, SnapshotName: "snapshot", SnapshotUID: "snapshot-uid"})
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(m.Checkpoint, &fields))
	require.NotContains(t, fields, "metadata")
	require.JSONEq(t, `"stopped"`, string(fields["phase"]))
	require.JSONEq(t, `"snapshot-uid"`, string(fields["snapshot_uid"]))
}
func TestKitGKEAuthorityWritesRequireTransaction(t *testing.T) {
	t.Parallel()
	err := NewKitGKEPostgresAuthorityStore(nil).Save(t.Context(), nil, assistantRuntimeRecord{}, []byte(`{}`), []byte(`{}`))
	require.ErrorContains(t, err, "require a lifecycle transaction")
}

// Exercise Stop through the settlement transport, including a close whose ACK
// is lost after the gateway could have changed its resident-session inventory.
type kitGKEUncertainCloseProtocol struct {
	kitGKEBackendProtocol
	transport *kitCheckpointFake
}

func (p *kitGKEUncertainCloseProtocol) CloseCheckpointSessions(ctx context.Context, _ assistantRuntimeRecord) error {
	return settleKitCheckpointSessions(ctx, p.transport, "/workspace", map[string]bool{"bound": true}, true)
}
func TestKitGKEAuthorityFailedClosePermanentlyFencesCheckpoint(t *testing.T) {
	t.Parallel()
	k, client, r, fences := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	k.config.Provision.SnapshotsEnabled = true
	k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
	transport := &kitCheckpointFake{inventories: []string{kitCheckpointResident}, status: kitCheckpointSettled, closeErr: fmt.Errorf("close acknowledgement lost")}
	protocol := &kitGKEUncertainCloseProtocol{kitGKEBackendProtocol: kitGKEBackendProtocol{settled: true}, transport: transport}
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) { return protocol, nil }
	client.ClearActions()
	require.ErrorContains(t, k.Stop(t.Context(), r), "close")
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "closing", state.Phase)
	require.Contains(t, transport.calls, "session/close")
	calls := len(transport.calls)
	require.ErrorContains(t, k.Stop(t.Context(), r), "reconciliation required")
	require.Len(t, transport.calls, calls, "a retry must not reinterpret an old restorable transcript as a close ACK")
	require.Zero(t, *fences)
	for _, action := range client.Actions() {
		require.NotEqual(t, "create", action.GetVerb())
		require.NotEqual(t, "delete", action.GetVerb())
	}
	_, err = k.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "not safely restorable")
}
