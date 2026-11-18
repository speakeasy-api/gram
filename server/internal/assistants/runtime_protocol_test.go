package assistants

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveRuntimeProtocolUsesPersistedVersion(t *testing.T) {
	t.Parallel()
	legacy := &stubRuntimeBackend{name: "legacy"}
	kit := &stubRuntimeBackend{name: "kit"}
	for _, version := range []int16{1, 2} {
		protocol, err := resolveRuntimeProtocol(version, legacy, kit)
		require.NoError(t, err)
		require.Same(t, legacy, protocol)
	}
	protocol, err := resolveRuntimeProtocol(3, legacy, kit)
	require.NoError(t, err)
	require.Same(t, kit, protocol)
}

func TestResolveRuntimeProtocolFailsClosed(t *testing.T) {
	t.Parallel()
	legacy := &stubRuntimeBackend{name: "legacy"}
	for _, version := range []int16{-1, 0, 4} {
		protocol, err := resolveRuntimeProtocol(version, legacy, legacy)
		require.ErrorContains(t, err, "unsupported assistant runtime version")
		require.Nil(t, protocol)
	}
	protocol, err := resolveRuntimeProtocol(3, legacy, nil)
	require.ErrorContains(t, err, "version 3 is not configured")
	require.Nil(t, protocol)
	protocol, err = resolveRuntimeProtocol(2, nil, legacy)
	require.ErrorContains(t, err, "version 2 is not configured")
	require.Nil(t, protocol)
}

// processEventTurn keeps legacy authorization tests focused on turn preparation.
// Production resolves the immutable runtime binding before claiming events.
func (s *ServiceCore) processEventTurn(ctx context.Context, thread assistantThreadRecord, assistant assistantRecord, runtime assistantRuntimeRecord, event assistantThreadEventRecord) ([]byte, error) {
	return s.processEventTurnWithProtocol(ctx, thread, assistant, runtime, event, s.runtime)
}
