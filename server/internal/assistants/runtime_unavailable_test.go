package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUnavailableLegacyRuntimeFailsClosed(t *testing.T) {
	t.Parallel()
	backend := NewUnavailableRuntimeBackend(runtimeBackendGKE, nil)
	r := assistantRuntimeRecord{Backend: runtimeBackendGKE, RuntimeVersion: 2}
	_, err := backend.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "not configured in this ATE preview")
	_, err = backend.RecycleImage(t.Context(), r)
	require.Error(t, err)
	require.Error(t, backend.RunTurn(t.Context(), r, runTurnRequest{}))
	_, err = backend.InterruptTurn(t.Context(), r, uuid.New())
	require.Error(t, err)
	_, err = backend.Status(t.Context(), r)
	require.Error(t, err)
	require.Error(t, backend.Stop(t.Context(), r))
	require.Error(t, backend.Reap(t.Context(), r))
	require.Error(t, backend.ReapStoppedMachine(t.Context(), r))
}
