package assistants

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func testRuntimeRecord(backend string) assistantRuntimeRecord {
	return assistantRuntimeRecord{
		ID:                  uuid.New(),
		AssistantThreadID:   uuid.Nil,
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             backend,
		BackendMetadataJSON: nil,
		State:               runtimeStateActive,
		WarmUntil:           pgtype.Timestamptz{},
	}
}

// stubRuntimeBackend records which calls land on it so router dispatch can be
// asserted by backend identity.
type stubRuntimeBackend struct {
	name       string
	serverURL  *url.URL
	imageRef   string
	reusesIdle bool

	ensureRecords           []assistantRuntimeRecord
	runTurnCount            int
	stopCount               int
	reapCount               int
	reapStoppedMachineCount int
}

func (s *stubRuntimeBackend) Backend() string               { return s.name }
func (s *stubRuntimeBackend) SupportsBackend(b string) bool { return b == s.name }
func (s *stubRuntimeBackend) ServerURL() *url.URL           { return s.serverURL }
func (s *stubRuntimeBackend) ImageRef() string              { return s.imageRef }
func (s *stubRuntimeBackend) ReusesIdleRuntimes() bool      { return s.reusesIdle }

func (s *stubRuntimeBackend) Ensure(_ context.Context, r assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	s.ensureRecords = append(s.ensureRecords, r)
	return RuntimeBackendEnsureResult{ColdStart: true, BackendMetadataJSON: []byte("{}")}, nil
}

func (s *stubRuntimeBackend) RecycleImage(_ context.Context, _ assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
}

func (s *stubRuntimeBackend) RunTurn(_ context.Context, _ assistantRuntimeRecord, _ uuid.UUID, _ string, _ string, _ string, _ []runtimeMCPServer) error {
	s.runTurnCount++
	return nil
}

func (s *stubRuntimeBackend) Status(_ context.Context, _ assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	return RuntimeBackendStatus{Configured: true, IdleSeconds: nil}, nil
}

func (s *stubRuntimeBackend) Stop(_ context.Context, _ assistantRuntimeRecord) error {
	s.stopCount++
	return nil
}

func (s *stubRuntimeBackend) Reap(_ context.Context, _ assistantRuntimeRecord) error {
	s.reapCount++
	return nil
}

func (s *stubRuntimeBackend) ReapStoppedMachine(_ context.Context, _ assistantRuntimeRecord) error {
	s.reapStoppedMachineCount++
	return nil
}

var _ RuntimeBackend = (*stubRuntimeBackend)(nil)

func newTestRouter(t *testing.T, target string) (*runtimeRouter, *stubRuntimeBackend, *stubRuntimeBackend) {
	t.Helper()
	fly := &stubRuntimeBackend{name: runtimeBackendFlyIO, serverURL: &url.URL{Scheme: "https", Host: "fly.example.com"}, imageRef: "fly:img", reusesIdle: true, ensureRecords: nil, runTurnCount: 0, stopCount: 0, reapCount: 0}
	gke := &stubRuntimeBackend{name: runtimeBackendGKE, serverURL: &url.URL{Scheme: "https", Host: "gke.example.com"}, imageRef: "gke:img", reusesIdle: false, ensureRecords: nil, runTurnCount: 0, stopCount: 0, reapCount: 0}
	router, err := newRuntimeRouter(target, map[string]RuntimeBackend{
		runtimeBackendFlyIO: fly,
		runtimeBackendGKE:   gke,
	})
	require.NoError(t, err)
	return router, fly, gke
}

func TestRuntimeRouterDispatchesByRecordBackend(t *testing.T) {
	t.Parallel()

	router, fly, gke := newTestRouter(t, runtimeBackendGKE)

	_, err := router.Ensure(t.Context(), testRuntimeRecord(runtimeBackendFlyIO))
	require.NoError(t, err)
	require.Len(t, fly.ensureRecords, 1)
	require.Empty(t, gke.ensureRecords)

	require.NoError(t, router.Reap(t.Context(), testRuntimeRecord(runtimeBackendGKE)))
	require.Equal(t, 1, gke.reapCount)
	require.Equal(t, 0, fly.reapCount)
}

func TestRuntimeRouterTargetSurface(t *testing.T) {
	t.Parallel()

	router, _, gke := newTestRouter(t, runtimeBackendGKE)
	require.Equal(t, runtimeBackendGKE, router.Backend())
	require.Equal(t, gke.serverURL, router.ServerURL())
	require.Equal(t, gke.imageRef, router.ImageRef())
	require.False(t, router.ReusesIdleRuntimes(), "gke target resolves to the gke backend's non-reuse")
	require.True(t, router.SupportsBackend(runtimeBackendFlyIO))
	require.True(t, router.SupportsBackend(runtimeBackendGKE))
	require.False(t, router.SupportsBackend("bogus"))

	flyTarget, _, _ := newTestRouter(t, runtimeBackendFlyIO)
	require.True(t, flyTarget.ReusesIdleRuntimes(), "fly target resolves to the fly backend's warm reuse")
}

func TestRuntimeRouterUnknownBackendErrors(t *testing.T) {
	t.Parallel()

	router, _, _ := newTestRouter(t, runtimeBackendGKE)
	err := router.Stop(t.Context(), testRuntimeRecord("bogus"))
	require.ErrorContains(t, err, `backend "bogus" is not configured`)
}

func TestNewRuntimeRouterRequiresTargetConfigured(t *testing.T) {
	t.Parallel()

	_, err := newRuntimeRouter(runtimeBackendGKE, map[string]RuntimeBackend{
		runtimeBackendFlyIO: &stubRuntimeBackend{name: runtimeBackendFlyIO, serverURL: nil, imageRef: "", reusesIdle: true, ensureRecords: nil, runTurnCount: 0, stopCount: 0, reapCount: 0},
	})
	require.ErrorContains(t, err, `target backend "gke" is not configured`)

	_, err = newRuntimeRouter(runtimeBackendFlyIO, map[string]RuntimeBackend{})
	require.ErrorContains(t, err, "at least one backend")
}
