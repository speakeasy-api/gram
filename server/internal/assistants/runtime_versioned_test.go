package assistants

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type readyKitBackend struct{ *stubRuntimeBackend }

func (*readyKitBackend) RuntimeReadyForAdmission(context.Context) error { return nil }

func TestVersionedRuntimeRoutesImmutableBinding(t *testing.T) {
	t.Parallel()
	legacy := &stubRuntimeBackend{name: runtimeBackendLocal}
	kit := &readyKitBackend{stubRuntimeBackend: &stubRuntimeBackend{name: runtimeBackendLocal}}
	backend := &versionedRuntimeBackend{legacy: legacy, kit: kit}
	record := testRuntimeRecord(runtimeBackendLocal)
	record.RuntimeVersion = 2
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{}))
	record.RuntimeVersion = 3
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{}))
	require.Equal(t, 1, legacy.runTurnCount)
	require.Equal(t, 1, kit.runTurnCount)
	record.RuntimeVersion = 4
	require.ErrorContains(t, backend.RunTurn(t.Context(), record, runTurnRequest{}), "unsupported assistant runtime version")
	require.Equal(t, 1, legacy.runTurnCount)
}

func TestVersionedRuntimeSwitchRequiresKitSettlementCapability(t *testing.T) {
	t.Parallel()
	legacy := &stubRuntimeBackend{name: runtimeBackendLocal}
	kit := &readyKitBackend{stubRuntimeBackend: &stubRuntimeBackend{name: runtimeBackendLocal}}
	backend := &versionedRuntimeBackend{legacy: legacy, kit: kit}
	record := testRuntimeRecord(runtimeBackendLocal)
	record.RuntimeVersion = 3
	_, err := backend.PrepareRuntimeSwitch(t.Context(), assistantRecord{}, []assistantRuntimeRecord{record}, 2)
	require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
	record.RuntimeVersion = 2
	ready, err := backend.PrepareRuntimeSwitch(t.Context(), assistantRecord{}, []assistantRuntimeRecord{record}, 3)
	require.NoError(t, err)
	require.True(t, ready.DestinationReady)
	require.True(t, ready.Settled)
	require.NotNil(t, ready.Release)
	ready.Release()
}

func TestVersionedRuntimeNeverFallsBackForUnconfiguredKit(t *testing.T) {
	t.Parallel()
	legacy := &stubRuntimeBackend{name: runtimeBackendLocal}
	backend := &versionedRuntimeBackend{legacy: legacy, kit: nil}
	_, err := backend.BackendForVersion(3)
	require.ErrorContains(t, err, "version 3 is not configured")
	record := testRuntimeRecord(runtimeBackendLocal)
	record.RuntimeVersion = 3
	require.Error(t, backend.Stop(t.Context(), record))
	require.Zero(t, legacy.stopCount)
}
