package background

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// These tests cover the Temporal dispatch helpers reachable from the MCP
// serving path. A process without a Temporal environment must get an error
// back rather than a nil pointer dereference, which would crash the process
// when the caller runs in a goroutine.

func TestTemporalChatAnalysisSignalerSignalWithoutTemporal(t *testing.T) {
	t.Parallel()

	signaler := &TemporalChatAnalysisSignaler{TemporalEnv: nil, Logger: nil}
	require.ErrorIs(t, signaler.Signal(t.Context(), uuid.New()), temporal.ErrNotConfigured)
}

func TestTemporalChatTitleGeneratorWithoutTemporal(t *testing.T) {
	t.Parallel()

	generator := &TemporalChatTitleGenerator{TemporalEnv: nil}
	require.ErrorIs(t, generator.ScheduleChatTitleGeneration(t.Context(), "chat", "org", "project"), temporal.ErrNotConfigured)
}

func TestOpenRouterKeyRefresherScheduleWithoutTemporal(t *testing.T) {
	t.Parallel()

	refresher := &OpenRouterKeyRefresher{TemporalEnv: nil}
	require.ErrorIs(t, refresher.ScheduleOpenRouterKeyRefresh(t.Context(), "org", openrouter.KeyType(""), nil), temporal.ErrNotConfigured)
}
